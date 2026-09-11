package orchestrator

import (
	"context"
	"os"
	"slices"
	"strings"
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

func TestCreateJob_AndGetJob(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	job, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"web01.example.com", "db01.example.com"}, "orchestrator-store-test-actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}

	if job.ID == 0 {
		t.Fatal("CreateJob() did not assign an ID")
	}
	if job.Kind != JobKindRun {
		t.Errorf("Kind = %q, want %q", job.Kind, JobKindRun)
	}
	if job.Status != StatusRunning {
		t.Errorf("Status = %q, want %q", job.Status, StatusRunning)
	}
	if job.TriggeredBy != "orchestrator-store-test-actor" {
		t.Errorf("TriggeredBy = %q", job.TriggeredBy)
	}
	if len(job.Targets) != 2 {
		t.Fatalf("len(Targets) = %d, want 2", len(job.Targets))
	}
	for _, target := range job.Targets {
		if target.Status != StatusPending {
			t.Errorf("target %q Status = %q, want %q", target.Certname, target.Status, StatusPending)
		}
	}

	got, err := s.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error: %v", err)
	}
	if len(got.Targets) != 2 {
		t.Fatalf("GetJob() len(Targets) = %d, want 2", len(got.Targets))
	}
}

func TestCreateJob_NoTargetsIsError(t *testing.T) {
	s := testStore(t)
	_, err := s.CreateJob(context.Background(), JobKindRun, "", "", nil, nil, "actor")
	if err == nil {
		t.Fatal("CreateJob() with no targets: expected an error, got nil")
	}
}

func TestRecordTargetResult_UpdatesTargetStatus(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	job, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"web01.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}

	exitCode := 2
	if err := s.RecordTargetResult(ctx, job.ID, "web01.example.com", StatusSucceeded, &exitCode, "changes applied", ""); err != nil {
		t.Fatalf("RecordTargetResult() error: %v", err)
	}

	got, err := s.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error: %v", err)
	}
	if len(got.Targets) != 1 {
		t.Fatalf("len(Targets) = %d, want 1", len(got.Targets))
	}
	target := got.Targets[0]
	if target.Status != StatusSucceeded {
		t.Errorf("Status = %q, want %q", target.Status, StatusSucceeded)
	}
	if target.ExitCode == nil || *target.ExitCode != 2 {
		t.Errorf("ExitCode = %v, want 2", target.ExitCode)
	}
	if target.Output != "changes applied" {
		t.Errorf("Output = %q", target.Output)
	}
	if target.FinishedAt == nil {
		t.Error("FinishedAt is nil after recording a result")
	}
}

func TestRecordTargetResult_UnknownTargetIsNotFound(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	job, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"web01.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}

	err = s.RecordTargetResult(ctx, job.ID, "not-a-target.example.com", StatusFailed, nil, "", "not connected")
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestSetTargetReport_LinksReportHash(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	job, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"web01.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}

	if err := s.SetTargetReport(ctx, job.ID, "web01.example.com", "abc123hash"); err != nil {
		t.Fatalf("SetTargetReport() error: %v", err)
	}

	got, err := s.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error: %v", err)
	}
	if got.Targets[0].ReportHash != "abc123hash" {
		t.Errorf("ReportHash = %q, want abc123hash", got.Targets[0].ReportHash)
	}
}

func TestCompleteJob_MarksTerminalStatus(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	job, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"web01.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}

	if err := s.CompleteJob(ctx, job.ID, StatusSucceeded); err != nil {
		t.Fatalf("CompleteJob() error: %v", err)
	}

	got, err := s.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error: %v", err)
	}
	if got.Status != StatusSucceeded {
		t.Errorf("Status = %q, want %q", got.Status, StatusSucceeded)
	}
	if got.FinishedAt == nil {
		t.Error("FinishedAt is nil after CompleteJob()")
	}
}

func TestCompleteJob_UnknownJobIsNotFound(t *testing.T) {
	s := testStore(t)
	err := s.CompleteJob(context.Background(), -1, StatusSucceeded)
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestGetJob_NotFound(t *testing.T) {
	s := testStore(t)
	_, err := s.GetJob(context.Background(), -1)
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestListJobs_MostRecentFirstAndPagination(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	first, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"a.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}
	second, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"b.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}

	firstPage, total, err := s.ListJobs(ctx, 1, 1, JobFilter{})
	if err != nil {
		t.Fatalf("ListJobs(page 1) error: %v", err)
	}
	if len(firstPage) != 1 {
		t.Fatalf("len(firstPage) = %d, want 1", len(firstPage))
	}
	if total < 2 {
		t.Errorf("total = %d, want at least 2", total)
	}
	if firstPage[0].ID != second.ID {
		t.Errorf("firstPage[0].ID = %d, want %d (most recently created)", firstPage[0].ID, second.ID)
	}
	// Targets are not loaded by ListJobs.
	if firstPage[0].Targets != nil {
		t.Errorf("ListJobs() loaded Targets, want nil (use GetJob for full detail)")
	}
	// But the lightweight target summary is.
	if firstPage[0].TargetCount != 1 {
		t.Errorf("firstPage[0].TargetCount = %d, want 1", firstPage[0].TargetCount)
	}
	if want := []string{"b.example.com"}; !slices.Equal(firstPage[0].TargetPreview, want) {
		t.Errorf("firstPage[0].TargetPreview = %v, want %v", firstPage[0].TargetPreview, want)
	}

	secondPage, _, err := s.ListJobs(ctx, 2, 1, JobFilter{})
	if err != nil {
		t.Fatalf("ListJobs(page 2) error: %v", err)
	}
	if len(secondPage) != 1 {
		t.Fatalf("len(secondPage) = %d, want 1", len(secondPage))
	}
	if secondPage[0].ID != first.ID {
		t.Errorf("secondPage[0].ID = %d, want %d (older job)", secondPage[0].ID, first.ID)
	}
}

func TestListJobs_TargetPreviewCapsFanOutJobs(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	targets := []string{"a.example.com", "b.example.com", "c.example.com", "d.example.com"}
	job, err := s.CreateJob(ctx, JobKindRun, "", "", nil, targets, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}

	page, _, err := s.ListJobs(ctx, 1, 1, JobFilter{})
	if err != nil {
		t.Fatalf("ListJobs() error: %v", err)
	}
	if len(page) != 1 || page[0].ID != job.ID {
		t.Fatalf("ListJobs() = %+v, want just job %d", page, job.ID)
	}

	if page[0].TargetCount != len(targets) {
		t.Errorf("TargetCount = %d, want %d", page[0].TargetCount, len(targets))
	}
	if want := targets[:targetPreviewLimit]; !slices.Equal(page[0].TargetPreview, want) {
		t.Errorf("TargetPreview = %v, want %v (capped at %d)", page[0].TargetPreview, want, targetPreviewLimit)
	}
}

// Filter tests below check "found, and everything returned actually
// matches" rather than an exact count/length - CreateJob's targets are
// fixed literals, so a prior run against this same shared, non-truncated
// dev database (see testStore) can leave earlier matching jobs behind.

func TestListJobs_FilterByKind(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	runJob, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"orchestrator-store-test-filter-kind.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob(run) error: %v", err)
	}
	taskJob, err := s.CreateJob(ctx, JobKindTask, "package", "", nil, []string{"orchestrator-store-test-filter-kind.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob(task) error: %v", err)
	}

	jobs, _, err := s.ListJobs(ctx, 1, 1000, JobFilter{Kind: string(JobKindRun)})
	if err != nil {
		t.Fatalf("ListJobs() error: %v", err)
	}
	found, sawTask := false, false
	for _, j := range jobs {
		if j.Kind != JobKindRun {
			t.Errorf("ListJobs() with Kind filter returned job with kind %q", j.Kind)
		}
		if j.ID == runJob.ID {
			found = true
		}
		if j.ID == taskJob.ID {
			sawTask = true
		}
	}
	if !found {
		t.Error("kind filter excluded the matching run job")
	}
	if sawTask {
		t.Error("kind=run filter included a task job")
	}
}

func TestListJobs_FilterByStatus(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	runningJob, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"orchestrator-store-test-filter-status.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}
	succeededJob, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"orchestrator-store-test-filter-status.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}
	if err := s.CompleteJob(ctx, succeededJob.ID, StatusSucceeded); err != nil {
		t.Fatalf("CompleteJob() error: %v", err)
	}

	jobs, _, err := s.ListJobs(ctx, 1, 1000, JobFilter{Status: StatusSucceeded})
	if err != nil {
		t.Fatalf("ListJobs() error: %v", err)
	}
	var sawSucceeded, sawRunning bool
	for _, j := range jobs {
		if j.ID == succeededJob.ID {
			sawSucceeded = true
		}
		if j.ID == runningJob.ID {
			sawRunning = true
		}
	}
	if !sawSucceeded {
		t.Error("status=succeeded filter excluded the succeeded job")
	}
	if sawRunning {
		t.Error("status=succeeded filter included a still-running job")
	}
}

func TestListJobs_FilterByTriggeredBy(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	targetJob, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"orchestrator-store-test-filter-triggeredby.example.com"}, "orchestrator-filter-test-actor")
	if err != nil {
		t.Fatalf("CreateJob(target) error: %v", err)
	}
	if _, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"orchestrator-store-test-filter-triggeredby.example.com"}, "someone-else"); err != nil {
		t.Fatalf("CreateJob(other) error: %v", err)
	}

	jobs, _, err := s.ListJobs(ctx, 1, 1000, JobFilter{TriggeredBy: "orchestrator-filter-test-actor"})
	if err != nil {
		t.Fatalf("ListJobs() error: %v", err)
	}
	found := false
	for _, j := range jobs {
		if j.TriggeredBy != "orchestrator-filter-test-actor" {
			t.Errorf("ListJobs() with TriggeredBy filter returned job triggered by %q", j.TriggeredBy)
		}
		if j.ID == targetJob.ID {
			found = true
		}
	}
	if !found {
		t.Error("triggeredBy filter excluded the matching job")
	}
}

func TestListJobs_FilterByTarget(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	targetJob, err := s.CreateJob(ctx, JobKindRun, "", "", nil,
		[]string{"orchestrator-store-test-filter-target.example.com", "orchestrator-store-test-filter-target-other.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob(matching) error: %v", err)
	}
	if _, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"orchestrator-store-test-filter-target-other.example.com"}, "actor"); err != nil {
		t.Fatalf("CreateJob(non-matching) error: %v", err)
	}

	jobs, _, err := s.ListJobs(ctx, 1, 1000, JobFilter{Target: "orchestrator-store-test-filter-target.example.com"})
	if err != nil {
		t.Fatalf("ListJobs() error: %v", err)
	}
	found := false
	for _, j := range jobs {
		if !slices.Contains(j.TargetPreview, "orchestrator-store-test-filter-target.example.com") {
			t.Errorf("ListJobs() with Target filter returned job %d whose preview %v doesn't include it", j.ID, j.TargetPreview)
		}
		if j.ID == targetJob.ID {
			found = true
		}
	}
	if !found {
		t.Error("target filter excluded the matching job")
	}
}

// Reproduces the reported bug: typing a partial certname (e.g. "web01"
// while searching for "web01.example.com") returned nothing, because
// Target used to be an exact match against job_targets.certname.
func TestListJobs_FilterByTarget_PartialMatch(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	targetJob, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"orchestrator-store-test-filter-target-partial.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}

	jobs, _, err := s.ListJobs(ctx, 1, 1000, JobFilter{Target: "filter-target-partial"})
	if err != nil {
		t.Fatalf("ListJobs() error: %v", err)
	}
	found := false
	for _, j := range jobs {
		if !slices.ContainsFunc(j.TargetPreview, func(c string) bool { return strings.Contains(c, "filter-target-partial") }) {
			t.Errorf("ListJobs() with partial Target filter returned job %d whose preview %v doesn't contain the substring", j.ID, j.TargetPreview)
		}
		if j.ID == targetJob.ID {
			found = true
		}
	}
	if !found {
		t.Error("partial target filter excluded the matching job")
	}

	// And case-insensitively.
	jobsUpper, _, err := s.ListJobs(ctx, 1, 1000, JobFilter{Target: "FILTER-TARGET-PARTIAL"})
	if err != nil {
		t.Fatalf("ListJobs() error: %v", err)
	}
	if !slices.ContainsFunc(jobsUpper, func(j Job) bool { return j.ID == targetJob.ID }) {
		t.Error("uppercase partial target filter excluded the matching job")
	}
}

func TestListJobs_SortAsc(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Scoped to just these two jobs via a TriggeredBy unique to this
	// test, rather than relying on a page size of 1000 comfortably
	// covering the whole (shared, ever-growing - see testStore) jobs
	// table: it doesn't once the table passes 1000 rows, which it has
	// after enough runs of this suite - see the ListJobs_FilterByTarget
	// bugfix this test was added alongside.
	const triggeredBy = "orchestrator-store-test-sortasc-actor"
	firstJob, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"orchestrator-store-test-sortasc-1.example.com"}, triggeredBy)
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}
	secondJob, err := s.CreateJob(ctx, JobKindRun, "", "", nil, []string{"orchestrator-store-test-sortasc-2.example.com"}, triggeredBy)
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}

	jobs, _, err := s.ListJobs(ctx, 1, 1000, JobFilter{TriggeredBy: triggeredBy, SortAsc: true})
	if err != nil {
		t.Fatalf("ListJobs() error: %v", err)
	}

	var firstIdx, secondIdx = -1, -1
	for i, j := range jobs {
		if j.ID == firstJob.ID {
			firstIdx = i
		}
		if j.ID == secondJob.ID {
			secondIdx = i
		}
	}
	if firstIdx == -1 || secondIdx == -1 {
		t.Fatal("test jobs not found in ListJobs()")
	}
	if firstIdx >= secondIdx {
		t.Errorf("SortAsc: older job at index %d, more recent at %d - want oldest first", firstIdx, secondIdx)
	}
}

func TestCreatePlan_AndGetPlanSteps(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	plan, err := s.CreatePlan(ctx, "deploy-and-verify", []PlanStep{
		{Kind: JobKindRun},
		{Kind: JobKindTask, TaskName: "verify", Params: []byte(`{"check":"health"}`)},
	}, []string{"web01.example.com"}, "orchestrator-store-test-actor")
	if err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}
	if plan.Kind != JobKindPlan {
		t.Errorf("Kind = %q, want %q", plan.Kind, JobKindPlan)
	}
	if plan.PlanName != "deploy-and-verify" {
		t.Errorf("PlanName = %q", plan.PlanName)
	}

	steps, err := s.GetPlanSteps(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlanSteps() error: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("len(steps) = %d, want 2", len(steps))
	}
	if steps[0].Kind != JobKindRun {
		t.Errorf("steps[0].Kind = %q, want %q", steps[0].Kind, JobKindRun)
	}
	if steps[0].ParentJobID == nil || *steps[0].ParentJobID != plan.ID {
		t.Errorf("steps[0].ParentJobID = %v, want %d", steps[0].ParentJobID, plan.ID)
	}
	if steps[0].StepOrder == nil || *steps[0].StepOrder != 0 {
		t.Errorf("steps[0].StepOrder = %v, want 0", steps[0].StepOrder)
	}
	if steps[1].Kind != JobKindTask || steps[1].TaskName != "verify" {
		t.Errorf("steps[1] = %+v, want kind task, task name verify", steps[1])
	}
	if steps[1].StepOrder == nil || *steps[1].StepOrder != 1 {
		t.Errorf("steps[1].StepOrder = %v, want 1", steps[1].StepOrder)
	}
	// Each step gets the plan's own targets.
	if len(steps[0].Targets) != 1 || steps[0].Targets[0].Certname != "web01.example.com" {
		t.Errorf("steps[0].Targets = %+v", steps[0].Targets)
	}
}

func TestCreatePlan_NoStepsIsError(t *testing.T) {
	s := testStore(t)
	_, err := s.CreatePlan(context.Background(), "empty-plan", nil, []string{"web01.example.com"}, "actor")
	if err == nil {
		t.Fatal("CreatePlan() with no steps: expected an error, got nil")
	}
}

func TestCreateJob_TaskKindStoresTaskName(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	job, err := s.CreateJob(ctx, JobKindTask, "package", "", []byte(`{"name":"nginx"}`), []string{"web01.example.com"}, "actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}
	if job.Kind != JobKindTask {
		t.Errorf("Kind = %q, want %q", job.Kind, JobKindTask)
	}
	if job.TaskName != "package" {
		t.Errorf("TaskName = %q, want package", job.TaskName)
	}
}
