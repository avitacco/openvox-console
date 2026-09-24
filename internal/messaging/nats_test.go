package messaging

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestStart_HealthyAfterStart(t *testing.T) {
	bus, err := Start()
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer bus.Close()

	if err := bus.Check(context.Background()); err != nil {
		t.Errorf("Check() error = %v, want nil", err)
	}
}

func TestPublishSubscribe_InProcess(t *testing.T) {
	bus, err := Start()
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer bus.Close()

	received := make(chan []byte, 1)
	sub, err := bus.Subscribe("test.subject", func(msg *nats.Msg) {
		received <- msg.Data
	})
	if err != nil {
		t.Fatalf("Subscribe() error: %v", err)
	}
	defer sub.Unsubscribe()

	if err := bus.Publish("test.subject", []byte("hello")); err != nil {
		t.Fatalf("Publish() error: %v", err)
	}

	select {
	case data := <-received:
		if string(data) != "hello" {
			t.Errorf("received %q, want %q", data, "hello")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscriber to receive message")
	}
}

// Embedded, the server must leave the process's signals alone - see
// options. nats-server's own handler would os.Exit on SIGTERM, skipping
// the console's graceful shutdown.
func TestEmbeddedServerDoesNotHandleSignals(t *testing.T) {
	for _, cfg := range []Config{{}, {Nodes: nil}} {
		opts, err := options(cfg, &authenticator{})
		if err != nil {
			t.Fatalf("options: %v", err)
		}
		if !opts.NoSigs {
			t.Error("embedded server would install its own signal handlers")
		}
	}
}
