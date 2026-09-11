package rbac

import (
	"context"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/messaging"
	"golang.org/x/crypto/bcrypt"
)

type fakeAuthStore struct {
	users       map[string]User // by username
	permissions map[int64][]string
}

func (f *fakeAuthStore) GetUserByUsername(_ context.Context, username string) (User, error) {
	u, ok := f.users[username]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (f *fakeAuthStore) UserPermissions(_ context.Context, userID int64) ([]string, error) {
	return f.permissions[userID], nil
}

// newTestAuthService returns an AuthService whose recorded audit events
// are collected in the returned slice pointer, so tests can assert on
// them directly rather than needing a real slog sink.
func newTestAuthService(t *testing.T) (*AuthService, *fakeAuthStore, *[]auditlog.Event) {
	t.Helper()

	key := testKey(t)
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	t.Cleanup(bus.Close)

	revoker := NewRevoker(bus, newFakeRevokedTokenStore())
	if err := revoker.Start(context.Background()); err != nil {
		t.Fatalf("revoker.Start() error: %v", err)
	}

	issuer := NewIssuer(key, testKid)
	verifier := singleKeyVerifier(testKid, key, revoker)

	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword() error: %v", err)
	}

	store := &fakeAuthStore{
		users: map[string]User{
			"alice": {ID: 1, Username: "alice", PasswordHash: string(hash)},
		},
		permissions: map[int64][]string{
			1: {"nodes:read", "classifier:read"},
		},
	}

	var recorded []auditlog.Event
	svc := NewAuthService(store, issuer, verifier, revoker, func(e auditlog.Event) {
		recorded = append(recorded, e)
	})
	return svc, store, &recorded
}

func TestLogin_Success(t *testing.T) {
	svc, _, recorded := newTestAuthService(t)

	pair, err := svc.Login(context.Background(), "alice", "correct-password")
	if err != nil {
		t.Fatalf("Login() error: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected both tokens to be issued")
	}

	claims, err := svc.verifier.Verify(pair.AccessToken)
	if err != nil {
		t.Fatalf("Verify(accessToken) error: %v", err)
	}
	if claims.Type != TokenTypeAccess {
		t.Errorf("access token Type = %q, want %q", claims.Type, TokenTypeAccess)
	}
	if !claims.HasPermission("nodes:read") || !claims.HasPermission("classifier:read") {
		t.Errorf("access token permissions = %v, want the user's roles", claims.Permissions)
	}

	refreshClaims, err := svc.verifier.Verify(pair.RefreshToken)
	if err != nil {
		t.Fatalf("Verify(refreshToken) error: %v", err)
	}
	if refreshClaims.Type != TokenTypeRefresh {
		t.Errorf("refresh token Type = %q, want %q", refreshClaims.Type, TokenTypeRefresh)
	}

	if len(*recorded) != 1 {
		t.Fatalf("recorded audit events = %v, want exactly 1", *recorded)
	}
	got := (*recorded)[0]
	if got.Action != "user.login.success" || got.Actor != "alice" {
		t.Errorf("recorded event = %+v, want action=user.login.success actor=alice", got)
	}
}

func TestLogin_WrongPasswordAndUnknownUsernameReturnTheSameError(t *testing.T) {
	svc, _, recorded := newTestAuthService(t)

	_, wrongPasswordErr := svc.Login(context.Background(), "alice", "not-the-password")
	_, unknownUserErr := svc.Login(context.Background(), "no-such-user", "anything")

	if wrongPasswordErr != ErrInvalidCredentials {
		t.Errorf("wrong password error = %v, want ErrInvalidCredentials", wrongPasswordErr)
	}
	if unknownUserErr != ErrInvalidCredentials {
		t.Errorf("unknown username error = %v, want ErrInvalidCredentials", unknownUserErr)
	}

	if len(*recorded) != 2 {
		t.Fatalf("recorded audit events = %v, want exactly 2", *recorded)
	}
	for i, wantActor := range []string{"alice", "no-such-user"} {
		got := (*recorded)[i]
		if got.Action != "user.login.failure" || got.Actor != wantActor {
			t.Errorf("event %d = %+v, want action=user.login.failure actor=%s", i, got, wantActor)
		}
	}
}

func TestRefresh_IssuesNewPairAndRevokesOldRefreshToken(t *testing.T) {
	svc, _, _ := newTestAuthService(t)

	original, err := svc.Login(context.Background(), "alice", "correct-password")
	if err != nil {
		t.Fatalf("Login() error: %v", err)
	}

	refreshed, err := svc.Refresh(context.Background(), original.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if refreshed.AccessToken == "" || refreshed.RefreshToken == "" {
		t.Fatal("expected a new token pair")
	}
	if refreshed.RefreshToken == original.RefreshToken {
		t.Error("expected a different refresh token after rotation")
	}

	// The old refresh token must no longer work.
	if _, err := svc.Refresh(context.Background(), original.RefreshToken); err != ErrInvalidCredentials {
		t.Errorf("re-using the old refresh token error = %v, want ErrInvalidCredentials", err)
	}
}

func TestRefresh_RejectsExpiredRefreshToken(t *testing.T) {
	svc, _, _ := newTestAuthService(t)

	expired, _, err := svc.issuer.IssueToken("alice", TokenTypeRefresh, []string{"nodes:read"}, -time.Minute)
	if err != nil {
		t.Fatalf("IssueToken() error: %v", err)
	}

	if _, err := svc.Refresh(context.Background(), expired); err != ErrInvalidCredentials {
		t.Errorf("Refresh() with expired token error = %v, want ErrInvalidCredentials", err)
	}
}

func TestRefresh_RejectsAccessTokenUsedAsRefreshToken(t *testing.T) {
	svc, _, _ := newTestAuthService(t)

	pair, err := svc.Login(context.Background(), "alice", "correct-password")
	if err != nil {
		t.Fatalf("Login() error: %v", err)
	}

	if _, err := svc.Refresh(context.Background(), pair.AccessToken); err != ErrInvalidCredentials {
		t.Errorf("Refresh() with an access token error = %v, want ErrInvalidCredentials", err)
	}
}
