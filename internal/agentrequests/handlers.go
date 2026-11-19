package agentrequests

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
)

// Handlers serves the Approvals page's API.
type Handlers struct {
	service *Service
	// scope is the caller's node scope for the permission that gated the
	// request: a request is shown to a caller only when every node it acts
	// on is in it.
	scope nodescope.Func
	// approver says who is deciding and what their own roles let them run
	// - their node scope and tasks for orchestrator:run.
	approver        func(r *http.Request) (Approver, error)
	recordAuditRead func(r *http.Request, event auditlog.Event)
}

// NewHandlers builds Handlers over service. approver is how a deciding
// caller is described; scope is their scope for reading.
func NewHandlers(service *Service, scope nodescope.Func, approver func(r *http.Request) (Approver, error), recordAuditRead func(r *http.Request, event auditlog.Event)) *Handlers {
	if recordAuditRead == nil {
		recordAuditRead = func(*http.Request, auditlog.Event) {}
	}
	return &Handlers{service: service, scope: scope, approver: approver, recordAuditRead: recordAuditRead}
}

// Register wires the routes onto mux. Reading needs agent-requests:read;
// deciding needs agent-requests:approve and, checked by the service, the
// approver's own orchestrator:run over what the request does.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/agent-requests", authorize("agent-requests:read", h.list))
	mux.HandleFunc("GET /api/v1/agent-requests/pending-count", authorize("agent-requests:read", h.pendingCount))
	mux.HandleFunc("GET /api/v1/agent-requests/{id}", authorize("agent-requests:read", h.get))
	mux.HandleFunc("POST /api/v1/agent-requests/{id}/approve", authorize("agent-requests:approve", h.approve))
	mux.HandleFunc("POST /api/v1/agent-requests/{id}/reject", authorize("agent-requests:approve", h.reject))
}

// visible reports whether a reader with scope may see r: it names no node
// outside the scope, and a plan that picks its own nodes is shown only to a
// reader who may see every node.
func visible(r Request, scope nodescope.Scope) bool {
	if scope.All() {
		return true
	}
	if len(r.Targets) == 0 {
		return false
	}
	return len(scope.Outside(r.Targets)) == 0
}

// readerScope is the caller's node scope for reading, answering 502 when it
// cannot be worked out: nothing is shown on an unknown scope.
func (h *Handlers) readerScope(w http.ResponseWriter, r *http.Request) (nodescope.Scope, bool) {
	scope, err := h.scope(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not determine which nodes you may see: "+err.Error())
		return nodescope.Scope{}, false
	}
	return scope, true
}

func (h *Handlers) list(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.readerScope(w, r)
	if !ok {
		return
	}
	filter := Filter{Status: Status(r.URL.Query().Get("status"))}
	switch filter.Status {
	case "", StatusPending, StatusApproved, StatusRejected, StatusExpired, StatusFailed:
	default:
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	limit := 100
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 200 {
		limit = n
	}
	// Read more than asked for when scoped, since some will be filtered out.
	fetch := limit
	if !scope.All() {
		fetch = 200
	}
	all, err := h.service.store.List(r.Context(), filter, fetch)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := make([]Request, 0, len(all))
	for _, req := range all {
		if visible(req, scope) && len(out) < limit {
			out = append(out, req)
		}
	}
	h.recordAuditRead(r, auditlog.Event{Action: "agent_request.list.viewed", ResourceType: "agent_request"})
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handlers) pendingCount(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.readerScope(w, r)
	if !ok {
		return
	}
	pending, err := h.service.store.List(r.Context(), Filter{Status: StatusPending}, 200)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	n := 0
	for _, req := range pending {
		if visible(req, scope) {
			n++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": n})
}

func requestID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func (h *Handlers) get(w http.ResponseWriter, r *http.Request) {
	id, ok := requestID(w, r)
	if !ok {
		return
	}
	scope, ok := h.readerScope(w, r)
	if !ok {
		return
	}
	req, err := h.service.store.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) || (err == nil && !visible(req, scope)) {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.recordAuditRead(r, auditlog.Event{Action: "agent_request.viewed", ResourceType: "agent_request", ResourceID: strconv.FormatInt(id, 10)})
	writeJSON(w, http.StatusOK, req)
}

type decision struct {
	Note string `json:"note"`
}

func (h *Handlers) decide(w http.ResponseWriter, r *http.Request, do func(id int64, a Approver, note string) (Request, error)) {
	id, ok := requestID(w, r)
	if !ok {
		return
	}
	var body decision
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	if len(body.Note) > 1000 {
		writeError(w, http.StatusBadRequest, "the note is too long (at most 1000 characters)")
		return
	}
	// A reader who may not see the request may not decide it either, and is
	// told no more than a reader would be.
	scope, ok := h.readerScope(w, r)
	if !ok {
		return
	}
	existing, err := h.service.store.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) || (err == nil && !visible(existing, scope)) {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	approver, err := h.approver(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not determine what you may run: "+err.Error())
		return
	}
	req, err := do(id, approver, body.Note)
	var refusal *Refusal
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "request not found")
	case errors.Is(err, ErrNotPending):
		writeError(w, http.StatusConflict, "the request has already been decided or has expired")
	case errors.As(err, &refusal):
		// A failure after approval is the request's own state; any other is a refusal.
		status := http.StatusForbidden
		if req.Status == StatusFailed {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]any{"error": refusal.Reason, "request": nilIfZero(req)})
	case err != nil:
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		writeJSON(w, http.StatusOK, req)
	}
}

func nilIfZero(r Request) *Request {
	if r.ID == 0 {
		return nil
	}
	return &r
}

func (h *Handlers) approve(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, func(id int64, a Approver, note string) (Request, error) {
		return h.service.Approve(r.Context(), id, a, note)
	})
}

func (h *Handlers) reject(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, func(id int64, a Approver, note string) (Request, error) {
		return h.service.Reject(r.Context(), id, a, note)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
