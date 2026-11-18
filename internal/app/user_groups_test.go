package app_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/runtime"
	"github.com/voxpupuli/enterprise-console/internal/testapp"
)

// A user whose only roles come from a group is limited by those roles'
// task allowlists exactly as if the roles had been assigned to them, and
// stops being when they leave the group.
func TestUserGroups_AMembersTokenCarriesTheGroupsRoles(t *testing.T) {
	cluster := testapp.NewCluster(t)
	inst := cluster.Start(testapp.Config{Mode: runtime.ModeAll})
	c := sweepClient{t: t, inst: inst}
	admin := inst.Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)

	type idOnly struct {
		ID int64 `json:"id"`
	}
	var role, user, group idOnly
	c.send(http.MethodPost, "/api/v1/roles", admin, map[string]any{
		"name": "grp-patch", "permissions": []string{"orchestrator:run"}, "taskPatterns": []string{"patch::*"},
	}, &role)
	const password = "a-long-enough-password"
	c.send(http.MethodPost, "/api/v1/users", admin, map[string]string{"username": "grouped", "password": password}, &user)
	c.send(http.MethodPost, "/api/v1/user-groups", admin, map[string]string{"name": "patchers"}, &group)
	for _, path := range []string{
		fmt.Sprintf("/api/v1/user-groups/%d/roles/%d", group.ID, role.ID),
		fmt.Sprintf("/api/v1/user-groups/%d/members/%d", group.ID, user.ID),
	} {
		if status, body := c.do(http.MethodPost, path, admin, nil); status != http.StatusNoContent {
			t.Fatalf("POST %s = %d %s", path, status, body)
		}
	}

	const node = "grouped-node.example.test"
	start := func(token, task string) int {
		status, _ := c.do(http.MethodPost, "/api/v1/orchestrator/tasks", token, map[string]any{"targets": []string{node}, "task": task})
		return status
	}

	member := inst.Login(t, "grouped", password)
	// Allowed as far as the task catalogue, which this console does not
	// have, is concerned: 503. The role's own allowlist still applies.
	if got := start(member, "patch::apply"); got != http.StatusServiceUnavailable {
		t.Errorf("a task the group's role allows = %d, want it allowed", got)
	}
	if got := start(member, "package::install"); got != http.StatusForbidden {
		t.Errorf("a task the group's role does not allow = %d, want 403", got)
	}

	if status, body := c.do(http.MethodDelete, fmt.Sprintf("/api/v1/user-groups/%d/members/%d", group.ID, user.ID), admin, nil); status != http.StatusNoContent {
		t.Fatalf("remove member = %d %s", status, body)
	}
	// An open session keeps what it was issued; a new login does not.
	if got := start(inst.Login(t, "grouped", password), "patch::apply"); got != http.StatusForbidden {
		t.Errorf("after leaving the group, a new login = %d, want 403", got)
	}
}
