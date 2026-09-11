package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/certstatus"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

func noopRecordAudit(_ *http.Request, _ auditlog.Event)     {}
func noopRecordAuditRead(_ *http.Request, _ auditlog.Event) {}

type fakeClient struct {
	nodes         []openvoxdb.Node
	facts         map[string][]openvoxdb.Fact
	factCertnames map[[2]string][]string
	err           error
	// byCertname backs NodeByCertname for a certname not present in
	// nodes - e.g. one that's already deactivated, which a real
	// openvoxdb excludes from Nodes' collection query entirely but
	// still finds via the single-node lookup route (see design.md in
	// add-node-deletion). NodeByCertname falls back to scanning nodes
	// itself first, so most tests don't need to set this.
	byCertname          map[string]openvoxdb.Node
	deactivateErr       error
	deactivatedCertname string
}

func (f *fakeClient) Nodes(context.Context) ([]openvoxdb.Node, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.nodes, nil
}

func (f *fakeClient) NodeByCertname(_ context.Context, certname string) (*openvoxdb.Node, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, n := range f.nodes {
		if n.Certname == certname {
			return &n, nil
		}
	}
	if n, ok := f.byCertname[certname]; ok {
		return &n, nil
	}
	return nil, openvoxdb.ErrNodeNotFound
}

func (f *fakeClient) DeactivateNode(_ context.Context, certname string) error {
	if f.deactivateErr != nil {
		return f.deactivateErr
	}
	f.deactivatedCertname = certname
	return nil
}

func (f *fakeClient) Facts(_ context.Context, certname string) ([]openvoxdb.Fact, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.facts[certname], nil
}

func (f *fakeClient) FactCertnames(_ context.Context, name, value string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.factCertnames[[2]string{name, value}], nil
}

func statusPtr(s string) *string { return &s }

func TestListNodes_ReturnsAllNodes(t *testing.T) {
	now := time.Now()
	client := &fakeClient{nodes: []openvoxdb.Node{
		{Certname: "web01", LatestReportStatus: statusPtr("success"), ReportTimestamp: &now},
		{Certname: "db01", LatestReportStatus: statusPtr("failed"), ReportTimestamp: &now},
	}}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.listNodes(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil))

	var page pagination.Page[nodeSummary]
	decodeBody(t, rec, &page)
	if len(page.Items) != 2 {
		t.Fatalf("got %d nodes, want 2", len(page.Items))
	}
	if page.Total != 2 {
		t.Errorf("Total = %d, want 2", page.Total)
	}
}

func boolPtr(b bool) *bool { return &b }

func TestNodesSummary_ClassifiesAllFourCategories(t *testing.T) {
	client := &fakeClient{nodes: []openvoxdb.Node{
		{Certname: "failed01", LatestReportStatus: statusPtr("failed")},
		{Certname: "corrected01", LatestReportStatus: statusPtr("changed"), LatestReportCorrectiveChange: boolPtr(true)},
		{Certname: "intentional01", LatestReportStatus: statusPtr("changed"), LatestReportCorrectiveChange: boolPtr(false)},
		{Certname: "intentional02-untracked", LatestReportStatus: statusPtr("changed"), LatestReportCorrectiveChange: nil},
		{Certname: "unchanged01", LatestReportStatus: statusPtr("unchanged")},
	}}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.nodesSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nodes/summary", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var s summary
	decodeBody(t, rec, &s)
	if s.Total != 5 {
		t.Errorf("Total = %d, want 5", s.Total)
	}
	want := map[string]int{"failed": 1, "corrected": 1, "intentional": 2, "unchanged": 1}
	for status, count := range want {
		if s.ByStatus[status] != count {
			t.Errorf("ByStatus[%q] = %d, want %d", status, s.ByStatus[status], count)
		}
	}
}

func TestNodesSummary_ExcludesNoopAndUnreportedButKeepsTotal(t *testing.T) {
	client := &fakeClient{nodes: []openvoxdb.Node{
		{Certname: "noop01", LatestReportStatus: statusPtr("noop")},
		{Certname: "unreported01", LatestReportStatus: nil},
		{Certname: "unchanged01", LatestReportStatus: statusPtr("unchanged")},
	}}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.nodesSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nodes/summary", nil))

	var s summary
	decodeBody(t, rec, &s)
	if s.Total != 3 {
		t.Errorf("Total = %d, want 3 (excluded nodes still counted in Total)", s.Total)
	}
	sumOfCategories := s.ByStatus["failed"] + s.ByStatus["corrected"] + s.ByStatus["intentional"] + s.ByStatus["unchanged"]
	if sumOfCategories != 1 {
		t.Errorf("sum of the four categories = %d, want 1 (only unchanged01 counted; noop01/unreported01 excluded)", sumOfCategories)
	}
	if s.ByStatus["unchanged"] != 1 {
		t.Errorf(`ByStatus["unchanged"] = %d, want 1`, s.ByStatus["unchanged"])
	}
}

func TestNodesSummary_EmptyCategoriesStillPresentAtZero(t *testing.T) {
	client := &fakeClient{nodes: []openvoxdb.Node{
		{Certname: "unchanged01", LatestReportStatus: statusPtr("unchanged")},
	}}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.nodesSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nodes/summary", nil))

	var s summary
	decodeBody(t, rec, &s)
	for _, category := range []string{"failed", "corrected", "intentional", "unchanged"} {
		if _, ok := s.ByStatus[category]; !ok {
			t.Errorf("ByStatus missing key %q, want it present (even at 0)", category)
		}
	}
}

func TestListNodes_EmptyIsEmptyPageNotError(t *testing.T) {
	h := NewHandlers(&fakeClient{nodes: nil}, nil, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.listNodes(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var page pagination.Page[nodeSummary]
	decodeBody(t, rec, &page)
	if len(page.Items) != 0 {
		t.Errorf("Items = %+v, want empty", page.Items)
	}
	if page.Total != 0 {
		t.Errorf("Total = %d, want 0", page.Total)
	}
}

func TestListNodes_FilterByName(t *testing.T) {
	client := &fakeClient{nodes: []openvoxdb.Node{
		{Certname: "web01"},
		{Certname: "db01"},
	}}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.listNodes(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nodes?name=web", nil))

	var page pagination.Page[nodeSummary]
	decodeBody(t, rec, &page)
	if len(page.Items) != 1 || page.Items[0].Certname != "web01" {
		t.Errorf("got %+v, want only web01", page.Items)
	}
}

func TestListNodes_FilterByFactValue(t *testing.T) {
	client := &fakeClient{
		nodes: []openvoxdb.Node{
			{Certname: "web01"},
			{Certname: "db01"},
		},
		factCertnames: map[[2]string][]string{
			{"osfamily", "Debian"}: {"web01"},
		},
	}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.listNodes(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nodes?fact=osfamily&value=Debian", nil))

	var page pagination.Page[nodeSummary]
	decodeBody(t, rec, &page)
	if len(page.Items) != 1 || page.Items[0].Certname != "web01" {
		t.Errorf("got %+v, want only web01", page.Items)
	}
}

func TestListNodes_Pagination(t *testing.T) {
	client := &fakeClient{nodes: []openvoxdb.Node{
		{Certname: "a"},
		{Certname: "b"},
		{Certname: "c"},
	}}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.listNodes(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nodes?page=2&pageSize=1", nil))

	var page pagination.Page[nodeSummary]
	decodeBody(t, rec, &page)
	if len(page.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(page.Items))
	}
	if page.Items[0].Certname != "b" {
		t.Errorf("Items[0].Certname = %q, want b", page.Items[0].Certname)
	}
	if page.Total != 3 {
		t.Errorf("Total = %d, want 3", page.Total)
	}
	if page.Page != 2 || page.PageSize != 1 {
		t.Errorf("Page = %d, PageSize = %d, want 2, 1", page.Page, page.PageSize)
	}
}

func TestListNodes_PageBeyondEndIsEmpty(t *testing.T) {
	client := &fakeClient{nodes: []openvoxdb.Node{{Certname: "a"}}}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.listNodes(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nodes?page=99&pageSize=10", nil))

	var page pagination.Page[nodeSummary]
	decodeBody(t, rec, &page)
	if len(page.Items) != 0 {
		t.Errorf("Items = %+v, want empty", page.Items)
	}
	if page.Total != 1 {
		t.Errorf("Total = %d, want 1", page.Total)
	}
}

func TestGetNode_ReturnsFacts(t *testing.T) {
	client := &fakeClient{facts: map[string][]openvoxdb.Fact{
		"web01": {{Certname: "web01", Name: "osfamily", Value: json.RawMessage(`"Debian"`)}},
	}}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/web01", nil)
	req.SetPathValue("name", "web01")
	rec := httptest.NewRecorder()
	h.getNode(rec, req)

	var detail nodeDetail
	decodeBody(t, rec, &detail)
	if detail.Certname != "web01" {
		t.Errorf("Certname = %q, want web01", detail.Certname)
	}
	if string(detail.Facts["osfamily"]) != `"Debian"` {
		t.Errorf("Facts[osfamily] = %s, want \"Debian\"", detail.Facts["osfamily"])
	}
}

func TestInventory_ReadHandlers_RecordReadAuditEvents(t *testing.T) {
	client := &fakeClient{
		nodes: []openvoxdb.Node{{Certname: "web01"}},
		facts: map[string][]openvoxdb.Fact{"web01": {{Certname: "web01", Name: "osfamily", Value: json.RawMessage(`"Debian"`)}}},
	}
	var recorded []auditlog.Event
	h := NewHandlers(client, nil, noopRecordAudit, func(_ *http.Request, e auditlog.Event) { recorded = append(recorded, e) })

	h.listNodes(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil))
	h.nodesSummary(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/nodes/summary", nil))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/web01", nil)
	req.SetPathValue("name", "web01")
	h.getNode(httptest.NewRecorder(), req)

	if len(recorded) != 3 {
		t.Fatalf("recorded %d read audit events, want 3: %+v", len(recorded), recorded)
	}
	wantActions := []string{"node.list.viewed", "node.summary.viewed", "node.viewed"}
	for i, want := range wantActions {
		if recorded[i].Action != want {
			t.Errorf("event %d Action = %q, want %q", i, recorded[i].Action, want)
		}
	}
	if recorded[2].ResourceID != "web01" {
		t.Errorf("node.viewed ResourceID = %q, want web01", recorded[2].ResourceID)
	}
}

type fakeCertCleaner struct {
	cleanErr        error
	cleanedCertname string
}

func (f *fakeCertCleaner) Clean(_ context.Context, certname string) error {
	if f.cleanErr != nil {
		return f.cleanErr
	}
	f.cleanedCertname = certname
	return nil
}

func deleteRequest(certname string) *http.Request {
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/nodes/"+certname, nil)
	req.SetPathValue("name", certname)
	return req
}

func TestDeleteNode_ActiveNodeSucceeds(t *testing.T) {
	client := &fakeClient{nodes: []openvoxdb.Node{{Certname: "web01"}}}
	certClient := &fakeCertCleaner{}
	var recorded []auditlog.Event
	h := NewHandlers(client, certClient, func(_ *http.Request, e auditlog.Event) { recorded = append(recorded, e) }, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.deleteNode(rec, deleteRequest("web01"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}
	if client.deactivatedCertname != "web01" {
		t.Errorf("DeactivateNode called with %q, want web01", client.deactivatedCertname)
	}
	if certClient.cleanedCertname != "web01" {
		t.Errorf("Clean called with %q, want web01 - deletion should also clean the CA cert record (see design.md: otherwise the node keeps appearing via the connectivity/CA registry)", certClient.cleanedCertname)
	}
	if len(recorded) != 1 || recorded[0].Action != "node.deleted" || recorded[0].ResourceID != "web01" {
		t.Errorf("audit events = %+v, want one node.deleted event for web01", recorded)
	}
}

func TestDeleteNode_NoCertClientConfiguredStillSucceeds(t *testing.T) {
	client := &fakeClient{nodes: []openvoxdb.Node{{Certname: "web01"}}}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.deleteNode(rec, deleteRequest("web01"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 even with no CA client configured: %s", rec.Code, rec.Body)
	}
}

func TestDeleteNode_NoCertRecordToCleanIsNotAnError(t *testing.T) {
	client := &fakeClient{nodes: []openvoxdb.Node{{Certname: "web01"}}}
	certClient := &fakeCertCleaner{cleanErr: &certstatus.NotFoundError{Certname: "web01"}}
	h := NewHandlers(client, certClient, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.deleteNode(rec, deleteRequest("web01"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 - a node with no CA record has nothing to clean, not an error: %s", rec.Code, rec.Body)
	}
}

func TestDeleteNode_CertCleanErrorPropagates(t *testing.T) {
	client := &fakeClient{nodes: []openvoxdb.Node{{Certname: "web01"}}}
	certClient := &fakeCertCleaner{cleanErr: errors.New("CA unreachable")}
	h := NewHandlers(client, certClient, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.deleteNode(rec, deleteRequest("web01"))

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 - a real CA error should not be silently swallowed", rec.Code)
	}
}

// TestDeleteNode_AlreadyDeactivatedSucceeds exercises the case a real
// openvoxdb's Nodes() collection query can't distinguish from "unknown"
// on its own (see design.md) - byCertname represents a node
// NodeByCertname can still find via the single-node lookup route even
// though it's absent from nodes.
func TestDeleteNode_AlreadyDeactivatedSucceeds(t *testing.T) {
	deactivatedAt := time.Now()
	client := &fakeClient{
		byCertname: map[string]openvoxdb.Node{
			"web01": {Certname: "web01", Deactivated: &deactivatedAt},
		},
	}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.deleteNode(rec, deleteRequest("web01"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}
	if client.deactivatedCertname != "web01" {
		t.Errorf("DeactivateNode called with %q, want web01", client.deactivatedCertname)
	}
}

func TestDeleteNode_UnknownCertnameReturns404(t *testing.T) {
	client := &fakeClient{}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.deleteNode(rec, deleteRequest("ghost01"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
	}
	if client.deactivatedCertname != "" {
		t.Errorf("DeactivateNode was called (%q) for an unknown certname, want not called", client.deactivatedCertname)
	}
}

func TestDeleteNode_DeactivateErrorPropagates(t *testing.T) {
	client := &fakeClient{
		nodes:         []openvoxdb.Node{{Certname: "web01"}},
		deactivateErr: errors.New("openvoxdb unreachable"),
	}
	h := NewHandlers(client, nil, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.deleteNode(rec, deleteRequest("web01"))

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

// TestRegister_DeleteRequiresNodesManage confirms node deletion is
// wired to a different, stricter permission than every read endpoint in
// this capability - see specs/inventory's "Endpoints require
// authentication" requirement in add-node-deletion (nodes:manage, not
// nodes:read, so a read-only role can't delete a node).
func TestRegister_DeleteRequiresNodesManage(t *testing.T) {
	h := NewHandlers(&fakeClient{}, nil, noopRecordAudit, noopRecordAuditRead)
	mux := http.NewServeMux()

	// authorize is called once per route, synchronously, during
	// Register itself - captured here at that point, not inside the
	// returned handler (which only runs if a request is later sent
	// through mux, which this test never does).
	var gotPermissions []string
	authorize := func(permission string, next http.HandlerFunc) http.HandlerFunc {
		gotPermissions = append(gotPermissions, permission)
		return next
	}
	h.Register(mux, authorize)

	want := []string{"nodes:read", "nodes:read", "nodes:read", "nodes:manage"}
	if len(gotPermissions) != len(want) {
		t.Fatalf("Register called authorize %d times, want %d: %v", len(gotPermissions), len(want), gotPermissions)
	}
	for i, w := range want {
		if gotPermissions[i] != w {
			t.Errorf("authorize call %d permission = %q, want %q", i, gotPermissions[i], w)
		}
	}
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}
