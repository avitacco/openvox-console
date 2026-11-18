package app_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/voxpupuli/enterprise-console/internal/testapp"
)

// The route sweep for node group scope (add-group-scoped-node-access).
//
// Scope is enforced handler by handler, so the failure it has to guard
// against is a handler nobody remembered: a new endpoint returning node
// data, or an existing one whose handler set was never given the scope.
// Per-package tests cannot catch that, because the forgotten handler is
// exactly the one with no test.
//
// So this runs a real console against a stand-in openvoxdb holding two
// nodes, logs in as a user whose roles grant every read permission on
// one node's group only - every read permission and every action
// permission - and calls every permission-gated route in the API.
// Nothing that identifies the other node may come back from a read, and
// every action on it must be refused with nothing done. Which routes
// exist is read from the source, so a route added later is either swept
// here or listed below as exempt, with the reason - it cannot be
// neither.

const (
	visibleNode = "visible-web.example.test"
	hiddenNode  = "hidden-db.example.test"
)

// hiddenMarkers are strings that exist only in the hidden node's data.
var hiddenMarkers = []string{
	hiddenNode,
	"hidden-only-package",
	"hidden-fact-value",
	"hidden-report-hash",
	"Hidden_resource_title",
	"CVE-SWEEP-hidden-only",
	"hidden-job-output",
	"hidden-only-patch",
	"hidden-agentless-target",
}

// exemptRoutes are the routes that are deliberately not scoped, and
// why. Everything else gated by a permission is swept: reads for what
// they return, actions for being refused.
// visibleAgentless is an agentless target the scoped user's group pins.
const visibleAgentless = "visible-agentless-target"

var exemptRoutes = map[string]string{
	"GET /api/v1/orchestrator/saved-plans":         "a saved plan names no nodes: every caller with orchestrator:read sees every plan (checked below)",
	"GET /api/v1/orchestrator/saved-plans/{id}":    "saved plan",
	"POST /api/v1/orchestrator/saved-plans":        "a saved plan names no nodes; running one is POST /orchestrator/plans, which is swept",
	"PUT /api/v1/orchestrator/saved-plans/{id}":    "saved plan",
	"DELETE /api/v1/orchestrator/saved-plans/{id}": "saved plan",
	"GET /api/v1/groups":                           "group definitions stay visible to classifier:read (proposal: not in this change)",
	"GET /api/v1/groups/{id}":                      "group definitions stay visible to classifier:read",
	"GET /api/v1/enc/{certname}":                   "machine endpoint for the Puppet server; unaffected by node scope",
	"GET /api/v1/enc/{certname}/data":              "machine endpoint for the Puppet server; unaffected by node scope",
	"GET /api/v1/audit-log":                        "the activity log is an audit surface with its own permission",
	"GET /api/v1/audit-log/categories":             "activity log",
	"GET /api/v1/audit-log/actions":                "activity log",
	"GET /api/v1/classes/environments":             "class catalogue: code, not node data",
	"GET /api/v1/classes":                          "class catalogue",
	"POST /api/v1/classes/refresh":                 "class catalogue: re-reads code, touches no node",
	"GET /api/v1/code-plans":                       "plan catalogue: code, not node data",
	"GET /api/v1/code-plans/{name}":                "plan catalogue",
	"GET /api/v1/tasks/environments":               "task catalogue: code, not node data",
	"GET /api/v1/tasks":                            "task catalogue",
	"GET /api/v1/tasks/{module}/{task}":            "task catalogue",
	"GET /api/v1/code-deploys":                     "deploy history: code, not node data",
	"GET /api/v1/code-deploys/sources":             "code sources",
	"GET /api/v1/code-deploys/refs":                "code refs",
	"GET /api/v1/status":                           "console instances and dependencies, not managed nodes",
	"GET /api/v1/users":                            "rbac administration",
	"GET /api/v1/users/{id}":                       "rbac administration",
	"GET /api/v1/users/{id}/roles":                 "rbac administration",
	"GET /api/v1/roles":                            "rbac administration",
	"GET /api/v1/user-groups":                      "rbac administration",
	"GET /api/v1/user-groups/{id}":                 "rbac administration",
	"GET /api/v1/users/{id}/groups":                "rbac administration",
	"GET /api/v1/roles/{id}":                       "rbac administration",
	"GET /api/v1/service-tokens":                   "rbac administration",
	"GET /api/v1/vulnerability-provider-types":     "provider configuration, not node data",
	"GET /api/v1/vulnerability-providers":          "provider configuration",
	"GET /api/v1/vulnerability-providers/{id}":     "provider configuration",
	"GET /api/v1/patches/settings":                 "the periodic scan interval: console configuration, not node data",

	// Writes that are not actions on a node.
	"POST /api/v1/groups":                              "group definitions: classifier:write is not scoped to a subtree",
	"PUT /api/v1/groups/{id}":                          "group definitions",
	"DELETE /api/v1/groups/{id}":                       "group definitions",
	"POST /api/v1/code-deploys":                        "a code deploy is not an action on a node",
	"POST /api/v1/users":                               "rbac administration",
	"POST /api/v1/users/{id}/unlock":                   "rbac administration: unlocks a console account",
	"POST /api/v1/patches/scan":                        "a group scan skips nodes outside the caller's scope and counts them, rather than refusing: patchinventory.TestScanGroup_ReportsEachNodeAndSkips",
	"PUT /api/v1/patches/settings":                     "the periodic scan interval: console configuration, not an action on a node",
	"PUT /api/v1/account-policy":                       "rbac administration: the account policy",
	"PUT /api/v1/users/{id}":                           "rbac administration",
	"DELETE /api/v1/users/{id}":                        "rbac administration",
	"POST /api/v1/users/{id}/roles/{roleId}":           "rbac administration",
	"DELETE /api/v1/users/{id}/roles/{roleId}":         "rbac administration",
	"POST /api/v1/roles":                               "rbac administration",
	"POST /api/v1/user-groups":                         "rbac administration",
	"PUT /api/v1/user-groups/{id}":                     "rbac administration",
	"DELETE /api/v1/user-groups/{id}":                  "rbac administration",
	"POST /api/v1/user-groups/{id}/members/{userId}":   "rbac administration",
	"DELETE /api/v1/user-groups/{id}/members/{userId}": "rbac administration",
	"POST /api/v1/user-groups/{id}/roles/{roleId}":     "rbac administration",
	"DELETE /api/v1/user-groups/{id}/roles/{roleId}":   "rbac administration",
	"PUT /api/v1/roles/{id}":                           "rbac administration",
	"DELETE /api/v1/roles/{id}":                        "rbac administration",
	"POST /api/v1/service-tokens":                      "rbac administration",
	"DELETE /api/v1/service-tokens/{id}":               "rbac administration",
	"POST /api/v1/vulnerability-providers":             "provider configuration",
	"PUT /api/v1/vulnerability-providers/{id}":         "provider configuration",
	"DELETE /api/v1/vulnerability-providers/{id}":      "provider configuration",
	"POST /api/v1/vulnerability-providers/{id}/sync":   "provider configuration",
}

var routePattern = regexp.MustCompile(`"((?:GET|POST|PUT|PATCH|DELETE) /api/v1/[^"]+)",\s*(?:h\.verifier\.Authorize|authorize)\("[^"]+"`)

// gatedRoutes reads every permission-gated route out of the handler
// sources, as "METHOD /path".
func gatedRoutes(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../*/*.go")
	if err != nil {
		t.Fatalf("glob handler sources: %v", err)
	}
	seen := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, m := range routePattern.FindAllSubmatch(src, -1) {
			seen[string(m[1])] = true
		}
	}
	routes := make([]string, 0, len(seen))
	for r := range seen {
		routes = append(routes, r)
	}
	sort.Strings(routes)
	if len(routes) < 45 {
		t.Fatalf("found only %d gated routes in the source; the pattern no longer matches how routes are registered", len(routes))
	}
	return routes
}

// fakeOpenvoxdb answers the PQL this console sends, over the two-node
// fleet. It honours the few filters the console uses - certname
// equality and membership, report hash, package name - and otherwise
// returns everything, so a handler that fails to filter gets the hidden
// node's rows.
//
// It also stands in for the CA, and records every request that would
// change something - an openvoxdb command or a certificate action - so
// the sweep can assert that a refused action changed nothing.
type fakeBackends struct {
	URL string

	mu        sync.Mutex
	mutations []string
}

func (f *fakeBackends) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mutations = append(f.mutations, r.Method+" "+r.URL.RequestURI())
}

// Mutations returns the state-changing requests received so far.
func (f *fakeBackends) Mutations() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.mutations...)
}

func fakeOpenvoxdb(t *testing.T, cluster *testapp.Cluster) *fakeBackends {
	t.Helper()
	backends := &fakeBackends{}
	now := time.Now().UTC().Format(time.RFC3339)

	type row = map[string]any
	node := func(certname, role, status, pkg, factValue, hash, resource string) map[string][]row {
		return map[string][]row{
			"nodes":     {{"certname": certname, "latest_report_status": status, "report_timestamp": now, "report_environment": "production"}},
			"inventory": {{"certname": certname, "facts": row{"role": role, "marker": factValue}}},
			"facts": {
				{"certname": certname, "name": "role", "value": role},
				{"certname": certname, "name": "marker", "value": factValue},
			},
			"reports":           {{"certname": certname, "hash": hash, "status": status, "start_time": now, "end_time": now, "receive_time": now}},
			"events":            {{"certname": certname, "report": hash, "resource_type": "File", "resource_title": resource, "status": "success", "timestamp": now}},
			"package_inventory": {{"certname": certname, "package_name": pkg, "version": "1.0", "provider": "apt", "count": 1}},
		}
	}
	fleet := []map[string][]row{
		node(visibleNode, "web", "unchanged", "visible-only-package", "visible-fact-value", "visible-report-hash", "Visible_resource_title"),
		node(hiddenNode, "db", "failed", "hidden-only-package", "hidden-fact-value", "hidden-report-hash", "Hidden_resource_title"),
	}

	entity := regexp.MustCompile(`^\s*([a-z_]+)`)
	certEq := regexp.MustCompile(`certname = "([^"]+)"`)
	certIn := regexp.MustCompile(`certname in \[([^\]]*)\]`)
	reportEq := regexp.MustCompile(`report = "([^"]+)"`)
	pkgEq := regexp.MustCompile(`package_name = "([^"]+)"`)
	quoted := regexp.MustCompile(`"([^"]+)"`)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/pdb/cmd/"):
			backends.record(r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"uuid":"00000000-0000-4000-8000-000000000000"}`))
			return
		case strings.HasPrefix(r.URL.Path, "/puppet-ca/"):
			if r.Method != http.MethodGet {
				backends.record(r)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(r.URL.Path, "/certificate_statuses/all") {
				_, _ = w.Write([]byte(`[{"name":"` + visibleNode + `","state":"signed"},{"name":"` + hiddenNode + `","state":"requested"}]`))
				return
			}
			name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			state := "requested"
			if name == visibleNode {
				state = "signed"
			}
			_, _ = w.Write([]byte(`{"name":"` + name + `","state":"` + state + `"}`))
			return
		case strings.HasPrefix(r.URL.Path, "/pdb/query/v4/nodes/"):
			// The single-node lookup node removal uses.
			name := strings.TrimPrefix(r.URL.Path, "/pdb/query/v4/nodes/")
			w.Header().Set("Content-Type", "application/json")
			if name != visibleNode && name != hiddenNode {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"No information is known about node ` + name + `"}`))
				return
			}
			_, _ = w.Write([]byte(`{"certname":"` + name + `"}`))
			return
		}

		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		pql := body.Query

		m := entity.FindStringSubmatch(pql)
		if m == nil {
			http.Error(w, "unparseable query: "+pql, http.StatusBadRequest)
			return
		}
		var allowed map[string]bool
		if eq := certEq.FindStringSubmatch(pql); eq != nil {
			allowed = map[string]bool{eq[1]: true}
		} else if in := certIn.FindStringSubmatch(pql); in != nil {
			allowed = map[string]bool{}
			for _, q := range quoted.FindAllStringSubmatch(in[1], -1) {
				allowed[q[1]] = true
			}
		}

		rows := []row{}
		for _, n := range fleet {
			for _, candidate := range n[m[1]] {
				if allowed != nil && !allowed[candidate["certname"].(string)] {
					continue
				}
				if eq := reportEq.FindStringSubmatch(pql); eq != nil && candidate["report"] != eq[1] {
					continue
				}
				if eq := pkgEq.FindStringSubmatch(pql); eq != nil && candidate["package_name"] != eq[1] {
					continue
				}
				rows = append(rows, candidate)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rows)
	})

	certPath, keyPath := cluster.CA().Issue(t, "openvoxdb-fake", true)
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("load fake openvoxdb certificate: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	t.Cleanup(server.Close)
	backends.URL = server.URL
	return backends
}

type sweepClient struct {
	t    *testing.T
	inst *testapp.Instance
}

func (c sweepClient) get(path, token string) (int, string) {
	c.t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.inst.BaseURL+path, nil)
	if err != nil {
		c.t.Fatalf("build request for %s: %v", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// do issues a request with an optional JSON body and returns the status
// and body, whatever the status.
func (c sweepClient) do(method, path, token string, in any) (int, string) {
	c.t.Helper()
	var payload io.Reader
	if in != nil {
		encoded, _ := json.Marshal(in)
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, c.inst.BaseURL+path, payload)
	if err != nil {
		c.t.Fatalf("build %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// send issues a JSON request as the admin and decodes the response into
// out (which may be nil), failing on any non-2xx status.
func (c sweepClient) send(method, path, token string, in, out any) {
	c.t.Helper()
	var payload io.Reader
	if in != nil {
		encoded, _ := json.Marshal(in)
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, c.inst.BaseURL+path, payload)
	if err != nil {
		c.t.Fatalf("build %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		c.t.Fatalf("%s %s = %d: %s", method, path, resp.StatusCode, body)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			c.t.Fatalf("decode %s %s response %q: %v", method, path, body, err)
		}
	}
}

// seedVulnerabilities gives each node one vulnerability of its own, from
// a provider of a type no scheduler knows how to sync.
func seedVulnerabilities(t *testing.T, dsn string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	const provider = "5c0be5ee-0000-4000-8000-000000000001"
	statements := []string{
		fmt.Sprintf(`INSERT INTO vulnerability_providers (id, type, name, enabled, sync_interval_seconds)
		             VALUES ('%s', 'sweep-fixture', 'sweep-fixture', true, 86400)`, provider),
	}
	for certname, vuln := range map[string]string{visibleNode: "CVE-SWEEP-visible-only", hiddenNode: "CVE-SWEEP-hidden-only"} {
		statements = append(statements,
			fmt.Sprintf(`INSERT INTO vulnerability_observations (provider_id, certname, vuln_id, package, record_id, fix_available, severity, observed_at)
			             VALUES ('%s', '%s', '%s', 'openssl', '%s', true, 'high', now())`, provider, certname, vuln, vuln),
			fmt.Sprintf(`INSERT INTO vulnerability_findings (certname, vuln_id, status, first_seen_at, last_seen_at)
			             VALUES ('%s', '%s', 'open', now(), now())`, certname, vuln),
		)
	}
	for _, stmt := range statements {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("seed vulnerabilities: %v\n%s", err, stmt)
		}
	}
}

// seedPatches gives each node one pending update of its own.
// seedAgentless adds two agentless targets, one the scoped user's group
// pins and one nobody does, and returns their ids in that order. The
// credential columns hold a placeholder: nothing here connects.
func seedAgentless(t *testing.T, dsn string) (visibleID, hiddenID int64) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	for name, id := range map[string]*int64{visibleAgentless: &visibleID, "hidden-agentless-target": &hiddenID} {
		if err := conn.QueryRow(ctx, `INSERT INTO agentless_targets (name, address, username, sealed_password, created_by, updated_by)
			VALUES ($1, '10.9.9.9', 'ops', '\x01', 'sweep', 'sweep') RETURNING id`, name).Scan(id); err != nil {
			t.Fatalf("seed agentless: %v", err)
		}
	}
	return visibleID, hiddenID
}

func seedPatches(t *testing.T, dsn string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	for certname, pkg := range map[string]string{visibleNode: "visible-only-patch", hiddenNode: "hidden-only-patch"} {
		for _, stmt := range []string{
			fmt.Sprintf(`INSERT INTO node_patch_scans (certname, manager, update_count, scanned_at, attempted_at)
			             VALUES ('%s', 'apt', 1, now(), now())`, certname),
			fmt.Sprintf(`INSERT INTO node_patch_updates (certname, name, available) VALUES ('%s', '%s', '2')`, certname, pkg),
		} {
			if _, err := conn.Exec(ctx, stmt); err != nil {
				t.Fatalf("seed patches: %v\n%s", err, stmt)
			}
		}
	}
}

// seedJobs records three finished runs - on the visible node, on the
// hidden node, and on both - and returns their ids in that order.
func seedJobs(t *testing.T, dsn string) (visibleJob, hiddenJob, bothJob int64) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	job := func(targets map[string]string) int64 {
		var id int64
		if err := conn.QueryRow(ctx, `INSERT INTO jobs (kind, status, triggered_by, started_at, finished_at)
			VALUES ('run', 'succeeded', 'sweep-seed', now(), now()) RETURNING id`).Scan(&id); err != nil {
			t.Fatalf("seed job: %v", err)
		}
		for certname, output := range targets {
			if _, err := conn.Exec(ctx, `INSERT INTO job_targets (job_id, certname, status, output)
				VALUES ($1, $2, 'succeeded', $3)`, id, certname, output); err != nil {
				t.Fatalf("seed job target: %v", err)
			}
		}
		return id
	}
	return job(map[string]string{visibleNode: "visible-job-output"}),
		job(map[string]string{hiddenNode: "hidden-job-output"}),
		job(map[string]string{visibleNode: "visible-job-output", hiddenNode: "hidden-job-output"})
}

// jobsNamingHiddenNode counts the jobs with the hidden node among their
// targets - what a refused action must leave unchanged.
func jobsNamingHiddenNode(t *testing.T, dsn string) int {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(DISTINCT job_id) FROM job_targets WHERE certname = $1`, hiddenNode).Scan(&n); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	return n
}

func TestNodeScope_NoRouteReachesANodeOutsideTheCallersGroups(t *testing.T) {
	cluster := testapp.NewCluster(t)
	backends := fakeOpenvoxdb(t, cluster)
	// A plan in a deployed code directory, for the plan route to be reached.
	codeDir := t.TempDir()
	planDir := filepath.Join(codeDir, "environments", "production", "modules", "sweep", "plans")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "plan.yaml"), []byte("parameters:\n  targets: {type: TargetSpec}\nsteps:\n  - task: service\n    targets: $targets\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	caCert, caKey := cluster.CA().Issue(t, "console-ca-client", true)
	inst := cluster.Start(testapp.Config{Env: map[string]string{
		"CONSOLE_OPENVOXDB_URL":       backends.URL,
		"CONSOLE_CA_CLIENT_URL":       backends.URL,
		"CONSOLE_CA_CLIENT_CERT_FILE": caCert,
		"CONSOLE_CA_CLIENT_KEY_FILE":  caKey,
		"CONSOLE_CA_CLIENT_CA_FILE":   cluster.CA().PEMFile(t),
		"CONSOLE_CODE_DIR_PATH":       codeDir,
	}})
	seedVulnerabilities(t, cluster.DSN())
	seedPatches(t, cluster.DSN())
	visibleAgentlessID, hiddenAgentlessID := seedAgentless(t, cluster.DSN())
	visibleJob, hiddenJob, bothJob := seedJobs(t, cluster.DSN())
	c := sweepClient{t: t, inst: inst}
	admin := inst.Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)

	// Two sibling groups by the "role" fact, and a user whose only role
	// carries every read permission on the web group.
	type idOnly struct {
		ID int64 `json:"id"`
	}
	group := func(name, role string, priority int, pins ...string) int64 {
		var g idOnly
		c.send(http.MethodPost, "/api/v1/groups", admin, map[string]any{
			"name": name, "priority": priority, "pins": pins,
			"rule": []map[string]string{{"factPath": "role", "operator": "=", "value": role}},
		}, &g)
		return g.ID
	}
	web := group("sweep-web", "web", 10, visibleAgentless)
	db := group("sweep-db", "db", 20)

	var role, user idOnly
	c.send(http.MethodPost, "/api/v1/roles", admin, map[string]any{
		"name": "sweep-web-readers",
		"permissions": []string{
			"nodes:read", "classifier:read", "vulnerabilities:read", "code:read",
			"orchestrator:read", "activity:read", "status:read",
			"orchestrator:run", "nodes:manage", "nodes:certs:manage", "agentless:manage",
		},
		"nodeGroups": []int64{web},
	}, &role)
	c.send(http.MethodPost, "/api/v1/users", admin, map[string]string{"username": "sweep-reader", "password": "sweep-reader-password"}, &user)
	c.send(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/roles/%d", user.ID, role.ID), admin, nil, nil)
	scoped := inst.Login(t, "sweep-reader", "sweep-reader-password")

	// A schedule the administrator owns, naming the hidden node. It is
	// an hour away, so it does not fire during the sweep.
	var adminSchedule idOnly
	c.send(http.MethodPost, "/api/v1/orchestrator/schedules", admin, map[string]any{
		"name": "hidden-nightly", "kind": "run", "targets": []string{hiddenNode},
		"startAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}, &adminSchedule)
	schedulePath := fmt.Sprintf("/api/v1/orchestrator/schedules/%d", adminSchedule.ID)

	// Concrete requests for each scoped route, aimed at the hidden node
	// wherever the route names one.
	requests := map[string][]string{
		"GET /api/v1/nodes":                           {"/api/v1/nodes", "/api/v1/nodes?name=hidden", "/api/v1/nodes?fact=role&value=db"},
		"GET /api/v1/nodes/summary":                   {"/api/v1/nodes/summary"},
		"GET /api/v1/nodes/{name}":                    {"/api/v1/nodes/" + hiddenNode},
		"GET /api/v1/nodes/{name}/reports":            {"/api/v1/nodes/" + hiddenNode + "/reports"},
		"GET /api/v1/reports/{id}/events":             {"/api/v1/reports/hidden-report-hash/events"},
		"GET /api/v1/node-connectivity":               {"/api/v1/node-connectivity"},
		"GET /api/v1/nodes/{name}/packages":           {"/api/v1/nodes/" + hiddenNode + "/packages"},
		"GET /api/v1/nodes/{name}/packages/reporting": {"/api/v1/nodes/" + hiddenNode + "/packages/reporting"},
		"GET /api/v1/packages":                        {"/api/v1/packages?name=hidden-only-package"},
		"GET /api/v1/packages/catalog": {
			"/api/v1/packages/catalog",
			"/api/v1/packages/catalog?name=hidden",
			fmt.Sprintf("/api/v1/packages/catalog?group=%d", db),
		},
		"GET /api/v1/agentless/targets": {"/api/v1/agentless/targets"},
		"GET /api/v1/agentless/targets/{id}": {
			fmt.Sprintf("/api/v1/agentless/targets/%d", hiddenAgentlessID),
			"/api/v1/agentless/targets/999999999",
		},
		"GET /api/v1/nodes/{name}/patches": {"/api/v1/nodes/" + hiddenNode + "/patches"},
		"GET /api/v1/patches": {
			"/api/v1/patches",
			"/api/v1/patches?package=hidden-only-patch",
			fmt.Sprintf("/api/v1/patches?group=%d", db),
		},
		"GET /api/v1/patches/packages":             {"/api/v1/patches/packages?name=hidden-only-patch", "/api/v1/patches/packages?name=a"},
		"GET /api/v1/vulnerabilities":              {"/api/v1/vulnerabilities?pageSize=200"},
		"GET /api/v1/vulnerabilities/{vulnId}":     {"/api/v1/vulnerabilities/CVE-SWEEP-hidden-only"},
		"GET /api/v1/nodes/{name}/vulnerabilities": {"/api/v1/nodes/" + hiddenNode + "/vulnerabilities"},
		"GET /api/v1/groups/{id}/nodes":            {fmt.Sprintf("/api/v1/groups/%d/nodes", db), fmt.Sprintf("/api/v1/groups/%d/nodes", web)},
		"GET /api/v1/groups/node-counts":           {"/api/v1/groups/node-counts"},
		"GET /api/v1/code-repositories":            {"/api/v1/code-repositories"},
		"GET /api/v1/orchestrator/jobs": {
			"/api/v1/orchestrator/jobs?pageSize=100",
			"/api/v1/orchestrator/jobs?pageSize=100&target=hidden",
		},
		"GET /api/v1/orchestrator/schedules":      {"/api/v1/orchestrator/schedules"},
		"GET /api/v1/orchestrator/schedules/{id}": {schedulePath},
		"GET /api/v1/orchestrator/jobs/{id}": {
			fmt.Sprintf("/api/v1/orchestrator/jobs/%d", hiddenJob),
			fmt.Sprintf("/api/v1/orchestrator/jobs/%d", bothJob),
		},
	}

	// Each action, aimed at the hidden node. Multi-target ones also name
	// the caller's own node, to show the request is refused whole.
	type action struct {
		method, path string
		body         any
	}
	mixed := []string{visibleNode, hiddenNode}
	actions := map[string][]action{
		"POST /api/v1/orchestrator/runs": {
			{http.MethodPost, "/api/v1/orchestrator/runs", map[string]any{"targets": []string{hiddenNode}}},
			{http.MethodPost, "/api/v1/orchestrator/runs", map[string]any{"targets": mixed}},
		},
		"POST /api/v1/orchestrator/tasks": {
			{http.MethodPost, "/api/v1/orchestrator/tasks", map[string]any{"targets": mixed, "task": "service"}},
		},
		"POST /api/v1/orchestrator/code-plans": {
			{http.MethodPost, "/api/v1/orchestrator/code-plans", map[string]any{"plan": "sweep::plan", "parameters": map[string]any{"targets": []string{hiddenNode}}}},
			{http.MethodPost, "/api/v1/orchestrator/code-plans", map[string]any{"plan": "sweep::plan", "parameters": map[string]any{"targets": mixed}}},
		},
		"POST /api/v1/agentless/targets": {
			{http.MethodPost, "/api/v1/agentless/targets", map[string]any{"name": hiddenNode, "address": "10.0.0.1", "username": "u", "password": "x"}},
		},
		"POST /api/v1/orchestrator/patches": {
			{http.MethodPost, "/api/v1/orchestrator/patches", map[string]any{"targets": []string{hiddenNode}}},
			{http.MethodPost, "/api/v1/orchestrator/patches", map[string]any{"targets": mixed}},
		},
		"POST /api/v1/orchestrator/targets/resolve": {
			{http.MethodPost, "/api/v1/orchestrator/targets/resolve", map[string]any{"targets": mixed}},
		},
		"POST /api/v1/orchestrator/schedules": {
			{http.MethodPost, "/api/v1/orchestrator/schedules", map[string]any{
				"name": "sweep", "kind": "run", "targets": mixed,
				"startAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			}},
		},
		"POST /api/v1/orchestrator/plans": {
			{http.MethodPost, "/api/v1/orchestrator/plans", map[string]any{"targets": mixed, "name": "sweep", "steps": []map[string]string{{"kind": "run"}}}},
		},
		"PUT /api/v1/nodes/{name}/packages/reporting": {
			{http.MethodPut, "/api/v1/nodes/" + hiddenNode + "/packages/reporting", map[string]bool{"enabled": true}},
		},
		"POST /api/v1/nodes/{name}/patches/scan":    {{http.MethodPost, "/api/v1/nodes/" + hiddenNode + "/patches/scan", nil}},
		"POST /api/v1/nodes/{name}/agent-upgrade":   {{http.MethodPost, "/api/v1/nodes/" + hiddenNode + "/agent-upgrade", nil}},
		"DELETE /api/v1/nodes/{name}":               {{http.MethodDelete, "/api/v1/nodes/" + hiddenNode, nil}},
		"POST /api/v1/nodes/{certname}/cert/sign":   {{http.MethodPost, "/api/v1/nodes/" + hiddenNode + "/cert/sign", nil}},
		"POST /api/v1/nodes/{certname}/cert/revoke": {{http.MethodPost, "/api/v1/nodes/" + hiddenNode + "/cert/revoke", nil}},
		"DELETE /api/v1/nodes/{certname}/cert":      {{http.MethodDelete, "/api/v1/nodes/" + hiddenNode + "/cert", nil}},
	}

	// Changes to something the caller may not see. A schedule belongs
	// to its owner: to a scoped caller who is not the owner it does not
	// exist, so each of these answers as it would for an id nothing
	// has, and changes nothing.
	ownTargets := map[string]any{
		"name": "taken over", "kind": "run", "targets": []string{visibleNode},
		"startAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	notFound := map[string]action{
		"PUT /api/v1/orchestrator/schedules/{id}":         {http.MethodPut, schedulePath, ownTargets},
		"DELETE /api/v1/orchestrator/schedules/{id}":      {http.MethodDelete, schedulePath, nil},
		"POST /api/v1/orchestrator/schedules/{id}/pause":  {http.MethodPost, schedulePath + "/pause", nil},
		"POST /api/v1/orchestrator/schedules/{id}/resume": {http.MethodPost, schedulePath + "/resume", nil},
	}

	// Agentless targets are addressed by id, so a target the caller may
	// not see answers as an id nothing has: the same 404 whatever the
	// action, and nothing changes. Shared credentials belong to no node,
	// so a caller limited to some nodes is refused them outright.
	agentlessByID := map[string]action{
		"PUT /api/v1/agentless/targets/{id}":                   {http.MethodPut, "/api/v1/agentless/targets/%d", map[string]any{"address": "10.1.1.1"}},
		"DELETE /api/v1/agentless/targets/{id}":                {http.MethodDelete, "/api/v1/agentless/targets/%d", nil},
		"POST /api/v1/agentless/targets/{id}/check":            {http.MethodPost, "/api/v1/agentless/targets/%d/check", nil},
		"POST /api/v1/agentless/targets/{id}/host-key/accept":  {http.MethodPost, "/api/v1/agentless/targets/%d/host-key/accept", map[string]any{"fingerprint": "SHA256:x"}},
		"POST /api/v1/agentless/targets/{id}/host-key/replace": {http.MethodPost, "/api/v1/agentless/targets/%d/host-key/replace", map[string]any{"fingerprint": "SHA256:x"}},
	}
	agentlessCredentials := map[string]action{
		"GET /api/v1/agentless/credentials":         {http.MethodGet, "/api/v1/agentless/credentials", nil},
		"GET /api/v1/agentless/credentials/{id}":    {http.MethodGet, "/api/v1/agentless/credentials/1", nil},
		"POST /api/v1/agentless/credentials":        {http.MethodPost, "/api/v1/agentless/credentials", map[string]any{"name": "c", "username": "u", "password": "x"}},
		"PUT /api/v1/agentless/credentials/{id}":    {http.MethodPut, "/api/v1/agentless/credentials/1", map[string]any{"password": "x"}},
		"DELETE /api/v1/agentless/credentials/{id}": {http.MethodDelete, "/api/v1/agentless/credentials/1", nil},
	}

	// Every gated route is swept as a read, swept as an action, or
	// exempt - exactly one of the three.
	for _, route := range gatedRoutes(t) {
		_, read := requests[route]
		_, acted := actions[route]
		_, hidden := notFound[route]
		_, byID := agentlessByID[route]
		_, cred := agentlessCredentials[route]
		swept := read || acted || hidden || byID || cred
		_, exempt := exemptRoutes[route]
		switch {
		case swept && exempt:
			t.Errorf("route %s is listed as both swept and exempt", route)
		case !swept && !exempt:
			t.Errorf("route %s is neither swept for node scope nor listed as exempt: if it returns node data, scope it and add it to requests; if it acts on a node, scope it and add it to actions; if neither, add it to exemptRoutes with the reason", route)
		}
	}

	// The fixture must really contain what the sweep looks for: the
	// fleet-wide administrator sees the hidden node on every surface.
	for path, marker := range map[string]string{
		"/api/v1/nodes":                                        hiddenNode,
		"/api/v1/nodes/" + hiddenNode:                          "hidden-fact-value",
		"/api/v1/nodes/" + hiddenNode + "/reports":             "hidden-report-hash",
		"/api/v1/reports/hidden-report-hash/events":            "Hidden_resource_title",
		"/api/v1/packages?name=hidden-only-package":            hiddenNode,
		"/api/v1/packages/catalog":                             "hidden-only-package",
		"/api/v1/vulnerabilities?pageSize=200":                 "CVE-SWEEP-hidden-only",
		"/api/v1/vulnerabilities/CVE-SWEEP-hidden-only":        hiddenNode,
		fmt.Sprintf("/api/v1/groups/%d/nodes", db):             hiddenNode,
		fmt.Sprintf("/api/v1/orchestrator/jobs/%d", hiddenJob): "hidden-job-output",
		"/api/v1/node-connectivity":                            hiddenNode,
		"/api/v1/patches?package=hidden-only-patch":            hiddenNode,
		"/api/v1/patches/packages?name=hidden-only-patch":      "hidden-only-patch",
		"/api/v1/nodes/" + hiddenNode + "/patches":             "hidden-only-patch",
		"/api/v1/agentless/targets":                            "hidden-agentless-target",
	} {
		if status, body := c.get(path, admin); status != http.StatusOK || !strings.Contains(body, marker) {
			t.Fatalf("fixture check: admin GET %s = %d, missing %q:\n%s", path, status, marker, body)
		}
	}

	// The sweep.
	for route, paths := range requests {
		for _, path := range paths {
			status, body := c.get(path, scoped)
			if status >= 500 {
				t.Errorf("%s: GET %s = %d: %s", route, path, status, body)
				continue
			}
			for _, marker := range hiddenMarkers {
				// A request echoes the certname it was asked about, as
				// it would for a node that does not exist; that alone
				// tells the caller nothing.
				leaked := strings.ReplaceAll(body, `"certname":"`+hiddenNode+`"`, "")
				leaked = strings.ReplaceAll(leaked, `"vulnId":"CVE-SWEEP-hidden-only"`, "")
				leaked = strings.ReplaceAll(leaked, "not connected to the node transport): "+hiddenNode, "")
				if strings.Contains(leaked, marker) {
					t.Errorf("%s: GET %s as the scoped user returned %q, which belongs to a node outside their groups:\n%s", route, path, marker, body)
				}
			}
		}
	}

	// A hidden job answers as a job that does not exist; their own job
	// is there in full.
	hiddenStatus, hiddenBody := c.get(fmt.Sprintf("/api/v1/orchestrator/jobs/%d", hiddenJob), scoped)
	missingStatus, missingBody := c.get("/api/v1/orchestrator/jobs/999999999", scoped)
	if hiddenStatus != http.StatusNotFound || hiddenStatus != missingStatus || hiddenBody != missingBody {
		t.Errorf("hidden job: %d %q; missing job: %d %q; want identical 404s", hiddenStatus, hiddenBody, missingStatus, missingBody)
	}
	if status, body := c.get(fmt.Sprintf("/api/v1/orchestrator/jobs/%d", visibleJob), scoped); status != http.StatusOK || !strings.Contains(body, "visible-job-output") {
		t.Errorf("the scoped user's own job: %d %s", status, body)
	}
	if _, body := c.get(fmt.Sprintf("/api/v1/orchestrator/jobs/%d", bothJob), scoped); !strings.Contains(body, `"targetsHidden":true`) || !strings.Contains(body, "visible-job-output") {
		t.Errorf("a job spanning both nodes should show the caller's node and say some are hidden: %s", body)
	}

	// The action sweep: every action on the hidden node is refused, and
	// nothing happens - no job, no openvoxdb command, no CA call.
	jobsBefore := jobsNamingHiddenNode(t, cluster.DSN())
	for route, attempts := range actions {
		for _, a := range attempts {
			status, body := c.do(a.method, a.path, scoped, a.body)
			if status != http.StatusForbidden || !strings.Contains(body, `"outOfScope":["`+hiddenNode+`"]`) {
				t.Errorf("%s: %s %s as the scoped user = %d %s; want 403 naming only the hidden node", route, a.method, a.path, status, body)
			}
		}
	}
	if after := jobsNamingHiddenNode(t, cluster.DSN()); after != jobsBefore {
		t.Errorf("refused actions created %d job(s) naming the hidden node", after-jobsBefore)
	}
	if mutations := backends.Mutations(); len(mutations) != 0 {
		t.Errorf("refused actions still reached openvoxdb or the CA: %v", mutations)
	}

	for route, a := range agentlessByID {
		status, body := c.do(a.method, fmt.Sprintf(a.path, hiddenAgentlessID), scoped, a.body)
		missingStatus, missingBody := c.do(a.method, fmt.Sprintf(a.path, int64(999999999)), scoped, a.body)
		if status != http.StatusNotFound || status != missingStatus || body != missingBody {
			t.Errorf("%s on a hidden target = %d %s; a missing one = %d %s; want identical 404s", route, status, body, missingStatus, missingBody)
		}
	}
	// Their own target is theirs to read and manage.
	if status, body := c.get(fmt.Sprintf("/api/v1/agentless/targets/%d", visibleAgentlessID), scoped); status != http.StatusOK || !strings.Contains(body, visibleAgentless) {
		t.Errorf("the scoped user's own agentless target: %d %s", status, body)
	}
	for route, a := range agentlessCredentials {
		if status, body := c.do(a.method, a.path, scoped, a.body); status != http.StatusForbidden {
			t.Errorf("%s as a caller limited to some nodes = %d %s; want 403", route, status, body)
		}
	}
	if status, body := c.get(fmt.Sprintf("/api/v1/agentless/targets/%d", hiddenAgentlessID), admin); status != http.StatusOK || !strings.Contains(body, "10.9.9.9") {
		t.Errorf("the hidden target after the scoped user's attempts = %d %s; want it unchanged", status, body)
	}

	// Another caller's schedule: every change answers exactly as for a
	// schedule that does not exist, and it is left as it was.
	for route, a := range notFound {
		status, body := c.do(a.method, a.path, scoped, a.body)
		missingStatus, missingBody := c.do(a.method, strings.Replace(a.path, schedulePath, "/api/v1/orchestrator/schedules/999999999", 1), scoped, a.body)
		if status != http.StatusNotFound || status != missingStatus || body != missingBody {
			t.Errorf("%s as the scoped user = %d %s; a missing schedule = %d %s; want identical 404s", route, status, body, missingStatus, missingBody)
		}
	}
	if status, body := c.get(schedulePath, admin); status != http.StatusOK || !strings.Contains(body, `"name":"hidden-nightly"`) ||
		!strings.Contains(body, `"state":"active"`) || !strings.Contains(body, hiddenNode) {
		t.Errorf("the administrator's schedule after the scoped user's attempts = %d %s; want it unchanged", status, body)
	}
	if status, body := c.get("/api/v1/orchestrator/schedules", admin); status != http.StatusOK || !strings.Contains(body, "hidden-nightly") {
		t.Errorf("fixture check: the administrator does not list their schedule: %d %s", status, body)
	}

	// Targets chosen by a query or a node group are not refused: they
	// are narrowed to the caller's nodes, and nothing says what else
	// matched. A subquery, which could read across nodes, is refused.
	everyNode := map[string]any{"query": "nodes[certname] { }"}
	for name, body := range map[string]map[string]any{
		"query":              everyNode,
		"the caller's group": {"group": web},
	} {
		status, out := c.do(http.MethodPost, "/api/v1/orchestrator/targets/resolve", scoped, body)
		if status != http.StatusOK || !strings.Contains(out, visibleNode) || strings.Contains(out, hiddenNode) {
			t.Errorf("preview by %s as the scoped user = %d %s; want only the caller's node", name, status, out)
		}
	}
	otherTeams, otherBody := c.do(http.MethodPost, "/api/v1/orchestrator/runs", scoped, map[string]any{"group": db})
	if otherTeams != http.StatusBadRequest || strings.Contains(otherBody, hiddenNode) || !strings.Contains(otherBody, "no nodes matched") {
		t.Errorf("run on another team's group = %d %s; want the 400 of a group with no nodes", otherTeams, otherBody)
	}
	if status, out := c.do(http.MethodPost, "/api/v1/orchestrator/runs", scoped, everyNode); status != http.StatusOK || strings.Contains(out, hiddenNode) {
		t.Errorf("run by query as the scoped user = %d %s; want a job on the caller's node only", status, out)
	}
	if status, out := c.do(http.MethodPost, "/api/v1/orchestrator/targets/resolve", scoped,
		map[string]any{"query": `nodes[certname] { certname in facts[certname] { name = "role" and value = "db" } }`}); status != http.StatusBadRequest || !strings.Contains(out, "subqueries") {
		t.Errorf("subquery as the scoped user = %d %s; want 400", status, out)
	}
	if status, out := c.do(http.MethodPost, "/api/v1/orchestrator/targets/resolve", admin, everyNode); status != http.StatusOK || !strings.Contains(out, hiddenNode) {
		t.Errorf("preview by query as the administrator = %d %s; want every node", status, out)
	}
	if after := jobsNamingHiddenNode(t, cluster.DSN()); after != jobsBefore {
		t.Errorf("query and group targets created %d job(s) naming the hidden node", after-jobsBefore)
	}

	// A plan's target is scoped as a run's is: a query is narrowed, and
	// every step runs on the caller's node alone.
	planSteps := []map[string]string{{"kind": "run"}, {"kind": "run"}}
	var narrowedPlan idOnly
	if status, out := c.do(http.MethodPost, "/api/v1/orchestrator/plans", scoped, map[string]any{"name": "sweep-plan", "query": "nodes[certname] { }", "steps": planSteps}); status != http.StatusOK || strings.Contains(out, hiddenNode) {
		t.Errorf("plan by query as the scoped user = %d %s; want a plan on the caller's node only", status, out)
	} else if err := json.Unmarshal([]byte(out), &narrowedPlan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if status, out := c.get(fmt.Sprintf("/api/v1/orchestrator/jobs/%d", narrowedPlan.ID), admin); status != http.StatusOK || strings.Contains(out, hiddenNode) || strings.Count(out, `"certname":"`+visibleNode+`"`) != 2 {
		t.Errorf("the narrowed plan as the administrator reads it = %d %s; want two steps, each on the caller's node only", status, out)
	}
	if after := jobsNamingHiddenNode(t, cluster.DSN()); after != jobsBefore {
		t.Errorf("a plan by query created %d job(s) naming the hidden node", after-jobsBefore)
	}

	// Saved plans are not limited by node groups: the scoped caller
	// sees and may change the plan the administrator saved.
	var savedPlan idOnly
	c.send(http.MethodPost, "/api/v1/orchestrator/saved-plans", admin, map[string]any{"name": "sweep-saved-plan", "steps": planSteps}, &savedPlan)
	if status, out := c.get("/api/v1/orchestrator/saved-plans", scoped); status != http.StatusOK || !strings.Contains(out, "sweep-saved-plan") {
		t.Errorf("saved plans as the scoped user = %d %s; want the administrator's plan listed", status, out)
	}
	if status, out := c.do(http.MethodPut, fmt.Sprintf("/api/v1/orchestrator/saved-plans/%d", savedPlan.ID), scoped, map[string]any{"name": "sweep-saved-plan", "steps": planSteps[:1]}); status != http.StatusOK {
		t.Errorf("changing a saved plan as the scoped user = %d %s, want 200", status, out)
	}

	// The same actions on the caller's own node are not refused for
	// scope - the sweep is not passing because everything is forbidden.
	if status, body := c.do(http.MethodPost, "/api/v1/orchestrator/runs", scoped, map[string]any{"targets": []string{visibleNode}}); status != http.StatusOK {
		t.Errorf("run on the caller's own node = %d %s, want 200", status, body)
	}
	if status, body := c.do(http.MethodPost, "/api/v1/nodes/"+visibleNode+"/cert/revoke", scoped, nil); status != http.StatusNoContent {
		t.Errorf("revoke on the caller's own node = %d %s, want 204", status, body)
	}
	if mutations := backends.Mutations(); len(mutations) != 1 || !strings.Contains(mutations[0], visibleNode) {
		t.Errorf("CA calls after acting on the caller's own node = %v, want one, for that node", mutations)
	}

	// And the fleet-wide administrator can still act on the hidden node.
	if status, body := c.do(http.MethodPost, "/api/v1/nodes/"+hiddenNode+"/cert/sign", admin, nil); status != http.StatusNoContent {
		t.Errorf("admin sign of the hidden node = %d %s, want 204", status, body)
	}
	if status, body := c.do(http.MethodDelete, "/api/v1/nodes/"+hiddenNode, admin, nil); status != http.StatusNoContent {
		t.Errorf("admin removal of the hidden node = %d %s, want 204", status, body)
	}

	// And the scoped user does see their own node, and counts that add
	// up to it alone.
	if _, body := c.get("/api/v1/nodes", scoped); !strings.Contains(body, visibleNode) {
		t.Errorf("scoped node list is missing the in-scope node: %s", body)
	}
	var summary struct {
		Total int `json:"total"`
	}
	_, body := c.get("/api/v1/nodes/summary", scoped)
	_ = json.Unmarshal([]byte(body), &summary)
	if summary.Total != 1 {
		t.Errorf("scoped summary total = %d, want 1: %s", summary.Total, body)
	}
	var counts struct {
		Counts []struct {
			GroupID int64 `json:"groupId"`
			Nodes   int   `json:"nodes"`
		} `json:"counts"`
	}
	_, body = c.get("/api/v1/groups/node-counts", scoped)
	_ = json.Unmarshal([]byte(body), &counts)
	total := 0
	for _, count := range counts.Counts {
		if count.GroupID == db && count.Nodes != 0 {
			t.Errorf("scoped count for the db group = %d, want 0", count.Nodes)
		}
		if count.Nodes > total {
			total = count.Nodes
		}
	}
	if total != 1 {
		t.Errorf("largest scoped group count = %d, want 1 (the root holds only the caller's node): %s", total, body)
	}

	// The scoped user's own profile says what they are looking at.
	_, body = c.get("/api/v1/me", scoped)
	if !strings.Contains(body, `"all":false`) || !strings.Contains(body, "sweep-web") {
		t.Errorf("/api/v1/me nodeScope = %s, want not fleet-wide and naming sweep-web", body)
	}
	if _, body := c.get("/api/v1/me", admin); !strings.Contains(body, `"all":true`) {
		t.Errorf("admin /api/v1/me nodeScope = %s, want fleet-wide", body)
	}
}
