package codemanager

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/messaging"
)

type recordedActivity struct {
	action, summary string
}

func noopActor(*http.Request) string { return "test-actor" }

func noopRecordAudit(_ *http.Request, _ auditlog.Event) {}

// realHandlers builds Handlers wired to a real Postgres store, a real
// embedded NATS bus, and a real Deployer against the g10k binary/control
// repo fixtures - see realFixtures in deploy_test.go. Skips if either
// fixture isn't set up.
func realHandlers(t *testing.T, codeDirPath string) (*Handlers, *messaging.Bus, *[]recordedActivity, *[]auditlog.Event) {
	t.Helper()
	g10kBin, controlRepoURL := realFixtures(t)
	store := testStore(t)

	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	t.Cleanup(bus.Close)

	deployer := NewDeployer(Config{
		G10KBinPath:    g10kBin,
		ControlRepoURL: controlRepoURL,
		CodeDirPath:    codeDirPath,
	})

	var mu sync.Mutex
	recorded := make([]recordedActivity, 0)
	recordActivity := func(_ *http.Request, action, summary string) {
		mu.Lock()
		defer mu.Unlock()
		recorded = append(recorded, recordedActivity{action: action, summary: summary})
	}
	recordedAudit := make([]auditlog.Event, 0)
	recordAudit := func(_ *http.Request, e auditlog.Event) {
		mu.Lock()
		defer mu.Unlock()
		recordedAudit = append(recordedAudit, e)
	}

	h := NewHandlers(
		deployer, store, bus, "unused-in-these-tests", slog.New(slog.NewTextHandler(io.Discard, nil)),
		recordActivity, noopActor, recordAudit, noopRecordAudit,
	)
	return h, bus, &recorded, &recordedAudit
}

func TestTriggerDeploy_RealSuccess_PublishesEventAndRecordsActivity(t *testing.T) {
	codeDir := t.TempDir()
	h, bus, recorded, recordedAudit := realHandlers(t, codeDir)

	var eventMu sync.Mutex
	var gotEvent *DeployedEvent
	if _, err := SubscribeDeployed(bus, func(e DeployedEvent) {
		eventMu.Lock()
		defer eventMu.Unlock()
		gotEvent = &e
	}); err != nil {
		t.Fatalf("SubscribeDeployed() error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-deploys", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.triggerDeploy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}

	liveMarker := filepath.Join(codeDir, "environments", "production", "manifests", "site.pp")
	if _, err := os.Stat(liveMarker); err != nil {
		t.Errorf("expected live environment to be activated, but %s is missing: %v", liveMarker, err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		eventMu.Lock()
		got := gotEvent
		eventMu.Unlock()
		if got != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	if gotEvent == nil {
		t.Error("no deployment-ready event was published for a successful deploy")
	} else if gotEvent.Environment != "production" {
		t.Errorf("event Environment = %q, want production", gotEvent.Environment)
	}

	var sawSucceeded bool
	for _, r := range *recorded {
		if r.action == "deploy.succeeded" {
			sawSucceeded = true
		}
	}
	if !sawSucceeded {
		t.Errorf("no deploy.succeeded activity recorded, got: %+v", *recorded)
	}

	var sawAuditSucceeded bool
	for _, e := range *recordedAudit {
		if e.Action == "deploy.succeeded" && e.ResourceID != "" {
			sawAuditSucceeded = true
		}
	}
	if !sawAuditSucceeded {
		t.Errorf("no deploy.succeeded audit event recorded, got: %+v", *recordedAudit)
	}
}

func TestTriggerDeploy_RealFailure_NoActivationNoEventFailedActivityRecorded(t *testing.T) {
	codeDir := t.TempDir()
	g10kBin, _ := realFixtures(t)
	store := testStore(t)

	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	defer bus.Close()

	// Pre-activate a known-good version so we can prove a subsequent
	// failed deploy leaves it untouched.
	deployer := NewDeployer(Config{G10KBinPath: g10kBin, ControlRepoURL: "file://" + filepath.Join(codeDir, "nonexistent"), CodeDirPath: codeDir})
	baselineDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(baselineDir, "marker.txt"), []byte("baseline"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := deployer.Activate("production", baselineDir); err != nil {
		t.Fatalf("baseline Activate() error: %v", err)
	}

	var eventFired bool
	var eventMu sync.Mutex
	if _, err := SubscribeDeployed(bus, func(DeployedEvent) {
		eventMu.Lock()
		eventFired = true
		eventMu.Unlock()
	}); err != nil {
		t.Fatalf("SubscribeDeployed() error: %v", err)
	}

	var mu sync.Mutex
	recorded := make([]recordedActivity, 0)
	recordActivity := func(_ *http.Request, action, summary string) {
		mu.Lock()
		defer mu.Unlock()
		recorded = append(recorded, recordedActivity{action: action, summary: summary})
	}
	recordedAudit := make([]auditlog.Event, 0)
	recordAudit := func(_ *http.Request, e auditlog.Event) {
		mu.Lock()
		defer mu.Unlock()
		recordedAudit = append(recordedAudit, e)
	}

	h := NewHandlers(
		deployer, store, bus, "unused", slog.New(slog.NewTextHandler(io.Discard, nil)),
		recordActivity, noopActor, recordAudit, noopRecordAudit,
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-deploys", nil)
	rec := httptest.NewRecorder()
	h.triggerDeploy(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 for a failed deploy; body: %s", rec.Code, rec.Body)
	}

	got, err := os.ReadFile(filepath.Join(codeDir, "environments", "production", "marker.txt"))
	if err != nil {
		t.Fatalf("read live environment after failed deploy: %v", err)
	}
	if string(got) != "baseline" {
		t.Errorf("live environment content = %q after a failed deploy, want unchanged %q", got, "baseline")
	}

	time.Sleep(200 * time.Millisecond) // give any (unwanted) async publish a chance to land
	eventMu.Lock()
	fired := eventFired
	eventMu.Unlock()
	if fired {
		t.Error("deployment-ready event was published for a failed deploy")
	}

	mu.Lock()
	defer mu.Unlock()
	var sawFailed bool
	for _, r := range recorded {
		if r.action == "deploy.failed" {
			sawFailed = true
		}
	}
	if !sawFailed {
		t.Errorf("no deploy.failed activity recorded, got: %+v", recorded)
	}
	var sawAuditFailed bool
	for _, e := range recordedAudit {
		if e.Action == "deploy.failed" && e.ResourceID != "" {
			sawAuditFailed = true
		}
	}
	if !sawAuditFailed {
		t.Errorf("no deploy.failed audit event recorded, got: %+v", recordedAudit)
	}

	deploys, _, err := store.ListDeploys(context.Background(), 1, 1000, DeployFilter{})
	if err != nil {
		t.Fatalf("ListDeploys() error: %v", err)
	}
	if len(deploys) == 0 || deploys[0].Status != StatusFailed {
		t.Errorf("most recent deploy record status = %+v, want failed", deploys[0])
	}
}

func TestListDeploys_RecordsReadAuditEvent(t *testing.T) {
	store := testStore(t)
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	t.Cleanup(bus.Close)

	var recordedAudit []auditlog.Event
	h := NewHandlers(
		NewDeployer(Config{}), store, bus, "unused", slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(*http.Request, string, string) {}, noopActor,
		noopRecordAudit, func(_ *http.Request, e auditlog.Event) { recordedAudit = append(recordedAudit, e) },
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-deploys", nil)
	rec := httptest.NewRecorder()
	h.listDeploys(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	if len(recordedAudit) != 1 || recordedAudit[0].Action != "deploy.list.viewed" {
		t.Errorf("recorded = %+v, want exactly one deploy.list.viewed", recordedAudit)
	}
}

func TestListDeploys_InvalidStatusIsBadRequest(t *testing.T) {
	store := testStore(t)
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	t.Cleanup(bus.Close)

	h := NewHandlers(
		NewDeployer(Config{}), store, bus, "unused", slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(*http.Request, string, string) {}, noopActor,
		noopRecordAudit, noopRecordAudit,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-deploys?status=bogus", nil)
	rec := httptest.NewRecorder()
	h.listDeploys(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body)
	}
}

func TestListDeploys_FiltersByQueryParams(t *testing.T) {
	store := testStore(t)
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	t.Cleanup(bus.Close)

	ctx := context.Background()
	targetID, err := store.CreateDeploy(ctx, "codemanager-handlers-test-filter-target", "codemanager-handlers-filter-actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	if _, err := store.CreateDeploy(ctx, "codemanager-handlers-test-filter-other", "someone-else"); err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	h := NewHandlers(
		NewDeployer(Config{}), store, bus, "unused", slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(*http.Request, string, string) {}, noopActor,
		noopRecordAudit, noopRecordAudit,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-deploys?ref=codemanager-handlers-test-filter-target&triggeredBy=codemanager-handlers-filter-actor&status=running", nil)
	rec := httptest.NewRecorder()
	h.listDeploys(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}

	var page struct {
		Items []Deploy `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	// Not an exact-length assertion: ref/triggeredBy here are fixed
	// literals, so a prior run against this same shared dev database
	// (see testStore) can leave an earlier matching row behind - see the
	// identical reasoning in TestListDeploys_FilterByRef.
	found := false
	for _, d := range page.Items {
		if d.Ref != "codemanager-handlers-test-filter-target" || d.TriggeredBy != "codemanager-handlers-filter-actor" || d.Status != "running" {
			t.Errorf("Items contains %+v, which doesn't match all three filters", d)
		}
		if d.ID == targetID {
			found = true
		}
	}
	if !found {
		t.Error("combined ref/triggeredBy/status filters excluded the matching deploy")
	}
}

func TestListRefs_ReturnsRecordedRef(t *testing.T) {
	store := testStore(t)
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	t.Cleanup(bus.Close)

	ctx := context.Background()
	if _, err := store.CreateDeploy(ctx, "codemanager-handlers-test-refs", "actor"); err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	h := NewHandlers(
		NewDeployer(Config{}), store, bus, "unused", slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(*http.Request, string, string) {}, noopActor,
		noopRecordAudit, noopRecordAudit,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/code-deploys/refs", nil)
	rec := httptest.NewRecorder()
	h.listRefs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var refs []string
	if err := json.Unmarshal(rec.Body.Bytes(), &refs); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !slices.Contains(refs, "codemanager-handlers-test-refs") {
		t.Errorf("refs = %v, want it to contain the just-created ref", refs)
	}
}
