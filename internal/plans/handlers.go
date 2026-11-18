package plans

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// Handlers serves the saved-plan HTTP API.
type Handlers struct {
	service         *Service
	actor           func(r *http.Request) string
	recordAuditRead func(r *http.Request, event auditlog.Event)

	// taskScope says which tasks the caller may run: a plan is saved only
	// with tasks its editor may run, and shows a reader which steps they
	// may not. See SetTaskScope.
	taskScope taskscope.Func
}

// SetTaskScope limits each caller to the tasks their roles or token
// allow. Without it every caller may run every task.
func (h *Handlers) SetTaskScope(tasks taskscope.Func) { h.taskScope = tasks }

// tasksFor works out the caller's task scope, answering and reporting
// false when it cannot: a plan is never saved or shown on an unknown one.
func (h *Handlers) tasksFor(w http.ResponseWriter, r *http.Request) (taskscope.Scope, bool) {
	tasks, err := h.taskScope(r)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "could not determine which tasks you may run: " + err.Error()})
		return taskscope.Scope{}, false
	}
	return tasks, true
}

// NewHandlers builds Handlers over service. actor names the caller.
func NewHandlers(service *Service, actor func(r *http.Request) string, recordAuditRead func(r *http.Request, event auditlog.Event)) *Handlers {
	if recordAuditRead == nil {
		recordAuditRead = func(*http.Request, auditlog.Event) {}
	}
	return &Handlers{service: service, actor: actor, recordAuditRead: recordAuditRead, taskScope: taskscope.UnrestrictedFunc}
}

// Register wires the saved-plan routes onto mux. Reading needs
// orchestrator:read and changing needs orchestrator:run, the permission
// running a plan needs.
//
// These routes are not limited by node groups: a saved plan names no
// nodes. What a caller may run one on is decided when they run it.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/orchestrator/saved-plans", authorize("orchestrator:read", h.list))
	mux.HandleFunc("GET /api/v1/orchestrator/saved-plans/{id}", authorize("orchestrator:read", h.get))
	mux.HandleFunc("POST /api/v1/orchestrator/saved-plans", authorize("orchestrator:run", h.create))
	mux.HandleFunc("PUT /api/v1/orchestrator/saved-plans/{id}", authorize("orchestrator:run", h.replace))
	mux.HandleFunc("DELETE /api/v1/orchestrator/saved-plans/{id}", authorize("orchestrator:run", h.delete))
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid plan id"})
		return 0, false
	}
	return id, true
}

// writeFailure answers a request that did not succeed. A refusal names
// the plan's field it concerns in "field", or the step in "step" (with
// "fields" for that step's task parameters), so the editor can show the
// reason where it belongs.
func writeFailure(w http.ResponseWriter, err error) {
	var invalid *InvalidPlanError
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": ErrNotFound.Error()})
	case errors.As(err, &invalid):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": invalid.Reason, "field": invalid.Field})
	case orchestrator.IsRefusal(err):
		status, body := orchestrator.RefusalResponse(err)
		if _, isStep := body["step"]; !isStep {
			body["field"] = "steps"
		}
		writeJSON(w, status, body)
	default:
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
	}
}

func (h *Handlers) list(w http.ResponseWriter, r *http.Request) {
	plans, err := h.service.List(r.Context())
	if err != nil {
		writeFailure(w, err)
		return
	}
	h.recordAuditRead(r, auditlog.Event{Action: "plan.list.viewed", ResourceType: "plan"})
	writeJSON(w, http.StatusOK, map[string]any{"items": plans})
}

func (h *Handlers) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	tasks, ok := h.tasksFor(w, r)
	if !ok {
		return
	}
	plan, err := h.service.Get(r.Context(), id, tasks)
	if err != nil {
		writeFailure(w, err)
		return
	}
	h.recordAuditRead(r, auditlog.Event{Action: "plan.viewed", ResourceType: "plan", ResourceID: strconv.FormatInt(id, 10)})
	writeJSON(w, http.StatusOK, plan)
}

func decodeInput(w http.ResponseWriter, r *http.Request) (Input, bool) {
	var in Input
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body: " + err.Error()})
		return Input{}, false
	}
	return in, true
}

func (h *Handlers) create(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeInput(w, r)
	if !ok {
		return
	}
	tasks, ok := h.tasksFor(w, r)
	if !ok {
		return
	}
	plan, err := h.service.Create(r.Context(), in, h.actor(r), tasks)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, plan)
}

func (h *Handlers) replace(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	in, ok := decodeInput(w, r)
	if !ok {
		return
	}
	tasks, ok := h.tasksFor(w, r)
	if !ok {
		return
	}
	plan, err := h.service.Replace(r.Context(), id, in, h.actor(r), tasks)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (h *Handlers) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), id, h.actor(r)); err != nil {
		writeFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
