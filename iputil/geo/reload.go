package geo

import (
	"io"
	"log"
	"net"
	"os"
	"sync/atomic"
	"time"
)

type fileStamp struct {
	size    int64
	modTime time.Time
}

type reloading struct {
	current atomic.Pointer[Reader]
}

// Reloading opens a Reader, and opens it again whenever one of the files changes.
// Until the files open, it serves no GeoIP data. The previous Reader is closed
// one interval after it is replaced.
func Reloading(open func() (Reader, error), paths []string, interval time.Duration) Reader {
	stamps := stampFiles(paths)
	first, err := open()
	if err != nil {
		log.Printf("Serving no GeoIP data until the database files change: %s", err)
		first = &geoip{}
	}
	r := &reloading{}
	r.current.Store(&first)
	go func() {
		var retired Reader
		for range time.Tick(interval) {
			closeReader(retired)
			retired = nil
			now := stampFiles(paths)
			if sameStamps(now, stamps) {
				continue
			}
			stamps = now
			next, err := open()
			if err != nil {
				log.Printf("Keeping the previous GeoIP database: %s", err)
				continue
			}
			retired = *r.current.Swap(&next)
			log.Printf("Reloaded GeoIP database")
		}
	}()
	return r
}

func stampFiles(paths []string) []fileStamp {
	stamps := make([]fileStamp, len(paths))
	for i, p := range paths {
		if st, err := os.Stat(p); err == nil {
			stamps[i] = fileStamp{size: st.Size(), modTime: st.ModTime()}
		}
	}
	return stamps
}

func sameStamps(a, b []fileStamp) bool {
	for i := range a {
		if a[i].size != b[i].size || !a[i].modTime.Equal(b[i].modTime) {
			return false
		}
	}
	return true
}

func closeReader(r Reader) {
	if c, ok := r.(io.Closer); ok {
		c.Close()
	}
}

func (r *reloading) get() Reader { return *r.current.Load() }

func (r *reloading) Country(ip net.IP) (Country, error) { return r.get().Country(ip) }
func (r *reloading) City(ip net.IP) (City, error)       { return r.get().City(ip) }
func (r *reloading) ASN(ip net.IP) (ASN, error)         { return r.get().ASN(ip) }
func (r *reloading) IsEmpty() bool                      { return r.get().IsEmpty() }

func (r *reloading) Attribution() Attribution {
	if a, ok := r.get().(Attributor); ok {
		return a.Attribution()
	}
	return Attribution{}
}
