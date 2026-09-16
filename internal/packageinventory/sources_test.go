package packageinventory

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

// listNodePackagesRaw runs the node package list handler for certname and
// decodes each entry as a raw field map, so tests can tell an omitted
// source field from an empty one.
func listNodePackagesRaw(t *testing.T, client *fakeClient, certname string) (*httptest.ResponseRecorder, []map[string]string) {
	t.Helper()
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+certname+"/packages", nil)
	req.SetPathValue("name", certname)
	rec := httptest.NewRecorder()
	h.listNodePackages(rec, req)
	if rec.Code != http.StatusOK {
		return rec, nil
	}
	var got []map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return rec, got
}

func companionFact(t *testing.T, value string) *openvoxdb.Fact {
	t.Helper()
	return &openvoxdb.Fact{Certname: "web01", Name: consolePackageInventoryFact, Value: json.RawMessage(value)}
}

var debianPackages = []openvoxdb.Package{
	{Certname: "web01", PackageName: "libssl3", Provider: "apt", Version: "3.0.11-1~deb12u1"},
	{Certname: "web01", PackageName: "openssl", Provider: "apt", Version: "3.0.11-1~deb12u1"},
	{Certname: "web01", PackageName: "puppet-strings", Provider: "puppet_gem", Version: "4.1.0"},
}

func TestListNodePackages_AptSourcesFromCompanionFact(t *testing.T) {
	client := &fakeClient{
		nodePackages: map[string][]openvoxdb.Package{"web01": debianPackages},
		nodeFacts: map[string]*openvoxdb.Fact{
			"web01": companionFact(t, `{"format":2,"sources":{"libssl3":["openssl","3.0.11-1~deb12u1"]}}`),
		},
	}

	_, got := listNodePackagesRaw(t, client, "web01")

	if len(got) != 3 {
		t.Fatalf("got %d packages, want 3", len(got))
	}
	if got[0]["sourcePackage"] != "openssl" || got[0]["sourceVersion"] != "3.0.11-1~deb12u1" {
		t.Errorf("libssl3 = %v, want source openssl 3.0.11-1~deb12u1", got[0])
	}
	if got[1]["sourcePackage"] != "openssl" || got[1]["sourceVersion"] != "3.0.11-1~deb12u1" {
		t.Errorf("openssl = %v, want itself as source (absent from sources map)", got[1])
	}
	if _, ok := got[2]["sourcePackage"]; ok {
		t.Errorf("non-apt package = %v, want no source fields", got[2])
	}
}

func TestListNodePackages_NoCompanionFactOmitsSources(t *testing.T) {
	client := &fakeClient{nodePackages: map[string][]openvoxdb.Package{"web01": debianPackages}}

	_, got := listNodePackagesRaw(t, client, "web01")

	for _, p := range got {
		if _, ok := p["sourcePackage"]; ok {
			t.Errorf("package %v has sourcePackage, want it omitted without the companion fact", p)
		}
		if _, ok := p["sourceVersion"]; ok {
			t.Errorf("package %v has sourceVersion, want it omitted without the companion fact", p)
		}
	}
}

func TestListNodePackages_UnusableCompanionFactOmitsSources(t *testing.T) {
	for name, value := range map[string]string{
		"format 1":      `{"format":1,"sources":{"libssl3":["openssl","3.0.11-1~deb12u1"]}}`,
		"no sources":    `{"format":2}`,
		"not an object": `"garbage"`,
	} {
		t.Run(name, func(t *testing.T) {
			client := &fakeClient{
				nodePackages: map[string][]openvoxdb.Package{"web01": debianPackages},
				nodeFacts:    map[string]*openvoxdb.Fact{"web01": companionFact(t, value)},
			}

			rec, got := listNodePackagesRaw(t, client, "web01")

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			for _, p := range got {
				if _, ok := p["sourcePackage"]; ok {
					t.Errorf("package %v has sourcePackage, want it omitted for an unusable fact", p)
				}
			}
		})
	}
}

func TestListNodePackages_RpmNodeSkipsFactLookup(t *testing.T) {
	client := &fakeClient{
		nodePackages: map[string][]openvoxdb.Package{"db01": {
			{Certname: "db01", PackageName: "openssl", Provider: "rpm", Version: "1:3.0.7-24.el9"},
		}},
		factErr: errors.New("must not be called"),
	}

	rec, got := listNodePackagesRaw(t, client, "db01")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if client.factCalls != 0 {
		t.Errorf("NodeFact called %d times, want 0 for a node with no apt packages", client.factCalls)
	}
	if len(got) != 1 || got[0]["version"] != "1:3.0.7-24.el9" {
		t.Errorf("got %v, want the rpm package with its epoch version", got)
	}
	if _, ok := got[0]["sourcePackage"]; ok {
		t.Errorf("rpm package = %v, want no source fields", got[0])
	}
}

func TestListNodePackages_FactLookupErrorIsBadGateway(t *testing.T) {
	client := &fakeClient{
		nodePackages: map[string][]openvoxdb.Package{"web01": debianPackages},
		factErr:      errors.New("openvoxdb unreachable"),
	}

	rec, _ := listNodePackagesRaw(t, client, "web01")

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}
