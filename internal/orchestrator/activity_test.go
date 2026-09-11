package orchestrator

import (
	"context"
	"sync"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
)

type recordedActivity struct {
	action, actor, summary string
}

func capturingRecorder(dst *[]recordedActivity, mu *sync.Mutex) ActivityRecorder {
	return func(action, actor, summary string) {
		mu.Lock()
		defer mu.Unlock()
		*dst = append(*dst, recordedActivity{action: action, actor: actor, summary: summary})
	}
}

func capturingAuditRecorder(dst *[]auditlog.Event, mu *sync.Mutex) AuditRecorder {
	return func(e auditlog.Event) {
		mu.Lock()
		defer mu.Unlock()
		*dst = append(*dst, e)
	}
}

func TestDispatcher_RecordsActivityForTriggerAndCompletion(t *testing.T) {
	store := testStore(t)
	broker, stopAgent := startRealAgent(t, "activity-target.example.com", fakeRunner("ok", 0))
	defer stopAgent()

	var mu sync.Mutex
	var recorded []recordedActivity
	var recordedAudit []auditlog.Event

	dispatcher := NewDispatcher(store, broker, nil)
	dispatcher.SetActivityRecorder(capturingRecorder(&recorded, &mu))
	dispatcher.SetAuditRecorder(capturingAuditRecorder(&recordedAudit, &mu))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)

	job, err := store.CreateJob(ctx, JobKindRun, "", "", nil, []string{"activity-target.example.com"}, "activity-test-actor")
	if err != nil {
		t.Fatalf("CreateJob() error: %v", err)
	}
	dispatcher.DispatchRun(ctx, job)

	waitForJobStatus(t, store, job.ID, StatusSucceeded)

	mu.Lock()
	defer mu.Unlock()
	if len(recorded) != 2 {
		t.Fatalf("recorded %d activity events, want 2 (trigger + completion); got %+v", len(recorded), recorded)
	}
	if recorded[0].action != "run.triggered" {
		t.Errorf("recorded[0].action = %q, want run.triggered", recorded[0].action)
	}
	if recorded[0].actor != "activity-test-actor" {
		t.Errorf("recorded[0].actor = %q, want activity-test-actor", recorded[0].actor)
	}
	if recorded[1].action != "run.succeeded" {
		t.Errorf("recorded[1].action = %q, want run.succeeded", recorded[1].action)
	}

	if len(recordedAudit) != 2 {
		t.Fatalf("recorded %d audit events, want 2 (trigger + completion); got %+v", len(recordedAudit), recordedAudit)
	}
	if recordedAudit[0].Action != "run.triggered" || recordedAudit[0].Actor != "activity-test-actor" || recordedAudit[0].ResourceID == "" {
		t.Errorf("audit[0] = %+v, want action=run.triggered actor=activity-test-actor with a resourceId", recordedAudit[0])
	}
	if recordedAudit[1].Action != "run.succeeded" {
		t.Errorf("audit[1].Action = %q, want run.succeeded", recordedAudit[1].Action)
	}
}

func TestDispatcher_DoesNotRecordActivityForPlanSteps(t *testing.T) {
	store := testStore(t)
	broker, stopAgent := startRealAgent(t, "plan-activity-target.example.com", fakeRunner("ok", 0))
	defer stopAgent()

	var mu sync.Mutex
	var recorded []recordedActivity
	var recordedAudit []auditlog.Event

	dispatcher := NewDispatcher(store, broker, nil)
	dispatcher.SetActivityRecorder(capturingRecorder(&recorded, &mu))
	dispatcher.SetAuditRecorder(capturingAuditRecorder(&recordedAudit, &mu))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dispatcher.Run(ctx)

	plan, err := store.CreatePlan(ctx, "activity-plan", []PlanStep{
		{Kind: JobKindRun},
	}, []string{"plan-activity-target.example.com"}, "plan-actor")
	if err != nil {
		t.Fatalf("CreatePlan() error: %v", err)
	}
	dispatcher.DispatchPlan(ctx, plan)

	waitForJobStatus(t, store, plan.ID, StatusSucceeded)

	mu.Lock()
	defer mu.Unlock()
	// Exactly plan.triggered + plan.succeeded - no run.* entries for the
	// individual step job.
	if len(recorded) != 2 {
		t.Fatalf("recorded %d activity events, want 2 (plan trigger + completion only); got %+v", len(recorded), recorded)
	}
	if recorded[0].action != "plan.triggered" || recorded[1].action != "plan.succeeded" {
		t.Errorf("recorded actions = %q, %q, want plan.triggered, plan.succeeded", recorded[0].action, recorded[1].action)
	}

	if len(recordedAudit) != 2 || recordedAudit[0].Action != "plan.triggered" || recordedAudit[1].Action != "plan.succeeded" {
		t.Errorf("recorded audit events = %+v, want exactly [plan.triggered, plan.succeeded]", recordedAudit)
	}
}
