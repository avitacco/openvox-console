package activity

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testStore builds a Store against a real Postgres instance from
// CONSOLE_TEST_POSTGRES_DSN, skipping if it isn't set - mirrors
// internal/classifier's testStore pattern.
func testStore(t *testing.T) *Store {
	t.Helper()

	dsn := os.Getenv("CONSOLE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONSOLE_TEST_POSTGRES_DSN not set; skipping integration test")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error: %v", err)
	}
	t.Cleanup(pool.Close)

	return NewStore(pool)
}

func TestRecordEvent_AndListEvents(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	e := Event{
		Category:   "activity-store-test",
		Action:     "test.recorded",
		Actor:      "activity-store-test-actor",
		Summary:    "a test event",
		OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.RecordEvent(ctx, e); err != nil {
		t.Fatalf("RecordEvent() error: %v", err)
	}

	events, _, err := s.ListEvents(ctx, 1, 1000, EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents() error: %v", err)
	}

	var found bool
	for _, got := range events {
		if got.Category == e.Category && got.Actor == e.Actor {
			found = true
			if got.Action != e.Action || got.Summary != e.Summary {
				t.Errorf("recorded event = %+v, want action/summary matching %+v", got, e)
			}
		}
	}
	if !found {
		t.Fatal("recorded event not found in ListEvents()")
	}
}

func TestListEvents_MostRecentFirst(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	older := Event{
		Category: "activity-store-test-order", Action: "first", Actor: "a",
		Summary: "older", OccurredAt: time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond),
	}
	newer := Event{
		Category: "activity-store-test-order", Action: "second", Actor: "a",
		Summary: "newer", OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.RecordEvent(ctx, older); err != nil {
		t.Fatalf("RecordEvent(older) error: %v", err)
	}
	if err := s.RecordEvent(ctx, newer); err != nil {
		t.Fatalf("RecordEvent(newer) error: %v", err)
	}

	events, _, err := s.ListEvents(ctx, 1, 1000, EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents() error: %v", err)
	}

	var newerIdx, olderIdx = -1, -1
	for i, e := range events {
		if e.Category == "activity-store-test-order" && e.Action == "second" {
			newerIdx = i
		}
		if e.Category == "activity-store-test-order" && e.Action == "first" {
			olderIdx = i
		}
	}
	if newerIdx == -1 || olderIdx == -1 {
		t.Fatal("test events not found in ListEvents()")
	}
	if newerIdx >= olderIdx {
		t.Errorf("newer event at index %d, older at %d - want newer to come first", newerIdx, olderIdx)
	}
}

func TestListEvents_Pagination(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		e := Event{
			Category: "activity-store-test-page", Action: "recorded", Actor: "a",
			Summary: "page test event", OccurredAt: time.Now().UTC().Add(time.Duration(i) * time.Second).Truncate(time.Millisecond),
		}
		if err := s.RecordEvent(ctx, e); err != nil {
			t.Fatalf("RecordEvent() error: %v", err)
		}
	}

	firstPage, total, err := s.ListEvents(ctx, 1, 1, EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents(page 1) error: %v", err)
	}
	if len(firstPage) != 1 {
		t.Fatalf("len(firstPage) = %d, want 1", len(firstPage))
	}
	if total < 3 {
		t.Errorf("total = %d, want at least 3", total)
	}

	secondPage, _, err := s.ListEvents(ctx, 2, 1, EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents(page 2) error: %v", err)
	}
	if len(secondPage) != 1 {
		t.Fatalf("len(secondPage) = %d, want 1", len(secondPage))
	}
	if firstPage[0].Summary == secondPage[0].Summary && firstPage[0].OccurredAt.Equal(secondPage[0].OccurredAt) {
		t.Error("page 1 and page 2 returned the same event")
	}
}

// Filter tests below check "found, and everything returned actually
// matches" rather than an exact count/length - these are fixed literal
// values, so a prior run against this same shared, non-truncated dev
// database (see testStore) can leave an earlier matching row behind.
// Asserting an exact total is only safe when the value under test is
// unique per run (e.g. TestListEvents_Pagination above generates its
// own isolated group of events and only compares within that group).

func TestListEvents_FilterByCategory(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	target := Event{
		Category: "activity-store-test-filter-category", Action: "recorded", Actor: "a",
		Summary: "target", OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	other := Event{
		Category: "activity-store-test-filter-category-other", Action: "recorded", Actor: "a",
		Summary: "other", OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.RecordEvent(ctx, target); err != nil {
		t.Fatalf("RecordEvent(target) error: %v", err)
	}
	if err := s.RecordEvent(ctx, other); err != nil {
		t.Fatalf("RecordEvent(other) error: %v", err)
	}

	events, _, err := s.ListEvents(ctx, 1, 1000, EventFilter{Category: "activity-store-test-filter-category"})
	if err != nil {
		t.Fatalf("ListEvents() error: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Category != "activity-store-test-filter-category" {
			t.Errorf("ListEvents() with Category filter returned event with category %q", e.Category)
		}
		if e.Summary == "target" {
			found = true
		}
	}
	if !found {
		t.Error("category filter excluded the matching event")
	}
}

func TestListEvents_FilterByAction(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	target := Event{
		Category: "activity-store-test-filter-action", Action: "group.created", Actor: "a",
		Summary: "target", OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	other := Event{
		Category: "activity-store-test-filter-action", Action: "group.deleted", Actor: "a",
		Summary: "other", OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.RecordEvent(ctx, target); err != nil {
		t.Fatalf("RecordEvent(target) error: %v", err)
	}
	if err := s.RecordEvent(ctx, other); err != nil {
		t.Fatalf("RecordEvent(other) error: %v", err)
	}

	events, _, err := s.ListEvents(ctx, 1, 1000, EventFilter{Category: "activity-store-test-filter-action", Action: "group.created"})
	if err != nil {
		t.Fatalf("ListEvents() error: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Action != "group.created" {
			t.Errorf("ListEvents() with Action filter returned event with action %q", e.Action)
		}
		if e.Summary == "target" {
			found = true
		}
	}
	if !found {
		t.Error("action filter excluded the matching event")
	}
}

func TestListEvents_FilterByActor(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	target := Event{
		Category: "activity-store-test-filter-actor", Action: "recorded", Actor: "activity-filter-test-actor",
		Summary: "target", OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	other := Event{
		Category: "activity-store-test-filter-actor", Action: "recorded", Actor: "someone-else",
		Summary: "other", OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.RecordEvent(ctx, target); err != nil {
		t.Fatalf("RecordEvent(target) error: %v", err)
	}
	if err := s.RecordEvent(ctx, other); err != nil {
		t.Fatalf("RecordEvent(other) error: %v", err)
	}

	events, _, err := s.ListEvents(ctx, 1, 1000, EventFilter{Actor: "activity-filter-test-actor"})
	if err != nil {
		t.Fatalf("ListEvents() error: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Actor != "activity-filter-test-actor" {
			t.Errorf("ListEvents() with Actor filter returned event with actor %q", e.Actor)
		}
		if e.Summary == "target" {
			found = true
		}
	}
	if !found {
		t.Error("actor filter excluded the matching event")
	}
}

func TestListEvents_SortAsc(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	older := Event{
		Category: "activity-store-test-sortasc", Action: "first", Actor: "a",
		Summary: "older", OccurredAt: time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond),
	}
	newer := Event{
		Category: "activity-store-test-sortasc", Action: "second", Actor: "a",
		Summary: "newer", OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.RecordEvent(ctx, older); err != nil {
		t.Fatalf("RecordEvent(older) error: %v", err)
	}
	if err := s.RecordEvent(ctx, newer); err != nil {
		t.Fatalf("RecordEvent(newer) error: %v", err)
	}

	// Scoped to just these two events via their shared Category, rather
	// than relying on a page size of 1000 comfortably covering the whole
	// (shared, ever-growing - see testStore) activity_log table - see
	// the identical fix (and why) in orchestrator's TestListJobs_SortAsc.
	events, _, err := s.ListEvents(ctx, 1, 1000, EventFilter{Category: "activity-store-test-sortasc", SortAsc: true})
	if err != nil {
		t.Fatalf("ListEvents() error: %v", err)
	}

	var olderIdx, newerIdx = -1, -1
	for i, e := range events {
		if e.Category == "activity-store-test-sortasc" && e.Action == "first" {
			olderIdx = i
		}
		if e.Category == "activity-store-test-sortasc" && e.Action == "second" {
			newerIdx = i
		}
	}
	if olderIdx == -1 || newerIdx == -1 {
		t.Fatal("test events not found in ListEvents()")
	}
	if olderIdx >= newerIdx {
		t.Errorf("SortAsc: newer event at index %d, older at %d - want oldest first", newerIdx, olderIdx)
	}
}

func TestDistinctCategories_IncludesRecordedCategory(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.RecordEvent(ctx, Event{
		Category: "activity-store-test-distinct-category", Action: "recorded", Actor: "a",
		Summary: "distinct category test", OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}); err != nil {
		t.Fatalf("RecordEvent() error: %v", err)
	}

	categories, err := s.DistinctCategories(ctx)
	if err != nil {
		t.Fatalf("DistinctCategories() error: %v", err)
	}
	if !slices.Contains(categories, "activity-store-test-distinct-category") {
		t.Errorf("DistinctCategories() = %v, want it to contain the just-recorded category", categories)
	}
	if !slices.IsSorted(categories) {
		t.Errorf("DistinctCategories() = %v, want alphabetically sorted", categories)
	}
}

func TestDistinctActions_IncludesRecordedAction(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.RecordEvent(ctx, Event{
		Category: "activity-store-test", Action: "activity-store-test-distinct-action", Actor: "a",
		Summary: "distinct action test", OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
	}); err != nil {
		t.Fatalf("RecordEvent() error: %v", err)
	}

	actions, err := s.DistinctActions(ctx)
	if err != nil {
		t.Fatalf("DistinctActions() error: %v", err)
	}
	if !slices.Contains(actions, "activity-store-test-distinct-action") {
		t.Errorf("DistinctActions() = %v, want it to contain the just-recorded action", actions)
	}
	if !slices.IsSorted(actions) {
		t.Errorf("DistinctActions() = %v, want alphabetically sorted", actions)
	}
}
