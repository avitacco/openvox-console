package activity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func TestListEvents_ReturnsRecordedEvents(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if err := store.RecordEvent(ctx, Event{
		Category: "activity-handlers-test", Action: "recorded", Actor: "a",
		Summary: "handler sanity check", OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}); err != nil {
		t.Fatalf("RecordEvent() error: %v", err)
	}

	h := NewHandlers(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-log", nil)
	rec := httptest.NewRecorder()
	h.listEvents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
}

func TestListEvents_FiltersByQueryParams(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if err := store.RecordEvent(ctx, Event{
		Category: "activity-handlers-test-filter", Action: "group.created", Actor: "activity-handlers-filter-actor",
		Summary: "target", OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}); err != nil {
		t.Fatalf("RecordEvent(target) error: %v", err)
	}
	if err := store.RecordEvent(ctx, Event{
		Category: "activity-handlers-test-filter", Action: "group.deleted", Actor: "someone-else",
		Summary: "other", OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}); err != nil {
		t.Fatalf("RecordEvent(other) error: %v", err)
	}

	h := NewHandlers(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-log?category=activity-handlers-test-filter&action=group.created&actor=activity-handlers-filter-actor", nil)
	rec := httptest.NewRecorder()
	h.listEvents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}

	var page struct {
		Items []Event `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	// Not an exact-length assertion: category/action/actor here are
	// fixed literals, so a prior run against this same shared dev
	// database (see testStore) can leave an earlier matching row behind
	// - see the identical reasoning in store_test.go's filter tests.
	found := false
	for _, e := range page.Items {
		if e.Category != "activity-handlers-test-filter" || e.Action != "group.created" || e.Actor != "activity-handlers-filter-actor" {
			t.Errorf("Items contains %+v, which doesn't match all three filters", e)
		}
		if e.Summary == "target" {
			found = true
		}
	}
	if !found {
		t.Error("combined category/action/actor filters excluded the matching event")
	}
}

func TestListCategories_ReturnsRecordedCategory(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if err := store.RecordEvent(ctx, Event{
		Category: "activity-handlers-test-categories", Action: "recorded", Actor: "a",
		Summary: "categories endpoint test", OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}); err != nil {
		t.Fatalf("RecordEvent() error: %v", err)
	}

	h := NewHandlers(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-log/categories", nil)
	rec := httptest.NewRecorder()
	h.listCategories(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var categories []string
	if err := json.Unmarshal(rec.Body.Bytes(), &categories); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !slices.Contains(categories, "activity-handlers-test-categories") {
		t.Errorf("categories = %v, want it to contain the just-recorded category", categories)
	}
}

func TestListActions_ReturnsRecordedAction(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if err := store.RecordEvent(ctx, Event{
		Category: "activity-handlers-test", Action: "activity-handlers-test-actions-endpoint", Actor: "a",
		Summary: "actions endpoint test", OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}); err != nil {
		t.Fatalf("RecordEvent() error: %v", err)
	}

	h := NewHandlers(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-log/actions", nil)
	rec := httptest.NewRecorder()
	h.listActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var actions []string
	if err := json.Unmarshal(rec.Body.Bytes(), &actions); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !slices.Contains(actions, "activity-handlers-test-actions-endpoint") {
		t.Errorf("actions = %v, want it to contain the just-recorded action", actions)
	}
}
