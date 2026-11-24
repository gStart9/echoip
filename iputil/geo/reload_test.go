package geo

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitForASN(t *testing.T, r Reader, ip string, want uint) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		asn, err := r.ASN(net.ParseIP(ip))
		if err != nil {
			t.Fatal(err)
		}
		if asn.AutonomousSystemNumber == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("ASN(%s) = %d, want %d", ip, asn.AutonomousSystemNumber, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// replace writes the file under a temporary name and renames it into place,
// with a modification time that differs from the previous file's.
func replace(t *testing.T, path string, data []byte, mtime time.Time) {
	t.Helper()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func TestReloadingStartsWithoutFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "location.db")
	open := func() (Reader, error) { return OpenLocation(path) }
	r := Reloading(open, []string{path}, 10*time.Millisecond)
	if !r.IsEmpty() {
		t.Error("reader without a database is not empty")
	}
	waitForASN(t, r, "192.0.2.1", 0)

	start := time.Now().Add(-time.Hour)
	replace(t, path, buildLocation(t, []testNetwork{{"192.0.2.0/24", "EB", 64512}}, nil), start)
	waitForASN(t, r, "192.0.2.1", 64512)

	replace(t, path, []byte("not a database"), start.Add(time.Minute))
	time.Sleep(50 * time.Millisecond)
	waitForASN(t, r, "192.0.2.1", 64512)

	replace(t, path, buildLocation(t, []testNetwork{{"192.0.2.0/24", "EB", 64513}}, nil), start.Add(2*time.Minute))
	waitForASN(t, r, "192.0.2.1", 64513)
}
