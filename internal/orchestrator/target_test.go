package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

func groupID(id int64) *int64 { return &id }

// fakeSources is a fleet of web and database servers behind a query and
// a group lookup.
type fakeSources struct {
	// rows is what any query returns; queries records what was asked.
	rows     []map[string]any
	queryErr error
	queries  []string
	groups   map[int64][]string
}

func (f *fakeSources) query(_ context.Context, pql string) ([]map[string]any, error) {
	f.queries = append(f.queries, pql)
	return f.rows, f.queryErr
}

func (f *fakeSources) group(_ context.Context, id int64) (string, []string, error) {
	certnames, ok := f.groups[id]
	if !ok {
		return "", nil, ErrGroupNotFound
	}
	return "Group " + string(rune('A'+id)), certnames, nil
}

func (f *fakeSources) resolver() *TargetResolver { return NewTargetResolver(f.query, f.group) }

func certRows(certnames ...string) []map[string]any {
	rows := make([]map[string]any, len(certnames))
	for i, c := range certnames {
		rows[i] = map[string]any{"certname": c}
	}
	return rows
}

func invalidTarget(t *testing.T, err error, mention string) {
	t.Helper()
	var invalid *InvalidTargetError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want an InvalidTargetError", err)
	}
	if !strings.Contains(invalid.Reason, mention) {
		t.Errorf("reason = %q, want it to mention %q", invalid.Reason, mention)
	}
}

func TestTargetKind_ExactlyOne(t *testing.T) {
	valid := map[string]Target{
		TargetKindNodes: {Nodes: []string{"a"}},
		TargetKindQuery: {Query: "nodes[certname] { }"},
		TargetKindGroup: {Group: groupID(0)},
	}
	for want, target := range valid {
		if got, err := target.Kind(); err != nil || got != want {
			t.Errorf("Kind(%+v) = %q, %v; want %q", target, got, err, want)
		}
	}
	invalid := []Target{
		{},
		{Query: "   "},
		{Nodes: []string{"a"}, Query: "nodes[certname] { }"},
		{Query: "nodes[certname] { }", Group: groupID(1)},
		{Nodes: []string{"a"}, Group: groupID(1)},
	}
	for _, target := range invalid {
		if _, err := target.Kind(); err == nil {
			t.Errorf("Kind(%+v) = nil error, want a refusal", target)
		}
	}
}

func TestResolve_NodeListIsAllOrNothing(t *testing.T) {
	r := (&fakeSources{}).resolver()
	ctx := context.Background()

	got, source, err := r.Resolve(ctx, Target{Nodes: []string{"web1", "web2", "web1"}}, nodescope.Of("web1", "web2"))
	if err != nil || source != nil || !reflect.DeepEqual(got, []string{"web1", "web2"}) {
		t.Fatalf("Resolve() = %v, %v, %v; want each node once and no source", got, source, err)
	}

	_, _, err = r.Resolve(ctx, Target{Nodes: []string{"web1", "db1"}}, nodescope.Of("web1"))
	var outside *OutOfScopeError
	if !errors.As(err, &outside) || !reflect.DeepEqual(outside.Outside, []string{"db1"}) {
		t.Errorf("error = %v, want an OutOfScopeError naming db1", err)
	}
}

func TestResolve_QueryIsNarrowedToScope(t *testing.T) {
	f := &fakeSources{rows: certRows("web2", "db1", "web1", "db2", "web1")}
	const pql = `inventory[certname] { facts.os.family = "Debian" }`

	got, source, err := f.resolver().Resolve(context.Background(), Target{Query: pql}, nodescope.Of("web1", "web2"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"web1", "web2"}) {
		t.Errorf("nodes = %v, want the in-scope matches, each once, sorted", got)
	}
	if source == nil || source.Kind != TargetKindQuery || source.Query != pql {
		t.Errorf("source = %+v, want the query", source)
	}

	all, _, err := f.resolver().Resolve(context.Background(), Target{Query: pql}, nodescope.Everything())
	if err != nil || len(all) != 4 {
		t.Errorf("fleet-wide nodes = %v, %v; want all four", all, err)
	}
}

// A query matching only other teams' nodes and one matching nothing are
// the same answer.
func TestResolve_OnlyOutOfScopeMatchesLookLikeNoMatches(t *testing.T) {
	scope := nodescope.Of("web1")
	others, _, errOthers := (&fakeSources{rows: certRows("db1", "db2")}).resolver().Resolve(context.Background(), Target{Query: "nodes[certname] { }"}, scope)
	none, _, errNone := (&fakeSources{}).resolver().Resolve(context.Background(), Target{Query: "nodes[certname] { }"}, scope)
	if errOthers != nil || errNone != nil || len(others) != 0 || len(none) != 0 {
		t.Errorf("others = %v, %v; none = %v, %v; want both empty without error", others, errOthers, none, errNone)
	}
}

func TestResolve_SubqueryIsRefusedForAScopedCallerBeforeItIsSent(t *testing.T) {
	const pql = `nodes[certname] { certname in facts[certname] { name = "role" } }`
	f := &fakeSources{rows: certRows("web1")}

	_, _, err := f.resolver().Resolve(context.Background(), Target{Query: pql}, nodescope.Of("web1"))
	invalidTarget(t, err, "subqueries")
	if len(f.queries) != 0 {
		t.Errorf("the query was sent to openvoxdb: %v", f.queries)
	}

	if _, _, err := f.resolver().Resolve(context.Background(), Target{Query: pql}, nodescope.Everything()); err != nil {
		t.Errorf("fleet-wide caller: %v, want the subquery run", err)
	}
	if len(f.queries) != 1 {
		t.Errorf("queries sent = %d, want 1", len(f.queries))
	}
}

func TestResolve_UnusableQueries(t *testing.T) {
	ctx := context.Background()
	everything := nodescope.Everything()

	f := &fakeSources{}
	for pql, mention := range map[string]string{
		`["from", "nodes"]`:                 "PQL string",
		strings.Repeat("x", 4001) + " { }":  "longer than",
		`nodes[certname] { certname = "a" `: "unbalanced",
	} {
		_, _, err := f.resolver().Resolve(ctx, Target{Query: pql}, everything)
		invalidTarget(t, err, mention)
	}
	if len(f.queries) != 0 {
		t.Errorf("malformed queries were sent to openvoxdb: %d", len(f.queries))
	}

	// openvoxdb rejects it: its own message is relayed.
	rejected := &fakeSources{queryErr: &openvoxdb.InvalidQueryError{Message: "PQL parse error at line 1, column 30"}}
	_, _, err := rejected.resolver().Resolve(ctx, Target{Query: "nodes[certname] { certname = }"}, everything)
	invalidTarget(t, err, "PQL parse error at line 1, column 30")

	// A row without a certname.
	counted := &fakeSources{rows: []map[string]any{{"count": float64(2)}}}
	_, _, err = counted.resolver().Resolve(ctx, Target{Query: "nodes[count()] { }"}, everything)
	invalidTarget(t, err, "must return certname")

	// openvoxdb unreachable is a failure, not a refusal.
	down := &fakeSources{queryErr: errors.New("openvoxdb unreachable: connection refused")}
	_, _, err = down.resolver().Resolve(ctx, Target{Query: "nodes[certname] { }"}, everything)
	if err == nil || IsRefusal(err) {
		t.Errorf("unreachable openvoxdb = %v, want an error that is not a refusal", err)
	}
}

func TestResolve_Group(t *testing.T) {
	f := &fakeSources{groups: map[int64][]string{1: {"web1", "db1"}, 2: {}}}
	ctx := context.Background()

	got, source, err := f.resolver().Resolve(ctx, Target{Group: groupID(1)}, nodescope.Everything())
	if err != nil || !reflect.DeepEqual(got, []string{"db1", "web1"}) {
		t.Fatalf("nodes = %v, %v; want both", got, err)
	}
	if source == nil || source.Kind != TargetKindGroup || source.GroupID != 1 || source.GroupName == "" {
		t.Errorf("source = %+v, want the group with its name", source)
	}

	got, _, err = f.resolver().Resolve(ctx, Target{Group: groupID(1)}, nodescope.Of("web1"))
	if err != nil || !reflect.DeepEqual(got, []string{"web1"}) {
		t.Errorf("scoped nodes = %v, %v; want web1 only", got, err)
	}

	_, _, err = f.resolver().Resolve(ctx, Target{Group: groupID(9)}, nodescope.Everything())
	invalidTarget(t, err, "no longer exists")
}

func TestResolve_SourcesNotConfigured(t *testing.T) {
	r := NewTargetResolver(nil, nil)
	_, _, err := r.Resolve(context.Background(), Target{Query: "nodes[certname] { }"}, nodescope.Everything())
	invalidTarget(t, err, "not available")
	_, _, err = r.Resolve(context.Background(), Target{Group: groupID(1)}, nodescope.Everything())
	invalidTarget(t, err, "not available")
}

// --- handlers ---

func targetHarness(t *testing.T, f *fakeSources) *taskHarness {
	t.Helper()
	h := newTaskHarness(t, taskResponse("ok", "", 0, `{}`))
	h.handlers.SetTargetSources(f.query, f.group)
	return h
}

func jobTargets(job Job) []string {
	names := make([]string, len(job.Targets))
	for i, target := range job.Targets {
		names[i] = target.Certname
	}
	return names
}

func TestTriggerRun_ByQuery(t *testing.T) {
	f := &fakeSources{rows: certRows("c.example.com", "a.example.com", "b.example.com", "a.example.com")}
	h := targetHarness(t, f)
	const pql = `inventory[certname] { facts.os.family = "Debian" }`

	job := decodeJob(t, h.post(t, h.handlers.triggerRun, map[string]any{"query": pql}))
	if got := jobTargets(job); !reflect.DeepEqual(got, []string{"a.example.com", "b.example.com", "c.example.com"}) {
		t.Errorf("targets = %v, want the three matches once each", got)
	}
	if job.TargetSource == nil || job.TargetSource.Query != pql {
		t.Errorf("targetSource = %+v, want the query", job.TargetSource)
	}

	stored, err := h.store.GetJob(context.Background(), job.ID)
	if err != nil || stored.TargetSource == nil || stored.TargetSource.Kind != TargetKindQuery {
		t.Errorf("stored targetSource = %+v, %v", stored.TargetSource, err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.activity) == 0 || !strings.Contains(h.activity[0], "matched by a query") {
		t.Errorf("activity = %v, want the trigger to say a query chose the nodes", h.activity)
	}
}

func TestTriggerTask_ByQueryValidatesParametersAsUsual(t *testing.T) {
	h := targetHarness(t, &fakeSources{rows: certRows("a.example.com")})

	rec := h.post(t, h.handlers.triggerTask, map[string]any{
		"query": "nodes[certname] { }", "task": "mod::echo", "params": map[string]any{"count": "three"},
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"count"`) {
		t.Fatalf("status = %d, body %s; want 400 naming count", rec.Code, rec.Body)
	}

	job := decodeJob(t, h.post(t, h.handlers.triggerTask, map[string]any{
		"query": "nodes[certname] { }", "task": "mod::echo", "params": map[string]any{"message": "hi"},
	}))
	if got := jobTargets(job); !reflect.DeepEqual(got, []string{"a.example.com"}) {
		t.Errorf("targets = %v", got)
	}
}

func TestTriggerRun_ByGroup(t *testing.T) {
	h := targetHarness(t, &fakeSources{groups: map[int64][]string{1: {"w1", "w2", "w3", "w4"}, 2: {}}})

	job := decodeJob(t, h.post(t, h.handlers.triggerRun, map[string]any{"group": 1}))
	if len(job.Targets) != 4 || job.TargetSource == nil || job.TargetSource.GroupID != 1 {
		t.Errorf("targets = %v, source = %+v; want the group's four nodes", jobTargets(job), job.TargetSource)
	}
}

func TestTriggers_RefusedTargetsCreateNoJob(t *testing.T) {
	f := &fakeSources{rows: nil, groups: map[int64][]string{2: {}}}
	h := targetHarness(t, f)

	cases := []struct {
		name    string
		body    map[string]any
		status  int
		mention string
	}{
		{"two kinds", map[string]any{"targets": []string{"a"}, "query": "nodes[certname] { }"}, 400, "only one"},
		{"query and group", map[string]any{"query": "nodes[certname] { }", "group": 2}, 400, "only one"},
		{"no kind", map[string]any{}, 400, "at least one target"},
		{"matches nothing", map[string]any{"query": "nodes[certname] { }"}, 400, "no nodes matched"},
		{"empty group", map[string]any{"group": 2}, 400, "no nodes matched"},
		{"missing group", map[string]any{"group": 77}, 400, "no longer exists"},
		{"AST form", map[string]any{"query": `["from","nodes"]`}, 400, "PQL string"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for name, handler := range map[string]http.HandlerFunc{"run": h.handlers.triggerRun, "task": h.handlers.triggerTask} {
				body := map[string]any{"task": "mod::echo"}
				for k, v := range c.body {
					body[k] = v
				}
				rec := h.post(t, handler, body)
				if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.mention) {
					t.Errorf("%s: status = %d, body %s; want %d mentioning %q", name, rec.Code, rec.Body, c.status, c.mention)
				}
			}
		})
	}

	f.queryErr = &openvoxdb.InvalidQueryError{Message: "PQL parse error at line 1"}
	rec := h.post(t, h.handlers.triggerRun, map[string]any{"query": "nodes[certname] { certname = }"})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "PQL parse error at line 1") {
		t.Errorf("rejected query: status = %d, body %s; want 400 with openvoxdb's message", rec.Code, rec.Body)
	}

	f.queryErr = errors.New("openvoxdb unreachable: connection refused")
	rec = h.post(t, h.handlers.triggerRun, map[string]any{"query": "nodes[certname] { }"})
	if rec.Code != http.StatusBadGateway {
		t.Errorf("unreachable openvoxdb: status = %d, want 502", rec.Code)
	}

	if n := h.jobCount(t); n != 0 {
		t.Errorf("%d jobs were created by refused requests", n)
	}
}

func TestTriggerRun_QueryNarrowedForAScopedCaller(t *testing.T) {
	f := &fakeSources{rows: certRows("web1", "web2", "db1", "db2", "db3")}
	h := targetHarness(t, f)
	h.handlers.SetScope(scopedTo("web1", "web2"))

	rec := h.post(t, h.handlers.triggerRun, map[string]any{"query": "nodes[certname] { }"})
	job := decodeJob(t, rec)
	if got := jobTargets(job); !reflect.DeepEqual(got, []string{"web1", "web2"}) {
		t.Errorf("targets = %v, want the two web servers", got)
	}

	// Only other teams' nodes: the same refusal as nothing matching, and
	// none of their names anywhere in it.
	f.rows = certRows("db1", "db2")
	others := h.post(t, h.handlers.triggerRun, map[string]any{"query": "nodes[certname] { }"})
	f.rows = nil
	nothing := h.post(t, h.handlers.triggerRun, map[string]any{"query": "nodes[certname] { }"})
	if others.Code != http.StatusBadRequest || others.Body.String() != nothing.Body.String() {
		t.Errorf("only-out-of-scope = %d %s; nothing = %d %s; want identical 400s", others.Code, others.Body, nothing.Code, nothing.Body)
	}

	// A named node outside scope is still refused whole, by name.
	named := h.post(t, h.handlers.triggerRun, map[string]any{"targets": []string{"web1", "db1"}})
	if named.Code != http.StatusForbidden || !reflect.DeepEqual(refusal(t, named).OutOfScope, []string{"db1"}) {
		t.Errorf("named node outside scope = %d %s, want 403 naming db1", named.Code, named.Body)
	}

	sub := h.post(t, h.handlers.triggerRun, map[string]any{"query": `nodes[certname] { facts { name = "role" } }`})
	if sub.Code != http.StatusBadRequest || !strings.Contains(sub.Body.String(), "subqueries") {
		t.Errorf("subquery = %d %s, want 400", sub.Code, sub.Body)
	}
}

func TestResolveTargets_Preview(t *testing.T) {
	f := &fakeSources{rows: certRows("web1", "web2", "db1"), groups: map[int64][]string{1: {"web1", "db1"}}}
	h := targetHarness(t, f)

	preview := func(body map[string]any) (int, resolveTargetsResponse, string) {
		rec := h.post(t, h.handlers.resolveTargets, body)
		var out resolveTargetsResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out, rec.Body.String()
	}

	code, out, _ := preview(map[string]any{"query": "nodes[certname] { }"})
	if code != http.StatusOK || out.Count != 3 || len(out.Nodes) != 3 {
		t.Errorf("fleet-wide preview = %d %+v, want three nodes", code, out)
	}

	h.handlers.SetScope(scopedTo("web1", "web2"))
	code, out, body := preview(map[string]any{"query": "nodes[certname] { }"})
	if code != http.StatusOK || !reflect.DeepEqual(out.Nodes, []string{"web1", "web2"}) || strings.Contains(body, "db1") {
		t.Errorf("scoped preview = %d %s, want only the web servers", code, body)
	}
	code, out, _ = preview(map[string]any{"group": 1})
	if code != http.StatusOK || !reflect.DeepEqual(out.Nodes, []string{"web1"}) {
		t.Errorf("scoped group preview = %d %+v, want web1", code, out)
	}

	// Nothing matching is an answer, not a refusal.
	f.rows = nil
	code, out, body = preview(map[string]any{"query": "nodes[certname] { }"})
	if code != http.StatusOK || out.Count != 0 || !strings.Contains(body, `"nodes":[]`) {
		t.Errorf("empty preview = %d %s, want 200 with an empty list", code, body)
	}

	// A refusal is the one a run would get.
	f.queryErr = &openvoxdb.InvalidQueryError{Message: "PQL parse error"}
	code, _, body = preview(map[string]any{"query": "nodes[certname] { certname = }"})
	run := h.post(t, h.handlers.triggerRun, map[string]any{"query": "nodes[certname] { certname = }"})
	if code != http.StatusBadRequest || body != run.Body.String() {
		t.Errorf("preview refusal = %d %s; run refusal = %d %s; want the same", code, body, run.Code, run.Body)
	}

	if n := h.jobCount(t); n != 0 {
		t.Errorf("previews created %d jobs", n)
	}
}

func TestResolveTargets_RequiresTheRunPermission(t *testing.T) {
	h := targetHarness(t, &fakeSources{})
	mux := http.NewServeMux()
	var gate string
	h.handlers.Register(mux, func(permission string, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { gate = permission; next(w, r) }
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/targets/resolve", strings.NewReader(`{"targets":["a"]}`)))
	if gate != "orchestrator:run" || rec.Code != http.StatusOK {
		t.Errorf("targets/resolve: gated by %q, status %d; want orchestrator:run and 200", gate, rec.Code)
	}
}

// --- store ---

func TestJobOrigin_StoredAndRead(t *testing.T) {
	f := newJobScopeFixture(t)
	ctx := context.Background()
	const pql = `nodes[certname] { certname ~ "db" }`

	job, err := f.store.CreateRunJobFrom(ctx, RunOptions{}, []string{f.web, f.db}, f.actor, JobOrigin{
		Source:   &TargetSource{Kind: TargetKindQuery, Query: pql},
		Schedule: &ScheduleRef{Name: "deleted long ago"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.TargetSource == nil || job.TargetSource.Query != pql {
		t.Errorf("targetSource = %+v", job.TargetSource)
	}
	// A schedule that no longer exists: the name is kept, without an id.
	if job.Schedule == nil || job.Schedule.Name != "deleted long ago" || job.Schedule.ID != 0 {
		t.Errorf("schedule = %+v, want the name alone", job.Schedule)
	}

	webOnly, err := f.store.CreateRunJobFrom(ctx, RunOptions{}, []string{f.web}, f.actor, JobOrigin{Source: &TargetSource{Kind: TargetKindQuery, Query: pql}})
	if err != nil {
		t.Fatal(err)
	}

	// A reader who is shown every node sees the query; one who is not,
	// does not - in the detail and in the list alike.
	scoped := []string{f.web}
	partial, err := f.store.GetJobInScope(ctx, job.ID, scoped)
	if err != nil || !partial.TargetsHidden || partial.TargetSource == nil || partial.TargetSource.Query != "" || partial.TargetSource.Kind != TargetKindQuery {
		t.Errorf("partly hidden job: %+v, hidden=%v, err=%v; want the kind without the query text", partial.TargetSource, partial.TargetsHidden, err)
	}
	whole, err := f.store.GetJobInScope(ctx, webOnly.ID, scoped)
	if err != nil || whole.TargetSource == nil || whole.TargetSource.Query != pql {
		t.Errorf("wholly visible job: %+v, %v; want the query text", whole.TargetSource, err)
	}

	listed, _ := f.list(t, scoped)
	if got := listed[job.ID].TargetSource; got == nil || got.Query != "" {
		t.Errorf("listed partly hidden job source = %+v, want no query text", got)
	}
	if got := listed[webOnly.ID].TargetSource; got == nil || got.Query != pql {
		t.Errorf("listed wholly visible job source = %+v, want the query text", got)
	}
	unscoped, _ := f.list(t, nil)
	if got := unscoped[job.ID].TargetSource; got == nil || got.Query != pql {
		t.Errorf("unscoped list source = %+v, want the query text", got)
	}
	if got := unscoped[f.webOnly.ID]; got.TargetSource != nil || got.Schedule != nil {
		t.Errorf("a job started from a list of nodes has an origin: %+v %+v", got.TargetSource, got.Schedule)
	}
}
