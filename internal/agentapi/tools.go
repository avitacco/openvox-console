package agentapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/voxpupuli/enterprise-console/internal/agentrequests"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/rbac"
)

// param is one argument of a tool.
type param struct {
	name     string
	typ      string // string, integer, boolean, object, array
	desc     string
	required bool
	items    string // element type of an array
	path     bool   // substituted into the path of a read tool
}

// tool is one thing an agent may do. Read tools call the console's own GET
// endpoints; change tools submit a request for approval.
type tool struct {
	name        string
	description string
	permission  string
	readOnly    bool
	params      []param
	run         func(c *call, args map[string]any) (string, error)
}

func (t tool) allowed(claims *rbac.Claims) bool { return claims.HasPermission(t.permission) }

func (t tool) schema() map[string]any {
	props := map[string]any{}
	required := []string{}
	for _, p := range t.params {
		prop := map[string]any{"type": p.typ, "description": p.desc}
		if p.typ == "array" {
			prop["items"] = map[string]any{"type": p.items}
		}
		props[p.name] = prop
		if p.required {
			required = append(required, p.name)
		}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

// call is one tool call: the HTTP request it arrived on and the token that
// made it.
type call struct {
	h      *Handler
	r      *http.Request
	claims *rbac.Claims
}

// get reads path from the console's own API as the agent, in process. The
// token travels with the request, so the endpoint authorizes, scopes and
// masks exactly as it would for any caller holding that token.
func (c *call) get(path string, query url.Values) (string, error) {
	if c.h.internal == nil {
		return "", errors.New("reading is not available right now")
	}
	target := path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req := httptest.NewRequest(http.MethodGet, target, nil).WithContext(rbac.AllowAgentTokens(c.r.Context()))
	req.Header.Set("Authorization", c.r.Header.Get("Authorization"))
	rec := httptest.NewRecorder()
	c.h.internal.ServeHTTP(rec, req)
	body := strings.TrimSpace(rec.Body.String())
	if rec.Code >= 200 && rec.Code < 300 {
		return body, nil
	}
	var failure struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(body), &failure) == nil && failure.Error != "" {
		return "", errors.New(failure.Error)
	}
	return "", fmt.Errorf("the console answered %d", rec.Code)
}

func (c *call) caller() (agentrequests.Caller, error) {
	caller, err := c.h.cfg.Caller(c.r, c.claims)
	if err != nil {
		return agentrequests.Caller{}, fmt.Errorf("could not work out what this token may reach: %w", err)
	}
	return caller, nil
}

// text renders a value as the text of a result.
func text(v any) (string, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	return string(out), err
}

// argString, argInt and friends read an argument, refusing the wrong type.

func argString(args map[string]any, name string) (string, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return s, nil
}

func argInt(args map[string]any, name string) (int64, bool, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return 0, false, nil
	}
	n, ok := v.(json.Number)
	if !ok {
		return 0, false, fmt.Errorf("%s must be a whole number", name)
	}
	i, err := n.Int64()
	if err != nil {
		return 0, false, fmt.Errorf("%s must be a whole number", name)
	}
	return i, true, nil
}

func argStrings(args map[string]any, name string) ([]string, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a list of strings", name)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be a list of strings", name)
		}
		out = append(out, s)
	}
	return out, nil
}

// argJSON re-encodes an object or array argument as JSON.
func argJSON(args map[string]any, name string) (json.RawMessage, error) {
	v, ok := args[name]
	if !ok || v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

func checkKnown(t tool, args map[string]any) error {
	known := map[string]bool{}
	for _, p := range t.params {
		known[p.name] = true
	}
	var unknown []string
	for name := range args {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown argument(s): %s", strings.Join(unknown, ", "))
	}
	return nil
}

// readTool builds a tool that reads one console endpoint. Path arguments
// are substituted into pathTemplate; every other argument is a query
// parameter.
func readTool(name, description, permission, pathTemplate string, params ...param) tool {
	t := tool{name: name, description: description, permission: permission, readOnly: true, params: params}
	t.run = func(c *call, args map[string]any) (string, error) {
		if err := checkKnown(t, args); err != nil {
			return "", err
		}
		path := pathTemplate
		query := url.Values{}
		for _, p := range t.params {
			v, present := args[p.name]
			if !present || v == nil {
				if p.required {
					return "", fmt.Errorf("%s is required", p.name)
				}
				continue
			}
			var s string
			switch p.typ {
			case "string":
				str, ok := v.(string)
				if !ok {
					return "", fmt.Errorf("%s must be a string", p.name)
				}
				s = str
			case "integer":
				n, ok := v.(json.Number)
				if !ok {
					return "", fmt.Errorf("%s must be a whole number", p.name)
				}
				if _, err := n.Int64(); err != nil {
					return "", fmt.Errorf("%s must be a whole number", p.name)
				}
				s = n.String()
			case "boolean":
				b, ok := v.(bool)
				if !ok {
					return "", fmt.Errorf("%s must be true or false", p.name)
				}
				s = strconv.FormatBool(b)
			}
			if p.path {
				if s == "" || strings.ContainsAny(s, "/?#") {
					return "", fmt.Errorf("%s is not valid", p.name)
				}
				path = strings.ReplaceAll(path, "{"+p.name+"}", url.PathEscape(s))
			} else {
				query.Set(p.name, s)
			}
		}
		return c.get(path, query)
	}
	return t
}

var (
	pageParams = []param{
		{name: "page", typ: "integer", desc: "Page number, starting at 1."},
		{name: "pageSize", typ: "integer", desc: "Results per page (at most 100)."},
	}
	targetParams = []param{
		{name: "targets", typ: "array", items: "string", desc: "Node certnames to act on. Give exactly one of targets, query or group."},
		{name: "query", typ: "string", desc: "A PQL query whose matching nodes to act on."},
		{name: "group", typ: "integer", desc: "The id of a node group whose nodes to act on."},
	}
)

func withPaging(params ...param) []param { return append(params, pageParams...) }

func buildTools() []tool {
	const approval = " This does not run anything: it stores a request that a person must approve in the console. It returns a request id; use get_request to see whether it was approved, rejected or has expired."
	tools := []tool{
		readTool("list_nodes", "List the nodes you may see, with their status.", "nodes:read", "/api/v1/nodes",
			withPaging(
				param{name: "name", typ: "string", desc: "Only nodes whose certname contains this text."},
				param{name: "fact", typ: "string", desc: "Only nodes with this fact (a dotted path such as os.family); give value as well."},
				param{name: "value", typ: "string", desc: "The fact's value to match."})...),
		readTool("get_node", "Show one node: its facts, status and last report.", "nodes:read", "/api/v1/nodes/{name}",
			param{name: "name", typ: "string", desc: "The node's certname.", required: true, path: true}),
		readTool("get_node_reports", "List a node's recent Puppet reports.", "nodes:read", "/api/v1/nodes/{name}/reports",
			withPaging(
				param{name: "name", typ: "string", desc: "The node's certname.", required: true, path: true},
				param{name: "status", typ: "string", desc: "Only reports with this status: changed, unchanged or failed."})...),
		readTool("get_node_patches", "Show a node's pending operating system updates.", "nodes:read", "/api/v1/nodes/{name}/patches",
			param{name: "name", typ: "string", desc: "The node's certname.", required: true, path: true}),
		readTool("get_patch_inventory", "Summarize pending updates across the nodes you may see.", "nodes:read", "/api/v1/patches",
			param{name: "os", typ: "string", desc: "Only this operating system family."},
			param{name: "package", typ: "string", desc: "Only nodes with an update for this package."}),
		readTool("list_groups", "List node groups: their classes, environment and match rule.", "classifier:read", "/api/v1/groups"),
		readTool("get_group", "Show one node group.", "classifier:read", "/api/v1/groups/{id}",
			param{name: "id", typ: "integer", desc: "The group's id.", required: true, path: true}),
		readTool("get_group_nodes", "List the nodes currently in a node group.", "classifier:read", "/api/v1/groups/{id}/nodes",
			param{name: "id", typ: "integer", desc: "The group's id.", required: true, path: true}),
		readTool("list_tasks", "List the tasks the console's deployed code offers, with their descriptions.", "orchestrator:read", "/api/v1/tasks",
			param{name: "environment", typ: "string", desc: "The code environment to list tasks from; default production."}),
		readTool("get_task", "Show one task: its parameters, their types and which are sensitive.", "orchestrator:read", "/api/v1/tasks/{module}/{task}",
			param{name: "module", typ: "string", desc: "The task's module, e.g. package.", required: true, path: true},
			param{name: "task", typ: "string", desc: "The task's name within the module, e.g. install (init for the module's default task).", required: true, path: true},
			param{name: "environment", typ: "string", desc: "The code environment; default production."}),
		readTool("list_code_plans", "List the plans written in the deployed code.", "orchestrator:read", "/api/v1/code-plans",
			param{name: "environment", typ: "string", desc: "The code environment; default production."}),
		readTool("get_code_plan", "Show one plan from code: its parameters and steps.", "orchestrator:read", "/api/v1/code-plans/{name}",
			param{name: "name", typ: "string", desc: "The plan's full name, e.g. ops::restart.", required: true, path: true},
			param{name: "environment", typ: "string", desc: "The code environment; default production."}),
		readTool("list_jobs", "List jobs, newest first.", "orchestrator:read", "/api/v1/orchestrator/jobs",
			withPaging(
				param{name: "kind", typ: "string", desc: "Only this kind: run, task, plan, patch or codeplan."},
				param{name: "status", typ: "string", desc: "Only this status: running, succeeded or failed."},
				param{name: "target", typ: "string", desc: "Only jobs that ran on a node whose certname contains this text."},
				param{name: "agent", typ: "boolean", desc: "Only jobs started by approved agent requests."})...),
		readTool("get_job", "Show one job with every node's result and output.", "orchestrator:read", "/api/v1/orchestrator/jobs/{id}",
			param{name: "id", typ: "integer", desc: "The job's id.", required: true, path: true}),
	}

	tools = append(tools,
		tool{
			name: "resolve_targets", readOnly: true, permission: "orchestrator:run",
			description: "Show which nodes a set of targets would act on, within what you may reach, without asking for anything. Use it before a request_ tool.",
			params:      targetParams,
			run:         resolveTargets,
		},
		tool{
			name: "get_request", readOnly: true, permission: "orchestrator:run",
			description: "Show one of your requests: whether it is pending, approved (with the job it started), rejected (with the approver's note), failed or expired.",
			params:      []param{{name: "requestId", typ: "integer", desc: "The id request_ returned.", required: true}},
			run:         getRequest,
		},
		tool{
			name: "list_requests", readOnly: true, permission: "orchestrator:run",
			description: "List your own requests, newest first.",
			params:      []param{{name: "status", typ: "string", desc: "Only this status: pending, approved, rejected, expired or failed."}},
			run:         listRequests,
		},
		tool{
			name:        "request_task",
			permission:  "orchestrator:run",
			description: "Ask to run a task on nodes." + approval,
			params: append([]param{
				{name: "task", typ: "string", desc: "The task's full name, e.g. package::install.", required: true},
				{name: "environment", typ: "string", desc: "The code environment to take the task from; default production."},
				{name: "params", typ: "object", desc: "The task's parameters. See get_task for their names and types."},
				{name: "timeoutSeconds", typ: "integer", desc: "How long a node may run the task before it is stopped."},
			}, targetParams...),
			run: submit(orchestrator.JobKindTask),
		},
		tool{
			name:        "request_puppet_run",
			permission:  "orchestrator:run",
			description: "Ask to run Puppet on nodes." + approval,
			params: append([]param{
				{name: "options", typ: "object", desc: "Optional: noop (true to change nothing and only report), environment (a code environment) and debug (true for debug output)."},
			}, targetParams...),
			run: submit(orchestrator.JobKindRun),
		},
		tool{
			name:        "request_patch_run",
			permission:  "orchestrator:run",
			description: "Ask to apply operating system updates to nodes, in batches." + approval,
			params: append([]param{
				{name: "options", typ: "object", desc: "scope (security, all or packages), packages (a list, with scope packages), reboot, dryRun (true to install nothing), batchSize and stopAfterFailures."},
			}, targetParams...),
			run: submit(orchestrator.JobKindPatch),
		},
		tool{
			name:        "request_plan",
			permission:  "orchestrator:run",
			description: "Ask to run an ordered list of steps (Puppet runs and tasks) on nodes." + approval,
			params: append([]param{
				{name: "name", typ: "string", desc: "A name for the plan.", required: true},
				{name: "steps", typ: "array", items: "object", desc: "The steps, in order. Each has kind (run or task), and for a task taskName and params; for a run, options.", required: true},
			}, targetParams...),
			run: submit(orchestrator.JobKindPlan),
		},
		tool{
			name:        "request_code_plan",
			permission:  "orchestrator:run",
			description: "Ask to run a plan written in the deployed code. See list_code_plans and get_code_plan." + approval,
			params: []param{
				{name: "plan", typ: "string", desc: "The plan's full name, e.g. ops::restart.", required: true},
				{name: "environment", typ: "string", desc: "The code environment; default production."},
				{name: "parameters", typ: "object", desc: "The plan's parameters."},
			},
			run: submit(orchestrator.JobKindCodePlan),
		},
	)
	return tools
}

func parseTarget(args map[string]any) (orchestrator.Target, error) {
	nodes, err := argStrings(args, "targets")
	if err != nil {
		return orchestrator.Target{}, err
	}
	query, err := argString(args, "query")
	if err != nil {
		return orchestrator.Target{}, err
	}
	group, hasGroup, err := argInt(args, "group")
	if err != nil {
		return orchestrator.Target{}, err
	}
	target := orchestrator.Target{Nodes: nodes, Query: query}
	if hasGroup {
		target.Group = &group
	}
	if _, err := target.Kind(); err != nil {
		return orchestrator.Target{}, errors.New("give exactly one of targets, query or group")
	}
	return target, nil
}

func resolveTargets(c *call, args map[string]any) (string, error) {
	t := tool{params: targetParams}
	if err := checkKnown(t, args); err != nil {
		return "", err
	}
	target, err := parseTarget(args)
	if err != nil {
		return "", err
	}
	caller, err := c.caller()
	if err != nil {
		return "", err
	}
	nodes, err := c.h.cfg.Requests.Resolve(c.r.Context(), caller, target)
	if err != nil {
		return "", refusal(err)
	}
	return text(map[string]any{"count": len(nodes), "nodes": nodes})
}

// refusal turns a console refusal into the words an agent is shown.
func refusal(err error) error {
	var r *agentrequests.Refusal
	if errors.As(err, &r) {
		return errors.New(r.Reason)
	}
	if orchestrator.IsRefusal(err) {
		_, body := orchestrator.RefusalResponse(err)
		if msg, ok := body["error"].(string); ok {
			if fields, ok := body["fields"]; ok {
				detail, _ := json.Marshal(fields)
				return fmt.Errorf("%s: %s", msg, detail)
			}
			return errors.New(msg)
		}
	}
	return err
}

func submit(kind orchestrator.JobKind) func(c *call, args map[string]any) (string, error) {
	return func(c *call, args map[string]any) (string, error) {
		in := agentrequests.Input{Kind: kind}
		var err error
		if kind != orchestrator.JobKindCodePlan {
			if in.Target, err = parseTarget(args); err != nil {
				return "", err
			}
		}
		switch kind {
		case orchestrator.JobKindTask:
			if in.Task, err = argString(args, "task"); err != nil {
				return "", err
			}
			if in.Environment, err = argString(args, "environment"); err != nil {
				return "", err
			}
			if in.Params, err = argJSON(args, "params"); err != nil {
				return "", err
			}
			timeout, _, err := argInt(args, "timeoutSeconds")
			if err != nil {
				return "", err
			}
			in.TimeoutSeconds = int(timeout)
		case orchestrator.JobKindRun, orchestrator.JobKindPatch:
			if in.Options, err = argJSON(args, "options"); err != nil {
				return "", err
			}
		case orchestrator.JobKindPlan:
			if in.Name, err = argString(args, "name"); err != nil {
				return "", err
			}
			raw, err := argJSON(args, "steps")
			if err != nil {
				return "", err
			}
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&in.Steps); err != nil {
				return "", fmt.Errorf("steps are not valid: %w", err)
			}
		case orchestrator.JobKindCodePlan:
			if in.Plan, err = argString(args, "plan"); err != nil {
				return "", err
			}
			if in.Environment, err = argString(args, "environment"); err != nil {
				return "", err
			}
			if v, ok := args["parameters"]; ok && v != nil {
				m, ok := v.(map[string]any)
				if !ok {
					return "", errors.New("parameters must be an object")
				}
				in.Parameters = m
			}
		}
		caller, err := c.caller()
		if err != nil {
			return "", err
		}
		saved, err := c.h.cfg.Requests.Submit(c.r.Context(), caller, in)
		if err != nil {
			return "", refusal(err)
		}
		return text(map[string]any{
			"requestId": saved.ID,
			"status":    "pending",
			"summary":   saved.Summary,
			"nodes":     len(saved.Targets),
			"expiresAt": saved.ExpiresAt,
			"message":   "Awaiting approval: a person must approve this in the console before anything runs. Check it with get_request.",
		})
	}
}

// view is a request as its own agent sees it.
func view(r agentrequests.Request) map[string]any {
	out := map[string]any{"requestId": r.ID, "status": r.Status, "kind": r.Kind, "summary": r.Summary, "nodes": len(r.Targets), "createdAt": r.CreatedAt, "expiresAt": r.ExpiresAt}
	if r.DecidedBy != "" {
		out["decidedBy"] = r.DecidedBy
	}
	if r.Note != "" {
		out["note"] = r.Note
	}
	if r.JobID != nil {
		out["jobId"] = *r.JobID
		out["next"] = "Use get_job with this id to see the result."
	}
	if r.Failure != "" {
		out["failure"] = r.Failure
	}
	return out
}

func getRequest(c *call, args map[string]any) (string, error) {
	if err := checkKnown(tool{params: []param{{name: "requestId"}}}, args); err != nil {
		return "", err
	}
	id, ok, err := argInt(args, "requestId")
	if err != nil || !ok {
		return "", errors.New("requestId is required and must be a whole number")
	}
	caller, err := c.caller()
	if err != nil {
		return "", err
	}
	r, err := c.h.cfg.Requests.Store().Get(c.r.Context(), id)
	// Another token's request does not exist, as far as this one can tell.
	if errors.Is(err, agentrequests.ErrNotFound) || (err == nil && r.TokenID != caller.TokenID) {
		return "", errors.New("request not found")
	}
	if err != nil {
		return "", err
	}
	return text(view(r))
}

func listRequests(c *call, args map[string]any) (string, error) {
	if err := checkKnown(tool{params: []param{{name: "status"}}}, args); err != nil {
		return "", err
	}
	status, err := argString(args, "status")
	if err != nil {
		return "", err
	}
	caller, err := c.caller()
	if err != nil {
		return "", err
	}
	list, err := c.h.cfg.Requests.Store().List(c.r.Context(), agentrequests.Filter{TokenID: caller.TokenID, Status: agentrequests.Status(status)}, 50)
	if err != nil {
		return "", err
	}
	out := make([]map[string]any, 0, len(list))
	for _, r := range list {
		out = append(out, view(r))
	}
	return text(map[string]any{"requests": out})
}
