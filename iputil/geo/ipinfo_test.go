package geo

import (
	"net"
	"testing"
)

// testdata/ipinfo-lite-test.mmdb holds two networks in IPinfo Lite's schema.
func TestIPinfo(t *testing.T) {
	r, err := OpenIPinfo("testdata/ipinfo-lite-test.mmdb")
	if err != nil {
		t.Fatal(err)
	}
	defer r.(*ipinfo).Close()
	tests := []struct {
		ip      string
		country Country
		asn     ASN
	}{
		{"192.0.2.1", Country{Name: "Elbonia", ISO: "EB"}, ASN{64512, "Dilbert Technologies"}},
		{"2001:db8::1", Country{Name: "Germany", ISO: "DE", IsEU: true}, ASN{64513, "Example GmbH"}},
		{"198.51.100.1", Country{}, ASN{}},
	}
	for _, tt := range tests {
		country, err := r.Country(net.ParseIP(tt.ip))
		if err != nil {
			t.Fatal(err)
		}
		if country != tt.country {
			t.Errorf("Country(%s) = %+v, want %+v", tt.ip, country, tt.country)
		}
		asn, err := r.ASN(net.ParseIP(tt.ip))
		if err != nil {
			t.Fatal(err)
		}
		if asn != tt.asn {
			t.Errorf("ASN(%s) = %+v, want %+v", tt.ip, asn, tt.asn)
		}
	}
}

func TestIP2LocationPlaceholders(t *testing.T) {
	for in, want := range map[string]string{
		"-": "",
		"This parameter is unavailable for selected data file. Please upgrade the data file.": "",
		"Invalid IP address.": "",
		"Zurich":              "Zurich",
	} {
		if got := field(in); got != want {
			t.Errorf("field(%q) = %q, want %q", in, got, want)
		}
	}
}
