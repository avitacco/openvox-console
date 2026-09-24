package nodetransport

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/voxpupuli/enterprise-console/internal/messaging"
	"github.com/voxpupuli/enterprise-console/internal/testca"
)

// startWithCRLFile starts a transport enforcing the CRL at crlPath.
func startWithCRLFile(t *testing.T, ca *testca.CA, crlPath string) *Server {
	t.Helper()

	cert, key := ca.Issue(t, "test-node-transport", true)
	s, err := New(Config{
		ListenAddr: "127.0.0.1:0",
		CertFile:   cert,
		KeyFile:    key,
		CAFile:     ca.PEMFile(t),
		CRLFile:    crlPath,
	}, messaging.Config{})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestCRL_RevokedCertificateCannotConnect(t *testing.T) {
	ca := testca.NewCA(t)
	revokedCert, revokedKey := ca.Issue(t, "revoked.example.com", false)
	goodCert, goodKey := ca.Issue(t, "good.example.com", false)
	s := startWithCRLFile(t, ca, ca.CRLFile(t, revokedCert))

	if conn, err := dialNodeWith(s.Addr(), ca, revokedCert, revokedKey); err == nil {
		conn.Close()
		t.Fatal("a node presenting a revoked certificate connected")
	}

	conn, err := dialNodeWith(s.Addr(), ca, goodCert, goodKey)
	if err != nil {
		t.Fatalf("a node with an unrevoked certificate was refused: %v", err)
	}
	conn.Close()
}

// A certificate revoked while its node is connected is cut off at the
// next CRL refresh, not merely refused on its next connect.
func TestCRL_RefreshDisconnectsNewlyRevokedNode(t *testing.T) {
	ca := testca.NewCA(t)
	nodeCert, nodeKey := ca.Issue(t, "node.example.com", false)
	crlPath := ca.CRLFile(t)
	s := startWithCRLFile(t, ca, crlPath)

	conn, err := dialNodeWith(s.Addr(), ca, nodeCert, nodeKey)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	// A file's modification time can have coarse resolution; make sure
	// the rewrite is seen as a change.
	time.Sleep(10 * time.Millisecond)
	ca.WriteCRL(t, crlPath, nodeCert)

	// What the refresh loop does each tick, without waiting a minute.
	if err := s.refreshCRL(context.Background()); err != nil {
		t.Fatalf("refreshCRL: %v", err)
	}
	s.disconnectRevokedSessions()

	deadline := time.Now().Add(5 * time.Second)
	for conn.IsConnected() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if conn.IsConnected() {
		t.Fatal("a node whose certificate was revoked is still connected after a CRL refresh")
	}
}

// A CRL must be signed by the CA it speaks for. One that is not is
// refused outright rather than silently not enforced.
func TestCRL_NotSignedByTrustedCAIsRefused(t *testing.T) {
	ca := testca.NewCA(t)
	other := testca.NewCA(t)
	cert, key := ca.Issue(t, "test-node-transport", true)

	_, err := New(Config{
		ListenAddr: "127.0.0.1:0",
		CertFile:   cert,
		KeyFile:    key,
		CAFile:     ca.PEMFile(t),
		CRLFile:    other.CRLFile(t),
	}, messaging.Config{})
	if err == nil {
		t.Fatal("a CRL signed by an untrusted CA was accepted")
	}
	if !strings.Contains(err.Error(), "not signed") {
		t.Errorf("error = %v, want one naming the signature problem", err)
	}
}

// A node may answer a request it received, and publish nothing else.
func TestNodeMayOnlyPublishReplies(t *testing.T) {
	s, ca := startTestServer(t)

	node, err := dialNode(t, s.Addr(), ca, "node.example.com")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer node.Close()

	violations := make(chan error, 4)
	node.SetErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
		violations <- err
	})

	if _, err := node.Subscribe(DispatchSubject("node.example.com"), func(m *nats.Msg) {
		_ = m.Respond([]byte("pong"))
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	_ = node.Flush()

	resp, err := s.Dispatch(context.Background(), "node.example.com", []byte("ping"), 5*time.Second)
	if err != nil {
		t.Fatalf("a node could not reply to a dispatch: %v", err)
	}
	if string(resp) != "pong" {
		t.Errorf("response = %q, want pong", resp)
	}

	// Anything that is not a reply is refused - including a publish
	// straight into the reply namespace the dispatcher listens on.
	_ = node.Publish("_INBOX.forged", []byte("forged"))
	_ = node.Flush()
	select {
	case err := <-violations:
		if !strings.Contains(strings.ToLower(err.Error()), "permissions violation") {
			t.Errorf("error = %v, want a permissions violation", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a node published outside its replies without a permissions violation")
	}
}

// A timeout beyond the response window could never be honored - the
// node's permission to reply would lapse first - so it is refused rather
// than left to wait for an answer that cannot arrive.
func TestDispatch_TimeoutBeyondResponseWindowIsRefused(t *testing.T) {
	s, _ := startTestServer(t)

	if _, err := s.Dispatch(context.Background(), "node.example.com", nil, ResponseWindow+time.Minute); err == nil {
		t.Fatal("a dispatch timeout longer than the response window was accepted")
	}
}

// Node connections are long-lived and often cross a NAT or firewall; the
// server's keepalive must be tuned below their idle timeouts (see
// PingInterval), not left at nats-server's defaults.
func TestNodeListenerCarriesKeepaliveTuning(t *testing.T) {
	ca := testca.NewCA(t)
	cert, key := ca.Issue(t, "test-node-transport", true)
	l, err := NewListener(Config{ListenAddr: "127.0.0.1:0", CertFile: cert, KeyFile: key, CAFile: ca.PEMFile(t)})
	if err != nil {
		t.Fatalf("NewListener() error: %v", err)
	}
	bus, err := messaging.StartWith(messaging.Config{Nodes: l.NodeListener()})
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer bus.Close()

	v, err := bus.Server().Varz(nil)
	if err != nil {
		t.Fatalf("Varz: %v", err)
	}
	if v.PingInterval != PingInterval || v.MaxPingsOut != MaxPingsOut {
		t.Errorf("server keepalive = %s x%d, want %s x%d", v.PingInterval, v.MaxPingsOut, PingInterval, MaxPingsOut)
	}
}
