package orchestrator

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/pagination"
	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// Handlers serves the orchestrator HTTP API.
type Handlers struct {
	store      *Store
	dispatcher *Dispatcher
	actor      func(r *http.Request) string
	catalog    TaskCatalog // see SetTaskCatalog

	// recordAuditRead is bound to the orchestrator category
	// (configurable-audit-logging) - trigger/completion audit events are
	// recorded by Dispatcher itself (see AuditRecorder), since those fire
	// outside any HTTP request's lifetime; this closure covers only the
	// read endpoints here.
	recordAuditRead func(r *http.Request, event auditlog.Event)

	// scope is the caller's node group scope for the permission that
	// gated the request: orchestrator:run for the triggers, which may
	// only name nodes inside it, and orchestrator:read for jobs, which
	// show only the nodes inside it. See SetScope.
	scope nodescope.Func

	// taskScope says which tasks the caller may run, from their roles or
	// token: see SetTaskScope.
	taskScope taskscope.Func

	// recordAudit writes audit events for what these handlers refuse -
	// see SetAuditWrite. Optional.
	recordAudit func(r *http.Request, event auditlog.Event)

	// starter checks and starts runs and tasks - see Starter.
	starter *Starter

	// reportEnvironments serves run options - see
	// SetReportEnvironments. Optional.
	reportEnvironments ReportEnvironments
}

// Starter is what these handlers start runs and tasks with, for the
// scheduler to start them the same way.
func (h *Handlers) Starter() *Starter { return h.starter }

// SetTargetSources lets a job's targets be given as a PQL query or a
// node group. Without them only a list of nodes is accepted.
func (h *Handlers) SetTargetSources(query QueryFunc, group GroupLookup) {
	h.starter.targets = NewTargetResolver(query, group)
}

// SetPatches lets patch runs be started. Without it a patch request is
// refused as unavailable.
func (h *Handlers) SetPatches(c *PatchCoordinator) { h.starter.patches = c }

// SetScope limits each caller to the nodes in their node group scope -
// see internal/nodescope. Without it every caller is fleet-wide.
func (h *Handlers) SetScope(scope nodescope.Func) { h.scope = scope }

// SetTaskScope limits each caller to the tasks their roles or token
// allow - see internal/taskscope. Without it every caller may run every
// task.
func (h *Handlers) SetTaskScope(tasks taskscope.Func) { h.taskScope = tasks }

// SetAuditWrite records an audit event for each task start refused
// because the caller's roles do not allow the task.
func (h *Handlers) SetAuditWrite(record func(r *http.Request, event auditlog.Event)) {
	h.recordAudit = record
}

// NewHandlers builds Handlers backed by store and dispatcher. actor
// extracts the acting user's identity from a request - injected, like
// the recordActivity pattern elsewhere, so this package never imports
// internal/rbac directly (see design.md).
func NewHandlers(store *Store, dispatcher *Dispatcher, actor func(r *http.Request) string, recordAuditRead func(r *http.Request, event auditlog.Event)) *Handlers {
	return &Handlers{
		store: store, dispatcher: dispatcher, actor: actor, recordAuditRead: recordAuditRead, scope: nodescope.Unscoped, taskScope: taskscope.UnrestrictedFunc,
		starter: &Starter{store: store, dispatcher: dispatcher, targets: NewTargetResolver(nil, nil)},
	}
}

// SetTaskCatalog attaches the task catalogue tasks are resolved and
// validated against. Without one, every task request is refused as "not
// configured".
func (h *Handlers) SetTaskCatalog(c TaskCatalog) {
	h.catalog = c
	h.starter.catalog = c
}

// Register wires this package's routes onto mux: triggering a run,
// task, or plan requires orchestrator:run; reading job status/history
// requires orchestrator:read - see internal/rbac.Verifier.Authorize
// for what authorize does.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /api/v1/orchestrator/runs", authorize("orchestrator:run", h.triggerRun))
	mux.HandleFunc("POST /api/v1/orchestrator/tasks", authorize("orchestrator:run", h.triggerTask))
	mux.HandleFunc("POST /api/v1/orchestrator/patches", authorize("orchestrator:run", h.triggerPatch))
	mux.HandleFunc("POST /api/v1/orchestrator/targets/resolve", authorize("orchestrator:run", h.resolveTargets))
	mux.HandleFunc("POST /api/v1/orchestrator/plans", authorize("orchestrator:run", h.triggerPlan))
	mux.HandleFunc("GET /api/v1/orchestrator/jobs", authorize("orchestrator:read", h.listJobs))
	mux.HandleFunc("GET /api/v1/orchestrator/jobs/{id}", authorize("orchestrator:read", h.getJob))
}

// targetFields are how a request names its targets: exactly one of a
// list of nodes, a PQL query, or a node group id.
type targetFields struct {
	Targets []string `json:"targets"`
	Query   string   `json:"query,omitempty"`
	Group   *int64   `json:"group,omitempty"`
}

func (t targetFields) target() Target {
	return Target{Nodes: t.Targets, Query: t.Query, Group: t.Group}
}

// startRequest builds the part of a request that is the same for a run
// and a task. It reports false, having answered, when the caller's
// scope cannot be worked out: an action never proceeds on an unknown
// scope.
func (h *Handlers) startRequest(w http.ResponseWriter, r *http.Request, target Target) (StartRequest, bool) {
	scope, err := h.scope(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, errors.New("could not determine which nodes you may act on: "+err.Error()))
		return StartRequest{}, false
	}
	tasks, err := h.taskScope(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, errors.New("could not determine which tasks you may run: "+err.Error()))
		return StartRequest{}, false
	}
	return StartRequest{Target: target, Scope: scope, Tasks: tasks, Actor: h.actor(r)}, true
}

// writeStartError answers a run, task or plan request that was not
// started. A task refused because the caller's roles do not allow it is
// also audited: the actor, the task and, for a plan, the step, and never
// the parameters.
func (h *Handlers) writeStartError(w http.ResponseWriter, r *http.Request, err error) {
	var notPermitted *TaskNotPermittedError
	if errors.As(err, &notPermitted) && h.recordAudit != nil {
		event := auditlog.Event{Action: "task.denied", ResourceType: "task", ResourceID: notPermitted.Task}
		var step *PlanStepError
		if errors.As(err, &step) {
			event.After = map[string]int{"step": step.Step}
		}
		h.recordAudit(r, event)
	}
	status, body := RefusalResponse(err)
	writeJSON(w, status, body)
}

// RefusalResponse is the HTTP answer to a run or task request that was
// not started: 403 naming the nodes outside the caller's scope, 400 for
// anything else about the request that was refused (with "fields" for
// task parameters), and 502 when something the console depends on
// failed. It is exported so that saving a schedule, which is checked as
// the same request, is refused in the same words.
func RefusalResponse(err error) (int, map[string]any) {
	status, body := refusalResponse(err)
	// A plan's refused step is named, so a form can show the reason on
	// the step it belongs to.
	var step *PlanStepError
	if errors.As(err, &step) {
		body["error"] = err.Error()
		body["step"] = step.Step
	}
	return status, body
}

func refusalResponse(err error) (int, map[string]any) {
	var outside *OutOfScopeError
	var task *InvalidTaskError
	var notPermitted *TaskNotPermittedError
	var patchOptions *InvalidPatchOptionsError
	var beingPatched *NodesBeingPatchedError
	var agentless *AgentlessTargetsError
	switch {
	case errors.As(err, &agentless):
		return http.StatusBadRequest, map[string]any{"error": err.Error(), "agentless": agentless.Targets}
	case errors.As(err, &patchOptions):
		body := map[string]any{"error": err.Error()}
		if patchOptions.Field != "" {
			body["field"] = patchOptions.Field
		}
		return http.StatusBadRequest, body
	case errors.As(err, &beingPatched):
		return http.StatusConflict, map[string]any{"error": err.Error(), "nodes": beingPatched.Nodes}
	case errors.As(err, &notPermitted):
		// Before the out-of-scope case: a plan step can wrap either, and
		// the task is what the caller needs to be told.
		return http.StatusForbidden, map[string]any{"error": err.Error(), "task": notPermitted.Task}
	case errors.As(err, &outside):
		message := "this node is not in the node groups your role can act on: " + outside.Outside[0]
		if len(outside.Outside) > 1 {
			message = "these nodes are not in the node groups your role can act on: " + strings.Join(outside.Outside, ", ")
		}
		if outside.Task != "" {
			// Inside their node groups, but not a place their roles allow
			// this task: said so, rather than as if the node were hidden.
			message = outside.Error()
		}
		body := map[string]any{"error": message, "outOfScope": outside.Outside}
		if outside.Task != "" {
			body["task"] = outside.Task
		}
		return http.StatusForbidden, body
	case errors.As(err, &task):
		return taskErrorResponse(err)
	case IsRefusal(err):
		return http.StatusBadRequest, map[string]any{"error": err.Error()}
	default:
		return http.StatusBadGateway, map[string]any{"error": err.Error()}
	}
}

type triggerRunRequest struct {
	targetFields
	// Options are a RunOptions; absent, null or empty is a plain run.
	Options json.RawMessage `json:"options,omitempty"`
}

func (h *Handlers) triggerRun(w http.ResponseWriter, r *http.Request) {
	var req triggerRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid request body"))
		return
	}
	start, ok := h.startRequest(w, r, req.target())
	if !ok {
		return
	}
	job, err := h.starter.StartRun(r.Context(), RunRequest{StartRequest: start, Options: req.Options})
	if err != nil {
		h.writeStartError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

type triggerPatchRequest struct {
	targetFields
	// Options are a PatchOptions; absent, null or empty takes the defaults.
	Options json.RawMessage `json:"options,omitempty"`
}

func (h *Handlers) triggerPatch(w http.ResponseWriter, r *http.Request) {
	var req triggerPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid request body"))
		return
	}
	start, ok := h.startRequest(w, r, req.target())
	if !ok {
		return
	}
	job, err := h.starter.StartPatch(r.Context(), PatchRequest{StartRequest: start, Options: req.Options})
	if err != nil {
		h.writeStartError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

type triggerTaskRequest struct {
	targetFields
	Task   string          `json:"task"`
	Params json.RawMessage `json:"params,omitempty"`
	// Environment defaults to DefaultTaskEnvironment.
	Environment string `json:"environment,omitempty"`
	// TimeoutSeconds defaults to DefaultTaskTimeout, and may not exceed
	// MaxTaskTimeout.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

func (h *Handlers) triggerTask(w http.ResponseWriter, r *http.Request) {
	var req triggerTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid request body"))
		return
	}
	start, ok := h.startRequest(w, r, req.target())
	if !ok {
		return
	}
	job, err := h.starter.StartTask(r.Context(), TaskRequest{
		StartRequest: start, Task: req.Task, Environment: req.Environment,
		Params: req.Params, TimeoutSeconds: req.TimeoutSeconds,
	})
	if err != nil {
		h.writeStartError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

type resolveTargetsResponse struct {
	Nodes []string `json:"nodes"`
	Count int      `json:"count"`
}

// resolveTargets is the preview: the nodes a job the caller started now
// would run on, or the refusal it would get. Nothing is started. An
// empty answer is an answer here, not a refusal - a preview should be
// able to say that nothing matches.
func (h *Handlers) resolveTargets(w http.ResponseWriter, r *http.Request) {
	var req targetFields
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid request body"))
		return
	}
	start, ok := h.startRequest(w, r, req.target())
	if !ok {
		return
	}
	nodes, _, err := h.starter.targets.Resolve(r.Context(), start.Target, start.Scope)
	if err != nil {
		h.writeStartError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resolveTargetsResponse{Nodes: nodes, Count: len(nodes)})
}

type triggerPlanRequest struct {
	targetFields
	Name  string     `json:"name"`
	Steps []PlanStep `json:"steps"`
}

func (h *Handlers) triggerPlan(w http.ResponseWriter, r *http.Request) {
	var req triggerPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid request body"))
		return
	}
	start, ok := h.startRequest(w, r, req.target())
	if !ok {
		return
	}
	// Every step is checked before anything is created, so a plan with
	// a bad step is refused whole rather than failing partway through.
	plan, err := h.starter.StartPlan(r.Context(), PlanRequest{StartRequest: start, Name: req.Name, Steps: req.Steps})
	if err != nil {
		h.writeStartError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// writeTaskError answers a task request the console refused - see
// taskErrorResponse.
func writeTaskError(w http.ResponseWriter, err error) {
	status, body := taskErrorResponse(err)
	writeJSON(w, status, body)
}

// taskErrorResponse is the answer to a task request the console
// refused, with the status the task catalogue uses for the same cause,
// and - for invalid parameters - a "fields" map of parameter to
// problem, the shape the console's other forms already read, so a form
// can mark every field at once.
func taskErrorResponse(err error) (int, map[string]any) {
	body := map[string]any{"error": err.Error()}
	var ve *taskcatalog.ValidationError
	if errors.As(err, &ve) {
		fields := map[string]string{}
		for _, p := range ve.Problems {
			fields[p.Param] = p.Message
		}
		body["fields"] = fields
	}
	status := taskcatalog.HTTPStatus(err)
	var nf *taskcatalog.NotFoundError
	if errors.As(err, &nf) || status == http.StatusInternalServerError {
		// The request named something that does not exist, or carried
		// task metadata the console cannot use: either way the request
		// is what is wrong, not this endpoint.
		status = http.StatusBadRequest
	}
	return status, body
}

func (h *Handlers) listJobs(w http.ResponseWriter, r *http.Request) {
	page, pageSize := pagination.Params(r)

	filter := JobFilter{
		TriggeredBy: r.URL.Query().Get("triggeredBy"),
		Target:      r.URL.Query().Get("target"),
		SortAsc:     r.URL.Query().Get("sort") == "asc",
	}
	if parent := r.URL.Query().Get("parent"); parent != "" {
		id, err := strconv.ParseInt(parent, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, errors.New("invalid parent: must be a plan's job id"))
			return
		}
		filter.Parent = &id
	}
	if kind := r.URL.Query().Get("kind"); kind != "" {
		switch JobKind(kind) {
		case JobKindRun, JobKindTask, JobKindPlan, JobKindPatch, JobKindCodePlan:
			filter.Kind = kind
		default:
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid kind %q: must be %q, %q, %q, %q or %q", kind, JobKindRun, JobKindTask, JobKindPlan, JobKindPatch, JobKindCodePlan))
			return
		}
	}
	if status := r.URL.Query().Get("status"); status != "" {
		switch status {
		// Not StatusPending - a job (unlike a job target) never sits in
		// that state, per the jobs table's own CHECK constraint.
		case StatusRunning, StatusSucceeded, StatusFailed:
			filter.Status = status
		default:
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid status %q: must be %q, %q, or %q", status, StatusRunning, StatusSucceeded, StatusFailed))
			return
		}
	}

	scope, err := h.scope(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	// A job is the record of an action on nodes: a scoped caller sees
	// the jobs that touched their nodes, and only those nodes in them.
	filter.Certnames = scope.Certnames()

	jobs, total, err := h.store.ListJobs(r.Context(), page, pageSize, filter)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	h.recordAuditRead(r, auditlog.Event{Action: "job.list.viewed", ResourceType: "job"})
	writeJSON(w, http.StatusOK, pagination.New(jobs, page, pageSize, total))
}

func (h *Handlers) getJob(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid job id"))
		return
	}

	scope, err := h.scope(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	job, err := h.store.GetJobInScope(r.Context(), id, scope.Certnames())
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	h.fillReportEnvironments(r.Context(), &job)
	h.recordAuditRead(r, auditlog.Event{Action: "job.viewed", ResourceType: "job", ResourceID: strconv.FormatInt(id, 10)})
	writeJSON(w, http.StatusOK, job)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
