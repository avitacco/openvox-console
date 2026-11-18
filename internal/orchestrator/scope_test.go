package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
)

const scopeTarget = "handlers-test-target.example.com"

func scopedTo(certnames ...string) nodescope.Func {
	return func(*http.Request) (nodescope.Scope, error) { return nodescope.Of(certnames...), nil }
}

// jobsNaming counts the jobs that have certname among their targets.
func jobsNaming(t *testing.T, h *Handlers, certname string) int {
	t.Helper()
	_, total, err := h.store.ListJobs(context.Background(), 1, 1, JobFilter{Target: certname})
	if err != nil {
		t.Fatalf("ListJobs() error: %v", err)
	}
	return total
}

func post(h http.HandlerFunc, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(data)))
	return rec
}

func refusal(t *testing.T, rec *httptest.ResponseRecorder) nodescope.Refusal {
	t.Helper()
	var r nodescope.Refusal
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode refusal %q: %v", rec.Body.String(), err)
	}
	return r
}

// Each trigger, given one target in scope and one outside it, refuses
// the whole request and creates nothing - not even for the node the
// caller was allowed to act on.
func TestTriggers_MixedTargetsAreRefusedWholeAndCreateNothing(t *testing.T) {
	h := realHandlers(t)
	h.SetScope(scopedTo(scopeTarget))
	const outside = "scope-test-outside.example.com"
	targets := []string{scopeTarget, outside}

	triggers := map[string]func() *httptest.ResponseRecorder{
		"run": func() *httptest.ResponseRecorder { return post(h.triggerRun, triggerRunRequest{Targets: targets}) },
		"task": func() *httptest.ResponseRecorder {
			return post(h.triggerTask, triggerTaskRequest{Targets: targets, Task: "demo::echo"})
		},
		"patch": func() *httptest.ResponseRecorder { return post(h.triggerPatch, triggerPatchRequest{Targets: targets}) },
		"plan": func() *httptest.ResponseRecorder {
			return post(h.triggerPlan, triggerPlanRequest{Targets: targets, Name: "scope-plan", Steps: []PlanStep{{Kind: JobKindRun}}})
		},
	}
	for name, trigger := range triggers {
		t.Run(name, func(t *testing.T) {
			before := jobsNaming(t, h, outside)
			beforeAllowed := jobsNaming(t, h, scopeTarget)

			rec := trigger()
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body)
			}
			if got := refusal(t, rec).OutOfScope; !reflect.DeepEqual(got, []string{outside}) {
				t.Errorf("outOfScope = %v, want only %s", got, outside)
			}
			if after := jobsNaming(t, h, outside); after != before {
				t.Errorf("a job naming the out-of-scope node was created (%d -> %d)", before, after)
			}
			if after := jobsNaming(t, h, scopeTarget); after != beforeAllowed {
				t.Errorf("a job was created for the in-scope node of a refused request (%d -> %d)", beforeAllowed, after)
			}
		})
	}
}

func TestTriggerRun_InScopeProceeds(t *testing.T) {
	h := realHandlers(t)
	h.SetScope(scopedTo(scopeTarget))
	before := jobsNaming(t, h, scopeTarget)

	rec := post(h.triggerRun, triggerRunRequest{Targets: []string{scopeTarget}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if after := jobsNaming(t, h, scopeTarget); after != before+1 {
		t.Errorf("jobs for the in-scope node: %d -> %d, want one more", before, after)
	}
}

// A node outside the caller's groups and a name no node has are refused
// alike: nothing but the echoed name differs.
func TestTriggerRun_HiddenAndUnknownNodesAreRefusedIdentically(t *testing.T) {
	h := realHandlers(t)
	h.SetScope(scopedTo(scopeTarget))

	hidden := post(h.triggerRun, triggerRunRequest{Targets: []string{"real-but-hidden.example.com"}})
	unknown := post(h.triggerRun, triggerRunRequest{Targets: []string{"no-such-node.example.com"}})
	if hidden.Code != unknown.Code {
		t.Fatalf("status %d vs %d", hidden.Code, unknown.Code)
	}
	normalised := strings.ReplaceAll(unknown.Body.String(), "no-such-node.example.com", "real-but-hidden.example.com")
	if hidden.Body.String() != normalised {
		t.Errorf("refusals differ beyond the name:\n hidden:  %s\n unknown: %s", hidden.Body, unknown.Body)
	}
}

func TestTriggerRun_ScopeErrorIsNeverTreatedAsAllowed(t *testing.T) {
	h := realHandlers(t)
	h.SetScope(func(*http.Request) (nodescope.Scope, error) { return nodescope.Scope{}, errors.New("openvoxdb down") })
	before := jobsNaming(t, h, scopeTarget)

	rec := post(h.triggerRun, triggerRunRequest{Targets: []string{scopeTarget}})
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	if after := jobsNaming(t, h, scopeTarget); after != before {
		t.Error("a job was created although the caller's scope could not be determined")
	}
}
