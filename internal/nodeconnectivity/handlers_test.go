package nodeconnectivity

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
)

type fakeRegistry struct {
	connected map[string]bool
	history   map[string][2]*time.Time // [lastConnected, lastDisconnected]
}

func (f *fakeRegistry) KnownCertnames() []string {
	seen := map[string]struct{}{}
	for c := range f.connected {
		seen[c] = struct{}{}
	}
	for c := range f.history {
		seen[c] = struct{}{}
	}
	certnames := make([]string, 0, len(seen))
	for c := range seen {
		certnames = append(certnames, c)
	}
	return certnames
}

func (f *fakeRegistry) Lookup(certname string) bool { return f.connected[certname] }

func (f *fakeRegistry) LastConnected(certname string) *time.Time { return f.history[certname][0] }

func (f *fakeRegistry) LastDisconnected(certname string) *time.Time { return f.history[certname][1] }

type fakeCertStatusClient struct {
	statuses map[string]string
	err      error

	signErr, revokeErr, cleanErr                        error
	signedCertnames, revokedCertnames, cleanedCertnames []string
}

func (f *fakeCertStatusClient) Statuses(_ context.Context) (map[string]string, error) {
	return f.statuses, f.err
}

func (f *fakeCertStatusClient) Sign(_ context.Context, certname string) error {
	if f.signErr != nil {
		return f.signErr
	}
	f.signedCertnames = append(f.signedCertnames, certname)
	return nil
}

func (f *fakeCertStatusClient) Revoke(_ context.Context, certname string) error {
	if f.revokeErr != nil {
		return f.revokeErr
	}
	f.revokedCertnames = append(f.revokedCertnames, certname)
	return nil
}

func (f *fakeCertStatusClient) Clean(_ context.Context, certname string) error {
	if f.cleanErr != nil {
		return f.cleanErr
	}
	f.cleanedCertnames = append(f.cleanedCertnames, certname)
	return nil
}

func noopRecordAuditRead(_ *http.Request, _ auditlog.Event) {}

func noopRecordAudit(_ *http.Request, _ auditlog.Event) {}

func TestListConnected_EmptySet(t *testing.T) {
	h := NewHandlers(&fakeRegistry{}, nil, nil, noopRecordAudit, noopRecordAuditRead, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/node-connectivity", nil)
	rec := httptest.NewRecorder()
	h.listConnected(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var resp connectivityResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 0 {
		t.Errorf("Nodes = %v, want empty", resp.Nodes)
	}
}

func TestListConnected_PopulatedSet(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Hour)
	h := NewHandlers(&fakeRegistry{
		connected: map[string]bool{"web01.example.com": true},
		history: map[string][2]*time.Time{
			"web01.example.com": {&now, nil},
			"web02.example.com": {&earlier, &now},
		},
	}, nil, nil, noopRecordAudit, noopRecordAuditRead, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/node-connectivity", nil)
	rec := httptest.NewRecorder()
	h.listConnected(rec, req)

	var resp connectivityResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byName := map[string]nodeStatus{}
	for _, n := range resp.Nodes {
		byName[n.Certname] = n
	}

	web01, ok := byName["web01.example.com"]
	if !ok {
		t.Fatalf("web01.example.com missing from response: %+v", resp.Nodes)
	}
	if !web01.Connected {
		t.Error("web01.example.com Connected = false, want true")
	}
	if web01.LastConnected == nil || !web01.LastConnected.Equal(now) {
		t.Errorf("web01.example.com LastConnected = %v, want %v", web01.LastConnected, now)
	}
	if web01.LastDisconnected != nil {
		t.Errorf("web01.example.com LastDisconnected = %v, want nil", web01.LastDisconnected)
	}
	if web01.CertStatus != certStatusUnknown {
		t.Errorf("web01.example.com CertStatus = %q, want %q (no cert status client configured)", web01.CertStatus, certStatusUnknown)
	}

	web02, ok := byName["web02.example.com"]
	if !ok {
		t.Fatalf("web02.example.com missing from response: %+v", resp.Nodes)
	}
	if web02.Connected {
		t.Error("web02.example.com Connected = true, want false")
	}
	if web02.LastDisconnected == nil || !web02.LastDisconnected.Equal(now) {
		t.Errorf("web02.example.com LastDisconnected = %v, want %v", web02.LastDisconnected, now)
	}
}

func TestListConnected_NilRegistryReturnsEmptySet(t *testing.T) {
	h := NewHandlers(nil, nil, nil, noopRecordAudit, noopRecordAuditRead, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/node-connectivity", nil)
	rec := httptest.NewRecorder()
	h.listConnected(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var resp connectivityResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 0 {
		t.Errorf("Nodes = %v, want empty when registry is nil", resp.Nodes)
	}
}

func TestListConnected_CertStatusPopulated(t *testing.T) {
	h := NewHandlers(
		&fakeRegistry{connected: map[string]bool{"web01.example.com": true}},
		&fakeCertStatusClient{statuses: map[string]string{"web01.example.com": "signed"}},
		nil,
		noopRecordAudit, noopRecordAuditRead, nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/node-connectivity", nil)
	rec := httptest.NewRecorder()
	h.listConnected(rec, req)

	var resp connectivityResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 1 || resp.Nodes[0].CertStatus != "signed" {
		t.Errorf("Nodes = %+v, want a single node with CertStatus \"signed\"", resp.Nodes)
	}
}

// TestListConnected_CertStatusOnlyNodeStillAppears is the core reason
// the response's certname universe is a union, not just the registry's
// - see handlers.go's comment. A node whose cert was never signed never
// connects either, so it would otherwise be completely invisible in
// this response, exactly when its (non-"signed") status is most useful.
func TestListConnected_CertStatusOnlyNodeStillAppears(t *testing.T) {
	h := NewHandlers(
		&fakeRegistry{}, // no connectivity history for this node at all
		&fakeCertStatusClient{statuses: map[string]string{"never-connected.example.com": "requested"}},
		nil,
		noopRecordAudit, noopRecordAuditRead, nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/node-connectivity", nil)
	rec := httptest.NewRecorder()
	h.listConnected(rec, req)

	var resp connectivityResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("Nodes = %+v, want exactly one node", resp.Nodes)
	}
	n := resp.Nodes[0]
	if n.Certname != "never-connected.example.com" || n.CertStatus != "requested" {
		t.Errorf("Nodes[0] = %+v, want certname never-connected.example.com with CertStatus \"requested\"", n)
	}
	if n.Connected {
		t.Error("Connected = true, want false (never seen by the registry)")
	}
}

func TestListConnected_CertStatusErrorFallsBackToUnknown(t *testing.T) {
	h := NewHandlers(
		&fakeRegistry{connected: map[string]bool{"web01.example.com": true}},
		&fakeCertStatusClient{err: errors.New("openvoxserver unreachable")},
		nil,
		noopRecordAudit, noopRecordAuditRead, nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/node-connectivity", nil)
	rec := httptest.NewRecorder()
	h.listConnected(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (a CA outage should not fail connectivity reporting); body: %s", rec.Code, rec.Body)
	}
	var resp connectivityResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 1 || resp.Nodes[0].CertStatus != certStatusUnknown {
		t.Errorf("Nodes = %+v, want a single node with CertStatus %q", resp.Nodes, certStatusUnknown)
	}
	if !resp.Nodes[0].Connected {
		t.Error("Connected = false, want true (connectivity data should be unaffected by the cert status error)")
	}
}

type fakeInfrastructureCertnames struct {
	reasons map[string]string
}

func (f *fakeInfrastructureCertnames) Lookup(certname string) (string, bool) {
	reason, ok := f.reasons[certname]
	return reason, ok
}

func TestListConnected_InfrastructureCertFlaggedWithReason(t *testing.T) {
	h := NewHandlers(
		&fakeRegistry{connected: map[string]bool{"openvoxdb": false}},
		nil,
		&fakeInfrastructureCertnames{reasons: map[string]string{"openvoxdb": "openvoxdb's own server certificate"}},
		noopRecordAudit, noopRecordAuditRead, nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/node-connectivity", nil)
	rec := httptest.NewRecorder()
	h.listConnected(rec, req)

	var resp connectivityResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("Nodes = %+v, want exactly one node", resp.Nodes)
	}
	n := resp.Nodes[0]
	if !n.IsInfrastructure {
		t.Error("IsInfrastructure = false, want true")
	}
	if n.InfrastructureReason != "openvoxdb's own server certificate" {
		t.Errorf("InfrastructureReason = %q, want %q", n.InfrastructureReason, "openvoxdb's own server certificate")
	}
}

func TestListConnected_ManagedNodeNotFlaggedAsInfrastructure(t *testing.T) {
	h := NewHandlers(
		&fakeRegistry{connected: map[string]bool{"web01.example.com": true}},
		nil,
		&fakeInfrastructureCertnames{reasons: map[string]string{"openvoxdb": "openvoxdb's own server certificate"}},
		noopRecordAudit, noopRecordAuditRead, nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/node-connectivity", nil)
	rec := httptest.NewRecorder()
	h.listConnected(rec, req)

	var resp connectivityResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("Nodes = %+v, want exactly one node", resp.Nodes)
	}
	n := resp.Nodes[0]
	if n.IsInfrastructure {
		t.Error("IsInfrastructure = true, want false for a managed node never added to the infrastructure set")
	}
	if n.InfrastructureReason != "" {
		t.Errorf("InfrastructureReason = %q, want empty", n.InfrastructureReason)
	}
}

func TestListConnected_NilInfrastructureCertnamesNeverFlags(t *testing.T) {
	h := NewHandlers(
		&fakeRegistry{connected: map[string]bool{"web01.example.com": true}},
		nil,
		nil,
		noopRecordAudit, noopRecordAuditRead, nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/node-connectivity", nil)
	rec := httptest.NewRecorder()
	h.listConnected(rec, req)

	var resp connectivityResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 1 || resp.Nodes[0].IsInfrastructure {
		t.Errorf("Nodes = %+v, want a single node with IsInfrastructure = false", resp.Nodes)
	}
}

// certActionRequest builds a request for one of the sign/revoke/clean
// endpoints, with certname already set as a path value the same way
// http.ServeMux would after matching "/api/v1/nodes/{certname}/cert/...".
func certActionRequest(method, certname string) *http.Request {
	req := httptest.NewRequest(method, "/api/v1/nodes/"+certname+"/cert", nil)
	req.SetPathValue("certname", certname)
	return req
}

func TestSignCert_Success(t *testing.T) {
	fake := &fakeCertStatusClient{}
	var audited []auditlog.Event
	h := NewHandlers(nil, fake, nil, func(_ *http.Request, e auditlog.Event) { audited = append(audited, e) }, noopRecordAuditRead, nil)

	rec := httptest.NewRecorder()
	h.signCert(rec, certActionRequest(http.MethodPost, "web01.example.com"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", rec.Code, rec.Body)
	}
	if len(fake.signedCertnames) != 1 || fake.signedCertnames[0] != "web01.example.com" {
		t.Errorf("signedCertnames = %v, want [web01.example.com]", fake.signedCertnames)
	}
	if len(audited) != 1 || audited[0].Action != "node.certificate.signed" || audited[0].ResourceID != "web01.example.com" {
		t.Errorf("audited = %+v, want a single node.certificate.signed event for web01.example.com", audited)
	}
}

func TestRevokeCert_Success(t *testing.T) {
	fake := &fakeCertStatusClient{}
	var audited []auditlog.Event
	h := NewHandlers(nil, fake, nil, func(_ *http.Request, e auditlog.Event) { audited = append(audited, e) }, noopRecordAuditRead, nil)

	rec := httptest.NewRecorder()
	h.revokeCert(rec, certActionRequest(http.MethodPost, "web01.example.com"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", rec.Code, rec.Body)
	}
	if len(fake.revokedCertnames) != 1 || fake.revokedCertnames[0] != "web01.example.com" {
		t.Errorf("revokedCertnames = %v, want [web01.example.com]", fake.revokedCertnames)
	}
	if len(audited) != 1 || audited[0].Action != "node.certificate.revoked" {
		t.Errorf("audited = %+v, want a single node.certificate.revoked event", audited)
	}
}

func TestCleanCert_Success(t *testing.T) {
	fake := &fakeCertStatusClient{}
	var audited []auditlog.Event
	h := NewHandlers(nil, fake, nil, func(_ *http.Request, e auditlog.Event) { audited = append(audited, e) }, noopRecordAuditRead, nil)

	rec := httptest.NewRecorder()
	h.cleanCert(rec, certActionRequest(http.MethodDelete, "web01.example.com"))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body: %s", rec.Code, rec.Body)
	}
	if len(fake.cleanedCertnames) != 1 || fake.cleanedCertnames[0] != "web01.example.com" {
		t.Errorf("cleanedCertnames = %v, want [web01.example.com]", fake.cleanedCertnames)
	}
	if len(audited) != 1 || audited[0].Action != "node.certificate.cleaned" {
		t.Errorf("audited = %+v, want a single node.certificate.cleaned event", audited)
	}
}

func TestSignCert_WrongStateRejectedAndNotAudited(t *testing.T) {
	fake := &fakeCertStatusClient{signErr: &certstatus.WrongStateError{Certname: "web01.example.com", CurrentState: "signed", RequiredState: "requested"}}
	var audited []auditlog.Event
	h := NewHandlers(nil, fake, nil, func(_ *http.Request, e auditlog.Event) { audited = append(audited, e) }, noopRecordAuditRead, nil)

	rec := httptest.NewRecorder()
	h.signCert(rec, certActionRequest(http.MethodPost, "web01.example.com"))

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
	if len(audited) != 0 {
		t.Errorf("audited = %+v, want no audit entry for a rejected action", audited)
	}
}

func TestRevokeCert_UnknownCertnameRejectedAndNotAudited(t *testing.T) {
	fake := &fakeCertStatusClient{revokeErr: &certstatus.NotFoundError{Certname: "ghost.example.com"}}
	var audited []auditlog.Event
	h := NewHandlers(nil, fake, nil, func(_ *http.Request, e auditlog.Event) { audited = append(audited, e) }, noopRecordAuditRead, nil)

	rec := httptest.NewRecorder()
	h.revokeCert(rec, certActionRequest(http.MethodPost, "ghost.example.com"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if len(audited) != 0 {
		t.Errorf("audited = %+v, want no audit entry for a rejected action", audited)
	}
}

func TestCertActions_NotConfiguredRejectedAndNotAudited(t *testing.T) {
	var audited []auditlog.Event
	recordAudit := func(_ *http.Request, e auditlog.Event) { audited = append(audited, e) }
	h := NewHandlers(nil, nil, nil, recordAudit, noopRecordAuditRead, nil)

	tests := []struct {
		name   string
		method string
		call   func(w http.ResponseWriter, r *http.Request)
	}{
		{"sign", http.MethodPost, h.signCert},
		{"revoke", http.MethodPost, h.revokeCert},
		{"clean", http.MethodDelete, h.cleanCert},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tt.call(rec, certActionRequest(tt.method, "web01.example.com"))
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want 503 when certificate management is not configured", rec.Code)
			}
		})
	}
	if len(audited) != 0 {
		t.Errorf("audited = %+v, want no audit entries when not configured", audited)
	}
}
