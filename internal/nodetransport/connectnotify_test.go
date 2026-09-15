package nodetransport

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/voxpupuli/enterprise-console/internal/testca"
)

// startTestServerWithObserver is startTestServer plus an OnNodeConnect
// observer - kept separate so the shared helper's signature stays as it
// is for every test that does not care about notifications.
func startTestServerWithObserver(t *testing.T, onConnect func(certname string)) (*Server, *testca.CA) {
	t.Helper()

	ca := testca.NewCA(t)
	serverCert, serverKey := ca.Issue(t, "test-node-transport", true)

	s, err := New(Config{
		ListenAddr:    "127.0.0.1:0",
		CertFile:      serverCert,
		KeyFile:       serverKey,
		CAFile:        ca.PEMFile(t),
		OnNodeConnect: onConnect,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(s.Close)

	return s, ca
}

func TestOnNodeConnect_ReceivesCertnameWhenNodeConnects(t *testing.T) {
	got := make(chan string, 4)
	s, ca := startTestServerWithObserver(t, func(certname string) {
		got <- certname
	})

	conn, err := dialNode(t, s.Addr(), ca, "node-a.example.com")
	if err != nil {
		t.Fatalf("dialNode() error: %v", err)
	}
	defer conn.Close()

	select {
	case certname := <-got:
		if certname != "node-a.example.com" {
			t.Fatalf("observer got certname %q, want %q", certname, "node-a.example.com")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("observer was not notified within 5s of a node connecting")
	}
}

func TestOnNodeConnect_NotifiedOncePerConnection(t *testing.T) {
	got := make(chan string, 8)
	s, ca := startTestServerWithObserver(t, func(certname string) {
		got <- certname
	})

	// Connect, disconnect, connect again: the contract is a
	// notification per connection, explicitly not once per node.
	for range 2 {
		conn, err := dialNode(t, s.Addr(), ca, "node-b.example.com")
		if err != nil {
			t.Fatalf("dialNode() error: %v", err)
		}
		select {
		case certname := <-got:
			if certname != "node-b.example.com" {
				t.Fatalf("observer got certname %q, want %q", certname, "node-b.example.com")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("observer was not notified within 5s of a node connecting")
		}
		conn.Close()
	}
}

// TestOnNodeConnect_BlockingObserverDoesNotAffectTransport is the
// reason notification runs on its own goroutine: this observer never
// returns, and the transport must carry on regardless - accepting the
// connection, tracking it, and still dispatching to it.
func TestOnNodeConnect_BlockingObserverDoesNotAffectTransport(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	entered := make(chan struct{}, 4)
	s, ca := startTestServerWithObserver(t, func(string) {
		entered <- struct{}{}
		<-release // block for the whole test
	})

	nodeConn, err := dialNode(t, s.Addr(), ca, "node-a.example.com")
	if err != nil {
		t.Fatalf("dialNode() error: %v", err)
	}
	defer nodeConn.Close()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("observer was never called")
	}

	sub, err := nodeConn.Subscribe(DispatchSubject("node-a.example.com"), func(msg *nats.Msg) {
		_ = msg.Respond([]byte("ack: " + string(msg.Data)))
	})
	if err != nil {
		t.Fatalf("Subscribe() error: %v", err)
	}
	defer sub.Unsubscribe()
	nodeConn.Flush()

	resp, err := s.Dispatch(context.Background(), "node-a.example.com", []byte("run"), 5*time.Second)
	if err != nil {
		t.Fatalf("Dispatch() error while an observer was blocked: %v", err)
	}
	if got, want := string(resp), "ack: run"; got != want {
		t.Errorf("Dispatch() response = %q, want %q", got, want)
	}

	// A second node must still be observed and tracked - the stuck
	// observer must not have wedged the CONNECT subscription itself.
	otherConn, err := dialNode(t, s.Addr(), ca, "node-c.example.com")
	if err != nil {
		t.Fatalf("dialNode(node-c) error: %v", err)
	}
	defer otherConn.Close()

	deadline := time.Now().Add(5 * time.Second)
	for !s.Registry().Lookup("node-c.example.com") {
		if time.Now().After(deadline) {
			t.Fatal("node-c never appeared in the registry while an observer was blocked")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestOnNodeConnect_PanickingObserverDoesNotAffectTransport: an
// unrecovered panic on a NATS callback goroutine would take the process
// down, so notifyConnect recovers.
func TestOnNodeConnect_PanickingObserverDoesNotAffectTransport(t *testing.T) {
	var mu sync.Mutex
	var calls int

	s, ca := startTestServerWithObserver(t, func(certname string) {
		mu.Lock()
		calls++
		mu.Unlock()
		panic("observer blew up: " + certname)
	})

	nodeConn, err := dialNode(t, s.Addr(), ca, "node-a.example.com")
	if err != nil {
		t.Fatalf("dialNode() error: %v", err)
	}
	defer nodeConn.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		seen := calls
		mu.Unlock()
		if seen > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("panicking observer was never called")
		}
		time.Sleep(10 * time.Millisecond)
	}

	sub, err := nodeConn.Subscribe(DispatchSubject("node-a.example.com"), func(msg *nats.Msg) {
		_ = msg.Respond([]byte("ack: " + string(msg.Data)))
	})
	if err != nil {
		t.Fatalf("Subscribe() error: %v", err)
	}
	defer sub.Unsubscribe()
	nodeConn.Flush()

	resp, err := s.Dispatch(context.Background(), "node-a.example.com", []byte("run"), 5*time.Second)
	if err != nil {
		t.Fatalf("Dispatch() error after an observer panicked: %v", err)
	}
	if got, want := string(resp), "ack: run"; got != want {
		t.Errorf("Dispatch() response = %q, want %q", got, want)
	}
}

// A nil observer is the default for every other test and for any
// deployment that does not wire one up.
func TestOnNodeConnect_NilObserverIsSafe(t *testing.T) {
	s, ca := startTestServerWithObserver(t, nil)

	conn, err := dialNode(t, s.Addr(), ca, "node-a.example.com")
	if err != nil {
		t.Fatalf("dialNode() error: %v", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(5 * time.Second)
	for !s.Registry().Lookup("node-a.example.com") {
		if time.Now().After(deadline) {
			t.Fatal("node-a never appeared in the registry with a nil observer")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
