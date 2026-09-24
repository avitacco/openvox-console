package nodetransport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/voxpupuli/enterprise-console/internal/messaging"
)

// ErrNodeNotConnected is returned by Dispatch when no node-agent is
// currently subscribed to receive dispatches for the given certname.
// This is backed directly by NATS's own "no responders" signal (see
// design.md's "Decisions") - not a separate pre-dispatch registry check -
// so it's returned immediately, not only after timeout elapses.
var ErrNodeNotConnected = errors.New("node not connected")

// Dispatcher sends requests to node-agents, wherever in the cluster they
// are connected.
//
// It needs only the bus, not a node listener: every routed instance
// carries messaging.AccountNodes, so an instance with no nodes of its
// own - a web instance serving the package inventory, say - dispatches
// to nodes connected to another. On an instance where no node is
// reachable at all, every dispatch reports ErrNodeNotConnected, which is
// exactly what it should.
type Dispatcher struct {
	conn *nats.Conn
}

// NewDispatcher opens the dispatcher's connection into bus's
// AccountNodes.
func NewDispatcher(bus *messaging.Bus) (*Dispatcher, error) {
	conn, err := bus.Connect(messaging.AccountNodes, "dispatch")
	if err != nil {
		return nil, err
	}
	return &Dispatcher{conn: conn}, nil
}

// Dispatch sends payload to certname's node-agent (via its dispatch
// subject - see subjects.go) and waits up to timeout for its response.
// Returns ErrNodeNotConnected immediately if no node-agent is currently
// connected and subscribed, rather than waiting out the full timeout.
func (d *Dispatcher) Dispatch(ctx context.Context, certname string, payload []byte, timeout time.Duration) ([]byte, error) {
	if timeout > ResponseWindow {
		return nil, fmt.Errorf("dispatch timeout %s exceeds the node response window %s", timeout, ResponseWindow)
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	msg, err := d.conn.RequestWithContext(reqCtx, DispatchSubject(certname), payload)
	switch {
	case err == nil:
		return msg.Data, nil
	case errors.Is(err, nats.ErrNoResponders):
		return nil, ErrNodeNotConnected
	case ctx.Err() != nil:
		// The caller gave up, which is not the node's timeout.
		return nil, ctx.Err()
	default:
		return nil, fmt.Errorf("wait for node-agent response: %w", err)
	}
}

// Close drains the dispatcher's connection, letting a response already
// arriving be delivered.
func (d *Dispatcher) Close() {
	messaging.DrainAndClose(d.conn)
}
