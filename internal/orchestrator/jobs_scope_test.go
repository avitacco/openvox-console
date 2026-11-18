package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

// jobScopeFixture is three jobs and a plan by one unique actor, on two
// uniquely named nodes: "web" is in the caller's scope, "db" is not.
type jobScopeFixture struct {
	store              *Store
	actor, web, db     string
	webOnly, dbOnly    Job
	both, plan, dbPlan Job
}

func newJobScopeFixture(t *testing.T) *jobScopeFixture {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	f := &jobScopeFixture{
		store: s,
		actor: "job-scope-" + tag,
		web:   "web-" + tag + ".example.com",
		db:    "db-" + tag + ".example.com",
	}
	run := func(targets ...string) Job {
		job, err := s.CreateJob(ctx, JobKindRun, "", "", nil, targets, f.actor)
		if err != nil {
			t.Fatalf("CreateJob() error: %v", err)
		}
		return job
	}
	f.webOnly, f.dbOnly, f.both = run(f.web), run(f.db), run(f.web, f.db)

	var err error
	if f.plan, err = s.CreatePlan(ctx, "mixed-plan", []PlanStep{{Kind: JobKindRun}}, []string{f.web, f.db}, f.actor); err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}
	if f.dbPlan, err = s.CreatePlan(ctx, "db-plan", []PlanStep{{Kind: JobKindRun}}, []string{f.db}, f.actor); err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}
	return f
}

func (f *jobScopeFixture) list(t *testing.T, certnames []string) (map[int64]Job, int) {
	t.Helper()
	jobs, total, err := f.store.ListJobs(context.Background(), 1, 100, JobFilter{TriggeredBy: f.actor, Certnames: certnames})
	if err != nil {
		t.Fatalf("ListJobs() error: %v", err)
	}
	byID := map[int64]Job{}
	for _, j := range jobs {
		byID[j.ID] = j
	}
	return byID, total
}

func TestListJobs_NodeScope(t *testing.T) {
	f := newJobScopeFixture(t)

	// Unrestricted: three runs and two plans. A plan's steps are read
	// through the plan, not listed beside it.
	if _, total := f.list(t, nil); total != 5 {
		t.Fatalf("unrestricted total = %d, want 5", total)
	}

	jobs, total := f.list(t, []string{f.web})
	// webOnly, both and the mixed plan.
	if total != 3 || len(jobs) != 3 {
		t.Fatalf("scoped total = %d (%d listed), want 3: %v", total, len(jobs), jobs)
	}
	if _, ok := jobs[f.dbOnly.ID]; ok {
		t.Error("a job that ran only outside scope was listed")
	}
	if _, ok := jobs[f.dbPlan.ID]; ok {
		t.Error("a plan whose steps ran only outside scope was listed")
	}
	if _, ok := jobs[f.plan.ID]; !ok {
		t.Error("a plan with a step in scope should be listed")
	}
	spanning := jobs[f.both.ID]
	if spanning.TargetCount != 1 || len(spanning.TargetPreview) != 1 || spanning.TargetPreview[0] != f.web {
		t.Errorf("spanning job count %d preview %v, want 1 and only the in-scope node", spanning.TargetCount, spanning.TargetPreview)
	}

	// Paging and totals agree for a scoped caller.
	page, total, err := f.store.ListJobs(context.Background(), 2, 2, JobFilter{TriggeredBy: f.actor, Certnames: []string{f.web}})
	if err != nil || total != 3 || len(page) != 1 {
		t.Errorf("page 2 of 2: %d jobs, total %d, err %v; want 1 and 3", len(page), total, err)
	}

	// An empty scope matches nothing - never everything.
	if _, total := f.list(t, []string{}); total != 0 {
		t.Errorf("empty scope total = %d, want 0", total)
	}
}

func TestGetJobInScope(t *testing.T) {
	f := newJobScopeFixture(t)
	ctx := context.Background()
	scope := []string{f.web}

	inside, err := f.store.GetJobInScope(ctx, f.webOnly.ID, scope)
	if err != nil || len(inside.Targets) != 1 || inside.TargetsHidden {
		t.Errorf("job inside scope: %d targets, hidden %v, err %v; want 1, not hidden", len(inside.Targets), inside.TargetsHidden, err)
	}

	if _, err := f.store.GetJobInScope(ctx, f.dbOnly.ID, scope); !errors.Is(err, ErrNotFound) {
		t.Errorf("job outside scope: err = %v, want ErrNotFound", err)
	}

	spanning, err := f.store.GetJobInScope(ctx, f.both.ID, scope)
	if err != nil || len(spanning.Targets) != 1 || spanning.Targets[0].Certname != f.web || !spanning.TargetsHidden {
		t.Errorf("spanning job: targets %+v, hidden %v, err %v; want only the in-scope node, flagged", spanning.Targets, spanning.TargetsHidden, err)
	}

	plan, err := f.store.GetJobInScope(ctx, f.plan.ID, scope)
	if err != nil || !plan.TargetsHidden {
		t.Errorf("mixed plan: hidden %v, err %v; want found and flagged", plan.TargetsHidden, err)
	}
	if len(plan.Steps) != 1 || len(plan.Steps[0].Targets) != 1 || plan.Steps[0].Targets[0].Certname != f.web {
		t.Errorf("mixed plan steps = %+v, want one step showing only the in-scope node", plan.Steps)
	}
	if _, err := f.store.GetJobInScope(ctx, f.dbPlan.ID, scope); !errors.Is(err, ErrNotFound) {
		t.Errorf("plan outside scope: err = %v, want ErrNotFound", err)
	}

	// No restriction is exactly GetJob.
	all, err := f.store.GetJobInScope(ctx, f.both.ID, nil)
	if err != nil || len(all.Targets) != 2 || all.TargetsHidden {
		t.Errorf("unrestricted: %d targets, hidden %v, err %v; want both, not flagged", len(all.Targets), all.TargetsHidden, err)
	}
}

func TestJobHandlers_NodeScope(t *testing.T) {
	f := newJobScopeFixture(t)
	h := NewHandlers(f.store, nil, noopActor, noopRecordAuditRead)
	h.SetScope(scopedTo(f.web))

	get := func(id int64) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetPathValue("id", strconv.FormatInt(id, 10))
		rec := httptest.NewRecorder()
		h.getJob(rec, req)
		return rec
	}

	// A job outside scope answers exactly as a job that does not exist.
	hidden, missing := get(f.dbOnly.ID), get(1<<60)
	if hidden.Code != http.StatusNotFound || hidden.Body.String() != missing.Body.String() {
		t.Errorf("hidden job: %d %q; missing job: %d %q", hidden.Code, hidden.Body, missing.Code, missing.Body)
	}

	var spanning Job
	if err := json.Unmarshal(get(f.both.ID).Body.Bytes(), &spanning); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(spanning.Targets) != 1 || spanning.Targets[0].Certname != f.web || !spanning.TargetsHidden {
		t.Errorf("spanning job = %+v, want one in-scope target and targetsHidden", spanning)
	}

	rec := httptest.NewRecorder()
	h.listJobs(rec, httptest.NewRequest(http.MethodGet, "/?pageSize=100&triggeredBy="+f.actor, nil))
	var page pagination.Page[Job]
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if page.Total != 3 {
		t.Errorf("scoped list total = %d, want 3", page.Total)
	}
	for _, j := range page.Items {
		for _, certname := range j.TargetPreview {
			if certname == f.db {
				t.Errorf("job %d previews the out-of-scope node", j.ID)
			}
		}
	}

	// Fleet-wide: unchanged.
	h.SetScope(func(*http.Request) (nodescope.Scope, error) { return nodescope.Everything(), nil })
	rec = httptest.NewRecorder()
	h.listJobs(rec, httptest.NewRequest(http.MethodGet, "/?pageSize=100&triggeredBy="+f.actor, nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if page.Total != 5 {
		t.Errorf("fleet-wide list total = %d, want 5", page.Total)
	}
	if rec := get(f.dbOnly.ID); rec.Code != http.StatusOK {
		t.Errorf("fleet-wide get = %d, want 200", rec.Code)
	}

	// An unknown scope fails the request.
	h.SetScope(func(*http.Request) (nodescope.Scope, error) { return nodescope.Scope{}, fmt.Errorf("openvoxdb down") })
	if rec := get(f.webOnly.ID); rec.Code != http.StatusBadGateway {
		t.Errorf("scope error on get = %d, want 502", rec.Code)
	}
}
