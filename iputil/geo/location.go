package geo

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
)

// The on-disk layout of an IPFire Location (libloc) database, version 1.
// Integers are big-endian, and every section offset is absolute.
const (
	locMagic         = "LOCDBXX"
	locMagicSize     = 8
	locHeaderSize    = 4192
	locSignatureSize = 2048
	locNodeSize      = 12
	locNetworkSize   = 12
	locASSize        = 8
	locCountrySize   = 8
	locNoNetwork     = 0xffffffff
	locRootV4Level   = 96
	locAddressBits   = 128
	locSigLengthsAt  = locMagicSize + 60
	locSignature1At  = locSigLengthsAt + 4
	locSignature2At  = locSignature1At + locSignatureSize
	locSignaturesEnd = locSignature2At + locSignatureSize
	locDataStartsAt  = locMagicSize + locHeaderSize
)

type locSection struct {
	offset, length uint32
}

type location struct {
	f         *os.File
	tree      locSection
	networks  locSection
	ases      []byte
	countries []byte
	pool      []byte
	rootV4    uint32
}

type locNetwork struct {
	country string
	asn     uint32
}

// OpenLocation opens an IPFire Location database.
func OpenLocation(path string) (Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	db, err := newLocation(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return db, nil
}

func newLocation(f *os.File) (*location, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	head := make([]byte, locDataStartsAt)
	if _, err := f.ReadAt(head, 0); err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	if string(head[:7]) != locMagic {
		return nil, errors.New("not an IPFire Location database")
	}
	if head[7] != 1 {
		return nil, fmt.Errorf("unsupported database version %d", head[7])
	}
	// created_at (8 bytes), vendor, description and license precede the sections.
	field := func(i int) uint32 { return binary.BigEndian.Uint32(head[locMagicSize+8+4*i:]) }
	section := func(i int, recordSize uint32) (locSection, error) {
		s := locSection{offset: field(i), length: field(i + 1)}
		if int64(s.offset)+int64(s.length) > size || s.length%recordSize != 0 {
			return s, errors.New("section out of bounds")
		}
		return s, nil
	}
	db := &location{f: f}
	ases, err := section(3, locASSize)
	if err != nil {
		return nil, err
	}
	if db.networks, err = section(5, locNetworkSize); err != nil {
		return nil, err
	}
	if db.tree, err = section(7, locNodeSize); err != nil {
		return nil, err
	}
	if db.tree.length == 0 {
		return nil, errors.New("empty network tree")
	}
	countries, err := section(9, locCountrySize)
	if err != nil {
		return nil, err
	}
	pool, err := section(11, 1)
	if err != nil {
		return nil, err
	}
	if db.ases, err = readSection(f, ases); err != nil {
		return nil, err
	}
	if db.countries, err = readSection(f, countries); err != nil {
		return nil, err
	}
	if db.pool, err = readSection(f, pool); err != nil {
		return nil, err
	}
	if len(db.pool) > 0 && db.pool[len(db.pool)-1] != 0 {
		return nil, errors.New("string pool is not terminated")
	}
	if db.rootV4, err = db.findRootV4(); err != nil {
		return nil, err
	}
	return db, nil
}

func readSection(f *os.File, s locSection) ([]byte, error) {
	buf := make([]byte, s.length)
	if _, err := f.ReadAt(buf, int64(s.offset)); err != nil {
		return nil, err
	}
	return buf, nil
}

func (db *location) node(i uint32) (zero, one, network uint32, err error) {
	if i >= db.tree.length/locNodeSize {
		return 0, 0, 0, errors.New("network tree index out of range")
	}
	var buf [locNodeSize]byte
	if _, err := db.f.ReadAt(buf[:], int64(db.tree.offset)+int64(i)*locNodeSize); err != nil {
		return 0, 0, 0, err
	}
	return binary.BigEndian.Uint32(buf[0:]), binary.BigEndian.Uint32(buf[4:]), binary.BigEndian.Uint32(buf[8:]), nil
}

func addressBit(addr net.IP, level int) uint32 {
	return uint32(addr[level/8]>>(7-uint(level%8))) & 1
}

// findRootV4 returns the node of ::ffff:0:0/96, under which IPv4 addresses live.
func (db *location) findRootV4() (uint32, error) {
	mapped := net.IPv4zero.To16()
	var i uint32
	for level := 0; level < locRootV4Level; level++ {
		zero, one, _, err := db.node(i)
		if err != nil {
			return 0, err
		}
		if addressBit(mapped, level) == 1 {
			i = one
		} else {
			i = zero
		}
		if i == 0 {
			return 0, nil
		}
	}
	return i, nil
}

// lookup returns the most specific network on the address's path through the tree.
func (db *location) lookup(ip net.IP) (locNetwork, bool, error) {
	addr := ip.To16()
	if addr == nil {
		return locNetwork{}, false, errors.New("invalid IP address")
	}
	var i uint32
	level := 0
	if ip.To4() != nil {
		if db.rootV4 == 0 {
			return locNetwork{}, false, nil
		}
		i, level = db.rootV4, locRootV4Level
	}
	found := int64(-1)
	for {
		zero, one, network, err := db.node(i)
		if err != nil {
			return locNetwork{}, false, err
		}
		if network != locNoNetwork {
			found = int64(network)
		}
		if level >= locAddressBits {
			break
		}
		if addressBit(addr, level) == 1 {
			i = one
		} else {
			i = zero
		}
		if i == 0 {
			break
		}
		level++
	}
	if found < 0 {
		return locNetwork{}, false, nil
	}
	if uint32(found) >= db.networks.length/locNetworkSize {
		return locNetwork{}, false, errors.New("network index out of range")
	}
	var buf [locNetworkSize]byte
	if _, err := db.f.ReadAt(buf[:], int64(db.networks.offset)+found*locNetworkSize); err != nil {
		return locNetwork{}, false, err
	}
	return locNetwork{
		country: string(bytes.TrimRight(buf[0:2], "\x00")),
		asn:     binary.BigEndian.Uint32(buf[4:8]),
	}, true, nil
}

func (db *location) str(offset uint32) string {
	if int(offset) >= len(db.pool) {
		return ""
	}
	s := db.pool[offset:]
	if end := bytes.IndexByte(s, 0); end >= 0 {
		s = s[:end]
	}
	return string(s)
}

func (db *location) countryName(code string) string {
	n := len(db.countries) / locCountrySize
	i := sort.Search(n, func(i int) bool {
		return string(db.countries[i*locCountrySize:i*locCountrySize+2]) >= code
	})
	if i < n && string(db.countries[i*locCountrySize:i*locCountrySize+2]) == code {
		return db.str(binary.BigEndian.Uint32(db.countries[i*locCountrySize+4:]))
	}
	return ""
}

func (db *location) asName(number uint32) string {
	n := len(db.ases) / locASSize
	i := sort.Search(n, func(i int) bool {
		return binary.BigEndian.Uint32(db.ases[i*locASSize:]) >= number
	})
	if i < n && binary.BigEndian.Uint32(db.ases[i*locASSize:]) == number {
		return db.str(binary.BigEndian.Uint32(db.ases[i*locASSize+4:]))
	}
	return ""
}

func (db *location) Country(ip net.IP) (Country, error) {
	network, ok, err := db.lookup(ip)
	if err != nil || !ok || network.country == "" {
		return Country{}, err
	}
	return Country{Name: db.countryName(network.country), ISO: network.country, IsEU: isEU(network.country)}, nil
}

func (db *location) City(net.IP) (City, error) { return City{}, nil }

func (db *location) ASN(ip net.IP) (ASN, error) {
	network, ok, err := db.lookup(ip)
	if err != nil || !ok || network.asn == 0 {
		return ASN{}, err
	}
	return ASN{AutonomousSystemNumber: uint(network.asn), AutonomousSystemOrganization: db.asName(network.asn)}, nil
}

func (db *location) IsEmpty() bool { return false }

func (db *location) Attribution() Attribution {
	return Attribution{Name: "IPFire Location", URL: "https://www.ipfire.org/location"}
}

func (db *location) Close() error { return db.f.Close() }

// VerifyLocation checks that an IPFire Location database carries a valid
// signature from the PEM-encoded ECDSA public key.
func VerifyLocation(r io.ReaderAt, size int64, publicKeyPEM []byte) error {
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil {
		return errors.New("no PEM public key found")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return err
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return errors.New("public key is not an ECDSA key")
	}
	if size < locDataStartsAt {
		return errors.New("database is shorter than its header")
	}
	head := make([]byte, locDataStartsAt)
	if _, err := r.ReadAt(head, 0); err != nil {
		return err
	}
	if string(head[:7]) != locMagic || head[7] != 1 {
		return errors.New("not a version 1 IPFire Location database")
	}
	// The digest covers the file with both signatures and their lengths zeroed.
	h := sha256.New()
	h.Write(head[:locSigLengthsAt])
	h.Write(make([]byte, locSignaturesEnd-locSigLengthsAt))
	h.Write(head[locSignaturesEnd:])
	if _, err := io.Copy(h, io.NewSectionReader(r, locDataStartsAt, size-locDataStartsAt)); err != nil {
		return err
	}
	digest := h.Sum(nil)
	for i, at := range []int{locSignature1At, locSignature2At} {
		length := int(binary.BigEndian.Uint16(head[locSigLengthsAt+2*i:]))
		if length == 0 || length > locSignatureSize {
			continue
		}
		if ecdsa.VerifyASN1(key, digest, head[at:at+length]) {
			return nil
		}
	}
	return errors.New("no valid signature")
}
