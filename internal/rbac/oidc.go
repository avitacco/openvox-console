package rbac

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
)

// oidcStateTTL bounds how long an in-progress OIDC login (state persisted
// in Postgres, see oidc_state_store.go) stays usable - long enough for a
// real login through an identity provider, short enough that an
// abandoned attempt doesn't linger.
const oidcStateTTL = 10 * time.Minute

// Errors OIDCService returns for its own failure modes, distinct from
// ErrInvalidCredentials since these callers (HTTP handlers) respond
// differently (503 vs 401) - see internal/rbac/handlers.go.
var (
	ErrOIDCNotConfigured = errors.New("oidc is not configured")
	ErrInvalidOIDCState  = errors.New("invalid or expired oidc login state")
)

// OIDCConfig configures an OIDCService. See internal/runtime.Config for
// where these values come from; all are optional, and OIDC stays
// inactive unless at least Issuer is set.
type OIDCConfig struct {
	Issuer        string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	Scopes        string // space-separated
	UsernameClaim string
	RoleClaim     string
	RoleMapping   string // JSON object: claim-value -> local role name
}

// oidcStore is the subset of *Store OIDCService needs for the login state
// round-trip and user/role provisioning - kept as an interface so it can
// be tested without a live Postgres.
type oidcStore interface {
	PersistOIDCState(ctx context.Context, state, verifier, nonce string, expiresAt time.Time) error
	ConsumeOIDCState(ctx context.Context, state string) (verifier, nonce string, err error)
	GetUserByOIDCSubject(ctx context.Context, subject string) (User, error)
	CreateOIDCUser(ctx context.Context, subject, username string) (User, error)
	GetRoleByName(ctx context.Context, name string) (Role, error)
	ReconcileOIDCRoles(ctx context.Context, userID int64, desiredRoleIDs []int64) (added, removed []int64, err error)
	UserPermissions(ctx context.Context, userID int64) ([]string, error)
	UpdateProfile(ctx context.Context, id int64, firstName, lastName, email string) error
}

// OIDCService implements OIDC login: authorization URL generation,
// callback handling (code exchange, ID token verification, user
// provisioning, role reconciliation), and issuing the console's own
// token pair via the same issuePair used by password login - see
// design.md in the oidc-authentication change.
type OIDCService struct {
	logger         *slog.Logger
	store          oidcStore
	issuer         *Issuer
	recordActivity func(action, summary string)
	recordAudit    func(auditlog.Event)

	active bool // false when unconfigured or provider discovery failed

	verifier      *oidc.IDTokenVerifier
	oauth2Cfg     oauth2.Config
	usernameClaim string
	roleClaim     string
	roleMapping   map[string]string
}

// NewOIDCService builds an OIDCService. If cfg.Issuer is empty, or
// provider discovery or the role mapping fails to parse, the returned
// service is inactive (Configured() reports false) and the reason is
// logged - it is never a fatal error, so a transient identity-provider
// outage or a config mistake never prevents the console from starting or
// affects local login (see design.md: "Discovery/config failure at
// startup is logged, not fatal"). recordActivity is called (with actor
// implicitly "system", baked in by the caller) when OIDC login
// provisions a new user or actually changes a user's role assignments -
// see design.md in the phase-4-activity-and-audit-log change.
func NewOIDCService(ctx context.Context, logger *slog.Logger, store oidcStore, issuer *Issuer, cfg OIDCConfig, recordActivity func(action, summary string), recordAudit func(auditlog.Event)) *OIDCService {
	svc := &OIDCService{logger: logger, store: store, issuer: issuer, recordActivity: recordActivity, recordAudit: recordAudit}

	if cfg.Issuer == "" {
		return svc
	}

	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		logger.Error("OIDC provider discovery failed; OIDC login is inactive", "issuer", cfg.Issuer, "error", err)
		return svc
	}

	var roleMapping map[string]string
	if cfg.RoleMapping != "" {
		if err := json.Unmarshal([]byte(cfg.RoleMapping), &roleMapping); err != nil {
			logger.Error("invalid CONSOLE_OIDC_ROLE_MAPPING (must be a JSON object); OIDC login is inactive", "error", err)
			return svc
		}
	}

	svc.verifier = provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	svc.oauth2Cfg = oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       strings.Fields(cfg.Scopes),
	}
	svc.usernameClaim = cfg.UsernameClaim
	svc.roleClaim = cfg.RoleClaim
	svc.roleMapping = roleMapping
	svc.active = true
	return svc
}

// Configured reports whether OIDC login is available.
func (o *OIDCService) Configured() bool {
	return o != nil && o.active
}

// AuthorizationURL persists a fresh login state (PKCE verifier + nonce)
// and returns the URL to redirect the browser to at the OIDC provider.
func (o *OIDCService) AuthorizationURL(ctx context.Context) (string, error) {
	if !o.Configured() {
		return "", ErrOIDCNotConfigured
	}

	state, err := randomURLSafeString()
	if err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	nonce, err := randomURLSafeString()
	if err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	verifier := oauth2.GenerateVerifier()

	if err := o.store.PersistOIDCState(ctx, state, verifier, nonce, time.Now().Add(oidcStateTTL)); err != nil {
		return "", fmt.Errorf("persist oidc login state: %w", err)
	}

	url := o.oauth2Cfg.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
	return url, nil
}

// HandleCallback completes an OIDC login: consumes the single-use login
// state, exchanges the authorization code, verifies the ID token,
// provisions/reconciles the local user, and issues the console's own
// access/refresh token pair.
func (o *OIDCService) HandleCallback(ctx context.Context, code, state string) (TokenPair, error) {
	if !o.Configured() {
		return TokenPair{}, ErrOIDCNotConfigured
	}

	verifier, nonce, err := o.store.ConsumeOIDCState(ctx, state)
	if err != nil {
		return TokenPair{}, ErrInvalidOIDCState
	}

	oauth2Token, err := o.oauth2Cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return TokenPair{}, fmt.Errorf("exchange authorization code: %w", err)
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return TokenPair{}, errors.New("token response missing id_token")
	}

	idToken, err := o.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return TokenPair{}, fmt.Errorf("verify id token: %w", err)
	}
	if idToken.Nonce != nonce {
		return TokenPair{}, errors.New("id token nonce mismatch")
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return TokenPair{}, fmt.Errorf("decode id token claims: %w", err)
	}

	user, created, err := o.provisionUser(ctx, idToken.Subject, claims)
	if err != nil {
		return TokenPair{}, fmt.Errorf("provision user: %w", err)
	}
	if created {
		o.recordActivity("user.provisioned", "provisioned user "+user.Username+" via OIDC")
	}

	if err := o.reconcileRoles(ctx, user, claims); err != nil {
		return TokenPair{}, fmt.Errorf("reconcile roles: %w", err)
	}

	user, err = o.syncProfile(ctx, user, claims)
	if err != nil {
		return TokenPair{}, fmt.Errorf("sync profile: %w", err)
	}

	perms, err := o.store.UserPermissions(ctx, user.ID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("load permissions: %w", err)
	}

	pair, err := issuePair(o.issuer, user.Username, perms)
	if err != nil {
		return TokenPair{}, err
	}
	o.recordAudit(auditlog.Event{Action: "user.login.success", Actor: user.Username, ResourceType: "user", ResourceID: user.Username})
	return pair, nil
}

// provisionUser finds the local user for subject, creating one (seeded
// from the configured username claim, falling back to the subject
// itself) if this is its first OIDC login. created reports whether a new
// user was created (vs. an existing one being reused).
func (o *OIDCService) provisionUser(ctx context.Context, subject string, claims map[string]any) (user User, created bool, err error) {
	user, err = o.store.GetUserByOIDCSubject(ctx, subject)
	if err == nil {
		return user, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return User{}, false, fmt.Errorf("lookup oidc user: %w", err)
	}

	username := subject
	if v, ok := claims[o.usernameClaim].(string); ok && v != "" {
		username = v
	}
	user, err = o.store.CreateOIDCUser(ctx, subject, username)
	if err != nil {
		return User{}, false, err
	}
	return user, true, nil
}

// reconcileRoles maps the configured role claim's current values to
// local role names, reconciles the user's OIDC-derived role assignments
// against them (see Store.ReconcileOIDCRoles), and publishes an activity
// event only when something actually changed - a login where nothing
// changed shouldn't add audit noise.
func (o *OIDCService) reconcileRoles(ctx context.Context, user User, claims map[string]any) error {
	claimValues := claimStringSlice(claims[o.roleClaim])

	var desired []int64
	var matchedRoles, unmappedClaimValues []string
	for _, v := range claimValues {
		roleName, ok := o.roleMapping[v]
		if !ok {
			unmappedClaimValues = append(unmappedClaimValues, v)
			continue
		}
		role, err := o.store.GetRoleByName(ctx, roleName)
		if errors.Is(err, ErrNotFound) {
			o.logger.Warn("OIDC role mapping references a role that does not exist", "claimValue", v, "role", roleName)
			continue
		}
		if err != nil {
			return fmt.Errorf("lookup mapped role %q: %w", roleName, err)
		}
		matchedRoles = append(matchedRoles, roleName)
		desired = append(desired, role.ID)
	}

	// Logged unconditionally (not just on a warning/error path) because a
	// mismatch here - the IdP not sending the role claim at all, sending
	// it under an unexpected name, or a claim value that doesn't match
	// any configured mapping key exactly (case, typo) - is a common
	// first-time OIDC setup problem with no other visible symptom besides
	// "the user got no roles," which is otherwise silent and hard to
	// diagnose from outside the running service.
	o.logger.Info("OIDC role reconciliation",
		"user", user.Username, "roleClaim", o.roleClaim,
		"claimValues", claimValues, "matchedRoles", matchedRoles, "unmappedClaimValues", unmappedClaimValues)

	added, removed, err := o.store.ReconcileOIDCRoles(ctx, user.ID, desired)
	if err != nil {
		return err
	}
	if len(added) == 0 && len(removed) == 0 {
		return nil
	}
	o.recordActivity("role.reconciled", fmt.Sprintf(
		"OIDC role reconciliation for user %s: %d added, %d removed", user.Username, len(added), len(removed),
	))
	return nil
}

// syncProfile updates user's stored first name/last name/email from the
// ID token's standard given_name/family_name/email claims, refreshed on
// every login the same way reconcileRoles refreshes role assignments. A
// claim the provider doesn't send leaves the existing stored value
// alone, rather than blanking a value that was set another way (see
// Handlers.updateUser) just because one login's claims omitted it.
// Returns the updated user (or user unchanged if nothing needed to
// change), so the caller's token issuance embeds the current profile,
// not a stale one.
func (o *OIDCService) syncProfile(ctx context.Context, user User, claims map[string]any) (User, error) {
	firstName := claimString(claims["given_name"], derefOrEmpty(user.FirstName))
	lastName := claimString(claims["family_name"], derefOrEmpty(user.LastName))
	email := claimString(claims["email"], derefOrEmpty(user.Email))

	if firstName == derefOrEmpty(user.FirstName) && lastName == derefOrEmpty(user.LastName) && email == derefOrEmpty(user.Email) {
		return user, nil
	}

	if err := o.store.UpdateProfile(ctx, user.ID, firstName, lastName, email); err != nil {
		return User{}, err
	}
	user.FirstName, user.LastName, user.Email = nullIfEmpty(firstName), nullIfEmpty(lastName), nullIfEmpty(email)
	return user, nil
}

// claimString returns claim if it's a non-empty string, else fallback.
func claimString(claim any, fallback string) string {
	if s, ok := claim.(string); ok && s != "" {
		return s
	}
	return fallback
}

// claimStringSlice normalizes a claim value into a string slice: OIDC
// providers commonly emit a "groups"-style claim as a JSON array, but
// some emit a bare string when there's exactly one value.
func claimStringSlice(v any) []string {
	switch val := v.(type) {
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return []string{val}
	default:
		return nil
	}
}

func randomURLSafeString() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
