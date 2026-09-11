package rbac

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const testKid = "test-key"

func testKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	return key
}

// singleKeyVerifier builds a Verifier whose verification set holds only
// key under kid, with kid as the primary (used for tokens with no kid in
// their header) - the common case in tests that don't exercise rotation
// itself.
func singleKeyVerifier(kid string, key *ecdsa.PrivateKey, revoker revocationChecker) *Verifier {
	return NewVerifier(map[string]*ecdsa.PublicKey{kid: &key.PublicKey}, kid, revoker)
}

type fakeRevocationChecker struct {
	revoked map[string]bool
}

func (f *fakeRevocationChecker) IsRevoked(jti string) bool { return f.revoked[jti] }

func TestIssueAndVerify_RoundTrips(t *testing.T) {
	key := testKey(t)
	issuer := NewIssuer(key, testKid)
	verifier := singleKeyVerifier(testKid, key, &fakeRevocationChecker{revoked: map[string]bool{}})

	tokenString, issued, err := issuer.IssueToken("alice", TokenTypeAccess, []string{"nodes:read"}, AccessTokenTTL)
	if err != nil {
		t.Fatalf("IssueToken() error: %v", err)
	}

	got, err := verifier.Verify(tokenString)
	if err != nil {
		t.Fatalf("Verify() error: %v", err)
	}
	if got.Subject != "alice" {
		t.Errorf("Subject = %q, want alice", got.Subject)
	}
	if got.Type != TokenTypeAccess {
		t.Errorf("Type = %q, want %q", got.Type, TokenTypeAccess)
	}
	if !got.HasPermission("nodes:read") {
		t.Error("expected nodes:read permission")
	}
	if got.ID != issued.ID {
		t.Errorf("jti = %q, want %q", got.ID, issued.ID)
	}
}

func TestVerify_RejectsExpiredToken(t *testing.T) {
	key := testKey(t)
	issuer := NewIssuer(key, testKid)
	verifier := singleKeyVerifier(testKid, key, &fakeRevocationChecker{revoked: map[string]bool{}})

	tokenString, _, err := issuer.IssueToken("alice", TokenTypeAccess, nil, -time.Minute)
	if err != nil {
		t.Fatalf("IssueToken() error: %v", err)
	}

	if _, err := verifier.Verify(tokenString); err != ErrInvalidToken {
		t.Errorf("Verify() error = %v, want ErrInvalidToken", err)
	}
}

func TestVerify_RejectsMalformedToken(t *testing.T) {
	key := testKey(t)
	verifier := singleKeyVerifier(testKid, key, &fakeRevocationChecker{revoked: map[string]bool{}})

	if _, err := verifier.Verify("not.a.jwt"); err != ErrInvalidToken {
		t.Errorf("Verify() error = %v, want ErrInvalidToken", err)
	}
}

func TestVerify_RejectsWrongSigningKey(t *testing.T) {
	issuerKey := testKey(t)
	otherKey := testKey(t)
	issuer := NewIssuer(issuerKey, testKid)
	// otherKey is registered under the same kid the issuer used, so this
	// exercises a signature mismatch under a matching kid, not an unknown
	// kid (that's TestVerify_RejectsUnknownKid below).
	verifier := singleKeyVerifier(testKid, otherKey, &fakeRevocationChecker{revoked: map[string]bool{}})

	tokenString, _, err := issuer.IssueToken("alice", TokenTypeAccess, nil, AccessTokenTTL)
	if err != nil {
		t.Fatalf("IssueToken() error: %v", err)
	}

	if _, err := verifier.Verify(tokenString); err != ErrInvalidToken {
		t.Errorf("Verify() error = %v, want ErrInvalidToken (wrong key)", err)
	}
}

func TestVerify_RejectsRevokedToken(t *testing.T) {
	key := testKey(t)
	issuer := NewIssuer(key, testKid)

	tokenString, issued, err := issuer.IssueToken("alice", TokenTypeAccess, nil, AccessTokenTTL)
	if err != nil {
		t.Fatalf("IssueToken() error: %v", err)
	}

	verifier := singleKeyVerifier(testKid, key, &fakeRevocationChecker{revoked: map[string]bool{issued.ID: true}})
	if _, err := verifier.Verify(tokenString); err != ErrTokenRevoked {
		t.Errorf("Verify() error = %v, want ErrTokenRevoked", err)
	}
}

// --- Key rotation (kid selection) ---

func TestVerify_SelectsKeyByKid(t *testing.T) {
	oldKey := testKey(t)
	newKey := testKey(t)
	// A Verifier mid-rotation holds both the retired and active keys.
	verifier := NewVerifier(map[string]*ecdsa.PublicKey{
		"old": &oldKey.PublicKey,
		"new": &newKey.PublicKey,
	}, "new", &fakeRevocationChecker{revoked: map[string]bool{}})

	oldToken, _, err := NewIssuer(oldKey, "old").IssueToken("alice", TokenTypeAccess, nil, AccessTokenTTL)
	if err != nil {
		t.Fatalf("IssueToken() (old) error: %v", err)
	}
	newToken, _, err := NewIssuer(newKey, "new").IssueToken("alice", TokenTypeAccess, nil, AccessTokenTTL)
	if err != nil {
		t.Fatalf("IssueToken() (new) error: %v", err)
	}

	if _, err := verifier.Verify(oldToken); err != nil {
		t.Errorf("Verify(oldToken) error = %v, want a still-valid retired-key token", err)
	}
	if _, err := verifier.Verify(newToken); err != nil {
		t.Errorf("Verify(newToken) error = %v, want a valid active-key token", err)
	}
}

func TestVerify_RejectsUnknownKid(t *testing.T) {
	unknownKey := testKey(t)
	knownKey := testKey(t)
	verifier := singleKeyVerifier("known", knownKey, &fakeRevocationChecker{revoked: map[string]bool{}})

	tokenString, _, err := NewIssuer(unknownKey, "unknown").IssueToken("alice", TokenTypeAccess, nil, AccessTokenTTL)
	if err != nil {
		t.Fatalf("IssueToken() error: %v", err)
	}

	if _, err := verifier.Verify(tokenString); err != ErrInvalidToken {
		t.Errorf("Verify() error = %v, want ErrInvalidToken (unknown kid)", err)
	}
}

func TestVerify_MissingKidFallsBackToPrimary(t *testing.T) {
	key := testKey(t)
	verifier := singleKeyVerifier("primary", key, &fakeRevocationChecker{revoked: map[string]bool{}})

	// Issue a token the way a pre-rotation Issuer would have: no kid
	// header at all, simulating a token issued before this feature
	// existed - bypasses Issuer (which always sets kid) to build one
	// directly.
	now := time.Now()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "alice",
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
		},
		Type: TokenTypeAccess,
	}
	tokenString, err := jwt.NewWithClaims(jwt.SigningMethodES256, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign no-kid token: %v", err)
	}

	if _, err := verifier.Verify(tokenString); err != nil {
		t.Errorf("Verify() error = %v, want a valid no-kid token falling back to the primary key", err)
	}
}
