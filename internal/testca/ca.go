// Package testca provides a minimal self-signed CA for tests exercising
// mutual TLS (internal/nodetransport, internal/nodeagent,
// internal/orchestrator) without depending on real OpenVox CA fixtures.
// It's a normal (non-_test.go) package specifically so its exported
// helpers can be imported by other packages' tests - never imported by
// any production code path.
package testca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// CA is a throwaway self-signed certificate authority for issuing test
// leaf certificates.
type CA struct {
	Cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// NewCA generates a fresh self-signed CA.
func NewCA(t testing.TB) *CA {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA certificate: %v", err)
	}
	return &CA{Cert: cert, key: key}
}

// PEMFile writes ca's certificate to a temp file and returns its path.
func (ca *CA) PEMFile(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	writePEM(t, path, "CERTIFICATE", ca.Cert.Raw)
	return path
}

// Issue signs a leaf certificate for commonName, writing its cert/key
// PEM files and returning their paths. serverAuth selects server vs
// client extended key usage.
func (ca *CA) Issue(t testing.TB, commonName string, serverAuth bool) (certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if serverAuth {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		tmpl.DNSNames = []string{"localhost"}
	} else {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("create leaf certificate for %q: %v", commonName, err)
	}

	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	writePEM(t, certPath, "CERTIFICATE", der)

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal leaf key for %q: %v", commonName, err)
	}
	writePEM(t, keyPath, "EC PRIVATE KEY", keyDER)

	return certPath, keyPath
}

// SelfSigned issues a certificate signed by its own throwaway key, not
// by any CA - a stand-in for an untrusted client certificate.
func SelfSigned(t testing.TB, commonName string) (certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate self-signed key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create self-signed certificate: %v", err)
	}

	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	writePEM(t, certPath, "CERTIFICATE", der)

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal self-signed key: %v", err)
	}
	writePEM(t, keyPath, "EC PRIVATE KEY", keyDER)

	return certPath, keyPath
}

func writePEM(t testing.TB, path, blockType string, der []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		t.Fatalf("encode PEM to %s: %v", path, err)
	}
}
