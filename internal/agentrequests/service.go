package agentrequests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/codeplans"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/sealer"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// Actor is how an agent token is recorded as the one acting: a job's
// "triggered by", an audit entry's actor.
func Actor(tokenName string) string { return "agent:" + tokenName }

// Defaults for the limits on requests; each can be set on the Service.
const (
	DefaultTTL        = time.Hour
	DefaultMaxPending = 20
)

// Starter is the part of *orchestrator.Starter requests use: the same
// checks and the same starts a person's request goes through.
type Starter interface {
	CheckRun(ctx context.Context, req orchestrator.RunRequest) error
	CheckTask(ctx context.Context, req orchestrator.TaskRequest) (orchestrator.TaskCheck, error)
	CheckPlan(ctx context.Context, req orchestrator.PlanRequest) ([]orchestrator.TaskCheck, error)
	CheckPatch(ctx context.Context, req orchestrator.PatchRequest) error
	StartRun(ctx context.Context, req orchestrator.RunRequest) (orchestrator.Job, error)
	StartTask(ctx context.Context, req orchestrator.TaskRequest) (orchestrator.Job, error)
	StartPlan(ctx context.Context, req orchestrator.PlanRequest) (orchestrator.Job, error)
	StartPatch(ctx context.Context, req orchestrator.PatchRequest) (orchestrator.Job, error)
	Targets() *orchestrator.TargetResolver
}

// CodePlans is the part of *codeplans.Coordinator requests use.
type CodePlans interface {
	Check(ctx context.Context, req codeplans.Request) (codeplans.Checked, error)
	Start(ctx context.Context, req codeplans.Request) (orchestrator.Job, error)
}

// Authorization is what an agent token may do at the moment its request is
// approved, as the token stands then.
type Authorization struct {
	Scope nodescope.Scope
	Tasks taskscope.Scope
	// Owner is the user who issued the token, who may not approve its
	// requests.
	Owner string
}

// ErrTokenGone is what an Authority returns for a token that was revoked or
// has expired.
var ErrTokenGone = errors.New("the agent token has been revoked or has expired")

// Authority answers what the agent token tokenID may do now, or
// ErrTokenGone.
type Authority func(ctx context.Context, tokenID int64) (Authorization, error)

// Caller is an agent making a request: its token and what the token may
// reach.
type Caller struct {
	TokenID   int64
	TokenName string
	Scope     nodescope.Scope
	Tasks     taskscope.Scope
}

// Approver is a person deciding a request: who they are and what their own
// roles let them run.
type Approver struct {
	Name  string
	Scope nodescope.Scope
	Tasks taskscope.Scope
}

// Input is a request as an agent writes it. Kind decides which of the other
// fields count.
type Input struct {
	Kind   orchestrator.JobKind `json:"kind"`
	Target orchestrator.Target  `json:"target"`

	// Task requests.
	Task           string          `json:"task,omitempty"`
	Environment    string          `json:"environment,omitempty"`
	Params         json.RawMessage `json:"params,omitempty"`
	TimeoutSeconds int             `json:"timeoutSeconds,omitempty"`

	// Run and patch requests.
	Options json.RawMessage `json:"options,omitempty"`

	// Plan requests.
	Name  string                  `json:"name,omitempty"`
	Steps []orchestrator.PlanStep `json:"steps,omitempty"`

	// Code plan requests.
	Plan       string         `json:"plan,omitempty"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// Refusal is a request that was not accepted or not carried out, for a
// reason to show the agent or the approver.
type Refusal struct{ Reason string }

func (e *Refusal) Error() string { return e.Reason }

// Service is the rules of requests: what is stored, who may decide, and
// what happens on approval.
type Service struct {
	store      *Store
	starter    Starter
	codePlans  CodePlans
	authority  Authority
	sealer     *sealer.Sealer
	ttl        time.Duration
	maxPending int

	recordActivity func(action, actor, summary string)
	recordAudit    func(auditlog.Event)
}

// NewService builds a Service. codePlans and sealer may be nil: without
// code plans such requests are refused, and without a secrets key a request
// carrying a sensitive value is.
func NewService(store *Store, starter Starter, codePlans CodePlans, authority Authority, sealer *sealer.Sealer) *Service {
	return &Service{store: store, starter: starter, codePlans: codePlans, authority: authority, sealer: sealer,
		ttl: DefaultTTL, maxPending: DefaultMaxPending}
}

// SetLimits sets how long a request waits for a decision and how many one
// token may have waiting; a zero keeps the default.
func (s *Service) SetLimits(ttl time.Duration, maxPending int) {
	if ttl > 0 {
		s.ttl = ttl
	}
	if maxPending > 0 {
		s.maxPending = maxPending
	}
}

// SetRecorders sets where the activity log and the audit log hear of what
// happens to requests.
func (s *Service) SetRecorders(activity func(action, actor, summary string), audit func(auditlog.Event)) {
	s.recordActivity, s.recordAudit = activity, audit
}

// Store returns the requests' store, for the handlers that list them.
func (s *Service) Store() *Store { return s.store }

func (s *Service) record(action, actor string, r Request, summary string, after any) {
	if s.recordActivity != nil {
		s.recordActivity(action, actor, summary)
	}
	if s.recordAudit != nil {
		s.recordAudit(auditlog.Event{Action: action, Actor: actor, ResourceType: "agent_request", ResourceID: strconv.FormatInt(r.ID, 10), After: after})
	}
}

func sealBinding(id int64) []byte { return []byte("agent_request:" + strconv.FormatInt(id, 10)) }

// stored is the sensitive part of a request, as it is sealed.
type sensitiveValues struct {
	Params map[string]any            `json:"params,omitempty"`
	Steps  map[string]map[string]any `json:"steps,omitempty"`
}

func (v sensitiveValues) empty() bool { return len(v.Params) == 0 && len(v.Steps) == 0 }

func (s *Service) sealFunc(v sensitiveValues) (SealFunc, error) {
	if v.empty() {
		return nil, nil
	}
	if s.sealer == nil {
		return nil, &Refusal{Reason: "this request has a sensitive parameter, and the console can hold one only when it has a secrets key (CONSOLE_SECRETS_KEY); ask an administrator to configure it"}
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode sensitive parameters: %w", err)
	}
	return func(id int64) ([]byte, error) { return s.sealer.Seal(encoded, sealBinding(id)) }, nil
}

func (s *Service) openSealed(ctx context.Context, id int64) (sensitiveValues, error) {
	var v sensitiveValues
	sealed, err := s.store.sealedOf(ctx, id)
	if err != nil || len(sealed) == 0 {
		return v, err
	}
	if s.sealer == nil {
		return v, errors.New("the console has no secrets key (CONSOLE_SECRETS_KEY) to read this request's sensitive parameters with")
	}
	plain, err := s.sealer.Open(sealed, sealBinding(id))
	if err != nil {
		return v, errors.New("this request's sensitive parameters could not be read; the secrets key may have changed")
	}
	if err := json.Unmarshal(plain, &v); err != nil {
		return v, fmt.Errorf("decode sensitive parameters: %w", err)
	}
	return v, nil
}

// body is a request as it is stored and shown: what to do, with sensitive
// values redacted, and how the agent named its targets.
type body struct {
	Target orchestrator.Target `json:"target"`

	Task           string          `json:"task,omitempty"`
	Environment    string          `json:"environment,omitempty"`
	Params         json.RawMessage `json:"params,omitempty"`
	TimeoutSeconds int             `json:"timeoutSeconds,omitempty"`

	Options json.RawMessage `json:"options,omitempty"`

	Name  string                  `json:"name,omitempty"`
	Steps []orchestrator.PlanStep `json:"steps,omitempty"`

	Plan       string         `json:"plan,omitempty"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// Submit checks in as the console checks a person's request, within what
// the agent's token may reach, and stores it pending. Nothing is started.
// A refusal is the console's own, for the agent to correct.
func (s *Service) Submit(ctx context.Context, caller Caller, in Input) (Request, error) {
	start := orchestrator.StartRequest{Scope: caller.Scope, Tasks: caller.Tasks, Actor: Actor(caller.TokenName)}
	b := body{Target: in.Target}
	var sensitive sensitiveValues
	var nodes []string
	var summary string

	switch in.Kind {
	case orchestrator.JobKindCodePlan:
		if s.codePlans == nil {
			return Request{}, &Refusal{Reason: "plans from code are not available on this console instance"}
		}
		env := in.Environment
		if env == "" {
			env = orchestrator.DefaultTaskEnvironment
		}
		checked, err := s.codePlans.Check(ctx, codeplans.Request{Plan: in.Plan, Environment: env, Parameters: in.Parameters, Start: start})
		if err != nil {
			return Request{}, err
		}
		b = body{Plan: in.Plan, Environment: env, Parameters: checked.Parameters}
		sensitive.Params = checked.Sensitive
		summary = fmt.Sprintf("plan %s from %s", in.Plan, env)

	default:
		var err error
		if nodes, err = s.freezeTargets(ctx, caller, in); err != nil {
			return Request{}, err
		}
		start.Target = orchestrator.Target{Nodes: nodes}
		switch in.Kind {
		case orchestrator.JobKindRun:
			if err := s.starter.CheckRun(ctx, orchestrator.RunRequest{StartRequest: start, Options: in.Options}); err != nil {
				return Request{}, err
			}
			b.Options = in.Options
			summary = "a Puppet run"
		case orchestrator.JobKindPatch:
			if err := s.starter.CheckPatch(ctx, orchestrator.PatchRequest{StartRequest: start, Options: in.Options}); err != nil {
				return Request{}, err
			}
			b.Options = in.Options
			summary = "a patch run"
		case orchestrator.JobKindTask:
			check, err := s.starter.CheckTask(ctx, orchestrator.TaskRequest{StartRequest: start, Task: in.Task, Environment: in.Environment, Params: in.Params, TimeoutSeconds: in.TimeoutSeconds})
			if err != nil {
				return Request{}, err
			}
			b.Task, b.Environment, b.Params, b.TimeoutSeconds = in.Task, check.Environment, check.StoredParams, in.TimeoutSeconds
			sensitive.Params = check.Sensitive
			summary = "task " + in.Task
		case orchestrator.JobKindPlan:
			checks, err := s.starter.CheckPlan(ctx, orchestrator.PlanRequest{StartRequest: start, Name: in.Name, Steps: in.Steps})
			if err != nil {
				return Request{}, err
			}
			steps := append([]orchestrator.PlanStep(nil), in.Steps...)
			for i, c := range checks {
				if i >= len(steps) {
					break
				}
				if c.StoredParams != nil {
					steps[i].Params = c.StoredParams
				}
				if len(c.Sensitive) > 0 {
					if sensitive.Steps == nil {
						sensitive.Steps = map[string]map[string]any{}
					}
					sensitive.Steps[strconv.Itoa(i)] = c.Sensitive
				}
			}
			b.Name, b.Steps = in.Name, steps
			summary = fmt.Sprintf("plan %q (%d steps)", in.Name, len(in.Steps))
		default:
			return Request{}, &Refusal{Reason: fmt.Sprintf("unknown kind %q", in.Kind)}
		}
		summary += " on " + describeNodes(nodes)
	}

	seal, err := s.sealFunc(sensitive)
	if err != nil {
		return Request{}, err
	}
	encoded, err := json.Marshal(b)
	if err != nil {
		return Request{}, fmt.Errorf("encode agent request: %w", err)
	}
	saved, err := s.store.Create(ctx, Request{
		TokenID: caller.TokenID, TokenName: caller.TokenName, Kind: string(in.Kind), Summary: summary, Body: encoded, Targets: nodes,
	}, s.ttl, s.maxPending, SealFunc(seal))
	if errors.Is(err, ErrTooManyPending) {
		return Request{}, &Refusal{Reason: fmt.Sprintf("this token already has %d requests waiting for a decision; wait for some to be decided or to expire", s.maxPending)}
	}
	if err != nil {
		return Request{}, err
	}
	s.record("agent_request.submitted", Actor(caller.TokenName), saved,
		fmt.Sprintf("agent %s asked to run %s (request %d)", caller.TokenName, saved.Summary, saved.ID),
		map[string]any{"kind": saved.Kind, "token": caller.TokenName, "targets": len(saved.Targets), "expiresAt": saved.ExpiresAt})
	return saved, nil
}

// Resolve is the nodes target names for caller, within the reach of its
// token, for an agent to preview a request with. Nothing is stored.
func (s *Service) Resolve(ctx context.Context, caller Caller, target orchestrator.Target) ([]string, error) {
	nodes, _, err := s.starter.Targets().Resolve(ctx, target, caller.Scope)
	return nodes, err
}

func describeNodes(nodes []string) string {
	switch len(nodes) {
	case 1:
		return nodes[0]
	default:
		return fmt.Sprintf("%d nodes", len(nodes))
	}
}

// freezeTargets resolves the request's targets now, within the agent's
// reach, so the approver sees the exact nodes. For a task a query or group
// is also narrowed to the nodes the token's allowlist permits the task on,
// as a person's request is.
func (s *Service) freezeTargets(ctx context.Context, caller Caller, in Input) ([]string, error) {
	nodes, source, err := s.starter.Targets().Resolve(ctx, in.Target, caller.Scope)
	if err != nil {
		return nil, err
	}
	if source != nil && in.Kind == orchestrator.JobKindTask && in.Task != "" {
		nodes = caller.Tasks.NodesFor(in.Task).Intersect(nodes).Certnames()
		if nodes == nil { // NodesFor was everything: Intersect kept nothing to list
			nodes = []string{}
		}
	}
	if len(nodes) == 0 {
		return nil, orchestrator.ErrNoNodesMatched
	}
	return nodes, nil
}

// Approve decides a pending request for: it claims the decision, checks the
// agent's token and the approver's own authority, and starts the job
// through the orchestrator, recording who asked and who approved. A refusal
// before the claim leaves the request pending; a failure after it marks the
// request failed.
func (s *Service) Approve(ctx context.Context, id int64, approver Approver, note string) (Request, error) {
	req, err := s.store.Get(ctx, id)
	if err != nil {
		return Request{}, err
	}
	if req.Status != StatusPending {
		return Request{}, ErrNotPending
	}
	auth, err := s.authority(ctx, req.TokenID)
	if errors.Is(err, ErrTokenGone) {
		_ = s.store.FailPending(ctx, id, ErrTokenGone.Error())
		failed, _ := s.store.Get(ctx, id)
		s.record("agent_request.failed", approver.Name, failed, fmt.Sprintf("request %d could not run: %s", id, ErrTokenGone), map[string]any{"reason": ErrTokenGone.Error()})
		return failed, &Refusal{Reason: ErrTokenGone.Error() + "; nothing was started"}
	}
	if err != nil {
		return Request{}, err
	}
	if approver.Name == auth.Owner {
		return Request{}, &Refusal{Reason: "you issued this agent's token, so someone else must approve its requests"}
	}
	var decoded body
	if err := json.Unmarshal(req.Body, &decoded); err != nil {
		return Request{}, fmt.Errorf("decode agent request: %w", err)
	}
	if err := approver.covers(req, decoded, auth); err != nil {
		return Request{}, err
	}

	claimed, err := s.store.Claim(ctx, id, approver.Name, note)
	if err != nil {
		return Request{}, err
	}
	job, err := s.start(ctx, claimed, decoded, auth, approver)
	if err != nil {
		reason := err.Error()
		_ = s.store.Fail(ctx, id, reason)
		failed, _ := s.store.Get(ctx, id)
		s.record("agent_request.failed", approver.Name, failed, fmt.Sprintf("request %d was approved but could not run: %s", id, reason), map[string]any{"reason": reason})
		return failed, &Refusal{Reason: "approved, but it could not be started: " + reason}
	}
	if err := s.store.SetJob(ctx, id, job.ID); err != nil {
		return Request{}, err
	}
	done, _ := s.store.Get(ctx, id)
	s.record("agent_request.approved", approver.Name, done,
		fmt.Sprintf("%s approved %s from agent %s (request %d, job %d)", approver.Name, done.Summary, done.TokenName, id, job.ID),
		map[string]any{"kind": done.Kind, "token": done.TokenName, "jobId": job.ID, "note": note})
	return done, nil
}

// covers refuses an approver whose own authority does not reach what the
// request would do: every node it acts on, and for tasks the task itself. A
// plan from code decides its own nodes within the agent's reach, so the
// approver must reach everything the agent can.
func (a Approver) covers(req Request, b body, agent Authorization) error {
	if req.Kind == string(orchestrator.JobKindCodePlan) {
		if a.Scope.All() {
			return nil
		}
		if agent.Scope.All() || len(a.Scope.Outside(agent.Scope.Certnames())) > 0 {
			return &Refusal{Reason: "this plan can act on every node its agent can reach, and your own access to run things does not cover all of them"}
		}
		return nil
	}
	if outside := a.Scope.Outside(req.Targets); len(outside) > 0 {
		return &Refusal{Reason: fmt.Sprintf("you may not run things on %s, so you cannot approve this request", strings.Join(firstN(outside, 5), ", "))}
	}
	var tasks []string
	switch req.Kind {
	case string(orchestrator.JobKindTask):
		tasks = []string{b.Task}
	case string(orchestrator.JobKindPlan):
		for _, st := range b.Steps {
			if st.Kind == orchestrator.JobKindTask && st.TaskName != "" {
				tasks = append(tasks, st.TaskName)
			}
		}
	}
	for _, task := range tasks {
		if !a.Tasks.Allows(task) || len(a.Tasks.NodesFor(task).Outside(req.Targets)) > 0 {
			return &Refusal{Reason: fmt.Sprintf("you may not run task %s on all of this request's nodes, so you cannot approve it", task)}
		}
	}
	return nil
}

func firstN(v []string, n int) []string {
	if len(v) > n {
		return append(v[:n:n], fmt.Sprintf("and %d more", len(v)-n))
	}
	return v
}

// start rebuilds the request as the agent's token may make it now, with
// sensitive values put back, and starts it.
func (s *Service) start(ctx context.Context, req Request, b body, auth Authorization, approver Approver) (orchestrator.Job, error) {
	ref := &orchestrator.AgentRef{RequestID: req.ID, Token: req.TokenName, ApprovedBy: approver.Name}
	startReq := orchestrator.StartRequest{
		Target: orchestrator.Target{Nodes: req.Targets}, Scope: auth.Scope, Tasks: auth.Tasks, Actor: Actor(req.TokenName), Agent: ref,
	}
	sensitive, err := s.openSealed(ctx, req.ID)
	if err != nil {
		return orchestrator.Job{}, err
	}
	switch orchestrator.JobKind(req.Kind) {
	case orchestrator.JobKindRun:
		return s.starter.StartRun(ctx, orchestrator.RunRequest{StartRequest: startReq, Options: b.Options})
	case orchestrator.JobKindPatch:
		return s.starter.StartPatch(ctx, orchestrator.PatchRequest{StartRequest: startReq, Options: b.Options})
	case orchestrator.JobKindTask:
		params, err := mergeParams(b.Params, sensitive.Params)
		if err != nil {
			return orchestrator.Job{}, err
		}
		return s.starter.StartTask(ctx, orchestrator.TaskRequest{StartRequest: startReq, Task: b.Task, Environment: b.Environment, Params: params, TimeoutSeconds: b.TimeoutSeconds})
	case orchestrator.JobKindPlan:
		steps := append([]orchestrator.PlanStep(nil), b.Steps...)
		for i := range steps {
			values := sensitive.Steps[strconv.Itoa(i)]
			if len(values) == 0 {
				continue
			}
			if steps[i].Params, err = mergeParams(steps[i].Params, values); err != nil {
				return orchestrator.Job{}, err
			}
		}
		return s.starter.StartPlan(ctx, orchestrator.PlanRequest{StartRequest: startReq, Name: b.Name, Steps: steps})
	case orchestrator.JobKindCodePlan:
		if s.codePlans == nil {
			return orchestrator.Job{}, errors.New("plans from code are not available on this console instance")
		}
		params := map[string]any{}
		for k, v := range b.Parameters {
			params[k] = v
		}
		for k, v := range sensitive.Params {
			params[k] = v
		}
		return s.codePlans.Start(ctx, codeplans.Request{Plan: b.Plan, Environment: b.Environment, Parameters: params, Start: startReq})
	}
	return orchestrator.Job{}, fmt.Errorf("unknown request kind %q", req.Kind)
}

// mergeParams puts sensitive values back into stored parameters.
func mergeParams(stored json.RawMessage, sensitive map[string]any) (json.RawMessage, error) {
	if len(sensitive) == 0 {
		return stored, nil
	}
	params := map[string]any{}
	if len(stored) > 0 {
		if err := json.Unmarshal(stored, &params); err != nil {
			return nil, fmt.Errorf("decode stored parameters: %w", err)
		}
	}
	for k, v := range sensitive {
		params[k] = v
	}
	return json.Marshal(params)
}

// Reject decides a pending request against. Any approver may, the token's
// owner included: refusing needs no reach of its own, and a person who
// issued an agent's token can always call off what it asked.
func (s *Service) Reject(ctx context.Context, id int64, approver Approver, note string) (Request, error) {
	rejected, err := s.store.Reject(ctx, id, approver.Name, note)
	if err != nil {
		return Request{}, err
	}
	s.record("agent_request.rejected", approver.Name, rejected,
		fmt.Sprintf("%s rejected %s from agent %s (request %d)", approver.Name, rejected.Summary, rejected.TokenName, id),
		map[string]any{"kind": rejected.Kind, "token": rejected.TokenName, "note": note})
	return rejected, nil
}

// Expire marks requests past their time expired and records each; it is
// what the periodic sweep calls.
func (s *Service) Expire(ctx context.Context) (int, error) {
	due, err := s.store.ExpireDue(ctx)
	for _, r := range due {
		s.record("agent_request.expired", "system", r,
			fmt.Sprintf("request %d from agent %s expired undecided: %s", r.ID, r.TokenName, r.Summary),
			map[string]any{"kind": r.Kind, "token": r.TokenName})
	}
	return len(due), err
}
