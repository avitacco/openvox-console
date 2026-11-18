package codeplans

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

func (m memSource) Files(string) ([]File, error) {
	var out []File
	for name, src := range m {
		out = append(out, File{Name: name, Module: strings.SplitN(name, "::", 2)[0], Path: "modules/x/plans/" + name + ".yaml", Data: []byte(src)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func apiRig(t *testing.T, plans memSource) (*rig, *Handlers, *http.ServeMux) {
	r := newRig(t, plans)
	h := NewHandlers(plans, r.co, func(*http.Request) string { return r.actor })
	mux := http.NewServeMux()
	h.Register(mux, func(_ string, next http.HandlerFunc) http.HandlerFunc { return next })
	return r, h, mux
}

func do(mux *http.ServeMux, method, path string, body any) (int, string) {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, rd))
	return rec.Code, rec.Body.String()
}

const apiPlan = `
description: Echo something
parameters:
  targets: {type: TargetSpec, description: Where}
  msg: {type: String, default: hi}
  secret: {type: "Optional[Sensitive[String]]"}
steps:
  - name: e
    description: Echo it
    task: mod::echo
    targets: $targets
    parameters: {message: $msg}
`

func TestAPI_ListAndGet(t *testing.T) {
	_, _, mux := apiRig(t, memSource{"ops::demo": apiPlan, "ops::broken": "steps:\n  - command: x\n    targets: [a]\n"})
	code, body := do(mux, "GET", "/api/v1/code-plans", nil)
	var list struct {
		Available bool
		Items     []Info
	}
	_ = json.Unmarshal([]byte(body), &list)
	if code != 200 || !list.Available || len(list.Items) != 2 {
		t.Fatalf("list = %d %s", code, body)
	}
	broken, demo := list.Items[0], list.Items[1]
	if broken.Name != "ops::broken" || broken.Usable || !strings.Contains(broken.Reason, "commands are not supported") {
		t.Errorf("broken = %+v", broken)
	}
	if !demo.Usable || demo.Description != "Echo something" || len(demo.Parameters) != 3 || len(demo.Steps) != 0 {
		t.Errorf("demo = %+v", demo)
	}
	p := map[string]ParamInfo{}
	for _, x := range demo.Parameters {
		p[x.Name] = x
	}
	if !p["targets"].Target || !p["targets"].Required || p["msg"].Default != "hi" || p["msg"].Required || !p["secret"].Sensitive || p["secret"].Required {
		t.Errorf("parameters = %+v", p)
	}
	code, body = do(mux, "GET", "/api/v1/code-plans/ops::demo", nil)
	var one Info
	_ = json.Unmarshal([]byte(body), &one)
	if code != 200 || len(one.Steps) != 1 || one.Steps[0].Task != "mod::echo" || one.Steps[0].Description != "Echo it" {
		t.Errorf("get = %d %s", code, body)
	}
	if code, _ := do(mux, "GET", "/api/v1/code-plans/ops::nope", nil); code != 404 {
		t.Errorf("missing plan = %d", code)
	}
}

func TestAPI_NoCodeOnThisInstanceIsNotNoPlans(t *testing.T) {
	r := newRig(t, memSource{})
	h := NewHandlers(NewLoader(""), r.co, func(*http.Request) string { return "x" })
	mux := http.NewServeMux()
	h.Register(mux, func(_ string, next http.HandlerFunc) http.HandlerFunc { return next })
	code, body := do(mux, "GET", "/api/v1/code-plans", nil)
	if code != 200 || !strings.Contains(body, `"available":false`) || !strings.Contains(body, "not available") {
		t.Errorf("list = %d %s", code, body)
	}
}

func TestAPI_Start(t *testing.T) {
	r, h, mux := apiRig(t, memSource{"ops::demo": apiPlan, "ops::broken": "steps:\n  - command: x\n    targets: [a]\n"})
	mine, theirs := uniq("api-mine"), uniq("api-theirs")

	code, body := do(mux, "POST", "/api/v1/orchestrator/code-plans", map[string]any{"plan": "ops::demo", "parameters": map[string]any{"targets": []string{mine}, "msg": "hello"}})
	var job orchestrator.Job
	_ = json.Unmarshal([]byte(body), &job)
	if code != 200 || job.Kind != orchestrator.JobKindCodePlan || job.PlanName != "ops::demo" || job.Environment != "production" || job.TriggeredBy != r.actor {
		t.Fatalf("start = %d %s", code, body)
	}
	r.co.Wait()
	got, _ := r.store.GetJob(context.Background(), job.ID)
	if got.Status != orchestrator.StatusSucceeded {
		t.Errorf("job = %s %s", got.Status, got.Error)
	}

	cases := map[string]struct {
		body   map[string]any
		status int
		want   string
	}{
		"no plan named":          {map[string]any{}, 400, "give the plan"},
		"a plan not found":       {map[string]any{"plan": "ops::nope"}, 400, "was not found"},
		"an unusable plan":       {map[string]any{"plan": "ops::broken"}, 400, "commands are not supported"},
		"a missing parameter":    {map[string]any{"plan": "ops::demo"}, 400, "targets is required"},
		"a wrong type":           {map[string]any{"plan": "ops::demo", "parameters": map[string]any{"targets": []string{mine}, "msg": 5}}, 400, "msg"},
		"a bad environment name": {map[string]any{"plan": "ops::demo", "environment": "nope bad", "parameters": map[string]any{"targets": []string{mine}}}, 400, "invalid environment name"},
	}
	for name, c := range cases {
		code, body := do(mux, "POST", "/api/v1/orchestrator/code-plans", c.body)
		if code != c.status || !strings.Contains(body, c.want) {
			t.Errorf("%s = %d %s", name, code, body)
		}
	}
	// A parameter error names its field.
	_, body = do(mux, "POST", "/api/v1/orchestrator/code-plans", map[string]any{"plan": "ops::demo", "parameters": map[string]any{"targets": []string{mine}, "msg": 5}})
	if !strings.Contains(body, `"field":"msg"`) {
		t.Errorf("no field named: %s", body)
	}

	// Node scope: a node outside it refuses the request, naming it.
	h.SetScope(func(*http.Request) (nodescope.Scope, error) { return nodescope.Of(mine), nil })
	code, body = do(mux, "POST", "/api/v1/orchestrator/code-plans", map[string]any{"plan": "ops::demo", "parameters": map[string]any{"targets": []string{mine, theirs}}})
	if code != 403 || !strings.Contains(body, theirs) {
		t.Errorf("out of scope = %d %s", code, body)
	}
	if r.tr != nil && containsNode(r.tr.askedNodes(), theirs) {
		t.Error("a node outside the caller's scope was asked")
	}
	// Task permissions: the plan's task is named, so a caller without it is refused up front.
	h.SetScope(nodescope.Unscoped)
	h.SetTaskScope(func(*http.Request) (taskscope.Scope, error) {
		return taskscope.Resolve(context.Background(), []taskscope.Grant{{Tasks: []string{"mod::other"}, Fleet: true}}, nil)
	})
	code, body = do(mux, "POST", "/api/v1/orchestrator/code-plans", map[string]any{"plan": "ops::demo", "parameters": map[string]any{"targets": []string{mine}}})
	if code != 403 || !strings.Contains(body, "mod::echo") {
		t.Errorf("a task the roles do not allow = %d %s", code, body)
	}
	// Audit holds the names of the parameters, never their values.
	r.mu.Lock()
	defer r.mu.Unlock()
	var started *auditlog.Event
	for i := range r.audits {
		if r.audits[i].Action == "codeplan.triggered" {
			started = &r.audits[i]
			break
		}
	}
	b, _ := json.Marshal(started)
	if started == nil || !strings.Contains(string(b), `"msg"`) || strings.Contains(string(b), "hello") || strings.Contains(string(b), mine) {
		t.Errorf("start audit = %s", b)
	}
}

func containsNode(list []string, n string) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}
