package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// Handlers serves the schedule HTTP API.
type Handlers struct {
	service *Service
	// owner identifies the caller: their name and whether they are a
	// user or a service token. Injected, so this package never imports
	// internal/rbac.
	owner func(r *http.Request) Owner
	// scope is the caller's node scope for the permission that gated
	// the request - orchestrator:read for the reads, orchestrator:run
	// for everything else.
	scope nodescope.Func
	// taskScope is which tasks the caller may run - see SetTaskScope.
	taskScope       taskscope.Func
	recordAuditRead func(r *http.Request, event auditlog.Event)
}

// NewHandlers builds Handlers over service.
func NewHandlers(service *Service, owner func(r *http.Request) Owner, recordAuditRead func(r *http.Request, event auditlog.Event)) *Handlers {
	if recordAuditRead == nil {
		recordAuditRead = func(*http.Request, auditlog.Event) {}
	}
	return &Handlers{service: service, owner: owner, scope: nodescope.Unscoped, taskScope: taskscope.UnrestrictedFunc, recordAuditRead: recordAuditRead}
}

// SetScope limits each caller to their node group scope - see
// internal/nodescope.
func (h *Handlers) SetScope(scope nodescope.Func) { h.scope = scope }

// SetTaskScope limits each caller to the tasks their roles or token
// allow: a schedule is saved only for tasks its saver may run. Without it
// every caller may run every task.
func (h *Handlers) SetTaskScope(tasks taskscope.Func) { h.taskScope = tasks }

// Register wires the schedule routes onto mux. Reading needs
// orchestrator:read; anything that changes a schedule needs
// orchestrator:run, the permission the jobs it starts need.
//
// A schedule is shown to its owner and to callers whose scope covers
// every node; to anyone else it does not exist.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/orchestrator/schedules", authorize("orchestrator:read", h.list))
	mux.HandleFunc("POST /api/v1/orchestrator/schedules", authorize("orchestrator:run", h.create))
	mux.HandleFunc("GET /api/v1/orchestrator/schedules/{id}", authorize("orchestrator:read", h.get))
	mux.HandleFunc("PUT /api/v1/orchestrator/schedules/{id}", authorize("orchestrator:run", h.replace))
	mux.HandleFunc("DELETE /api/v1/orchestrator/schedules/{id}", authorize("orchestrator:run", h.delete))
	mux.HandleFunc("POST /api/v1/orchestrator/schedules/{id}/pause", authorize("orchestrator:run", h.pause))
	mux.HandleFunc("POST /api/v1/orchestrator/schedules/{id}/resume", authorize("orchestrator:run", h.resume))
}

// caller identifies the request's caller. It reports false, having
// answered, when their scope cannot be worked out.
func (h *Handlers) caller(w http.ResponseWriter, r *http.Request) (Caller, bool) {
	scope, err := h.scope(r)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "could not determine which nodes you may act on: " + err.Error()})
		return Caller{}, false
	}
	tasks, err := h.taskScope(r)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "could not determine which tasks you may run: " + err.Error()})
		return Caller{}, false
	}
	return Caller{Owner: h.owner(r), Scope: scope, Tasks: tasks}, true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid schedule id"})
		return 0, false
	}
	return id, true
}

// writeFailure answers a request that did not succeed. A refusal names,
// in "field", the part of the schedule it concerns - name, startAt,
// repeat, target, options, task, params or steps (with "step" saying
// which one) - so a form can show it there.
func writeFailure(w http.ResponseWriter, err error) {
	var invalid *InvalidScheduleError
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": ErrNotFound.Error()})
	case errors.As(err, &invalid):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": invalid.Reason, "field": invalid.Field})
	case orchestrator.IsRefusal(err):
		status, body := orchestrator.RefusalResponse(err)
		body["field"] = refusedField(err)
		writeJSON(w, status, body)
	default:
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
	}
}

func refusedField(err error) string {
	var options *orchestrator.InvalidRunOptionsError
	var task *orchestrator.InvalidTaskError
	var notPermitted *orchestrator.TaskNotPermittedError
	if orchestrator.IsPlanShapeError(err) {
		return "steps"
	}
	switch {
	case errors.As(err, &notPermitted):
		return "task"
	case errors.As(err, &options):
		return "options"
	case errors.As(err, &task):
		return "task"
	}
	return "target"
}

func (h *Handlers) list(w http.ResponseWriter, r *http.Request) {
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	schedules, err := h.service.List(r.Context(), caller)
	if err != nil {
		writeFailure(w, err)
		return
	}
	h.recordAuditRead(r, auditlog.Event{Action: "schedule.list.viewed", ResourceType: "schedule"})
	writeJSON(w, http.StatusOK, map[string]any{"items": schedules})
}

func (h *Handlers) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	sched, err := h.service.Get(r.Context(), id, caller)
	if err != nil {
		writeFailure(w, err)
		return
	}
	h.recordAuditRead(r, auditlog.Event{Action: "schedule.viewed", ResourceType: "schedule", ResourceID: strconv.FormatInt(id, 10)})
	writeJSON(w, http.StatusOK, sched)
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
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	sched, err := h.service.Create(r.Context(), in, caller)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sched)
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
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	sched, err := h.service.Replace(r.Context(), id, in, caller)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sched)
}

func (h *Handlers) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), id, caller); err != nil {
		writeFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) pause(w http.ResponseWriter, r *http.Request) {
	h.setEnabled(w, r, h.service.Pause)
}

func (h *Handlers) resume(w http.ResponseWriter, r *http.Request) {
	h.setEnabled(w, r, h.service.Resume)
}

func (h *Handlers) setEnabled(w http.ResponseWriter, r *http.Request, change func(ctx context.Context, id int64, caller Caller) (Schedule, error)) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	sched, err := change(r.Context(), id, caller)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sched)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
