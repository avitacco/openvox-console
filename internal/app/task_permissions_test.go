package app_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/runtime"
	"github.com/voxpupuli/enterprise-console/internal/testapp"
)

// A user whose only role allows the patch module is refused every other
// task on every surface that starts, saves or lists tasks, and the same
// on a second instance. The test console has no Puppet server, so a task
// that is allowed answers "not configured" (503) where one that is not
// is refused first (403, or 404 when asked for): the difference is what
// is being asserted, and it shows that each handler set was given the
// caller's task scope - one that was not would let every task through to
// that same 503.
func TestTaskPermissions_EveryTaskHandlerIsLimitedOnEveryInstance(t *testing.T) {
	cluster := testapp.NewCluster(t)
	// Two clustered instances; the first writes its audit log to a file,
	// to check the entries, which are log lines rather than activity rows.
	const secret = "test-cluster-secret"
	seedAddr := testapp.FreeAddr(t)
	auditFile := filepath.Join(t.TempDir(), "audit.log")
	instances := []*testapp.Instance{
		cluster.Start(testapp.Config{Mode: runtime.ModeAll, Env: map[string]string{
			"CONSOLE_CLUSTER_ADDR": seedAddr, "CONSOLE_CLUSTER_SECRET": secret, "CONSOLE_AUDIT_LOG_PATH": auditFile,
		}}),
		cluster.Start(testapp.Config{Mode: runtime.ModeAll, Env: map[string]string{
			"CONSOLE_CLUSTER_ADDR": testapp.FreeAddr(t), "CONSOLE_CLUSTER_PEERS": seedAddr, "CONSOLE_CLUSTER_SECRET": secret,
		}}),
	}
	a := sweepClient{t: t, inst: instances[0]}
	admin := instances[0].Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)

	var role struct {
		ID int64 `json:"id"`
	}
	a.send(http.MethodPost, "/api/v1/roles", admin, map[string]any{
		"name": "patcher", "permissions": []string{"orchestrator:read", "orchestrator:run"}, "taskPatterns": []string{"patch::*"},
	}, &role)
	var user struct {
		ID int64 `json:"id"`
	}
	const password = "a-long-enough-password"
	a.send(http.MethodPost, "/api/v1/users", admin, map[string]string{"username": "patcher", "password": password}, &user)
	if status, body := a.do(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/roles/%d", user.ID, role.ID), admin, nil); status >= 300 {
		t.Fatalf("assign role = %d %s", status, body)
	}

	// A role is read back with its list.
	if status, body := a.get(fmt.Sprintf("/api/v1/roles/%d", role.ID), admin); status != http.StatusOK || !strings.Contains(body, `"taskPatterns":["patch::*"]`) {
		t.Fatalf("role = %d %s, want its allowlist", status, body)
	}

	const node = "task-perm-node.example.test"
	soon := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	for n, inst := range instances {
		c := sweepClient{t: t, inst: inst}
		token := inst.Login(t, "patcher", password)
		where := fmt.Sprintf("instance %d", n+1)

		expect := func(what string, wantStatus int, status int, body string) {
			t.Helper()
			if status != wantStatus {
				t.Errorf("%s, %s: %d %s, want %d", where, what, status, body, wantStatus)
			}
		}

		status, body := c.do(http.MethodPost, "/api/v1/orchestrator/tasks", token, map[string]any{"targets": []string{node}, "task": "package::install"})
		expect("starting a task outside the list", http.StatusForbidden, status, body)
		if !strings.Contains(body, `"task":"package::install"`) {
			t.Errorf("%s: the refusal does not name the task: %s", where, body)
		}
		status, body = c.do(http.MethodPost, "/api/v1/orchestrator/tasks", token, map[string]any{"targets": []string{node}, "task": "patch::apply"})
		expect("starting a task on the list", http.StatusServiceUnavailable, status, body)

		status, body = c.do(http.MethodPost, "/api/v1/orchestrator/plans", token, map[string]any{
			"name": "p", "targets": []string{node},
			"steps": []map[string]any{{"kind": "run"}, {"kind": "task", "taskName": "package::install"}},
		})
		expect("a plan with a task outside the list", http.StatusForbidden, status, body)
		if !strings.Contains(body, `"step":2`) {
			t.Errorf("%s: the refusal does not name step 2: %s", where, body)
		}

		status, body = c.do(http.MethodPost, "/api/v1/orchestrator/saved-plans", token, map[string]any{
			"name": fmt.Sprintf("saved %d", n), "steps": []map[string]any{{"kind": "task", "taskName": "package::install"}},
		})
		expect("saving a plan with a task outside the list", http.StatusForbidden, status, body)

		status, body = c.do(http.MethodPost, "/api/v1/orchestrator/schedules", token, map[string]any{
			"name": fmt.Sprintf("sched %d", n), "kind": "task", "task": "package::install", "targets": []string{node}, "startAt": soon,
		})
		expect("scheduling a task outside the list", http.StatusForbidden, status, body)

		// A plan of runs is not limited.
		status, body = c.do(http.MethodPost, "/api/v1/orchestrator/plans", token, map[string]any{
			"name": "runs", "targets": []string{node}, "steps": []map[string]any{{"kind": "run"}},
		})
		expect("a plan of Puppet runs", http.StatusOK, status, body)

		// The catalogue answers a task outside the list as unknown.
		status, body = c.get("/api/v1/tasks/package/install", token)
		expect("the details of a task outside the list", http.StatusNotFound, status, body)
		status, body = c.get("/api/v1/tasks/patch/apply", token)
		expect("the details of a task on the list", http.StatusServiceUnavailable, status, body)

		// Nothing was started or saved by any of the refusals.
		status, body = c.get("/api/v1/orchestrator/saved-plans", token)
		if status != http.StatusOK || strings.Contains(body, "saved ") {
			t.Errorf("%s: a refused plan was saved: %d %s", where, status, body)
		}
	}

	// The administrator holds no limit, so is not refused by name.
	status, body := a.do(http.MethodPost, "/api/v1/orchestrator/tasks", admin, map[string]any{"targets": []string{node}, "task": "package::install"})
	if status != http.StatusServiceUnavailable {
		t.Errorf("an unrestricted caller = %d %s, want it to reach the catalogue", status, body)
	}

	// A refused start is audited by the instance that answered it, naming
	// the actor and the task, and for a plan the step.
	raw, err := os.ReadFile(auditFile)
	if err != nil {
		t.Fatal(err)
	}
	logged := string(raw)
	if !strings.Contains(logged, `"step":2`) {
		t.Errorf("the audit entry for the refused plan does not name step 2: %s", logged)
	}
	if !strings.Contains(logged, `"action":"task.denied"`) || !strings.Contains(logged, `"resourceId":"package::install"`) || !strings.Contains(logged, `"actor":"patcher"`) {
		t.Errorf("the refusals were not audited: %s", logged)
	}
}

// Narrowing a role takes effect for the next token: an open session
// keeps what it was issued, a new login does not.
func TestTaskPermissions_ANarrowedRoleReachesTheNextToken(t *testing.T) {
	cluster := testapp.NewCluster(t)
	inst := cluster.Start(testapp.Config{Mode: runtime.ModeAll})
	c := sweepClient{t: t, inst: inst}
	admin := inst.Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)

	var role, user struct {
		ID int64 `json:"id"`
	}
	c.send(http.MethodPost, "/api/v1/roles", admin, map[string]any{"name": "wide", "permissions": []string{"orchestrator:run"}}, &role)
	const password = "a-long-enough-password"
	c.send(http.MethodPost, "/api/v1/users", admin, map[string]string{"username": "narrowed", "password": password}, &user)
	if status, body := c.do(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/roles/%d", user.ID, role.ID), admin, nil); status >= 300 {
		t.Fatalf("assign role = %d %s", status, body)
	}
	const node = "narrowed-node.example.test"
	start := func(token string) int {
		status, _ := c.do(http.MethodPost, "/api/v1/orchestrator/tasks", token, map[string]any{"targets": []string{node}, "task": "package::install"})
		return status
	}

	before := inst.Login(t, "narrowed", password)
	if got := start(before); got != http.StatusServiceUnavailable {
		t.Fatalf("with no allowlist = %d, want it to reach the catalogue", got)
	}

	if status, body := c.do(http.MethodPut, fmt.Sprintf("/api/v1/roles/%d", role.ID), admin, map[string]any{
		"name": "wide", "permissions": []string{"orchestrator:run"}, "taskPatterns": []string{"patch::*"},
	}); status != http.StatusOK {
		t.Fatalf("narrow the role = %d %s", status, body)
	}
	if got := start(before); got != http.StatusServiceUnavailable {
		t.Errorf("the session opened before the change = %d, want it to keep what it was issued", got)
	}
	if got := start(inst.Login(t, "narrowed", password)); got != http.StatusForbidden {
		t.Errorf("a new login after the change = %d, want 403", got)
	}
}

// The pairing end to end, through the real group membership: a user with
// one role allowing patching on the web tier and another allowing every
// task on the database tier may not run anything else on a web node. A
// plain union of the two lists would allow it.
func TestTaskPermissions_ARoleOnOneTierIsNotWidenedByARoleOnAnother(t *testing.T) {
	cluster := testapp.NewCluster(t)
	backends := fakeOpenvoxdb(t, cluster)
	inst := cluster.Start(testapp.Config{Env: map[string]string{"CONSOLE_OPENVOXDB_URL": backends.URL}})
	c := sweepClient{t: t, inst: inst}
	admin := inst.Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)

	type idOnly struct {
		ID int64 `json:"id"`
	}
	group := func(name, role string, priority int) int64 {
		var g idOnly
		c.send(http.MethodPost, "/api/v1/groups", admin, map[string]any{
			"name": name, "priority": priority,
			"rule": []map[string]string{{"factPath": "role", "operator": "=", "value": role}},
		}, &g)
		return g.ID
	}
	web := group("pair-web", "web", 10)
	db := group("pair-db", "db", 20)

	var patchWeb, anyDB, user idOnly
	c.send(http.MethodPost, "/api/v1/roles", admin, map[string]any{
		"name": "pair-patch-web", "permissions": []string{"orchestrator:run"}, "nodeGroups": []int64{web}, "taskPatterns": []string{"patch::*"},
	}, &patchWeb)
	c.send(http.MethodPost, "/api/v1/roles", admin, map[string]any{
		"name": "pair-any-db", "permissions": []string{"orchestrator:run"}, "nodeGroups": []int64{db},
	}, &anyDB)
	const password = "a-long-enough-password"
	c.send(http.MethodPost, "/api/v1/users", admin, map[string]string{"username": "pairing", "password": password}, &user)
	for _, role := range []int64{patchWeb.ID, anyDB.ID} {
		c.send(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/roles/%d", user.ID, role), admin, nil, nil)
	}
	token := inst.Login(t, "pairing", password)

	start := func(task, node string) (int, string) {
		return c.do(http.MethodPost, "/api/v1/orchestrator/tasks", token, map[string]any{"targets": []string{node}, "task": task})
	}

	// Allowed on both tiers, as far as the catalogue (which this console
	// does not have) is concerned: 503.
	for _, ok := range [][2]string{{"patch::apply", visibleNode}, {"patch::apply", hiddenNode}, {"package::install", hiddenNode}} {
		if status, body := start(ok[0], ok[1]); status != http.StatusServiceUnavailable {
			t.Errorf("%s on %s = %d %s, want it allowed", ok[0], ok[1], status, body)
		}
	}
	// The task the web role does not allow, on a web node: outside.
	status, body := start("package::install", visibleNode)
	if status != http.StatusForbidden || !strings.Contains(body, `"outOfScope":["`+visibleNode+`"]`) {
		t.Errorf("package::install on the web node = %d %s, want it refused as outside the task's nodes", status, body)
	}
}
