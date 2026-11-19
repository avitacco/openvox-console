package agentapi

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/voxpupuli/enterprise-console/internal/rbac"
)

// route describes one endpoint of the REST API in the OpenAPI document.
type route struct {
	method, path, permission, summary string
	query                             []string
	// body names a schema in bodies, for a request that takes one.
	body string
}

// documented is every endpoint the document describes: the ones a tool or
// script built on the console would use. The rest of the API is
// administration the console's own pages use, and is left out on purpose,
// by prefix, in undocumented - a test fails when a route is in neither.
var documented = []route{
	{"GET", "/api/v1/nodes", "nodes:read", "List nodes", []string{"name", "fact", "value", "page", "pageSize"}, ""},
	{"GET", "/api/v1/nodes/summary", "nodes:read", "Count nodes by status", nil, ""},
	{"GET", "/api/v1/nodes/{name}", "nodes:read", "Show a node, with its facts", nil, ""},
	{"GET", "/api/v1/nodes/{name}/reports", "nodes:read", "List a node's Puppet reports", []string{"status", "page", "pageSize"}, ""},
	{"GET", "/api/v1/reports/{id}/events", "nodes:read", "List a report's resource events", nil, ""},
	{"GET", "/api/v1/nodes/{name}/packages", "nodes:read", "List a node's installed packages", nil, ""},
	{"GET", "/api/v1/packages", "nodes:read", "Find nodes by installed package", []string{"name"}, ""},
	{"GET", "/api/v1/packages/catalog", "nodes:read", "Search the package catalogue", []string{"name", "group"}, ""},
	{"GET", "/api/v1/nodes/{name}/patches", "nodes:read", "Show a node's pending updates", nil, ""},
	{"GET", "/api/v1/patches", "nodes:read", "Summarize pending updates", []string{"os", "package", "group"}, ""},
	{"GET", "/api/v1/patches/packages", "nodes:read", "Search pending updates by package", []string{"name"}, ""},
	{"GET", "/api/v1/nodes/{name}/vulnerabilities", "vulnerabilities:read", "List a node's vulnerabilities", nil, ""},
	{"GET", "/api/v1/vulnerabilities", "vulnerabilities:read", "List vulnerabilities", []string{"page", "pageSize"}, ""},
	{"GET", "/api/v1/vulnerabilities/{vulnId}", "vulnerabilities:read", "Show a vulnerability and the nodes it affects", nil, ""},
	{"GET", "/api/v1/groups", "classifier:read", "List node groups", []string{"all"}, ""},
	{"GET", "/api/v1/groups/{id}", "classifier:read", "Show a node group", nil, ""},
	{"GET", "/api/v1/groups/{id}/nodes", "classifier:read", "List the nodes in a node group", nil, ""},
	{"GET", "/api/v1/tasks", "orchestrator:read", "List tasks from deployed code", []string{"environment"}, ""},
	{"GET", "/api/v1/tasks/{module}/{task}", "orchestrator:read", "Show a task and its parameters", []string{"environment"}, ""},
	{"GET", "/api/v1/code-plans", "orchestrator:read", "List plans from deployed code", []string{"environment"}, ""},
	{"GET", "/api/v1/code-plans/{name}", "orchestrator:read", "Show a plan from code", []string{"environment"}, ""},
	{"GET", "/api/v1/orchestrator/jobs", "orchestrator:read", "List jobs", []string{"kind", "status", "target", "triggeredBy", "agent", "sort", "page", "pageSize"}, ""},
	{"GET", "/api/v1/orchestrator/jobs/{id}", "orchestrator:read", "Show a job with every node's result", nil, ""},
	{"POST", "/api/v1/orchestrator/targets/resolve", "orchestrator:run", "Preview the nodes a target names", nil, "target"},
	{"POST", "/api/v1/orchestrator/runs", "orchestrator:run", "Run Puppet on nodes", nil, "run"},
	{"POST", "/api/v1/orchestrator/tasks", "orchestrator:run", "Run a task on nodes", nil, "task"},
	{"POST", "/api/v1/orchestrator/plans", "orchestrator:run", "Run an ordered list of steps on nodes", nil, "plan"},
	{"POST", "/api/v1/orchestrator/patches", "orchestrator:run", "Apply operating system updates to nodes", nil, "patch"},
	{"POST", "/api/v1/orchestrator/code-plans", "orchestrator:run", "Run a plan from deployed code", nil, "codeplan"},
	{"GET", "/api/v1/agent-requests", "agent-requests:read", "List requests made by agents", []string{"status", "limit"}, ""},
	{"GET", "/api/v1/agent-requests/pending-count", "agent-requests:read", "Count requests awaiting a decision", nil, ""},
	{"GET", "/api/v1/agent-requests/{id}", "agent-requests:read", "Show an agent's request", nil, ""},
	{"POST", "/api/v1/agent-requests/{id}/approve", "agent-requests:approve", "Approve an agent's request, starting its job", nil, "decision"},
	{"POST", "/api/v1/agent-requests/{id}/reject", "agent-requests:approve", "Reject an agent's request", nil, "decision"},
}

// undocumented are the route prefixes the document leaves out, and why. A
// route under none of these and not in documented fails the drift test.
var undocumented = map[string]string{
	"/api/v1/users":                           "administration of accounts",
	"/api/v1/user-groups":                     "administration of accounts",
	"/api/v1/roles":                           "administration of access",
	"/api/v1/service-tokens":                  "administration of credentials",
	"/api/v1/agent-tokens":                    "administration of credentials",
	"/api/v1/account-policy":                  "administration of access",
	"/api/v1/audit-log":                       "the audit log is read in the console",
	"/api/v1/status":                          "the console's own health page",
	"/api/v1/enc":                             "read by the node classifier (enc-bridge), not by integrations",
	"/api/v1/classes":                         "the group editor's class picker",
	"/api/v1/code-deploys":                    "code deployment is driven by webhooks and the console",
	"/api/v1/code-repositories":               "the code page",
	"/api/v1/agentless":                       "administration of agentless targets and their credentials",
	"/api/v1/node-connectivity":               "the nodes page",
	"/api/v1/vulnerability-":                  "administration of vulnerability providers",
	"/api/v1/patches/scan":                    "started from the console",
	"/api/v1/patches/settings":                "settings",
	"/api/v1/packages/reporting":              "settings",
	"/api/v1/orchestrator/saved-":             "plan templates the console's pages manage",
	"/api/v1/orchestrator/schedul":            "schedules the console's pages manage",
	"/api/v1/tasks/environments":              "the task picker",
	"/api/v1/groups/node-counts":              "the groups page",
	"/api/v1/nodes/{name}/agent-upgrade":      "managing nodes",
	"/api/v1/nodes/{name}/packages/reporting": "settings",
	"/api/v1/nodes/{name}/patches/scan":       "started from the console",
	"/api/v1/nodes/{certname}":                "certificate management",
}

// changesLeftOut are prefixes under which only reading is documented: a
// route there that changes something is configuration the console's own
// pages make.
var changesLeftOut = []string{"/api/v1/groups", "/api/v1/nodes", "/api/v1/classes", "/api/v1/vulnerabilities"}

// Undocumented reports why a route is left out of the OpenAPI document, or
// "" when it is in it. A route that is neither documented nor excluded on
// purpose returns "?", which the drift test fails on.
func Undocumented(method, path string) string {
	for _, r := range documented {
		if r.method == method && r.path == path {
			return ""
		}
	}
	for prefix, why := range undocumented {
		if strings.HasPrefix(path, prefix) {
			return why
		}
	}
	if method != "GET" {
		for _, prefix := range changesLeftOut {
			if strings.HasPrefix(path, prefix) {
				return "changing configuration or nodes is done in the console"
			}
		}
	}
	return "?"
}

var bodies = map[string]map[string]any{
	"target": obj(map[string]any{
		"targets": arr("string", "Node certnames."), "query": str("A PQL query."), "group": integer("A node group id."),
	}, "Exactly one of targets, query or group."),
	"run": obj(map[string]any{
		"targets": arr("string", "Node certnames."), "query": str("A PQL query."), "group": integer("A node group id."),
		"options": obj(map[string]any{"noop": boolean(), "environment": str(""), "debug": boolean()}, "Optional run options."),
	}, "Exactly one of targets, query or group."),
	"task": obj(map[string]any{
		"task": str("The task's full name, e.g. package::install."), "environment": str("Code environment; default production."),
		"params": map[string]any{"type": "object", "description": "The task's parameters."}, "timeoutSeconds": integer(""),
		"targets": arr("string", "Node certnames."), "query": str("A PQL query."), "group": integer("A node group id."),
	}, "Exactly one of targets, query or group."),
	"plan": obj(map[string]any{
		"name": str("A name for the plan."), "steps": map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"targets": arr("string", "Node certnames."), "query": str("A PQL query."), "group": integer("A node group id."),
	}, ""),
	"patch": obj(map[string]any{
		"targets": arr("string", "Node certnames."), "query": str("A PQL query."), "group": integer("A node group id."),
		"options": obj(map[string]any{"scope": str("security, all or packages"), "packages": arr("string", ""), "reboot": boolean(), "dryRun": boolean(), "batchSize": integer(""), "stopAfterFailures": integer("")}, ""),
	}, ""),
	"codeplan": obj(map[string]any{
		"plan": str("The plan's full name."), "environment": str("Code environment; default production."), "parameters": map[string]any{"type": "object"},
	}, ""),
	"decision": obj(map[string]any{"note": str("An optional note, shown to the agent.")}, ""),
}

func obj(props map[string]any, desc string) map[string]any {
	o := map[string]any{"type": "object", "properties": props}
	if desc != "" {
		o["description"] = desc
	}
	return o
}
func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func integer(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
func boolean() map[string]any { return map[string]any{"type": "boolean"} }
func arr(item, desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": item}, "description": desc}
}

var pathParam = regexp.MustCompile(`\{([^}]+)\}`)

// OpenAPI builds the document describing the documented routes.
func OpenAPI(version string) map[string]any {
	paths := map[string]map[string]any{}
	for _, r := range documented {
		op := map[string]any{
			"summary":  r.summary,
			"security": []map[string][]string{{"bearer": {}}},
			"responses": map[string]any{
				"200": map[string]any{"description": "Success."},
				"401": map[string]any{"description": "Missing, invalid, expired or revoked token."},
				"403": map[string]any{"description": "The token lacks " + r.permission + ", or a node is outside its scope."},
			},
			"x-required-permission": r.permission,
		}
		var params []map[string]any
		for _, m := range pathParam.FindAllStringSubmatch(r.path, -1) {
			params = append(params, map[string]any{"name": m[1], "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
		}
		for _, q := range r.query {
			params = append(params, map[string]any{"name": q, "in": "query", "schema": map[string]any{"type": "string"}})
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		if r.body != "" {
			op["requestBody"] = map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": bodies[r.body]}}}
		}
		if paths[r.path] == nil {
			paths[r.path] = map[string]any{}
		}
		paths[r.path][strings.ToLower(r.method)] = op
	}
	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":   "OpenVox Console API",
			"version": version,
			"description": "The endpoints an integration is likely to use. Authenticate with a user's access token or a service token. " +
				"AI agents should use the MCP endpoint at /mcp instead: its tokens cannot call this API, and what an agent asks to change waits for a person's approval.",
		},
		"paths": paths,
		"components": map[string]any{"securitySchemes": map[string]any{
			"bearer": map[string]any{"type": "http", "scheme": "bearer", "bearerFormat": "JWT"},
		}},
	}
}

// RegisterOpenAPI serves the document at GET /api/v1/openapi.json to any
// authenticated caller.
func RegisterOpenAPI(mux *http.ServeMux, verifier *rbac.Verifier, version string) {
	doc := OpenAPI(version)
	mux.HandleFunc("GET /api/v1/openapi.json", verifier.Authenticate(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(doc)
	}))
}

// sortedPaths lists the documented paths, for tests.
func sortedPaths() []string {
	seen := map[string]bool{}
	for _, r := range documented {
		seen[r.method+" "+r.path] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
