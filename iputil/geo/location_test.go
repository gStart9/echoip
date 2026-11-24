package geo

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
)

type testNetwork struct {
	cidr    string
	country string
	asn     uint32
}

// buildLocation writes a version 1 IPFire Location database holding the
// networks, and signs it with the key when one is given.
func buildLocation(t *testing.T, networks []testNetwork, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	type node struct{ zero, one, network uint32 }
	nodes := []node{{network: locNoNetwork}}
	var netData []byte
	for i, n := range networks {
		_, ipnet, err := net.ParseCIDR(n.cidr)
		if err != nil {
			t.Fatal(err)
		}
		ones, bits := ipnet.Mask.Size()
		if bits == 32 {
			ones += locRootV4Level
		}
		addr := ipnet.IP.To16()
		cur := uint32(0)
		for level := 0; level < ones; level++ {
			one := addressBit(addr, level) == 1
			next := nodes[cur].zero
			if one {
				next = nodes[cur].one
			}
			if next == 0 {
				nodes = append(nodes, node{network: locNoNetwork})
				next = uint32(len(nodes) - 1)
				if one {
					nodes[cur].one = next
				} else {
					nodes[cur].zero = next
				}
			}
			cur = next
		}
		nodes[cur].network = uint32(i)
		rec := make([]byte, locNetworkSize)
		copy(rec, n.country)
		binary.BigEndian.PutUint32(rec[4:], n.asn)
		netData = append(netData, rec...)
	}
	var tree []byte
	for _, n := range nodes {
		tree = binary.BigEndian.AppendUint32(tree, n.zero)
		tree = binary.BigEndian.AppendUint32(tree, n.one)
		tree = binary.BigEndian.AppendUint32(tree, n.network)
	}
	pool := []byte("\x00Elbonia\x00Dilbert Technologies\x00")
	countries := append([]byte("EBEU"), 0, 0, 0, 1)
	ases := binary.BigEndian.AppendUint32(nil, 64512)
	ases = binary.BigEndian.AppendUint32(ases, 9)

	file := make([]byte, locDataStartsAt)
	copy(file, locMagic)
	file[7] = 1
	put := func(i int, v uint32) { binary.BigEndian.PutUint32(file[locMagicSize+8+4*i:], v) }
	for i, s := range [][]byte{ases, netData, tree, countries, pool} {
		field := []int{3, 5, 7, 9, 11}[i]
		put(field, uint32(len(file)))
		put(field+1, uint32(len(s)))
		file = append(file, s...)
	}
	if key != nil {
		h := sha256.New()
		h.Write(file[:locSigLengthsAt])
		h.Write(make([]byte, locSignaturesEnd-locSigLengthsAt))
		h.Write(file[locSignaturesEnd:])
		sig, err := ecdsa.SignASN1(rand.Reader, key, h.Sum(nil))
		if err != nil {
			t.Fatal(err)
		}
		binary.BigEndian.PutUint16(file[locSigLengthsAt:], uint16(len(sig)))
		copy(file[locSignature1At:], sig)
	}
	return file
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "location.db")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLocationLookup(t *testing.T) {
	db, err := OpenLocation(writeTemp(t, buildLocation(t, []testNetwork{
		{"192.0.2.0/24", "EB", 64512},
		{"192.0.2.128/25", "DE", 0},
		{"2001:db8::/32", "EB", 64512},
	}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.(*location).Close()

	tests := []struct {
		ip      string
		country Country
		asn     ASN
	}{
		{"192.0.2.1", Country{Name: "Elbonia", ISO: "EB"}, ASN{64512, "Dilbert Technologies"}},
		{"192.0.2.200", Country{ISO: "DE", IsEU: true}, ASN{}},
		{"2001:db8::1", Country{Name: "Elbonia", ISO: "EB"}, ASN{64512, "Dilbert Technologies"}},
		{"198.51.100.1", Country{}, ASN{}},
		{"2001:db9::1", Country{}, ASN{}},
	}
	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		country, err := db.Country(ip)
		if err != nil {
			t.Fatal(err)
		}
		if country != tt.country {
			t.Errorf("Country(%s) = %+v, want %+v", tt.ip, country, tt.country)
		}
		asn, err := db.ASN(ip)
		if err != nil {
			t.Fatal(err)
		}
		if asn != tt.asn {
			t.Errorf("ASN(%s) = %+v, want %+v", tt.ip, asn, tt.asn)
		}
	}
}

func TestLocationRejectsOtherFiles(t *testing.T) {
	if _, err := OpenLocation(writeTemp(t, []byte("not a database"))); err == nil {
		t.Error("opened a file that is not a database")
	}
}

func TestVerifyLocation(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	data := buildLocation(t, []testNetwork{{"192.0.2.0/24", "EB", 64512}}, key)

	if err := VerifyLocation(bytes.NewReader(data), int64(len(data)), publicPEM); err != nil {
		t.Errorf("valid signature rejected: %s", err)
	}
	tampered := bytes.Clone(data)
	tampered[len(tampered)-2] ^= 0xff
	if err := VerifyLocation(bytes.NewReader(tampered), int64(len(tampered)), publicPEM); err == nil {
		t.Error("tampered database accepted")
	}
	unsigned := buildLocation(t, []testNetwork{{"192.0.2.0/24", "EB", 64512}}, nil)
	if err := VerifyLocation(bytes.NewReader(unsigned), int64(len(unsigned)), publicPEM); err == nil {
		t.Error("unsigned database accepted")
	}
}

// TestRealLocation reads the database and key that ECHOIP_LOCATION_DB and
// ECHOIP_LOCATION_KEY name.
func TestRealLocation(t *testing.T) {
	path, keyPath := os.Getenv("ECHOIP_LOCATION_DB"), os.Getenv("ECHOIP_LOCATION_KEY")
	if path == "" || keyPath == "" {
		t.Skip("ECHOIP_LOCATION_DB and ECHOIP_LOCATION_KEY are unset")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	if err := VerifyLocation(f, st.Size(), key); err != nil {
		t.Fatalf("signature: %s", err)
	}
	db, err := OpenLocation(path)
	if err != nil {
		t.Fatal(err)
	}
	for ip, want := range map[string]uint{"8.8.8.8": 15169, "1.1.1.1": 13335, "2001:4860:4860::8888": 15169} {
		country, _ := db.Country(net.ParseIP(ip))
		asn, _ := db.ASN(net.ParseIP(ip))
		t.Logf("%s: %+v %+v", ip, country, asn)
		if asn.AutonomousSystemNumber != want {
			t.Errorf("ASN(%s) = %d, want %d", ip, asn.AutonomousSystemNumber, want)
		}
	}
}
