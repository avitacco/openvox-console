package app

import (
	"context"
	"errors"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/rbac"
	"github.com/voxpupuli/enterprise-console/internal/scheduler"
	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

// A schedule follows its owner's groups at each firing: when the owner
// leaves the group that gave them the right to run it, it no longer has
// authority.
func TestUserGroups_AScheduleFollowsItsOwnersGroups(t *testing.T) {
	store := rbac.NewStore(testdb.Pool(t))
	ctx := context.Background()

	role, err := store.CreateRoleFull(ctx, rbac.Role{Name: "sched-grp-role", Permissions: []string{"orchestrator:run"}, TaskPatterns: []string{"patch::*"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.DeleteRole(ctx, role.ID) })
	user, err := store.CreateUser(ctx, "sched-grp-user", "password-long-enough")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.DeleteUser(ctx, user.ID) })
	group, err := store.CreateGroup(ctx, "sched-grp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = store.DeleteGroup(ctx, group.ID, true) })
	_ = store.GrantGroupRole(ctx, group.ID, role.ID)
	_ = store.AddGroupMember(ctx, group.ID, user.ID)

	authority := scheduleAuthority(store, groupScopes{})
	owner := scheduler.Owner{Kind: scheduler.OwnerUser, Name: "sched-grp-user"}

	got, err := authority(ctx, owner)
	if err != nil {
		t.Fatalf("a member's authority: %v", err)
	}
	if !got.Tasks.Allows("patch::apply") || got.Tasks.Allows("package::install") {
		t.Errorf("tasks allowed: patch::apply=%v package::install=%v, want true and false",
			got.Tasks.Allows("patch::apply"), got.Tasks.Allows("package::install"))
	}

	_ = store.RemoveGroupMember(ctx, group.ID, user.ID)
	if _, err := authority(ctx, owner); !errors.Is(err, scheduler.ErrNoAuthority) {
		t.Errorf("after leaving the group: err = %v, want ErrNoAuthority", err)
	}
}
