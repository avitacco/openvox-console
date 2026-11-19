package agentrequests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/codeplans"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/sealer"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

// fakeStarter checks and starts nothing real. It records what it was asked
// to start, so a test can say that nothing was started - or exactly what was.
type fakeStarter struct {
	mu        sync.Mutex
	started   []string
	taskReq   orchestrator.TaskRequest
	planReq   orchestrator.PlanRequest
	runReq    orchestrator.RunRequest
	checkErr  error
	sensitive map[string]any
	nextID    atomic.Int64
}

func (f *fakeStarter) Targets() *orchestrator.TargetResolver {
	return orchestrator.NewTargetResolver(nil, nil)
}
func (f *fakeStarter) CheckRun(context.Context, orchestrator.RunRequest) error     { return f.checkErr }
func (f *fakeStarter) CheckPatch(context.Context, orchestrator.PatchRequest) error { return f.checkErr }
func (f *fakeStarter) CheckTask(_ context.Context, r orchestrator.TaskRequest) (orchestrator.TaskCheck, error) {
	if f.checkErr != nil {
		return orchestrator.TaskCheck{}, f.checkErr
	}
	stored := json.RawMessage(`{"user":"deploy","password":"[redacted]"}`)
	return orchestrator.TaskCheck{Environment: "production", StoredParams: stored, Sensitive: f.sensitive}, nil
}
func (f *fakeStarter) CheckPlan(_ context.Context, r orchestrator.PlanRequest) ([]orchestrator.TaskCheck, error) {
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	checks := make([]orchestrator.TaskCheck, len(r.Steps))
	for i, st := range r.Steps {
		if st.Kind == orchestrator.JobKindTask {
			checks[i] = orchestrator.TaskCheck{StoredParams: json.RawMessage(`{"password":"[redacted]"}`), Sensitive: f.sensitive}
		}
	}
	return checks, nil
}
func (f *fakeStarter) start(kind string) (orchestrator.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, kind)
	return orchestrator.Job{ID: 1000 + f.nextID.Add(1), Kind: orchestrator.JobKind(kind)}, nil
}
func (f *fakeStarter) StartRun(_ context.Context, r orchestrator.RunRequest) (orchestrator.Job, error) {
	f.runReq = r
	return f.start("run")
}
func (f *fakeStarter) StartPatch(context.Context, orchestrator.PatchRequest) (orchestrator.Job, error) {
	return f.start("patch")
}
func (f *fakeStarter) StartTask(_ context.Context, r orchestrator.TaskRequest) (orchestrator.Job, error) {
	f.taskReq = r
	return f.start("task")
}
func (f *fakeStarter) StartPlan(_ context.Context, r orchestrator.PlanRequest) (orchestrator.Job, error) {
	f.planReq = r
	return f.start("plan")
}

type fakeCodePlans struct{ started *codeplans.Request }

func (f *fakeCodePlans) Check(_ context.Context, r codeplans.Request) (codeplans.Checked, error) {
	return codeplans.Checked{Parameters: map[string]any{"user": "deploy", "token": "[redacted]"}, Sensitive: map[string]any{"token": "s3cret"}}, nil
}
func (f *fakeCodePlans) Start(_ context.Context, r codeplans.Request) (orchestrator.Job, error) {
	f.started = &r
	return orchestrator.Job{ID: 4242, Kind: orchestrator.JobKindCodePlan}, nil
}

type env struct {
	svc      *Service
	starter  *fakeStarter
	plans    *fakeCodePlans
	caller   Caller
	owner    string
	tokenOK  *atomic.Bool
	approver Approver
	audit    *[]auditlog.Event
	scope    nodescope.Scope
}

func newEnv(t *testing.T, withSealer bool) *env {
	t.Helper()
	pool := testdb.Pool(t)
	ctx := context.Background()
	name := fmt.Sprintf("bot-%d", time.Now().UnixNano())
	var tokenID int64
	if err := pool.QueryRow(ctx, `INSERT INTO agent_tokens (name, jti, owner, permissions, expires_at)
		VALUES ($1, $1, 'owner', '{orchestrator:run}', now() + interval '1 day') RETURNING id`, name).Scan(&tokenID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM agent_tokens WHERE id = $1`, tokenID) })

	var sl *sealer.Sealer
	if withSealer {
		var err error
		if sl, err = sealer.New([]byte("0123456789abcdef0123456789abcdef")); err != nil {
			t.Fatal(err)
		}
	}
	e := &env{starter: &fakeStarter{}, plans: &fakeCodePlans{}, owner: "owner", tokenOK: &atomic.Bool{}, audit: &[]auditlog.Event{}}
	e.tokenOK.Store(true)
	nodeA, nodeB := name+"-a", name+"-b"
	e.scope = nodescope.Of(nodeA, nodeB)
	e.caller = Caller{TokenID: tokenID, TokenName: name, Scope: e.scope, Tasks: taskscope.Unrestricted()}
	e.approver = Approver{Name: "alice", Scope: nodescope.Everything(), Tasks: taskscope.Unrestricted()}
	authority := func(_ context.Context, id int64) (Authorization, error) {
		if !e.tokenOK.Load() {
			return Authorization{}, ErrTokenGone
		}
		return Authorization{Scope: e.scope, Tasks: taskscope.Unrestricted(), Owner: e.owner}, nil
	}
	e.svc = NewService(NewStore(pool), e.starter, e.plans, authority, sl)
	var mu sync.Mutex
	e.svc.SetRecorders(nil, func(ev auditlog.Event) { mu.Lock(); *e.audit = append(*e.audit, ev); mu.Unlock() })
	return e
}

func (e *env) taskInput(nodes ...string) Input {
	if len(nodes) == 0 {
		nodes = e.scope.Certnames()[:1]
	}
	return Input{Kind: orchestrator.JobKindTask, Target: orchestrator.Target{Nodes: nodes}, Task: "pkg::install",
		Params: json.RawMessage(`{"user":"deploy","password":"hunter2"}`)}
}

func TestSubmit_StoresARequestAndStartsNothing(t *testing.T) {
	e := newEnv(t, true)
	e.starter.sensitive = map[string]any{"password": "hunter2"}
	req, err := e.svc.Submit(context.Background(), e.caller, e.taskInput())
	if err != nil {
		t.Fatal(err)
	}
	if req.Status != StatusPending || req.Kind != "task" || len(req.Targets) != 1 || req.JobID != nil {
		t.Errorf("request = %+v", req)
	}
	if len(e.starter.started) != 0 {
		t.Fatalf("submitting started %v", e.starter.started)
	}
	// What is shown holds no secret, and neither does what was audited.
	if strings.Contains(string(req.Body), "hunter2") {
		t.Errorf("body shows the secret: %s", req.Body)
	}
	for _, ev := range *e.audit {
		if strings.Contains(fmt.Sprintf("%+v", ev), "hunter2") {
			t.Errorf("audit holds the secret: %+v", ev)
		}
	}
	listed, _ := e.svc.Store().List(context.Background(), Filter{TokenID: e.caller.TokenID}, 10)
	if len(listed) != 1 || strings.Contains(string(listed[0].Body), "hunter2") {
		t.Errorf("listing = %+v", listed)
	}
	if len(*e.audit) != 1 || (*e.audit)[0].Action != "agent_request.submitted" || (*e.audit)[0].Actor != Actor(e.caller.TokenName) {
		t.Errorf("audit = %+v", *e.audit)
	}
}

func TestSubmit_RefusalsStoreNothing(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()

	e.starter.checkErr = &orchestrator.InvalidTaskError{}
	if _, err := e.svc.Submit(ctx, e.caller, e.taskInput()); err == nil {
		t.Fatal("a refused task was stored")
	}
	e.starter.checkErr = nil

	// A node outside the token's reach is refused as it is for a person.
	_, err := e.svc.Submit(ctx, e.caller, e.taskInput("some-other-node"))
	var outside *orchestrator.OutOfScopeError
	if !errors.As(err, &outside) {
		t.Errorf("out of scope = %v", err)
	}
	if listed, _ := e.svc.Store().List(ctx, Filter{TokenID: e.caller.TokenID}, 10); len(listed) != 0 {
		t.Errorf("refusals left %d requests", len(listed))
	}
}

func TestSubmit_ASensitiveValueNeedsASecretsKey(t *testing.T) {
	e := newEnv(t, false)
	e.starter.sensitive = map[string]any{"password": "hunter2"}
	_, err := e.svc.Submit(context.Background(), e.caller, e.taskInput())
	var refusal *Refusal
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, "CONSOLE_SECRETS_KEY") {
		t.Fatalf("err = %v", err)
	}
	if listed, _ := e.svc.Store().List(context.Background(), Filter{TokenID: e.caller.TokenID}, 10); len(listed) != 0 {
		t.Error("a request with a secret was stored without a key")
	}
}

func TestApprove_RunsExactlyWhatWasFrozenWithItsSecrets(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	e.starter.sensitive = map[string]any{"password": "hunter2"}
	req, err := e.svc.Submit(ctx, e.caller, e.taskInput())
	if err != nil {
		t.Fatal(err)
	}
	done, err := e.svc.Approve(ctx, req.ID, e.approver, "ok")
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != StatusApproved || done.JobID == nil || done.DecidedBy != "alice" || done.Note != "ok" {
		t.Errorf("approved = %+v", done)
	}
	got := e.starter.taskReq
	if got.Task != "pkg::install" || got.Actor != Actor(e.caller.TokenName) || len(got.Target.Nodes) != 1 || got.Target.Nodes[0] != req.Targets[0] {
		t.Errorf("task request = %+v", got)
	}
	if !strings.Contains(string(got.Params), "hunter2") {
		t.Errorf("the sealed value was not put back: %s", got.Params)
	}
	if got.Agent == nil || got.Agent.RequestID != req.ID || got.Agent.ApprovedBy != "alice" || got.Agent.Token != e.caller.TokenName {
		t.Errorf("agent ref = %+v", got.Agent)
	}
	// Approval is audited without the secret.
	last := (*e.audit)[len(*e.audit)-1]
	if last.Action != "agent_request.approved" || last.Actor != "alice" || strings.Contains(fmt.Sprintf("%+v", last), "hunter2") {
		t.Errorf("audit = %+v", last)
	}
	// Deciding again is refused.
	if _, err := e.svc.Approve(ctx, req.ID, e.approver, ""); !errors.Is(err, ErrNotPending) {
		t.Errorf("second approval = %v", err)
	}
}

func TestApprove_Refusals(t *testing.T) {
	ctx := context.Background()

	t.Run("the token's owner", func(t *testing.T) {
		e := newEnv(t, true)
		req, _ := e.svc.Submit(ctx, e.caller, e.taskInput())
		owner := e.approver
		owner.Name = "owner"
		if _, err := e.svc.Approve(ctx, req.ID, owner, ""); err == nil || len(e.starter.started) != 0 {
			t.Errorf("owner approval = %v, started %v", err, e.starter.started)
		}
		if got, _ := e.svc.Store().Get(ctx, req.ID); got.Status != StatusPending {
			t.Errorf("status = %s, want pending", got.Status)
		}
		// But the owner may call it off.
		if rejected, err := e.svc.Reject(ctx, req.ID, owner, "no"); err != nil || rejected.Status != StatusRejected {
			t.Errorf("owner rejection = %v %v", rejected.Status, err)
		}
	})

	t.Run("an approver who cannot reach the nodes", func(t *testing.T) {
		e := newEnv(t, true)
		req, _ := e.svc.Submit(ctx, e.caller, e.taskInput())
		narrow := e.approver
		narrow.Scope = nodescope.Of("somewhere-else")
		if _, err := e.svc.Approve(ctx, req.ID, narrow, ""); err == nil || len(e.starter.started) != 0 {
			t.Errorf("narrow approver = %v, started %v", err, e.starter.started)
		}
		if got, _ := e.svc.Store().Get(ctx, req.ID); got.Status != StatusPending {
			t.Errorf("status = %s, want pending", got.Status)
		}
	})

	t.Run("an approver who may not run the task", func(t *testing.T) {
		e := newEnv(t, true)
		req, _ := e.svc.Submit(ctx, e.caller, e.taskInput())
		limited := e.approver
		limited.Tasks = taskscope.Scope{}
		if _, err := e.svc.Approve(ctx, req.ID, limited, ""); err == nil || len(e.starter.started) != 0 {
			t.Errorf("limited approver = %v", err)
		}
	})

	t.Run("token revoked while waiting", func(t *testing.T) {
		e := newEnv(t, true)
		req, _ := e.svc.Submit(ctx, e.caller, e.taskInput())
		e.tokenOK.Store(false)
		got, err := e.svc.Approve(ctx, req.ID, e.approver, "")
		if err == nil || len(e.starter.started) != 0 {
			t.Fatalf("approval after revoke = %v, started %v", err, e.starter.started)
		}
		if got.Status != StatusFailed || !strings.Contains(got.Failure, "revoked") {
			t.Errorf("request = %s %q", got.Status, got.Failure)
		}
	})

	t.Run("a start that fails", func(t *testing.T) {
		e := newEnv(t, true)
		req, _ := e.svc.Submit(ctx, e.caller, Input{Kind: orchestrator.JobKindRun, Target: orchestrator.Target{Nodes: e.scope.Certnames()[:1]}})
		e.starter.checkErr = nil
		e.starter.started = nil
		// A plan with no code plans configured fails at start.
		bad, _ := e.svc.Submit(ctx, e.caller, Input{Kind: orchestrator.JobKindCodePlan, Plan: "a::b"})
		e.svc.codePlans = nil
		got, err := e.svc.Approve(ctx, bad.ID, e.approver, "")
		if err == nil || got.Status != StatusFailed {
			t.Errorf("failed start = %v %s", err, got.Status)
		}
		_ = req
	})
}

func TestApprove_TwoApproversStartOneJob(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	req, _ := e.svc.Submit(ctx, e.caller, e.taskInput())
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.svc.Approve(ctx, req.ID, e.approver, ""); err == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 || len(e.starter.started) != 1 {
		t.Errorf("approvals won = %d, jobs started = %d, want 1 and 1", wins.Load(), len(e.starter.started))
	}
}

func TestRejectAndExpiry(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	req, _ := e.svc.Submit(ctx, e.caller, e.taskInput())
	rejected, err := e.svc.Reject(ctx, req.ID, e.approver, "not now")
	if err != nil || rejected.Status != StatusRejected || rejected.Note != "not now" {
		t.Fatalf("reject = %+v %v", rejected, err)
	}
	if _, err := e.svc.Approve(ctx, req.ID, e.approver, ""); !errors.Is(err, ErrNotPending) || len(e.starter.started) != 0 {
		t.Errorf("approving a rejected request = %v", err)
	}

	// A request past its time cannot be approved, even before the sweep
	// has marked it, and the sweep then records it.
	e.svc.SetLimits(time.Second, 0)
	old, _ := e.svc.Submit(ctx, e.caller, e.taskInput())
	if _, err := e.svc.store.pool.Exec(ctx, `UPDATE agent_requests SET expires_at = now() - interval '1 minute' WHERE id = $1`, old.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.svc.Store().Get(ctx, old.ID); got.Status != StatusExpired {
		t.Errorf("an overdue request reads %s", got.Status)
	}
	if _, err := e.svc.Approve(ctx, old.ID, e.approver, ""); !errors.Is(err, ErrNotPending) {
		t.Errorf("approving an expired request = %v", err)
	}
	if n, err := e.svc.Expire(ctx); err != nil || n < 1 {
		t.Errorf("Expire = %d %v", n, err)
	}
	if got, _ := e.svc.Store().Get(ctx, old.ID); got.Status != StatusExpired {
		t.Errorf("after the sweep: %s", got.Status)
	}
}

func TestSubmit_ATokenHoldsAtMostSoManyPendingRequests(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	e.svc.SetLimits(0, 2)
	for i := 0; i < 2; i++ {
		if _, err := e.svc.Submit(ctx, e.caller, e.taskInput()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.svc.Submit(ctx, e.caller, e.taskInput()); err == nil || !strings.Contains(err.Error(), "waiting") {
		t.Errorf("third request = %v", err)
	}
}

func TestPlansAndCodePlans(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	e.starter.sensitive = map[string]any{"password": "hunter2"}
	nodes := e.scope.Certnames()

	plan, err := e.svc.Submit(ctx, e.caller, Input{Kind: orchestrator.JobKindPlan, Name: "two steps", Target: orchestrator.Target{Nodes: nodes},
		Steps: []orchestrator.PlanStep{{Kind: orchestrator.JobKindRun}, {Kind: orchestrator.JobKindTask, TaskName: "pkg::install", Params: json.RawMessage(`{"password":"hunter2"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plan.Body), "hunter2") {
		t.Errorf("plan body shows the secret: %s", plan.Body)
	}
	if _, err := e.svc.Approve(ctx, plan.ID, e.approver, ""); err != nil {
		t.Fatal(err)
	}
	if got := e.starter.planReq.Steps[1].Params; !strings.Contains(string(got), "hunter2") {
		t.Errorf("the plan step's secret was not put back: %s", got)
	}

	cp, err := e.svc.Submit(ctx, e.caller, Input{Kind: orchestrator.JobKindCodePlan, Plan: "ops::demo", Parameters: map[string]any{"user": "deploy", "token": "s3cret"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cp.Body), "s3cret") {
		t.Errorf("code plan body shows the secret: %s", cp.Body)
	}
	// An approver who cannot reach everything the agent can is refused.
	narrow := e.approver
	narrow.Scope = nodescope.Of(nodes[0])
	if _, err := e.svc.Approve(ctx, cp.ID, narrow, ""); err == nil {
		t.Error("a narrower approver approved a code plan")
	}
	if _, err := e.svc.Approve(ctx, cp.ID, e.approver, ""); err != nil {
		t.Fatal(err)
	}
	if e.plans.started == nil || e.plans.started.Parameters["token"] != "s3cret" || e.plans.started.Start.Agent == nil {
		t.Errorf("code plan start = %+v", e.plans.started)
	}
}
