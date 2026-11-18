package orchestrator

import (
	"context"
	"encoding/json"
	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
	"strings"
	"testing"
	"time"
)

func newCodePlan(t *testing.T, s *Store) Job {
	t.Helper()
	job, err := s.CreateCodePlanJob(context.Background(), "ops::demo", "production", json.RawMessage(`{"targets":["x"]}`), "tester", JobOrigin{})
	if err != nil {
		t.Fatal(err)
	}
	closeAfter(t, s, job.ID)
	return job
}

func TestCodePlanJob_TraceResultAndFinish(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	job := newCodePlan(t, s)
	if job.Kind != JobKindCodePlan || job.PlanName != "ops::demo" || job.Environment != "production" || job.HeartbeatAt == nil || len(job.Trace) != 0 {
		t.Fatalf("job = %+v", job)
	}
	now := time.Now().UTC()
	jid := int64(99)
	trace := []TraceEntry{{Seq: 1, Path: "1", Depth: 0, Plan: "ops::demo", Kind: "task", Task: "a::b", State: "succeeded", StartedAt: &now, JobID: &jid}}
	if err := s.SaveTrace(ctx, job.ID, trace); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetJob(ctx, job.ID)
	if len(got.Trace) != 1 || got.Trace[0].Task != "a::b" || got.Trace[0].JobID == nil || *got.Trace[0].JobID != 99 {
		t.Errorf("trace = %+v", got.Trace)
	}
	ok, err := s.FinishCodePlan(ctx, job.ID, StatusSucceeded, trace, json.RawMessage(`{"done":true}`), "")
	if err != nil || !ok {
		t.Fatalf("finish = %v, %v", ok, err)
	}
	got, _ = s.GetJob(ctx, job.ID)
	if got.Status != StatusSucceeded || got.FinishedAt == nil || string(got.Result) != `{"done": true}` && string(got.Result) != `{"done":true}` {
		t.Errorf("finished = %s %s %v", got.Status, got.Result, got.FinishedAt)
	}
	// A job already ended is left as it is: the reaper may have failed it.
	if ok, _ := s.FinishCodePlan(ctx, job.ID, StatusFailed, trace, nil, "late"); ok {
		t.Error("a finished plan was finished again")
	}
	if err := s.SaveTrace(ctx, job.ID, trace); err != ErrNotFound {
		t.Errorf("saving a trace after the end = %v", err)
	}
	if kind, err := s.JobKindOf(ctx, job.ID); err != nil || kind != JobKindCodePlan {
		t.Errorf("kind = %v, %v", kind, err)
	}
}

func TestReaper_ACodePlanWithAFreshHeartbeatSurvivesAndASilentOneFails(t *testing.T) {
	r, s := quietReaper(t)
	ctx := context.Background()
	alive, silent := newCodePlan(t, s), newCodePlan(t, s)
	pool := s.pool
	if _, err := pool.Exec(ctx, `UPDATE jobs SET started_at = now() - interval '6 hours', heartbeat_at = now() - interval '30 seconds' WHERE id = $1`, alive.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET heartbeat_at = now() - interval '12 minutes' WHERE id = $1`, silent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReapOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetJob(ctx, alive.ID); got.Status != StatusRunning {
		t.Errorf("a plan that reported 30 seconds ago is %s", got.Status)
	}
	if got, _ := s.GetJob(ctx, silent.ID); got.Status != StatusFailed {
		t.Errorf("a silent plan is %s", got.Status)
	}
}

// A code plan's step jobs are children of the plan, hidden from the list,
// and a reader limited to some nodes sees the plan through the steps they
// may see, without what the plan printed or was given.
func TestCodePlanJob_IsReadWithinTheCallersScope(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mine, theirs := uniq("cp-mine"), uniq("cp-theirs")
	plan, err := s.CreateCodePlanJob(ctx, "ops::demo", "production", json.RawMessage(`{"targets":["`+theirs+`"]}`), "tester", JobOrigin{})
	if err != nil {
		t.Fatal(err)
	}
	closeAfter(t, s, plan.ID)
	for i, n := range []string{mine, theirs} {
		step := i + 1
		job, err := s.CreateTaskJobChild(ctx, "mod::echo", "production", nil, []string{n}, "tester", JobOrigin{}, &plan.ID, &step)
		if err != nil {
			t.Fatal(err)
		}
		closeAfter(t, s, job.ID)
	}
	trace := []TraceEntry{{Seq: 1, Path: "1", Plan: "ops::demo", Kind: "message", State: "succeeded", Message: "restarted " + theirs}}
	if _, err := s.FinishCodePlan(ctx, plan.ID, StatusFailed, trace, json.RawMessage(`{"on":"`+theirs+`"}`), "failed on "+theirs); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetJobInScope(ctx, plan.ID, []string{mine})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), theirs) || !got.TargetsHidden || !got.TraceHidden || len(got.Trace) != 1 || got.Trace[0].Kind != "message" {
		t.Errorf("scoped plan = %s", b)
	}
	// Someone with every node sees all of it.
	all, _ := s.GetJobInScope(ctx, plan.ID, nil)
	if !strings.Contains(all.Trace[0].Message, theirs) || all.TraceHidden || !strings.Contains(string(all.Result), theirs) {
		t.Errorf("the unrestricted plan = %+v", all)
	}
	// Someone with none of its nodes does not find it.
	if _, err := s.GetJobInScope(ctx, plan.ID, []string{"someone-else"}); err != ErrNotFound {
		t.Errorf("a reader with none of its nodes = %v", err)
	}
	// The list holds the plan and not its steps.
	list, _, err := s.ListJobs(ctx, 1, 100, JobFilter{Kind: "codeplan"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, j := range list {
		if j.ID == plan.ID {
			found = true
		}
		if j.Kind == JobKindTask && j.ParentJobID != nil {
			t.Errorf("a step job %d is in the list", j.ID)
		}
	}
	if !found {
		t.Error("the plan is not in the list")
	}
}

// A saved plan's steps advance by themselves; a code plan follows its own
// steps, so finishing one must not start another.
func TestDispatcher_DoesNotAdvanceACodePlansSteps(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	tr := &scriptedTransport{stdout: "ok"}
	d := NewDispatcher(s, tr, nil)
	go d.Run(ctx)

	plan := newCodePlan(t, s)
	n1, n2 := uniq("adv-1"), uniq("adv-2")
	var steps []Job
	for i, n := range []string{n1, n2} {
		step := i + 1
		job, err := s.CreateTaskJobChild(ctx, "mod::echo", "production", nil, []string{n}, "tester", JobOrigin{}, &plan.ID, &step)
		if err != nil {
			t.Fatal(err)
		}
		closeAfter(t, s, job.ID)
		steps = append(steps, job)
	}
	d.DispatchTask(ctx, steps[0], taskcatalogResolved())

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got, _ := s.GetJob(ctx, steps[0].ID); got.Status != StatusRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if asked := tr.askedOf(); len(asked) != 1 || asked[0] != n1 {
		t.Errorf("the transport was asked %v, want only the first step's node", asked)
	}
	if got, _ := s.GetJob(ctx, steps[1].ID); got.Targets[0].Status != StatusPending {
		t.Errorf("the second step moved to %s by itself", got.Targets[0].Status)
	}
	if got, _ := s.GetJob(ctx, plan.ID); got.Status != StatusRunning {
		t.Errorf("the plan was ended by its step: %s", got.Status)
	}
}

func taskcatalogResolved() taskcatalog.Resolved { return taskcatalog.Resolved{Task: "mod::echo"} }
