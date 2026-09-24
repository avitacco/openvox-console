package rbac

import (
	"context"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/messaging"
)

// fakeRevokedTokenStore is an in-memory revokedTokenStore, for testing
// the Revoker's in-memory/NATS behavior without a live Postgres.
type fakeRevokedTokenStore struct {
	revocations map[string]time.Time
}

func newFakeRevokedTokenStore() *fakeRevokedTokenStore {
	return &fakeRevokedTokenStore{revocations: map[string]time.Time{}}
}

func (f *fakeRevokedTokenStore) PersistRevocation(_ context.Context, jti string, expiresAt time.Time) error {
	f.revocations[jti] = expiresAt
	return nil
}

func (f *fakeRevokedTokenStore) UnexpiredRevocations(_ context.Context) ([]revocationEvent, error) {
	events := make([]revocationEvent, 0)
	for jti, expiresAt := range f.revocations {
		if expiresAt.After(time.Now()) {
			events = append(events, revocationEvent{JTI: jti, ExpiresAt: expiresAt})
		}
	}
	return events, nil
}

func TestRevoker_RevokeIsImmediatelyVisibleLocally(t *testing.T) {
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	defer bus.Close()

	db := newFakeRevokedTokenStore()
	r := NewRevoker(bus, db)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if r.IsRevoked("abc") {
		t.Fatal("expected 'abc' not revoked before Revoke() is called")
	}

	if err := r.Revoke(context.Background(), "abc", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Revoke() error: %v", err)
	}

	if !r.IsRevoked("abc") {
		t.Error("expected 'abc' revoked immediately after Revoke() returns")
	}
	if _, ok := db.revocations["abc"]; !ok {
		t.Error("expected revocation persisted to the store")
	}
}

func TestRevoker_LenTracksRevocationCount(t *testing.T) {
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	defer bus.Close()

	db := newFakeRevokedTokenStore()
	r := NewRevoker(bus, db)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if got := r.Len(); got != 0 {
		t.Fatalf("Len() = %d, want 0 before any revocation", got)
	}

	if err := r.Revoke(context.Background(), "abc", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Revoke() error: %v", err)
	}
	if err := r.Revoke(context.Background(), "def", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Revoke() error: %v", err)
	}

	if got := r.Len(); got != 2 {
		t.Errorf("Len() = %d, want 2 after two revocations", got)
	}
}

func TestRevoker_PropagatesToASecondIndependentSubscriber(t *testing.T) {
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	defer bus.Close()

	// Two independent Revoker instances (each its own in-memory set,
	// each its own fake store) sharing the same NATS bus - simulating
	// two running console instances that only coordinate over NATS.
	revokerA := NewRevoker(bus, newFakeRevokedTokenStore())
	revokerB := NewRevoker(bus, newFakeRevokedTokenStore())
	if err := revokerA.Start(context.Background()); err != nil {
		t.Fatalf("revokerA.Start() error: %v", err)
	}
	if err := revokerB.Start(context.Background()); err != nil {
		t.Fatalf("revokerB.Start() error: %v", err)
	}

	if err := revokerA.Revoke(context.Background(), "xyz", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("revokerA.Revoke() error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if revokerB.IsRevoked("xyz") {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("revokerB did not observe revokerA's revocation within 2s")
}

func TestRevoker_LoadsUnexpiredRevocationsAtStartup(t *testing.T) {
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	defer bus.Close()

	db := newFakeRevokedTokenStore()
	db.revocations["still-valid"] = time.Now().Add(time.Hour)
	db.revocations["already-expired"] = time.Now().Add(-time.Hour)

	r := NewRevoker(bus, db)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if !r.IsRevoked("still-valid") {
		t.Error("expected still-unexpired revocation loaded into memory at startup")
	}
	if r.IsRevoked("already-expired") {
		t.Error("expected already-expired revocation NOT loaded into memory at startup")
	}
}

// A revocation this instance never heard about on the bus - persisted by
// another instance while a route was down - is picked up from Postgres.
func TestRevoker_ResyncPicksUpRevocationMissedOnBus(t *testing.T) {
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	defer bus.Close()

	db := newFakeRevokedTokenStore()
	r := NewRevoker(bus, db)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Written straight to the store: no event is published.
	db.revocations["missed"] = time.Now().Add(time.Hour)
	if r.IsRevoked("missed") {
		t.Fatal("test setup: the revocation should not be known before a resync")
	}

	if err := r.resync(context.Background()); err != nil {
		t.Fatalf("resync() error: %v", err)
	}
	if !r.IsRevoked("missed") {
		t.Error("a revocation persisted without a bus event was not picked up by resync")
	}
}

func TestRevoker_ResyncPrunesExpiredEntries(t *testing.T) {
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	defer bus.Close()

	r := NewRevoker(bus, newFakeRevokedTokenStore())
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	r.mu.Lock()
	r.revoked["expired"] = time.Now().Add(-time.Minute)
	r.mu.Unlock()

	if err := r.resync(context.Background()); err != nil {
		t.Fatalf("resync() error: %v", err)
	}
	if r.Len() != 0 {
		t.Errorf("Len() = %d after resync, want the expired entry pruned", r.Len())
	}
}
