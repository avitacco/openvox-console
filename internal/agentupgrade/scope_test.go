package agentupgrade

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
)

func upgradeAs(scope nodescope.Func, up *fakeUpgrader, certname string) (*httptest.ResponseRecorder, int) {
	audited := 0
	h := NewHandlers(up, func(*http.Request, auditlog.Event) { audited++ }, nil)
	if scope != nil {
		h.SetScope(scope)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+certname+"/agent-upgrade", nil)
	req.SetPathValue("name", certname)
	rec := httptest.NewRecorder()
	h.upgrade(rec, req)
	return rec, audited
}

func TestUpgradeEndpoint_NodeScope(t *testing.T) {
	scoped := func(*http.Request) (nodescope.Scope, error) { return nodescope.Of("web01.example.com"), nil }

	up := &fakeUpgrader{}
	rec, audited := upgradeAs(scoped, up, "db01.example.com")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"outOfScope":["db01.example.com"]`) {
		t.Errorf("out of scope: %d %s, want 403 naming the node", rec.Code, rec.Body)
	}
	if len(up.calls) != 0 || audited != 0 {
		t.Errorf("an out-of-scope upgrade reached the coordinator (%v) or the audit log (%d)", up.calls, audited)
	}

	unknown, _ := upgradeAs(scoped, &fakeUpgrader{}, "no-such.example.com")
	if got := strings.ReplaceAll(unknown.Body.String(), "no-such.example.com", "db01.example.com"); unknown.Code != rec.Code || got != rec.Body.String() {
		t.Errorf("unknown node refused differently from a hidden one:\n hidden:  %s\n unknown: %s", rec.Body, unknown.Body)
	}

	up = &fakeUpgrader{}
	if rec, _ := upgradeAs(scoped, up, "web01.example.com"); rec.Code != http.StatusAccepted || len(up.calls) != 1 {
		t.Errorf("in scope: %d, calls %v; want 202 and one upgrade", rec.Code, up.calls)
	}

	up = &fakeUpgrader{}
	if rec, _ := upgradeAs(nil, up, "db01.example.com"); rec.Code != http.StatusAccepted {
		t.Errorf("fleet-wide caller: %d, want 202 as before", rec.Code)
	}
}
