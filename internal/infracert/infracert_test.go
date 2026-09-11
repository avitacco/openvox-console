package infracert

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/testca"
)

func TestSelfCertname(t *testing.T) {
	ca := testca.NewCA(t)
	certPath, _ := ca.Issue(t, "web01.example.com", false)

	got, err := SelfCertname(certPath)
	if err != nil {
		t.Fatalf("SelfCertname() error: %v", err)
	}
	if got != "web01.example.com" {
		t.Errorf("SelfCertname() = %q, want %q", got, "web01.example.com")
	}
}

func TestSelfCertname_MissingFile(t *testing.T) {
	if _, err := SelfCertname("/nonexistent/cert.pem"); err == nil {
		t.Fatal("SelfCertname() error = nil, want an error for a missing file")
	}
}

func TestPeerCertname(t *testing.T) {
	ca := testca.NewCA(t)
	caFile := ca.PEMFile(t)
	serverCertPath, serverKeyPath := ca.Issue(t, "test-server", true)
	clientCertPath, clientKeyPath := ca.Issue(t, "test-client", false)

	serverCert, err := tls.LoadX509KeyPair(serverCertPath, serverKeyPath)
	if err != nil {
		t.Fatalf("load server cert: %v", err)
	}

	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{serverCert},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.(*tls.Conn).Handshake()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addr := "https://" + listener.Addr().String()
	got, err := PeerCertname(ctx, addr, clientCertPath, clientKeyPath, caFile)
	if err != nil {
		t.Fatalf("PeerCertname() error: %v", err)
	}
	if got != "test-server" {
		t.Errorf("PeerCertname() = %q, want %q", got, "test-server")
	}
}

func TestSet_LookupPopulated(t *testing.T) {
	var s Set
	s.Add("openvoxdb", "the certificate openvoxdb uses for its own server identity")

	reason, ok := s.Lookup("openvoxdb")
	if !ok {
		t.Fatal("Lookup(openvoxdb) ok = false, want true")
	}
	if reason != "the certificate openvoxdb uses for its own server identity" {
		t.Errorf("Lookup(openvoxdb) reason = %q, want the configured reason", reason)
	}
}

func TestSet_LookupMiss(t *testing.T) {
	var s Set
	s.Add("openvoxdb", "some reason")

	if _, ok := s.Lookup("web01.example.com"); ok {
		t.Error("Lookup(web01.example.com) ok = true, want false (never added)")
	}
}

func TestSet_EmptySet(t *testing.T) {
	var s Set
	if _, ok := s.Lookup("anything"); ok {
		t.Error("Lookup on empty Set ok = true, want false")
	}
}

func TestSet_NilSet(t *testing.T) {
	var s *Set
	if _, ok := s.Lookup("anything"); ok {
		t.Error("Lookup on nil *Set ok = true, want false")
	}
}

func TestPeerCertname_Unreachable(t *testing.T) {
	ca := testca.NewCA(t)
	caFile := ca.PEMFile(t)
	clientCertPath, clientKeyPath := ca.Issue(t, "test-client", false)

	// Find a free port, then don't listen on it.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	addr := "https://" + l.Addr().String()
	l.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := PeerCertname(ctx, addr, clientCertPath, clientKeyPath, caFile); err == nil {
		t.Fatal("PeerCertname() error = nil, want an error for an unreachable address")
	}
}
