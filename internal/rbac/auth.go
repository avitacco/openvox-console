package rbac

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// ErrInvalidCredentials is returned for any login/refresh failure whose
// cause shouldn't be distinguishable to the caller (wrong password,
// unknown username, expired/revoked refresh token) - see the "without
// revealing whether the username exists" requirement.
var ErrInvalidCredentials = errors.New("invalid credentials")

// authStore is the subset of *Store the auth service needs.
type authStore interface {
	GetUserByUsername(ctx context.Context, username string) (User, error)
	UserPermissions(ctx context.Context, userID int64) ([]string, error)
	UserNodeScopes(ctx context.Context, userID int64) (map[string][]int64, error)
	UserTaskGrants(ctx context.Context, userID int64) ([]taskscope.Grant, error)

	// The account policy's lockout - see policy.go.
	AccountPolicy(ctx context.Context) (AccountPolicy, error)
	RecordFailedLogin(ctx context.Context, id int64, threshold int, period time.Duration) (bool, *time.Time, error)
	ResetFailedLogins(ctx context.Context, id int64) (bool, error)
}

// AuthService implements password login and refresh-token rotation.
type AuthService struct {
	store       authStore
	issuer      *Issuer
	verifier    *Verifier
	revoker     *Revoker
	recordAudit func(auditlog.Event)
	// recordActivity is told when an account is locked. Optional - see
	// SetActivityRecorder.
	recordActivity func(action, actor, summary string)
}

// SetActivityRecorder has an account being locked written to the
// activity log, where an administrator will see it.
func (a *AuthService) SetActivityRecorder(record func(action, actor, summary string)) {
	a.recordActivity = record
}

// NewAuthService builds an AuthService. recordAudit is called for every
// login attempt (success or failure) - see design.md in the
// configurable-audit-logging change for why this is an injected closure
// rather than an internal/auditlog import held directly by callers.
func NewAuthService(store authStore, issuer *Issuer, verifier *Verifier, revoker *Revoker, recordAudit func(auditlog.Event)) *AuthService {
	return &AuthService{store: store, issuer: issuer, verifier: verifier, revoker: revoker, recordAudit: recordAudit}
}

// TokenPair is an issued access/refresh token pair.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
}

// Login verifies username/password and, on success, issues a new
// access/refresh token pair carrying the user's current permissions.
func (a *AuthService) Login(ctx context.Context, username, password string) (TokenPair, error) {
	user, err := a.store.GetUserByUsername(ctx, username)
	if errors.Is(err, ErrNotFound) {
		// Compare against a dummy hash anyway, so a nonexistent username
		// takes the same time as a wrong password for a real one -
		// timing shouldn't reveal whether the account exists.
		_ = bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(password))
		a.recordLoginFailure(username)
		return TokenPair{}, ErrInvalidCredentials
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("lookup user: %w", err)
	}

	// The comparison is made whether or not the account is locked, so
	// that how long a refusal takes says nothing about the lock - and
	// every refusal below is the same ErrInvalidCredentials, so that
	// nothing else does either: saying "locked" would say the account
	// exists.
	wrong := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil
	if user.Locked() {
		a.recordAudit(auditlog.Event{Action: "user.login.locked", Actor: username, ResourceType: "user", ResourceID: user.Username})
		return TokenPair{}, ErrInvalidCredentials
	}
	if wrong {
		a.recordLoginFailure(username)
		a.CountFailedPassword(ctx, user)
		return TokenPair{}, ErrInvalidCredentials
	}
	if user.FailedLogins > 0 {
		if _, err := a.store.ResetFailedLogins(ctx, user.ID); err != nil {
			return TokenPair{}, fmt.Errorf("reset failed logins: %w", err)
		}
	}

	perms, err := a.store.UserPermissions(ctx, user.ID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("load permissions: %w", err)
	}

	scopes, err := a.store.UserNodeScopes(ctx, user.ID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("load node scopes: %w", err)
	}

	tasks, err := a.store.UserTaskGrants(ctx, user.ID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("load task grants: %w", err)
	}

	pair, err := issuePair(a.issuer, user.Username, perms, scopes, tasks)
	if err != nil {
		return TokenPair{}, err
	}
	a.recordAudit(auditlog.Event{Action: "user.login.success", Actor: user.Username, ResourceType: "user", ResourceID: user.Username})
	return pair, nil
}

// CountFailedPassword counts a wrong password against user under the
// account policy, locking the account when the count reaches the
// policy's threshold. It is used for a failed login and for a wrong
// current password given to a password change, which would otherwise
// let a stolen session guess without limit.
//
// A failure to count is not reported to the caller, who is being
// refused either way; the attempt itself is already in the audit log.
func (a *AuthService) CountFailedPassword(ctx context.Context, user User) {
	policy, err := a.store.AccountPolicy(ctx)
	if err != nil {
		policy = DefaultAccountPolicy()
	}
	locked, until, err := a.store.RecordFailedLogin(ctx, user.ID, policy.LockoutThreshold, policy.LockoutPeriod())
	if err != nil || !locked {
		return
	}
	a.recordAudit(auditlog.Event{Action: "user.locked", Actor: user.Username, ResourceType: "user", ResourceID: user.Username,
		After: map[string]any{"lockedUntil": until.UTC().Format(time.RFC3339), "failedLogins": policy.LockoutThreshold}})
	if a.recordActivity != nil {
		a.recordActivity("user.locked", user.Username, fmt.Sprintf("account %s was locked for %d minutes after %d failed logins",
			user.Username, policy.LockoutMinutes, policy.LockoutThreshold))
	}
}

// recordLoginFailure audits a failed login attempt, recording the
// attempted username - never the password - see design.md's risk note:
// this is standard audit-trail practice, distinct from the timing
// protection above (which is about what's returned to the requester,
// not what's recorded for the operator).
func (a *AuthService) recordLoginFailure(username string) {
	a.recordAudit(auditlog.Event{Action: "user.login.failure", Actor: username, ResourceType: "user", ResourceID: username})
}

// Refresh verifies refreshToken, revokes it (rotation - single use), and
// issues a new access/refresh pair carrying the user's current
// permissions (re-read, since roles may have changed since the original
// login).
func (a *AuthService) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	claims, err := a.verifier.Verify(refreshToken)
	if err != nil {
		return TokenPair{}, ErrInvalidCredentials
	}
	if claims.Type != TokenTypeRefresh {
		return TokenPair{}, ErrInvalidCredentials
	}

	user, err := a.store.GetUserByUsername(ctx, claims.Subject)
	if err != nil {
		return TokenPair{}, ErrInvalidCredentials
	}

	perms, err := a.store.UserPermissions(ctx, user.ID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("load permissions: %w", err)
	}

	scopes, err := a.store.UserNodeScopes(ctx, user.ID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("load node scopes: %w", err)
	}

	tasks, err := a.store.UserTaskGrants(ctx, user.ID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("load task grants: %w", err)
	}

	expiresAt := claims.ExpiresAt.Time
	if err := a.revoker.Revoke(ctx, claims.ID, expiresAt); err != nil {
		return TokenPair{}, fmt.Errorf("revoke used refresh token: %w", err)
	}

	return issuePair(a.issuer, user.Username, perms, scopes, tasks)
}

// issuePair issues an access/refresh token pair for subject carrying
// permissions and the node groups each applies to. Shared by password login/refresh (AuthService) and OIDC
// login (OIDCService), so every login path produces tokens the same way.
func issuePair(issuer *Issuer, subject string, permissions []string, nodeScopes map[string][]int64, taskGrants []taskscope.Grant) (TokenPair, error) {
	access, _, err := issuer.IssueUserTokenWithTasks(subject, TokenTypeAccess, permissions, nodeScopes, taskGrants, AccessTokenTTL)
	if err != nil {
		return TokenPair{}, fmt.Errorf("issue access token: %w", err)
	}
	refresh, _, err := issuer.IssueUserTokenWithTasks(subject, TokenTypeRefresh, permissions, nodeScopes, taskGrants, RefreshTokenTTL)
	if err != nil {
		return TokenPair{}, fmt.Errorf("issue refresh token: %w", err)
	}
	return TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}
