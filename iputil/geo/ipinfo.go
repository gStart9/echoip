package geo

import (
	"net"
	"strconv"
	"strings"

	"github.com/oschwald/maxminddb-golang"
)

type ipinfo struct {
	db *maxminddb.Reader
}

type ipinfoRecord struct {
	Country     string `maxminddb:"country"`
	CountryCode string `maxminddb:"country_code"`
	ASN         string `maxminddb:"asn"`
	ASName      string `maxminddb:"as_name"`
}

// OpenIPinfo opens an IPinfo Lite database in MMDB format.
func OpenIPinfo(path string) (Reader, error) {
	db, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	return &ipinfo{db: db}, nil
}

func (r *ipinfo) lookup(ip net.IP) (ipinfoRecord, error) {
	var rec ipinfoRecord
	err := r.db.Lookup(ip, &rec)
	return rec, err
}

func (r *ipinfo) Country(ip net.IP) (Country, error) {
	rec, err := r.lookup(ip)
	if err != nil {
		return Country{}, err
	}
	return Country{Name: rec.Country, ISO: rec.CountryCode, IsEU: isEU(rec.CountryCode)}, nil
}

func (r *ipinfo) City(net.IP) (City, error) { return City{}, nil }

func (r *ipinfo) ASN(ip net.IP) (ASN, error) {
	rec, err := r.lookup(ip)
	if err != nil {
		return ASN{}, err
	}
	number, _ := strconv.ParseUint(strings.TrimPrefix(rec.ASN, "AS"), 10, 32)
	return ASN{AutonomousSystemNumber: uint(number), AutonomousSystemOrganization: rec.ASName}, nil
}

func (r *ipinfo) IsEmpty() bool { return false }

func (r *ipinfo) Attribution() Attribution {
	return Attribution{Name: "IPinfo", URL: "https://ipinfo.io"}
}

func (r *ipinfo) Close() error { return r.db.Close() }
