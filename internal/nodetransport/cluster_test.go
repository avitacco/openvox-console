package nodetransport

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/voxpupuli/enterprise-console/internal/messaging"
	"github.com/voxpupuli/enterprise-console/internal/testca"
)

const (
	testClusterSecret = "test-transport-cluster-secret"
	testLeafSecret    = "test-transport-leaf-secret"
)

// peerTLS is the peer TLS material for an instance presenting certPath
// and keyPath, trusting ca.
func peerTLS(t *testing.T, ca *testca.CA, certPath, keyPath string) *messaging.PeerTLS {
	t.Helper()
	return &messaging.PeerTLS{CertFile: certPath, KeyFile: keyPath, CAFile: ca.PEMFile(t)}
}

// nodeConfig is the node listener configuration for an instance
// presenting certPath/keyPath, trusting ca.
func nodeConfig(t *testing.T, ca *testca.CA, certPath, keyPath string) Config {
	t.Helper()
	return Config{ListenAddr: "127.0.0.1:0", CertFile: certPath, KeyFile: keyPath, CAFile: ca.PEMFile(t)}
}

// freeAddr reserves a loopback port by binding and releasing it.
func freeAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("release reserved port: %v", err)
	}
	return addr
}

// startPeeredServers starts two transports sharing one CA and peered
// together, as two console instances behind a load balancer would be.
func startPeeredServers(t *testing.T) (a, b *Server, ca *testca.CA) {
	t.Helper()

	ca = testca.NewCA(t)
	certA, keyA := ca.Issue(t, "test-node-transport-a", true)
	certB, keyB := ca.Issue(t, "test-node-transport-b", true)
	clusterA := freeAddr(t)

	a, err := New(nodeConfig(t, ca, certA, keyA), messaging.Config{
		ListenAddr: clusterA,
		Secret:     testClusterSecret,
		TLS:        peerTLS(t, ca, certA, keyA),
	})
	if err != nil {
		t.Fatalf("start transport A: %v", err)
	}
	t.Cleanup(a.Close)

	b, err = New(nodeConfig(t, ca, certB, keyB), messaging.Config{
		ListenAddr: freeAddr(t),
		Peers:      []string{clusterA},
		Secret:     testClusterSecret,
		TLS:        peerTLS(t, ca, certB, keyB),
	})
	if err != nil {
		t.Fatalf("start transport B: %v", err)
	}
	t.Cleanup(b.Close)

	return a, b, ca
}

// respondAs connects a fake node-agent to srv and answers dispatches on
// its own subject, the way internal/nodeagent does.
func respondAs(t *testing.T, srv *Server, ca *testca.CA, certname string, reply []byte) {
	t.Helper()

	conn, err := dialNode(t, srv.Addr(), ca, certname)
	if err != nil {
		t.Fatalf("connect node %q: %v", certname, err)
	}
	t.Cleanup(conn.Close)

	if _, err := conn.Subscribe(DispatchSubject(certname), func(m *nats.Msg) {
		_ = m.Respond(reply)
	}); err != nil {
		t.Fatalf("subscribe node %q: %v", certname, err)
	}
	if err := conn.Flush(); err != nil {
		t.Fatalf("flush node %q: %v", certname, err)
	}
}

// The property transport clustering exists for: an instance can dispatch
// to a node whose connection is terminated at a different instance.
// Without it, a node is reachable only from whichever instance it
// happened to connect to, and on-demand orchestration needs sticky
// routing.
func TestDispatchReachesANodeConnectedToAnotherInstance(t *testing.T) {
	a, b, ca := startPeeredServers(t)

	// The node connects to B; the dispatch is published from A.
	respondAs(t, b, ca, "node-on-b.example.com", []byte("pong"))

	var (
		resp []byte
		err  error
	)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = a.Dispatch(context.Background(), "node-on-b.example.com", []byte("ping"), 2*time.Second)
		if err == nil {
			break
		}
		// Route establishment and interest propagation are
		// asynchronous, so a dispatch sent too early reports no
		// responders. Retrying is what makes this assert "the dispatch
		// routes" rather than "it routes within one arbitrary sleep".
		if !errors.Is(err, ErrNodeNotConnected) {
			t.Fatalf("Dispatch() error: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("a dispatch from instance A never reached a node connected to instance B: %v", err)
	}
	if string(resp) != "pong" {
		t.Errorf("response = %q, want %q", resp, "pong")
	}
}

// Per-node isolation must hold across the cluster, not just within one
// instance: routing a dispatch between instances must not give a node
// any way to observe another node's traffic.
func TestSubjectIsolationHoldsAcrossInstances(t *testing.T) {
	a, b, ca := startPeeredServers(t)

	// A node on A tries to watch a node on B.
	conn, err := dialNode(t, a.Addr(), ca, "snooper.example.com")
	if err != nil {
		t.Fatalf("connect snooper: %v", err)
	}
	defer conn.Close()

	received := make(chan struct{}, 1)
	sub, err := conn.Subscribe(DispatchSubject("victim.example.com"), func(*nats.Msg) {
		received <- struct{}{}
	})
	if err != nil {
		// A refused subscription is an equally correct outcome.
		return
	}
	defer sub.Unsubscribe()
	_ = conn.Flush()

	respondAs(t, b, ca, "victim.example.com", []byte("pong"))

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, _ = a.Dispatch(context.Background(), "victim.example.com", []byte("ping"), 500*time.Millisecond)
		select {
		case <-received:
			t.Fatal("a node connected to instance A observed the dispatch traffic of a node connected to instance B")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// A node connected to no instance in the cluster is reported
// not-connected promptly, rather than after the dispatch's full timeout.
// The no-responders signal must keep working once routes exist.
func TestDispatchToANodeConnectedNowhereFailsPromptly(t *testing.T) {
	a, b, ca := startPeeredServers(t)

	// One connected node, so the cluster has interest to propagate and
	// the result is not simply "nothing is connected anywhere".
	respondAs(t, b, ca, "present.example.com", []byte("pong"))

	start := time.Now()
	_, err := a.Dispatch(context.Background(), "absent.example.com", []byte("ping"), 10*time.Second)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrNodeNotConnected) {
		t.Fatalf("Dispatch() error = %v, want ErrNodeNotConnected", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("dispatch to an absent node took %s; it waited out the timeout rather than using the no-responders signal", elapsed)
	}
}

// An instance presenting the wrong cluster secret must not become a
// peer, even with a certificate the CA issued - which every managed node
// also has. Routes authenticate with their own credential, which no node
// holds.
func TestNodeCertificateCannotJoinTheCluster(t *testing.T) {
	ca := testca.NewCA(t)
	cert, key := ca.Issue(t, "test-node-transport", true)
	clusterAddr := freeAddr(t)

	srv, err := New(nodeConfig(t, ca, cert, key), messaging.Config{
		ListenAddr: clusterAddr,
		Secret:     testClusterSecret,
		TLS:        peerTLS(t, ca, cert, key),
	})
	if err != nil {
		t.Fatalf("start transport: %v", err)
	}
	defer srv.Close()

	// An impostor peering with the wrong secret. nats-server retries a
	// rejected route in the background rather than failing startup, so
	// the assertion is that no traffic ever flows.
	impostorCert, impostorKey := ca.Issue(t, "impostor-transport", true)
	impostor, err := New(nodeConfig(t, ca, impostorCert, impostorKey), messaging.Config{
		ListenAddr: freeAddr(t),
		Peers:      []string{clusterAddr},
		Secret:     "not-the-cluster-secret",
		TLS:        peerTLS(t, ca, impostorCert, impostorKey),
	})
	if err != nil {
		t.Fatalf("start impostor: %v", err)
	}
	defer impostor.Close()

	// A node connected to the legitimate server must stay unreachable
	// from the impostor.
	respondAs(t, srv, ca, "node.example.com", []byte("pong"))

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := impostor.Dispatch(context.Background(), "node.example.com", []byte("ping"), 300*time.Millisecond); err == nil {
			t.Fatal("an instance presenting the wrong cluster secret joined the transport and dispatched to a node")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Peering without a secret is refused outright rather than opening an
// unauthenticated listener onto every node's traffic.
func TestTransportClusteringWithoutASecretIsRefused(t *testing.T) {
	ca := testca.NewCA(t)
	cert, key := ca.Issue(t, "test-node-transport", true)

	_, err := New(nodeConfig(t, ca, cert, key), messaging.Config{
		ListenAddr: freeAddr(t),
		TLS:        peerTLS(t, ca, cert, key),
	})
	if err == nil {
		t.Error("a transport peer listener started with no cluster secret configured")
	}
}

// Connection state is cluster-wide: a node connected to one instance is
// reported connected by the other, so a lookup does not depend on which
// instance answers it.
func TestRegistryReflectsConnectionsClusterWide(t *testing.T) {
	a, b, ca := startPeeredServers(t)

	respondAs(t, b, ca, "node-on-b.example.com", []byte("pong"))

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if a.Registry().Lookup("node-on-b.example.com") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("instance A does not report a node connected to instance B as connected")
}

// An edge instance attached as a leaf carries the console's internal
// bus and nothing else: it must not be able to reach a node, even one
// connected to the very instance it is attached to.
func TestLeafCannotReachNodes(t *testing.T) {
	ca := testca.NewCA(t)
	hubCert, hubKey := ca.Issue(t, "hub", true)
	leafCert, leafKey := ca.Issue(t, "edge", true)
	leafAddr := freeAddr(t)

	hub, err := New(nodeConfig(t, ca, hubCert, hubKey), messaging.Config{
		ListenAddr:     freeAddr(t),
		LeafListenAddr: leafAddr,
		Secret:         testClusterSecret,
		LeafSecret:     testLeafSecret,
		TLS:            peerTLS(t, ca, hubCert, hubKey),
	})
	if err != nil {
		t.Fatalf("start hub: %v", err)
	}
	defer hub.Close()

	edge, err := messaging.StartWith(messaging.Config{
		Leaf:       true,
		Peers:      []string{leafAddr},
		LeafSecret: testLeafSecret,
		TLS:        peerTLS(t, ca, leafCert, leafKey),
	})
	if err != nil {
		t.Fatalf("start edge: %v", err)
	}
	defer edge.Close()

	edgeDispatcher, err := NewDispatcher(edge)
	if err != nil {
		t.Fatalf("edge dispatcher: %v", err)
	}
	defer edgeDispatcher.Close()

	respondAs(t, hub, ca, "node.example.com", []byte("pong"))

	// The leaf is attached - the console account flows - before the
	// negative assertion means anything.
	received := make(chan struct{}, 1)
	if _, err := edge.Subscribe("probe", func(*nats.Msg) {
		select {
		case received <- struct{}{}:
		default:
		}
	}); err != nil {
		t.Fatalf("subscribe on edge: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	attached := false
	for !attached && time.Now().Before(deadline) {
		_ = hub.Bus().Publish("probe", nil)
		select {
		case <-received:
			attached = true
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !attached {
		t.Fatal("the edge never attached to the hub")
	}

	if _, err := hub.Dispatch(context.Background(), "node.example.com", []byte("ping"), 2*time.Second); err != nil {
		t.Fatalf("the hub cannot dispatch to its own node: %v", err)
	}
	for range 5 {
		_, err := edgeDispatcher.Dispatch(context.Background(), "node.example.com", []byte("ping"), 500*time.Millisecond)
		if !errors.Is(err, ErrNodeNotConnected) {
			t.Fatalf("dispatch from a leaf = %v, want ErrNodeNotConnected: a leaf reached the node transport", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// A certificate revoked through the console stops working everywhere at
// once: the connection is dropped by whichever instance holds it, and a
// reconnect is refused by the freshly fetched CRL.
func TestAnnouncedRevocationDisconnectsNodeAcrossCluster(t *testing.T) {
	ca := testca.NewCA(t)
	certA, keyA := ca.Issue(t, "instance-a", true)
	certB, keyB := ca.Issue(t, "instance-b", true)
	nodeCert, nodeKey := ca.Issue(t, "revoked.example.com", false)
	clusterA := freeAddr(t)

	// The CA's CRL, as FetchCRL would return it: empty until the node
	// is revoked.
	crlPath := ca.CRLFile(t)
	fetch := func(context.Context) ([]byte, error) { return os.ReadFile(crlPath) }

	cfgA := nodeConfig(t, ca, certA, keyA)
	cfgA.FetchCRL = fetch
	a, err := New(cfgA, messaging.Config{ListenAddr: clusterA, Secret: testClusterSecret, TLS: peerTLS(t, ca, certA, keyA)})
	if err != nil {
		t.Fatalf("start A: %v", err)
	}
	defer a.Close()
	cfgB := nodeConfig(t, ca, certB, keyB)
	cfgB.FetchCRL = fetch
	b, err := New(cfgB, messaging.Config{ListenAddr: freeAddr(t), Peers: []string{clusterA}, Secret: testClusterSecret, TLS: peerTLS(t, ca, certB, keyB)})
	if err != nil {
		t.Fatalf("start B: %v", err)
	}
	defer b.Close()

	// The node connects to B.
	conn, err := dialNodeWith(b.Addr(), ca, nodeCert, nodeKey)
	if err != nil {
		t.Fatalf("node connect: %v", err)
	}
	defer conn.Close()

	// Wait for the route, then revoke via A - the instance the node is
	// *not* connected to.
	waitForRoute(t, a.Bus(), b.Bus())
	ca.WriteCRL(t, crlPath, nodeCert)
	if err := AnnounceCertificateRevoked(a.Bus(), "revoked.example.com"); err != nil {
		t.Fatalf("announce: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for conn.IsConnected() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if conn.IsConnected() {
		t.Fatal("the revoked node is still connected")
	}

	if c, err := dialNodeWith(b.Addr(), ca, nodeCert, nodeKey); err == nil {
		c.Close()
		t.Fatal("the revoked node reconnected")
	}
}

// waitForRoute blocks until a message published on one bus reaches the
// other.
func waitForRoute(t *testing.T, from, to *messaging.Bus) {
	t.Helper()

	ready := make(chan struct{}, 1)
	sub, err := to.Subscribe("test.ready", func(*nats.Msg) {
		select {
		case ready <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("subscribe readiness probe: %v", err)
	}
	defer sub.Unsubscribe()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		_ = from.Publish("test.ready", nil)
		select {
		case <-ready:
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("the route never came up")
}
