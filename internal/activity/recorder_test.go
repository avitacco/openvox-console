package activity

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/messaging"
)

// fakeRecorderStore records what was persisted, so the test can assert on
// it without a live Postgres.
type fakeRecorderStore struct {
	mu     sync.Mutex
	events []Event
}

func (s *fakeRecorderStore) RecordEvent(_ context.Context, e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

func (s *fakeRecorderStore) all() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}

func TestRecorder_PersistsPublishedEvent(t *testing.T) {
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	defer bus.Close()

	store := &fakeRecorderStore{}
	rec := NewRecorder(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := rec.Start(bus); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	want := Event{
		Category:   "activity-recorder-test",
		Action:     "test.published",
		Actor:      "tester",
		Summary:    "a published event",
		OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	if err := bus.Publish(Subject, data); err != nil {
		t.Fatalf("Publish() error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(store.all()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	got := store.all()
	if len(got) != 1 {
		t.Fatalf("store received %d events, want 1", len(got))
	}
	if got[0] != want {
		t.Errorf("persisted event = %+v, want %+v", got[0], want)
	}
}
