package activity

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEvent_JSONRoundTrip(t *testing.T) {
	want := Event{
		Category:   "classifier",
		Action:     "group.created",
		Actor:      "admin",
		Summary:    "created group web-servers",
		OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if got != want {
		t.Errorf("round-tripped Event = %+v, want %+v", got, want)
	}
}
