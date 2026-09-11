package packageinventory

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodetransport"
)

// fakeTransport records the last dispatched request and replies with a
// canned response or error - lets dispatch.go and the reporting
// endpoints be tested without a live NATS transport.
type fakeTransport struct {
	gotCertname string
	gotPayload  dispatchRequestData
	respond     dispatchWireResponse
	err         error
}

func (f *fakeTransport) Dispatch(_ context.Context, certname string, payload []byte, _ time.Duration) ([]byte, error) {
	f.gotCertname = certname
	_ = json.Unmarshal(payload, &f.gotPayload)
	if f.err != nil {
		return nil, f.err
	}
	data, _ := json.Marshal(f.respond)
	return data, nil
}

func TestDispatchPackageInventory_StatusRequest(t *testing.T) {
	ft := &fakeTransport{respond: dispatchWireResponse{Result: &dispatchResult{
		PackageInventory: &packageInventoryResult{Enabled: true},
	}}}

	result, err := dispatchPackageInventory(context.Background(), ft, "web01", actionPackageInventoryStatus, nil)
	if err != nil {
		t.Fatalf("dispatchPackageInventory() error: %v", err)
	}
	if ft.gotCertname != "web01" {
		t.Errorf("Dispatch called with certname %q, want web01", ft.gotCertname)
	}
	if ft.gotPayload.Module != moduleNamePuppet || ft.gotPayload.Action != actionPackageInventoryStatus {
		t.Errorf("dispatched payload = %+v, want module %q action %q", ft.gotPayload, moduleNamePuppet, actionPackageInventoryStatus)
	}
	if len(ft.gotPayload.Params) != 0 {
		t.Errorf("dispatched Params = %s, want empty for a status request", ft.gotPayload.Params)
	}
	if !result.PackageInventory.Enabled {
		t.Error("Enabled = false, want true")
	}
}

func TestDispatchPackageInventory_SetRequestEncodesParams(t *testing.T) {
	ft := &fakeTransport{respond: dispatchWireResponse{Result: &dispatchResult{
		PackageInventory: &packageInventoryResult{Enabled: true, Ran: true},
	}}}

	_, err := dispatchPackageInventory(context.Background(), ft, "web01", actionPackageInventorySet, packageInventorySetParams{Enabled: true})
	if err != nil {
		t.Fatalf("dispatchPackageInventory() error: %v", err)
	}
	var params packageInventorySetParams
	if err := json.Unmarshal(ft.gotPayload.Params, &params); err != nil {
		t.Fatalf("decode dispatched params: %v", err)
	}
	if !params.Enabled {
		t.Error("dispatched params.Enabled = false, want true")
	}
}

func TestDispatchPackageInventory_NodeErrorPropagates(t *testing.T) {
	ft := &fakeTransport{respond: dispatchWireResponse{Error: "node is busy executing another request"}}

	_, err := dispatchPackageInventory(context.Background(), ft, "web01", actionPackageInventoryStatus, nil)
	if err == nil {
		t.Fatal("error is nil, want the node-reported error surfaced")
	}
}

func TestDispatchPackageInventory_TransportErrorPropagates(t *testing.T) {
	ft := &fakeTransport{err: nodetransport.ErrNodeNotConnected}

	_, err := dispatchPackageInventory(context.Background(), ft, "web01", actionPackageInventoryStatus, nil)
	if err != nodetransport.ErrNodeNotConnected {
		t.Errorf("error = %v, want ErrNodeNotConnected", err)
	}
}

func reportingRequest(method, certname string, body any) *http.Request {
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, "/api/v1/nodes/"+certname+"/packages/reporting", bytes.NewReader(b))
	} else {
		r = httptest.NewRequest(method, "/api/v1/nodes/"+certname+"/packages/reporting", nil)
	}
	r.SetPathValue("name", certname)
	return r
}

func TestReportingStatus_Success(t *testing.T) {
	ft := &fakeTransport{respond: dispatchWireResponse{Result: &dispatchResult{
		PackageInventory: &packageInventoryResult{Enabled: true},
	}}}
	var reads []auditlog.Event
	h := NewHandlers(&fakeClient{}, ft, noopRecordAudit, func(_ *http.Request, e auditlog.Event) { reads = append(reads, e) })

	rec := httptest.NewRecorder()
	h.reportingStatus(rec, reportingRequest(http.MethodGet, "web01", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got reportingStatus
	decodeBody(t, rec, &got)
	if !got.Enabled {
		t.Error("Enabled = false, want true")
	}
	if got.Output != nil {
		t.Errorf("Output = %+v, want nil (a status request never runs Puppet)", got.Output)
	}
	if len(reads) != 1 || reads[0].Action != "node.packageInventory.viewed" {
		t.Errorf("read audit events = %+v, want one node.packageInventory.viewed event", reads)
	}
}

func TestReportingStatus_NodeOffline(t *testing.T) {
	ft := &fakeTransport{err: nodetransport.ErrNodeNotConnected}
	h := NewHandlers(&fakeClient{}, ft, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.reportingStatus(rec, reportingRequest(http.MethodGet, "web01", nil))

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (node offline), body: %s", rec.Code, rec.Body)
	}
}

func TestReportingStatus_NoTransportConfiguredIsNodeOffline(t *testing.T) {
	h := NewHandlers(&fakeClient{}, nil, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.reportingStatus(rec, reportingRequest(http.MethodGet, "web01", nil))

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (no transport configured, same as offline)", rec.Code)
	}
}

func TestSetReporting_Success(t *testing.T) {
	ft := &fakeTransport{respond: dispatchWireResponse{Result: &dispatchResult{
		Output:           dispatchOutput{Stdout: "changes applied", ExitCode: 2},
		PackageInventory: &packageInventoryResult{Enabled: true, Ran: true},
	}}}
	var recorded []auditlog.Event
	h := NewHandlers(&fakeClient{}, ft, func(_ *http.Request, e auditlog.Event) { recorded = append(recorded, e) }, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.setReporting(rec, reportingRequest(http.MethodPut, "web01", map[string]bool{"enabled": true}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got reportingStatus
	decodeBody(t, rec, &got)
	if !got.Enabled {
		t.Error("Enabled = false, want true")
	}
	if got.Output == nil || got.Output.Stdout != "changes applied" {
		t.Errorf("Output = %+v, want the Puppet run's real output since Ran was true", got.Output)
	}
	if ft.gotPayload.Action != actionPackageInventorySet {
		t.Errorf("dispatched action = %q, want %q", ft.gotPayload.Action, actionPackageInventorySet)
	}
	if len(recorded) != 1 || recorded[0].Action != "node.packageInventory.set" || recorded[0].ResourceID != "web01" {
		t.Errorf("audit events = %+v, want one node.packageInventory.set event for web01", recorded)
	}
}

func TestSetReporting_NoopOmitsOutput(t *testing.T) {
	ft := &fakeTransport{respond: dispatchWireResponse{Result: &dispatchResult{
		PackageInventory: &packageInventoryResult{Enabled: true, Ran: false},
	}}}
	h := NewHandlers(&fakeClient{}, ft, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.setReporting(rec, reportingRequest(http.MethodPut, "web01", map[string]bool{"enabled": true}))

	var got reportingStatus
	decodeBody(t, rec, &got)
	if got.Output != nil {
		t.Errorf("Output = %+v, want nil - Ran was false (no-op toggle)", got.Output)
	}
}

func TestSetReporting_NodeOffline(t *testing.T) {
	ft := &fakeTransport{err: nodetransport.ErrNodeNotConnected}
	h := NewHandlers(&fakeClient{}, ft, noopRecordAudit, noopRecordAuditRead)

	rec := httptest.NewRecorder()
	h.setReporting(rec, reportingRequest(http.MethodPut, "web01", map[string]bool{"enabled": true}))

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (node offline), body: %s", rec.Code, rec.Body)
	}
}

func TestSetReporting_InvalidBodyIsBadRequest(t *testing.T) {
	h := NewHandlers(&fakeClient{}, &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/nodes/web01/packages/reporting", bytes.NewReader([]byte("not json")))
	req.SetPathValue("name", "web01")
	rec := httptest.NewRecorder()
	h.setReporting(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestRegister_ReportingPermissions confirms the status endpoint
// requires nodes:read (matching this capability's other read
// endpoints) and the set endpoint requires orchestrator:run (matching
// the permission that already gates other node-affecting dispatched
// actions - see design.md in add-package-inventory-toggle for why that
// permission, not a new one).
func TestRegister_ReportingPermissions(t *testing.T) {
	h := NewHandlers(&fakeClient{}, &fakeTransport{}, noopRecordAudit, noopRecordAuditRead)
	mux := http.NewServeMux()

	var order []string
	authorize := func(permission string, next http.HandlerFunc) http.HandlerFunc {
		order = append(order, permission)
		return next
	}
	h.Register(mux, authorize)

	want := []string{"nodes:read", "nodes:read", "nodes:read", "orchestrator:run"}
	if len(order) != len(want) {
		t.Fatalf("Register called authorize %d times, want %d: %v", len(order), len(want), order)
	}
	for i, w := range want {
		if order[i] != w {
			t.Errorf("authorize call %d permission = %q, want %q", i, order[i], w)
		}
	}
}
