package messaging_test

import (
	"net"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/voxpupuli/enterprise-console/internal/messaging"
)

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

	a, err := messaging.StartWith(messaging.Config{ListenAddr: addrA, Secret: secret})
	if err != nil {
		t.Fatalf("start instance A: %v", err)
	}
	defer a.Close()

	b, err := messaging.StartWith(messaging.Config{
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

	a, err := messaging.StartWith(messaging.Config{ListenAddr: addrA, Secret: secret})
	if err != nil {
		t.Fatalf("start instance A: %v", err)
	}
	defer a.Close()

	b, err := messaging.StartWith(messaging.Config{
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

	a, err := messaging.StartWith(messaging.Config{ListenAddr: addrA, Secret: "the-real-secret"})
	if err != nil {
		t.Fatalf("start instance A: %v", err)
	}
	defer a.Close()

	// nats-server retries a rejected route in the background rather than
	// failing startup, so the assertion is about delivery, not about
	// StartWith returning an error.
	impostor, err := messaging.StartWith(messaging.Config{
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
	if _, err := messaging.StartWith(messaging.Config{ListenAddr: freeAddr(t)}); err == nil {
		t.Error("a peer listener started with no secret configured")
	}
	if _, err := messaging.StartWith(messaging.Config{Peers: []string{"127.0.0.1:6222"}}); err == nil {
		t.Error("peers were dialled with no secret configured")
	}
}
