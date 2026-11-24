# echoip

[![ci](https://github.com/mpolden/echoip/actions/workflows/ci.yml/badge.svg)](https://github.com/mpolden/echoip/actions/workflows/ci.yml)

A simple service for looking up your IP address. This is the code that powers
https://ifconfig.co.

## Usage

Just the business, please:

```
$ curl ifconfig.co
127.0.0.1

$ http ifconfig.co
127.0.0.1

$ wget -qO- ifconfig.co
127.0.0.1

$ fetch -qo- https://ifconfig.co
127.0.0.1

$ bat -print=b ifconfig.co/ip
127.0.0.1
```

Country and city lookup:

```
$ curl ifconfig.co/country
Elbonia

$ curl ifconfig.co/country-iso
EB

$ curl ifconfig.co/city
Bornyasherk

$ curl ifconfig.co/asn
AS31337

$ curl ifconfig.co/asn-org
Dilbert Technologies
```

As JSON:

```
$ curl -H 'Accept: application/json' ifconfig.co  # or curl ifconfig.co/json
{
  "city": "Bornyasherk",
  "country": "Elbonia",
  "country_iso": "EB",
  "ip": "127.0.0.1",
  "ip_decimal": 2130706433,
  "asn": "AS31337",
  "asn_org": "Dilbert Technologies"
}
```

Port testing:

```
$ curl ifconfig.co/port/80
{
  "ip": "127.0.0.1",
  "port": 80,
  "reachable": false,
  "status": "refused"
}
```

Pass the appropriate flag (usually `-4` and `-6`) to your client to switch
between IPv4 and IPv6 lookup.

## Features

* Easy to remember domain name
* Fast
* Supports IPv6
* Supports HTTPS
* Supports common command-line clients (e.g. `curl`, `httpie`, `ht`, `wget` and `fetch`)
* JSON output
* ASN, country and city lookup, using data from MaxMind
* Port testing
* All endpoints (except `/port`) can return information about a custom IP address specified via `?ip=` query parameter
* Open source under the [BSD 3-Clause license](https://opensource.org/licenses/BSD-3-Clause)

## Why?

* To scratch an itch
* An excuse to use Go for something
* Faster than ifconfig.me and has IPv6 support

## Building

Compiling requires the [Golang compiler](https://golang.org/) to be installed.
This package can be installed with:

`go install github.com/mpolden/echoip/...@latest`

For more information on building a Go project, see the [official Go
documentation](https://golang.org/doc/code.html).

## Docker image

A Docker image is available on [Docker
Hub](https://hub.docker.com/r/mpolden/echoip), which can be downloaded with:

`docker pull mpolden/echoip`

## Geolocation data

`echoip` can show additional information about IP addresses, such as their
country, city and ASN, from one of these sources:

| Source | Signup | Data | Flags |
| --- | --- | --- | --- |
| [MaxMind GeoLite2](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data) | account and license key | country, city, ASN | `-f`, `-c`, `-a` |
| [DB-IP Lite](https://db-ip.com/db/lite.php) | none | country, city, ASN | `-f`, `-c`, `-a` |
| [IPFire Location](https://www.ipfire.org/location) | none | country, ASN | `-ipfire` |
| [IPinfo Lite](https://ipinfo.io/lite) | free token | country, ASN | `-ipinfo` |
| [IP2Location LITE](https://lite.ip2location.com) | account and token | country, region, city, coordinates | `-ip2location` |

The page credits the source of its data. Name databases from one source only.
`-R 1h` reloads them when their files change.

`echoip-geoupdate` downloads one source's databases into a directory, checks
them (including the signature of the IPFire database) and replaces the files
atomically:

```
$ echoip-geoupdate -source ipfire -dir data
$ echoip -ipfire data/location.db -R 1h
```

With `-interval 24h` it keeps checking, and skips files that have not changed.
It reads credentials from `MAXMIND_ACCOUNT_ID` and `GEOIP_LICENSE_KEY`,
`IPINFO_TOKEN`, or `IP2LOCATION_TOKEN`.

The MaxMind databases can also be downloaded with:

`GEOIP_LICENSE_KEY=<key> MAXMIND_ACCOUNT_ID=<account-id> make geoip-download`

Downloading requires a MaxMind account and license key. See the following links for more information:

- https://dev.maxmind.com/geoip/geolite2-free-geolocation-data
- https://dev.maxmind.com/geoip/updating-databases/#directly-downloading-databases

### Usage

```
$ echoip -h
Usage of echoip:
  -C int
        Size of response cache. Set to 0 to disable
  -H value
        Header to trust for remote IP, if present (e.g. X-Real-IP)
  -P    Enables profiling handlers
  -R duration
        Reload databases when their files change, checked at this interval. Set to 0 to disable
  -a string
        Path to GeoIP ASN database
  -c string
        Path to GeoIP city database
  -f string
        Path to GeoIP country database
  -ip2location string
        Path to IP2Location database (BIN)
  -ipfire string
        Path to IPFire Location database
  -ipinfo string
        Path to IPinfo Lite database (MMDB)
  -l string
        Listening address (default ":8080")
  -L    Disable custom IP lookup
  -p    Enable port lookup
  -r    Perform reverse hostname lookups
  -s    Show sponsor logo
  -t string
        Path to template dir (default "html")
  -n    Show the coordinates as text instead of embedding an OpenStreetMap map
```
