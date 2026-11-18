package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/rbac"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

func taskScopeFor(t *testing.T, claims *rbac.Claims) taskscope.Scope {
	t.Helper()
	groups := nodescope.NewResolver(func(_ context.Context, ids []int64) (bool, []string, error) {
		var nodes []string
		for _, id := range ids {
			switch id {
			case 1:
				nodes = append(nodes, "web1")
			case 2:
				nodes = append(nodes, "db1")
			}
		}
		return false, nodes, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if claims != nil {
		req = req.WithContext(rbac.WithClaims(req.Context(), claims))
	}
	scope, err := taskScopeFunc(groups)(req)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func TestTaskScopeFunc(t *testing.T) {
	run := []string{"orchestrator:read", "orchestrator:run"}

	t.Run("no claims allows nothing", func(t *testing.T) {
		if s := taskScopeFor(t, nil); s.Allows("any::task") || !s.Restricted() {
			t.Error("a request with no claims was allowed a task")
		}
	})

	t.Run("a token without grants is unrestricted", func(t *testing.T) {
		if s := taskScopeFor(t, &rbac.Claims{Permissions: run}); !s.Allows("any::task") || s.Restricted() {
			t.Error("a token with no grants is restricted")
		}
	})

	t.Run("the two-role example is paired", func(t *testing.T) {
		s := taskScopeFor(t, &rbac.Claims{Permissions: run, TaskGrants: []taskscope.Grant{
			{Tasks: []string{"patch::*"}, Groups: []int64{1}},
			{Tasks: []string{"*"}, Groups: []int64{2}},
		}})
		if got := s.NodesFor("patch::apply").Certnames(); len(got) != 2 {
			t.Errorf("patch::apply on %v, want web1 and db1", got)
		}
		if got := s.NodesFor("package::install").Certnames(); len(got) != 1 || got[0] != "db1" {
			t.Errorf("package::install on %v, want db1 only", got)
		}
	})

	t.Run("a read-only caller is not limited", func(t *testing.T) {
		s := taskScopeFor(t, &rbac.Claims{Permissions: []string{"orchestrator:read"}, TaskGrants: []taskscope.Grant{
			{Tasks: []string{"patch::*"}, Groups: []int64{1}},
		}})
		if !s.Allows("package::install") {
			t.Error("a caller who cannot start tasks was limited in what they may see")
		}
	})

	t.Run("a service token's grant is fleet-wide", func(t *testing.T) {
		s := taskScopeFor(t, &rbac.Claims{Permissions: run, TaskGrants: []taskscope.Grant{{Tasks: []string{"deploy::*"}, Fleet: true}}})
		if !s.NodesFor("deploy::app").All() || s.Allows("package::install") {
			t.Error("a fleet-wide deploy-only grant is wrong")
		}
	})
}
