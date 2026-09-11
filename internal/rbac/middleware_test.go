package rbac

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testVerifierWithRevoked(t *testing.T, revokedJTI string) (*Verifier, *Issuer) {
	t.Helper()
	key := testKey(t)
	issuer := NewIssuer(key, testKid)
	checker := &fakeRevocationChecker{revoked: map[string]bool{}}
	if revokedJTI != "" {
		checker.revoked[revokedJTI] = true
	}
	return singleKeyVerifier(testKid, key, checker), issuer
}

func handlerCalled() (http.HandlerFunc, *bool) {
	called := false
	return func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}, &called
}

func TestAuthorize_MissingTokenRejected(t *testing.T) {
	verifier, _ := testVerifierWithRevoked(t, "")
	h, called := handlerCalled()

	rec := httptest.NewRecorder()
	verifier.Authorize("nodes:read", h)(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if *called {
		t.Error("handler should not have been called")
	}
}

func TestAuthorize_MalformedTokenRejected(t *testing.T) {
	verifier, _ := testVerifierWithRevoked(t, "")
	h, called := handlerCalled()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rec := httptest.NewRecorder()
	verifier.Authorize("nodes:read", h)(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if *called {
		t.Error("handler should not have been called")
	}
}

func TestAuthorize_ExpiredTokenRejected(t *testing.T) {
	verifier, issuer := testVerifierWithRevoked(t, "")
	token, _, err := issuer.IssueToken("alice", TokenTypeAccess, []string{"nodes:read"}, -time.Minute)
	if err != nil {
		t.Fatalf("IssueToken() error: %v", err)
	}
	h, called := handlerCalled()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	verifier.Authorize("nodes:read", h)(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if *called {
		t.Error("handler should not have been called")
	}
}

func TestAuthorize_RevokedTokenRejected(t *testing.T) {
	key := testKey(t)
	issuer := NewIssuer(key, testKid)
	token, claims, err := issuer.IssueToken("alice", TokenTypeAccess, []string{"nodes:read"}, AccessTokenTTL)
	if err != nil {
		t.Fatalf("IssueToken() error: %v", err)
	}
	verifier := singleKeyVerifier(testKid, key, &fakeRevocationChecker{revoked: map[string]bool{claims.ID: true}})
	h, called := handlerCalled()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	verifier.Authorize("nodes:read", h)(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if *called {
		t.Error("handler should not have been called")
	}
}

func TestAuthorize_ValidTokenWithoutPermissionRejected(t *testing.T) {
	verifier, issuer := testVerifierWithRevoked(t, "")
	token, _, err := issuer.IssueToken("alice", TokenTypeAccess, []string{"nodes:read"}, AccessTokenTTL)
	if err != nil {
		t.Fatalf("IssueToken() error: %v", err)
	}
	h, called := handlerCalled()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	verifier.Authorize("classifier:write", h)(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if *called {
		t.Error("handler should not have been called")
	}
}

func TestAuthorize_ValidTokenWithPermissionInvokesHandler(t *testing.T) {
	verifier, issuer := testVerifierWithRevoked(t, "")
	token, _, err := issuer.IssueToken("alice", TokenTypeAccess, []string{"nodes:read"}, AccessTokenTTL)
	if err != nil {
		t.Fatalf("IssueToken() error: %v", err)
	}
	h, called := handlerCalled()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	verifier.Authorize("nodes:read", h)(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !*called {
		t.Error("handler should have been called")
	}
}
