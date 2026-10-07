package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T, domain string) Project {
	t.Helper()
	dir := t.TempDir()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: domain}, DNSNames: []string{domain}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(60 * 24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	cp, kp := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	os.WriteFile(cp, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	os.WriteFile(kp, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600)
	return Project{Name: domain, Mode: "alicdn", Domain: domain, CertPath: cp, KeyPath: kp, AccessKeyID: "fixture", AccessKeySecret: "fixture"}
}

func TestPreflightStopsAllWritesWhenAnyDomainIsWrong(t *testing.T) {
	a, b := fixture(t, "a.example.com"), fixture(t, "b.example.com")
	b.Domain = "wrong.example.com"
	calls := 0
	err := processProjects([]Project{a, b}, false, false, false, func(Project, *x509.Certificate) (bool, error) { calls++; return false, nil }, func(Project, []byte, []byte) error { calls++; return nil })
	if err == nil || calls != 0 {
		t.Fatalf("error=%v cloud calls=%d", err, calls)
	}
}

func TestIdempotenceAndReadOnlyVerification(t *testing.T) {
	p := fixture(t, "a.example.com")
	writes := 0
	update := func(Project, []byte, []byte) error { writes++; return nil }
	match := func(Project, *x509.Certificate) (bool, error) { return true, nil }
	if err := processProjects([]Project{p}, false, false, false, match, update); err != nil {
		t.Fatal(err)
	}
	if writes != 0 {
		t.Fatal("matching certificate was uploaded again")
	}
	different := func(Project, *x509.Certificate) (bool, error) { return false, nil }
	if err := processProjects([]Project{p}, false, true, false, different, update); err == nil {
		t.Fatal("verification should report mismatch")
	}
	if writes != 0 {
		t.Fatal("verification wrote to cloud")
	}
	denied := func(Project, *x509.Certificate) (bool, error) { return false, fmt.Errorf("permission denied") }
	if err := processProjects([]Project{p}, false, false, false, denied, update); err == nil || writes != 0 {
		t.Fatal("read failure must prevent write")
	}
}

func TestPostWriteVerificationAndSnapshot(t *testing.T) {
	p := fixture(t, "a.example.com")
	reads, writes := 0, 0
	err := processProjects([]Project{p}, false, false, false, func(Project, *x509.Certificate) (bool, error) { reads++; return reads > 1, nil }, func(_ Project, cert, key []byte) error {
		writes++
		os.WriteFile(p.CertPath, []byte("changed by renewer"), 0600)
		if _, err := x509.ParseCertificate(mustDecode(t, cert)); err != nil {
			t.Fatal(err)
		}
		return nil
	})
	if err != nil || reads != 2 || writes != 1 {
		t.Fatalf("err=%v reads=%d writes=%d", err, reads, writes)
	}
}

func mustDecode(t *testing.T, value []byte) []byte {
	t.Helper()
	b, _ := pem.Decode(value)
	if b == nil {
		t.Fatal("missing PEM")
	}
	return b.Bytes
}
