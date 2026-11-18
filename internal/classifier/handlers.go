package classifier

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

// store is the subset of *Store this package's handlers need, so they can
// be tested against a fake without a live Postgres.
type store interface {
	CreateGroup(ctx context.Context, g Group) (Group, error)
	GetGroup(ctx context.Context, id int64) (Group, error)
	ListGroups(ctx context.Context, page, pageSize int) ([]Group, int, error)
	ListAllGroups(ctx context.Context) ([]Group, error)
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

	// checkClasses compares a group's classes with its environment's
	// code - see SetClassChecker. Nil means groups are never checked.
	checkClasses ClassChecker
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

// groupResponse is a group as the API returns it: the stored group plus
// what is worked out from the whole tree rather than stored.
type groupResponse struct {
	Group

	// Priority shadows Group.Priority so the root, which has none, is
	// returned without one instead of with a misleading zero.
	Priority *int `json:"priority,omitempty"`

	// Warnings lists the values on which this group overrides a
	// higher-priority ancestor. Always present, empty when there are
	// none.
	Warnings []Warning `json:"warnings"`

	// OverriddenBy is set on the response to an update only: the
	// descendants that override this group as it now stands, since
	// this save may be what put them in that state.
	OverriddenBy []Override `json:"overriddenBy,omitempty"`

	// ClassEnvironment is the environment this group's classes are
	// looked up in: its own, else its nearest ancestor's, else
	// production.
	ClassEnvironment string `json:"classEnvironment"`

	// ClassWarnings lists the classes, parameters and values that do
	// not match ClassEnvironment's code. Always present, empty when
	// there are none - or when they could not be checked, which
	// ClassesChecked tells apart.
	ClassWarnings []ClassWarning `json:"classWarnings"`

	// ClassesChecked is true when the group's classes were compared
	// with the code. False for a group with no classes, and whenever
	// class discovery is unavailable.
	ClassesChecked bool `json:"classesChecked"`
}

func newGroupResponse(g Group, warnings map[int64][]Warning) groupResponse {
	resp := groupResponse{Group: g, Warnings: warnings[g.ID]}
	if resp.Warnings == nil {
		resp.Warnings = []Warning{}
	}
	if !g.IsRoot {
		priority := g.Priority
		resp.Priority = &priority
	}
	return resp
}

// allWarnings works out every group's override warnings. They depend on
// a group's ancestors, so they need the whole set, not the group alone.
// A failure to load it is not allowed to fail the request the warnings
// are decorating: the groups are returned without them and the failure
// is logged.
func (h *Handlers) allWarnings(r *http.Request) ([]Group, map[int64][]Warning) {
	all, err := h.store.ListAllGroups(r.Context())
	if err != nil {
		slog.WarnContext(r.Context(), "could not load groups to work out override warnings", "error", err)
		return nil, map[int64][]Warning{}
	}
	return all, Warnings(all)
}

// listGroups returns one page of groups ordered by priority, or with
// ?all=true every group in one response, which is what drawing the
// hierarchy needs.
func (h *Handlers) listGroups(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("all") == "true" {
		groups, err := h.store.ListAllGroups(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		warnings := Warnings(groups)
		items := make([]groupResponse, len(groups))
		for i, g := range groups {
			items[i] = h.withClassWarnings(r.Context(), groups, newGroupResponse(g, warnings))
		}
		h.recordAuditRead(r, auditlog.Event{Action: "group.list.viewed", ResourceType: "group"})
		writeJSON(w, http.StatusOK, pagination.New(items, 1, len(items), len(items)))
		return
	}

	page, pageSize := pagination.Params(r)
	groups, total, err := h.store.ListGroups(r.Context(), page, pageSize)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	all, warnings := h.allWarnings(r)
	items := make([]groupResponse, len(groups))
	for i, g := range groups {
		items[i] = h.withClassWarnings(r.Context(), all, newGroupResponse(g, warnings))
	}
	h.recordAuditRead(r, auditlog.Event{Action: "group.list.viewed", ResourceType: "group"})
	writeJSON(w, http.StatusOK, pagination.New(items, page, pageSize, total))
}

// overrideSummary describes, for the activity log and the server log,
// the overrides a save has left in place: the ancestors this group
// overrides, and the descendants overriding it. Empty when there are
// none.
func overrideSummary(warnings []Warning, overriddenBy []Override) string {
	var parts []string
	if len(warnings) > 0 {
		seen := map[string]bool{}
		var ancestors []string
		for _, w := range warnings {
			if !seen[w.AncestorName] {
				seen[w.AncestorName] = true
				ancestors = append(ancestors, w.AncestorName)
			}
		}
		sort.Strings(ancestors)
		parts = append(parts, "it overrides higher-priority ancestor "+strings.Join(ancestors, ", "))
	}
	if len(overriddenBy) > 0 {
		descendants := make([]string, len(overriddenBy))
		for i, o := range overriddenBy {
			descendants[i] = o.GroupName
		}
		parts = append(parts, "it is overridden by lower-priority descendant "+strings.Join(descendants, ", "))
	}
	return strings.Join(parts, "; ")
}

// recordSave records a create or update in the activity log, and - when
// the save leaves a group overriding a higher-priority ancestor - says
// so there and in the server log. Reported, never refused: the
// descendant's value is the one used either way.
func (h *Handlers) recordSave(r *http.Request, action, verb string, resp groupResponse) {
	summary := verb + " group " + resp.Name
	if overrides := overrideSummary(resp.Warnings, resp.OverriddenBy); overrides != "" {
		summary += " (" + overrides + ")"
		slog.WarnContext(r.Context(), "node group hierarchy overrides priority",
			"group", resp.Name, "group_id", resp.ID, "detail", overrides)
	}
	h.recordActivity(r, action, summary)
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
	all, warnings := h.allWarnings(r)
	resp := h.withClassWarnings(r.Context(), all, newGroupResponse(created, warnings))
	h.recordSave(r, "group.created", "created", resp)
	h.recordAudit(r, auditlog.Event{Action: "group.created", ResourceType: "group", ResourceID: created.Name, After: auditOf(created, nil)})
	writeJSON(w, http.StatusCreated, resp)
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
	all, warnings := h.allWarnings(r)
	h.recordAuditRead(r, auditlog.Event{Action: "group.viewed", ResourceType: "group", ResourceID: g.Name})
	writeJSON(w, http.StatusOK, h.withClassWarnings(r.Context(), all, newGroupResponse(g, warnings)))
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

	// Read back rather than echoing the request: the store fills in a
	// parent the request left out and the rule match mode, and keeps
	// the root's fixed fields whatever was sent.
	after, err := h.store.GetGroup(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	all, warnings := h.allWarnings(r)
	resp := h.withClassWarnings(r.Context(), all, newGroupResponse(after, warnings))
	resp.OverriddenBy = OverridesOf(all, id)

	h.recordSave(r, "group.updated", "updated", resp)
	h.recordAudit(r, auditlog.Event{Action: "group.updated", ResourceType: "group", ResourceID: after.Name, Before: auditOf(before, nil), After: auditOf(after, &before)})
	writeJSON(w, http.StatusOK, resp)
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
	h.recordAudit(r, auditlog.Event{Action: "group.deleted", ResourceType: "group", ResourceID: g.Name, Before: auditOf(g, nil)})
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
	var cfg *ConfigDataError
	switch {
	case errors.As(err, &cfg):
		// The field is named so the page can show the reason on it.
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  err.Error(),
			"fields": map[string]string{"configData": err.Error()},
		})
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, ErrDuplicateName), errors.Is(err, ErrDuplicatePriority),
		errors.Is(err, ErrHasChildren), errors.Is(err, ErrHierarchyLoop), errors.Is(err, ErrGroupGranted):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, ErrParentNotFound), errors.Is(err, ErrInvalidRuleMatch), errors.Is(err, ErrRootImmutable):
		writeError(w, http.StatusBadRequest, err)
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

// auditedGroup is a group as the audit log records it: everything but its
// configuration data, which is recorded as key names and never as values.
// People put credentials in data that is not meant for them, and the audit
// log is read by more people than the groups are. (ConfigData here shadows
// Group's and is always omitted.)
type auditedGroup struct {
	Group
	ConfigData       *struct{}         `json:"configData,omitempty"`
	ConfigDataKeys   []string          `json:"configDataKeys,omitempty"`
	ConfigDataChange *ConfigDataChange `json:"configDataChange,omitempty"`
}

// auditOf prepares g for the audit log. When previous is set, the keys
// that differ from it are listed as well.
func auditOf(g Group, previous *Group) auditedGroup {
	a := auditedGroup{Group: g}
	a.Group.ConfigData = nil
	for k := range g.ConfigData {
		a.ConfigDataKeys = append(a.ConfigDataKeys, k)
	}
	sort.Strings(a.ConfigDataKeys)
	if previous != nil {
		if change := DiffConfigData(previous.ConfigData, g.ConfigData); !change.Empty() {
			a.ConfigDataChange = &change
		}
	}
	return a
}
