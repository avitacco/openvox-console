package rbac

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
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
}

// AuthService implements password login and refresh-token rotation.
type AuthService struct {
	store       authStore
	issuer      *Issuer
	verifier    *Verifier
	revoker     *Revoker
	recordAudit func(auditlog.Event)
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

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		a.recordLoginFailure(username)
		return TokenPair{}, ErrInvalidCredentials
	}

	perms, err := a.store.UserPermissions(ctx, user.ID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("load permissions: %w", err)
	}

	pair, err := issuePair(a.issuer, user.Username, perms)
	if err != nil {
		return TokenPair{}, err
	}
	a.recordAudit(auditlog.Event{Action: "user.login.success", Actor: user.Username, ResourceType: "user", ResourceID: user.Username})
	return pair, nil
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

	expiresAt := claims.ExpiresAt.Time
	if err := a.revoker.Revoke(ctx, claims.ID, expiresAt); err != nil {
		return TokenPair{}, fmt.Errorf("revoke used refresh token: %w", err)
	}

	return issuePair(a.issuer, user.Username, perms)
}

// issuePair issues an access/refresh token pair for subject carrying
// permissions. Shared by password login/refresh (AuthService) and OIDC
// login (OIDCService), so every login path produces tokens the same way.
func issuePair(issuer *Issuer, subject string, permissions []string) (TokenPair, error) {
	access, _, err := issuer.IssueToken(subject, TokenTypeAccess, permissions, AccessTokenTTL)
	if err != nil {
		return TokenPair{}, fmt.Errorf("issue access token: %w", err)
	}
	refresh, _, err := issuer.IssueToken(subject, TokenTypeRefresh, permissions, RefreshTokenTTL)
	if err != nil {
		return TokenPair{}, fmt.Errorf("issue refresh token: %w", err)
	}
	return TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}
