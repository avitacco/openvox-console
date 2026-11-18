package taskcatalog

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/rbac"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

type fixedGroups map[int64][]string

func (g fixedGroups) For(_ context.Context, ids []int64) (nodescope.Scope, error) {
	var nodes []string
	for _, id := range ids {
		nodes = append(nodes, g[id]...)
	}
	return nodescope.Of(nodes...), nil
}

// scopedServer serves the catalogue the way internal/app wires it: the
// task scope comes from the caller's token. token issues one holding
// orchestrator:read, and orchestrator:run when run is set, with the
// given task grants.
func scopedServer(t *testing.T) (*httptest.Server, func(run bool, grants []taskscope.Grant) string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issuer := rbac.NewIssuer(key, "test")
	verifier := rbac.NewVerifier(map[string]*ecdsa.PublicKey{"test": &key.PublicKey}, "test", noRevocations{})
	groups := fixedGroups{1: {"web1"}, 2: {}}

	h := NewHandlers(newTLSClient(t, fakePuppetServer(t)))
	h.SetTaskScope(func(r *http.Request) (taskscope.Scope, error) {
		claims, ok := rbac.ClaimsFromContext(r.Context())
		if !ok {
			return taskscope.Scope{}, nil
		}
		if !claims.HasPermission("orchestrator:run") {
			return taskscope.Unrestricted(), nil
		}
		return taskscope.Resolve(r.Context(), claims.TaskGrants, groups)
	})
	mux := http.NewServeMux()
	h.Register(mux, verifier.Authorize)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, func(run bool, grants []taskscope.Grant) string {
		perms := []string{"orchestrator:read"}
		if run {
			perms = append(perms, "orchestrator:run")
		}
		tok, _, err := issuer.IssueUserTokenWithTasks("tester", "access", perms, map[string][]int64{}, grants, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
}

func names(tasks []TaskSummary) map[string]bool {
	out := map[string]bool{}
	for _, t := range tasks {
		out[t.Name] = true
	}
	return out
}

func TestAPIListTasks_OffersOnlyWhatTheCallerMayRun(t *testing.T) {
	srv, token := scopedServer(t)
	type listing struct {
		Tasks []TaskSummary
	}

	// "The picker": a caller allowed one module sees only its tasks.
	var limited listing
	grants := []taskscope.Grant{{Tasks: []string{"task_fixture::*"}, Groups: []int64{1}}}
	if code := get(t, srv, "/api/v1/tasks", token(true, grants), &limited); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(limited.Tasks) == 0 {
		t.Fatal("a caller allowed the module saw no tasks")
	}
	for name := range names(limited.Tasks) {
		if !taskscope.Matches("task_fixture::*", name) {
			t.Errorf("a task outside the allowlist was offered: %s", name)
		}
	}

	// A caller allowed a different module sees none of this one's.
	var other listing
	get(t, srv, "/api/v1/tasks", token(true, []taskscope.Grant{{Tasks: []string{"patch::*"}, Groups: []int64{1}}}), &other)
	if len(other.Tasks) != 0 {
		t.Errorf("a caller allowed only patch::* was offered %v", names(other.Tasks))
	}

	// A grant that covers no node offers nothing, though it names tasks.
	var nowhere listing
	get(t, srv, "/api/v1/tasks", token(true, []taskscope.Grant{{Tasks: []string{"*"}, Groups: []int64{2}}}), &nowhere)
	if len(nowhere.Tasks) != 0 {
		t.Errorf("a grant covering no node offered %v", names(nowhere.Tasks))
	}

	// "A read-only caller": every task is listed, even with grants.
	var all listing
	get(t, srv, "/api/v1/tasks", token(false, grants), &all)
	if len(all.Tasks) != 8 {
		t.Errorf("a read-only caller sees %d tasks, want all 8", len(all.Tasks))
	}

	// No limit: every task.
	var unlimited listing
	get(t, srv, "/api/v1/tasks", token(true, nil), &unlimited)
	if len(unlimited.Tasks) != 8 {
		t.Errorf("an unrestricted caller sees %d tasks, want 8", len(unlimited.Tasks))
	}
}

func TestAPIGetTask_AHiddenTaskIsAnsweredAsUnknown(t *testing.T) {
	srv, token := scopedServer(t)
	patchOnly := token(true, []taskscope.Grant{{Tasks: []string{"patch::*"}, Groups: []int64{1}}})

	var hidden, missing map[string]string
	hiddenCode := get(t, srv, "/api/v1/tasks/task_fixture/echo", patchOnly, &hidden)
	missingCode := get(t, srv, "/api/v1/tasks/task_fixture/nope", patchOnly, &missing)
	if hiddenCode != http.StatusNotFound || missingCode != http.StatusNotFound {
		t.Fatalf("hidden = %d, missing = %d, want 404 for both", hiddenCode, missingCode)
	}
	if hidden["error"] != `task "task_fixture::echo" not found` || missing["error"] != `task "task_fixture::nope" not found` {
		t.Errorf("the two answers differ in shape: %v vs %v", hidden, missing)
	}

	// The same task is available to a caller allowed it.
	allowed := token(true, []taskscope.Grant{{Tasks: []string{"task_fixture::echo"}, Groups: []int64{1}}})
	if code := get(t, srv, "/api/v1/tasks/task_fixture/echo", allowed, nil); code != http.StatusOK {
		t.Errorf("an allowed task = %d, want 200", code)
	}
}
