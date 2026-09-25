package messaging_test

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/voxpupuli/enterprise-console/internal/messaging"
	"github.com/voxpupuli/enterprise-console/internal/testca"
)

var (
	testCAsMu sync.Mutex
	testCAs   = map[*testing.T]*testca.CA{}
)

// caFor returns the one CA every instance in test t trusts, so the
// instances a test starts can peer with each other.
func caFor(t *testing.T) *testca.CA {
	t.Helper()
	testCAsMu.Lock()
	defer testCAsMu.Unlock()
	if ca, ok := testCAs[t]; ok {
		return ca
	}
	ca := testca.NewCA(t)
	testCAs[t] = ca
	t.Cleanup(func() {
		testCAsMu.Lock()
		delete(testCAs, t)
		testCAsMu.Unlock()
	})
	return ca
}

// peerTLSFrom issues a peer certificate from ca.
func peerTLSFrom(t *testing.T, ca *testca.CA) *messaging.PeerTLS {
	t.Helper()
	cert, key := ca.Issue(t, "console-peer-test", true)
	return &messaging.PeerTLS{CertFile: cert, KeyFile: key, CAFile: ca.PEMFile(t)}
}

// startPeer is messaging.StartWith, supplying peer TLS from the test's
// shared CA whenever cfg peers and names none of its own.
func startPeer(t *testing.T, cfg messaging.Config) (*messaging.Bus, error) {
	t.Helper()
	if cfg.TLS == nil && (cfg.ListenAddr != "" || cfg.LeafListenAddr != "" || len(cfg.Peers) > 0) {
		cfg.TLS = peerTLSFrom(t, caFor(t))
	}
	return messaging.StartWith(cfg)
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

// A single instance opens no messaging listener at all - the behavior
// the binary has always had, and the thing that must not change for a
// deployment that never opts into clustering.
func TestUnclusteredBusOpensNoListener(t *testing.T) {
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer bus.Close()

	if got := bus.ClusterAddr(); got != "" {
		t.Errorf("an unclustered bus listens on %q, want no listener", got)
	}
}

// The core property clustering exists for: an event published on one
// instance reaches a subscriber on another.
func TestEventPublishedOnOneInstanceReachesAnother(t *testing.T) {
	addrA := freeAddr(t)
	secret := "test-cluster-secret"

	a, err := startPeer(t, messaging.Config{ListenAddr: addrA, Secret: secret})
	if err != nil {
		t.Fatalf("start instance A: %v", err)
	}
	defer a.Close()

	b, err := startPeer(t, messaging.Config{
		ListenAddr: freeAddr(t),
		Peers:      []string{addrA},
		Secret:     secret,
	})
	if err != nil {
		t.Fatalf("start instance B: %v", err)
	}
	defer b.Close()

	received := make(chan string, 1)
	if _, err := b.Subscribe("test.subject", func(m *nats.Msg) {
		received <- string(m.Data)
	}); err != nil {
		t.Fatalf("subscribe on instance B: %v", err)
	}

	// The route is established asynchronously, and a publish before the
	// subscriber's interest has propagated is simply dropped - core NATS
	// has no persistence. Retrying until the deadline is what makes this
	// test assert "delivery happens" rather than "delivery happens
	// within one arbitrary sleep".
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if err := a.Publish("test.subject", []byte("hello")); err != nil {
			t.Fatalf("publish on instance A: %v", err)
		}
		select {
		case got := <-received:
			if got != "hello" {
				t.Errorf("received %q, want %q", got, "hello")
			}
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("an event published on instance A never reached the subscriber on instance B")
}

// A queue subscription delivers each message to exactly one member,
// whichever instance it runs on. This is what stops a clustered
// deployment doing a subscriber's side effect once per instance.
func TestQueueSubscriptionDeliversToExactlyOneInstance(t *testing.T) {
	addrA := freeAddr(t)
	secret := "test-cluster-secret"

	a, err := startPeer(t, messaging.Config{ListenAddr: addrA, Secret: secret})
	if err != nil {
		t.Fatalf("start instance A: %v", err)
	}
	defer a.Close()

	b, err := startPeer(t, messaging.Config{
		ListenAddr: freeAddr(t),
		Peers:      []string{addrA},
		Secret:     secret,
	})
	if err != nil {
		t.Fatalf("start instance B: %v", err)
	}
	defer b.Close()

	deliveries := make(chan string, 16)
	for _, sub := range []struct {
		name string
		bus  *messaging.Bus
	}{{"a", a}, {"b", b}} {
		name := sub.name
		if _, err := sub.bus.QueueSubscribe("test.queue", "workers", func(m *nats.Msg) {
			deliveries <- name
		}); err != nil {
			t.Fatalf("queue subscribe on instance %s: %v", name, err)
		}
	}

	// Wait for both subscriptions to be known cluster-wide before
	// publishing the message under test, otherwise "exactly one
	// delivery" could just mean the other instance had not joined yet.
	waitForClusterDelivery(t, a, b)

	if err := a.Publish("test.queue", []byte("work")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case <-deliveries:
	case <-time.After(10 * time.Second):
		t.Fatal("a queue message was never delivered to any member")
	}

	// A second delivery of the same message would mean each instance
	// handled it - precisely the duplicate-work bug queue groups exist
	// to prevent.
	select {
	case extra := <-deliveries:
		t.Errorf("the same queue message was delivered more than once (again to instance %q)", extra)
	case <-time.After(500 * time.Millisecond):
	}
}

// waitForClusterDelivery blocks until a message published on one bus is
// observed on the other, proving the route is up and interest has
// propagated.
func waitForClusterDelivery(t *testing.T, from, to *messaging.Bus) {
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

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if err := from.Publish("test.ready", nil); err != nil {
			t.Fatalf("publish readiness probe: %v", err)
		}
		select {
		case <-ready:
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("the cluster route never came up")
}

// A peer that cannot present the configured secret is refused, and
// receives nothing.
func TestPeerWithWrongSecretIsRefused(t *testing.T) {
	addrA := freeAddr(t)

	a, err := startPeer(t, messaging.Config{ListenAddr: addrA, Secret: "the-real-secret"})
	if err != nil {
		t.Fatalf("start instance A: %v", err)
	}
	defer a.Close()

	// nats-server retries a rejected route in the background rather than
	// failing startup, so the assertion is about delivery, not about
	// StartWith returning an error.
	impostor, err := startPeer(t, messaging.Config{
		ListenAddr: freeAddr(t),
		Peers:      []string{addrA},
		Secret:     "the-wrong-secret",
	})
	if err != nil {
		t.Fatalf("start impostor: %v", err)
	}
	defer impostor.Close()

	received := make(chan struct{}, 1)
	if _, err := impostor.Subscribe("test.secret", func(*nats.Msg) {
		received <- struct{}{}
	}); err != nil {
		t.Fatalf("subscribe on impostor: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := a.Publish("test.secret", []byte("secret")); err != nil {
			t.Fatalf("publish on instance A: %v", err)
		}
		select {
		case <-received:
			t.Fatal("an instance presenting the wrong cluster secret joined the bus and received events")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Peering without a secret is refused outright rather than opening an
// unauthenticated listener.
func TestClusteringWithoutASecretIsRefused(t *testing.T) {
	if _, err := startPeer(t, messaging.Config{ListenAddr: freeAddr(t)}); err == nil {
		t.Error("a peer listener started with no secret configured")
	}
	if _, err := startPeer(t, messaging.Config{Peers: []string{"127.0.0.1:6222"}}); err == nil {
		t.Error("peers were dialed with no secret configured")
	}
}

// Routes are mutual TLS: an instance holding the right secret but a
// certificate from a CA the cluster does not trust never joins.
func TestPeerWithUntrustedCertificateIsRefused(t *testing.T) {
	addrA := freeAddr(t)
	secret := "test-cluster-secret"

	a, err := startPeer(t, messaging.Config{ListenAddr: addrA, Secret: secret})
	if err != nil {
		t.Fatalf("start instance A: %v", err)
	}
	defer a.Close()

	// Trusts the cluster's CA - so the handshake is not failing merely
	// on its side - but presents a certificate the cluster does not.
	untrusted := testca.NewCA(t)
	tlsFiles := peerTLSFrom(t, untrusted)
	tlsFiles.CAFile = caFor(t).PEMFile(t)
	impostor, err := messaging.StartWith(messaging.Config{
		ListenAddr: freeAddr(t),
		Peers:      []string{addrA},
		Secret:     secret,
		TLS:        tlsFiles,
	})
	if err != nil {
		t.Fatalf("start impostor: %v", err)
	}
	defer impostor.Close()

	received := make(chan struct{}, 1)
	if _, err := impostor.Subscribe("test.tls", func(*nats.Msg) {
		received <- struct{}{}
	}); err != nil {
		t.Fatalf("subscribe on impostor: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = a.Publish("test.tls", nil)
		select {
		case <-received:
			t.Fatal("an instance with an untrusted certificate joined the bus")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Peering without TLS is refused outright: the secret would travel in
// the clear, and so would every event after it.
func TestPeeringWithoutTLSIsRefused(t *testing.T) {
	_, err := messaging.StartWith(messaging.Config{ListenAddr: freeAddr(t), Secret: "s"})
	if err == nil {
		t.Fatal("a peer listener started without TLS")
	}
}

// The leaf credential must not double as the route credential, or an
// edge host could join as a full peer and reach every node.
func TestLeafSecretMustDifferFromClusterSecret(t *testing.T) {
	_, err := startPeer(t, messaging.Config{
		ListenAddr:     freeAddr(t),
		LeafListenAddr: freeAddr(t),
		Secret:         "same",
		LeafSecret:     "same",
	})
	if err == nil {
		t.Fatal("a leaf secret equal to the cluster secret was accepted")
	}
}

// A clustered server has to open a client port (see options), but no
// network client may use it: only in-process connections and verified
// nodes are ever accepted.
func TestNetworkClientOnClusteredServerIsRefused(t *testing.T) {
	bus, err := startPeer(t, messaging.Config{ListenAddr: freeAddr(t), Secret: "test-cluster-secret"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer bus.Close()

	addr := bus.Server().Addr()
	if addr == nil {
		t.Fatal("expected a loopback client port on a clustered server")
	}
	for _, opts := range [][]nats.Option{
		nil,
		{nats.UserInfo("console-peer", "test-cluster-secret")},
		{nats.Name("in-process:CONSOLE/forged")},
	} {
		conn, err := nats.Connect("nats://"+addr.String(), append(opts, nats.NoReconnect())...)
		if err == nil {
			conn.Close()
			t.Fatal("a network client connected to the clustered server's client port")
		}
	}
}

// A subscriber that falls behind loses messages; that loss is counted
// and logged rather than silent.
func TestSlowConsumerIsReported(t *testing.T) {
	var logs strings.Builder
	var logsMu sync.Mutex
	logger := slog.New(slog.NewTextHandler(writerFunc(func(p []byte) (int, error) {
		logsMu.Lock()
		defer logsMu.Unlock()
		return logs.Write(p)
	}), nil))

	bus, err := messaging.StartWith(messaging.Config{Logger: logger})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer bus.Close()

	release := make(chan struct{})
	sub, err := bus.Subscribe("test.slow", func(*nats.Msg) { <-release })
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := sub.SetPendingLimits(1, -1); err != nil {
		t.Fatalf("set pending limits: %v", err)
	}
	for range 20 {
		_ = bus.Publish("test.slow", nil)
	}
	_ = bus.Flush()

	deadline := time.Now().Add(5 * time.Second)
	for bus.AsyncErrors() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	if bus.AsyncErrors() == 0 {
		t.Fatal("a slow consumer dropped messages without being counted")
	}
	logsMu.Lock()
	defer logsMu.Unlock()
	if !strings.Contains(logs.String(), "messages were dropped") {
		t.Errorf("slow consumer not logged; logs:\n%s", logs.String())
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// AskAll's window is a floor only for a caller willing to wait: a caller
// whose context ends gets control back then, not when the window does.
func TestAskAllReturnsWhenContextEnds(t *testing.T) {
	bus, err := messaging.StartWith(messaging.Config{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer bus.Close()

	// A responder that never answers, so AskAll has something to wait
	// for - with none at all it returns at once on no-responders.
	if _, err := bus.Subscribe("test.silent", func(*nats.Msg) {}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = bus.AskAll(ctx, "test.silent", nil, 30*time.Second)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("AskAll took %s after its context ended", elapsed)
	}
	if err == nil {
		t.Error("AskAll reported no error although its context ended")
	}
}
