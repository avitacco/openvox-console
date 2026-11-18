package rbac

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/taskscope"
	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

func testGroup(t *testing.T, s *Store, name string) Group {
	t.Helper()
	g, err := s.CreateGroup(context.Background(), name)
	if err != nil {
		t.Fatalf("CreateGroup(%q): %v", name, err)
	}
	t.Cleanup(func() { _, _ = s.DeleteGroup(context.Background(), g.ID, true) })
	return g
}

func testUser(t *testing.T, s *Store, name string) User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), name, "password-long-enough")
	if err != nil {
		t.Fatalf("CreateUser(%q): %v", name, err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), u.ID) })
	return u
}

func TestGroups_NamesAreUniqueAndBounded(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	g := testGroup(t, s, "ug-web-ops")
	if g.MemberCount != 0 || len(g.Roles) != 0 {
		t.Errorf("a new group has %d members and %d roles, want none", g.MemberCount, len(g.Roles))
	}

	if _, err := s.CreateGroup(ctx, "UG-Web-Ops"); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("a name differing only in case: err = %v, want ErrDuplicateName", err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", MaxGroupNameLength+1)} {
		if _, err := s.CreateGroup(ctx, bad); !errors.Is(err, ErrInvalidGroupName) {
			t.Errorf("CreateGroup(%q) err = %v, want ErrInvalidGroupName", bad, err)
		}
	}

	renamed, err := s.RenameGroup(ctx, g.ID, "  ug-web-team ")
	if err != nil || renamed.Name != "ug-web-team" {
		t.Fatalf("RenameGroup = %+v, %v; want the trimmed name", renamed, err)
	}
	other := testGroup(t, s, "ug-other")
	if _, err := s.RenameGroup(ctx, other.ID, "UG-WEB-TEAM"); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("renaming onto an existing name: err = %v, want ErrDuplicateName", err)
	}
	if _, err := s.RenameGroup(ctx, -1, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("renaming a missing group: err = %v, want ErrNotFound", err)
	}
}

func TestGroups_MembersAndRoles(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	g := testGroup(t, s, "ug-members")
	u := testUser(t, s, "ug-user-a")
	r := taskRole(t, s, "ug-role-a", []string{"nodes:read"}, nil, nil)

	for i := 0; i < 2; i++ { // twice: the second changes nothing
		if err := s.AddGroupMember(ctx, g.ID, u.ID); err != nil {
			t.Fatalf("AddGroupMember (%d): %v", i, err)
		}
		if err := s.GrantGroupRole(ctx, g.ID, r.ID); err != nil {
			t.Fatalf("GrantGroupRole (%d): %v", i, err)
		}
	}
	got, err := s.GetGroup(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.MemberCount != 1 || len(got.Members) != 1 || got.Members[0].Username != "ug-user-a" {
		t.Errorf("members = %+v, want just ug-user-a", got.Members)
	}
	if len(got.Roles) != 1 || got.Roles[0].Name != "ug-role-a" {
		t.Errorf("roles = %+v, want just ug-role-a", got.Roles)
	}

	if err := s.AddGroupMember(ctx, g.ID, -1); !errors.Is(err, ErrNotFound) {
		t.Errorf("adding a missing user: err = %v, want ErrNotFound", err)
	}
	if err := s.GrantGroupRole(ctx, g.ID, -1); !errors.Is(err, ErrNotFound) {
		t.Errorf("granting a missing role: err = %v, want ErrNotFound", err)
	}

	list, err := s.ListGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, lg := range list {
		if lg.ID == g.ID {
			found = true
			if lg.MemberCount != 1 || len(lg.Roles) != 1 || lg.Members != nil {
				t.Errorf("list entry = %+v, want a count and roles but no members", lg)
			}
		}
	}
	if !found {
		t.Error("the group is not in the list")
	}

	if err := s.RevokeGroupRole(ctx, g.ID, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveGroupMember(ctx, g.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetGroup(ctx, g.ID)
	if got.MemberCount != 0 || len(got.Roles) != 0 {
		t.Errorf("after removal: %+v, want empty", got)
	}
}

func TestEffectiveRoles_GroupRolesCountLikeDirectOnes(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	web := testNodeGroup(t, s, "ug-eff-web")
	db := testNodeGroup(t, s, "ug-eff-db")
	run := []string{"orchestrator:run"}

	patchWeb := taskRole(t, s, "ug-eff-patch-web", run, []string{"patch::*"}, []int64{web})
	anyDB := taskRole(t, s, "ug-eff-any-db", append([]string{"nodes:read"}, run...), nil, []int64{db})
	u := testUser(t, s, "ug-eff-user")
	g := testGroup(t, s, "ug-eff-group")
	if err := s.AddGroupMember(ctx, g.ID, u.ID); err != nil {
		t.Fatal(err)
	}

	t.Run("a role held only through a group gives permissions, scopes and grants", func(t *testing.T) {
		if err := s.GrantGroupRole(ctx, g.ID, anyDB.ID); err != nil {
			t.Fatal(err)
		}
		perms, _ := s.UserPermissions(ctx, u.ID)
		if !reflect.DeepEqual(perms, []string{"nodes:read", "orchestrator:run"}) {
			t.Errorf("permissions = %v", perms)
		}
		scopes, _ := s.UserNodeScopes(ctx, u.ID)
		if !reflect.DeepEqual(scopes["orchestrator:run"], []int64{db}) {
			t.Errorf("run scope = %v, want [%d]", scopes["orchestrator:run"], db)
		}
	})

	t.Run("pairing survives a group", func(t *testing.T) {
		// patch::* on web directly, any task on db through the group.
		if err := s.AssignRole(ctx, u.ID, patchWeb.ID); err != nil {
			t.Fatal(err)
		}
		grants, err := s.UserTaskGrants(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		want := []taskscope.Grant{
			{Tasks: []string{"*"}, Groups: []int64{db}},
			{Tasks: []string{"patch::*"}, Groups: []int64{web}},
		}
		sort.Slice(grants, func(i, j int) bool { return grants[i].Tasks[0] < grants[j].Tasks[0] })
		if !reflect.DeepEqual(grants, want) {
			t.Errorf("grants = %+v, want %+v", grants, want)
		}
	})

	t.Run("a role held twice counts once and outlives either way", func(t *testing.T) {
		if err := s.GrantGroupRole(ctx, g.ID, patchWeb.ID); err != nil {
			t.Fatal(err)
		}
		roles, err := s.ListUserRoles(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(roles) != 2 {
			t.Fatalf("roles = %d, want 2 (patch-web once, any-db once)", len(roles))
		}
		for _, r := range roles {
			if r.ID == patchWeb.ID && !reflect.DeepEqual(r.Via, []string{ViaDirect, "ug-eff-group"}) {
				t.Errorf("patch-web via = %v, want direct then the group", r.Via)
			}
			if r.ID == anyDB.ID && !reflect.DeepEqual(r.Via, []string{"ug-eff-group"}) {
				t.Errorf("any-db via = %v, want just the group", r.Via)
			}
		}
		grants, _ := s.UserTaskGrants(ctx, u.ID)
		if len(grants) != 2 {
			t.Errorf("grants = %d, want 2: a role held twice is one grant", len(grants))
		}

		if err := s.UnassignRole(ctx, u.ID, patchWeb.ID); err != nil {
			t.Fatal(err)
		}
		perms, _ := s.UserPermissions(ctx, u.ID)
		scopes, _ := s.UserNodeScopes(ctx, u.ID)
		if len(perms) == 0 || len(scopes["orchestrator:run"]) != 2 {
			t.Errorf("removing the direct assignment dropped the group's grant: perms=%v scopes=%v", perms, scopes)
		}
		if err := s.RevokeGroupRole(ctx, g.ID, patchWeb.ID); err != nil {
			t.Fatal(err)
		}
		scopes, _ = s.UserNodeScopes(ctx, u.ID)
		if !reflect.DeepEqual(scopes["orchestrator:run"], []int64{db}) {
			t.Errorf("after revoking, run scope = %v, want only the db group", scopes["orchestrator:run"])
		}
	})

	t.Run("leaving the group removes what it gave", func(t *testing.T) {
		if err := s.RemoveGroupMember(ctx, g.ID, u.ID); err != nil {
			t.Fatal(err)
		}
		perms, _ := s.UserPermissions(ctx, u.ID)
		roles, _ := s.ListUserRoles(ctx, u.ID)
		if len(perms) != 0 || len(roles) != 0 {
			t.Errorf("perms=%v roles=%v, want none", perms, roles)
		}
	})
}

func TestEffectiveRoles_NoGroupsMeansDirectRolesOnly(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	r := taskRole(t, s, "ug-direct-only", []string{"nodes:read"}, nil, nil)
	u := testUser(t, s, "ug-direct-user")
	if err := s.AssignRole(ctx, u.ID, r.ID); err != nil {
		t.Fatal(err)
	}
	roles, _ := s.ListUserRoles(ctx, u.ID)
	if len(roles) != 1 || !reflect.DeepEqual(roles[0].Via, []string{ViaDirect}) {
		t.Errorf("roles = %+v, want the one direct role", roles)
	}
	groups, _ := s.UserGroups(ctx, u.ID)
	if len(groups) != 0 {
		t.Errorf("groups = %v, want none", groups)
	}
}

func TestDeleteGroup_NeedsConfirmationWithMembers(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	g := testGroup(t, s, "ug-delete")
	for _, n := range []string{"ug-del-1", "ug-del-2", "ug-del-3"} {
		if err := s.AddGroupMember(ctx, g.ID, testUser(t, s, n).ID); err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.DeleteGroup(ctx, g.ID, false)
	var has *GroupHasMembersError
	if !errors.As(err, &has) || has.Members != 3 {
		t.Fatalf("DeleteGroup without confirm: err = %v, want 3 members", err)
	}
	if _, err := s.GetGroup(ctx, g.ID); err != nil {
		t.Fatalf("the group was deleted anyway: %v", err)
	}
	n, err := s.DeleteGroup(ctx, g.ID, true)
	if err != nil || n != 3 {
		t.Fatalf("DeleteGroup confirmed = %d, %v; want 3, nil", n, err)
	}
	if _, err := s.GetGroup(ctx, g.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after deleting: err = %v, want ErrNotFound", err)
	}
	if _, err := s.DeleteGroup(ctx, g.ID, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting twice: err = %v, want ErrNotFound", err)
	}
}

func TestDeletingAUserOrARoleCleansUpGroups(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	g := testGroup(t, s, "ug-cascade")
	u, err := s.CreateUser(ctx, "ug-cascade-user", "password-long-enough")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateRole(ctx, "ug-cascade-role", []string{"nodes:read"})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.AddGroupMember(ctx, g.ID, u.ID)
	_ = s.GrantGroupRole(ctx, g.ID, r.ID)

	if err := s.DeleteUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRole(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetGroup(ctx, g.ID)
	if got.MemberCount != 0 || len(got.Roles) != 0 {
		t.Errorf("group after deleting its user and role = %+v, want empty", got)
	}
}

func TestGroups_TheLastAdministratorCannotBeRemovedThroughAGroup(t *testing.T) {
	// A scratch database: the shared one always has other administrators.
	s := NewStore(testdb.ScratchPool(t))
	ctx := context.Background()
	admin, err := s.CreateRole(ctx, "scratch-admin", []string{"rbac:admin"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateUser(ctx, "scratch-admin-user", "password-long-enough")
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.CreateGroup(ctx, "admins")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.AddGroupMember(ctx, g.ID, u.ID)
	_ = s.GrantGroupRole(ctx, g.ID, admin.ID)

	if err := s.RemoveGroupMember(ctx, g.ID, u.ID); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("removing the only administrator: err = %v, want ErrLastAdmin", err)
	}
	if err := s.RevokeGroupRole(ctx, g.ID, admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("revoking the admin role: err = %v, want ErrLastAdmin", err)
	}
	if _, err := s.DeleteGroup(ctx, g.ID, true); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("deleting the group: err = %v, want ErrLastAdmin", err)
	}
	if got, err := s.GetGroup(ctx, g.ID); err != nil || got.MemberCount != 1 || len(got.Roles) != 1 {
		t.Fatalf("a refused change was applied anyway: %+v, %v", got, err)
	}

	// With a second administrator holding the role directly, each is allowed.
	other, _ := s.CreateUser(ctx, "scratch-admin-two", "password-long-enough")
	if err := s.AssignRole(ctx, other.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveGroupMember(ctx, g.ID, u.ID); err != nil {
		t.Errorf("removing one of two administrators: %v", err)
	}
	// A direct path is unchanged: the guard covers group changes only.
	if err := s.UnassignRole(ctx, other.ID, admin.ID); err != nil {
		t.Errorf("unassigning directly: %v", err)
	}
}
