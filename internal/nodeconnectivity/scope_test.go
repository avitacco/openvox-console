package nodeconnectivity

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
)

func TestListConnected_OnlyNodesInScope(t *testing.T) {
	registry := &fakeRegistry{connected: map[string]bool{"web01": true, "db01": true}}
	certs := &fakeCertStatusClient{statuses: map[string]string{
		"web01": "signed", "db01": "signed",
		// Pending requests from nodes that have never reported facts.
		"new-web": "requested", "new-db": "requested",
	}}
	h := NewHandlers(registry, certs, nil, noopRecordAudit, noopRecordAuditRead, nil)

	list := func() map[string]nodeStatus {
		rec := httptest.NewRecorder()
		h.listConnected(rec, httptest.NewRequest(http.MethodGet, "/api/v1/node-connectivity", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		var resp connectivityResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		byName := map[string]nodeStatus{}
		for _, n := range resp.Nodes {
			byName[n.Certname] = n
		}
		return byName
	}

	if all := list(); len(all) != 4 {
		t.Fatalf("unscoped: %d nodes, want all 4", len(all))
	}

	// new-web is in scope by being pinned into the caller's group; the
	// resolver, not this handler, decides that.
	h.SetScope(func(*http.Request) (nodescope.Scope, error) { return nodescope.Of("web01", "new-web"), nil })
	scoped := list()
	if len(scoped) != 2 {
		t.Fatalf("scoped: %v, want only web01 and new-web", scoped)
	}
	if !scoped["web01"].Connected || scoped["new-web"].CertStatus != "requested" {
		t.Errorf("in-scope rows lost their data: %+v", scoped)
	}
	for _, hidden := range []string{"db01", "new-db"} {
		if _, ok := scoped[hidden]; ok {
			t.Errorf("%s is outside scope but was listed", hidden)
		}
	}
}

func TestListConnected_ScopeErrorFailsTheRequest(t *testing.T) {
	h := NewHandlers(&fakeRegistry{connected: map[string]bool{"web01": true}}, nil, nil, noopRecordAudit, noopRecordAuditRead, nil)
	h.SetScope(func(*http.Request) (nodescope.Scope, error) { return nodescope.Scope{}, errors.New("openvoxdb down") })

	rec := httptest.NewRecorder()
	h.listConnected(rec, httptest.NewRequest(http.MethodGet, "/api/v1/node-connectivity", nil))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 rather than an unfiltered list", rec.Code)
	}
}

// Certificate actions are judged before the CA is asked anything.
func TestCertActions_NodeScope(t *testing.T) {
	actions := map[string]struct {
		call   func(h *Handlers) http.HandlerFunc
		method string
		done   func(f *fakeCertStatusClient) []string
	}{
		"sign":   {func(h *Handlers) http.HandlerFunc { return h.signCert }, http.MethodPost, func(f *fakeCertStatusClient) []string { return f.signedCertnames }},
		"revoke": {func(h *Handlers) http.HandlerFunc { return h.revokeCert }, http.MethodPost, func(f *fakeCertStatusClient) []string { return f.revokedCertnames }},
		"clean":  {func(h *Handlers) http.HandlerFunc { return h.cleanCert }, http.MethodDelete, func(f *fakeCertStatusClient) []string { return f.cleanedCertnames }},
	}
	for name, action := range actions {
		t.Run(name, func(t *testing.T) {
			fake := &fakeCertStatusClient{}
			audited := 0
			h := NewHandlers(nil, fake, nil, func(*http.Request, auditlog.Event) { audited++ }, noopRecordAuditRead, nil)
			// new-web has no facts; it is in scope because it is pinned
			// into the caller's group, which the resolver decides.
			h.SetScope(func(*http.Request) (nodescope.Scope, error) { return nodescope.Of("web01", "new-web"), nil })

			hidden := httptest.NewRecorder()
			action.call(h)(hidden, certActionRequest(action.method, "db01"))
			if hidden.Code != http.StatusForbidden || !strings.Contains(hidden.Body.String(), `"outOfScope":["db01"]`) {
				t.Fatalf("out of scope: %d %s, want 403 naming the node", hidden.Code, hidden.Body)
			}
			if len(action.done(fake)) != 0 || audited != 0 {
				t.Errorf("an out-of-scope %s reached the CA (%v) or the audit log (%d)", name, action.done(fake), audited)
			}

			unknown := httptest.NewRecorder()
			action.call(h)(unknown, certActionRequest(action.method, "nope"))
			if got := strings.ReplaceAll(unknown.Body.String(), "nope", "db01"); unknown.Code != hidden.Code || got != hidden.Body.String() {
				t.Errorf("unknown certname refused differently from a hidden one:\n hidden:  %s\n unknown: %s", hidden.Body, unknown.Body)
			}

			for _, certname := range []string{"web01", "new-web"} {
				rec := httptest.NewRecorder()
				action.call(h)(rec, certActionRequest(action.method, certname))
				if rec.Code != http.StatusNoContent {
					t.Errorf("in scope (%s): %d %s, want 204", certname, rec.Code, rec.Body)
				}
			}
			if got := action.done(fake); len(got) != 2 {
				t.Errorf("CA calls = %v, want the two in-scope certnames", got)
			}
		})
	}
}
