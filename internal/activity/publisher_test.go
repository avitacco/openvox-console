package activity

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

type failingStore struct{}

func (failingStore) RecordEvent(context.Context, Event) error {
	return errors.New("database is down")
}

type recordingStore struct {
	mu     sync.Mutex
	events []Event
}

func (s *recordingStore) RecordEvent(_ context.Context, e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

func TestPublisher_RecordFailure_IsLoggedNotReturned(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	p := NewPublisher(failingStore{}, logger, "test-category")

	// Publish has no error return - this line alone proves the failure
	// isn't surfaced to the caller. If it panicked, the test would fail.
	p.Publish("test.action", "tester", "a summary")

	if !strings.Contains(logs.String(), "failed to record activity event") {
		t.Errorf("expected a logged failure, got log output: %q", logs.String())
	}
}

func TestPublisher_Publish_RecordsExactlyOneEvent(t *testing.T) {
	store := &recordingStore{}
	p := NewPublisher(store, slog.New(slog.NewTextHandler(new(bytes.Buffer), nil)), "test-category")

	p.Publish("test.action", "tester", "a summary")

	if len(store.events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(store.events))
	}
	got := store.events[0]
	if got.Category != "test-category" || got.Action != "test.action" || got.Actor != "tester" || got.Summary != "a summary" {
		t.Errorf("recorded event = %+v", got)
	}
	if got.OccurredAt.IsZero() {
		t.Error("recorded event has no timestamp")
	}
}
