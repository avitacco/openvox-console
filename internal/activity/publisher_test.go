package activity

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

type fakeFailingBus struct{}

func (fakeFailingBus) Publish(string, []byte) error {
	return errors.New("bus is closed")
}

type recordingBus struct {
	subject string
	data    []byte
}

func (b *recordingBus) Publish(subject string, data []byte) error {
	b.subject = subject
	b.data = data
	return nil
}

func TestPublisher_PublishFailure_IsLoggedNotReturned(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	p := NewPublisher(fakeFailingBus{}, logger, "test-category")

	// Publish has no error return - this line alone proves the failure
	// isn't surfaced to the caller. If it panicked, the test would fail.
	p.Publish("test.action", "tester", "a summary")

	if !strings.Contains(logs.String(), "failed to publish activity event") {
		t.Errorf("expected a logged failure, got log output: %q", logs.String())
	}
}

func TestPublisher_Publish_SendsOnSharedSubject(t *testing.T) {
	bus := &recordingBus{}
	logger := slog.New(slog.NewTextHandler(new(bytes.Buffer), nil))
	p := NewPublisher(bus, logger, "test-category")

	p.Publish("test.action", "tester", "a summary")

	if bus.subject != Subject {
		t.Errorf("published subject = %q, want %q", bus.subject, Subject)
	}
	if !strings.Contains(string(bus.data), `"category":"test-category"`) {
		t.Errorf("published data missing category: %s", bus.data)
	}
	if !strings.Contains(string(bus.data), `"action":"test.action"`) {
		t.Errorf("published data missing action: %s", bus.data)
	}
}
