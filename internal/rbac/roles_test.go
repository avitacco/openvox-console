package rbac

import (
	"context"
	"reflect"
	"sort"
	"testing"
)

func TestCreateRole_RoundTripsPermissions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.CreateRole(ctx, "rbac-store-test-role", []string{"nodes:read", "classifier:read"})
	if err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteRole(context.Background(), r.ID) })

	got, err := s.GetRole(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetRole() error: %v", err)
	}
	sort.Strings(got.Permissions)
	want := []string{"classifier:read", "nodes:read"}
	if !reflect.DeepEqual(got.Permissions, want) {
		t.Errorf("Permissions = %v, want %v", got.Permissions, want)
	}
}

func TestGetRoleByName(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.CreateRole(ctx, "rbac-store-test-role-by-name", []string{"nodes:read"})
	if err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteRole(context.Background(), r.ID) })

	got, err := s.GetRoleByName(ctx, "rbac-store-test-role-by-name")
	if err != nil {
		t.Fatalf("GetRoleByName() error: %v", err)
	}
	if got.ID != r.ID {
		t.Errorf("ID = %d, want %d", got.ID, r.ID)
	}
}

func TestGetRoleByName_NotFound(t *testing.T) {
	s := testStore(t)
	_, err := s.GetRoleByName(context.Background(), "rbac-store-test-role-does-not-exist")
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestUpdateRole_ReplacesPermissions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.CreateRole(ctx, "rbac-store-test-role-update", []string{"nodes:read"})
	if err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteRole(context.Background(), r.ID) })

	r.Permissions = []string{"classifier:write"}
	if err := s.UpdateRole(ctx, r); err != nil {
		t.Fatalf("UpdateRole() error: %v", err)
	}

	got, err := s.GetRole(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetRole() error: %v", err)
	}
	if len(got.Permissions) != 1 || got.Permissions[0] != "classifier:write" {
		t.Errorf("Permissions = %v, want [classifier:write]", got.Permissions)
	}
}

func TestUserPermissions_UnionAcrossAssignedRoles(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "rbac-store-test-permissions-user", "password123")
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), u.ID) })

	roleA, err := s.CreateRole(ctx, "rbac-store-test-role-a", []string{"nodes:read"})
	if err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteRole(context.Background(), roleA.ID) })

	roleB, err := s.CreateRole(ctx, "rbac-store-test-role-b", []string{"classifier:write"})
	if err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteRole(context.Background(), roleB.ID) })

	if err := s.AssignRole(ctx, u.ID, roleA.ID); err != nil {
		t.Fatalf("AssignRole() error: %v", err)
	}
	if err := s.AssignRole(ctx, u.ID, roleB.ID); err != nil {
		t.Fatalf("AssignRole() error: %v", err)
	}

	perms, err := s.UserPermissions(ctx, u.ID)
	if err != nil {
		t.Fatalf("UserPermissions() error: %v", err)
	}
	sort.Strings(perms)
	want := []string{"classifier:write", "nodes:read"}
	if !reflect.DeepEqual(perms, want) {
		t.Errorf("UserPermissions() = %v, want %v", perms, want)
	}

	if err := s.UnassignRole(ctx, u.ID, roleB.ID); err != nil {
		t.Fatalf("UnassignRole() error: %v", err)
	}
	perms, err = s.UserPermissions(ctx, u.ID)
	if err != nil {
		t.Fatalf("UserPermissions() error: %v", err)
	}
	if len(perms) != 1 || perms[0] != "nodes:read" {
		t.Errorf("UserPermissions() after unassign = %v, want [nodes:read]", perms)
	}
}
