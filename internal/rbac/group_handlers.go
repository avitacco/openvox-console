package rbac

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
)

// registerGroups wires the user group routes. Like users and roles they
// need rbac:admin: a group is a way of granting roles, so managing one is
// managing access.
func (h *Handlers) registerGroups(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/user-groups", h.verifier.Authorize("rbac:admin", h.listGroups))
	mux.HandleFunc("POST /api/v1/user-groups", h.verifier.Authorize("rbac:admin", h.createGroup))
	mux.HandleFunc("GET /api/v1/user-groups/{id}", h.verifier.Authorize("rbac:admin", h.getGroup))
	mux.HandleFunc("PUT /api/v1/user-groups/{id}", h.verifier.Authorize("rbac:admin", h.renameGroup))
	mux.HandleFunc("DELETE /api/v1/user-groups/{id}", h.verifier.Authorize("rbac:admin", h.deleteGroup))
	mux.HandleFunc("POST /api/v1/user-groups/{id}/members/{userId}", h.verifier.Authorize("rbac:admin", h.addGroupMember))
	mux.HandleFunc("DELETE /api/v1/user-groups/{id}/members/{userId}", h.verifier.Authorize("rbac:admin", h.removeGroupMember))
	mux.HandleFunc("POST /api/v1/user-groups/{id}/roles/{roleId}", h.verifier.Authorize("rbac:admin", h.grantGroupRole))
	mux.HandleFunc("DELETE /api/v1/user-groups/{id}/roles/{roleId}", h.verifier.Authorize("rbac:admin", h.revokeGroupRole))
	mux.HandleFunc("GET /api/v1/users/{id}/groups", h.verifier.Authorize("rbac:admin", h.listUserGroups))
}

type groupRequest struct {
	Name string `json:"name"`
}

// groupAudit is what a group change records. It never carries anything
// about a user beyond the name.
type groupAudit struct {
	Name        string `json:"name,omitempty"`
	MemberCount *int   `json:"memberCount,omitempty"`
	User        string `json:"user,omitempty"`
	Role        string `json:"role,omitempty"`
}

// writeGroupError answers an error from the group store.
func writeGroupError(w http.ResponseWriter, err error) {
	var has *GroupHasMembersError
	switch {
	case errors.As(err, &has):
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "memberCount": has.Members})
	case errors.Is(err, ErrLastAdmin):
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "reason": "last-admin"})
	case errors.Is(err, ErrInvalidGroupName):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  err.Error(),
			"fields": map[string]string{"name": err.Error()},
		})
	case errors.Is(err, ErrDuplicateName):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":  err.Error(),
			"fields": map[string]string{"name": err.Error()},
		})
	default:
		writeStoreError(w, err)
	}
}

func (h *Handlers) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.store.ListGroups(r.Context())
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

func (h *Handlers) getGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	g, err := h.store.GetGroup(r.Context(), id)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (h *Handlers) createGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	g, err := h.store.CreateGroup(r.Context(), req.Name)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	h.recordActivity(r, "group.created", "created user group "+g.Name)
	h.recordAudit(r, auditlog.Event{Action: "group.created", ResourceType: "user_group", ResourceID: g.Name})
	writeJSON(w, http.StatusCreated, g)
}

func (h *Handlers) renameGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	before, err := h.store.GetGroup(r.Context(), id)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	var req groupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	g, err := h.store.RenameGroup(r.Context(), id, req.Name)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	h.recordActivity(r, "group.renamed", "renamed user group "+before.Name+" to "+g.Name)
	h.recordAudit(r, auditlog.Event{
		Action: "group.renamed", ResourceType: "user_group", ResourceID: g.Name,
		Before: groupAudit{Name: before.Name}, After: groupAudit{Name: g.Name},
	})
	writeJSON(w, http.StatusOK, g)
}

func (h *Handlers) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	g, err := h.store.GetGroup(r.Context(), id)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	confirm := r.URL.Query().Get("confirm") == "true"
	members, err := h.store.DeleteGroup(r.Context(), id, confirm)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	h.recordActivity(r, "group.deleted", "deleted user group "+g.Name+" ("+strconv.Itoa(members)+" members)")
	h.recordAudit(r, auditlog.Event{
		Action: "group.deleted", ResourceType: "user_group", ResourceID: g.Name,
		Before: groupAudit{Name: g.Name, MemberCount: &members},
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) addGroupMember(w http.ResponseWriter, r *http.Request) {
	h.changeMember(w, r, true)
}

func (h *Handlers) removeGroupMember(w http.ResponseWriter, r *http.Request) {
	h.changeMember(w, r, false)
}

func (h *Handlers) changeMember(w http.ResponseWriter, r *http.Request, add bool) {
	groupID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	userID, ok := pathID(w, r, "userId")
	if !ok {
		return
	}
	g, err := h.store.GetGroup(r.Context(), groupID)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	u, err := h.store.GetUser(r.Context(), userID)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	action, summary := "group.member.removed", "removed "+u.Username+" from user group "+g.Name
	if add {
		err = h.store.AddGroupMember(r.Context(), groupID, userID)
		action, summary = "group.member.added", "added "+u.Username+" to user group "+g.Name
	} else {
		err = h.store.RemoveGroupMember(r.Context(), groupID, userID)
	}
	if err != nil {
		writeGroupError(w, err)
		return
	}
	h.recordActivity(r, action, summary)
	h.recordAudit(r, auditlog.Event{Action: action, ResourceType: "user_group", ResourceID: g.Name, After: groupAudit{User: u.Username}})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) grantGroupRole(w http.ResponseWriter, r *http.Request) {
	h.changeGroupRole(w, r, true)
}

func (h *Handlers) revokeGroupRole(w http.ResponseWriter, r *http.Request) {
	h.changeGroupRole(w, r, false)
}

func (h *Handlers) changeGroupRole(w http.ResponseWriter, r *http.Request, grant bool) {
	groupID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	roleID, ok := pathID(w, r, "roleId")
	if !ok {
		return
	}
	g, err := h.store.GetGroup(r.Context(), groupID)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	role, err := h.store.GetRole(r.Context(), roleID)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	action, summary := "group.role.revoked", "revoked role "+role.Name+" from user group "+g.Name
	if grant {
		err = h.store.GrantGroupRole(r.Context(), groupID, roleID)
		action, summary = "group.role.granted", "granted role "+role.Name+" to user group "+g.Name
	} else {
		err = h.store.RevokeGroupRole(r.Context(), groupID, roleID)
	}
	if err != nil {
		writeGroupError(w, err)
		return
	}
	h.recordActivity(r, action, summary)
	h.recordAudit(r, auditlog.Event{Action: action, ResourceType: "user_group", ResourceID: g.Name, After: groupAudit{Role: role.Name}})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) listUserGroups(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	groups, err := h.store.UserGroups(r.Context(), id)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, groups)
}
