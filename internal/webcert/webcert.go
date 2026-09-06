// Package webcert manages the self-signed TLS certificate for the web
// management server: generated on first use, persisted in the app-data dir,
// identified by its SHA-256 fingerprint. Fleet agents pin this fingerprint
// (trust on first use), so the pair must stay stable unless rotated.
package webcert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	certFile = "web-cert.pem"
	keyFile  = "web-key.pem"
	validFor = 10 * 365 * 24 * time.Hour
)

// Pair is a loaded certificate with its fingerprint.
type Pair struct {
	Cert        tls.Certificate
	Fingerprint string // hex SHA-256 of the leaf DER
	CertPath    string
	KeyPath     string
}

// Ensure loads the certificate from dir, generating a fresh self-signed pair
// when missing (or unparsable). hostname is used as the CN/SAN along with
// loopback and the machine's current non-loopback IPv4 addresses.
func Ensure(dir, hostname string) (*Pair, error) {
	certPath := filepath.Join(dir, certFile)
	keyPath := filepath.Join(dir, keyFile)

	if pair, err := load(certPath, keyPath); err == nil {
		return pair, nil
	}

	certDER, key, err := mint(hostname)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return nil, err
	}
	return load(certPath, keyPath)
}

// Remove deletes the persisted pair (used by rotation before re-minting).
func Remove(dir string) {
	_ = os.Remove(filepath.Join(dir, certFile))
	_ = os.Remove(filepath.Join(dir, keyFile))
}

// load reads and validates the pair, computing its fingerprint.
func load(certPath, keyPath string) (*Pair, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	if len(pair.Certificate) == 0 {
		return nil, fmt.Errorf("empty certificate chain")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	if time.Now().After(leaf.NotAfter) {
		return nil, fmt.Errorf("certificate expired")
	}
	return &Pair{
		Cert:        pair,
		Fingerprint: Fingerprint(pair.Certificate[0]),
		CertPath:    certPath,
		KeyPath:     keyPath,
	}, nil
}

// mint creates a fresh self-signed ECDSA P-256 certificate.
func mint(hostname string) ([]byte, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "easytier-pro " + hostname},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(validFor),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true, // self-signed: must be able to certify itself
		DNSNames:              []string{"localhost", hostname},
	}
	for _, ip := range localIPs() {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	return der, key, nil
}

// Fingerprint is the hex SHA-256 of a DER certificate.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// localIPs returns loopback plus every non-loopback IPv4 of this machine
// (SAN coverage so browsers that import the cert can reach all interfaces).
func localIPs() []net.IP {
	out := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifaces {
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := ipnet.IP.To4()
			if v4 != nil && !v4.IsLoopback() {
				out = append(out, v4)
			}
		}
	}
	return out
}
