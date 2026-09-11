package reporting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

func noopRecordAuditRead(_ *http.Request, _ auditlog.Event) {}

type fakeClient struct {
	reports map[string][]openvoxdb.Report
	events  map[string][]openvoxdb.Event
}

func (f *fakeClient) Reports(_ context.Context, certname string) ([]openvoxdb.Report, error) {
	return f.reports[certname], nil
}

func (f *fakeClient) Events(_ context.Context, reportHash string) ([]openvoxdb.Event, error) {
	return f.events[reportHash], nil
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

func TestListReports_ReturnsAll(t *testing.T) {
	now := time.Now()
	client := &fakeClient{reports: map[string][]openvoxdb.Report{
		"web01": {
			{Hash: "aaa", Status: "success", StartTime: now, EndTime: now},
			{Hash: "bbb", Status: "failed", StartTime: now, EndTime: now},
		},
	}}
	h := NewHandlers(client, noopRecordAuditRead)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/web01/reports", nil)
	req.SetPathValue("name", "web01")
	rec := httptest.NewRecorder()
	h.listReports(rec, req)

	var reports []reportSummary
	decodeBody(t, rec, &reports)
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want 2", len(reports))
	}
}

func TestListReports_EmptyIsEmptyArrayNotError(t *testing.T) {
	h := NewHandlers(&fakeClient{}, noopRecordAuditRead)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/nonexistent/reports", nil)
	req.SetPathValue("name", "nonexistent")
	rec := httptest.NewRecorder()
	h.listReports(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "[]\n" {
		t.Errorf("body = %q, want an empty JSON array", body)
	}
}

func TestListReports_FilterByStatus(t *testing.T) {
	now := time.Now()
	client := &fakeClient{reports: map[string][]openvoxdb.Report{
		"web01": {
			{Hash: "aaa", Status: "success", StartTime: now, EndTime: now},
			{Hash: "bbb", Status: "failed", StartTime: now, EndTime: now},
		},
	}}
	h := NewHandlers(client, noopRecordAuditRead)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/web01/reports?status=failed", nil)
	req.SetPathValue("name", "web01")
	rec := httptest.NewRecorder()
	h.listReports(rec, req)

	var reports []reportSummary
	decodeBody(t, rec, &reports)
	if len(reports) != 1 || reports[0].Hash != "bbb" {
		t.Errorf("got %+v, want only report bbb", reports)
	}
}

func TestListEvents_FilterByStatus(t *testing.T) {
	now := time.Now()
	client := &fakeClient{events: map[string][]openvoxdb.Event{
		"aaa": {
			{ResourceType: "File", ResourceTitle: "/tmp/a", Status: "success", Timestamp: now},
			{ResourceType: "File", ResourceTitle: "/tmp/b", Status: "failure", Timestamp: now},
		},
	}}
	h := NewHandlers(client, noopRecordAuditRead)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/aaa/events?status=failure", nil)
	req.SetPathValue("id", "aaa")
	rec := httptest.NewRecorder()
	h.listEvents(rec, req)

	var events []eventDetail
	decodeBody(t, rec, &events)
	if len(events) != 1 || events[0].ResourceTitle != "/tmp/b" {
		t.Errorf("got %+v, want only the failure event", events)
	}
}

func TestReporting_ReadHandlers_RecordReadAuditEvents(t *testing.T) {
	now := time.Now()
	client := &fakeClient{
		reports: map[string][]openvoxdb.Report{"web01": {{Hash: "aaa", Status: "success", StartTime: now, EndTime: now}}},
		events:  map[string][]openvoxdb.Event{"aaa": {{ResourceType: "File", ResourceTitle: "/tmp/a", Status: "success", Timestamp: now}}},
	}
	var recorded []auditlog.Event
	h := NewHandlers(client, func(_ *http.Request, e auditlog.Event) { recorded = append(recorded, e) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/web01/reports", nil)
	req.SetPathValue("name", "web01")
	h.listReports(httptest.NewRecorder(), req)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/reports/aaa/events", nil)
	req.SetPathValue("id", "aaa")
	h.listEvents(httptest.NewRecorder(), req)

	if len(recorded) != 2 {
		t.Fatalf("recorded %d read audit events, want 2: %+v", len(recorded), recorded)
	}
	if recorded[0].Action != "report.list.viewed" || recorded[0].ResourceID != "web01" {
		t.Errorf("event 0 = %+v, want action=report.list.viewed resourceId=web01", recorded[0])
	}
	if recorded[1].Action != "report.events.viewed" || recorded[1].ResourceID != "aaa" {
		t.Errorf("event 1 = %+v, want action=report.events.viewed resourceId=aaa", recorded[1])
	}
}
