package geo

import (
	"net"
	"strconv"
	"strings"

	"github.com/ip2location/ip2location-go/v9"
)

type ip2loc struct {
	db *ip2location.DB
}

// OpenIP2Location opens an IP2Location database in BIN format.
func OpenIP2Location(path string) (Reader, error) {
	db, err := ip2location.OpenDB(path)
	if err != nil {
		return nil, err
	}
	return &ip2loc{db: db}, nil
}

// field drops the placeholders that the library returns for missing data.
func field(s string) string {
	if s == "-" || strings.HasPrefix(s, "This parameter is unavailable") ||
		strings.HasPrefix(s, "Invalid IP address") || strings.HasPrefix(s, "IPv6 address missing") {
		return ""
	}
	return s
}

func (r *ip2loc) Country(ip net.IP) (Country, error) {
	rec, err := r.db.Get_all(ip.String())
	if err != nil {
		return Country{}, err
	}
	iso := field(rec.Country_short)
	return Country{Name: field(rec.Country_long), ISO: iso, IsEU: isEU(iso)}, nil
}

func (r *ip2loc) City(ip net.IP) (City, error) {
	rec, err := r.db.Get_all(ip.String())
	if err != nil {
		return City{}, err
	}
	return City{
		Name:       field(rec.City),
		RegionName: field(rec.Region),
		PostalCode: field(rec.Zipcode),
		Timezone:   field(rec.Timezone),
		Latitude:   float64(rec.Latitude),
		Longitude:  float64(rec.Longitude),
	}, nil
}

func (r *ip2loc) ASN(ip net.IP) (ASN, error) {
	rec, err := r.db.Get_all(ip.String())
	if err != nil {
		return ASN{}, err
	}
	number, _ := strconv.ParseUint(field(rec.Asn), 10, 32)
	return ASN{AutonomousSystemNumber: uint(number), AutonomousSystemOrganization: field(rec.As)}, nil
}

func (r *ip2loc) IsEmpty() bool { return false }

func (r *ip2loc) Attribution() Attribution {
	return Attribution{Name: "IP2Location LITE", URL: "https://lite.ip2location.com"}
}

func (r *ip2loc) Close() error {
	r.db.Close()
	return nil
}
