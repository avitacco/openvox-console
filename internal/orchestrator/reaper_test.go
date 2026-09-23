package orchestrator

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

// A job whose dispatching instance stopped would otherwise read
// "running" forever, indistinguishable from one still in progress.
func TestReaperFailsAJobLeftRunning(t *testing.T) {
	pool := testdb.Pool(t)
	store := NewStore(pool)
	ctx := context.Background()

	job, err := store.CreateJob(ctx, JobKindRun, "", "", nil, []string{"stranded.example.com"}, "tester")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}

	// Backdate past the staleness threshold, standing in for an
	// instance that dispatched this job and then stopped.
	if _, err := pool.Exec(ctx,
		`UPDATE jobs SET started_at = now() - interval '25 hours' WHERE id = $1`, job.ID); err != nil {
		t.Fatalf("backdate job: %v", err)
	}

	r := NewReaper(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// The count is not asserted: this runs against a shared test
	// database that accumulates jobs across runs, so the meaningful
	// assertion is what happened to *this* job.
	if _, err := r.ReapOnce(ctx); err != nil {
		t.Fatalf("ReapOnce() error: %v", err)
	}

	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error: %v", err)
	}
	if got.Status != StatusFailed {
		t.Errorf("job status = %q, want %q; it was left running indefinitely", got.Status, StatusFailed)
	}
	if got.FinishedAt == nil {
		t.Error("a reaped job has no finished_at")
	}
	for _, target := range got.Targets {
		if target.Status != StatusFailed {
			t.Errorf("target %q status = %q, want %q", target.Certname, target.Status, StatusFailed)
		}
		if target.ErrorDetail != StaleReason {
			t.Errorf("target %q error detail = %q, want the untrackable reason", target.Certname, target.ErrorDetail)
		}
	}
}

// A job still within the threshold is one whose dispatch may genuinely
// still be in flight - reaping it would fail work that is succeeding.
func TestReaperLeavesRecentJobsAlone(t *testing.T) {
	pool := testdb.Pool(t)
	store := NewStore(pool)
	ctx := context.Background()

	job, err := store.CreateJob(ctx, JobKindRun, "", "", nil, []string{"inflight.example.com"}, "tester")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}

	r := NewReaper(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := r.ReapOnce(ctx); err != nil {
		t.Fatalf("ReapOnce() error: %v", err)
	}

	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error: %v", err)
	}
	if got.Status == StatusFailed {
		t.Error("the reaper failed a job that had only just started")
	}
}

// Reaping twice is harmless: the second pass finds nothing, so two
// instances overlapping cannot double-fail anything.
func TestReaperIsIdempotent(t *testing.T) {
	pool := testdb.Pool(t)
	store := NewStore(pool)
	ctx := context.Background()

	job, err := store.CreateJob(ctx, JobKindRun, "", "", nil, []string{"stranded2.example.com"}, "tester")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE jobs SET started_at = now() - interval '25 hours' WHERE id = $1`, job.ID); err != nil {
		t.Fatalf("backdate job: %v", err)
	}

	r := NewReaper(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := r.ReapOnce(ctx); err != nil {
		t.Fatalf("first ReapOnce() error: %v", err)
	}
	second, err := r.ReapOnce(ctx)
	if err != nil {
		t.Fatalf("second ReapOnce() error: %v", err)
	}
	// Nothing is left to fail: the first pass already made every stale
	// job terminal, so a second instance overlapping cannot double-fail
	// anything.
	if second != 0 {
		t.Errorf("a second reap failed %d further jobs, want 0", second)
	}

	got, err := store.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error: %v", err)
	}
	if got.Status != StatusFailed {
		t.Errorf("job status = %q, want %q", got.Status, StatusFailed)
	}
}
