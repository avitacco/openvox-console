package rbac

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// fakeIdentityProvider is just enough of an OpenID Connect provider for
// OIDCService to complete a login against: discovery, keys, and a token
// endpoint that signs an ID token for one fixed subject.
type fakeIdentityProvider struct {
	server   *httptest.Server
	key      *rsa.PrivateKey
	clientID string
	subject  string
	username string

	mu    sync.Mutex
	nonce string
}

func newFakeIdentityProvider(t *testing.T, subject, username string) *fakeIdentityProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeIdentityProvider{key: key, clientID: "console", subject: subject, username: username}
	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer": p.server.URL, "authorization_endpoint": p.server.URL + "/authorize",
			"token_endpoint": p.server.URL + "/token", "jwks_uri": p.server.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "idp-key", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		p.mu.Lock()
		nonce := p.nonce
		p.mu.Unlock()
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss": p.server.URL, "sub": p.subject, "aud": p.clientID, "nonce": nonce,
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(),
			"preferred_username": p.username,
		})
		token.Header["kid"] = "idp-key"
		signed, err := token.SignedString(key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"access_token": "idp-access-token", "token_type": "Bearer", "expires_in": 60, "id_token": signed})
	})
	p.server = httptest.NewServer(mux)
	t.Cleanup(p.server.Close)
	return p
}

// login takes svc through a whole identity-provider login, as a browser
// redirected there and back would.
func (p *fakeIdentityProvider) login(t *testing.T, svc *OIDCService) (TokenPair, error) {
	t.Helper()
	ctx := context.Background()
	authURL, err := svc.AuthorizationURL(ctx)
	if err != nil {
		t.Fatalf("AuthorizationURL() error: %v", err)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.nonce = parsed.Query().Get("nonce")
	p.mu.Unlock()
	return svc.HandleCallback(ctx, "an-authorization-code", parsed.Query().Get("state"))
}

// Lockout is about guessing a password. A user who signs in through
// the identity provider is not guessing one, and is let in whether or
// not password login is locked for their account.
func TestLockout_IdentityProviderLoginIsNotRefused(t *testing.T) {
	e := newPolicyEnv(t)
	e.setPolicy(func(p *AccountPolicy) { p.LockoutThreshold = 3 })
	idp := newFakeIdentityProvider(t, "subject-"+e.tag, e.name("sso-user"))
	svc := NewOIDCService(context.Background(), discardLogger(), e.store, e.issuer, OIDCConfig{
		Issuer: idp.server.URL, ClientID: idp.clientID, ClientSecret: "secret",
		RedirectURL: "https://console.example.test/callback", Scopes: "openid profile", UsernameClaim: "preferred_username",
	}, noopRecordSystemActivity, noopRecordAudit)
	if !svc.Configured() {
		t.Fatal("the OIDC service did not accept the fake identity provider")
	}

	// A first login provisions the account.
	if _, err := idp.login(t, svc); err != nil {
		t.Fatalf("first identity-provider login: %v", err)
	}
	user, err := e.store.GetUserByUsername(context.Background(), e.name("sso-user"))
	if err != nil {
		t.Fatalf("the account was not provisioned: %v", err)
	}

	// Somebody guesses passwords for it until it is locked.
	e.fail(user, 3)
	if !e.read(user).Locked() {
		t.Fatal("not locked")
	}

	pair, err := idp.login(t, svc)
	if err != nil {
		t.Fatalf("identity-provider login while locked: %v", err)
	}
	claims, err := e.verifier.Verify(pair.AccessToken)
	if err != nil || claims.Subject != user.Username {
		t.Errorf("token = %+v, %v; want one for the locked account's user", claims, err)
	}
	// And it did not unlock password login behind the administrator's
	// back.
	if !e.read(user).Locked() {
		t.Error("an identity-provider login cleared the lock on password login")
	}
}
