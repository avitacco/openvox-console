package agentupgrade

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
)

type fakeUpgrader struct {
	err   error
	calls []string
}

func (f *fakeUpgrader) UpgradeNow(_ context.Context, certname string) error {
	f.calls = append(f.calls, certname)
	return f.err
}

// serve registers the handlers behind an authorize that grants only the
// listed permissions, and performs one upgrade request.
func serve(t *testing.T, up *fakeUpgrader, granted ...string) (*httptest.ResponseRecorder, []auditlog.Event) {
	t.Helper()
	var audited []auditlog.Event
	h := NewHandlers(up, func(_ *http.Request, e auditlog.Event) { audited = append(audited, e) }, nil)
	mux := http.NewServeMux()
	h.Register(mux, func(permission string, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			for _, g := range granted {
				if g == permission {
					next(w, r)
					return
				}
			}
			http.Error(w, "forbidden", http.StatusForbidden)
		}
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/nodes/web01.example.com/agent-upgrade", nil))
	return rec, audited
}

func TestUpgradeEndpoint_Accepted(t *testing.T) {
	up := &fakeUpgrader{}
	rec, audited := serve(t, up, "nodes:manage")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body)
	}
	if len(up.calls) != 1 || up.calls[0] != "web01.example.com" {
		t.Errorf("UpgradeNow calls = %v", up.calls)
	}
	if len(audited) != 1 || audited[0].Action != "node.agent.upgrade_requested" || audited[0].ResourceID != "web01.example.com" {
		t.Errorf("audit = %+v", audited)
	}
}

func TestUpgradeEndpoint_RequiresNodeManagement(t *testing.T) {
	up := &fakeUpgrader{}
	rec, audited := serve(t, up, "nodes:read", "nodes:certs:manage")

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if len(up.calls) != 0 {
		t.Error("an upgrade was started for a user without nodes:manage")
	}
	if len(audited) != 0 {
		t.Error("a refused request was audited as an upgrade")
	}
}

func TestUpgradeEndpoint_Refusals(t *testing.T) {
	for _, c := range []struct {
		err    error
		reason string
	}{
		{ErrNotConnected, "not-connected"},
		{ErrBusy, "busy"},
		{ErrCurrent, "current"},
		{ErrInProgress, "in-progress"},
		{&CannotUpgradeError{Blocked: BlockedInstallScript}, "install-script"},
		{&CannotUpgradeError{Blocked: BlockedDeclined}, "declined"},
		{&CannotUpgradeError{Blocked: BlockedNoArtifact}, "no-artifact"},
	} {
		rec, audited := serve(t, &fakeUpgrader{err: c.err}, "nodes:manage")
		if rec.Code != http.StatusConflict {
			t.Errorf("%s: status = %d, want 409", c.reason, rec.Code)
		}
		var body upgradeRefused
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: %v", c.reason, err)
		}
		// The message has to say which case applies, in words.
		if body.Reason != c.reason || body.Error != c.err.Error() || body.Error == "" {
			t.Errorf("%s: body = %+v", c.reason, body)
		}
		if len(audited) != 0 {
			t.Errorf("%s: a refused upgrade was audited", c.reason)
		}
	}
}

func TestUpgradeEndpoint_UnexpectedFailure(t *testing.T) {
	rec, audited := serve(t, &fakeUpgrader{err: errors.New("database is down")}, "nodes:manage")
	if rec.Code != http.StatusInternalServerError || len(audited) != 0 {
		t.Errorf("status = %d, audited = %v", rec.Code, audited)
	}
}

func TestView(t *testing.T) {
	catalog := stampedCatalog()

	if v := View(nil, catalog); v.State != StateUnreported || v.OutOfDate || v.CanUpgrade || v.Upgrade != nil {
		t.Errorf("no record: %+v", v)
	}

	current := reported("console-build", at(0))
	current.LastError, current.UpgradeState, current.TargetDigest = "an old failure", UpgradeFailed, "some-earlier-build"
	if v := View(current, catalog); v.State != StateCurrent || v.Upgrade != nil || v.CanUpgrade {
		t.Errorf("current node: %+v", v)
	}

	outdated := reported("old", at(-1))
	outdated.Version = "v1"
	v := View(outdated, catalog)
	if v.State != StateOutdated || !v.OutOfDate || !v.CanUpgrade || v.TargetVersion != "v2" || v.Version != "v1" || v.Upgrade != nil {
		t.Errorf("outdated node: %+v", v)
	}

	inProgress := reported("old", at(-1))
	inProgress.UpgradeState, inProgress.TargetDigest = UpgradeRequested, "console-build"
	v = View(inProgress, catalog)
	if v.CanUpgrade || v.Upgrade == nil || v.Upgrade.State != UpgradeRequested {
		t.Errorf("upgrade in progress: %+v", v)
	}

	next := consoleBuilt
	failed := reported("old", at(-1))
	failed.UpgradeState, failed.TargetDigest, failed.LastError, failed.Attempts, failed.NextAttemptAt = UpgradeFailed, "console-build", "digest mismatch", 2, &next
	v = View(failed, catalog)
	if !v.CanUpgrade || v.Upgrade == nil || v.Upgrade.Error != "digest mismatch" || v.Upgrade.Attempts != 2 ||
		v.Upgrade.MaxAttempts != MaxAttempts || v.Upgrade.NextAttemptAt == nil {
		t.Errorf("failed upgrade: %+v / %+v", v, v.Upgrade)
	}

	// A failure against a build this console no longer serves is not
	// shown as the state of the upgrade it would make now.
	failed.TargetDigest = "a-build-from-the-previous-console"
	if v = View(failed, catalog); v.Upgrade != nil || !v.CanUpgrade {
		t.Errorf("stale failure: %+v", v)
	}

	old := &Record{Certname: "n1", Capable: false}
	v = View(old, catalog)
	if v.State != StateUnknown || !v.OutOfDate || v.CanUpgrade || v.Blocked != BlockedInstallScript || v.Version != "" {
		t.Errorf("client that cannot report: %+v", v)
	}
}
