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
