package codemanager

import (
	"context"
	"slices"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

// testStore builds a Store against a real Postgres instance from
// CONSOLE_TEST_POSTGRES_DSN, skipping if it isn't set - mirrors every
// other package's testStore pattern in this project.
func testStore(t *testing.T) *Store {
	t.Helper()

	pool := testdb.Pool(t)

	return NewStore(pool)
}

func TestCreateDeploy_AndListDeploys(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-ref-1", "codemanager-store-test-actor")
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

	id, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-ref-2", "codemanager-store-test-actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	if err := s.CompleteDeploy(ctx, id, StatusSucceeded, "", nil, nil); err != nil {
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

	id, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-ref-3", "codemanager-store-test-actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	if err := s.CompleteDeploy(ctx, id, StatusFailed, "module resolution failed: boom", nil, nil); err != nil {
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
	err := s.CompleteDeploy(context.Background(), -1, StatusSucceeded, "", nil, nil)
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestListDeploys_MostRecentFirst(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	firstID, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-order-1", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	secondID, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-order-2", "actor")
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

	firstID, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-page-1", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	secondID, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-page-2", "actor")
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

	targetID, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-filter-ref-target", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	if _, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-filter-ref-other", "actor"); err != nil {
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

	runningID, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-filter-status-running", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	succeededID, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-filter-status-succeeded", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	if err := s.CompleteDeploy(ctx, succeededID, StatusSucceeded, "", nil, nil); err != nil {
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

	targetID, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-filter-triggeredby", "codemanager-filter-test-actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	if _, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-filter-triggeredby-other", "someone-else"); err != nil {
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
	firstID, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-sortasc-1", triggeredBy)
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	secondID, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-sortasc-2", triggeredBy)
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

	if _, err := s.CreateDeploy(ctx, DefaultSourceName, "codemanager-store-test-distinct-ref", "actor"); err != nil {
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

func TestCreateDeploy_RoundTripsSource(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id, err := s.CreateDeploy(ctx, "codemanager_store_test_source_a", "codemanager-store-test-source-ref", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	found, err := s.GetDeploy(ctx, id)
	if err != nil {
		t.Fatalf("GetDeploy() error: %v", err)
	}
	if found.Source != "codemanager_store_test_source_a" {
		t.Errorf("Source = %q, want the source it was created with", found.Source)
	}

	deploys, _, err := s.ListDeploys(ctx, 1, 1000, DeployFilter{Source: "codemanager_store_test_source_a"})
	if err != nil {
		t.Fatalf("ListDeploys() error: %v", err)
	}
	var listed bool
	for _, d := range deploys {
		if d.Source != "codemanager_store_test_source_a" {
			t.Errorf("source filter returned a deploy from %q", d.Source)
		}
		if d.ID == id {
			listed = true
		}
	}
	if !listed {
		t.Error("source filter excluded the matching deploy")
	}
}

func TestListDeploys_SameRefFromDifferentSourcesAreDistinguishable(t *testing.T) {
	// The reason ref alone stopped being enough: two control repos can
	// each have a production branch, and their attempts must not read
	// as repeated deploys of one thing.
	s := testStore(t)
	ctx := context.Background()

	const ref = "codemanager-store-test-shared-ref"
	aID, err := s.CreateDeploy(ctx, "codemanager_store_test_src_one", ref, "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	bID, err := s.CreateDeploy(ctx, "codemanager_store_test_src_two", ref, "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	byRef, _, err := s.ListDeploys(ctx, 1, 1000, DeployFilter{Ref: ref})
	if err != nil {
		t.Fatalf("ListDeploys() error: %v", err)
	}
	seen := map[int64]string{}
	for _, d := range byRef {
		seen[d.ID] = d.Source
	}
	if seen[aID] != "codemanager_store_test_src_one" || seen[bID] != "codemanager_store_test_src_two" {
		t.Errorf("attempts sharing a ref did not keep their own sources: %v", seen)
	}

	// Filtering by source narrows a shared ref to one source's attempts.
	oneOnly, _, err := s.ListDeploys(ctx, 1, 1000, DeployFilter{Ref: ref, Source: "codemanager_store_test_src_one"})
	if err != nil {
		t.Fatalf("ListDeploys() error: %v", err)
	}
	for _, d := range oneOnly {
		if d.ID == bID {
			t.Error("source filter returned the other source's attempt for a shared ref")
		}
	}
}

func TestDistinctSources_ReturnsRecordedSource(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.CreateDeploy(ctx, "codemanager_store_test_distinct_src", "codemanager-store-test-distinct-source-ref", "actor"); err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	sources, err := s.DistinctSources(ctx)
	if err != nil {
		t.Fatalf("DistinctSources() error: %v", err)
	}
	if !slices.Contains(sources, "codemanager_store_test_distinct_src") {
		t.Errorf("sources = %v, want it to contain the just-created source", sources)
	}
}

func TestCompleteDeploy_RecordsEnvironmentAndSize(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id, err := s.CreateDeploy(ctx, "codemanager_store_test_sized", "codemanager-store-test-sized-ref", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	// A running attempt has neither yet.
	running, err := s.GetDeploy(ctx, id)
	if err != nil {
		t.Fatalf("GetDeploy() error: %v", err)
	}
	if running.Environment != nil || running.SizeBytes != nil {
		t.Errorf("a running deploy already carries environment=%v size=%v", running.Environment, running.SizeBytes)
	}

	env, size := "codemanager_store_test_sized_production", int64(4096)
	if err := s.CompleteDeploy(ctx, id, StatusSucceeded, "", &env, &size); err != nil {
		t.Fatalf("CompleteDeploy() error: %v", err)
	}

	done, err := s.GetDeploy(ctx, id)
	if err != nil {
		t.Fatalf("GetDeploy() error: %v", err)
	}
	if done.Environment == nil || *done.Environment != env {
		t.Errorf("Environment = %v, want %q", done.Environment, env)
	}
	if done.SizeBytes == nil || *done.SizeBytes != size {
		t.Errorf("SizeBytes = %v, want %d", done.SizeBytes, size)
	}
}

func TestCompleteDeploy_AbsentSizeStaysAbsentRatherThanZero(t *testing.T) {
	// The distinction the nullable column exists for: a failed deploy
	// produced no tree, which must not read as an empty repository.
	s := testStore(t)
	ctx := context.Background()

	id, err := s.CreateDeploy(ctx, "codemanager_store_test_unsized", "codemanager-store-test-unsized-ref", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	if err := s.CompleteDeploy(ctx, id, StatusFailed, "boom", nil, nil); err != nil {
		t.Fatalf("CompleteDeploy() error: %v", err)
	}

	found, err := s.GetDeploy(ctx, id)
	if err != nil {
		t.Fatalf("GetDeploy() error: %v", err)
	}
	if found.SizeBytes != nil {
		t.Errorf("SizeBytes = %v, want nil (absent, not zero)", *found.SizeBytes)
	}
	if found.Environment != nil {
		t.Errorf("Environment = %v, want nil", *found.Environment)
	}

	// And through the list path, not just the single-row lookup.
	deploys, _, err := s.ListDeploys(ctx, 1, 1000, DeployFilter{Source: "codemanager_store_test_unsized"})
	if err != nil {
		t.Fatalf("ListDeploys() error: %v", err)
	}
	for _, d := range deploys {
		if d.ID == id && d.SizeBytes != nil {
			t.Errorf("listed SizeBytes = %v, want nil", *d.SizeBytes)
		}
	}
}

func TestDeploySummaryBySource(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	const (
		multi  = "codemanager_summary_multi"
		failed = "codemanager_summary_failed"
	)

	// A source with two environments, the later one carrying a size.
	prodSize := int64(1024)
	prodID, err := s.CreateDeploy(ctx, multi, "abc", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	if err := s.CompleteDeploy(ctx, prodID, StatusSucceeded, "", strptr(multi+"_production"), &prodSize); err != nil {
		t.Fatalf("CompleteDeploy() error: %v", err)
	}
	stagingSize := int64(2048)
	stagingID, err := s.CreateDeploy(ctx, multi, "def", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	if err := s.CompleteDeploy(ctx, stagingID, StatusSucceeded, "", strptr(multi+"_staging"), &stagingSize); err != nil {
		t.Fatalf("CompleteDeploy() error: %v", err)
	}

	// A source whose only attempt failed: it has deployed nothing.
	failedID, err := s.CreateDeploy(ctx, failed, "ghi", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	if err := s.CompleteDeploy(ctx, failedID, StatusFailed, "boom", nil, nil); err != nil {
		t.Fatalf("CompleteDeploy() error: %v", err)
	}

	summaries, err := s.DeploySummaryBySource(ctx)
	if err != nil {
		t.Fatalf("DeploySummaryBySource() error: %v", err)
	}

	got, ok := summaries[multi]
	if !ok {
		t.Fatalf("no summary for %q", multi)
	}
	if len(got.Environments) != 2 {
		t.Errorf("Environments = %v, want both deployed environments", got.Environments)
	}
	if got.LastDeployAt == nil {
		t.Error("LastDeployAt is nil for a source that deployed successfully")
	}
	// Size comes from the most recent success, not a sum across
	// environments - summing would double count hardlinked modules and
	// grow with branch count rather than with the repository.
	if got.SizeBytes == nil || *got.SizeBytes != stagingSize {
		t.Errorf("SizeBytes = %v, want %d (the most recent success)", got.SizeBytes, stagingSize)
	}

	if _, ok := summaries[failed]; ok {
		t.Errorf("a source whose only deploy failed appears as having deployed: %+v", summaries[failed])
	}

	// A configured source that has never deployed is simply absent, and
	// the caller reports it as absent rather than zero.
	if _, ok := summaries["codemanager_summary_never_deployed"]; ok {
		t.Error("a source with no deploys at all appears in the summary")
	}
}
