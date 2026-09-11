package rbac

import (
	"context"
	"testing"
)

func TestSyncProfile_PopulatesFromClaims(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateOIDCUser(ctx, "rbac-profile-test-sub-1", "rbac-profile-test-user-1")
	if err != nil {
		t.Fatalf("CreateOIDCUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), u.ID) })

	svc := &OIDCService{store: s}
	claims := map[string]any{"given_name": "Ada", "family_name": "Lovelace", "email": "ada@example.com"}

	updated, err := svc.syncProfile(ctx, u, claims)
	if err != nil {
		t.Fatalf("syncProfile() error: %v", err)
	}
	if derefOrEmpty(updated.FirstName) != "Ada" || derefOrEmpty(updated.LastName) != "Lovelace" || derefOrEmpty(updated.Email) != "ada@example.com" {
		t.Fatalf("updated = %+v, want Ada Lovelace <ada@example.com>", updated)
	}

	stored, err := s.GetUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUser() error: %v", err)
	}
	if derefOrEmpty(stored.Email) != "ada@example.com" {
		t.Errorf("stored email = %q, want ada@example.com", derefOrEmpty(stored.Email))
	}
}

func TestSyncProfile_MissingClaimPreservesExistingValue(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateOIDCUser(ctx, "rbac-profile-test-sub-2", "rbac-profile-test-user-2")
	if err != nil {
		t.Fatalf("CreateOIDCUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), u.ID) })

	svc := &OIDCService{store: s}

	// First login sets a full profile from the ID token's claims.
	u, err = svc.syncProfile(ctx, u, map[string]any{"given_name": "Ada", "family_name": "Lovelace", "email": "ada@example.com"})
	if err != nil {
		t.Fatalf("first syncProfile() error: %v", err)
	}

	// A later login whose claims omit family_name (some providers don't
	// always send every optional claim) must not blank what's stored.
	updated, err := svc.syncProfile(ctx, u, map[string]any{"given_name": "Ada", "email": "ada@example.com"})
	if err != nil {
		t.Fatalf("second syncProfile() error: %v", err)
	}
	if derefOrEmpty(updated.LastName) != "Lovelace" {
		t.Errorf("LastName = %q, want it preserved as Lovelace", derefOrEmpty(updated.LastName))
	}
}

func TestSyncProfile_NoClaimsIsNoOp(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateOIDCUser(ctx, "rbac-profile-test-sub-3", "rbac-profile-test-user-3")
	if err != nil {
		t.Fatalf("CreateOIDCUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), u.ID) })

	svc := &OIDCService{store: s}

	updated, err := svc.syncProfile(ctx, u, map[string]any{})
	if err != nil {
		t.Fatalf("syncProfile() error: %v", err)
	}
	if updated.FirstName != nil || updated.LastName != nil || updated.Email != nil {
		t.Errorf("updated = %+v, want profile still unset", updated)
	}
}
