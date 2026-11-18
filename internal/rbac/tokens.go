package rbac

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// Token kinds, carried in Claims.Type.
const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
	TokenTypeService = "service"
)

// Token lifetimes. Access tokens are short (10-15 minutes, per
// architecture-summary.md) to bound how long a compromised or stale
// token stays valid; refresh tokens are longer-lived but still bounded
// and revocable, not indefinite.
const (
	AccessTokenTTL  = 15 * time.Minute
	RefreshTokenTTL = 24 * time.Hour
)

// Claims is this console's JWT claim set: the standard registered claims
// (subject, jti, expiry, ...) plus the token's type and the permissions
// it carries. Permissions are embedded at issuance - see design.md for
// why this, not a per-request database lookup.
//
// Profile fields (first/last name, email) are deliberately NOT embedded
// here - an earlier version of this type did, so the frontend could
// render the header avatar without a separate fetch, but that meant an
// already-issued token showed a stale profile until its next refresh
// (up to AccessTokenTTL later) after an edit via PUT /api/v1/me. The
// frontend now fetches GET /api/v1/me directly for this instead of
// trusting the token, which is always current.
//
// NodeScopes says which node groups each permission applies to
// (permission -> group ids; see add-group-scoped-node-access). Group
// ids, not node names, because membership follows facts and must not be
// frozen into a token. It deliberately has no omitempty: a user token
// always states its scope, even an empty one, and only a token with no
// scope at all - a service token, or an access token issued before this
// field existed - is fleet-wide.
//
// TaskGrants limits which tasks the token's orchestrator:run may start,
// as grants pairing task patterns with node groups (see
// internal/taskscope). Absent means no limit: that is what a service
// token without an allowlist, a user none of whose roles has one, and a
// token issued before task permissions existed all state. It therefore
// must never be written as an empty list, which JSON would drop and so
// turn "may run nothing" into "may run everything"; IssueUserToken and
// IssueTokenWithTasks refuse to.
type Claims struct {
	jwt.RegisteredClaims
	Type        string             `json:"type"`
	Permissions []string           `json:"permissions"`
	NodeScopes  map[string][]int64 `json:"nodeScopes"`
	TaskGrants  []taskscope.Grant  `json:"taskGrants,omitempty"`
}

// NodeGroupsFor returns the node groups permission applies to for this
// token. fleetWide is true when the token states no scope at all, in
// which case groups is nil and every node is covered. Otherwise groups
// are exactly the ones granted - none, if the permission has no entry.
func (c *Claims) NodeGroupsFor(permission string) (groups []int64, fleetWide bool) {
	if c.NodeScopes == nil {
		return nil, true
	}
	return c.NodeScopes[permission], false
}

// HasPermission reports whether these claims carry permission.
func (c *Claims) HasPermission(permission string) bool {
	for _, p := range c.Permissions {
		if p == permission {
			return true
		}
	}
	return false
}

// Issuer signs new tokens with an ES256 private key, tagging each with a
// key ID (kid) identifying which key signed it - see keys.go and
// operations.md's key rotation runbook. A Verifier holding several keys
// (during a rotation window) uses this to pick the right one instead of
// guessing.
type Issuer struct {
	key *ecdsa.PrivateKey
	kid string
}

// NewIssuer builds an Issuer signing with key, tagging issued tokens with
// kid.
func NewIssuer(key *ecdsa.PrivateKey, kid string) *Issuer {
	return &Issuer{key: key, kid: kid}
}

// IssueToken signs a new JWT of tokenType for subject, carrying
// permissions, expiring after ttl. The token states no node scope and is
// therefore fleet-wide: this is the path for service tokens. Tokens for
// users go through IssueUserToken.
func (i *Issuer) IssueToken(subject, tokenType string, permissions []string, ttl time.Duration) (string, *Claims, error) {
	return i.issue(subject, tokenType, permissions, nil, nil, ttl)
}

// IssueTokenWithTasks is IssueToken for a service token with a task
// allowlist: taskGrants states which tasks it may run (see Claims).
func (i *Issuer) IssueTokenWithTasks(subject, tokenType string, permissions []string, taskGrants []taskscope.Grant, ttl time.Duration) (string, *Claims, error) {
	if taskGrants != nil && len(taskGrants) == 0 {
		return "", nil, errEmptyTaskGrants
	}
	return i.issue(subject, tokenType, permissions, nil, taskGrants, ttl)
}

var errEmptyTaskGrants = errors.New("an empty list of task grants cannot be written to a token: it would read as no limit")

// IssueUserToken is IssueToken for a user's token, which always states
// its node scope. A nil nodeScopes is written as an empty scope (no
// nodes), never left out: leaving it out would read as fleet-wide.
func (i *Issuer) IssueUserToken(subject, tokenType string, permissions []string, nodeScopes map[string][]int64, ttl time.Duration) (string, *Claims, error) {
	return i.IssueUserTokenWithTasks(subject, tokenType, permissions, nodeScopes, nil, ttl)
}

// IssueUserTokenWithTasks is IssueUserToken for a user whose roles limit
// the tasks they may run: taskGrants states how (see Claims), and nil
// states no limit.
func (i *Issuer) IssueUserTokenWithTasks(subject, tokenType string, permissions []string, nodeScopes map[string][]int64, taskGrants []taskscope.Grant, ttl time.Duration) (string, *Claims, error) {
	if nodeScopes == nil {
		nodeScopes = map[string][]int64{}
	}
	if taskGrants != nil && len(taskGrants) == 0 {
		return "", nil, errEmptyTaskGrants
	}
	return i.issue(subject, tokenType, permissions, nodeScopes, taskGrants, ttl)
}

func (i *Issuer) issue(subject, tokenType string, permissions []string, nodeScopes map[string][]int64, taskGrants []taskscope.Grant, ttl time.Duration) (string, *Claims, error) {
	now := time.Now()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		Type:        tokenType,
		Permissions: permissions,
		NodeScopes:  nodeScopes,
		TaskGrants:  taskGrants,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = i.kid
	signed, err := token.SignedString(i.key)
	if err != nil {
		return "", nil, fmt.Errorf("sign token: %w", err)
	}
	return signed, claims, nil
}

// Errors returned by Verifier.Verify - deliberately generic (don't leak
// *why* a token failed to a caller who might be an attacker probing).
var (
	ErrInvalidToken = errors.New("invalid or expired token")
	ErrTokenRevoked = errors.New("token has been revoked")
)

// revocationChecker is the subset of *Revoker that Verifier needs, so it
// can be tested without a live Postgres/NATS.
type revocationChecker interface {
	IsRevoked(jti string) bool
}

// Verifier checks a token's signature, expiry, and revocation status,
// selecting the verification key by the token's header kid - see keys.go
// and operations.md's key rotation runbook. This lets an outstanding
// token signed by a since-retired key keep verifying through its natural
// expiry, as long as that key is still in keys.
type Verifier struct {
	keys       map[string]*ecdsa.PublicKey
	primaryKid string
	revoker    revocationChecker
}

// NewVerifier builds a Verifier checking signatures against keys (kid ->
// public key) and revocation against revoker. primaryKid is used for a
// token with no kid in its header (pre-rotation tokens, issued before
// this feature existed).
func NewVerifier(keys map[string]*ecdsa.PublicKey, primaryKid string, revoker revocationChecker) *Verifier {
	return &Verifier{keys: keys, primaryKid: primaryKid, revoker: revoker}
}

// Verify parses and validates tokenString, returning its claims if it has
// a valid signature, isn't expired, and hasn't been revoked.
func (v *Verifier) Verify(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodECDSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			kid = v.primaryKid
		}
		key, ok := v.keys[kid]
		if !ok {
			return nil, fmt.Errorf("unknown signing key id: %q", kid)
		}
		return key, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}

	if v.revoker != nil && v.revoker.IsRevoked(claims.ID) {
		return nil, ErrTokenRevoked
	}

	return claims, nil
}
