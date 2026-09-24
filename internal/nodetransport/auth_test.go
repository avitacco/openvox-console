package nodetransport

import (
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// TestAuth_NodeCannotAccessAnotherNodesSubject proves the core isolation
// property auth.go's Permissions exist for: a node authenticated as one
// certname cannot subscribe to, or successfully publish to, a subject
// scoped to a different certname - see subjects.go and
// specs/node-transport/spec.md's "Per-node subject isolation" requirement.
func TestAuth_NodeCannotAccessAnotherNodesSubject(t *testing.T) {
	s, ca := startTestServer(t)

	nodeA, err := dialNode(t, s.Addr(), ca, "node-a.example.com")
	if err != nil {
		t.Fatalf("dial node-a: %v", err)
	}
	defer nodeA.Close()

	nodeB, err := dialNode(t, s.Addr(), ca, "node-b.example.com")
	if err != nil {
		t.Fatalf("dial node-b: %v", err)
	}
	defer nodeB.Close()

	var mu sync.Mutex
	var permissionViolations int
	nodeA.SetErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
		if err != nil {
			mu.Lock()
			permissionViolations++
			mu.Unlock()
		}
	})

	// node-a attempts to subscribe to node-b's dispatch subject - denied
	// by node-a's Permissions (scoped to node.node-a.example.com.>).
	received := make(chan []byte, 1)
	sub, err := nodeA.Subscribe(DispatchSubject("node-b.example.com"), func(msg *nats.Msg) {
		received <- msg.Data
	})
	if err != nil {
		t.Fatalf("Subscribe() call itself should not error (the server denies the interest server-side): %v", err)
	}
	defer sub.Unsubscribe()

	// Something is published on node-b's subject. Nodes publish nothing
	// but replies, so it comes from the console's side, as a dispatch
	// would.
	if err := s.Dispatcher.conn.Publish(DispatchSubject("node-b.example.com"), []byte("should not reach node-a")); err != nil {
		t.Fatalf("publish on node-b's subject: %v", err)
	}
	_ = s.Dispatcher.conn.Flush()

	select {
	case <-received:
		t.Fatal("node-a received a message on node-b's subject - subject isolation is broken")
	case <-time.After(300 * time.Millisecond):
		// Expected: node-a's denied subscription never delivers.
	}

	// node-a attempts to publish onto node-b's subject too.
	_ = nodeA.Publish(DispatchSubject("node-b.example.com"), []byte("spoofed"))
	nodeA.Flush()
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if permissionViolations == 0 {
		t.Error("expected at least one permissions-violation error observed by node-a's error handler")
	}
}
