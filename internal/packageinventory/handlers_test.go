package packageinventory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

func noopRecordAudit(_ *http.Request, _ auditlog.Event)     {}
func noopRecordAuditRead(_ *http.Request, _ auditlog.Event) {}

type fakeClient struct {
	nodePackages map[string][]openvoxdb.Package
	nodeFacts    map[string]*openvoxdb.Fact // by certname; only one fact name is ever asked for
	searchResult []openvoxdb.Package
	gotSearch    struct {
		name    string
		version *string
	}
	packageVersions []openvoxdb.PackageVersion
	nameCounts      []openvoxdb.PackageNameCount
	providers       []openvoxdb.ProviderCount
	nodeCounts      []openvoxdb.NodePackageCount
	nodes           []openvoxdb.Node
	gotFilter       openvoxdb.PackageFilter
	err             error
	factErr         error
	factCalls       int
}

func (f *fakeClient) PackageVersions(_ context.Context, filter openvoxdb.PackageFilter) ([]openvoxdb.PackageVersion, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.gotFilter = filter
	return f.packageVersions, nil
}

func (f *fakeClient) PackageNameCounts(_ context.Context, filter openvoxdb.PackageFilter) ([]openvoxdb.PackageNameCount, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.gotFilter = filter
	return f.nameCounts, nil
}

func (f *fakeClient) PackageProviders(_ context.Context) ([]openvoxdb.ProviderCount, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.providers, nil
}

func (f *fakeClient) PackageCountsByNode(_ context.Context) ([]openvoxdb.NodePackageCount, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.nodeCounts, nil
}

func (f *fakeClient) Nodes(_ context.Context) ([]openvoxdb.Node, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.nodes, nil
}

func (f *fakeClient) NodePackages(_ context.Context, certname string) ([]openvoxdb.Package, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.nodePackages[certname], nil
}

func (f *fakeClient) NodeFact(_ context.Context, certname, _ string) (*openvoxdb.Fact, error) {
	f.factCalls++
	if f.factErr != nil {
		return nil, f.factErr
	}
	return f.nodeFacts[certname], nil
}

func (f *fakeClient) SearchPackages(_ context.Context, name string, version *string) ([]openvoxdb.Package, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.gotSearch.name = name
	f.gotSearch.version = version
	return f.searchResult, nil
}

func catalogFake() *fakeClient {
	return &fakeClient{
		nameCounts: []openvoxdb.PackageNameCount{
			{PackageName: "openssl", Provider: "apt", Count: 3},
			{PackageName: "curl", Provider: "apt", Count: 2},
			// Same package from two providers: one row, counts summed.
			{PackageName: "bash", Provider: "apt", Count: 2},
			{PackageName: "bash", Provider: "rpm", Count: 1},
		},
		packageVersions: []openvoxdb.PackageVersion{
			{PackageName: "openssl", Version: "3.0.2"},
			{PackageName: "openssl", Version: "3.0.1"},
			{PackageName: "curl", Version: "8.5.0"},
			{PackageName: "bash", Version: "5.2.15"},
		},
		providers:  []openvoxdb.ProviderCount{{Provider: "rpm", Count: 1}, {Provider: "apt", Count: 7}},
		nodeCounts: []openvoxdb.NodePackageCount{{Certname: "a", Count: 4}, {Certname: "b", Count: 4}},
		nodes:      []openvoxdb.Node{{Certname: "a"}, {Certname: "b"}, {Certname: "c"}},
	}
}

// TestPackagesCatalog_CollapsesAndSorts covers the merge: one row per
// package name however many providers report it, counts summed,
// versions and providers sorted, and the list ordered by name so paging
// is stable across requests.
func TestPackagesCatalog_CollapsesAndSorts(t *testing.T) {
	client := catalogFake()
	h := NewHandlers(client, &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)
	rec := httptest.NewRecorder()
	h.packagesCatalog(rec, httptest.NewRequest(http.MethodGet, "/api/v1/packages/catalog", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got packageCatalog
	decodeBody(t, rec, &got)

	if got.Total != 3 {
		t.Fatalf("Total = %d, want 3 distinct packages", got.Total)
	}
	names := make([]string, 0, len(got.Items))
	for _, it := range got.Items {
		names = append(names, it.PackageName)
	}
	if !reflect.DeepEqual(names, []string{"bash", "curl", "openssl"}) {
		t.Errorf("names = %v, want them sorted [bash curl openssl]", names)
	}

	bash := got.Items[0]
	if bash.NodeCount != 3 {
		t.Errorf("bash NodeCount = %d, want 3 (2 apt + 1 rpm summed into one row)", bash.NodeCount)
	}
	if !reflect.DeepEqual(bash.Providers, []string{"apt", "rpm"}) {
		t.Errorf("bash Providers = %v, want [apt rpm]", bash.Providers)
	}
	openssl := got.Items[2]
	if !reflect.DeepEqual(openssl.Versions, []string{"3.0.1", "3.0.2"}) {
		t.Errorf("openssl Versions = %v, want them sorted", openssl.Versions)
	}
	if !reflect.DeepEqual(got.Providers, []string{"apt", "rpm"}) {
		t.Errorf("filter providers = %v, want [apt rpm] sorted", got.Providers)
	}
	if got.Coverage.NodesReporting != 2 || got.Coverage.NodesTotal != 3 {
		t.Errorf("coverage = %d/%d, want 2/3", got.Coverage.NodesReporting, got.Coverage.NodesTotal)
	}
}

// TestPackagesCatalog_GroupFilter covers the three group outcomes: a
// resolved group scopes the query to its certnames, a group matching
// nothing returns an empty catalogue rather than the whole fleet, and a
// caller without classifier:read is refused.
func TestPackagesCatalog_GroupFilter(t *testing.T) {
	t.Run("scopes the query to the group's nodes", func(t *testing.T) {
		client := catalogFake()
		h := NewHandlers(client, &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)
		h.SetGroupResolver(func(*http.Request, int64) ([]string, error) {
			return []string{"a.example.com", "b.example.com"}, nil
		})
		rec := httptest.NewRecorder()
		h.packagesCatalog(rec, httptest.NewRequest(http.MethodGet, "/api/v1/packages/catalog?group=7", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		want := []string{"a.example.com", "b.example.com"}
		if !reflect.DeepEqual(client.gotFilter.Certnames, want) {
			t.Errorf("filter certnames = %v, want %v", client.gotFilter.Certnames, want)
		}
	})

	t.Run("a group matching nothing returns nothing, not everything", func(t *testing.T) {
		client := catalogFake()
		h := NewHandlers(client, &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)
		h.SetGroupResolver(func(*http.Request, int64) ([]string, error) { return nil, nil })
		rec := httptest.NewRecorder()
		h.packagesCatalog(rec, httptest.NewRequest(http.MethodGet, "/api/v1/packages/catalog?group=7", nil))

		var got packageCatalog
		decodeBody(t, rec, &got)
		if len(got.Items) != 0 || got.Total != 0 {
			t.Errorf("got %d items (total %d), want an empty catalogue", len(got.Items), got.Total)
		}
		if client.gotFilter.Certnames != nil {
			t.Error("openvoxdb was queried for an empty group; it should be short-circuited")
		}
	})

	t.Run("refused without classifier:read", func(t *testing.T) {
		h := NewHandlers(catalogFake(), &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)
		h.SetGroupResolver(func(*http.Request, int64) ([]string, error) { return nil, ErrGroupForbidden })
		rec := httptest.NewRecorder()
		h.packagesCatalog(rec, httptest.NewRequest(http.MethodGet, "/api/v1/packages/catalog?group=7", nil))

		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403 - group filtering leaks group membership", rec.Code)
		}
	})

	t.Run("unavailable when no resolver is wired", func(t *testing.T) {
		h := NewHandlers(catalogFake(), &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)
		rec := httptest.NewRecorder()
		h.packagesCatalog(rec, httptest.NewRequest(http.MethodGet, "/api/v1/packages/catalog?group=7", nil))

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 rather than silently ignoring the group", rec.Code)
		}
	})
}

// TestPackagesCatalog_PassesFiltersAndPages confirms the filters reach
// openvoxdb rather than being applied after the fact, and that paging
// slices the sorted list.
func TestPackagesCatalog_PassesFiltersAndPages(t *testing.T) {
	client := catalogFake()
	h := NewHandlers(client, &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)
	rec := httptest.NewRecorder()
	h.packagesCatalog(rec, httptest.NewRequest(http.MethodGet, "/api/v1/packages/catalog?name=ssl&provider=apt&page=2&pageSize=2", nil))

	if client.gotFilter.NameContains != "ssl" || client.gotFilter.Provider != "apt" {
		t.Errorf("filter passed to openvoxdb = %+v, want {NameContains:ssl Provider:apt}", client.gotFilter)
	}
	var got packageCatalog
	decodeBody(t, rec, &got)
	if got.Page != 2 || got.PageSize != 2 {
		t.Errorf("page/pageSize = %d/%d, want 2/2", got.Page, got.PageSize)
	}
	// Three packages, 2 per page - page 2 holds the last one only.
	if len(got.Items) != 1 || got.Items[0].PackageName != "openssl" {
		t.Errorf("page 2 items = %+v, want just openssl", got.Items)
	}
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

func TestListNodePackages_ReturnsPackages(t *testing.T) {
	client := &fakeClient{nodePackages: map[string][]openvoxdb.Package{
		"web01": {
			{Certname: "web01", PackageName: "openssl", Provider: "apt", Version: "3.0.2-0ubuntu1.10"},
			{Certname: "web01", PackageName: "curl", Provider: "apt", Version: "7.81.0-1ubuntu1.15"},
		},
	}}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/web01/packages", nil)
	req.SetPathValue("name", "web01")
	rec := httptest.NewRecorder()
	h.listNodePackages(rec, req)

	var got []packageSummary
	decodeBody(t, rec, &got)
	if len(got) != 2 {
		t.Fatalf("got %d packages, want 2", len(got))
	}
	if got[0].PackageName != "openssl" || got[0].Version != "3.0.2-0ubuntu1.10" || got[0].Provider != "apt" {
		t.Errorf("got[0] = %+v, unexpected", got[0])
	}
}

func TestListNodePackages_NodeWithNoneReturnsEmptyList(t *testing.T) {
	client := &fakeClient{nodePackages: map[string][]openvoxdb.Package{}}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/unknown/packages", nil)
	req.SetPathValue("name", "unknown")
	rec := httptest.NewRecorder()
	h.listNodePackages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got []packageSummary
	decodeBody(t, rec, &got)
	if len(got) != 0 {
		t.Errorf("got %d packages, want 0", len(got))
	}
}

func TestSearchPackages_NameOnly(t *testing.T) {
	client := &fakeClient{searchResult: []openvoxdb.Package{
		{Certname: "web01", PackageName: "openssl", Provider: "apt", Version: "3.0.2-0ubuntu1.10"},
		{Certname: "web02", PackageName: "openssl", Provider: "apt", Version: "3.0.2-0ubuntu1.15"},
	}}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/packages?name=openssl", nil)
	rec := httptest.NewRecorder()
	h.searchPackages(rec, req)

	if client.gotSearch.name != "openssl" || client.gotSearch.version != nil {
		t.Errorf("SearchPackages called with name=%q version=%v, want name=openssl version=nil", client.gotSearch.name, client.gotSearch.version)
	}
	var got []packageSearchResult
	decodeBody(t, rec, &got)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
}

func TestSearchPackages_NameAndVersion(t *testing.T) {
	client := &fakeClient{searchResult: []openvoxdb.Package{
		{Certname: "web01", PackageName: "openssl", Provider: "apt", Version: "3.0.2-0ubuntu1.10"},
	}}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/packages?name=openssl&version=3.0.2-0ubuntu1.10", nil)
	rec := httptest.NewRecorder()
	h.searchPackages(rec, req)

	if client.gotSearch.name != "openssl" || client.gotSearch.version == nil || *client.gotSearch.version != "3.0.2-0ubuntu1.10" {
		t.Errorf("SearchPackages called with name=%q version=%v, want name=openssl version=3.0.2-0ubuntu1.10", client.gotSearch.name, client.gotSearch.version)
	}
	var got []packageSearchResult
	decodeBody(t, rec, &got)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
}

func TestSearchPackages_NoMatchReturnsEmptyResult(t *testing.T) {
	client := &fakeClient{searchResult: nil}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/packages?name=nonexistent-package", nil)
	rec := httptest.NewRecorder()
	h.searchPackages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got []packageSearchResult
	decodeBody(t, rec, &got)
	if len(got) != 0 {
		t.Errorf("got %d results, want 0", len(got))
	}
}

func TestSearchPackages_MissingNameIsBadRequest(t *testing.T) {
	client := &fakeClient{}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/packages", nil)
	rec := httptest.NewRecorder()
	h.searchPackages(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
