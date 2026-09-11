package rbac

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
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
type Claims struct {
	jwt.RegisteredClaims
	Type        string   `json:"type"`
	Permissions []string `json:"permissions"`
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
// permissions, expiring after ttl.
func (i *Issuer) IssueToken(subject, tokenType string, permissions []string, ttl time.Duration) (string, *Claims, error) {
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
