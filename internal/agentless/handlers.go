package agentless

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
)

// Checker connects to a target and reports what it found. *Executor is one.
type Checker interface {
	Check(ctx context.Context, name string) (CheckResult, error)
}

// Handlers serves the agentless API.
type Handlers struct {
	store   *Store
	checker Checker
	audit   func(r *http.Request, event auditlog.Event)
	actor   func(r *http.Request) string
	scope   nodescope.Func
	// nodeKnown says whether name is already a node's: its certname in
	// openvoxdb or the CA's registry. Optional.
	nodeKnown func(ctx context.Context, name string) (bool, error)
	// changed is called after a target is created or deleted, for the
	// router's cache.
	changed func()
}

// NewHandlers builds Handlers. audit records an action in the nodes
// category; actor names the caller.
func NewHandlers(store *Store, checker Checker, audit func(r *http.Request, event auditlog.Event), actor func(r *http.Request) string) *Handlers {
	return &Handlers{store: store, checker: checker, audit: audit, actor: actor, scope: nodescope.Unscoped}
}

// SetScope limits each route to the caller's node group scope for the
// permission that gates it.
func (h *Handlers) SetScope(scope nodescope.Func) { h.scope = scope }

// SetNodeKnown refuses a target named like a node that already exists.
func (h *Handlers) SetNodeKnown(f func(ctx context.Context, name string) (bool, error)) {
	h.nodeKnown = f
}

// SetChanged registers what to call after the set of targets changes.
func (h *Handlers) SetChanged(f func()) { h.changed = f }

// Register wires the routes. See internal/rbac.Verifier.Authorize.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/agentless/targets", authorize("nodes:read", h.listTargets))
	mux.HandleFunc("GET /api/v1/agentless/targets/{id}", authorize("nodes:read", h.getTarget))
	mux.HandleFunc("POST /api/v1/agentless/targets", authorize("agentless:manage", h.createTarget))
	mux.HandleFunc("PUT /api/v1/agentless/targets/{id}", authorize("agentless:manage", h.updateTarget))
	mux.HandleFunc("DELETE /api/v1/agentless/targets/{id}", authorize("agentless:manage", h.deleteTarget))
	mux.HandleFunc("POST /api/v1/agentless/targets/{id}/check", authorize("orchestrator:run", h.checkTarget))
	mux.HandleFunc("POST /api/v1/agentless/targets/{id}/host-key/accept", authorize("agentless:manage", h.acceptKey))
	mux.HandleFunc("POST /api/v1/agentless/targets/{id}/host-key/replace", authorize("agentless:manage", h.replaceKey))
	mux.HandleFunc("GET /api/v1/agentless/credentials", authorize("agentless:manage", h.listCredentials))
	mux.HandleFunc("GET /api/v1/agentless/credentials/{id}", authorize("agentless:manage", h.getCredential))
	mux.HandleFunc("POST /api/v1/agentless/credentials", authorize("agentless:manage", h.createCredential))
	mux.HandleFunc("PUT /api/v1/agentless/credentials/{id}", authorize("agentless:manage", h.updateCredential))
	mux.HandleFunc("DELETE /api/v1/agentless/credentials/{id}", authorize("agentless:manage", h.deleteCredential))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// fail answers a store error. A secret is never part of what is said.
func fail(w http.ResponseWriter, err error) {
	var invalid *InvalidError
	var inUse *InUseError
	switch {
	case errors.As(err, &invalid):
		body := map[string]string{"error": invalid.Reason}
		if invalid.Field != "" {
			body["field"] = invalid.Field
		}
		writeJSON(w, http.StatusBadRequest, body)
	case errors.As(err, &inUse):
		writeJSON(w, http.StatusConflict, map[string]any{"error": inUse.Error(), "targets": inUse.Targets})
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrNameTaken):
		writeJSON(w, http.StatusConflict, map[string]string{"error": ErrNameTaken.Error(), "field": "name"})
	case errors.Is(err, ErrNoSecretsKey):
		writeError(w, http.StatusConflict, ErrNoSecretsKey.Error())
	case errors.Is(err, ErrKeyNotPending):
		writeJSON(w, http.StatusConflict, map[string]string{"error": ErrKeyNotPending.Error(), "field": "fingerprint"})
	default:
		writeError(w, http.StatusInternalServerError, "the request could not be carried out")
	}
}

func (h *Handlers) scopeOf(w http.ResponseWriter, r *http.Request) (nodescope.Scope, bool) {
	s, err := h.scope(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not determine which nodes you may act on: "+err.Error())
		return nodescope.Scope{}, false
	}
	return s, true
}

func idOf(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// target loads the target the path names for a caller with scope. One
// outside the caller's nodes reads as one that does not exist.
func (h *Handlers) target(w http.ResponseWriter, r *http.Request) (Target, nodescope.Scope, bool) {
	scope, ok := h.scopeOf(w, r)
	if !ok {
		return Target{}, scope, false
	}
	id, ok := idOf(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return Target{}, scope, false
	}
	t, err := h.store.GetTarget(r.Context(), id)
	if err == nil && !scope.Allows(t.Name) {
		err = ErrNotFound
	}
	if err != nil {
		fail(w, err)
		return Target{}, scope, false
	}
	return t, scope, true
}

func (h *Handlers) listTargets(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.scopeOf(w, r)
	if !ok {
		return
	}
	all, err := h.store.ListTargets(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	items := make([]Target, 0, len(all))
	for _, t := range all {
		if scope.Allows(t.Name) {
			items = append(items, t)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (h *Handlers) getTarget(w http.ResponseWriter, r *http.Request) {
	if t, _, ok := h.target(w, r); ok {
		writeJSON(w, http.StatusOK, t)
	}
}

// targetBody is a create or update request. A secret left out is
// unchanged on update; given empty it is removed.
type targetBody struct {
	Name         string  `json:"name"`
	Address      string  `json:"address"`
	Port         int     `json:"port"`
	CredentialID *int64  `json:"credentialId"`
	Username     string  `json:"username"`
	RunAs        string  `json:"runAs"`
	PrivateKey   *string `json:"privateKey"`
	Passphrase   *string `json:"passphrase"`
	Password     *string `json:"password"`
}

func (b targetBody) input() TargetInput {
	return TargetInput{
		Name: b.Name, Address: b.Address, Port: b.Port, CredentialID: b.CredentialID,
		Login: Login{Username: b.Username, RunAs: b.RunAs, Secrets: Secrets{PrivateKey: b.PrivateKey, Passphrase: b.Passphrase, Password: b.Password}},
	}
}

// changedSecrets names the secrets a request sets, never their values.
func changedSecrets(s Secrets) []string {
	var out []string
	if s.PrivateKey != nil {
		out = append(out, "privateKey")
	}
	if s.Passphrase != nil {
		out = append(out, "passphrase")
	}
	if s.Password != nil {
		out = append(out, "password")
	}
	return out
}

// view is a target as audit shows it: no secret, and no more than
// reading it would.
func view(t Target) map[string]any {
	return map[string]any{
		"address": t.Address, "port": t.Port, "username": t.Username, "runAs": t.RunAs,
		"credential": t.CredentialName, "hasKey": t.HasKey, "hasPassword": t.HasPassword, "keyFingerprint": t.KeyFingerprint,
	}
}

// sharedAllowed refuses a caller whose agentless:manage covers only some
// node groups: a shared credential belongs to no node, so naming one is
// for a caller who may manage every node.
func sharedAllowed(w http.ResponseWriter, scope nodescope.Scope) bool {
	if scope.All() {
		return true
	}
	writeError(w, http.StatusForbidden, "shared credentials can only be managed or used by a caller whose agentless:manage covers every node")
	return false
}

func (h *Handlers) createTarget(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.scopeOf(w, r)
	if !ok {
		return
	}
	var body targetBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// The caller typed the name, so saying it is outside their groups
	// reveals nothing.
	if outside := scope.Outside([]string{body.Name}); len(outside) > 0 && body.Name != "" {
		nodescope.Refuse(w, outside)
		return
	}
	if body.CredentialID != nil && !sharedAllowed(w, scope) {
		return
	}
	if h.nodeKnown != nil && body.Name != "" {
		known, err := h.nodeKnown(r.Context(), body.Name)
		if err != nil {
			writeError(w, http.StatusBadGateway, "could not check whether that name is already a node: "+err.Error())
			return
		}
		if known {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "a node with that name already exists", "field": "name"})
			return
		}
	}
	t, err := h.store.CreateTarget(r.Context(), body.input(), h.actor(r))
	if err != nil {
		fail(w, err)
		return
	}
	h.audit(r, auditlog.Event{Action: "agentless.target.created", ResourceType: "agentless-target", ResourceID: t.Name,
		After: mergeInto(view(t), map[string]any{"secretsSet": changedSecrets(body.input().Login.Secrets)})})
	h.notify()
	writeJSON(w, http.StatusCreated, t)
}

func mergeInto(a, b map[string]any) map[string]any {
	for k, v := range b {
		a[k] = v
	}
	return a
}

func (h *Handlers) notify() {
	if h.changed != nil {
		h.changed()
	}
}

func (h *Handlers) updateTarget(w http.ResponseWriter, r *http.Request) {
	before, scope, ok := h.target(w, r)
	if !ok {
		return
	}
	var body targetBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.CredentialID != nil && !sharedAllowed(w, scope) {
		return
	}
	if body.Address == "" {
		body.Address = before.Address
	}
	if body.Port == 0 {
		body.Port = before.Port
	}
	t, err := h.store.UpdateTarget(r.Context(), before.ID, body.input(), h.actor(r))
	if err != nil {
		fail(w, err)
		return
	}
	h.audit(r, auditlog.Event{Action: "agentless.target.updated", ResourceType: "agentless-target", ResourceID: t.Name,
		Before: view(before), After: mergeInto(view(t), map[string]any{"secretsSet": changedSecrets(body.input().Login.Secrets)})})
	writeJSON(w, http.StatusOK, t)
}

func (h *Handlers) deleteTarget(w http.ResponseWriter, r *http.Request) {
	t, _, ok := h.target(w, r)
	if !ok {
		return
	}
	if err := h.store.DeleteTarget(r.Context(), t.ID); err != nil {
		fail(w, err)
		return
	}
	h.audit(r, auditlog.Event{Action: "agentless.target.deleted", ResourceType: "agentless-target", ResourceID: t.Name, Before: view(t)})
	h.notify()
	w.WriteHeader(http.StatusNoContent)
}

// checkResponse is what a check answers.
type checkResponse struct {
	OK         bool   `json:"ok"`
	Stage      Stage  `json:"stage,omitempty"`
	Reason     string `json:"reason,omitempty"`
	LoggedInAs string `json:"loggedInAs,omitempty"`
	RunAs      string `json:"runAs,omitempty"`
	Presented  string `json:"presented,omitempty"`
	NewKey     bool   `json:"newKey,omitempty"`
	Target     Target `json:"target"`
}

func (h *Handlers) checkTarget(w http.ResponseWriter, r *http.Request) {
	t, _, ok := h.target(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	res, err := h.checker.Check(ctx, t.Name)
	if err != nil {
		fail(w, err)
		return
	}
	stage, reason := string(res.Stage), res.Reason
	if res.OK {
		stage, reason = "", ""
	}
	if err := h.store.RecordCheck(r.Context(), t.ID, res.OK, stage, reason); err != nil {
		fail(w, err)
		return
	}
	h.audit(r, auditlog.Event{Action: "agentless.target.checked", ResourceType: "agentless-target", ResourceID: t.Name,
		After: map[string]any{"ok": res.OK, "stage": stage}})
	fresh, err := h.store.GetTarget(r.Context(), t.ID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, checkResponse{OK: res.OK, Stage: res.Stage, Reason: res.Reason, LoggedInAs: res.LoggedInAs, RunAs: res.RunAs, Presented: res.Presented, NewKey: res.NewKey, Target: fresh})
}

type fingerprintBody struct {
	Fingerprint string `json:"fingerprint"`
}

func (h *Handlers) pinKey(w http.ResponseWriter, r *http.Request, replace bool) {
	before, _, ok := h.target(w, r)
	if !ok {
		return
	}
	var body fingerprintBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Fingerprint == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "give the fingerprint you were shown", "field": "fingerprint"})
		return
	}
	var t Target
	var err error
	action := "agentless.hostKey.accepted"
	if replace {
		action = "agentless.hostKey.replaced"
		t, err = h.store.ReplaceKey(r.Context(), before.ID, body.Fingerprint)
	} else {
		t, err = h.store.AcceptKey(r.Context(), before.ID, body.Fingerprint)
	}
	if err != nil {
		fail(w, err)
		return
	}
	h.audit(r, auditlog.Event{Action: action, ResourceType: "agentless-target", ResourceID: t.Name,
		Before: map[string]any{"pinned": before.HostKey.Pinned}, After: map[string]any{"pinned": t.HostKey.Pinned}})
	writeJSON(w, http.StatusOK, t)
}

func (h *Handlers) acceptKey(w http.ResponseWriter, r *http.Request)  { h.pinKey(w, r, false) }
func (h *Handlers) replaceKey(w http.ResponseWriter, r *http.Request) { h.pinKey(w, r, true) }

// --- shared credentials ---

func (h *Handlers) fleetWide(w http.ResponseWriter, r *http.Request) bool {
	scope, ok := h.scopeOf(w, r)
	return ok && sharedAllowed(w, scope)
}

func (h *Handlers) listCredentials(w http.ResponseWriter, r *http.Request) {
	if !h.fleetWide(w, r) {
		return
	}
	items, err := h.store.ListCredentials(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (h *Handlers) getCredential(w http.ResponseWriter, r *http.Request) {
	if !h.fleetWide(w, r) {
		return
	}
	id, ok := idOf(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	c, err := h.store.GetCredential(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

type credentialBody struct {
	Name       string  `json:"name"`
	Username   string  `json:"username"`
	RunAs      string  `json:"runAs"`
	PrivateKey *string `json:"privateKey"`
	Passphrase *string `json:"passphrase"`
	Password   *string `json:"password"`
}

func (b credentialBody) login() Login {
	return Login{Username: b.Username, RunAs: b.RunAs, Secrets: Secrets{PrivateKey: b.PrivateKey, Passphrase: b.Passphrase, Password: b.Password}}
}

func credentialView(c Credential) map[string]any {
	return map[string]any{"username": c.Username, "runAs": c.RunAs, "hasKey": c.HasKey, "hasPassword": c.HasPassword, "keyFingerprint": c.KeyFingerprint}
}

func (h *Handlers) createCredential(w http.ResponseWriter, r *http.Request) {
	if !h.fleetWide(w, r) {
		return
	}
	var body credentialBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := h.store.CreateCredential(r.Context(), body.Name, body.login(), h.actor(r))
	if err != nil {
		fail(w, err)
		return
	}
	h.audit(r, auditlog.Event{Action: "agentless.credential.created", ResourceType: "agentless-credential", ResourceID: c.Name,
		After: mergeInto(credentialView(c), map[string]any{"secretsSet": changedSecrets(body.login().Secrets)})})
	writeJSON(w, http.StatusCreated, c)
}

func (h *Handlers) updateCredential(w http.ResponseWriter, r *http.Request) {
	if !h.fleetWide(w, r) {
		return
	}
	id, ok := idOf(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	before, err := h.store.GetCredential(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	var body credentialBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := h.store.UpdateCredential(r.Context(), id, body.login(), h.actor(r))
	if err != nil {
		fail(w, err)
		return
	}
	h.audit(r, auditlog.Event{Action: "agentless.credential.updated", ResourceType: "agentless-credential", ResourceID: c.Name,
		Before: credentialView(before), After: mergeInto(credentialView(c), map[string]any{"secretsSet": changedSecrets(body.login().Secrets)})})
	writeJSON(w, http.StatusOK, c)
}

func (h *Handlers) deleteCredential(w http.ResponseWriter, r *http.Request) {
	if !h.fleetWide(w, r) {
		return
	}
	id, ok := idOf(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	before, err := h.store.GetCredential(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	if err := h.store.DeleteCredential(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	h.audit(r, auditlog.Event{Action: "agentless.credential.deleted", ResourceType: "agentless-credential", ResourceID: before.Name, Before: credentialView(before)})
	w.WriteHeader(http.StatusNoContent)
}
