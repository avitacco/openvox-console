package rbac

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// serviceTokenTTL is the expiry given to service tokens - very long
// rather than absent, so verification never has to special-case a
// missing exp claim. Service tokens are revoked explicitly (see
// revokeServiceToken), not left to expire naturally.
func serviceTokenTTL() time.Duration {
	return 100 * 365 * 24 * time.Hour
}

// farFutureExpiry is used when revoking a service token: the revocation
// entry's own expiry just needs to outlive the token, so it's never
// pruned from the in-memory set while the token could still (in theory)
// be presented.
func farFutureExpiry() time.Time {
	return time.Now().Add(serviceTokenTTL())
}

// Handlers serves the rbac HTTP API: login/refresh/logout, OIDC login,
// user and role management, and service token issuance.
type Handlers struct {
	auth           *AuthService
	store          *Store
	verifier       *Verifier
	revoker        *Revoker
	issuer         *Issuer
	oidc           *OIDCService
	recordActivity func(r *http.Request, action, summary string)

	// recordAuditAuth is bound to the auth category (only used here for
	// logout - login/refresh audit events are emitted from AuthService/
	// OIDCService directly, since that's where that logic already lives).
	// recordAudit/recordAuditRead are bound to the rbac category, for
	// every user/role/service-token write and (recordAuditRead) read
	// this file serves. See design.md in the configurable-audit-logging
	// change for why these are injected functions, mirroring
	// recordActivity, rather than an internal/auditlog import; the
	// caller fills in event.Actor from the request, so call sites in
	// this file leave it unset.
	recordAuditAuth func(r *http.Request, event auditlog.Event)
	recordAudit     func(r *http.Request, event auditlog.Event)
	recordAuditRead func(r *http.Request, event auditlog.Event)
}

// NewHandlers builds Handlers. oidc may be an inactive (unconfigured)
// service - see OIDCService.Configured. recordActivity is called after
// every successful user/role/service-token mutation - see design.md in
// the phase-4-activity-and-audit-log change for why this is an injected
// function rather than an internal/activity import.
func NewHandlers(auth *AuthService, store *Store, verifier *Verifier, revoker *Revoker, issuer *Issuer, oidc *OIDCService, recordActivity func(r *http.Request, action, summary string), recordAuditAuth, recordAudit, recordAuditRead func(r *http.Request, event auditlog.Event)) *Handlers {
	return &Handlers{
		auth: auth, store: store, verifier: verifier, revoker: revoker, issuer: issuer, oidc: oidc,
		recordActivity: recordActivity, recordAuditAuth: recordAuditAuth, recordAudit: recordAudit, recordAuditRead: recordAuditRead,
	}
}

// Register wires this package's routes onto mux. Login/refresh/OIDC need
// no prior token (that's the point); everything else requires rbac:admin.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/auth/methods", h.authMethods)
	mux.HandleFunc("POST /api/v1/auth/login", h.login)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", h.verifier.Authenticate(h.logout))
	mux.HandleFunc("GET /api/v1/auth/oidc/login", h.oidcLogin)
	mux.HandleFunc("GET /api/v1/auth/oidc/callback", h.oidcCallback)

	mux.HandleFunc("GET /api/v1/me", h.verifier.Authenticate(h.getMe))
	mux.HandleFunc("PUT /api/v1/me", h.verifier.Authenticate(h.updateMe))

	mux.HandleFunc("GET /api/v1/users", h.verifier.Authorize("rbac:admin", h.listUsers))
	mux.HandleFunc("POST /api/v1/users", h.verifier.Authorize("rbac:admin", h.createUser))
	mux.HandleFunc("GET /api/v1/users/{id}", h.verifier.Authorize("rbac:admin", h.getUser))
	mux.HandleFunc("GET /api/v1/users/{id}/roles", h.verifier.Authorize("rbac:admin", h.listUserRoles))
	mux.HandleFunc("PUT /api/v1/users/{id}", h.verifier.Authorize("rbac:admin", h.updateUser))
	mux.HandleFunc("DELETE /api/v1/users/{id}", h.verifier.Authorize("rbac:admin", h.deleteUser))
	mux.HandleFunc("POST /api/v1/users/{id}/unlock", h.verifier.Authorize("rbac:admin", h.unlockUser))

	// Any signed-in caller may read the policy: the form in which they
	// change their own password shows its rules.
	mux.HandleFunc("GET /api/v1/account-policy", h.verifier.Authenticate(h.getAccountPolicy))
	mux.HandleFunc("PUT /api/v1/account-policy", h.verifier.Authorize("rbac:admin", h.updateAccountPolicy))
	mux.HandleFunc("POST /api/v1/users/{id}/roles/{roleId}", h.verifier.Authorize("rbac:admin", h.assignRole))
	mux.HandleFunc("DELETE /api/v1/users/{id}/roles/{roleId}", h.verifier.Authorize("rbac:admin", h.unassignRole))

	mux.HandleFunc("GET /api/v1/roles", h.verifier.Authorize("rbac:admin", h.listRoles))
	mux.HandleFunc("POST /api/v1/roles", h.verifier.Authorize("rbac:admin", h.createRole))
	mux.HandleFunc("GET /api/v1/roles/{id}", h.verifier.Authorize("rbac:admin", h.getRole))
	mux.HandleFunc("PUT /api/v1/roles/{id}", h.verifier.Authorize("rbac:admin", h.updateRole))
	mux.HandleFunc("DELETE /api/v1/roles/{id}", h.verifier.Authorize("rbac:admin", h.deleteRole))

	h.registerGroups(mux)

	mux.HandleFunc("GET /api/v1/service-tokens", h.verifier.Authorize("rbac:admin", h.listServiceTokens))
	mux.HandleFunc("POST /api/v1/service-tokens", h.verifier.Authorize("rbac:admin", h.createServiceToken))
	mux.HandleFunc("DELETE /api/v1/service-tokens/{id}", h.verifier.Authorize("rbac:admin", h.revokeServiceToken))
}

// --- auth ---

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type tokenPairResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

func (h *Handlers) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	pair, err := h.auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	writeJSON(w, http.StatusOK, tokenPairResponse(pair))
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

func (h *Handlers) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	pair, err := h.auth.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}
	writeJSON(w, http.StatusOK, tokenPairResponse(pair))
}

// authMethodsResponse reports which login methods the frontend should
// offer - the login page shows OIDC only when it's actually configured.
type authMethodsResponse struct {
	Password bool `json:"password"`
	OIDC     bool `json:"oidc"`
}

func (h *Handlers) authMethods(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, authMethodsResponse{Password: true, OIDC: h.oidc.Configured()})
}

// oidcLogin redirects the browser to the configured OIDC provider's
// authorization endpoint, having persisted this login attempt's PKCE
// verifier and nonce (see OIDCService.AuthorizationURL).
func (h *Handlers) oidcLogin(w http.ResponseWriter, r *http.Request) {
	if !h.oidc.Configured() {
		writeJSONError(w, http.StatusServiceUnavailable, "oidc is not configured")
		return
	}
	url, err := h.oidc.AuthorizationURL(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

// oidcCallback completes an OIDC login and hands the issued tokens to
// the frontend via a URL fragment (never sent to the server, not logged)
// - see design.md's "Token delivery" decision.
func (h *Handlers) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if !h.oidc.Configured() {
		writeJSONError(w, http.StatusServiceUnavailable, "oidc is not configured")
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		writeJSONError(w, http.StatusBadRequest, "missing code or state")
		return
	}

	pair, err := h.oidc.HandleCallback(r.Context(), code, state)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "oidc login failed")
		return
	}

	fragment := url.Values{
		"access_token":  {pair.AccessToken},
		"refresh_token": {pair.RefreshToken},
	}
	http.Redirect(w, r, "/oidc-callback.html#"+fragment.Encode(), http.StatusFound)
}

func (h *Handlers) logout(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())
	if err := h.revoker.Revoke(r.Context(), claims.ID, claims.ExpiresAt.Time); err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.recordAuditAuth(r, auditlog.Event{Action: "user.logout", ResourceType: "user", ResourceID: claims.Subject})
	w.WriteHeader(http.StatusNoContent)
}

// getMe returns the caller's own profile - the preferences page's data
// source. Unlike /api/v1/users/{id}, this needs no rbac:admin: any
// logged-in identity may read (and, via updateMe, edit) its own record.
func (h *Handlers) getMe(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())
	u, err := h.store.GetUserByUsername(r.Context(), claims.Subject)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	resp := meResponse{userResponse: toUserResponse(u)}
	scope, err := h.nodeScopeOf(r.Context(), claims)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	resp.NodeScope = scope
	writeJSON(w, http.StatusOK, resp)
}

// meResponse is the caller's own profile plus the node groups they can
// read nodes in, so the console can tell a scoped user what they are
// looking at.
type meResponse struct {
	userResponse
	NodeScope nodeScopeResponse `json:"nodeScope"`
}

// nodeScopeResponse describes a nodes:read scope. All is true when it
// covers every node; Groups then names only the root group, or nothing
// for a token that states no scope.
type nodeScopeResponse struct {
	All    bool           `json:"all"`
	Groups []NodeGroupRef `json:"groups"`
}

// nodeScopeOf describes claims' nodes:read scope. Groups that no longer
// exist are left out, as they contribute no nodes.
func (h *Handlers) nodeScopeOf(ctx context.Context, claims *Claims) (nodeScopeResponse, error) {
	ids, fleetWide := claims.NodeGroupsFor("nodes:read")
	if fleetWide {
		return nodeScopeResponse{All: true, Groups: []NodeGroupRef{}}, nil
	}
	refs, err := h.store.NodeGroupRefs(ctx, ids)
	if err != nil {
		return nodeScopeResponse{}, err
	}
	scope := nodeScopeResponse{Groups: refs}
	for _, ref := range refs {
		if ref.IsRoot {
			scope.All = true
		}
	}
	return scope, nil
}

type updateMeRequest struct {
	FirstName       string `json:"firstName,omitempty"`
	LastName        string `json:"lastName,omitempty"`
	Email           string `json:"email,omitempty"`
	CurrentPassword string `json:"currentPassword,omitempty"`
	NewPassword     string `json:"newPassword,omitempty"`
}

// updateMe lets the caller edit their own profile and, optionally,
// password - the preferences page's write path. A partial request merges
// with the current profile the same way an admin's PUT /users/{id} does
// (see Handlers.updateUser). Changing the password requires the current
// one: unlike the admin endpoint (gated by rbac:admin, a trusted actor
// acting on someone else), here the actor and the target are always the
// same person, so a merely-valid access token (e.g. one lifted from an
// unlocked, unattended browser tab) shouldn't be enough on its own to
// lock the real owner out.
func (h *Handlers) updateMe(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())
	current, err := h.store.GetUserByUsername(r.Context(), claims.Subject)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	var req updateMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	firstName, lastName, email := req.FirstName, req.LastName, req.Email
	if firstName == "" {
		firstName = derefOrEmpty(current.FirstName)
	}
	if lastName == "" {
		lastName = derefOrEmpty(current.LastName)
	}
	if email == "" {
		email = derefOrEmpty(current.Email)
	}
	if err := h.store.UpdateProfile(r.Context(), current.ID, firstName, lastName, email); err != nil {
		writeStoreError(w, err)
		return
	}

	if req.NewPassword != "" {
		// The current password is a guess like a login's: a wrong one
		// is counted, and while the account is locked the change is
		// refused whatever is given.
		wrong := bcrypt.CompareHashAndPassword([]byte(current.PasswordHash), []byte(req.CurrentPassword)) != nil
		if current.Locked() {
			h.recordAuditAuth(r, auditlog.Event{Action: "user.login.locked", ResourceType: "user", ResourceID: current.Username})
			writeJSONError(w, http.StatusUnauthorized, "this account is locked after repeated failed attempts; the password cannot be changed until the lock ends")
			return
		}
		if wrong {
			h.recordAuditAuth(r, auditlog.Event{Action: "user.login.failure", ResourceType: "user", ResourceID: current.Username})
			h.auth.CountFailedPassword(r.Context(), current)
			writeJSONError(w, http.StatusUnauthorized, "current password is incorrect")
			return
		}
		if !h.passwordAllowed(w, r, current.Username, req.NewPassword) {
			return
		}
		if err := h.store.UpdatePassword(r.Context(), current.ID, req.NewPassword); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	h.recordAudit(r, auditlog.Event{Action: "user.profile.updated", ResourceType: "user", ResourceID: current.Username})
	w.WriteHeader(http.StatusNoContent)
}

// --- users ---

type userResponse struct {
	ID        int64   `json:"id"`
	Username  string  `json:"username"`
	FirstName *string `json:"firstName,omitempty"`
	LastName  *string `json:"lastName,omitempty"`
	Email     *string `json:"email,omitempty"`
	// LockedUntil is set while password login is refused for the
	// account after repeated failures.
	LockedUntil *time.Time `json:"lockedUntil,omitempty"`
}

func toUserResponse(u User) userResponse {
	return userResponse{ID: u.ID, Username: u.Username, FirstName: u.FirstName, LastName: u.LastName, Email: u.Email, LockedUntil: u.LockedUntil}
}

// passwordAllowed checks password against the account policy for an
// account named username. It reports false, having answered 400 with
// the rules it fails, when it is refused. The refusal is audited with
// the account and the rules - never the password.
func (h *Handlers) passwordAllowed(w http.ResponseWriter, r *http.Request, username, password string) bool {
	policy, err := h.store.AccountPolicy(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return false
	}
	err = checkPassword(policy, username, password)
	if err == nil {
		return true
	}
	var refused *PasswordRuleError
	errors.As(err, &refused)
	h.recordAudit(r, auditlog.Event{Action: "user.password.rejected", ResourceType: "user", ResourceID: username,
		After: map[string]any{"rules": refused.Rules}})
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"error":  err.Error(),
		"fields": map[string]string{"password": err.Error()},
		"rules":  refused.Rules,
	})
	return false
}

// unlock ends user's lock and clears their failed-login count,
// recording it when the account was in fact locked.
func (h *Handlers) unlock(r *http.Request, user User) error {
	wasLocked, err := h.store.ResetFailedLogins(r.Context(), user.ID)
	if err != nil {
		return err
	}
	if wasLocked {
		h.recordActivity(r, "user.unlocked", "unlocked account "+user.Username)
		h.recordAudit(r, auditlog.Event{Action: "user.unlocked", ResourceType: "user", ResourceID: user.Username})
	}
	return nil
}

func (h *Handlers) unlockUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	user, err := h.store.GetUser(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := h.unlock(r, user); err != nil {
		writeStoreError(w, err)
		return
	}
	user.LockedUntil, user.FailedLogins = nil, 0
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

func (h *Handlers) getAccountPolicy(w http.ResponseWriter, r *http.Request) {
	policy, err := h.store.AccountPolicy(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (h *Handlers) updateAccountPolicy(w http.ResponseWriter, r *http.Request) {
	before, err := h.store.AccountPolicy(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// A request sets the whole policy. It is decoded over the current
	// one only so that a field a client does not know about is kept.
	policy := before
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	claims, _ := ClaimsFromContext(r.Context())
	saved, err := h.store.SetAccountPolicy(r.Context(), policy, claims.Subject)
	var invalid *InvalidPolicyError
	if errors.As(err, &invalid) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": invalid.Reason, "field": invalid.Field})
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	h.recordActivity(r, "account_policy.updated", "changed the account policy: "+describePolicy(saved))
	h.recordAudit(r, auditlog.Event{Action: "account_policy.updated", ResourceType: "account_policy", ResourceID: "account-policy",
		Before: policyValues(before), After: policyValues(saved)})
	writeJSON(w, http.StatusOK, saved)
}

// policyValues is a policy's settings, without who last saved it, for
// the audit log's before and after.
func policyValues(p AccountPolicy) map[string]any {
	return map[string]any{
		"minLength": p.MinLength, "requireLowercase": p.RequireLowercase, "requireUppercase": p.RequireUppercase,
		"requireDigit": p.RequireDigit, "requireSymbol": p.RequireSymbol,
		"lockoutThreshold": p.LockoutThreshold, "lockoutMinutes": p.LockoutMinutes,
	}
}

func describePolicy(p AccountPolicy) string {
	kinds := []string{}
	for name, on := range map[string]bool{"lowercase": p.RequireLowercase, "uppercase": p.RequireUppercase, "digit": p.RequireDigit, "symbol": p.RequireSymbol} {
		if on {
			kinds = append(kinds, name)
		}
	}
	sort.Strings(kinds)
	required := "no required character kinds"
	if len(kinds) > 0 {
		required = "requiring " + strings.Join(kinds, ", ")
	}
	lockout := "no lockout"
	if p.LockoutThreshold > 0 {
		lockout = fmt.Sprintf("lock for %d minutes after %d failed logins", p.LockoutMinutes, p.LockoutThreshold)
	}
	return fmt.Sprintf("passwords of at least %d characters, %s; %s", p.MinLength, required, lockout)
}

func (h *Handlers) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.store.ListUsers(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	resp := make([]userResponse, 0, len(users))
	for _, u := range users {
		resp = append(resp, toUserResponse(u))
	}
	h.recordAuditRead(r, auditlog.Event{Action: "user.list.viewed", ResourceType: "user"})
	writeJSON(w, http.StatusOK, resp)
}

type createUserRequest struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
	Email     string `json:"email,omitempty"`
}

func (h *Handlers) createUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !h.passwordAllowed(w, r, req.Username, req.Password) {
		return
	}
	u, err := h.store.CreateUser(r.Context(), req.Username, req.Password)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if req.FirstName != "" || req.LastName != "" || req.Email != "" {
		if err := h.store.UpdateProfile(r.Context(), u.ID, req.FirstName, req.LastName, req.Email); err != nil {
			writeStoreError(w, err)
			return
		}
		u.FirstName, u.LastName, u.Email = nullIfEmpty(req.FirstName), nullIfEmpty(req.LastName), nullIfEmpty(req.Email)
	}
	h.recordActivity(r, "user.created", "created user "+u.Username)
	h.recordAudit(r, auditlog.Event{Action: "user.created", ResourceType: "user", ResourceID: u.Username})
	writeJSON(w, http.StatusCreated, toUserResponse(u))
}

func (h *Handlers) getUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	u, err := h.store.GetUser(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	h.recordAuditRead(r, auditlog.Event{Action: "user.viewed", ResourceType: "user", ResourceID: u.Username})
	writeJSON(w, http.StatusOK, toUserResponse(u))
}

type updateUserRequest struct {
	Username  string `json:"username"`
	Password  string `json:"password,omitempty"`
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
	Email     string `json:"email,omitempty"`
}

func (h *Handlers) updateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	target, err := h.store.GetUser(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if req.Password != "" {
		// Checked before anything is changed, against the name the
		// account will have.
		username := target.Username
		if req.Username != "" {
			username = req.Username
		}
		if !h.passwordAllowed(w, r, username, req.Password) {
			return
		}
	}
	if req.Username != "" {
		if err := h.store.UpdateUsername(r.Context(), id, req.Username); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	if req.Password != "" {
		if err := h.store.UpdatePassword(r.Context(), id, req.Password); err != nil {
			writeStoreError(w, err)
			return
		}
		// A password set by an administrator is how a locked-out user
		// is let back in.
		if err := h.unlock(r, target); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	if req.FirstName != "" || req.LastName != "" || req.Email != "" {
		// A request only sets the fields it names - merge with the
		// current value for the rest, so e.g. adding an email doesn't
		// blank an already-set first/last name (see Store.UpdateProfile's
		// own doc: it replaces all three, callers merge first).
		current, err := h.store.GetUser(r.Context(), id)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		firstName, lastName, email := req.FirstName, req.LastName, req.Email
		if firstName == "" {
			firstName = derefOrEmpty(current.FirstName)
		}
		if lastName == "" {
			lastName = derefOrEmpty(current.LastName)
		}
		if email == "" {
			email = derefOrEmpty(current.Email)
		}
		if err := h.store.UpdateProfile(r.Context(), id, firstName, lastName, email); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	// Never unlogged anywhere before this - the old internal/activity
	// system only covered create/delete (see design.md's coverage-gap
	// list). No password value is included in the event.
	h.recordAudit(r, auditlog.Event{Action: "admin.user.updated", ResourceType: "user", ResourceID: strconv.FormatInt(id, 10)})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	// Fetched before deletion purely so the activity summary can name the
	// user rather than only its id - not required for the delete itself.
	u, err := h.store.GetUser(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := h.store.DeleteUser(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	h.recordActivity(r, "user.deleted", "deleted user "+u.Username)
	h.recordAudit(r, auditlog.Event{Action: "user.deleted", ResourceType: "user", ResourceID: u.Username})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) listUserRoles(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	roles, err := h.store.ListUserRoles(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

func (h *Handlers) assignRole(w http.ResponseWriter, r *http.Request) {
	userID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	roleID, ok := pathID(w, r, "roleId")
	if !ok {
		return
	}
	if err := h.store.AssignRole(r.Context(), userID, roleID); err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.recordActivity(r, "role.assigned", h.roleGrantSummary(r.Context(), userID, roleID))
	h.recordAudit(r, auditlog.Event{Action: "role.assigned", ResourceType: "role_assignment", ResourceID: roleAssignmentResourceID(userID, roleID)})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) unassignRole(w http.ResponseWriter, r *http.Request) {
	userID, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	roleID, ok := pathID(w, r, "roleId")
	if !ok {
		return
	}
	if err := h.store.UnassignRole(r.Context(), userID, roleID); err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.recordActivity(r, "role.unassigned", h.roleGrantSummary(r.Context(), userID, roleID))
	h.recordAudit(r, auditlog.Event{Action: "role.unassigned", ResourceType: "role_assignment", ResourceID: roleAssignmentResourceID(userID, roleID)})
	w.WriteHeader(http.StatusNoContent)
}

// roleAssignmentResourceID identifies a user/role pair as a single
// structured resource id - auditlog.Event has one resourceType/
// resourceId pair, not a slot per involved entity.
func roleAssignmentResourceID(userID, roleID int64) string {
	return "user:" + strconv.FormatInt(userID, 10) + "/role:" + strconv.FormatInt(roleID, 10)
}

// roleGrantSummary builds a human-readable summary naming the user and
// role for an assign/unassign activity event, falling back to their ids
// if either lookup fails (never blocks the action itself - the mutation
// already succeeded by the time this runs).
func (h *Handlers) roleGrantSummary(ctx context.Context, userID, roleID int64) string {
	username := strconv.FormatInt(userID, 10)
	if u, err := h.store.GetUser(ctx, userID); err == nil {
		username = u.Username
	}
	roleName := strconv.FormatInt(roleID, 10)
	if role, err := h.store.GetRole(ctx, roleID); err == nil {
		roleName = role.Name
	}
	return "role " + roleName + " for user " + username
}

// --- roles ---

func (h *Handlers) listRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.store.ListRoles(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.recordAuditRead(r, auditlog.Event{Action: "role.list.viewed", ResourceType: "role"})
	writeJSON(w, http.StatusOK, roles)
}

// roleRequest is the body of a role create or update. NodeGroups is a
// pointer so "not sent" (an API client that predates node group scope)
// can be told from "sent empty": the first means the root group on
// create and "leave as is" on update, the second means no nodes.
//
// TaskPatterns is a pointer for the same reason: not sent means no
// allowlist on create and "leave as is" on update, sent empty means every
// task.
type roleRequest struct {
	Name         string    `json:"name"`
	Permissions  []string  `json:"permissions"`
	NodeGroups   *[]int64  `json:"nodeGroups"`
	TaskPatterns *[]string `json:"taskPatterns"`
}

func (req roleRequest) taskPatterns() []string {
	if req.TaskPatterns == nil {
		return nil
	}
	if *req.TaskPatterns == nil {
		return []string{}
	}
	return *req.TaskPatterns
}

func (req roleRequest) nodeGroups() []int64 {
	if req.NodeGroups == nil {
		return nil
	}
	if *req.NodeGroups == nil {
		return []int64{}
	}
	return *req.NodeGroups
}

// roleAudit is what a role change records: its permissions and the node
// groups they apply to.
type roleAudit struct {
	Permissions  []string `json:"permissions"`
	NodeGroups   []int64  `json:"nodeGroups"`
	TaskPatterns []string `json:"taskPatterns"`
}

func auditOf(role Role) roleAudit {
	return roleAudit{Permissions: role.Permissions, NodeGroups: role.NodeGroups, TaskPatterns: role.TaskPatterns}
}

func (h *Handlers) createRole(w http.ResponseWriter, r *http.Request) {
	var req roleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	role, err := h.store.CreateRoleFull(r.Context(), Role{Name: req.Name, Permissions: req.Permissions, NodeGroups: req.nodeGroups(), TaskPatterns: req.taskPatterns()})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	h.recordActivity(r, "role.created", "created role "+role.Name)
	h.recordAudit(r, auditlog.Event{Action: "role.created", ResourceType: "role", ResourceID: role.Name, After: auditOf(role)})
	writeJSON(w, http.StatusCreated, role)
}

func (h *Handlers) getRole(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	role, err := h.store.GetRole(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	h.recordAuditRead(r, auditlog.Event{Action: "role.viewed", ResourceType: "role", ResourceID: role.Name})
	writeJSON(w, http.StatusOK, role)
}

func (h *Handlers) updateRole(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	// Fetched before the update purely to record what changed - the
	// existing internal/activity summary stays prose-only, but the
	// audit event below carries the actual permission diff (see
	// specs/audit-log-emission's "A permission change records what
	// changed" scenario).
	before, err := h.store.GetRole(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var req roleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	role := Role{ID: id, Name: req.Name, Permissions: req.Permissions, NodeGroups: req.nodeGroups(), TaskPatterns: req.taskPatterns()}
	if err := h.store.UpdateRole(r.Context(), role); err != nil {
		writeStoreError(w, err)
		return
	}
	if role.TaskPatterns == nil {
		role.TaskPatterns = before.TaskPatterns
	} else {
		role.TaskPatterns = taskscope.Unique(role.TaskPatterns)
	}
	if role.NodeGroups == nil {
		role.NodeGroups = before.NodeGroups
	} else {
		role.NodeGroups = uniqueIDs(role.NodeGroups)
	}
	h.recordActivity(r, "role.updated", "updated role "+role.Name)
	h.recordAudit(r, auditlog.Event{
		Action: "role.updated", ResourceType: "role", ResourceID: role.Name,
		Before: auditOf(before), After: auditOf(role),
	})
	writeJSON(w, http.StatusOK, role)
}

func (h *Handlers) deleteRole(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	// Fetched before deletion purely so the activity summary can name the
	// role rather than only its id - not required for the delete itself.
	role, err := h.store.GetRole(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := h.store.DeleteRole(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	h.recordActivity(r, "role.deleted", "deleted role "+role.Name)
	h.recordAudit(r, auditlog.Event{Action: "role.deleted", ResourceType: "role", ResourceID: role.Name, Before: auditOf(role)})
	w.WriteHeader(http.StatusNoContent)
}

// --- service tokens ---

func (h *Handlers) listServiceTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := h.store.ListServiceTokens(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.recordAuditRead(r, auditlog.Event{Action: "service_token.list.viewed", ResourceType: "service_token"})
	writeJSON(w, http.StatusOK, tokens)
}

type createServiceTokenRequest struct {
	Name         string   `json:"name"`
	Permissions  []string `json:"permissions"`
	TaskPatterns []string `json:"taskPatterns"`
}

type serviceTokenResponse struct {
	ServiceToken
	Token string `json:"token"` // only ever present on creation - shown once
}

func (h *Handlers) createServiceToken(w http.ResponseWriter, r *http.Request) {
	var req createServiceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Refused before a token is signed: nothing is created for a bad list.
	if err := taskscope.ValidatePatterns(taskscope.Unique(req.TaskPatterns)); err != nil {
		writeStoreError(w, err)
		return
	}

	signed, claims, err := h.issuer.IssueTokenWithTasks(req.Name, TokenTypeService, req.Permissions, ServiceTokenGrants(taskscope.Unique(req.TaskPatterns)), serviceTokenTTL())
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}

	st, err := h.store.CreateServiceTokenWithTasks(r.Context(), req.Name, claims.ID, req.Permissions, req.TaskPatterns)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	h.recordActivity(r, "service_token.created", "created service token "+st.Name)
	h.recordAudit(r, auditlog.Event{Action: "service_token.created", ResourceType: "service_token", ResourceID: st.Name, After: map[string][]string{"permissions": st.Permissions, "taskPatterns": st.TaskPatterns}})
	writeJSON(w, http.StatusCreated, serviceTokenResponse{ServiceToken: st, Token: signed})
}

func (h *Handlers) revokeServiceToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	st, err := h.store.GetServiceToken(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	if err := h.revoker.Revoke(r.Context(), st.JTI, farFutureExpiry()); err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := h.store.DeleteServiceToken(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	h.recordActivity(r, "service_token.revoked", "revoked service token "+st.Name)
	h.recordAudit(r, auditlog.Event{Action: "service_token.revoked", ResourceType: "service_token", ResourceID: st.Name})
	w.WriteHeader(http.StatusNoContent)
}

// --- shared helpers ---

func pathID(w http.ResponseWriter, r *http.Request, param string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(param), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func writeStoreError(w http.ResponseWriter, err error) {
	var pattern *taskscope.PatternError
	switch {
	case errors.As(err, &pattern):
		// The field is named so a form can show the reason on it.
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  err.Error(),
			"fields": map[string]string{"taskPatterns": err.Error()},
		})
	case errors.Is(err, ErrNotFound):
		writeJSONError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrDuplicateName):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrUnknownNodeGroup):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSONError(w, http.StatusBadGateway, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
