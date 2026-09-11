// Package messaging embeds a NATS server in-process and exposes a thin
// publish/subscribe helper for internal components to communicate over,
// without a separately deployed broker.
package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// Bus wraps an embedded, unclustered NATS server and a client connection to
// it, used for internal (in-process) publish/subscribe.
type Bus struct {
	server *server.Server
	conn   *nats.Conn
}

// Start boots an embedded NATS server (unclustered, no external listener
// required) and connects an internal client to it. It blocks until the
// server is ready or the timeout elapses.
func Start() (*Bus, error) {
	ns, err := server.NewServer(&server.Options{
		DontListen: true, // in-process only; no external NATS port
	})
	if err != nil {
		return nil, fmt.Errorf("create embedded NATS server: %w", err)
	}

	ns.Start()
	if !ns.ReadyForConnections(10 * time.Second) {
		return nil, fmt.Errorf("embedded NATS server did not become ready in time")
	}

	conn, err := nats.Connect("", nats.InProcessServer(ns))
	if err != nil {
		ns.Shutdown()
		return nil, fmt.Errorf("connect to embedded NATS server: %w", err)
	}

	return &Bus{server: ns, conn: conn}, nil
}

// Name identifies this checker in the health check endpoint.
func (b *Bus) Name() string { return "nats" }

// Check reports whether the embedded NATS server is running and the
// internal client connection to it is healthy.
func (b *Bus) Check(_ context.Context) error {
	if !b.server.Running() {
		return fmt.Errorf("embedded NATS server is not running")
	}
	if !b.conn.IsConnected() {
		return fmt.Errorf("internal NATS connection is not connected")
	}
	return nil
}

// Publish sends data to subject on the internal NATS bus.
func (b *Bus) Publish(subject string, data []byte) error {
	return b.conn.Publish(subject, data)
}

// Subscribe registers handler to be called for every message published to
// subject.
func (b *Bus) Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error) {
	return b.conn.Subscribe(subject, handler)
}

// Close drains the internal connection and shuts down the embedded server.
func (b *Bus) Close() {
	if b.conn != nil {
		b.conn.Close()
	}
	if b.server != nil {
		b.server.Shutdown()
		b.server.WaitForShutdown()
	}
}
