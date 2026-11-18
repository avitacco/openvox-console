package patchinventory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

type fakeSource struct{ nodes []string }

func (f fakeSource) Nodes(context.Context) ([]openvoxdb.Node, error) {
	var out []openvoxdb.Node
	for _, n := range f.nodes {
		out = append(out, openvoxdb.Node{Certname: n})
	}
	return out, nil
}

func (f fakeSource) FleetNodeFacts(context.Context) ([]openvoxdb.NodeFacts, error) {
	var out []openvoxdb.NodeFacts
	for _, n := range f.nodes {
		os := &openvoxdb.OSFact{Name: "Ubuntu"}
		os.Release.Full = "24.04"
		if strings.HasPrefix(n, "rh") {
			os = &openvoxdb.OSFact{Name: "Rocky"}
			os.Release.Full = "9.3"
		}
		out = append(out, openvoxdb.NodeFacts{Certname: n, OS: os})
	}
	return out, nil
}

type auditLog struct {
	mu     sync.Mutex
	events []auditlog.Event
}

func (a *auditLog) record(_ *http.Request, e auditlog.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, e)
}

type rig struct {
	h     *Handlers
	store *Store
	tr    *fakeTransport
	log   *auditLog
	mux   *http.ServeMux
}

func newRig(t *testing.T, nodes ...string) *rig {
	st := newStore(t)
	tr := &fakeTransport{answers: map[string]func() ([]byte, error){}}
	log := &auditLog{}
	h := NewHandlers(st, NewScanner(tr, st), fakeSource{nodes: nodes}, log.record)
	h.now = func() time.Time { return t0.Add(time.Hour) }
	h.SetGroupResolver(func(r *http.Request, id int64) ([]string, error) {
		switch id {
		case 1:
			return []string{"web01", "web02", "rh01"}, nil
		case 2:
			return nil, ErrGroupForbidden
		}
		return nil, nil
	})
	mux := http.NewServeMux()
	// Permissions are the router's job; this exercises the handlers.
	h.Register(mux, func(_ string, next http.HandlerFunc) http.HandlerFunc { return next })
	return &rig{h: h, store: st, tr: tr, log: log, mux: mux}
}

func (r *rig) do(method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.mux.ServeHTTP(rec, req)
	return rec
}

func (r *rig) scopeTo(certnames ...string) {
	r.h.SetScope(func(*http.Request) (nodescope.Scope, error) { return nodescope.Of(certnames...), nil })
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("%v: %s", err, rec.Body.String())
	}
	return v
}

func seed(t *testing.T, r *rig) {
	ctx := context.Background()
	_ = r.store.Save(ctx, "web01", ScanResult{Manager: "apt", RebootRequired: true, Updates: []Update{
		{Name: "openssl", Installed: "1", Available: "2", Security: true}, {Name: "curl", Available: "2"}}}, t0)
	_ = r.store.Save(ctx, "web02", ScanResult{Manager: "apt"}, t0.Add(-100*time.Hour)) // clean but stale
	_ = r.store.Save(ctx, "rh01", ScanResult{Manager: "dnf", Updates: []Update{{Name: "openssl", Available: "3"}}}, t0)
}

func TestFleet_ListsTotalsAndFlagsStaleAndNeverScanned(t *testing.T) {
	r := newRig(t, "web01", "web02", "rh01", "new01")
	seed(t, r)
	resp := decode[fleetResponse](t, r.do("GET", "/api/v1/patches", ""))
	if resp.Totals.Nodes != 4 || resp.Totals.Scanned != 3 || resp.Totals.NeverScanned != 1 ||
		resp.Totals.WithUpdates != 2 || resp.Totals.WithSecurity != 1 || resp.Totals.RebootRequired != 1 || resp.Totals.Stale != 1 {
		t.Errorf("totals = %+v", resp.Totals)
	}
	byName := map[string]NodeRow{}
	for _, n := range resp.Nodes.Items {
		byName[n.Certname] = n
	}
	if !byName["web02"].Stale || byName["web01"].Stale || byName["new01"].Scanned || byName["web01"].OS != "Ubuntu 24.04" {
		t.Errorf("rows = %+v", byName)
	}
	if resp.Nodes.Items[0].Certname != "web01" { // security first
		t.Errorf("order: %+v", resp.Nodes.Items)
	}
	if len(resp.OSNames) != 2 || resp.Settings.IntervalMinutes != 1440 {
		t.Errorf("osNames=%v settings=%+v", resp.OSNames, resp.Settings)
	}
}

func TestFleet_Filters(t *testing.T) {
	r := newRig(t, "web01", "web02", "rh01", "other")
	seed(t, r)
	names := func(q string) []string {
		resp := decode[fleetResponse](t, r.do("GET", "/api/v1/patches?"+q, ""))
		var out []string
		for _, n := range resp.Nodes.Items {
			out = append(out, n.Certname)
		}
		return out
	}
	if got := names("package=openssl"); len(got) != 2 {
		t.Errorf("package filter: %v", got)
	}
	if got := names("os=rocky+9.3"); len(got) != 1 || got[0] != "rh01" {
		t.Errorf("os filter: %v", got)
	}
	if got := names("group=1"); len(got) != 3 {
		t.Errorf("group filter should drop 'other': %v", got)
	}
	if rec := r.do("GET", "/api/v1/patches?group=2", ""); rec.Code != 403 {
		t.Errorf("group without classifier:read = %d", rec.Code)
	}
	if rec := r.do("GET", "/api/v1/patches?group=abc", ""); rec.Code != 400 {
		t.Errorf("bad group = %d", rec.Code)
	}
}

func TestFleet_ScopedUserSeesOnlyTheirNodesAndTotals(t *testing.T) {
	r := newRig(t, "web01", "web02", "rh01")
	seed(t, r)
	r.scopeTo("rh01")
	resp := decode[fleetResponse](t, r.do("GET", "/api/v1/patches", ""))
	if resp.Totals.Nodes != 1 || resp.Totals.WithSecurity != 0 || len(resp.Nodes.Items) != 1 || resp.Nodes.Items[0].Certname != "rh01" {
		t.Errorf("got %+v", resp)
	}
	if len(resp.OSNames) != 1 {
		t.Errorf("OS names leak other nodes' systems: %v", resp.OSNames)
	}
	// Package search and the package filter are scoped too.
	pk := decode[struct{ Items []Match }](t, r.do("GET", "/api/v1/patches/packages?name=openssl", ""))
	if len(pk.Items) != 1 || pk.Items[0].Certname != "rh01" {
		t.Errorf("package search leaked: %+v", pk.Items)
	}
	f := decode[fleetResponse](t, r.do("GET", "/api/v1/patches?package=curl", ""))
	if f.Totals.Nodes != 0 {
		t.Errorf("package filter leaked: %+v", f.Totals)
	}
}

func TestFleet_ScopeOfNothing(t *testing.T) {
	r := newRig(t, "web01")
	seed(t, r)
	r.scopeTo()
	resp := decode[fleetResponse](t, r.do("GET", "/api/v1/patches", ""))
	if resp.Totals.Nodes != 0 || len(resp.Nodes.Items) != 0 {
		t.Errorf("got %+v", resp)
	}
	if pk := decode[struct{ Items []Match }](t, r.do("GET", "/api/v1/patches/packages?name=a", "")); len(pk.Items) != 0 {
		t.Errorf("empty scope found %+v", pk.Items)
	}
}

func TestPackageSearch(t *testing.T) {
	r := newRig(t, "web01", "rh01")
	seed(t, r)
	pk := decode[struct {
		Items []Match
		Total int
	}](t, r.do("GET", "/api/v1/patches/packages?name=openssl", ""))
	if pk.Total != 2 || pk.Items[0].Certname != "rh01" || pk.Items[1].Installed != "1" || !pk.Items[1].Security {
		t.Errorf("got %+v", pk)
	}
	if rec := r.do("GET", "/api/v1/patches/packages", ""); rec.Code != 400 {
		t.Errorf("no name = %d", rec.Code)
	}
}

func TestNode_DetailAndUnknownAndOutOfScope(t *testing.T) {
	r := newRig(t, "web01", "new01")
	seed(t, r)
	d := decode[nodeResponse](t, r.do("GET", "/api/v1/nodes/web01/patches", ""))
	if !d.Scanned || d.Snapshot.UpdateCount != 2 || len(d.Updates) != 2 || d.Updates[0].Name != "openssl" || !d.Snapshot.RebootRequired {
		t.Errorf("got %+v", d)
	}
	n := decode[nodeResponse](t, r.do("GET", "/api/v1/nodes/new01/patches", ""))
	if n.Scanned || n.Snapshot != nil || n.Updates == nil {
		t.Errorf("a never-scanned node: %+v", n)
	}
	r.scopeTo("new01")
	inScope := r.do("GET", "/api/v1/nodes/web01/patches", "")
	unknown := r.do("GET", "/api/v1/nodes/nosuch/patches", "")
	if inScope.Code != 404 || inScope.Body.String() != unknown.Body.String() {
		t.Errorf("out of scope must read as unknown: %d %s vs %d %s", inScope.Code, inScope.Body, unknown.Code, unknown.Body)
	}
}

func TestScanNode(t *testing.T) {
	r := newRig(t, "web01")
	r.tr.answers["web01"] = ok(twoUpdates)
	rec := r.do("POST", "/api/v1/nodes/web01/patches/scan", "")
	resp := decode[scanResponse](t, rec)
	if rec.Code != 200 || resp.Scanned != 1 || resp.Results[0].Updates != 2 || resp.Results[0].Security != 1 {
		t.Fatalf("%d %+v", rec.Code, resp)
	}
	if len(r.log.events) != 1 || r.log.events[0].Action != "patch.scan.requested" || r.log.events[0].ResourceID != "web01" {
		t.Errorf("audit = %+v", r.log.events)
	}
}

func TestScanNode_OfflineAndOutOfScope(t *testing.T) {
	r := newRig(t, "web01", "web02")
	rec := r.do("POST", "/api/v1/nodes/web01/patches/scan", "")
	if resp := decode[scanResponse](t, rec); rec.Code != 200 || resp.Offline != 1 || resp.Scanned != 0 {
		t.Errorf("offline: %d %+v", rec.Code, resp)
	}
	r.scopeTo("web02")
	calls := r.tr.calls.Load()
	rec = r.do("POST", "/api/v1/nodes/web01/patches/scan", "")
	if rec.Code != 403 || r.tr.calls.Load() != calls {
		t.Errorf("out of scope: %d, dispatched %d times", rec.Code, r.tr.calls.Load()-calls)
	}
	if len(r.log.events) != 1 { // only the offline one was audited
		t.Errorf("audit = %+v", r.log.events)
	}
}

func TestScanGroup_ReportsEachNodeAndSkips(t *testing.T) {
	r := newRig(t, "web01", "web02", "rh01")
	r.tr.answers["web01"] = ok(twoUpdates)
	r.tr.answers["rh01"] = ok(twoUpdates) // web02 offline
	r.scopeTo("web01", "web02")           // rh01 is outside the caller's scope
	rec := r.do("POST", "/api/v1/patches/scan", `{"group":1}`)
	resp := decode[scanResponse](t, rec)
	if rec.Code != 200 || resp.Scanned != 1 || resp.Offline != 1 || resp.Skipped != 1 || len(resp.Results) != 2 {
		t.Fatalf("%d %+v", rec.Code, resp)
	}
	if snap, _, _ := r.store.Get(context.Background(), "rh01"); snap != nil {
		t.Error("a node outside scope was scanned")
	}
	if len(r.log.events) != 1 || r.log.events[0].ResourceType != "nodeGroup" {
		t.Fatalf("one audit entry per request, got %+v", r.log.events)
	}
	after := r.log.events[0].After.(map[string]int)
	if after["scanned"] != 1 || after["offline"] != 1 || after["skipped"] != 1 {
		t.Errorf("audit counts = %v", after)
	}
}

func TestScanGroup_Errors(t *testing.T) {
	r := newRig(t, "web01")
	if rec := r.do("POST", "/api/v1/patches/scan", `{}`); rec.Code != 400 {
		t.Errorf("no group = %d", rec.Code)
	}
	if rec := r.do("POST", "/api/v1/patches/scan", `{"group":2}`); rec.Code != 403 {
		t.Errorf("group without classifier:read = %d", rec.Code)
	}
	if len(r.log.events) != 0 {
		t.Errorf("refused requests were audited: %+v", r.log.events)
	}
}

func TestSettings(t *testing.T) {
	r := newRig(t, "web01")
	rec := r.do("PUT", "/api/v1/patches/settings", `{"enabled":true,"intervalMinutes":60}`)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(r.log.events) != 1 || r.log.events[0].Action != "patch.scanSettings.updated" {
		t.Errorf("audit = %+v", r.log.events)
	}
	rec = r.do("PUT", "/api/v1/patches/settings", `{"enabled":true,"intervalMinutes":5}`)
	var body struct {
		Error string
		Field string
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != 400 || !strings.Contains(body.Error, "15 minutes and 7 days") || body.Field != "intervalMinutes" {
		t.Errorf("out of range: %d %s", rec.Code, rec.Body)
	}
	if got := decode[Settings](t, r.do("GET", "/api/v1/patches/settings", "")); got.IntervalMinutes != 60 {
		t.Errorf("a refused change altered the setting: %+v", got)
	}
	if len(r.log.events) != 1 {
		t.Errorf("a refused change was audited: %+v", r.log.events)
	}
}

func TestRoutesAreGatedByTheRightPermission(t *testing.T) {
	st := newStore(t)
	h := NewHandlers(st, NewScanner(nil, st), fakeSource{}, func(*http.Request, auditlog.Event) {})
	gates := map[string]string{}
	mux := http.NewServeMux()
	h.Register(mux, func(perm string, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { gates[r.Method+" "+r.URL.Path] = perm; next(w, r) }
	})
	for _, c := range []struct{ method, path, perm string }{
		{"GET", "/api/v1/patches", "nodes:read"},
		{"GET", "/api/v1/patches/packages", "nodes:read"},
		{"GET", "/api/v1/nodes/a/patches", "nodes:read"},
		{"POST", "/api/v1/nodes/a/patches/scan", "orchestrator:run"},
		{"POST", "/api/v1/patches/scan", "orchestrator:run"},
		{"PUT", "/api/v1/patches/settings", "nodes:manage"},
	} {
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(c.method, c.path, strings.NewReader("{}")))
		if got := gates[c.method+" "+c.path]; got != c.perm {
			t.Errorf("%s %s gated by %q, want %q", c.method, c.path, got, c.perm)
		}
	}
}
