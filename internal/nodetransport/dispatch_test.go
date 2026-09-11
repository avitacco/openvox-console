package nodetransport

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestDispatch_RoundTripsWithAConnectedNodeAgent(t *testing.T) {
	s, ca := startTestServer(t)

	nodeConn, err := dialNode(t, s.Addr(), ca, "node-a.example.com")
	if err != nil {
		t.Fatalf("dial node-a: %v", err)
	}
	defer nodeConn.Close()

	// Simulate the node-agent side: subscribe to its own dispatch subject
	// and echo the payload back via Respond.
	sub, err := nodeConn.Subscribe(DispatchSubject("node-a.example.com"), func(msg *nats.Msg) {
		_ = msg.Respond([]byte("ack: " + string(msg.Data)))
	})
	if err != nil {
		t.Fatalf("node-a Subscribe() error: %v", err)
	}
	defer sub.Unsubscribe()
	nodeConn.Flush()

	resp, err := s.Dispatch(context.Background(), "node-a.example.com", []byte("run"), 2*time.Second)
	if err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}
	if got, want := string(resp), "ack: run"; got != want {
		t.Errorf("Dispatch() response = %q, want %q", got, want)
	}
}

func TestDispatch_ReturnsErrNodeNotConnectedImmediately(t *testing.T) {
	s, _ := startTestServer(t)

	start := time.Now()
	_, err := s.Dispatch(context.Background(), "no-such-node.example.com", []byte("run"), 10*time.Second)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrNodeNotConnected) {
		t.Fatalf("Dispatch() error = %v, want ErrNodeNotConnected", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("Dispatch() took %v to report ErrNodeNotConnected, want near-immediate (well under the 10s timeout)", elapsed)
	}
}
