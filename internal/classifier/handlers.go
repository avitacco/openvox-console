package classifier

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

// store is the subset of *Store this package's handlers need, so they can
// be tested against a fake without a live Postgres.
type store interface {
	CreateGroup(ctx context.Context, g Group) (Group, error)
	GetGroup(ctx context.Context, id int64) (Group, error)
	ListGroups(ctx context.Context, page, pageSize int) ([]Group, int, error)
	UpdateGroup(ctx context.Context, g Group) error
	DeleteGroup(ctx context.Context, id int64) error
}

// Handlers serves the node group management HTTP API.
type Handlers struct {
	store          store
	recordActivity func(r *http.Request, action, summary string)

	// recordAudit/recordAuditRead are the configurable-audit-logging
	// capability's equivalent of recordActivity (write and read/view
	// events respectively, bound to the classifier category) - see
	// design.md in that change for why these are injected functions
	// rather than an internal/auditlog import.
	recordAudit     func(r *http.Request, event auditlog.Event)
	recordAuditRead func(r *http.Request, event auditlog.Event)
}

// NewHandlers builds Handlers backed by s. recordActivity is called after
// every successful create/update/delete, so a group change - which can
// change how nodes are classified - is recorded in the activity log; see
// design.md in the phase-4-activity-and-audit-log change for why this is
// an injected function rather than an internal/activity import.
func NewHandlers(s store, recordActivity func(r *http.Request, action, summary string), recordAudit, recordAuditRead func(r *http.Request, event auditlog.Event)) *Handlers {
	return &Handlers{store: s, recordActivity: recordActivity, recordAudit: recordAudit, recordAuditRead: recordAuditRead}
}

// Register wires this package's routes onto mux: read endpoints require
// classifier:read, write endpoints require classifier:write - see
// internal/rbac.Verifier.Authorize for what authorize does.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/groups", authorize("classifier:read", h.listGroups))
	mux.HandleFunc("POST /api/v1/groups", authorize("classifier:write", h.createGroup))
	mux.HandleFunc("GET /api/v1/groups/{id}", authorize("classifier:read", h.getGroup))
	mux.HandleFunc("PUT /api/v1/groups/{id}", authorize("classifier:write", h.updateGroup))
	mux.HandleFunc("DELETE /api/v1/groups/{id}", authorize("classifier:write", h.deleteGroup))
}

func (h *Handlers) listGroups(w http.ResponseWriter, r *http.Request) {
	page, pageSize := pagination.Params(r)
	groups, total, err := h.store.ListGroups(r.Context(), page, pageSize)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	h.recordAuditRead(r, auditlog.Event{Action: "group.list.viewed", ResourceType: "group"})
	writeJSON(w, http.StatusOK, pagination.New(groups, page, pageSize, total))
}

func (h *Handlers) createGroup(w http.ResponseWriter, r *http.Request) {
	var g Group
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid request body: "+err.Error()))
		return
	}

	created, err := h.store.CreateGroup(r.Context(), g)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	h.recordActivity(r, "group.created", "created group "+created.Name)
	h.recordAudit(r, auditlog.Event{Action: "group.created", ResourceType: "group", ResourceID: created.Name, After: created})
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handlers) getGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	g, err := h.store.GetGroup(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	h.recordAuditRead(r, auditlog.Event{Action: "group.viewed", ResourceType: "group", ResourceID: g.Name})
	writeJSON(w, http.StatusOK, g)
}

func (h *Handlers) updateGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	// Fetched before the update purely to record what changed - see
	// specs/audit-log-emission's before/after requirement.
	before, err := h.store.GetGroup(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	var g Group
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid request body: "+err.Error()))
		return
	}
	g.ID = id

	if err := h.store.UpdateGroup(r.Context(), g); err != nil {
		writeStoreError(w, err)
		return
	}
	h.recordActivity(r, "group.updated", "updated group "+g.Name)
	h.recordAudit(r, auditlog.Event{Action: "group.updated", ResourceType: "group", ResourceID: g.Name, Before: before, After: g})
	writeJSON(w, http.StatusOK, g)
}

func (h *Handlers) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	// Fetched before deletion purely so the activity summary can name the
	// group rather than only its id - not required for the delete itself.
	g, err := h.store.GetGroup(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	if err := h.store.DeleteGroup(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	h.recordActivity(r, "group.deleted", "deleted group "+g.Name)
	h.recordAudit(r, auditlog.Event{Action: "group.deleted", ResourceType: "group", ResourceID: g.Name, Before: g})
	w.WriteHeader(http.StatusNoContent)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid group id"))
		return 0, false
	}
	return id, true
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, ErrDuplicateName), errors.Is(err, ErrDuplicatePriority):
		writeError(w, http.StatusConflict, err)
	default:
		writeError(w, http.StatusBadGateway, err)
	}
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
