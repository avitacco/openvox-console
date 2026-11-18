package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/sealer"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

func groupID(id int64) *int64 { return &id }

// --- saving ---

func TestCreate_StoresTheScheduleWithItsOwnerAndNextStart(t *testing.T) {
	h := newHarness(t)
	h.groups[7] = []string{"web1"}

	sched := h.create(Input{
		Name: "nightly no-op", Kind: orchestrator.JobKindRun, Options: json.RawMessage(`{"noop":true}`),
		Group: groupID(7), StartAt: h.in(14 * time.Hour),
	})
	if sched.Owner != h.owner.Name || sched.OwnerKind != OwnerUser {
		t.Errorf("owner = %s/%s, want the caller", sched.Owner, sched.OwnerKind)
	}
	if sched.NextRunAt == nil || !sched.NextRunAt.Equal(h.in(14*time.Hour)) || sched.State != StateActive {
		t.Errorf("next = %v, state = %s; want the start time, active", sched.NextRunAt, sched.State)
	}
	if sched.Group == nil || *sched.Group != 7 || string(sched.Options) != `{"noop": true}` && string(sched.Options) != `{"noop":true}` {
		t.Errorf("stored group = %v, options = %s", sched.Group, sched.Options)
	}
	if got := h.activityContaining("schedule.created"); len(got) != 1 || !strings.Contains(got[0], h.owner.Name) || !strings.Contains(got[0], "nightly no-op") {
		t.Errorf("activity = %v, want the caller creating it by name", got)
	}

	h.matches("a.example.com")
	task := h.create(Input{
		Name: "restart", Kind: orchestrator.JobKindTask, Task: "mod::echo", Params: json.RawMessage(`{"message":"hi"}`),
		Query: `nodes[certname] { }`, StartAt: h.in(time.Hour),
	})
	if task.Task != "mod::echo" || task.Environment != "production" || task.Query == "" || !strings.Contains(string(task.Params), `"hi"`) {
		t.Errorf("stored task schedule = %+v", task)
	}
}

func TestCreate_IsCheckedAsTheOnDemandRequestWouldBe(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	scoped := Caller{Owner: h.owner, Scope: nodescope.Of("web1"), Tasks: taskscope.Unrestricted()}

	// A task parameter of the wrong type.
	_, err := h.svc.Create(ctx, Input{Name: "n", Kind: orchestrator.JobKindTask, Task: "mod::echo",
		Params: json.RawMessage(`{"count":"three"}`), Targets: []string{"web1"}, StartAt: h.in(time.Hour)}, h.caller())
	var task *orchestrator.InvalidTaskError
	if !errors.As(err, &task) || !strings.Contains(err.Error(), "count") {
		t.Errorf("bad parameter = %v, want an InvalidTaskError naming count", err)
	}

	// A named node outside the caller's scope.
	_, err = h.svc.Create(ctx, h.runOn("web1", "db1"), scoped)
	var outside *orchestrator.OutOfScopeError
	if !errors.As(err, &outside) || !reflect.DeepEqual(outside.Outside, []string{"db1"}) {
		t.Errorf("out of scope = %v, want an OutOfScopeError naming db1", err)
	}

	// A query openvoxdb rejects, relayed in its words.
	h.queryErr = &openvoxdbRejected
	_, err = h.svc.Create(ctx, Input{Name: "n", Kind: orchestrator.JobKindRun, Query: "nodes[certname] { certname = }", StartAt: h.in(time.Hour)}, h.caller())
	if !orchestrator.IsRefusal(err) || !strings.Contains(err.Error(), "PQL parse error") {
		t.Errorf("rejected query = %v, want a refusal with openvoxdb's message", err)
	}
	h.queryErr = nil

	// A subquery from a scoped caller.
	_, err = h.svc.Create(ctx, Input{Name: "n", Kind: orchestrator.JobKindRun,
		Query: `nodes[certname] { facts { name = "role" } }`, StartAt: h.in(time.Hour)}, scoped)
	if !orchestrator.IsRefusal(err) || !strings.Contains(err.Error(), "subqueries") {
		t.Errorf("scoped subquery = %v, want a refusal", err)
	}

	// An environment the server does not have; unknown run option.
	_, err = h.svc.Create(ctx, Input{Name: "n", Kind: orchestrator.JobKindRun, Options: json.RawMessage(`{"environment":"nope"}`),
		Targets: []string{"web1"}, StartAt: h.in(time.Hour)}, h.caller())
	var options *orchestrator.InvalidRunOptionsError
	if !errors.As(err, &options) {
		t.Errorf("unknown environment = %v, want an InvalidRunOptionsError", err)
	}

	// A plan with nothing in it, or no kind at all.
	_, err = h.svc.Create(ctx, Input{Name: "n", Kind: orchestrator.JobKindPlan, Targets: []string{"web1"}, StartAt: h.in(time.Hour)}, h.caller())
	if !orchestrator.IsPlanShapeError(err) || !strings.Contains(err.Error(), "at least one step") {
		t.Errorf("a plan with no steps = %v, want it refused for having none", err)
	}
	_, err = h.svc.Create(ctx, Input{Name: "n", Targets: []string{"web1"}, StartAt: h.in(time.Hour)}, h.caller())
	wantField(t, err, "kind", `"run", "task" or "plan"`)

	// Timing.
	_, err = h.svc.Create(ctx, Input{Name: "n", Kind: orchestrator.JobKindRun, Targets: []string{"web1"}, StartAt: h.in(-time.Minute)}, h.caller())
	wantField(t, err, "startAt", "passed")
	_, err = h.svc.Create(ctx, Input{Name: "n", Kind: orchestrator.JobKindRun, Targets: []string{"web1"}, StartAt: h.in(time.Hour), Repeat: &Repeat{2, UnitMinute}}, h.caller())
	wantField(t, err, "repeat", "5 minutes")

	if all, _ := h.svc.List(ctx, Caller{Owner: h.owner}); len(all) != 0 {
		t.Errorf("%d schedules were stored by refused requests", len(all))
	}

	// A valid query that matches nothing yet is saved.
	h.matches()
	if _, err := h.svc.Create(ctx, Input{Name: "not yet", Kind: orchestrator.JobKindRun, Query: "nodes[certname] { }", StartAt: h.in(time.Hour)}, h.caller()); err != nil {
		t.Errorf("a query matching nothing yet = %v, want it saved", err)
	}
	// A repeating schedule may start in the past: it next comes due on
	// its own grid.
	past, err := h.svc.Create(ctx, Input{Name: "daily", Kind: orchestrator.JobKindRun, Targets: []string{"web1"},
		StartAt: h.in(-72*time.Hour + time.Hour), Repeat: &Repeat{1, UnitDay}}, h.caller())
	if err != nil || past.NextRunAt == nil || !past.NextRunAt.Equal(h.in(time.Hour)) {
		t.Errorf("repeating from the past: next = %v, %v; want an hour from now", past.NextRunAt, err)
	}
}

// --- sensitive parameters ---

func secretTask(h *harness, params string) Input {
	return Input{Name: "rotate", Kind: orchestrator.JobKindTask, Task: "mod::secret",
		Params: json.RawMessage(params), Targets: []string{"web1"}, StartAt: h.in(time.Hour)}
}

func TestSensitiveParameters_SealedRedactedAndSentWhenFired(t *testing.T) {
	h := newHarness(t)
	sched := h.create(secretTask(h, `{"user":"deploy","password":"hunter2"}`))

	encoded, _ := json.Marshal(h.get(sched.ID))
	if strings.Contains(string(encoded), "hunter2") || !strings.Contains(string(encoded), orchestrator.RedactedValue) {
		t.Errorf("schedule as read = %s; want the password redacted", encoded)
	}
	var raw, sealed []byte
	if err := h.store.pool.QueryRow(context.Background(), `SELECT params::text, sealed_params FROM scheduled_jobs WHERE id = $1`, sched.ID).Scan(&raw, &sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter2") || strings.Contains(string(sealed), "hunter2") || len(sealed) == 0 {
		t.Errorf("stored params = %s, sealed = %d bytes; the password must be stored only sealed", raw, len(sealed))
	}

	fired := h.fireAt(sched.ID, h.in(time.Hour))
	f := wantOutcome(t, fired, OutcomeStarted, "")
	job := h.waitFinished(*f.JobID)
	if strings.Contains(string(job.Params), "hunter2") {
		t.Errorf("job params = %s; want the password redacted", job.Params)
	}
	if sent := strings.Join(h.transport.sent(), "\n"); !strings.Contains(sent, "hunter2") {
		t.Errorf("the node was not sent the real password: %s", sent)
	}
}

func TestSensitiveParameters_RefusedWithoutASecretsKey(t *testing.T) {
	h := newHarness(t)
	h.svc.sealer = nil
	_, err := h.svc.Create(context.Background(), secretTask(h, `{"user":"deploy","password":"hunter2"}`), h.caller())
	wantField(t, err, "params", "CONSOLE_SECRETS_KEY")
	if all, _ := h.svc.List(context.Background(), Caller{Owner: h.owner}); len(all) != 0 {
		t.Error("the schedule was stored")
	}
	// A task with no sensitive parameter needs no key.
	if _, err := h.svc.Create(context.Background(), Input{Name: "echo", Kind: orchestrator.JobKindTask, Task: "mod::echo",
		Targets: []string{"web1"}, StartAt: h.in(time.Hour)}, h.caller()); err != nil {
		t.Errorf("a task without sensitive parameters = %v, want it saved", err)
	}
}

func TestSensitiveParameters_KeptOnlyForTheOwnerAndTheSameTask(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	sched := h.create(secretTask(h, `{"user":"deploy","password":"hunter2"}`))

	// The owner changes the start time, sending back what a read gave
	// them: the stored value is kept.
	change := secretTask(h, `{"user":"deploy","password":"`+orchestrator.RedactedValue+`"}`)
	change.StartAt = h.in(2 * time.Hour)
	if _, err := h.svc.Replace(ctx, sched.ID, change, h.caller()); err != nil {
		t.Fatalf("owner's change = %v, want the stored password kept", err)
	}
	h.fireAt(sched.ID, h.in(2*time.Hour))
	if sent := strings.Join(h.transport.sent(), "\n"); !strings.Contains(sent, "hunter2") || strings.Contains(sent, orchestrator.RedactedValue) {
		t.Errorf("sent after the owner's change = %s; want the real password", sent)
	}

	// Someone else saving it must type the value.
	other := h.other(nodescope.Everything())
	again := secretTask(h, `{"user":"deploy"}`)
	again.StartAt = h.in(5 * time.Hour)
	_, err := h.svc.Replace(ctx, sched.ID, again, other)
	var task *orchestrator.InvalidTaskError
	if !errors.As(err, &task) || !strings.Contains(err.Error(), "password") {
		t.Errorf("another user's change without the value = %v, want it refused naming password", err)
	}
	if got := h.get(sched.ID); got.Owner != h.owner.Name {
		t.Errorf("owner = %s after a refused change, want it unchanged", got.Owner)
	}
}

func TestSensitiveParameters_SealedValueIsBoundToItsSchedule(t *testing.T) {
	h := newHarness(t)
	a := h.create(secretTask(h, `{"user":"deploy","password":"hunter2"}`))
	b := h.create(secretTask(h, `{"user":"deploy","password":"other"}`))
	if _, err := h.store.pool.Exec(context.Background(),
		`UPDATE scheduled_jobs SET sealed_params = (SELECT sealed_params FROM scheduled_jobs WHERE id = $1) WHERE id = $2`, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	fired := h.fireAt(b.ID, h.in(time.Hour))
	wantOutcome(t, fired, OutcomeSkipped, "could not be read")
	if sent := strings.Join(h.transport.sent(), "\n"); strings.Contains(sent, "hunter2") {
		t.Error("a sealed value copied from another schedule was opened and sent")
	}

	// And a changed key reads the same way.
	otherKey, _ := sealer.New([]byte(strings.Repeat("z", sealer.KeySize)))
	h.svc.sealer = otherKey
	wantOutcome(t, h.fireAt(a.ID, h.in(time.Hour)), OutcomeSkipped, "could not be read")
}

// --- claiming ---

func TestClaim_ExactlyOneOfManyWins(t *testing.T) {
	h := newHarness(t)
	sched := h.create(Input{Name: "contended", Kind: orchestrator.JobKindRun, Targets: []string{"web1"},
		StartAt: h.in(time.Minute), Repeat: &Repeat{1, UnitHour}})
	due := *sched.NextRunAt
	next := due.Add(time.Hour)

	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, claimed, err := h.store.Claim(context.Background(), sched.ID, due, &next)
			if err != nil {
				t.Errorf("Claim() error: %v", err)
			}
			if claimed {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Errorf("%d claimers won the same due time, want exactly 1", wins.Load())
	}
	got := h.get(sched.ID)
	if len(got.Firings) != 1 || !got.NextRunAt.Equal(next) {
		t.Errorf("firings = %d, next = %v; want one firing and the next hour", len(got.Firings), got.NextRunAt)
	}
}

func TestFiring_SeveralInstancesStartOneJob(t *testing.T) {
	h := newHarness(t)
	sched := h.create(h.runOn("web1"))
	at := h.in(time.Hour)
	h.now = at
	due := h.get(sched.ID)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each "instance" read the schedule as due before any claimed.
			if err := h.svc.fire(context.Background(), due, at); err != nil {
				t.Errorf("fire() error: %v", err)
			}
		}()
	}
	wg.Wait()
	if jobs := h.ownJobs(); len(jobs) != 1 {
		t.Errorf("%d jobs were started, want exactly 1", len(jobs))
	}
}

func TestFiring_InterruptedIsShownAndNotRetried(t *testing.T) {
	h := newHarness(t)
	sched := h.create(Input{Name: "hourly", Kind: orchestrator.JobKindRun, Targets: []string{"web1"},
		StartAt: h.in(time.Hour), Repeat: &Repeat{1, UnitHour}})
	// An instance claims the firing and stops before starting the job.
	next := h.in(2 * time.Hour)
	firingID, claimed, err := h.store.Claim(context.Background(), sched.ID, h.in(time.Hour), &next)
	if err != nil || !claimed {
		t.Fatalf("Claim() = %v, %v", claimed, err)
	}
	if _, err := h.store.pool.Exec(context.Background(), `UPDATE scheduled_job_firings SET fired_at = now() - interval '11 minutes' WHERE id = $1`, firingID); err != nil {
		t.Fatal(err)
	}

	// Nobody else picks it up...
	got := h.fireAt(sched.ID, h.in(time.Hour+time.Minute))
	if len(h.ownJobs()) != 0 || len(got.Firings) != 1 {
		t.Errorf("jobs = %d, firings = %d; want the interrupted firing left alone", len(h.ownJobs()), len(got.Firings))
	}
	if f := lastFiring(t, got); f.Outcome != OutcomeInterrupted || f.Detail == "" {
		t.Errorf("firing = %s %q, want interrupted with a reason", f.Outcome, f.Detail)
	}
	// ...and the schedule comes due again at its next time.
	got = h.fireAt(sched.ID, h.in(2*time.Hour))
	wantOutcome(t, got, OutcomeStarted, "")
}

// --- firing ---

func TestFiring_IsTheRequestMadeAtThatMoment(t *testing.T) {
	h := newHarness(t)
	h.groups[3] = []string{"web1", "web2"}
	sched := h.create(Input{Name: "staging no-op", Kind: orchestrator.JobKindRun,
		Options: json.RawMessage(`{"noop":true,"environment":"staging"}`), Group: groupID(3),
		StartAt: h.in(time.Hour), Repeat: &Repeat{1, UnitWeek}})

	// A node joins the group after the schedule was saved.
	h.groups[3] = []string{"web1", "web2", "web3"}
	fired := h.fireAt(sched.ID, h.in(time.Hour))
	f := wantOutcome(t, fired, OutcomeStarted, "")
	if !f.DueAt.Equal(h.in(0)) && !f.DueAt.Equal(fired.StartAt) {
		t.Errorf("due at = %s, want the start time", f.DueAt)
	}
	if fired.NextRunAt == nil || !fired.NextRunAt.Equal(sched.StartAt.Add(7*24*time.Hour)) {
		t.Errorf("next = %v, want a week after the start", fired.NextRunAt)
	}

	job := h.waitFinished(*f.JobID)
	if !reflect.DeepEqual(targetNames(job), []string{"web1", "web2", "web3"}) {
		t.Errorf("targets = %v, want the group as it is now", targetNames(job))
	}
	if job.TriggeredBy != h.owner.Name || job.Schedule == nil || job.Schedule.ID != sched.ID || job.Schedule.Name != "staging no-op" {
		t.Errorf("job triggered by %q, schedule %+v; want the owner and this schedule", job.TriggeredBy, job.Schedule)
	}
	if job.RunOptions == nil || !job.RunOptions.Noop || job.RunOptions.Environment != "staging" {
		t.Errorf("run options = %+v, want no-op in staging", job.RunOptions)
	}
	if job.TargetSource == nil || job.TargetSource.GroupID != 3 {
		t.Errorf("target source = %+v, want the group", job.TargetSource)
	}
	for _, payload := range h.transport.sent() {
		if !strings.Contains(payload, "run_with_options") {
			t.Errorf("sent %s; want the run-with-options request an on-demand run sends", payload)
		}
	}
	if got := h.activityContaining("run.triggered"); len(got) != 1 || !strings.Contains(got[0], `started by schedule "staging no-op"`) {
		t.Errorf("activity = %v, want the run's entry to name the schedule", got)
	}

	// Deleting the schedule leaves the job, still naming it.
	if err := h.svc.Delete(context.Background(), sched.ID, h.caller()); err != nil {
		t.Fatal(err)
	}
	if after := h.job(job.ID); after.Schedule == nil || after.Schedule.Name != "staging no-op" || after.Schedule.ID != 0 {
		t.Errorf("job's schedule after deletion = %+v, want the name alone", after.Schedule)
	}
}

func TestFiring_OneOffFinishes(t *testing.T) {
	h := newHarness(t)
	sched := h.create(h.runOn("web1"))

	if early := h.fireAt(sched.ID, h.in(59*time.Minute)); len(early.Firings) != 0 {
		t.Fatal("fired before it was due")
	}
	fired := h.fireAt(sched.ID, h.in(time.Hour))
	wantOutcome(t, fired, OutcomeStarted, "")
	if fired.NextRunAt != nil || fired.State != StateFinished {
		t.Errorf("next = %v, state = %s; want none, finished", fired.NextRunAt, fired.State)
	}
	if again := h.fireAt(sched.ID, h.in(3*time.Hour)); len(again.Firings) != 1 {
		t.Error("a finished schedule fired again")
	}
}

func TestFiring_SkippedIsRecordedAndNotRetried(t *testing.T) {
	cases := []struct {
		name    string
		input   func(h *harness) Input
		break_  func(h *harness)
		mention string
	}{
		{"the task no longer exists",
			func(h *harness) Input {
				return Input{Kind: orchestrator.JobKindTask, Task: "mod::echo", Targets: []string{"web1"}}
			},
			func(h *harness) { h.catalog.remove("mod::echo") }, "mod::echo"},
		{"nothing matches",
			func(h *harness) Input {
				h.matches("web1")
				return Input{Kind: orchestrator.JobKindRun, Query: "nodes[certname] { }"}
			},
			func(h *harness) { h.matches() }, "no nodes matched"},
		{"the node group was deleted",
			func(h *harness) Input {
				h.groups[5] = []string{"web1"}
				return Input{Kind: orchestrator.JobKindRun, Group: groupID(5)}
			},
			func(h *harness) { delete(h.groups, 5) }, "no longer exists"},
		{"openvoxdb cannot be reached",
			func(h *harness) Input {
				h.matches("web1")
				return Input{Kind: orchestrator.JobKindRun, Query: "nodes[certname] { }"}
			},
			func(h *harness) { h.queryErr = errors.New("openvoxdb unreachable: connection refused") }, "unreachable"},
		{"the owner lost the permission",
			func(h *harness) Input { return Input{Kind: orchestrator.JobKindRun, Targets: []string{"web1"}} },
			func(h *harness) { delete(h.authority, h.owner.Name) }, "no longer holds orchestrator:run"},
		{"a named node left the owner's scope",
			func(h *harness) Input {
				return Input{Kind: orchestrator.JobKindRun, Targets: []string{"web1", "web2", "db1"}}
			},
			func(h *harness) { h.authority[h.owner.Name] = nodescope.Of("web1", "web2") }, "db1"},
		{"a subquery after the owner became scoped",
			func(h *harness) Input {
				h.matches("web1")
				return Input{Kind: orchestrator.JobKindRun, Query: `nodes[certname] { facts { name = "role" } }`}
			},
			func(h *harness) { h.authority[h.owner.Name] = nodescope.Of("web1") }, "subqueries"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			in := c.input(h)
			in.Name, in.StartAt, in.Repeat = c.name, h.in(time.Hour), &Repeat{1, UnitDay}
			sched := h.create(in)
			c.break_(h)

			fired := h.fireAt(sched.ID, h.in(time.Hour))
			wantOutcome(t, fired, OutcomeSkipped, c.mention)
			if len(h.ownJobs()) != 0 {
				t.Error("a job was started")
			}
			// Left in place, due again at its next time.
			if fired.NextRunAt == nil || !fired.NextRunAt.Equal(h.in(25*time.Hour)) || fired.State != StateActive {
				t.Errorf("next = %v, state = %s; want tomorrow, active", fired.NextRunAt, fired.State)
			}
			if got := h.activityContaining("schedule.skipped"); len(got) != 1 || !strings.Contains(got[0], c.mention) || !strings.Contains(got[0], "["+h.owner.Name+"]") {
				t.Errorf("activity = %v, want one skipped entry for the owner with the reason", got)
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			if len(h.audit) == 0 || h.audit[len(h.audit)-1].Action != "schedule.skipped" {
				t.Errorf("audit = %+v, want the skipped firing", h.audit)
			}
		})
	}
}

func TestFiring_OwnersScopeNarrowsAQuery(t *testing.T) {
	h := newHarness(t)
	h.matches("web1", "web2", "db1")
	sched := h.create(Input{Name: "q", Kind: orchestrator.JobKindRun, Query: "nodes[certname] { }", StartAt: h.in(time.Hour)})

	h.authority[h.owner.Name] = nodescope.Of("web1", "web2")
	f := wantOutcome(t, h.fireAt(sched.ID, h.in(time.Hour)), OutcomeStarted, "")
	if got := targetNames(h.job(*f.JobID)); !reflect.DeepEqual(got, []string{"web1", "web2"}) {
		t.Errorf("targets = %v, want the matching nodes still in the owner's scope", got)
	}
}

func TestFiring_SkippedWhileThePreviousJobIsRunning(t *testing.T) {
	h := newHarness(t)
	sched := h.create(Input{Name: "overlapping", Kind: orchestrator.JobKindRun, Targets: []string{"web1"},
		StartAt: h.in(time.Hour), Repeat: &Repeat{1, UnitHour}})

	h.hold()
	first := wantOutcome(t, h.fireAt(sched.ID, h.in(time.Hour)), OutcomeStarted, "")
	wantOutcome(t, h.fireAt(sched.ID, h.in(2*time.Hour)), OutcomeSkipped, "still running")
	if len(h.ownJobs()) != 1 {
		t.Errorf("jobs = %d, want no second job", len(h.ownJobs()))
	}

	h.release()
	h.waitFinished(*first.JobID)
	wantOutcome(t, h.fireAt(sched.ID, h.in(3*time.Hour)), OutcomeStarted, "")
}

func TestFiring_LateIsMissed(t *testing.T) {
	h := newHarness(t)

	// A one-off found seven hours late.
	once := h.create(h.runOn("web1"))
	got := h.fireAt(once.ID, h.in(8*time.Hour))
	wantOutcome(t, got, OutcomeMissed, "not started")
	if got.State != StateFinished || len(h.ownJobs()) != 0 {
		t.Errorf("state = %s, jobs = %d; want finished with nothing started", got.State, len(h.ownJobs()))
	}
	if entries := h.activityContaining("schedule.missed"); len(entries) != 1 {
		t.Errorf("activity = %v, want one missed entry", entries)
	}

	// An hourly schedule found four and a half hours after it was due:
	// one missed firing, and back on its grid.
	start := h.now.Add(time.Hour)
	hourly, err := h.svc.Create(context.Background(), Input{Name: "hourly", Kind: orchestrator.JobKindRun, Targets: []string{"web1"},
		StartAt: start, Repeat: &Repeat{1, UnitHour}}, h.caller())
	if err != nil {
		t.Fatal(err)
	}
	got = h.fireAt(hourly.ID, start.Add(4*time.Hour+30*time.Minute))
	wantOutcome(t, got, OutcomeMissed, "not started")
	if len(got.Firings) != 1 || !got.NextRunAt.Equal(start.Add(5*time.Hour)) || len(h.ownJobs()) != 0 {
		t.Errorf("firings = %d, next = %v, jobs = %d; want one missed firing and the next whole hour", len(got.Firings), got.NextRunAt, len(h.ownJobs()))
	}

	// Five minutes late still fires.
	got = h.fireAt(hourly.ID, start.Add(5*time.Hour+5*time.Minute))
	wantOutcome(t, got, OutcomeStarted, "")
}

// --- pausing, resuming, changing ---

func TestPauseAndResume(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	start := h.in(time.Hour)
	daily := h.create(Input{Name: "daily", Kind: orchestrator.JobKindRun, Targets: []string{"web1"}, StartAt: start, Repeat: &Repeat{1, UnitDay}})

	paused, err := h.svc.Pause(ctx, daily.ID, h.caller())
	if err != nil || paused.State != StatePaused {
		t.Fatalf("Pause() = %s, %v", paused.State, err)
	}
	if got := h.fireAt(daily.ID, start.Add(time.Minute)); len(got.Firings) != 0 || len(h.ownJobs()) != 0 {
		t.Error("a paused schedule fired")
	}

	// Resumed a week later: nothing for the days passed, next at its
	// next daily time.
	h.now = start.Add(7*24*time.Hour + time.Hour)
	resumed, err := h.svc.Resume(ctx, daily.ID, h.caller())
	if err != nil || resumed.State != StateActive || !resumed.NextRunAt.Equal(start.Add(8*24*time.Hour)) {
		t.Fatalf("Resume() = %s, next %v, %v; want active, due at the next daily time", resumed.State, resumed.NextRunAt, err)
	}
	if len(resumed.Firings) != 0 || len(h.ownJobs()) != 0 {
		t.Error("resuming started something for the time it was paused")
	}
	if resumed.Owner != h.owner.Name {
		t.Errorf("owner = %s, want it unchanged by pause and resume", resumed.Owner)
	}

	// A one-off whose time passed while paused needs a new start time.
	h.base = h.now
	once := h.create(h.runOn("web1"))
	if _, err := h.svc.Pause(ctx, once.ID, h.caller()); err != nil {
		t.Fatal(err)
	}
	h.now = h.in(2 * time.Hour)
	_, err = h.svc.Resume(ctx, once.ID, h.caller())
	wantField(t, err, "startAt", "new start time")

	for _, action := range []string{"schedule.paused", "schedule.resumed"} {
		if len(h.activityContaining(action)) == 0 {
			t.Errorf("no %s activity entry", action)
		}
	}
}

func TestReplace_TheSaverBecomesTheOwner(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	sched := h.create(h.runOn("web1"))
	admin := h.other(nodescope.Everything())
	h.authority[admin.Owner.Name] = nodescope.Everything()

	change := h.runOn("web1", "web2")
	change.Name = "nightly run, widened"
	saved, err := h.svc.Replace(ctx, sched.ID, change, admin)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Owner != admin.Owner.Name || saved.Name != "nightly run, widened" || len(saved.Targets) != 2 {
		t.Errorf("after another user's save: owner %s, %+v", saved.Owner, saved)
	}
	if got := h.activityContaining("schedule.updated"); len(got) != 1 || !strings.Contains(got[0], "taking it over from "+h.owner.Name) {
		t.Errorf("activity = %v, want the takeover recorded", got)
	}

	// It now fires on the new owner's authority: the old owner losing
	// theirs changes nothing.
	delete(h.authority, h.owner.Name)
	f := wantOutcome(t, h.fireAt(sched.ID, h.in(time.Hour)), OutcomeStarted, "")
	if job := h.job(*f.JobID); job.TriggeredBy != admin.Owner.Name {
		t.Errorf("job triggered by %s, want the new owner", job.TriggeredBy)
	}
}

func TestVisibility_OwnerOrFleetWide(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	sched := h.create(h.runOn("web1"))
	scopedOther := h.other(nodescope.Of("web1"))
	fleetOther := h.other(nodescope.Everything())

	// A scoped caller who is not the owner: not found, for every call,
	// exactly as for an id that does not exist.
	_, missing := h.svc.Get(ctx, 1<<40, scopedOther)
	calls := map[string]func() error{
		"get":     func() error { _, err := h.svc.Get(ctx, sched.ID, scopedOther); return err },
		"replace": func() error { _, err := h.svc.Replace(ctx, sched.ID, h.runOn("web1"), scopedOther); return err },
		"delete":  func() error { return h.svc.Delete(ctx, sched.ID, scopedOther) },
		"pause":   func() error { _, err := h.svc.Pause(ctx, sched.ID, scopedOther); return err },
		"resume":  func() error { _, err := h.svc.Resume(ctx, sched.ID, scopedOther); return err },
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, ErrNotFound) || err.Error() != missing.Error() {
			t.Errorf("%s by a scoped non-owner = %v, want the not-found of a missing schedule", name, err)
		}
	}
	if got := h.get(sched.ID); got.Owner != h.owner.Name || !got.Enabled {
		t.Errorf("schedule changed by refused calls: %+v", got)
	}
	if listed, _ := h.svc.List(ctx, scopedOther); len(listed) != 0 {
		t.Errorf("a scoped non-owner lists %d schedules, want none", len(listed))
	}

	// The owner sees it even when scoped; a fleet-wide caller sees it.
	if listed, _ := h.svc.List(ctx, Caller{Owner: h.owner, Scope: nodescope.Of("web1")}); len(listed) != 1 {
		t.Errorf("the scoped owner lists %d schedules, want their own", len(listed))
	}
	if _, err := h.svc.Get(ctx, sched.ID, fleetOther); err != nil {
		t.Errorf("fleet-wide caller: %v, want the schedule", err)
	}
	// A service token of the same name is a different owner.
	token := Caller{Owner: Owner{Name: h.owner.Name, Kind: OwnerServiceToken}, Scope: nodescope.Of("web1"), Tasks: taskscope.Unrestricted()}
	if _, err := h.svc.Get(ctx, sched.ID, token); !errors.Is(err, ErrNotFound) {
		t.Errorf("a service token sharing the owner's name = %v, want not found", err)
	}
}

func TestFirings_ListedNewestFirstAndTrimmed(t *testing.T) {
	h := newHarness(t)
	h.matches("web1")
	start := h.in(time.Hour)
	sched := h.create(Input{Name: "hourly", Kind: orchestrator.JobKindRun, Query: "nodes[certname] { }", StartAt: start, Repeat: &Repeat{1, UnitHour}})

	wantOutcome(t, h.fireAt(sched.ID, start), OutcomeStarted, "")
	h.waitFinished(*h.get(sched.ID).Firings[0].JobID)
	h.matches()
	wantOutcome(t, h.fireAt(sched.ID, start.Add(time.Hour)), OutcomeSkipped, "no nodes matched")
	h.matches("web1")
	got := h.fireAt(sched.ID, start.Add(2*time.Hour))

	if len(got.Firings) != 3 {
		t.Fatalf("firings = %d, want 3", len(got.Firings))
	}
	outcomes := []string{got.Firings[0].Outcome, got.Firings[1].Outcome, got.Firings[2].Outcome}
	if !reflect.DeepEqual(outcomes, []string{OutcomeStarted, OutcomeSkipped, OutcomeStarted}) {
		t.Errorf("outcomes newest first = %v", outcomes)
	}
	if got.Firings[0].JobID == nil || got.Firings[2].JobID == nil || got.Firings[1].JobID != nil || got.Firings[1].Detail == "" {
		t.Errorf("firings = %+v; want the started ones linked to jobs and the skipped one explained", got.Firings)
	}

	listed, err := h.svc.List(context.Background(), Caller{Owner: h.owner})
	if err != nil || len(listed) != 1 || listed[0].LastFiring == nil || listed[0].LastFiring.ID != got.Firings[0].ID {
		t.Errorf("listed = %+v, %v; want the schedule with its last firing", listed, err)
	}

	// Only the newest hundred are kept.
	if _, err := h.store.pool.Exec(context.Background(), `
		INSERT INTO scheduled_job_firings (schedule_id, due_at, outcome)
		SELECT $1, now(), 'skipped' FROM generate_series(1, 120)`, sched.ID); err != nil {
		t.Fatal(err)
	}
	h.waitFinished(*got.Firings[0].JobID)
	h.fireAt(sched.ID, start.Add(3*time.Hour))
	var kept int
	_ = h.store.pool.QueryRow(context.Background(), `SELECT count(*) FROM scheduled_job_firings WHERE schedule_id = $1`, sched.ID).Scan(&kept)
	if kept != maxFirings {
		t.Errorf("%d firings kept, want %d", kept, maxFirings)
	}
}
