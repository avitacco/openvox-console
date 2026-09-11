package orchestrator

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

// Handlers serves the orchestrator HTTP API.
type Handlers struct {
	store      *Store
	dispatcher *Dispatcher
	actor      func(r *http.Request) string

	// recordAuditRead is bound to the orchestrator category
	// (configurable-audit-logging) - trigger/completion audit events are
	// recorded by Dispatcher itself (see AuditRecorder), since those fire
	// outside any HTTP request's lifetime; this closure covers only the
	// read endpoints here.
	recordAuditRead func(r *http.Request, event auditlog.Event)
}

// NewHandlers builds Handlers backed by store and dispatcher. actor
// extracts the acting user's identity from a request - injected, like
// the recordActivity pattern elsewhere, so this package never imports
// internal/rbac directly (see design.md).
func NewHandlers(store *Store, dispatcher *Dispatcher, actor func(r *http.Request) string, recordAuditRead func(r *http.Request, event auditlog.Event)) *Handlers {
	return &Handlers{store: store, dispatcher: dispatcher, actor: actor, recordAuditRead: recordAuditRead}
}

// Register wires this package's routes onto mux: triggering a run,
// task, or plan requires orchestrator:run; reading job status/history
// requires orchestrator:read - see internal/rbac.Verifier.Authorize
// for what authorize does.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /api/v1/orchestrator/runs", authorize("orchestrator:run", h.triggerRun))
	mux.HandleFunc("POST /api/v1/orchestrator/tasks", authorize("orchestrator:run", h.triggerTask))
	mux.HandleFunc("POST /api/v1/orchestrator/plans", authorize("orchestrator:run", h.triggerPlan))
	mux.HandleFunc("GET /api/v1/orchestrator/jobs", authorize("orchestrator:read", h.listJobs))
	mux.HandleFunc("GET /api/v1/orchestrator/jobs/{id}", authorize("orchestrator:read", h.getJob))
}

type triggerRunRequest struct {
	Targets []string `json:"targets"`
}

func (h *Handlers) triggerRun(w http.ResponseWriter, r *http.Request) {
	var req triggerRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid request body"))
		return
	}
	if len(req.Targets) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("at least one target is required"))
		return
	}

	job, err := h.store.CreateJob(r.Context(), JobKindRun, "", "", nil, req.Targets, h.actor(r))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	h.dispatcher.DispatchRun(r.Context(), job)
	writeJSON(w, http.StatusOK, job)
}

type triggerTaskRequest struct {
	Targets []string        `json:"targets"`
	Task    string          `json:"task"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (h *Handlers) triggerTask(w http.ResponseWriter, r *http.Request) {
	var req triggerTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid request body"))
		return
	}
	if len(req.Targets) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("at least one target is required"))
		return
	}
	if req.Task == "" {
		writeError(w, http.StatusBadRequest, errors.New("task name is required"))
		return
	}

	job, err := h.store.CreateJob(r.Context(), JobKindTask, req.Task, "", req.Params, req.Targets, h.actor(r))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	h.dispatcher.DispatchTask(r.Context(), job, req.Task, req.Params)
	writeJSON(w, http.StatusOK, job)
}

type triggerPlanRequest struct {
	Targets []string   `json:"targets"`
	Name    string     `json:"name"`
	Steps   []PlanStep `json:"steps"`
}

func (h *Handlers) triggerPlan(w http.ResponseWriter, r *http.Request) {
	var req triggerPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid request body"))
		return
	}
	if len(req.Targets) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("at least one target is required"))
		return
	}
	if len(req.Steps) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("at least one step is required"))
		return
	}

	plan, err := h.store.CreatePlan(r.Context(), req.Name, req.Steps, req.Targets, h.actor(r))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	h.dispatcher.DispatchPlan(r.Context(), plan)
	writeJSON(w, http.StatusOK, plan)
}

func (h *Handlers) listJobs(w http.ResponseWriter, r *http.Request) {
	page, pageSize := pagination.Params(r)

	filter := JobFilter{
		TriggeredBy: r.URL.Query().Get("triggeredBy"),
		Target:      r.URL.Query().Get("target"),
		SortAsc:     r.URL.Query().Get("sort") == "asc",
	}
	if kind := r.URL.Query().Get("kind"); kind != "" {
		switch JobKind(kind) {
		case JobKindRun, JobKindTask, JobKindPlan:
			filter.Kind = kind
		default:
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid kind %q: must be %q, %q, or %q", kind, JobKindRun, JobKindTask, JobKindPlan))
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

	job, err := h.store.GetJob(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
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
