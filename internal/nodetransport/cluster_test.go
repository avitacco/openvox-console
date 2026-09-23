package nodetransport

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/voxpupuli/enterprise-console/internal/testca"
)

const testClusterSecret = "test-transport-cluster-secret"

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

	a, err := New(Config{
		ListenAddr:    "127.0.0.1:0",
		CertFile:      certA,
		KeyFile:       keyA,
		CAFile:        ca.PEMFile(t),
		ClusterAddr:   clusterA,
		ClusterSecret: testClusterSecret,
	})
	if err != nil {
		t.Fatalf("start transport A: %v", err)
	}
	t.Cleanup(a.Close)

	b, err = New(Config{
		ListenAddr:    "127.0.0.1:0",
		CertFile:      certB,
		KeyFile:       keyB,
		CAFile:        ca.PEMFile(t),
		ClusterAddr:   freeAddr(t),
		ClusterPeers:  []string{clusterA},
		ClusterSecret: testClusterSecret,
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

// A client presenting a node certificate to the cluster peer listener
// must not become a peer - routes authenticate with their own
// credential, which no node holds.
func TestNodeCertificateCannotJoinTheCluster(t *testing.T) {
	ca := testca.NewCA(t)
	cert, key := ca.Issue(t, "test-node-transport", true)
	clusterAddr := freeAddr(t)

	srv, err := New(Config{
		ListenAddr:    "127.0.0.1:0",
		CertFile:      cert,
		KeyFile:       key,
		CAFile:        ca.PEMFile(t),
		ClusterAddr:   clusterAddr,
		ClusterSecret: testClusterSecret,
	})
	if err != nil {
		t.Fatalf("start transport: %v", err)
	}
	defer srv.Close()

	// An impostor peering with the wrong secret. nats-server retries a
	// rejected route in the background rather than failing startup, so
	// the assertion is that no traffic ever flows.
	impostorCert, impostorKey := ca.Issue(t, "impostor-transport", true)
	impostor, err := New(Config{
		ListenAddr:    "127.0.0.1:0",
		CertFile:      impostorCert,
		KeyFile:       impostorKey,
		CAFile:        ca.PEMFile(t),
		ClusterAddr:   freeAddr(t),
		ClusterPeers:  []string{clusterAddr},
		ClusterSecret: "not-the-cluster-secret",
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

	_, err := New(Config{
		ListenAddr:  "127.0.0.1:0",
		CertFile:    cert,
		KeyFile:     key,
		CAFile:      ca.PEMFile(t),
		ClusterAddr: freeAddr(t),
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
