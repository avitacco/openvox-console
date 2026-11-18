package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/rbac"
	"github.com/voxpupuli/enterprise-console/internal/scheduler"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// fakeOwners is an rbac store with users, their permissions and node
// groups, and service tokens, all as they stand "now".
type fakeOwners struct {
	users       map[string]int64
	permissions map[int64][]string
	groups      map[int64]map[string][]int64
	tasks       map[int64][]taskscope.Grant // absent: no limit
	tokens      []rbac.ServiceToken
}

func (f *fakeOwners) UserTaskGrants(_ context.Context, id int64) ([]taskscope.Grant, error) {
	return f.tasks[id], nil
}

func (f *fakeOwners) GetUserByUsername(_ context.Context, username string) (rbac.User, error) {
	id, ok := f.users[username]
	if !ok {
		return rbac.User{}, rbac.ErrNotFound
	}
	return rbac.User{ID: id, Username: username}, nil
}

func (f *fakeOwners) UserPermissions(_ context.Context, id int64) ([]string, error) {
	return f.permissions[id], nil
}

func (f *fakeOwners) UserNodeScopes(_ context.Context, id int64) (map[string][]int64, error) {
	return f.groups[id], nil
}

func (f *fakeOwners) ListServiceTokens(context.Context) ([]rbac.ServiceToken, error) {
	return f.tokens, nil
}

// groupScopes resolves group 1 to every node and any other group to a
// node named after it.
type groupScopes struct{}

func (groupScopes) For(_ context.Context, groupIDs []int64) (nodescope.Scope, error) {
	var nodes []string
	for _, id := range groupIDs {
		if id == 1 {
			return nodescope.Everything(), nil
		}
		nodes = append(nodes, map[int64]string{2: "web1", 3: "db1"}[id])
	}
	return nodescope.Of(nodes...), nil
}

func TestScheduleAuthority(t *testing.T) {
	owners := &fakeOwners{
		users: map[string]int64{"alice": 10, "bob": 11, "carol": 12},
		permissions: map[int64][]string{
			10: {"orchestrator:read", "orchestrator:run"},
			11: {"orchestrator:read"}, // lost orchestrator:run
			12: {"orchestrator:run"},
		},
		groups: map[int64]map[string][]int64{
			10: {"orchestrator:run": {1}, "orchestrator:read": {1}},
			11: {"orchestrator:read": {1}},
			// Narrowed since the schedule was saved: reads are still
			// fleet-wide, but runs are on the web group only.
			12: {"orchestrator:run": {2}, "nodes:read": {1}},
		},
		tokens: []rbac.ServiceToken{
			{Name: "ci", Permissions: []string{"orchestrator:run"}},
			{Name: "reader", Permissions: []string{"orchestrator:read"}},
			// A token named like a user is still not that user.
			{Name: "bob", Permissions: []string{"orchestrator:run"}},
		},
	}
	authority := scheduleAuthority(owners, groupScopes{})
	ctx := context.Background()

	allowed := []struct {
		owner     scheduler.Owner
		all       bool
		certnames []string
	}{
		{scheduler.Owner{Name: "alice", Kind: scheduler.OwnerUser}, true, nil},
		{scheduler.Owner{Name: "carol", Kind: scheduler.OwnerUser}, false, []string{"web1"}},
		{scheduler.Owner{Name: "ci", Kind: scheduler.OwnerServiceToken}, true, nil},
		{scheduler.Owner{Name: "bob", Kind: scheduler.OwnerServiceToken}, true, nil},
	}
	for _, c := range allowed {
		auth, err := authority(ctx, c.owner)
		scope := auth.Scope
		if err != nil || scope.All() != c.all || !reflect.DeepEqual(scope.Certnames(), c.certnames) {
			t.Errorf("%+v: scope all=%v %v, err %v; want all=%v %v", c.owner, scope.All(), scope.Certnames(), err, c.all, c.certnames)
		}
	}

	refused := map[string]scheduler.Owner{
		"a user who lost the permission":         {Name: "bob", Kind: scheduler.OwnerUser},
		"a deleted user":                         {Name: "dave", Kind: scheduler.OwnerUser},
		"a revoked service token":                {Name: "old-ci", Kind: scheduler.OwnerServiceToken},
		"a service token without the permission": {Name: "reader", Kind: scheduler.OwnerServiceToken},
		"a user's name as a service token":       {Name: "alice", Kind: scheduler.OwnerServiceToken},
		"an unknown kind":                        {Name: "alice", Kind: "robot"},
	}
	for name, owner := range refused {
		if auth, err := authority(ctx, owner); !errors.Is(err, scheduler.ErrNoAuthority) || auth.Scope.All() || len(auth.Scope.Certnames()) != 0 || auth.Tasks.Allows("any::task") {
			t.Errorf("%s: scope %v, err %v; want ErrNoAuthority, no nodes and no tasks", name, auth.Scope.Certnames(), err)
		}
	}
}

// What a schedule's owner may run is read from their roles at the moment
// it fires: narrowing a role, or a token's list, takes effect at once.
func TestScheduleAuthority_TasksAreReadFromTheOwnersRolesNow(t *testing.T) {
	owners := &fakeOwners{
		users:       map[string]int64{"alice": 10},
		permissions: map[int64][]string{10: {"orchestrator:run"}},
		groups:      map[int64]map[string][]int64{10: {"orchestrator:run": {2}}},
		tokens: []rbac.ServiceToken{
			{Name: "deployer", Permissions: []string{"orchestrator:run"}, TaskPatterns: []string{"deploy::*"}},
			{Name: "anything", Permissions: []string{"orchestrator:run"}},
		},
	}
	authority := scheduleAuthority(owners, groupScopes{})
	ctx := context.Background()
	alice := scheduler.Owner{Name: "alice", Kind: scheduler.OwnerUser}

	// No limit to begin with.
	auth, err := authority(ctx, alice)
	if err != nil || !auth.Tasks.Allows("package::install") || auth.Tasks.Restricted() {
		t.Fatalf("before narrowing: %v, restricted %v", err, auth.Tasks.Restricted())
	}

	// The role is narrowed to patching on the web group.
	owners.tasks = map[int64][]taskscope.Grant{10: {{Tasks: []string{"patch::*"}, Groups: []int64{2}}}}
	auth, err = authority(ctx, alice)
	if err != nil || auth.Tasks.Allows("package::install") || !auth.Tasks.Allows("patch::apply") {
		t.Errorf("after narrowing: allows package::install = %v, patch::apply = %v (%v)", auth.Tasks.Allows("package::install"), auth.Tasks.Allows("patch::apply"), err)
	}
	if got := auth.Tasks.NodesFor("patch::apply").Certnames(); !reflect.DeepEqual(got, []string{"web1"}) {
		t.Errorf("patch::apply may run on %v, want the web node", got)
	}

	// A service token's list is its own, and fleet-wide.
	auth, err = authority(ctx, scheduler.Owner{Name: "deployer", Kind: scheduler.OwnerServiceToken})
	if err != nil || auth.Tasks.Allows("package::install") || !auth.Tasks.NodesFor("deploy::app").All() {
		t.Errorf("a deploy-only token: %v", err)
	}
	auth, err = authority(ctx, scheduler.Owner{Name: "anything", Kind: scheduler.OwnerServiceToken})
	if err != nil || auth.Tasks.Restricted() {
		t.Errorf("a token with no list is restricted: %v", err)
	}
}
