package messaging_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/voxpupuli/enterprise-console/internal/messaging"
)

const askWindow = 750 * time.Millisecond

// A single unclustered instance answers itself: no peer is required for
// the pattern to work, which is what keeps the single-instance
// deployment a first-class case rather than a degenerate one.
func TestAskAllCollectsALocalReply(t *testing.T) {
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer bus.Close()

	if _, err := bus.Subscribe("ask.test", func(m *nats.Msg) {
		_ = m.Respond([]byte("here"))
	}); err != nil {
		t.Fatalf("Subscribe() error: %v", err)
	}

	replies, err := bus.AskAll(context.Background(), "ask.test", nil, askWindow)
	if err != nil {
		t.Fatalf("AskAll() error: %v", err)
	}
	if len(replies) != 1 || string(replies[0]) != "here" {
		t.Errorf("replies = %q, want exactly one %q", replies, "here")
	}
}

// Replies are collected from a peer instance, not only the local one -
// the property the whole pattern exists for.
func TestAskAllCollectsRepliesFromPeers(t *testing.T) {
	const secret = "test-cluster-secret"
	addrA := freeAddr(t)

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

	for name, bus := range map[string]*messaging.Bus{"a": a, "b": b} {
		name := name
		if _, err := bus.Subscribe("ask.peers", func(m *nats.Msg) {
			_ = m.Respond([]byte(name))
		}); err != nil {
			t.Fatalf("subscribe on %s: %v", name, err)
		}
	}

	// Retried until both answer: the route and the subscription interest
	// propagate asynchronously, so an early ask legitimately sees one.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		replies, err := a.AskAll(context.Background(), "ask.peers", nil, askWindow)
		if err != nil {
			t.Fatalf("AskAll() error: %v", err)
		}
		if len(replies) == 2 {
			seen := map[string]bool{}
			for _, r := range replies {
				seen[string(r)] = true
			}
			if !seen["a"] || !seen["b"] {
				t.Errorf("replies = %v, want one from each instance", seen)
			}
			return
		}
	}
	t.Fatal("AskAll never collected a reply from both instances")
}

// A subscriber that never answers must not hold the call open: the
// window is the bound, and the replies that did arrive are still
// returned.
func TestAskAllReturnsWithinItsWindowWhenNobodyAnswers(t *testing.T) {
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer bus.Close()

	// Subscribed but deliberately silent - the wedged-instance case.
	if _, err := bus.Subscribe("ask.silent", func(*nats.Msg) {}); err != nil {
		t.Fatalf("Subscribe() error: %v", err)
	}

	start := time.Now()
	replies, err := bus.AskAll(context.Background(), "ask.silent", nil, askWindow)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("AskAll() error: %v", err)
	}
	if len(replies) != 0 {
		t.Errorf("replies = %v, want none", replies)
	}
	if elapsed < askWindow {
		t.Errorf("returned after %s, before the %s window elapsed", elapsed, askWindow)
	}
	if elapsed > askWindow+5*time.Second {
		t.Errorf("returned after %s, far beyond the %s window", elapsed, askWindow)
	}
}

// With no subscriber at all, the call still returns cleanly rather than
// erroring - "nobody is listening" is a legitimate answer.
func TestAskAllWithNoSubscriberReturnsNoReplies(t *testing.T) {
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer bus.Close()

	replies, err := bus.AskAll(context.Background(), "ask.nobody", nil, askWindow)
	if err != nil {
		t.Fatalf("AskAll() error: %v", err)
	}
	if len(replies) != 0 {
		t.Errorf("replies = %v, want none", replies)
	}
}

// LocalView counts distinct peer servers, not route connections: NATS
// opens a pool of connections per peer by default, so counting
// connections would report one peer as several.
func TestLocalViewCountsDistinctPeersNotConnections(t *testing.T) {
	const secret = "test-cluster-secret"
	addrA := freeAddr(t)

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

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if a.LocalView().RoutedPeers == 1 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("LocalView().RoutedPeers = %d with one peer connected, want 1",
		a.LocalView().RoutedPeers)
}

// An unclustered instance sees no peers at all.
func TestLocalViewOnAnUnclusteredInstance(t *testing.T) {
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer bus.Close()

	got := bus.LocalView()
	if got.RoutedPeers != 0 || got.LeafConnections != 0 {
		t.Errorf("LocalView() = %+v on an unclustered instance, want zeroes", got)
	}
}
