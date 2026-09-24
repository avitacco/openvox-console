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
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
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
// PEM files and returning their paths. serverAuth selects a certificate
// usable on both sides of a TLS connection - the shape the OpenVox CA
// issues for a host, and what a console needs for its cluster routes,
// where each instance is the server for some peers and the client of
// others - versus a client-only one.
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
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
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

// CRLFile writes a CRL signed by ca that revokes the certificates at
// certPaths (as returned by Issue), returning its path. With no paths it
// is a valid CRL revoking nothing.
func (ca *CA) CRLFile(t testing.TB, certPaths ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crl.pem")
	ca.WriteCRL(t, path, certPaths...)
	return path
}

// WriteCRL writes a CRL signed by ca revoking the certificates at
// certPaths to path, replacing whatever was there - for a test that
// needs a CRL to change underneath a running reader.
func (ca *CA) WriteCRL(t testing.TB, path string, certPaths ...string) {
	t.Helper()

	var revoked []x509.RevocationListEntry
	for _, p := range certPaths {
		revoked = append(revoked, x509.RevocationListEntry{
			SerialNumber:   LeafCert(t, p).SerialNumber,
			RevocationTime: time.Now().Add(-time.Minute),
		})
	}
	der, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		Number:                    big.NewInt(time.Now().UnixNano()),
		ThisUpdate:                time.Now().Add(-time.Minute),
		NextUpdate:                time.Now().Add(time.Hour),
		RevokedCertificateEntries: revoked,
	}, ca.Cert, ca.key)
	if err != nil {
		t.Fatalf("create CRL: %v", err)
	}
	// Written aside and renamed into place, so a reader polling path
	// never sees a half-written file.
	tmp := path + ".tmp"
	writePEM(t, tmp, "X509 CRL", der)
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("move CRL into place: %v", err)
	}
}

// LeafCert parses the first certificate in the PEM file at path.
func LeafCert(t testing.TB, path string) *x509.Certificate {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatalf("no PEM block in %s", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate in %s: %v", path, err)
	}
	return cert
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
