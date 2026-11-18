package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func patchHandlers(t *testing.T, fleet *fakePatchFleet) *Handlers {
	t.Helper()
	h := realHandlers(t)
	c := NewPatchCoordinator(h.store, fleet, nil)
	c.pollEvery = 5_000_000
	h.SetPatches(c)
	t.Cleanup(c.Wait)
	return h
}

func TestTriggerPatch_Starts(t *testing.T) {
	fleet := &fakePatchFleet{nodes: map[string]*patchNodeScript{"ph-a": okNode()}}
	h := patchHandlers(t, fleet)
	rec := post(h.triggerPatch, map[string]any{"targets": []string{"ph-a"}, "options": map[string]any{"scope": "all", "batchSize": 3}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var job Job
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.Kind != JobKindPatch || job.PatchOptions == nil || job.PatchOptions.Scope != "all" || job.PatchOptions.BatchSize != 3 || job.TriggeredBy != "test-actor" {
		t.Errorf("job = %+v", job)
	}
}

func TestTriggerPatch_RefusedOptionsNameTheField(t *testing.T) {
	h := patchHandlers(t, &fakePatchFleet{})
	rec := post(h.triggerPatch, map[string]any{"targets": []string{"ph-b"}, "options": map[string]any{"batchSize": 0}})
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusBadRequest || body["field"] != "batchSize" {
		t.Errorf("status %d body %s", rec.Code, rec.Body)
	}
	if jobsNaming(t, h, "ph-b") != 0 {
		t.Error("a refused request created a job")
	}
}

func TestTriggerPatch_ANodeAlreadyBeingPatchedIsRefused(t *testing.T) {
	h := patchHandlers(t, &fakePatchFleet{})
	// A run that is open: created directly, not followed.
	c, d := uniq("ph-c"), uniq("ph-d")
	open, err := h.store.CreatePatchJob(context.Background(), PatchOptions{Scope: "all", BatchSize: 1}, []string{c}, "tester", JobOrigin{})
	if err != nil {
		t.Fatal(err)
	}
	closeAfter(t, h.store, open.ID)
	rec := post(h.triggerPatch, map[string]any{"targets": []string{c, d}})
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), c) {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if jobsNaming(t, h, d) != 0 {
		t.Error("the free node was started although the request was refused")
	}
}

func TestTriggerPatch_UnavailableWithoutACoordinator(t *testing.T) {
	h := realHandlers(t)
	rec := post(h.triggerPatch, map[string]any{"targets": []string{"ph-e"}})
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d: %s", rec.Code, rec.Body)
	}
}

func TestTriggerPatch_OutOfScopeCreatesNothing(t *testing.T) {
	h := patchHandlers(t, &fakePatchFleet{})
	h.SetScope(scopedTo("ph-in"))
	rec := post(h.triggerPatch, map[string]any{"targets": []string{"ph-in", "ph-out"}})
	if rec.Code != http.StatusForbidden || jobsNaming(t, h, "ph-in") != 0 {
		t.Errorf("status %d: %s", rec.Code, rec.Body)
	}
}

// A patch job is read like any other: a reader limited to some nodes sees
// only those nodes' results, and is told others are hidden.
func TestPatchJob_IsReadWithinTheCallersScope(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mine, theirs := uniq("psr-mine"), uniq("psr-theirs")
	job, err := s.CreatePatchJob(ctx, PatchOptions{Scope: "all", BatchSize: 1, StopAfterFailures: 3}, []string{mine, theirs}, "tester", JobOrigin{})
	if err != nil {
		t.Fatal(err)
	}
	closeAfter(t, s, job.ID)
	if err := s.RecordTaskResult(ctx, job.ID, theirs, StatusSucceeded, nil, "", "", json.RawMessage(`{"packages":[{"name":"secret-pkg","outcome":"upgraded"}]}`)); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetJobInScope(ctx, job.ID, []string{mine})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Targets) != 1 || got.Targets[0].Certname != mine || !got.TargetsHidden || got.PatchOptions == nil {
		t.Errorf("job = %+v", got)
	}
	if b, _ := json.Marshal(got); strings.Contains(string(b), theirs) || strings.Contains(string(b), "secret-pkg") {
		t.Errorf("a hidden node's result is in %s", b)
	}
	if _, err := s.GetJobInScope(ctx, job.ID, []string{"someone-else"}); err != ErrNotFound {
		t.Errorf("a reader with none of its nodes = %v, want ErrNotFound", err)
	}
	list, _, err := s.ListJobs(ctx, 1, 50, JobFilter{Kind: "patch", Certnames: []string{mine}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, j := range list {
		if j.ID == job.ID {
			found = true
			if j.Kind != JobKindPatch || len(j.TargetPreview) != 1 || j.TargetPreview[0] != mine {
				t.Errorf("listed = %+v", j)
			}
		}
	}
	if !found {
		t.Error("the patch job is not in the list")
	}
}
