package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDBIPURLsFallBackToLastMonth(t *testing.T) {
	urls := dbipURLs("asn", time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC))
	want := []string{
		"https://download.db-ip.com/free/dbip-asn-lite-2026-01.mmdb.gz",
		"https://download.db-ip.com/free/dbip-asn-lite-2025-12.mmdb.gz",
	}
	if len(urls) != 2 || urls[0] != want[0] || urls[1] != want[1] {
		t.Errorf("dbipURLs = %v, want %v", urls, want)
	}
}

func TestRedactHidesTokens(t *testing.T) {
	got := redact("https://ipinfo.io/data/ipinfo_lite.mmdb?token=secret")
	if got != "https://ipinfo.io/data/ipinfo_lite.mmdb?token=REDACTED" {
		t.Errorf("redact = %s", got)
	}
}

// A file that fails its check never replaces the installed one, and an
// unchanged file is not downloaded again.
func TestFetchAndInstall(t *testing.T) {
	body := []byte("good")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write(body)
	}))
	defer srv.Close()
	dir := t.TempDir()
	check := func(path string) error {
		b, _ := os.ReadFile(path)
		if string(b) != "good" {
			return os.ErrInvalid
		}
		return nil
	}
	f := file{name: "db", urls: []string{srv.URL + "/missing", srv.URL}, unpack: identity, check: check}

	data, s, err := fetch(srv.Client(), dir, f)
	if err != nil || string(data) != "good" {
		t.Fatalf("first fetch = %q, %v", data, err)
	}
	if err := install(dir, f, data, s); err != nil {
		t.Fatal(err)
	}
	if data, _, err := fetch(srv.Client(), dir, f); err != nil || data != nil {
		t.Errorf("second fetch = %q, %v; want no download", data, err)
	}
	if err := install(dir, f, []byte("bad"), s); err == nil {
		t.Error("installed a file that failed its check")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "db")); string(b) != "good" {
		t.Errorf("installed file = %q", b)
	}
}
