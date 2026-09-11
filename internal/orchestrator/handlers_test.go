package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

func noopActor(*http.Request) string { return "test-actor" }

func noopRecordAuditRead(_ *http.Request, _ auditlog.Event) {}

// realHandlers builds Handlers wired to a real Postgres store and a
// real transport+dispatcher, mirroring codemanager/handlers_test.go's
// realHandlers pattern - skips if CONSOLE_TEST_POSTGRES_DSN isn't set.
func realHandlers(t *testing.T) *Handlers {
	t.Helper()
	store := testStore(t)
	broker, stopAgent := startRealAgent(t, "handlers-test-target.example.com", fakeRunner("ok", 0))
	t.Cleanup(stopAgent)
	dispatcher := NewDispatcher(store, broker, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go dispatcher.Run(ctx)
	return NewHandlers(store, dispatcher, noopActor, noopRecordAuditRead)
}

func TestTriggerRun_Success(t *testing.T) {
	h := realHandlers(t)
	body, _ := json.Marshal(triggerRunRequest{Targets: []string{"handlers-test-target.example.com"}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/runs", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.triggerRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var job Job
	if err := json.NewDecoder(rec.Body).Decode(&job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if job.ID == 0 {
		t.Error("expected an assigned job ID")
	}
	if job.TriggeredBy != "test-actor" {
		t.Errorf("TriggeredBy = %q, want test-actor", job.TriggeredBy)
	}
}

func TestTriggerRun_NoTargetsReturns400(t *testing.T) {
	h := realHandlers(t)
	body, _ := json.Marshal(triggerRunRequest{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/runs", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.triggerRun(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestTriggerTask_Success(t *testing.T) {
	h := realHandlers(t)
	body, _ := json.Marshal(triggerTaskRequest{
		Targets: []string{"handlers-test-target.example.com"},
		Task:    "package",
		Params:  json.RawMessage(`{"name":"nginx"}`),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/tasks", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.triggerTask(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var job Job
	if err := json.NewDecoder(rec.Body).Decode(&job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if job.TaskName != "package" {
		t.Errorf("TaskName = %q, want package", job.TaskName)
	}
}

func TestTriggerTask_MissingTaskNameReturns400(t *testing.T) {
	h := realHandlers(t)
	body, _ := json.Marshal(triggerTaskRequest{Targets: []string{"handlers-test-target.example.com"}})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/tasks", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.triggerTask(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestTriggerPlan_Success(t *testing.T) {
	h := realHandlers(t)
	body, _ := json.Marshal(triggerPlanRequest{
		Targets: []string{"handlers-test-target.example.com"},
		Name:    "my-plan",
		Steps:   []PlanStep{{Kind: JobKindRun}},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/plans", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.triggerPlan(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var job Job
	if err := json.NewDecoder(rec.Body).Decode(&job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if job.Kind != JobKindPlan {
		t.Errorf("Kind = %q, want %q", job.Kind, JobKindPlan)
	}
}

func TestTriggerPlan_NoStepsReturns400(t *testing.T) {
	h := realHandlers(t)
	body, _ := json.Marshal(triggerPlanRequest{Targets: []string{"handlers-test-target.example.com"}, Name: "empty"})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/plans", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.triggerPlan(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestListJobs_ReturnsRecent(t *testing.T) {
	h := realHandlers(t)
	body, _ := json.Marshal(triggerRunRequest{Targets: []string{"handlers-test-target.example.com"}})
	triggerReq := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/runs", bytes.NewReader(body))
	triggerRec := httptest.NewRecorder()
	h.triggerRun(triggerRec, triggerReq)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/jobs", nil)
	rec := httptest.NewRecorder()
	h.listJobs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var page pagination.Page[Job]
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Items) == 0 {
		t.Error("expected at least one job in the list")
	}
}

func TestListJobs_InvalidKindIsBadRequest(t *testing.T) {
	store := testStore(t)
	h := NewHandlers(store, nil, noopActor, noopRecordAuditRead)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/jobs?kind=bogus", nil)
	rec := httptest.NewRecorder()
	h.listJobs(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body)
	}
}

func TestListJobs_InvalidStatusIsBadRequest(t *testing.T) {
	store := testStore(t)
	h := NewHandlers(store, nil, noopActor, noopRecordAuditRead)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/jobs?status=bogus", nil)
	rec := httptest.NewRecorder()
	h.listJobs(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body)
	}
}

func TestListJobs_FiltersByQueryParams(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	targetJob, err := store.CreateJob(ctx, JobKindRun, "", "", nil, []string{"orchestrator-handlers-test-filter.example.com"}, "orchestrator-handlers-filter-actor")
	if err != nil {
		t.Fatalf("CreateJob(target) error: %v", err)
	}
	if _, err := store.CreateJob(ctx, JobKindTask, "package", "", nil, []string{"orchestrator-handlers-test-filter.example.com"}, "someone-else"); err != nil {
		t.Fatalf("CreateJob(other) error: %v", err)
	}

	h := NewHandlers(store, nil, noopActor, noopRecordAuditRead)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/jobs?kind=run&triggeredBy=orchestrator-handlers-filter-actor&status=running&target=orchestrator-handlers-test-filter.example.com", nil)
	rec := httptest.NewRecorder()
	h.listJobs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}

	var page pagination.Page[Job]
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Not an exact-length assertion: kind/triggeredBy/status here are
	// fixed literals, so a prior run against this same shared dev
	// database (see testStore) can leave an earlier matching row behind
	// - see the identical reasoning in store_test.go's filter tests.
	found := false
	for _, j := range page.Items {
		if j.Kind != JobKindRun || j.TriggeredBy != "orchestrator-handlers-filter-actor" || j.Status != StatusRunning {
			t.Errorf("Items contains %+v, which doesn't match all four filters", j)
		}
		if j.ID == targetJob.ID {
			found = true
		}
	}
	if !found {
		t.Error("combined kind/triggeredBy/status/target filters excluded the matching job")
	}
}

func TestListAndGetJob_RecordReadAuditEvents(t *testing.T) {
	store := testStore(t)
	broker, stopAgent := startRealAgent(t, "audit-read-target.example.com", fakeRunner("ok", 0))
	t.Cleanup(stopAgent)
	dispatcher := NewDispatcher(store, broker, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go dispatcher.Run(ctx)
	var recorded []auditlog.Event
	h := NewHandlers(store, dispatcher, noopActor, func(_ *http.Request, e auditlog.Event) { recorded = append(recorded, e) })

	body, _ := json.Marshal(triggerRunRequest{Targets: []string{"audit-read-target.example.com"}})
	triggerReq := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/runs", bytes.NewReader(body))
	triggerRec := httptest.NewRecorder()
	h.triggerRun(triggerRec, triggerReq)
	var created Job
	if err := json.NewDecoder(triggerRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode triggered job: %v", err)
	}

	h.listJobs(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/jobs", nil))

	idStr := strconv.FormatInt(created.ID, 10)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/jobs/"+idStr, nil)
	req.SetPathValue("id", idStr)
	h.getJob(httptest.NewRecorder(), req)

	if len(recorded) != 2 {
		t.Fatalf("recorded %d read audit events, want 2: %+v", len(recorded), recorded)
	}
	if recorded[0].Action != "job.list.viewed" {
		t.Errorf("event 0 Action = %q, want job.list.viewed", recorded[0].Action)
	}
	if recorded[1].Action != "job.viewed" || recorded[1].ResourceID != idStr {
		t.Errorf("event 1 = %+v, want action=job.viewed resourceId=%s", recorded[1], idStr)
	}
}

func TestGetJob_NotFoundReturns404(t *testing.T) {
	h := realHandlers(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/jobs/999999999", nil)
	req.SetPathValue("id", "999999999")
	rec := httptest.NewRecorder()
	h.getJob(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestGetJob_ReturnsFullDetail(t *testing.T) {
	h := realHandlers(t)
	body, _ := json.Marshal(triggerRunRequest{Targets: []string{"handlers-test-target.example.com"}})
	triggerReq := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/runs", bytes.NewReader(body))
	triggerRec := httptest.NewRecorder()
	h.triggerRun(triggerRec, triggerReq)
	var created Job
	json.NewDecoder(triggerRec.Body).Decode(&created)

	idStr := strconv.FormatInt(created.ID, 10)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/jobs/"+idStr, nil)
	req.SetPathValue("id", idStr)
	rec := httptest.NewRecorder()
	h.getJob(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var job Job
	if err := json.NewDecoder(rec.Body).Decode(&job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(job.Targets) != 1 {
		t.Errorf("len(Targets) = %d, want 1", len(job.Targets))
	}
}
