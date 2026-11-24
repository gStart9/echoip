// Command echoip-geoupdate downloads the databases of one GeoIP source into a
// directory, for echoip to read.
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mpolden/echoip/iputil/geo"
	"github.com/ulikunitz/xz"
)

// ipfireKey verifies the IPFire Location database. It is the key that libloc
// ships as signing-key.pem.
//
//go:embed ipfire-signing-key.pem
var ipfireKey []byte

const userAgent = "echoip-geoupdate"

// A file is one database that a source publishes.
type file struct {
	// name is the file written into the directory.
	name string
	// urls are tried in order until one answers.
	urls []string
	// user and password authenticate the download, when set.
	user, password string
	// unpack extracts the database from the download.
	unpack func(body []byte) ([]byte, error)
	// check rejects a database that echoip could not read.
	check func(path string) error
}

func env(name string) (string, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("%s is not set", name)
	}
	return v, nil
}

func checkMaxMindFormat(path string) error {
	r, err := geo.Open("", path, "")
	if err != nil {
		return err
	}
	return r.(io.Closer).Close()
}

func checkWith(open func(string) (geo.Reader, error)) func(string) error {
	return func(path string) error {
		r, err := open(path)
		if err != nil {
			return err
		}
		return r.(io.Closer).Close()
	}
}

func checkIPFire(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if err := geo.VerifyLocation(f, st.Size(), ipfireKey); err != nil {
		return fmt.Errorf("signature check failed: %w", err)
	}
	return checkWith(geo.OpenLocation)(path)
}

// dbipURLs returns the download for this month, then last month's, since a
// month's file appears some days into the month.
func dbipURLs(kind string, now time.Time) []string {
	var urls []string
	for _, t := range []time.Time{now, now.AddDate(0, -1, 0)} {
		urls = append(urls, fmt.Sprintf("https://download.db-ip.com/free/dbip-%s-lite-%s.mmdb.gz", kind, t.Format("2006-01")))
	}
	return urls
}

func sourceFiles(source string, now time.Time) ([]file, error) {
	switch source {
	case "dbip":
		return []file{
			{name: "city.mmdb", urls: dbipURLs("city", now), unpack: gunzip, check: checkMaxMindFormat},
			{name: "asn.mmdb", urls: dbipURLs("asn", now), unpack: gunzip, check: checkMaxMindFormat},
		}, nil
	case "maxmind":
		account, err := env("MAXMIND_ACCOUNT_ID")
		if err != nil {
			return nil, err
		}
		key, err := env("GEOIP_LICENSE_KEY")
		if err != nil {
			return nil, err
		}
		var files []file
		for name, edition := range map[string]string{"city.mmdb": "GeoLite2-City", "country.mmdb": "GeoLite2-Country", "asn.mmdb": "GeoLite2-ASN"} {
			files = append(files, file{
				name:     name,
				urls:     []string{"https://download.maxmind.com/geoip/databases/" + edition + "/download?suffix=tar.gz"},
				user:     account,
				password: key,
				unpack:   untarMMDB,
				check:    checkMaxMindFormat,
			})
		}
		return files, nil
	case "ipinfo":
		token, err := env("IPINFO_TOKEN")
		if err != nil {
			return nil, err
		}
		return []file{{
			name:   "ipinfo.mmdb",
			urls:   []string{"https://ipinfo.io/data/ipinfo_lite.mmdb?token=" + url.QueryEscape(token)},
			unpack: identity,
			check:  checkWith(geo.OpenIPinfo),
		}}, nil
	case "ipfire":
		return []file{{
			name:   "location.db",
			urls:   []string{"https://location.ipfire.org/databases/1/location.db.xz"},
			unpack: unxz,
			check:  checkIPFire,
		}}, nil
	case "ip2location":
		token, err := env("IP2LOCATION_TOKEN")
		if err != nil {
			return nil, err
		}
		return []file{{
			name:   "ip2location.bin",
			urls:   []string{"https://www.ip2location.com/download/?token=" + url.QueryEscape(token) + "&file=DB11LITEBINIPV6"},
			unpack: unzipBIN,
			check:  checkWith(geo.OpenIP2Location),
		}}, nil
	}
	return nil, fmt.Errorf("unknown source %q", source)
}

func identity(b []byte) ([]byte, error) { return b, nil }

func gunzip(b []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func unxz(b []byte) ([]byte, error) {
	r, err := xz.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func untarMMDB(b []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, errors.New("no .mmdb file in the archive")
		}
		if strings.HasSuffix(h.Name, ".mmdb") {
			return io.ReadAll(tr)
		}
	}
}

func unzipBIN(b []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		// IP2Location answers an error, such as a bad token or a download
		// limit, with a short plain-text body.
		return nil, fmt.Errorf("not a zip archive: %q", truncate(b, 200))
	}
	for _, f := range zr.File {
		if strings.HasSuffix(strings.ToUpper(f.Name), ".BIN") {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return io.ReadAll(r)
		}
	}
	return nil, errors.New("no .BIN file in the archive")
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		b = b[:n]
	}
	return string(b)
}

// state remembers what each download returned, for conditional requests.
type state struct {
	URL          string `json:"url"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
}

func statePath(dir, name string) string { return filepath.Join(dir, "."+name+".state") }

func loadState(dir, name string) state {
	var s state
	if b, err := os.ReadFile(statePath(dir, name)); err == nil {
		json.Unmarshal(b, &s)
	}
	return s
}

// fetch downloads the file unless the server reports it unchanged. It
// returns nil and no error when the file is unchanged.
func fetch(client *http.Client, dir string, f file) ([]byte, state, error) {
	prev := loadState(dir, f.name)
	_, statErr := os.Stat(filepath.Join(dir, f.name))
	var lastErr error
	for _, u := range f.urls {
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			return nil, state{}, err
		}
		req.Header.Set("User-Agent", userAgent)
		if f.user != "" {
			req.SetBasicAuth(f.user, f.password)
		}
		if statErr == nil && prev.URL == u {
			if prev.ETag != "" {
				req.Header.Set("If-None-Match", prev.ETag)
			}
			if prev.LastModified != "" {
				req.Header.Set("If-Modified-Since", prev.LastModified)
			}
		}
		res, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		switch {
		case err != nil:
			lastErr = err
		case res.StatusCode == http.StatusNotModified:
			return nil, prev, nil
		case res.StatusCode == http.StatusOK:
			return body, state{URL: u, ETag: res.Header.Get("ETag"), LastModified: res.Header.Get("Last-Modified")}, nil
		default:
			lastErr = fmt.Errorf("%s: %s", redact(u), res.Status)
		}
	}
	return nil, state{}, lastErr
}

// redact hides the credentials that some sources put in the query string.
func redact(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return "(unparseable URL)"
	}
	q := parsed.Query()
	if q.Has("token") {
		q.Set("token", "REDACTED")
		parsed.RawQuery = q.Encode()
	}
	return parsed.String()
}

// install writes the database next to its final path, checks it, and renames
// it into place.
func install(dir string, f file, data []byte, s state) error {
	tmp, err := os.CreateTemp(dir, "."+f.name+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := f.check(tmp.Name()); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, f.name)); err != nil {
		return err
	}
	b, _ := json.Marshal(s)
	return os.WriteFile(statePath(dir, f.name), b, 0o644)
}

func update(client *http.Client, source, dir string) error {
	files, err := sourceFiles(source, time.Now().UTC())
	if err != nil {
		return err
	}
	var errs []error
	for _, f := range files {
		body, s, err := fetch(client, dir, f)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.name, err))
			continue
		}
		if body == nil {
			log.Printf("%s is up to date", f.name)
			continue
		}
		data, err := f.unpack(body)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.name, err))
			continue
		}
		if err := install(dir, f, data, s); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.name, err))
			continue
		}
		log.Printf("Installed %s (%d bytes)", f.name, len(data))
	}
	return errors.Join(errs...)
}

func main() {
	log.SetPrefix("echoip-geoupdate: ")
	log.SetFlags(0)
	source := flag.String("source", "", "Database source: dbip, maxmind, ipinfo, ipfire or ip2location")
	dir := flag.String("dir", ".", "Directory to write the databases into")
	interval := flag.Duration("interval", 0, "Check for new databases at this interval. Set to 0 to check once and exit")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintln(flag.CommandLine.Output(), `
Credentials come from the environment:
  maxmind      MAXMIND_ACCOUNT_ID and GEOIP_LICENSE_KEY
  ipinfo       IPINFO_TOKEN
  ip2location  IP2LOCATION_TOKEN`)
	}
	flag.Parse()
	client := &http.Client{Timeout: 10 * time.Minute}
	for {
		err := update(client, *source, *dir)
		if *interval == 0 {
			if err != nil {
				log.Fatal(err)
			}
			return
		}
		if err != nil {
			log.Print(err)
		}
		time.Sleep(*interval)
	}
}
