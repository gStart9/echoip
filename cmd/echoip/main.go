package main

import (
	"errors"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/mpolden/echoip/http"
	"github.com/mpolden/echoip/iputil"
	"github.com/mpolden/echoip/iputil/geo"
)

type multiValueFlag []string

func (f *multiValueFlag) String() string {
	return strings.Join([]string(*f), ", ")
}

func (f *multiValueFlag) Set(v string) error {
	*f = append(*f, v)
	return nil
}

func init() {
	log.SetPrefix("echoip: ")
	log.SetFlags(log.Lshortfile)
}

func main() {
	countryFile := flag.String("f", "", "Path to GeoIP country database")
	cityFile := flag.String("c", "", "Path to GeoIP city database")
	asnFile := flag.String("a", "", "Path to GeoIP ASN database")
	ipinfoFile := flag.String("ipinfo", "", "Path to IPinfo Lite database (MMDB)")
	ipfireFile := flag.String("ipfire", "", "Path to IPFire Location database")
	ip2locationFile := flag.String("ip2location", "", "Path to IP2Location database (BIN)")
	reloadInterval := flag.Duration("R", 0, "Reload databases when their files change, checked at this interval. Set to 0 to disable")
	listen := flag.String("l", ":8080", "Listening address")
	reverseLookup := flag.Bool("r", false, "Perform reverse hostname lookups")
	portLookup := flag.Bool("p", false, "Enable port lookup")
	noCustomIP := flag.Bool("L", false, "Disable custom IP lookup")
	template := flag.String("t", "html", "Path to template dir")
	cacheSize := flag.Int("C", 0, "Size of response cache. Set to 0 to disable")
	profile := flag.Bool("P", false, "Enables profiling handlers")
	sponsor := flag.Bool("s", false, "Show sponsor logo")
	noMap := flag.Bool("n", false, "Show the coordinates as text instead of embedding an OpenStreetMap map")
	var headers multiValueFlag
	flag.Var(&headers, "H", "Header to trust for remote IP, if present (e.g. X-Real-IP)")
	flag.Parse()
	if len(flag.Args()) != 0 {
		flag.Usage()
		return
	}

	r, err := openGeo(*countryFile, *cityFile, *asnFile, *ipinfoFile, *ipfireFile, *ip2locationFile, *reloadInterval)
	if err != nil {
		log.Fatal(err)
	}
	cache := http.NewCache(*cacheSize)
	server := http.New(r, cache, *profile)
	server.IPHeaders = headers
	if _, err := os.Stat(*template); err == nil {
		server.Template = *template
	} else {
		log.Printf("Not configuring default handler: Template not found: %s", *template)
	}
	if *reverseLookup {
		log.Println("Enabling reverse lookup")
		server.LookupAddr = iputil.LookupAddr
	}
	if *portLookup {
		log.Println("Enabling port lookup")
		server.LookupPort = iputil.LookupPort
	}
	if *sponsor {
		log.Println("Enabling sponsor logo")
		server.Sponsor = *sponsor
	}
	if *noMap {
		log.Println("Disabling OpenStreetMap map")
		server.NoMap = true
	}
	if *noCustomIP {
		log.Println("Disabling custom IP lookup")
		server.NoCustomIP = true
	}
	if len(headers) > 0 {
		log.Printf("Trusting remote IP from header(s): %s", headers.String())
	}
	if *cacheSize > 0 {
		log.Printf("Cache capacity set to %d", *cacheSize)
	}
	if *profile {
		log.Printf("Enabling profiling handlers")
	}
	log.Printf("Listening on http://%s", *listen)
	if err := server.ListenAndServe(*listen); err != nil {
		log.Fatal(err)
	}
}

// openGeo opens the one database source that the flags name.
func openGeo(countryFile, cityFile, asnFile, ipinfoFile, ipfireFile, ip2locationFile string, reload time.Duration) (geo.Reader, error) {
	var open func() (geo.Reader, error)
	var paths []string
	sources := 0
	if countryFile != "" || cityFile != "" || asnFile != "" {
		sources++
		open = func() (geo.Reader, error) { return geo.Open(countryFile, cityFile, asnFile) }
		for _, p := range []string{countryFile, cityFile, asnFile} {
			if p != "" {
				paths = append(paths, p)
			}
		}
	}
	for _, s := range []struct {
		path string
		open func(string) (geo.Reader, error)
	}{
		{ipinfoFile, geo.OpenIPinfo},
		{ipfireFile, geo.OpenLocation},
		{ip2locationFile, geo.OpenIP2Location},
	} {
		if s.path == "" {
			continue
		}
		sources++
		path, openPath := s.path, s.open
		open = func() (geo.Reader, error) { return openPath(path) }
		paths = []string{path}
	}
	switch {
	case sources > 1:
		return nil, errors.New("name databases from one source only")
	case sources == 0:
		return geo.Open("", "", "")
	case reload > 0:
		log.Printf("Reloading databases when they change, checked every %s", reload)
		return geo.Reloading(open, paths, reload)
	default:
		return open()
	}
}
