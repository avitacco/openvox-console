package reporting

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

func scopedTo(certnames ...string) nodescope.Func {
	return func(*http.Request) (nodescope.Scope, error) { return nodescope.Of(certnames...), nil }
}

func scopeClient() *fakeClient {
	now := time.Now()
	return &fakeClient{
		reports: map[string][]openvoxdb.Report{
			"web01": {{Hash: "web-report", Status: "failed", StartTime: now, EndTime: now}},
			"db01":  {{Hash: "db-report", Status: "failed", StartTime: now, EndTime: now}},
		},
		events: map[string][]openvoxdb.Event{
			"web-report": {{Certname: "web01", ResourceType: "File", ResourceTitle: "/etc/motd", Status: "failure", Timestamp: now}},
			"db-report":  {{Certname: "db01", ResourceType: "Service", ResourceTitle: "postgresql", Status: "failure", Timestamp: now}},
		},
	}
}

func get(h http.HandlerFunc, param, value string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetPathValue(param, value)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestReports_OutOfScopeNodeAnswersAsUnknown(t *testing.T) {
	scoped := NewHandlers(scopeClient(), noopRecordAuditRead)
	scoped.SetScope(scopedTo("web01"))
	unscoped := NewHandlers(scopeClient(), noopRecordAuditRead)

	hidden := get(scoped.listReports, "name", "db01")
	unknown := get(unscoped.listReports, "name", "no-such-node")
	if hidden.Code != unknown.Code || hidden.Body.String() != unknown.Body.String() {
		t.Errorf("out of scope: %d %q\nunknown:      %d %q", hidden.Code, hidden.Body.String(), unknown.Code, unknown.Body.String())
	}

	var visible []reportSummary
	decodeBody(t, get(scoped.listReports, "name", "web01"), &visible)
	if len(visible) != 1 || visible[0].Hash != "web-report" {
		t.Errorf("in-scope reports = %+v, want web-report", visible)
	}
}

// A report is addressed by hash, so knowing (or guessing) one must not
// get a caller the events of a node outside their scope.
func TestEvents_ReportOfOutOfScopeNodeAnswersAsUnknown(t *testing.T) {
	scoped := NewHandlers(scopeClient(), noopRecordAuditRead)
	scoped.SetScope(scopedTo("web01"))
	unscoped := NewHandlers(scopeClient(), noopRecordAuditRead)

	hidden := get(scoped.listEvents, "id", "db-report")
	unknown := get(unscoped.listEvents, "id", "no-such-report")
	if hidden.Code != unknown.Code || hidden.Body.String() != unknown.Body.String() {
		t.Errorf("out of scope: %d %q\nunknown:      %d %q", hidden.Code, hidden.Body.String(), unknown.Code, unknown.Body.String())
	}

	var visible []eventDetail
	decodeBody(t, get(scoped.listEvents, "id", "web-report"), &visible)
	if len(visible) != 1 || visible[0].ResourceTitle != "/etc/motd" {
		t.Errorf("in-scope events = %+v, want the /etc/motd event", visible)
	}
}

func TestReporting_FleetWideCallerIsUnchanged(t *testing.T) {
	h := NewHandlers(scopeClient(), noopRecordAuditRead)
	h.SetScope(func(*http.Request) (nodescope.Scope, error) { return nodescope.Everything(), nil })
	var events []eventDetail
	decodeBody(t, get(h.listEvents, "id", "db-report"), &events)
	if len(events) != 1 {
		t.Errorf("fleet-wide events = %+v, want 1", events)
	}
}
