package nodetransport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

// ErrNodeNotConnected is returned by Dispatch when no node-agent is
// currently subscribed to receive dispatches for the given certname.
// This is backed directly by NATS's own "no responders" signal (see
// design.md's "Decisions") - not a separate pre-dispatch registry check -
// so it's returned immediately, not only after timeout elapses.
var ErrNodeNotConnected = errors.New("node not connected")

// Dispatch sends payload to certname's node-agent (via its dispatch
// subject - see subjects.go) and waits up to timeout for its response.
// Returns ErrNodeNotConnected immediately if no node-agent is currently
// connected and subscribed, rather than waiting out the full timeout.
func (s *Server) Dispatch(ctx context.Context, certname string, payload []byte, timeout time.Duration) ([]byte, error) {
	requestID := uuid.NewString()
	reply := ReplySubject(certname, requestID)

	sub, err := s.conn.SubscribeSync(reply)
	if err != nil {
		return nil, fmt.Errorf("subscribe to reply subject: %w", err)
	}
	defer sub.Unsubscribe()

	if err := s.conn.PublishRequest(DispatchSubject(certname), reply, payload); err != nil {
		return nil, fmt.Errorf("publish dispatch: %w", err)
	}

	type result struct {
		msg *nats.Msg
		err error
	}
	done := make(chan result, 1)
	go func() {
		msg, err := sub.NextMsg(timeout)
		done <- result{msg, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-done:
		if errors.Is(r.err, nats.ErrNoResponders) {
			return nil, ErrNodeNotConnected
		}
		if r.err != nil {
			return nil, fmt.Errorf("wait for node-agent response: %w", r.err)
		}
		return r.msg.Data, nil
	}
}
