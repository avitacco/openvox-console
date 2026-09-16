package packageinventory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	err       error
	factErr   error
	factCalls int
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
