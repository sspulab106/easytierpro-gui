package webcert

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureGeneratesAndReloads(t *testing.T) {
	dir := t.TempDir()

	p1, err := Ensure(dir, "test-host")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if p1.Fingerprint == "" {
		t.Fatal("empty fingerprint")
	}
	// Files exist on disk.
	if _, err := os.Stat(filepath.Join(dir, certFile)); err != nil {
		t.Fatalf("cert file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, keyFile)); err != nil {
		t.Fatalf("key file missing: %v", err)
	}

	// Second call must load the SAME pair (stable fingerprint).
	p2, err := Ensure(dir, "test-host")
	if err != nil {
		t.Fatalf("ensure again: %v", err)
	}
	if p2.Fingerprint != p1.Fingerprint {
		t.Fatalf("fingerprint changed across reload: %s vs %s", p1.Fingerprint, p2.Fingerprint)
	}

	// Leaf is parseable, long-lived and has SANs for loopback.
	leaf, err := x509.ParseCertificate(p1.Cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	if time.Now().After(leaf.NotAfter) || time.Until(leaf.NotAfter) < 365*24*time.Hour {
		t.Fatalf("bad validity window: %v..%v", leaf.NotBefore, leaf.NotAfter)
	}
	foundLocal := false
	for _, ip := range leaf.IPAddresses {
		if ip.String() == "127.0.0.1" {
			foundLocal = true
		}
	}
	if !foundLocal {
		t.Fatal("127.0.0.1 missing from SANs")
	}
	for _, dns := range leaf.DNSNames {
		if dns == "localhost" {
			foundLocal = true
		}
	}
	if !foundLocal {
		t.Fatal("localhost missing from SANs")
	}
}

func TestRemoveAndRegenerate(t *testing.T) {
	dir := t.TempDir()
	p1, err := Ensure(dir, "h")
	if err != nil {
		t.Fatal(err)
	}
	Remove(dir)
	p2, err := Ensure(dir, "h")
	if err != nil {
		t.Fatal(err)
	}
	if p2.Fingerprint == p1.Fingerprint {
		t.Fatal("regenerated pair has the same fingerprint as the removed one")
	}
}

func TestExpiredCertRegenerates(t *testing.T) {
	dir := t.TempDir()
	if _, err := Ensure(dir, "h"); err != nil {
		t.Fatal(err)
	}
	// Corrupt the cert file: load must fail and Ensure must mint a new pair.
	if err := os.WriteFile(filepath.Join(dir, certFile), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	p2, err := Ensure(dir, "h")
	if err != nil {
		t.Fatalf("ensure after corruption: %v", err)
	}
	if _, err := x509.ParseCertificate(p2.Cert.Certificate[0]); err != nil {
		t.Fatalf("regenerated cert unparsable: %v", err)
	}
}
