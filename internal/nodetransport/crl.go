package nodetransport

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// crlRefreshInterval is how often the CRL is re-read. It bounds how long
// a certificate revoked outside the console (with `puppetserver ca
// revoke`, say) can keep connecting. A revocation made through the
// console does not wait for it - see AnnounceCertificateRevoked.
const crlRefreshInterval = time.Minute

// crlFetchTimeout bounds one fetch of the CRL from the CA.
const crlFetchTimeout = 15 * time.Second

// revocationList is one parsed, signature-verified set of revoked
// certificates, keyed by certKey.
type revocationList struct {
	entries map[string]struct{}
}

// certKey identifies a certificate the way a CRL does: by its issuer and
// serial number. A serial alone is only unique per issuer.
func certKey(rawIssuer []byte, serial fmt.Stringer) string {
	return string(rawIssuer) + "\x00" + serial.String()
}

// crlState holds the current revocation list. The zero value enforces
// nothing: a Listener with no CRL source configured accepts any
// certificate the CA has signed, exactly as before CRLs existed.
type crlState struct {
	current atomic.Pointer[revocationList]
}

// revoked reports whether cert appears in the current list.
func (s *crlState) revoked(cert *x509.Certificate) bool {
	list := s.current.Load()
	if list == nil {
		return false
	}
	_, ok := list.entries[certKey(cert.RawIssuer, cert.SerialNumber)]
	return ok
}

// verifyConnection is installed as the node listener's
// tls.Config.VerifyConnection, so a revoked certificate fails the TLS
// handshake itself - before the connection exists as far as NATS is
// concerned. It runs after chain verification, so VerifiedChains is
// populated; every certificate in the chain but the root is checked,
// which catches a revoked intermediate as well as a revoked node.
func (s *crlState) verifyConnection(cs tls.ConnectionState) error {
	for _, chain := range cs.VerifiedChains {
		for i, cert := range chain {
			if i == len(chain)-1 && len(chain) > 1 {
				break // the root: trusted by configuration, not by CRL
			}
			if s.revoked(cert) {
				return fmt.Errorf("certificate %q (serial %s) has been revoked", cert.Subject.CommonName, cert.SerialNumber)
			}
		}
	}
	return nil
}

// parseRevocationList parses every CRL in data (PEM, possibly several
// blocks - the OpenVox CA publishes one per CA in its chain - or a
// single DER CRL) and verifies each one's signature against cas.
//
// A CRL that no configured CA signed is an error rather than ignored: a
// CRL is only meaningful from the issuer it speaks for, and silently
// dropping one would quietly stop enforcing it.
func parseRevocationList(data []byte, cas []*x509.Certificate) (*revocationList, error) {
	var ders [][]byte
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "X509 CRL" {
			ders = append(ders, block.Bytes)
		}
	}
	if len(ders) == 0 {
		if len(bytes.TrimSpace(data)) == 0 {
			return nil, errors.New("CRL is empty")
		}
		ders = [][]byte{data} // not PEM: try it as DER
	}

	list := &revocationList{entries: map[string]struct{}{}}
	for _, der := range ders {
		crl, err := x509.ParseRevocationList(der)
		if err != nil {
			return nil, fmt.Errorf("parse CRL: %w", err)
		}
		if err := verifyCRLSignature(crl, cas); err != nil {
			return nil, err
		}
		for _, e := range crl.RevokedCertificateEntries {
			list.entries[certKey(crl.RawIssuer, e.SerialNumber)] = struct{}{}
		}
	}
	return list, nil
}

func verifyCRLSignature(crl *x509.RevocationList, cas []*x509.Certificate) error {
	for _, ca := range cas {
		if !bytes.Equal(ca.RawSubject, crl.RawIssuer) {
			continue
		}
		if err := crl.CheckSignatureFrom(ca); err == nil {
			return nil
		}
	}
	return fmt.Errorf("CRL issued by %q is not signed by any configured CA", crl.Issuer.String())
}

// parseCertificates returns every certificate in a PEM bundle.
func parseCertificates(data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse CA certificate: %w", err)
		}
		certs = append(certs, cert)
	}
	return certs, nil
}

// crlSource loads the current CRL bytes. changed reports whether they
// may differ from the last successful load, so a file source can skip
// re-parsing an unchanged file.
type crlSource interface {
	load(ctx context.Context) (data []byte, changed bool, err error)
	describe() string
}

// fileCRLSource reads a CRL from disk - typically the console host's own
// Puppet agent's crl.pem, which the agent keeps current.
type fileCRLSource struct {
	path string

	mu      sync.Mutex
	modTime time.Time
	size    int64
}

func (f *fileCRLSource) load(context.Context) ([]byte, bool, error) {
	info, err := os.Stat(f.path)
	if err != nil {
		return nil, false, err
	}
	f.mu.Lock()
	unchanged := info.ModTime().Equal(f.modTime) && info.Size() == f.size
	f.mu.Unlock()
	if unchanged {
		return nil, false, nil
	}
	data, err := os.ReadFile(f.path)
	if err != nil {
		return nil, false, err
	}
	f.mu.Lock()
	f.modTime, f.size = info.ModTime(), info.Size()
	f.mu.Unlock()
	return data, true, nil
}

func (f *fileCRLSource) describe() string { return "file " + f.path }

// fetchCRLSource asks the CA for its current CRL.
type fetchCRLSource struct {
	fetch func(ctx context.Context) ([]byte, error)
}

func (f fetchCRLSource) load(ctx context.Context) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, crlFetchTimeout)
	defer cancel()
	data, err := f.fetch(ctx)
	return data, err == nil, err
}

func (fetchCRLSource) describe() string { return "the CA" }
