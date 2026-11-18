package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/sealer"
	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

// fakeTransport answers every dispatch as a node that ran it
// successfully - or, while hold is set, not at all, so a job stays
// running.
type fakeTransport struct {
	mu       sync.Mutex
	payloads []string
	hold     chan struct{}
}

func (f *fakeTransport) Dispatch(ctx context.Context, _ string, payload []byte, _ time.Duration) ([]byte, error) {
	f.mu.Lock()
	f.payloads = append(f.payloads, string(payload))
	hold := f.hold
	f.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return []byte(`{"result":{"output":{"stdout":"ok","stderr":"","exitcode":0},"result":{}}}`), nil
}

func (f *fakeTransport) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.payloads...)
}

// fakeCatalog has one plain task and one with a sensitive parameter.
// removed takes a task out of the deployed code.
type fakeCatalog struct {
	mu      sync.Mutex
	removed map[string]bool
}

func (c *fakeCatalog) remove(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.removed == nil {
		c.removed = map[string]bool{}
	}
	c.removed[name] = true
}

func (c *fakeCatalog) Task(_ context.Context, environment, name string) (taskcatalog.Task, error) {
	c.mu.Lock()
	gone := c.removed[name]
	c.mu.Unlock()
	if environment != "production" {
		return taskcatalog.Task{}, &taskcatalog.NotFoundError{Kind: "environment", Name: environment}
	}
	var file taskcatalog.File
	file.Filename = "task.sh"
	file.SHA256 = strings.Repeat("a", 64)
	file.SizeBytes = 10
	file.URI.Path = "/puppet/v3/file_content/tasks/mod/task.sh"
	file.URI.Params = map[string]string{"environment": "production"}

	switch {
	case gone:
	case name == "mod::echo":
		return taskcatalog.Task{Name: name, Environment: environment, Files: []taskcatalog.File{file},
			Metadata: taskcatalog.Metadata{Parameters: map[string]taskcatalog.Parameter{
				"message": {Type: "Optional[String]"},
				"count":   {Type: "Integer", Default: json.RawMessage(`1`)},
			}}}, nil
	case name == "mod::secret":
		return taskcatalog.Task{Name: name, Environment: environment, Files: []taskcatalog.File{file},
			Metadata: taskcatalog.Metadata{Parameters: map[string]taskcatalog.Parameter{
				"user":     {Type: "String"},
				"password": {Type: "String[1]", Sensitive: true},
			}}}, nil
	}
	return taskcatalog.Task{}, &taskcatalog.NotFoundError{Kind: "task", Name: name}
}

type environments []string

func (e environments) Environments(context.Context) ([]string, error) { return e, nil }

// harness is a Service over a real Postgres store and a real
// orchestrator (store, dispatcher, starter) with a fake fleet, task
// catalogue, transport and clock.
type harness struct {
	t         *testing.T
	svc       *Service
	store     *Store
	jobs      *orchestrator.Store
	transport *fakeTransport
	catalog   *fakeCatalog

	// owner is unique to the test, so its schedules and jobs can be
	// told from everything else in the shared database.
	owner Owner
	// base is when the test began; now is the clock the service reads,
	// which fireAt moves.
	base time.Time
	now  time.Time

	mu        sync.Mutex
	queryRows []string
	queryErr  error
	groups    map[int64][]string
	authority map[string]nodescope.Scope // by owner name; absent = no authority
	tasks     map[string]taskscope.Scope // by owner name; absent = every task
	activity  []string
	audit     []auditlog.Event
}

func testKey() []byte { return []byte(strings.Repeat("k", sealer.KeySize)) }

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := testdb.Pool(t)
	h := &harness{
		t: t, store: NewStore(pool), jobs: orchestrator.NewStore(pool),
		transport: &fakeTransport{}, catalog: &fakeCatalog{},
		owner:     Owner{Name: "sched-test-" + time.Now().Format("150405.000000000"), Kind: OwnerUser},
		base:      time.Now().UTC().Truncate(time.Second),
		groups:    map[int64][]string{},
		authority: map[string]nodescope.Scope{},
		tasks:     map[string]taskscope.Scope{},
	}
	h.now = h.base
	h.authority[h.owner.Name] = nodescope.Everything()

	dispatcher := orchestrator.NewDispatcher(h.jobs, h.transport, nil)
	dispatcher.SetActivityRecorder(func(action, actor, summary string) { h.note(action + " " + summary) })
	ctx, cancel := context.WithCancel(context.Background())
	go dispatcher.Run(ctx)

	handlers := orchestrator.NewHandlers(h.jobs, dispatcher, func(*http.Request) string { return h.owner.Name }, func(*http.Request, auditlog.Event) {})
	handlers.SetTaskCatalog(h.catalog)
	handlers.SetEnvironments(environments{"production", "staging"})
	handlers.SetTargetSources(
		func(context.Context, string) ([]map[string]any, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			rows := make([]map[string]any, len(h.queryRows))
			for i, c := range h.queryRows {
				rows[i] = map[string]any{"certname": c}
			}
			return rows, h.queryErr
		},
		func(_ context.Context, id int64) (string, []string, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			certnames, ok := h.groups[id]
			if !ok {
				return "", nil, orchestrator.ErrGroupNotFound
			}
			return "Web Servers", certnames, nil
		})

	key, err := sealer.New(testKey())
	if err != nil {
		t.Fatal(err)
	}
	h.svc = NewService(h.store, handlers.Starter(), func(_ context.Context, owner Owner) (Authorization, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		scope, ok := h.authority[owner.Name]
		if !ok || owner.Kind != OwnerUser {
			return Authorization{}, ErrNoAuthority
		}
		tasks, limited := h.tasks[owner.Name]
		if !limited {
			tasks = taskscope.Unrestricted()
		}
		return Authorization{Scope: scope, Tasks: tasks}, nil
	}, key, nil)
	h.svc.now = func(context.Context) (time.Time, error) { return h.now, nil }
	h.svc.SetRecorders(func(action, actor, summary string) { h.note(action + " [" + actor + "] " + summary) },
		func(e auditlog.Event) {
			h.mu.Lock()
			h.audit = append(h.audit, e)
			h.mu.Unlock()
		})

	t.Cleanup(func() {
		// Let anything held finish, then remove what the test made: a
		// due schedule left in the shared database would be picked up
		// by a console run against it.
		h.release()
		_, _ = pool.Exec(context.Background(), `DELETE FROM scheduled_jobs WHERE owner LIKE $1`, h.owner.Name+"%")
		cancel()
	})
	return h
}

func (h *harness) note(entry string) {
	h.mu.Lock()
	h.activity = append(h.activity, entry)
	h.mu.Unlock()
}

func (h *harness) activityContaining(substr string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var found []string
	for _, a := range h.activity {
		if strings.Contains(a, substr) {
			found = append(found, a)
		}
	}
	return found
}

// hold makes dispatched jobs stay running until release.
func (h *harness) hold() {
	h.transport.mu.Lock()
	h.transport.hold = make(chan struct{})
	h.transport.mu.Unlock()
}

func (h *harness) release() {
	h.transport.mu.Lock()
	if h.transport.hold != nil {
		close(h.transport.hold)
		h.transport.hold = nil
	}
	h.transport.mu.Unlock()
}

func (h *harness) matches(certnames ...string) {
	h.mu.Lock()
	h.queryRows = certnames
	h.mu.Unlock()
}

func (h *harness) caller() Caller {
	return Caller{Owner: h.owner, Scope: nodescope.Everything(), Tasks: taskscope.Unrestricted()}
}

// other is a second caller, limited to web1 unless given a scope.
func (h *harness) other(scope nodescope.Scope) Caller {
	return Caller{Owner: Owner{Name: h.owner.Name + "-other", Kind: OwnerUser}, Scope: scope, Tasks: taskscope.Unrestricted()}
}

// in is d after the test began, wherever the clock has been moved.
func (h *harness) in(d time.Duration) time.Time { return h.base.Add(d) }

// runOn is a one-off Puppet run on named nodes, an hour from now.
func (h *harness) runOn(nodes ...string) Input {
	return Input{Name: "nightly run", Kind: orchestrator.JobKindRun, Targets: nodes, StartAt: h.in(time.Hour)}
}

func (h *harness) create(in Input) Schedule {
	h.t.Helper()
	sched, err := h.svc.Create(context.Background(), in, h.caller())
	if err != nil {
		h.t.Fatalf("Create() error: %v", err)
	}
	return sched
}

func (h *harness) get(id int64) Schedule {
	h.t.Helper()
	sched, err := h.store.Get(context.Background(), id)
	if err != nil {
		h.t.Fatalf("Get(%d) error: %v", id, err)
	}
	return sched
}

// fireAt moves the clock to at and fires schedule id if it is due, as a
// tick would - but only this schedule, since the database is shared.
// It returns the schedule as it then stands.
func (h *harness) fireAt(id int64, at time.Time) Schedule {
	h.t.Helper()
	h.now = at
	sched := h.get(id)
	if sched.Enabled && sched.NextRunAt != nil && !sched.NextRunAt.After(at) {
		if err := h.svc.fire(context.Background(), sched, at); err != nil {
			h.t.Fatalf("fire() error: %v", err)
		}
	}
	return h.get(id)
}

func (h *harness) ownJobs() []orchestrator.Job {
	h.t.Helper()
	jobs, _, err := h.jobs.ListJobs(context.Background(), 1, 100, orchestrator.JobFilter{TriggeredBy: h.owner.Name, SortAsc: true})
	if err != nil {
		h.t.Fatal(err)
	}
	return jobs
}

func (h *harness) job(id int64) orchestrator.Job {
	h.t.Helper()
	job, err := h.jobs.GetJob(context.Background(), id)
	if err != nil {
		h.t.Fatal(err)
	}
	return job
}

func (h *harness) waitFinished(id int64) orchestrator.Job {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if job := h.job(id); job.Status != orchestrator.StatusRunning {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("job %d still running", id)
	return orchestrator.Job{}
}

func targetNames(job orchestrator.Job) []string {
	names := make([]string, len(job.Targets))
	for i, target := range job.Targets {
		names[i] = target.Certname
	}
	return names
}

func lastFiring(t *testing.T, sched Schedule) Firing {
	t.Helper()
	if len(sched.Firings) == 0 {
		t.Fatalf("schedule %d has no firings", sched.ID)
	}
	return sched.Firings[0]
}

func wantOutcome(t *testing.T, sched Schedule, outcome, mention string) Firing {
	t.Helper()
	f := lastFiring(t, sched)
	if f.Outcome != outcome || !strings.Contains(f.Detail, mention) {
		t.Fatalf("last firing = %s %q; want %s mentioning %q", f.Outcome, f.Detail, outcome, mention)
	}
	return f
}

func wantField(t *testing.T, err error, field, mention string) {
	t.Helper()
	var invalid *InvalidScheduleError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want an InvalidScheduleError", err)
	}
	if invalid.Field != field || !strings.Contains(invalid.Reason, mention) {
		t.Errorf("refused on %q: %q; want %q mentioning %q", invalid.Field, invalid.Reason, field, mention)
	}
}

// openvoxdbRejected is openvoxdb refusing a query it could not parse.
var openvoxdbRejected = openvoxdb.InvalidQueryError{Message: "PQL parse error at line 1, column 30"}
