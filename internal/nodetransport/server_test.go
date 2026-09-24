package nodetransport

import (
	"crypto/tls"
	"crypto/x509"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/voxpupuli/enterprise-console/internal/messaging"
	"github.com/voxpupuli/enterprise-console/internal/testca"
)

// startTestServer builds and starts a Server on an OS-assigned port,
// returning it and the CA it trusts.
func startTestServer(t *testing.T) (*Server, *testca.CA) {
	t.Helper()

	ca := testca.NewCA(t)
	serverCert, serverKey := ca.Issue(t, "test-node-transport", true)

	s, err := New(Config{
		ListenAddr: "127.0.0.1:0",
		CertFile:   serverCert,
		KeyFile:    serverKey,
		CAFile:     ca.PEMFile(t),
	}, messaging.Config{})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(s.Close)

	return s, ca
}

// dialNode connects to a test server's Addr as commonName, presenting a
// certificate issued by ca.
func dialNode(t *testing.T, addr string, ca *testca.CA, commonName string) (*nats.Conn, error) {
	t.Helper()

	certPath, keyPath := ca.Issue(t, commonName, false)
	return dialNodeWith(addr, ca, certPath, keyPath)
}

// dialNodeWith connects to addr presenting the certificate at
// certPath/keyPath.
func dialNodeWith(addr string, ca *testca.CA, certPath, keyPath string) (*nats.Conn, error) {
	clientPair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	caPool := x509.NewCertPool()
	caPool.AddCert(ca.Cert)

	return nats.Connect("tls://"+addr,
		nats.Secure(&tls.Config{
			Certificates: []tls.Certificate{clientPair},
			RootCAs:      caPool,
		}),
		// A test that expects to be cut off wants to see it, not have
		// the client quietly reconnect.
		nats.NoReconnect(),
	)
}

func TestServer_AcceptsConnectionWithValidCASignedCert(t *testing.T) {
	s, ca := startTestServer(t)

	conn, err := dialNode(t, s.Addr(), ca, "web01.example.com")
	if err != nil {
		t.Fatalf("dial with a CA-signed client cert should succeed, got: %v", err)
	}
	conn.Close()
}

func TestServer_RejectsConnectionWithUntrustedCert(t *testing.T) {
	s, ca := startTestServer(t)
	// Client cert signed by its own key, not by ca.
	untrustedCert, untrustedKey := testca.SelfSigned(t, "attacker.example.com")

	clientPair, err := tls.LoadX509KeyPair(untrustedCert, untrustedKey)
	if err != nil {
		t.Fatalf("load untrusted client cert: %v", err)
	}
	caPool := x509.NewCertPool()
	caPool.AddCert(ca.Cert)

	_, err = nats.Connect("tls://"+s.Addr(), nats.Secure(&tls.Config{
		Certificates: []tls.Certificate{clientPair},
		RootCAs:      caPool,
	}))
	if err == nil {
		t.Fatal("nats.Connect() with an untrusted client cert should fail, got no error")
	}
}
