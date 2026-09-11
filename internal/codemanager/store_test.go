package codemanager

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testStore builds a Store against a real Postgres instance from
// CONSOLE_TEST_POSTGRES_DSN, skipping if it isn't set - mirrors every
// other package's testStore pattern in this project.
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

func TestCreateDeploy_AndListDeploys(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id, err := s.CreateDeploy(ctx, "codemanager-store-test-ref-1", "codemanager-store-test-actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	found, err := s.GetDeploy(ctx, id)
	if err != nil {
		t.Fatalf("GetDeploy() error: %v", err)
	}
	if found.Ref != "codemanager-store-test-ref-1" {
		t.Errorf("Ref = %q", found.Ref)
	}
	if found.Status != StatusRunning {
		t.Errorf("Status = %q, want %q", found.Status, StatusRunning)
	}
	if found.TriggeredBy != "codemanager-store-test-actor" {
		t.Errorf("TriggeredBy = %q", found.TriggeredBy)
	}
	if found.FinishedAt != nil {
		t.Errorf("FinishedAt = %v, want nil for a running deploy", found.FinishedAt)
	}

	deploys, total, err := s.ListDeploys(ctx, 1, 1000, DeployFilter{})
	if err != nil {
		t.Fatalf("ListDeploys() error: %v", err)
	}
	if total != len(deploys) {
		t.Errorf("total = %d, want %d (page large enough to hold everything)", total, len(deploys))
	}
	var listed bool
	for _, d := range deploys {
		if d.ID == id {
			listed = true
		}
	}
	if !listed {
		t.Error("created deploy not found in ListDeploys()")
	}
}

func TestCompleteDeploy_Succeeded(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id, err := s.CreateDeploy(ctx, "codemanager-store-test-ref-2", "codemanager-store-test-actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	if err := s.CompleteDeploy(ctx, id, StatusSucceeded, ""); err != nil {
		t.Fatalf("CompleteDeploy() error: %v", err)
	}

	found, err := s.GetDeploy(ctx, id)
	if err != nil {
		t.Fatalf("GetDeploy() error: %v", err)
	}
	if found.Status != StatusSucceeded {
		t.Errorf("Status = %q, want %q", found.Status, StatusSucceeded)
	}
	if found.FinishedAt == nil {
		t.Error("FinishedAt is nil after completion")
	}
	if found.ErrorDetail != "" {
		t.Errorf("ErrorDetail = %q, want empty for a successful deploy", found.ErrorDetail)
	}
}

func TestCompleteDeploy_FailedWithErrorDetail(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id, err := s.CreateDeploy(ctx, "codemanager-store-test-ref-3", "codemanager-store-test-actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	if err := s.CompleteDeploy(ctx, id, StatusFailed, "module resolution failed: boom"); err != nil {
		t.Fatalf("CompleteDeploy() error: %v", err)
	}

	found, err := s.GetDeploy(ctx, id)
	if err != nil {
		t.Fatalf("GetDeploy() error: %v", err)
	}
	if found.Status != StatusFailed {
		t.Errorf("Status = %q, want %q", found.Status, StatusFailed)
	}
	if found.ErrorDetail != "module resolution failed: boom" {
		t.Errorf("ErrorDetail = %q", found.ErrorDetail)
	}
}

func TestGetDeploy_NotFound(t *testing.T) {
	s := testStore(t)
	_, err := s.GetDeploy(context.Background(), -1)
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestCompleteDeploy_NotFound(t *testing.T) {
	s := testStore(t)
	err := s.CompleteDeploy(context.Background(), -1, StatusSucceeded, "")
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestListDeploys_MostRecentFirst(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	firstID, err := s.CreateDeploy(ctx, "codemanager-store-test-order-1", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	secondID, err := s.CreateDeploy(ctx, "codemanager-store-test-order-2", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	deploys, _, err := s.ListDeploys(ctx, 1, 1000, DeployFilter{})
	if err != nil {
		t.Fatalf("ListDeploys() error: %v", err)
	}

	var firstIdx, secondIdx = -1, -1
	for i, d := range deploys {
		if d.ID == firstID {
			firstIdx = i
		}
		if d.ID == secondID {
			secondIdx = i
		}
	}
	if firstIdx == -1 || secondIdx == -1 {
		t.Fatal("test deploys not found in ListDeploys()")
	}
	if secondIdx >= firstIdx {
		t.Errorf("more recently created deploy at index %d, older at %d - want more recent first", secondIdx, firstIdx)
	}
}

func TestListDeploys_Pagination(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	firstID, err := s.CreateDeploy(ctx, "codemanager-store-test-page-1", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	secondID, err := s.CreateDeploy(ctx, "codemanager-store-test-page-2", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	firstPage, total, err := s.ListDeploys(ctx, 1, 1, DeployFilter{})
	if err != nil {
		t.Fatalf("ListDeploys(page 1) error: %v", err)
	}
	if len(firstPage) != 1 {
		t.Fatalf("len(firstPage) = %d, want 1", len(firstPage))
	}
	if total < 2 {
		t.Errorf("total = %d, want at least 2", total)
	}
	// Most recent first, so page 1 should hold secondID.
	if firstPage[0].ID != secondID {
		t.Errorf("firstPage[0].ID = %d, want %d (most recently created)", firstPage[0].ID, secondID)
	}

	secondPage, _, err := s.ListDeploys(ctx, 2, 1, DeployFilter{})
	if err != nil {
		t.Fatalf("ListDeploys(page 2) error: %v", err)
	}
	if len(secondPage) != 1 {
		t.Fatalf("len(secondPage) = %d, want 1", len(secondPage))
	}
	if secondPage[0].ID != firstID {
		t.Errorf("secondPage[0].ID = %d, want %d (older deploy)", secondPage[0].ID, firstID)
	}
}

func TestListDeploys_FilterByRef(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	targetID, err := s.CreateDeploy(ctx, "codemanager-store-test-filter-ref-target", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	if _, err := s.CreateDeploy(ctx, "codemanager-store-test-filter-ref-other", "actor"); err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	// Doesn't assert an exact count: this ref is a fixed literal, so a
	// prior run against the same shared dev database (tests here don't
	// truncate between runs - see testStore) can leave an earlier row
	// with the same ref behind. Asserting "found, and everything found
	// actually matches" is safe against that; asserting total == 1 isn't
	// - see the same reasoning in TestListDeploys_FilterByStatus/
	// FilterByTriggeredBy below.
	deploys, _, err := s.ListDeploys(ctx, 1, 1000, DeployFilter{Ref: "codemanager-store-test-filter-ref-target"})
	if err != nil {
		t.Fatalf("ListDeploys() error: %v", err)
	}
	found := false
	for _, d := range deploys {
		if d.Ref != "codemanager-store-test-filter-ref-target" {
			t.Errorf("ListDeploys() with Ref filter returned deploy with ref %q", d.Ref)
		}
		if d.ID == targetID {
			found = true
		}
	}
	if !found {
		t.Error("ref filter excluded the matching deploy")
	}
}

func TestListDeploys_FilterByStatus(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	runningID, err := s.CreateDeploy(ctx, "codemanager-store-test-filter-status-running", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	succeededID, err := s.CreateDeploy(ctx, "codemanager-store-test-filter-status-succeeded", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	if err := s.CompleteDeploy(ctx, succeededID, StatusSucceeded, ""); err != nil {
		t.Fatalf("CompleteDeploy() error: %v", err)
	}

	deploys, _, err := s.ListDeploys(ctx, 1, 1000, DeployFilter{Status: StatusSucceeded})
	if err != nil {
		t.Fatalf("ListDeploys() error: %v", err)
	}
	var sawSucceeded, sawRunning bool
	for _, d := range deploys {
		if d.ID == succeededID {
			sawSucceeded = true
		}
		if d.ID == runningID {
			sawRunning = true
		}
	}
	if !sawSucceeded {
		t.Error("status=succeeded filter excluded the succeeded deploy")
	}
	if sawRunning {
		t.Error("status=succeeded filter included a still-running deploy")
	}
}

func TestListDeploys_FilterByTriggeredBy(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	targetID, err := s.CreateDeploy(ctx, "codemanager-store-test-filter-triggeredby", "codemanager-filter-test-actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	if _, err := s.CreateDeploy(ctx, "codemanager-store-test-filter-triggeredby-other", "someone-else"); err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	deploys, _, err := s.ListDeploys(ctx, 1, 1000, DeployFilter{TriggeredBy: "codemanager-filter-test-actor"})
	if err != nil {
		t.Fatalf("ListDeploys() error: %v", err)
	}
	found := false
	for _, d := range deploys {
		if d.TriggeredBy != "codemanager-filter-test-actor" {
			t.Errorf("ListDeploys() with TriggeredBy filter returned deploy triggered by %q", d.TriggeredBy)
		}
		if d.ID == targetID {
			found = true
		}
	}
	if !found {
		t.Error("triggeredBy filter excluded the matching deploy")
	}
}

func TestListDeploys_SortAsc(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Scoped to just these two deploys via a TriggeredBy unique to this
	// test, rather than relying on a page size of 1000 comfortably
	// covering the whole (shared, ever-growing - see testStore) deploys
	// table - see the identical fix (and why) in orchestrator's
	// TestListJobs_SortAsc.
	const triggeredBy = "codemanager-store-test-sortasc-actor"
	firstID, err := s.CreateDeploy(ctx, "codemanager-store-test-sortasc-1", triggeredBy)
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	secondID, err := s.CreateDeploy(ctx, "codemanager-store-test-sortasc-2", triggeredBy)
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	deploys, _, err := s.ListDeploys(ctx, 1, 1000, DeployFilter{TriggeredBy: triggeredBy, SortAsc: true})
	if err != nil {
		t.Fatalf("ListDeploys() error: %v", err)
	}

	var firstIdx, secondIdx = -1, -1
	for i, d := range deploys {
		if d.ID == firstID {
			firstIdx = i
		}
		if d.ID == secondID {
			secondIdx = i
		}
	}
	if firstIdx == -1 || secondIdx == -1 {
		t.Fatal("test deploys not found in ListDeploys()")
	}
	if firstIdx >= secondIdx {
		t.Errorf("SortAsc: older deploy at index %d, more recent at %d - want oldest first", firstIdx, secondIdx)
	}
}

func TestDistinctRefs_IncludesRecordedRef(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.CreateDeploy(ctx, "codemanager-store-test-distinct-ref", "actor"); err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	refs, err := s.DistinctRefs(ctx)
	if err != nil {
		t.Fatalf("DistinctRefs() error: %v", err)
	}
	if !slices.Contains(refs, "codemanager-store-test-distinct-ref") {
		t.Errorf("DistinctRefs() = %v, want it to contain the just-created ref", refs)
	}
	if !slices.IsSorted(refs) {
		t.Errorf("DistinctRefs() = %v, want alphabetically sorted", refs)
	}
}
