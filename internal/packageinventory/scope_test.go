package packageinventory

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

func scopedTo(certnames ...string) nodescope.Func {
	return func(*http.Request) (nodescope.Scope, error) { return nodescope.Of(certnames...), nil }
}

func getNamed(h http.HandlerFunc, target, certname string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.SetPathValue("name", certname)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestListNodePackages_OutOfScopeAnswersAsUnknown(t *testing.T) {
	packages := map[string][]openvoxdb.Package{
		"web01": {{Certname: "web01", PackageName: "nginx", Version: "1.24", Provider: "apt"}},
		"db01":  {{Certname: "db01", PackageName: "postgresql", Version: "16", Provider: "apt"}},
	}
	scoped := NewHandlers(&fakeClient{nodePackages: packages}, &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)
	scoped.SetScope(scopedTo("web01"))
	unscoped := NewHandlers(&fakeClient{nodePackages: packages}, &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)

	hidden := getNamed(scoped.listNodePackages, "/", "db01")
	unknown := getNamed(unscoped.listNodePackages, "/", "no-such-node")
	if hidden.Code != unknown.Code || hidden.Body.String() != unknown.Body.String() {
		t.Errorf("out of scope: %d %q\nunknown:      %d %q", hidden.Code, hidden.Body.String(), unknown.Code, unknown.Body.String())
	}

	var visible []packageSummary
	decodeBody(t, getNamed(scoped.listNodePackages, "/", "web01"), &visible)
	if len(visible) != 1 || visible[0].PackageName != "nginx" {
		t.Errorf("in-scope packages = %+v, want nginx", visible)
	}
}

// The reporting status is read from the node itself. A node outside the
// caller's scope must answer as an unknown one - "offline" - and must
// not be sent anything.
func TestReportingStatus_OutOfScopeAnswersAsUnknownAndIsNotContacted(t *testing.T) {
	ft := &fakeTransport{respond: dispatchWireResponse{Result: &dispatchResult{
		PackageInventory: &packageInventoryResult{Enabled: true},
	}}}
	scoped := NewHandlers(&fakeClient{}, ft, noopRecordAudit, noopRecordAuditRead)
	scoped.SetScope(scopedTo("web01"))

	hidden := getNamed(scoped.reportingStatus, "/", "db01")
	if ft.gotCertname != "" {
		t.Errorf("a request was dispatched to out-of-scope node %q", ft.gotCertname)
	}
	// What a node that is not connected answers, with no transport at all.
	unknown := getNamed(NewHandlers(&fakeClient{}, nil, noopRecordAudit, noopRecordAuditRead).reportingStatus, "/", "db01")
	if hidden.Code != unknown.Code || hidden.Body.String() != unknown.Body.String() {
		t.Errorf("out of scope: %d %q\nunknown:      %d %q", hidden.Code, hidden.Body.String(), unknown.Code, unknown.Body.String())
	}

	if rec := getNamed(scoped.reportingStatus, "/", "web01"); rec.Code != http.StatusOK || ft.gotCertname != "web01" {
		t.Errorf("in-scope node: status %d, dispatched to %q", rec.Code, ft.gotCertname)
	}
}

func TestSearchPackages_OnlyInstallationsOnNodesInScope(t *testing.T) {
	client := &fakeClient{searchResult: []openvoxdb.Package{
		{Certname: "web01", PackageName: "openssl", Version: "3.0.2", Provider: "apt"},
		{Certname: "db01", PackageName: "openssl", Version: "3.0.1", Provider: "apt"},
	}}
	h := NewHandlers(client, &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)
	h.SetScope(scopedTo("web01"))

	rec := httptest.NewRecorder()
	h.searchPackages(rec, httptest.NewRequest(http.MethodGet, "/api/v1/packages?name=openssl", nil))
	var results []packageSearchResult
	decodeBody(t, rec, &results)
	if len(results) != 1 || results[0].Certname != "web01" {
		t.Errorf("results = %+v, want only web01's installation", results)
	}
}

func TestPackagesCatalog_Scope(t *testing.T) {
	catalog := func(h *Handlers, query string) (packageCatalog, int) {
		rec := httptest.NewRecorder()
		h.packagesCatalog(rec, httptest.NewRequest(http.MethodGet, "/api/v1/packages/catalog"+query, nil))
		var got packageCatalog
		if rec.Code == http.StatusOK {
			decodeBody(t, rec, &got)
		}
		return got, rec.Code
	}

	t.Run("queries only the caller's nodes and counts coverage over them", func(t *testing.T) {
		client := catalogFake()
		h := NewHandlers(client, &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)
		h.SetScope(scopedTo("a", "c"))
		got, _ := catalog(h, "")

		if want := []string{"a", "c"}; !reflect.DeepEqual(client.gotFilter.Certnames, want) {
			t.Errorf("filter certnames = %v, want %v", client.gotFilter.Certnames, want)
		}
		// a reports packages, c does not; b is out of scope entirely.
		if got.Coverage.NodesReporting != 1 || got.Coverage.NodesTotal != 2 {
			t.Errorf("coverage = %+v, want 1 of 2", got.Coverage)
		}
		// Providers come from the scoped query, not the fleet-wide list.
		if want := []string{"apt", "rpm"}; !reflect.DeepEqual(got.Providers, want) {
			t.Errorf("providers = %v, want %v", got.Providers, want)
		}
	})

	t.Run("a group filter narrows within the scope", func(t *testing.T) {
		client := catalogFake()
		h := NewHandlers(client, &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)
		h.SetScope(scopedTo("a", "c"))
		h.SetGroupResolver(func(*http.Request, int64) ([]string, error) { return []string{"a", "b"}, nil })
		catalog(h, "?group=7")

		if want := []string{"a"}; !reflect.DeepEqual(client.gotFilter.Certnames, want) {
			t.Errorf("filter certnames = %v, want only the node in both: %v", client.gotFilter.Certnames, want)
		}
	})

	t.Run("a group outside the scope returns nothing, not that group's packages", func(t *testing.T) {
		client := catalogFake()
		h := NewHandlers(client, &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)
		h.SetScope(scopedTo("a"))
		h.SetGroupResolver(func(*http.Request, int64) ([]string, error) { return []string{"b"}, nil })
		got, code := catalog(h, "?group=7")

		if code != http.StatusOK || len(got.Items) != 0 || got.Total != 0 {
			t.Errorf("status %d, %d items; want an empty catalogue", code, len(got.Items))
		}
		if client.gotFilter.Certnames != nil {
			t.Error("openvoxdb was queried although no node is both in the group and in scope")
		}
	})

	t.Run("a scope with no nodes returns nothing without querying", func(t *testing.T) {
		client := catalogFake()
		h := NewHandlers(client, &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)
		h.SetScope(scopedTo())
		got, _ := catalog(h, "")
		if len(got.Items) != 0 || client.gotFilter.NameContains != "" || client.gotFilter.Certnames != nil {
			t.Errorf("empty scope: %d items, filter %+v", len(got.Items), client.gotFilter)
		}
	})

	t.Run("a fleet-wide caller is unchanged", func(t *testing.T) {
		client := catalogFake()
		h := NewHandlers(client, &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)
		h.SetScope(func(*http.Request) (nodescope.Scope, error) { return nodescope.Everything(), nil })
		got, _ := catalog(h, "")
		if client.gotFilter.Certnames != nil {
			t.Errorf("fleet-wide caller was restricted to %v", client.gotFilter.Certnames)
		}
		if got.Total != 3 || got.Coverage.NodesTotal != 3 || got.Coverage.NodesReporting != 2 {
			t.Errorf("fleet-wide catalogue = total %d, coverage %+v", got.Total, got.Coverage)
		}
	})
}

// Switching package reporting sends the node a request, so it is judged
// first: nothing is dispatched to a node outside the caller's groups.
func TestSetReporting_NodeScope(t *testing.T) {
	ft := &fakeTransport{respond: dispatchWireResponse{Result: &dispatchResult{
		PackageInventory: &packageInventoryResult{Enabled: true},
	}}}
	audited := 0
	h := NewHandlers(&fakeClient{}, ft, func(*http.Request, auditlog.Event) { audited++ }, noopRecordAuditRead)
	h.SetScope(scopedTo("web01"))

	hidden := httptest.NewRecorder()
	h.setReporting(hidden, reportingRequest(http.MethodPut, "db01", map[string]bool{"enabled": true}))
	if hidden.Code != http.StatusForbidden || !strings.Contains(hidden.Body.String(), `"outOfScope":["db01"]`) {
		t.Fatalf("out of scope: %d %s, want 403 naming the node", hidden.Code, hidden.Body)
	}
	if ft.gotCertname != "" || audited != 0 {
		t.Errorf("an out-of-scope toggle was dispatched to %q (audited %d)", ft.gotCertname, audited)
	}

	unknown := httptest.NewRecorder()
	h.setReporting(unknown, reportingRequest(http.MethodPut, "nope", map[string]bool{"enabled": true}))
	if got := strings.ReplaceAll(unknown.Body.String(), "nope", "db01"); unknown.Code != hidden.Code || got != hidden.Body.String() {
		t.Errorf("unknown node refused differently from a hidden one:\n hidden:  %s\n unknown: %s", hidden.Body, unknown.Body)
	}

	allowed := httptest.NewRecorder()
	h.setReporting(allowed, reportingRequest(http.MethodPut, "web01", map[string]bool{"enabled": true}))
	if allowed.Code != http.StatusOK || ft.gotCertname != "web01" {
		t.Errorf("in scope: %d, dispatched to %q; want 200 and web01", allowed.Code, ft.gotCertname)
	}
}
