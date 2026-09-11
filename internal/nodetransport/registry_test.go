package nodetransport

import (
	"testing"
	"time"
)

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// TestRegistry_TracksRealConnectAndDisconnect proves the registry is fed
// by real $SYS.ACCOUNT.$G.CONNECT/.DISCONNECT events from a live node
// connection - not just its own in-memory bookkeeping.
func TestRegistry_TracksRealConnectAndDisconnect(t *testing.T) {
	s, ca := startTestServer(t)
	reg := s.Registry()

	if reg.Len() != 0 {
		t.Fatalf("Len() = %d before any node connects, want 0", reg.Len())
	}
	if reg.Lookup("node-a.example.com") {
		t.Fatal("Lookup() = true before node-a connects, want false")
	}

	conn, err := dialNode(t, s.Addr(), ca, "node-a.example.com")
	if err != nil {
		t.Fatalf("dial node-a: %v", err)
	}

	if !waitUntil(t, 2*time.Second, func() bool { return reg.Lookup("node-a.example.com") }) {
		t.Fatal("Lookup(\"node-a.example.com\") never became true after connecting")
	}
	if got := reg.Len(); got != 1 {
		t.Errorf("Len() = %d after node-a connects, want 1", got)
	}

	conn.Close()

	if !waitUntil(t, 2*time.Second, func() bool { return !reg.Lookup("node-a.example.com") }) {
		t.Fatal("Lookup(\"node-a.example.com\") never became false after disconnecting")
	}
	if got := reg.Len(); got != 0 {
		t.Errorf("Len() = %d after node-a disconnects, want 0", got)
	}
}

func TestRegistry_TracksLastConnectedAndDisconnectedTimestamps(t *testing.T) {
	s, ca := startTestServer(t)
	reg := s.Registry()

	if got := reg.LastConnected("node-a.example.com"); got != nil {
		t.Fatalf("LastConnected() = %v before ever connecting, want nil", got)
	}
	if got := reg.LastDisconnected("node-a.example.com"); got != nil {
		t.Fatalf("LastDisconnected() = %v before ever connecting, want nil", got)
	}

	before := time.Now()
	conn, err := dialNode(t, s.Addr(), ca, "node-a.example.com")
	if err != nil {
		t.Fatalf("dial node-a: %v", err)
	}

	if !waitUntil(t, 2*time.Second, func() bool { return reg.LastConnected("node-a.example.com") != nil }) {
		t.Fatal("LastConnected() never became non-nil after connecting")
	}
	connectedAt := reg.LastConnected("node-a.example.com")
	if connectedAt.Before(before) {
		t.Errorf("LastConnected() = %v, want it after %v", connectedAt, before)
	}
	if got := reg.LastDisconnected("node-a.example.com"); got != nil {
		t.Errorf("LastDisconnected() = %v while still connected, want nil", got)
	}

	conn.Close()

	if !waitUntil(t, 2*time.Second, func() bool { return reg.LastDisconnected("node-a.example.com") != nil }) {
		t.Fatal("LastDisconnected() never became non-nil after disconnecting")
	}
	disconnectedAt := reg.LastDisconnected("node-a.example.com")
	if disconnectedAt.Before(*connectedAt) {
		t.Errorf("LastDisconnected() = %v, want it after LastConnected() %v", disconnectedAt, connectedAt)
	}
	// LastConnected is preserved, not cleared, after disconnecting.
	if got := reg.LastConnected("node-a.example.com"); got == nil {
		t.Error("LastConnected() = nil after disconnecting, want it preserved")
	}
}

func TestRegistry_KnownCertnamesIncludesConnectedAndDisconnectedWithHistory(t *testing.T) {
	s, ca := startTestServer(t)
	reg := s.Registry()

	if got := reg.KnownCertnames(); len(got) != 0 {
		t.Fatalf("KnownCertnames() = %v before any node connects, want empty", got)
	}

	connA, err := dialNode(t, s.Addr(), ca, "node-a.example.com")
	if err != nil {
		t.Fatalf("dial node-a: %v", err)
	}
	connB, err := dialNode(t, s.Addr(), ca, "node-b.example.com")
	if err != nil {
		t.Fatalf("dial node-b: %v", err)
	}
	defer connB.Close()

	if !waitUntil(t, 2*time.Second, func() bool { return reg.Len() == 2 }) {
		t.Fatalf("Len() never reached 2, got %d", reg.Len())
	}

	// node-a disconnects - it should still appear in KnownCertnames
	// (with history), even though it's no longer currently connected.
	connA.Close()
	if !waitUntil(t, 2*time.Second, func() bool { return reg.LastDisconnected("node-a.example.com") != nil }) {
		t.Fatal("node-a never observed as disconnected")
	}

	got := map[string]bool{}
	for _, c := range reg.KnownCertnames() {
		got[c] = true
	}
	if !got["node-a.example.com"] || !got["node-b.example.com"] {
		t.Errorf("KnownCertnames() = %v, want both node-a.example.com (disconnected, with history) and node-b.example.com (still connected)", reg.KnownCertnames())
	}
}
