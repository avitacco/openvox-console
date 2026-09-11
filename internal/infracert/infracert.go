// Package infracert derives which certnames belong to this console's
// own infrastructure - certs it presents as a client, and certs
// presented to it by servers it connects to - so the Nodes page can
// tell "this console's own service identity" apart from a managed
// node, without any new configuration. See design.md in
// add-infrastructure-cert-detection for the full rationale.
package infracert

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/url"
	"os"
)

// SelfCertname parses the leaf certificate at certFile and returns its
// Subject Common Name - the certname this console presents as a client
// when using that cert file.
func SelfCertname(certFile string) (string, error) {
	data, err := os.ReadFile(certFile)
	if err != nil {
		return "", fmt.Errorf("read certificate file %s: %w", certFile, err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return "", fmt.Errorf("no PEM block found in %s", certFile)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse certificate in %s: %w", certFile, err)
	}
	return cert.Subject.CommonName, nil
}

// PeerCertname dials addr (a URL, e.g. "https://openvoxdb:8081") with
// TLS using the given client certificate and CA, and returns the
// Common Name of the certificate the server presents - the certname of
// the server this console connects to at addr. It performs a raw TLS
// handshake, not an application-level request, so it doesn't depend on
// any particular API being reachable.
func PeerCertname(ctx context.Context, addr, certFile, keyFile, caFile string) (string, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return "", fmt.Errorf("parse address %s: %w", addr, err)
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "443")
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return "", fmt.Errorf("load client certificate: %w", err)
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return "", fmt.Errorf("read CA file %s: %w", caFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return "", fmt.Errorf("no certificates found in CA file %s", caFile)
	}

	dialer := &tls.Dialer{
		Config: &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      pool,
			ServerName:   u.Hostname(),
		},
	}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return "", fmt.Errorf("dial %s: %w", host, err)
	}
	defer conn.Close()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return "", fmt.Errorf("dial %s: connection is not TLS", host)
	}
	peerCerts := tlsConn.ConnectionState().PeerCertificates
	if len(peerCerts) == 0 {
		return "", fmt.Errorf("%s presented no certificate", host)
	}
	return peerCerts[0].Subject.CommonName, nil
}

// Set holds the certnames this console has identified as its own
// infrastructure, each paired with a short, specific reason. The zero
// value is an empty, usable Set (every Lookup misses) - matches this
// project's nil-safe convention for optional dependencies elsewhere.
type Set struct {
	reasons map[string]string
}

// Add records certname as infrastructure, with reason explaining why.
// An empty certname is ignored (SelfCertname/PeerCertname can return
// one on a certificate with no CN set, which isn't useful to record).
func (s *Set) Add(certname, reason string) {
	if certname == "" {
		return
	}
	if s.reasons == nil {
		s.reasons = map[string]string{}
	}
	s.reasons[certname] = reason
}

// Lookup reports whether certname is one of this console's own
// infrastructure identities, and if so, why.
func (s *Set) Lookup(certname string) (reason string, ok bool) {
	if s == nil {
		return "", false
	}
	reason, ok = s.reasons[certname]
	return reason, ok
}
