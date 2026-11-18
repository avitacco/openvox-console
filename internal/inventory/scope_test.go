package inventory

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

func scopedTo(certnames ...string) nodescope.Func {
	return func(*http.Request) (nodescope.Scope, error) { return nodescope.Of(certnames...), nil }
}

func scopeFleet() *fakeClient {
	return &fakeClient{
		nodes: []openvoxdb.Node{
			{Certname: "web01", LatestReportStatus: statusPtr("failed")},
			{Certname: "web02", LatestReportStatus: statusPtr("unchanged")},
			{Certname: "db01", LatestReportStatus: statusPtr("failed")},
			{Certname: "db02", LatestReportStatus: statusPtr("unchanged")},
		},
		facts: map[string][]openvoxdb.Fact{
			"web01": {{Certname: "web01", Name: "osfamily", Value: json.RawMessage(`"Debian"`)}},
			"db01":  {{Certname: "db01", Name: "osfamily", Value: json.RawMessage(`"RedHat"`)}},
		},
		factCertnames: map[[2]string][]string{{"kernel", "Linux"}: {"web01", "db01"}},
	}
}

func TestListNodes_OnlyNodesInScope(t *testing.T) {
	h := NewHandlers(scopeFleet(), nil, noopRecordAudit, noopRecordAuditRead)
	h.SetScope(scopedTo("web01", "web02", "not-in-openvoxdb"))

	list := func(query string) pagination.Page[nodeSummary] {
		rec := httptest.NewRecorder()
		h.listNodes(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nodes"+query, nil))
		var page pagination.Page[nodeSummary]
		decodeBody(t, rec, &page)
		return page
	}

	page := list("")
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("total %d, %d items; want the 2 in-scope nodes: %+v", page.Total, len(page.Items), page.Items)
	}
	for _, n := range page.Items {
		if n.Certname != "web01" && n.Certname != "web02" {
			t.Errorf("out-of-scope node %q listed", n.Certname)
		}
	}
	// A filter narrows within the scope; it cannot reach outside it.
	if page := list("?name=db"); page.Total != 0 {
		t.Errorf("name filter reached %d out-of-scope nodes", page.Total)
	}
	if page := list("?fact=kernel&value=Linux"); page.Total != 1 || page.Items[0].Certname != "web01" {
		t.Errorf("fact filter = %+v, want only web01", page.Items)
	}
}

func TestNodesSummary_CountsOnlyNodesInScope(t *testing.T) {
	h := NewHandlers(scopeFleet(), nil, noopRecordAudit, noopRecordAuditRead)
	h.SetScope(scopedTo("web01", "web02"))

	rec := httptest.NewRecorder()
	h.nodesSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nodes/summary", nil))
	var s summary
	decodeBody(t, rec, &s)
	if s.Total != 2 || s.ByStatus["failed"] != 1 || s.ByStatus["unchanged"] != 1 {
		t.Errorf("summary = %+v, want total 2 with one failed and one unchanged", s)
	}
}

// A node the caller may not see must answer exactly as a node that
// does not exist, down to the bytes.
func TestGetNode_OutOfScopeIsIndistinguishableFromUnknown(t *testing.T) {
	get := func(h *Handlers, certname string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+certname, nil)
		req.SetPathValue("name", certname)
		rec := httptest.NewRecorder()
		h.getNode(rec, req)
		return rec
	}

	scoped := NewHandlers(scopeFleet(), nil, noopRecordAudit, noopRecordAuditRead)
	scoped.SetScope(scopedTo("web01"))
	hidden := get(scoped, "db01")

	// The same request, fleet-wide, against a fleet where db01 does not exist.
	without := scopeFleet()
	delete(without.facts, "db01")
	unknown := get(NewHandlers(without, nil, noopRecordAudit, noopRecordAuditRead), "db01")

	if hidden.Code != unknown.Code || hidden.Body.String() != unknown.Body.String() {
		t.Errorf("out of scope: %d %q\nunknown:      %d %q", hidden.Code, hidden.Body.String(), unknown.Code, unknown.Body.String())
	}

	var visible nodeDetail
	decodeBody(t, get(scoped, "web01"), &visible)
	if string(visible.Facts["osfamily"]) != `"Debian"` {
		t.Errorf("in-scope node lost its facts: %+v", visible)
	}
}

func TestInventory_FleetWideCallerIsUnchanged(t *testing.T) {
	h := NewHandlers(scopeFleet(), nil, noopRecordAudit, noopRecordAuditRead)
	h.SetScope(func(*http.Request) (nodescope.Scope, error) { return nodescope.Everything(), nil })

	rec := httptest.NewRecorder()
	h.listNodes(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil))
	var page pagination.Page[nodeSummary]
	decodeBody(t, rec, &page)
	if page.Total != 4 {
		t.Errorf("fleet-wide total = %d, want all 4", page.Total)
	}
}

func TestInventory_ScopeErrorFailsTheRequest(t *testing.T) {
	h := NewHandlers(scopeFleet(), nil, noopRecordAudit, noopRecordAuditRead)
	h.SetScope(func(*http.Request) (nodescope.Scope, error) { return nodescope.Scope{}, errors.New("openvoxdb down") })

	for name, call := range map[string]http.HandlerFunc{"list": h.listNodes, "summary": h.nodesSummary, "detail": h.getNode} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetPathValue("name", "web01")
		call(rec, req)
		if rec.Code != http.StatusBadGateway {
			t.Errorf("%s: status %d, want 502 rather than unfiltered data", name, rec.Code)
		}
	}
}

// Removal is judged before the node is looked up, so a node outside the
// caller's groups and one that does not exist are refused alike, and
// nothing is deactivated or cleaned.
func TestDeleteNode_NodeScope(t *testing.T) {
	client := scopeFleet()
	certs := &fakeCertCleaner{}
	audited := 0
	h := NewHandlers(client, certs, func(*http.Request, auditlog.Event) { audited++ }, noopRecordAuditRead)
	h.SetScope(scopedTo("web01"))

	hidden := httptest.NewRecorder()
	h.deleteNode(hidden, deleteRequest("db01"))
	if hidden.Code != http.StatusForbidden || !strings.Contains(hidden.Body.String(), `"outOfScope":["db01"]`) {
		t.Fatalf("out of scope: %d %s, want 403 naming the node", hidden.Code, hidden.Body)
	}
	if client.deactivatedCertname != "" || certs.cleanedCertname != "" || audited != 0 {
		t.Errorf("an out-of-scope removal had effects: deactivated %q, cleaned %q, audited %d", client.deactivatedCertname, certs.cleanedCertname, audited)
	}

	unknown := httptest.NewRecorder()
	h.deleteNode(unknown, deleteRequest("nope"))
	if got := strings.ReplaceAll(unknown.Body.String(), "nope", "db01"); unknown.Code != hidden.Code || got != hidden.Body.String() {
		t.Errorf("unknown node refused differently from a hidden one:\n hidden:  %s\n unknown: %s", hidden.Body, unknown.Body)
	}

	allowed := httptest.NewRecorder()
	h.deleteNode(allowed, deleteRequest("web01"))
	if allowed.Code != http.StatusNoContent || client.deactivatedCertname != "web01" {
		t.Errorf("in scope: %d, deactivated %q; want 204 and web01 removed", allowed.Code, client.deactivatedCertname)
	}
}
