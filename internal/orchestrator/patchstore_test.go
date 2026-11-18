package orchestrator

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

func quietReaper(t *testing.T) (*Reaper, *Store) {
	t.Helper()
	pool := testdb.Pool(t)
	return NewReaper(pool, slog.New(slog.NewTextHandler(io.Discard, nil))), NewStore(pool)
}

// uniq makes node names that no earlier run of the tests, in the shared
// test database, can still be holding.
func uniq(prefix string) string { return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()) }

// closeAfter ends the patch job when the test does, so it holds no node.
func closeAfter(t *testing.T, s *Store, id int64) {
	t.Helper()
	t.Cleanup(func() { _ = s.CompleteJob(context.Background(), id, StatusFailed) })
}

func patchOptions(batch int) PatchOptions {
	return PatchOptions{Scope: PatchScopeSecurity, BatchSize: batch, StopAfterFailures: 3}
}

func TestCreatePatchJob_BatchesNodesInOrder(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := uniq("pj") + "-"
	job, err := s.CreatePatchJob(ctx, patchOptions(2), []string{p + "e", p + "a", p + "c", p + "b", p + "d"}, "tester", JobOrigin{})
	if err != nil {
		t.Fatal(err)
	}
	closeAfter(t, s, job.ID)
	if job.Kind != JobKindPatch || job.PatchOptions == nil || job.PatchOptions.BatchSize != 2 || job.HeartbeatAt == nil {
		t.Fatalf("job = %+v", job)
	}
	want := map[string]int{p + "a": 1, p + "b": 1, p + "c": 2, p + "d": 2, p + "e": 3}
	for _, tg := range job.Targets {
		if tg.Batch == nil || *tg.Batch != want[tg.Certname] || tg.Status != StatusPending {
			t.Errorf("target %s = batch %v status %s, want batch %d pending", tg.Certname, tg.Batch, tg.Status, want[tg.Certname])
		}
	}
}

func TestPatchTargets_StartTouchAndCloseNotStarted(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, b := uniq("pt-a"), uniq("pt-b")
	job, err := s.CreatePatchJob(ctx, patchOptions(1), []string{a, b}, "tester", JobOrigin{})
	if err != nil {
		t.Fatal(err)
	}
	closeAfter(t, s, job.ID)
	if err := s.StartPatchTarget(ctx, job.ID, a); err != nil {
		t.Fatal(err)
	}
	if err := s.StartPatchTarget(ctx, job.ID, a); err != ErrNotFound {
		t.Errorf("starting a target twice = %v, want ErrNotFound", err)
	}
	if err := s.TouchTarget(ctx, job.ID, a); err != nil {
		t.Fatal(err)
	}
	n, err := s.CloseNotStarted(ctx, job.ID, "not started: test")
	if err != nil || n != 1 {
		t.Fatalf("CloseNotStarted = %d, %v; want 1", n, err)
	}
	got, _ := s.GetJob(ctx, job.ID)
	for _, tg := range got.Targets {
		switch tg.Certname {
		case a:
			if tg.Status != StatusRunning || tg.HeartbeatAt == nil {
				t.Errorf("a = %s heartbeat %v", tg.Status, tg.HeartbeatAt)
			}
		case b:
			if tg.Status != StatusFailed || tg.ErrorDetail != "not started: test" {
				t.Errorf("b = %s %q", tg.Status, tg.ErrorDetail)
			}
		}
	}
}

func TestNodesBeingPatched(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, b, c := uniq("nbp-a"), uniq("nbp-b"), uniq("nbp-c")
	job, err := s.CreatePatchJob(ctx, patchOptions(5), []string{a, b}, "tester", JobOrigin{})
	if err != nil {
		t.Fatal(err)
	}
	closeAfter(t, s, job.ID)
	got, err := s.NodesBeingPatched(ctx, []string{a, c})
	if err != nil || !slices.Equal(got, []string{a}) {
		t.Fatalf("NodesBeingPatched = %v, %v", got, err)
	}
	if err := s.CompleteJob(ctx, job.ID, StatusFailed); err != nil {
		t.Fatal(err)
	}
	got, _ = s.NodesBeingPatched(ctx, []string{a})
	if len(got) != 0 {
		t.Errorf("a finished run still holds its nodes: %v", got)
	}
}

// A patch run lasts hours: only silence fails it.
func TestReaper_PatchJobIsJudgedBySilence(t *testing.T) {
	r, s := quietReaper(t)
	ctx := context.Background()
	pool := testdb.Pool(t)

	alive, err := s.CreatePatchJob(ctx, patchOptions(1), []string{uniq("rp-alive")}, "tester", JobOrigin{})
	if err != nil {
		t.Fatal(err)
	}
	closeAfter(t, s, alive.ID)
	silent, err := s.CreatePatchJob(ctx, patchOptions(1), []string{uniq("rp-silent")}, "tester", JobOrigin{})
	if err != nil {
		t.Fatal(err)
	}
	// The long-running one started five hours ago but reported a minute ago.
	if _, err := pool.Exec(ctx, `UPDATE jobs SET started_at = now() - interval '5 hours', heartbeat_at = now() - interval '1 minute' WHERE id = $1`, alive.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET heartbeat_at = now() - interval '11 minutes' WHERE id = $1`, silent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReapOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetJob(ctx, alive.ID); got.Status != StatusRunning {
		t.Errorf("a patch job that reported a minute ago is %s", got.Status)
	}
	got, _ := s.GetJob(ctx, silent.ID)
	if got.Status != StatusFailed || got.Targets[0].Status != StatusFailed || got.Targets[0].ErrorDetail != SilentReason {
		t.Errorf("silent job = %s, target %s %q", got.Status, got.Targets[0].Status, got.Targets[0].ErrorDetail)
	}
}

// A job that does not report progress keeps the age rule, whatever its kind.
func TestReaper_JobsWithoutAHeartbeatKeepTheAgeRule(t *testing.T) {
	r, s := quietReaper(t)
	ctx := context.Background()
	pool := testdb.Pool(t)

	job, _ := s.CreateJob(ctx, JobKindTask, "", "", nil, []string{"rp-task"}, "tester")
	if _, err := pool.Exec(ctx, `UPDATE jobs SET started_at = now() - interval '121 minutes' WHERE id = $1`, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReapOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetJob(ctx, job.ID)
	if got.Status != StatusFailed || got.Targets[0].ErrorDetail != StaleReason {
		t.Errorf("a task over two hours old = %s %q", got.Status, got.Targets[0].ErrorDetail)
	}
}
