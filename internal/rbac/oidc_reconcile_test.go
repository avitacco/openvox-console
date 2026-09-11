package rbac

import (
	"context"
	"testing"
)

func assignedVia(t *testing.T, s *Store, userID, roleID int64) (string, bool) {
	t.Helper()
	var via string
	err := s.pool.QueryRow(context.Background(),
		`SELECT assigned_via FROM user_roles WHERE user_id = $1 AND role_id = $2`, userID, roleID,
	).Scan(&via)
	if err != nil {
		return "", false
	}
	return via, true
}

func TestReconcileOIDCRoles_AssignsDesiredRole(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateOIDCUser(ctx, "rbac-reconcile-test-sub-1", "rbac-reconcile-test-user-1")
	if err != nil {
		t.Fatalf("CreateOIDCUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), u.ID) })

	r, err := s.CreateRole(ctx, "rbac-reconcile-test-role-1", []string{"nodes:read"})
	if err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteRole(context.Background(), r.ID) })

	added, removed, err := s.ReconcileOIDCRoles(ctx, u.ID, []int64{r.ID})
	if err != nil {
		t.Fatalf("ReconcileOIDCRoles() error: %v", err)
	}
	if len(added) != 1 || added[0] != r.ID {
		t.Errorf("added = %v, want [%d]", added, r.ID)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want none", removed)
	}

	via, ok := assignedVia(t, s, u.ID, r.ID)
	if !ok {
		t.Fatal("expected role to be assigned after reconciliation")
	}
	if via != "oidc" {
		t.Errorf("assigned_via = %q, want %q", via, "oidc")
	}
}

func TestReconcileOIDCRoles_UnassignsStaleOIDCRole(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateOIDCUser(ctx, "rbac-reconcile-test-sub-2", "rbac-reconcile-test-user-2")
	if err != nil {
		t.Fatalf("CreateOIDCUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), u.ID) })

	r, err := s.CreateRole(ctx, "rbac-reconcile-test-role-2", []string{"nodes:read"})
	if err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteRole(context.Background(), r.ID) })

	// First login: claim maps to this role.
	if _, _, err := s.ReconcileOIDCRoles(ctx, u.ID, []int64{r.ID}); err != nil {
		t.Fatalf("first ReconcileOIDCRoles() error: %v", err)
	}
	if _, ok := assignedVia(t, s, u.ID, r.ID); !ok {
		t.Fatal("role should be assigned after first reconciliation")
	}

	// Second login: claim no longer maps to this role.
	added, removed, err := s.ReconcileOIDCRoles(ctx, u.ID, []int64{})
	if err != nil {
		t.Fatalf("second ReconcileOIDCRoles() error: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("added = %v, want none", added)
	}
	if len(removed) != 1 || removed[0] != r.ID {
		t.Errorf("removed = %v, want [%d]", removed, r.ID)
	}
	if _, ok := assignedVia(t, s, u.ID, r.ID); ok {
		t.Error("role should be unassigned after reconciliation drops it from the desired set")
	}
}

func TestReconcileOIDCRoles_PreservesManualRoleNotInDesiredSet(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateOIDCUser(ctx, "rbac-reconcile-test-sub-3", "rbac-reconcile-test-user-3")
	if err != nil {
		t.Fatalf("CreateOIDCUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), u.ID) })

	r, err := s.CreateRole(ctx, "rbac-reconcile-test-role-3", []string{"rbac:admin"})
	if err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteRole(context.Background(), r.ID) })

	// An administrator assigns this role manually - it should never be
	// touched by OIDC reconciliation.
	if err := s.AssignRole(ctx, u.ID, r.ID); err != nil {
		t.Fatalf("AssignRole() error: %v", err)
	}

	// Reconciliation with an empty desired set (no matching claims).
	added, removed, err := s.ReconcileOIDCRoles(ctx, u.ID, []int64{})
	if err != nil {
		t.Fatalf("ReconcileOIDCRoles() error: %v", err)
	}
	if len(added) != 0 || len(removed) != 0 {
		t.Errorf("added = %v, removed = %v, want both empty (manual role untouched)", added, removed)
	}

	via, ok := assignedVia(t, s, u.ID, r.ID)
	if !ok {
		t.Fatal("manually assigned role was removed by OIDC reconciliation")
	}
	if via != "manual" {
		t.Errorf("assigned_via = %q, want %q (must not be overwritten)", via, "manual")
	}
}

func TestReconcileOIDCRoles_ManualRoleAlsoInDesiredSetStaysManual(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateOIDCUser(ctx, "rbac-reconcile-test-sub-4", "rbac-reconcile-test-user-4")
	if err != nil {
		t.Fatalf("CreateOIDCUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), u.ID) })

	r, err := s.CreateRole(ctx, "rbac-reconcile-test-role-4", []string{"nodes:read"})
	if err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteRole(context.Background(), r.ID) })

	if err := s.AssignRole(ctx, u.ID, r.ID); err != nil {
		t.Fatalf("AssignRole() error: %v", err)
	}

	// This role also happens to be in the OIDC-mapped desired set - the
	// existing manual row must be left as-is (ON CONFLICT DO NOTHING),
	// not "upgraded" to oidc-managed.
	added, removed, err := s.ReconcileOIDCRoles(ctx, u.ID, []int64{r.ID})
	if err != nil {
		t.Fatalf("ReconcileOIDCRoles() error: %v", err)
	}
	if len(added) != 0 || len(removed) != 0 {
		t.Errorf("added = %v, removed = %v, want both empty (existing manual row untouched)", added, removed)
	}

	via, ok := assignedVia(t, s, u.ID, r.ID)
	if !ok {
		t.Fatal("role missing after reconciliation")
	}
	if via != "manual" {
		t.Errorf("assigned_via = %q, want %q (must not flip to oidc)", via, "manual")
	}

	// And a later login where the claim no longer maps to it must NOT
	// remove it, since it's still manually assigned.
	if _, _, err := s.ReconcileOIDCRoles(ctx, u.ID, []int64{}); err != nil {
		t.Fatalf("second ReconcileOIDCRoles() error: %v", err)
	}
	if _, ok := assignedVia(t, s, u.ID, r.ID); !ok {
		t.Error("manually assigned role was removed once no longer claim-mapped")
	}
}
