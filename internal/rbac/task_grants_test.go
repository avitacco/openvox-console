package rbac

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

func taskRole(t *testing.T, s *Store, name string, perms, patterns []string, groups []int64) Role {
	t.Helper()
	r, err := s.CreateRoleFull(context.Background(), Role{Name: name, Permissions: perms, NodeGroups: groups, TaskPatterns: patterns})
	if err != nil {
		t.Fatalf("CreateRoleFull(%q): %v", name, err)
	}
	t.Cleanup(func() { _ = s.DeleteRole(context.Background(), r.ID) })
	return r
}

func TestUserTaskGrants(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	web := testNodeGroup(t, s, "task-grants-web")
	db := testNodeGroup(t, s, "task-grants-db")

	user := func(name string, roles ...Role) User {
		u, err := s.CreateUser(ctx, name, "password-long-enough")
		if err != nil {
			t.Fatalf("CreateUser(%q): %v", name, err)
		}
		t.Cleanup(func() { _ = s.DeleteUser(context.Background(), u.ID) })
		for _, r := range roles {
			if err := s.AssignRole(ctx, u.ID, r.ID); err != nil {
				t.Fatalf("AssignRole: %v", err)
			}
		}
		return u
	}
	run := []string{"orchestrator:run"}

	patchWeb := taskRole(t, s, "tg-patch-web", run, []string{"patch::*", "package::install"}, []int64{web})
	anyDB := taskRole(t, s, "tg-any-db", run, nil, []int64{db})
	readOnly := taskRole(t, s, "tg-read", []string{"orchestrator:read"}, []string{"patch::*"}, []int64{web})
	noGroups := taskRole(t, s, "tg-nogroups", run, []string{"a::b"}, []int64{})

	t.Run("a restricted and an unrestricted role are both granted", func(t *testing.T) {
		got, err := s.UserTaskGrants(ctx, user("tg-user-two", patchWeb, anyDB, readOnly).ID)
		if err != nil {
			t.Fatal(err)
		}
		want := []taskscope.Grant{
			{Tasks: []string{"patch::*", "package::install"}, Groups: []int64{web}},
			{Tasks: []string{taskscope.Everything}, Groups: []int64{db}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("grants = %+v\nwant     %+v", got, want)
		}
	})

	t.Run("no role has a list: no limit", func(t *testing.T) {
		got, err := s.UserTaskGrants(ctx, user("tg-user-plain", anyDB).ID)
		if err != nil || got != nil {
			t.Errorf("grants = %#v, %v; want nil", got, err)
		}
	})

	t.Run("a list on a role without run permission is ignored", func(t *testing.T) {
		got, err := s.UserTaskGrants(ctx, user("tg-user-read", readOnly, anyDB).ID)
		if err != nil || got != nil {
			t.Errorf("grants = %#v, %v; want nil", got, err)
		}
	})

	t.Run("a user with no roles", func(t *testing.T) {
		got, err := s.UserTaskGrants(ctx, user("tg-user-none").ID)
		if err != nil || got != nil {
			t.Errorf("grants = %#v, %v; want nil", got, err)
		}
	})

	t.Run("a role with no node groups keeps its pairing", func(t *testing.T) {
		got, err := s.UserTaskGrants(ctx, user("tg-user-nogroups", noGroups).ID)
		if err != nil || len(got) != 1 || len(got[0].Groups) != 0 || got[0].Fleet {
			t.Errorf("grants = %#v, %v; want one grant on no groups", got, err)
		}
	})
}

func TestServiceTokenGrants(t *testing.T) {
	if ServiceTokenGrants(nil) != nil || ServiceTokenGrants([]string{}) != nil {
		t.Error("a token with no list should state no limit")
	}
	got := ServiceTokenGrants([]string{"deploy::*"})
	if want := []taskscope.Grant{{Tasks: []string{"deploy::*"}, Fleet: true}}; !reflect.DeepEqual(got, want) {
		t.Errorf("grants = %+v, want %+v", got, want)
	}
}

func TestIssue_TaskGrantsRoundTripAndAnEmptyListIsRefused(t *testing.T) {
	verifier, issuer := testVerifierWithRevoked(t, "")
	grants := []taskscope.Grant{{Tasks: []string{"patch::*"}, Groups: []int64{4}}, {Tasks: []string{"*"}, Fleet: true}}

	signed, _, err := issuer.IssueUserTokenWithTasks("alice", TokenTypeAccess, []string{"orchestrator:run"}, nil, grants, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := verifier.Verify(signed)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(claims.TaskGrants, grants) {
		t.Errorf("grants came back as %+v", claims.TaskGrants)
	}

	signed, _, err = issuer.IssueTokenWithTasks("ci", TokenTypeService, []string{"orchestrator:run"}, ServiceTokenGrants([]string{"deploy::*"}), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claims, _ = verifier.Verify(signed); len(claims.TaskGrants) != 1 || !claims.TaskGrants[0].Fleet {
		t.Errorf("a service token's grants = %+v", claims.TaskGrants)
	}

	// An empty non-nil list would be written as no claim at all and so
	// read as "no limit": the issuer must not let it happen.
	if _, _, err := issuer.IssueUserTokenWithTasks("alice", TokenTypeAccess, nil, nil, []taskscope.Grant{}, time.Minute); err == nil {
		t.Error("an empty list of grants was written to a user token")
	}
	if _, _, err := issuer.IssueTokenWithTasks("ci", TokenTypeService, nil, []taskscope.Grant{}, time.Minute); err == nil {
		t.Error("an empty list of grants was written to a service token")
	}
}

func TestIssue_WithoutGrantsWritesNoClaim(t *testing.T) {
	_, issuer := testVerifierWithRevoked(t, "")
	_, claims, err := issuer.IssueUserToken("alice", TokenTypeAccess, []string{"orchestrator:run"}, nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(claims)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if _, present := m["taskGrants"]; present {
		t.Errorf("a token with no limit carries taskGrants: %s", raw)
	}
}

// A token issued before task permissions existed has no claim, and
// reads as unrestricted when resolved.
func TestClaims_TokenWithoutTaskGrantsIsUnrestricted(t *testing.T) {
	var claims Claims
	if err := json.Unmarshal([]byte(`{"sub":"alice","type":"access","permissions":["orchestrator:run"],"nodeScopes":{"orchestrator:run":[1]}}`), &claims); err != nil {
		t.Fatal(err)
	}
	if claims.TaskGrants != nil {
		t.Fatalf("grants = %v, want nil", claims.TaskGrants)
	}
	scope, err := taskscope.Resolve(context.Background(), claims.TaskGrants, nil)
	if err != nil || !scope.Allows("any::task") || scope.Restricted() {
		t.Errorf("a token without the claim is restricted: %v", err)
	}
}

func TestLogin_AndRefresh_TokensCarryTheUsersTaskGrants(t *testing.T) {
	svc, store, _ := newTestAuthService(t)
	store.tasks = map[int64][]taskscope.Grant{1: {{Tasks: []string{"patch::*"}, Groups: []int64{3}}}}

	pair, err := svc.Login(context.Background(), "alice", "correct-password")
	if err != nil {
		t.Fatal(err)
	}
	for name, token := range map[string]string{"access": pair.AccessToken, "refresh": pair.RefreshToken} {
		claims, err := svc.verifier.Verify(token)
		if err != nil || !reflect.DeepEqual(claims.TaskGrants, store.tasks[1]) {
			t.Errorf("%s token grants = %+v (%v)", name, claims.TaskGrants, err)
		}
	}

	// A role narrowed after login reaches the next token at refresh.
	store.tasks[1] = []taskscope.Grant{{Tasks: []string{"deploy::*"}, Groups: []int64{3}}}
	pair, err = svc.Refresh(context.Background(), pair.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims, _ := svc.verifier.Verify(pair.AccessToken); !reflect.DeepEqual(claims.TaskGrants, store.tasks[1]) {
		t.Errorf("after a role change, refresh grants = %+v", claims.TaskGrants)
	}

	// And a user who loses the limit gets a token with no claim.
	store.tasks = nil
	pair, err = svc.Refresh(context.Background(), pair.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims, _ := svc.verifier.Verify(pair.AccessToken); claims.TaskGrants != nil {
		t.Errorf("a user with no limit carries grants: %+v", claims.TaskGrants)
	}
}
