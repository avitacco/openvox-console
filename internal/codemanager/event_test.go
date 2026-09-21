package codemanager

import (
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/messaging"
)

func TestPublishDeployed_RealSubscriberReceivesEvent(t *testing.T) {
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	defer bus.Close()

	var mu sync.Mutex
	var received *DeployedEvent
	if _, err := SubscribeDeployed(bus, func(e DeployedEvent) {
		mu.Lock()
		defer mu.Unlock()
		received = &e
	}); err != nil {
		t.Fatalf("SubscribeDeployed() error: %v", err)
	}

	if err := PublishDeployed(bus, "team_a", "team_a_production", "abc123"); err != nil {
		t.Fatalf("PublishDeployed() error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := received
		mu.Unlock()
		if got != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if received == nil {
		t.Fatal("subscriber never received the published event")
	}
	if received.Environment != "team_a_production" {
		t.Errorf("Environment = %q, want team_a_production", received.Environment)
	}
	// The source rides on the wire alongside the environment: two
	// sources can carry the same branch name, so a subscriber deciding
	// what to fetch cannot tell them apart without it.
	if received.Source != "team_a" {
		t.Errorf("Source = %q, want team_a", received.Source)
	}
	if received.Ref != "abc123" {
		t.Errorf("Ref = %q, want abc123", received.Ref)
	}
	if received.DeployedAt.IsZero() {
		t.Error("DeployedAt is zero")
	}
}
