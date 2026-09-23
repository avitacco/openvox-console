package codemanager

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
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
		G10KBinPath: g10kBin,
		Sources:     singleSource(controlRepoURL),
		CodeDirPath: codeDirPath,
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
	} else {
		if gotEvent.Environment != "production" {
			t.Errorf("event Environment = %q, want production", gotEvent.Environment)
		}
		if gotEvent.Source != DefaultSourceName {
			t.Errorf("event Source = %q, want %q", gotEvent.Source, DefaultSourceName)
		}
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
	deployer := NewDeployer(Config{G10KBinPath: g10kBin, Sources: singleSource("file://" + filepath.Join(codeDir, "nonexistent")), CodeDirPath: codeDir})
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
	targetID, err := store.CreateDeploy(ctx, DefaultSourceName, "codemanager-handlers-test-filter-target", "codemanager-handlers-filter-actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	if _, err := store.CreateDeploy(ctx, DefaultSourceName, "codemanager-handlers-test-filter-other", "someone-else"); err != nil {
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
	if _, err := store.CreateDeploy(ctx, DefaultSourceName, "codemanager-handlers-test-refs", "actor"); err != nil {
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

// multiSourceHandlers builds Handlers over two sources without any real
// g10k or control repo: these tests are about routing, authentication
// and status codes, all of which are decided before a deploy is
// attempted.
func multiSourceHandlers(t *testing.T, globalSecret string) *Handlers {
	t.Helper()
	store := testStore(t)

	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	t.Cleanup(bus.Close)

	deployer := NewDeployer(Config{
		G10KBinPath: "/nonexistent/g10k",
		Sources: Sources{
			{Name: DefaultSourceName, Remote: "file:///srv/control.git"},
			{Name: "team_a", Remote: "file:///srv/team-a.git", Prefix: prefixTrue, WebhookSecret: "team-a-secret"},
		},
		CodeDirPath: t.TempDir(),
	})

	return NewHandlers(
		deployer, store, bus, globalSecret, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(*http.Request, string, string) {}, noopActor,
		noopRecordAudit, noopRecordAudit,
	)
}

func signedWebhookRequest(t *testing.T, target, secret, body string) *http.Request {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	return req
}

func TestTriggerDeploy_UnknownSourceIsBadRequest(t *testing.T) {
	// A caller's typo, not an upstream failure - so 400, distinct from
	// the 502 a genuinely failed deploy returns.
	h := multiSourceHandlers(t, "global-secret")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-deploys", strings.NewReader(`{"source":"nope"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.triggerDeploy(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "nope") {
		t.Errorf("body = %s, want it to name the unknown source", rec.Body)
	}
}

func TestTriggerDeploy_UnconfiguredIsServiceUnavailable(t *testing.T) {
	store := testStore(t)
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	t.Cleanup(bus.Close)

	h := NewHandlers(
		NewDeployer(Config{}), store, bus, "unused", slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(*http.Request, string, string) {}, noopActor, noopRecordAudit, noopRecordAudit,
	)

	rec := httptest.NewRecorder()
	h.triggerDeploy(rec, httptest.NewRequest(http.MethodPost, "/api/v1/code-deploys", nil))

	// Not configured is a different thing from a bad source name, and
	// an operator who has configured nothing should be told that.
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body: %s", rec.Code, rec.Body)
	}
}

func TestWebhookDeploy_SourceRoutingAndSecrets(t *testing.T) {
	const body = `{"ref":"refs/heads/production","after":"abc123"}`

	for _, tc := range []struct {
		name       string
		sourcePath string // "" means the unsuffixed legacy route
		secret     string
		wantStatus int
	}{
		{
			// The legacy route keeps working against the global secret,
			// so an already-configured git host needs no change.
			name: "default source via legacy route", sourcePath: "", secret: "global-secret",
			wantStatus: http.StatusBadGateway, // signature accepted; the deploy itself then fails (no real g10k)
		},
		{
			name: "named source with its own secret", sourcePath: "team_a", secret: "team-a-secret",
			wantStatus: http.StatusBadGateway,
		},
		{
			// The isolation this design exists for: team_a's secret
			// must not be able to deploy the control repo.
			name: "one source's secret cannot trigger another's deploy", sourcePath: DefaultSourceName, secret: "team-a-secret",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "global secret does not authenticate a named source", sourcePath: "team_a", secret: "global-secret",
			wantStatus: http.StatusUnauthorized,
		},
		{
			// Same response as a bad signature: a different one would
			// let an unauthenticated caller enumerate source names.
			name: "unknown source", sourcePath: "no_such_source", secret: "global-secret",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "wrong signature", sourcePath: "team_a", secret: "wrong",
			wantStatus: http.StatusUnauthorized,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := multiSourceHandlers(t, "global-secret")

			target := "/api/v1/code-deploys/webhook"
			req := signedWebhookRequest(t, target, tc.secret, body)
			if tc.sourcePath != "" {
				req = signedWebhookRequest(t, target+"/"+tc.sourcePath, tc.secret, body)
				req.SetPathValue("source", tc.sourcePath)
			}

			rec := httptest.NewRecorder()
			h.webhookDeploy(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", rec.Code, tc.wantStatus, rec.Body)
			}
		})
	}
}

func TestWebhookDeploy_UnknownSourceAndBadSignatureAreIndistinguishable(t *testing.T) {
	const body = `{"ref":"refs/heads/production","after":"abc123"}`
	h := multiSourceHandlers(t, "global-secret")

	unknown := signedWebhookRequest(t, "/api/v1/code-deploys/webhook/no_such_source", "global-secret", body)
	unknown.SetPathValue("source", "no_such_source")
	unknownRec := httptest.NewRecorder()
	h.webhookDeploy(unknownRec, unknown)

	badSig := signedWebhookRequest(t, "/api/v1/code-deploys/webhook/team_a", "wrong-secret", body)
	badSig.SetPathValue("source", "team_a")
	badSigRec := httptest.NewRecorder()
	h.webhookDeploy(badSigRec, badSig)

	if unknownRec.Code != badSigRec.Code || unknownRec.Body.String() != badSigRec.Body.String() {
		t.Errorf("an unknown source is distinguishable from a bad signature, which lets a caller enumerate sources:\nunknown: %d %s\nbad sig: %d %s",
			unknownRec.Code, unknownRec.Body, badSigRec.Code, badSigRec.Body)
	}
}

func TestListSources_ReturnsNamesAndPrefixesOnly(t *testing.T) {
	h := multiSourceHandlers(t, "global-secret")

	rec := httptest.NewRecorder()
	h.listSources(rec, httptest.NewRequest(http.MethodGet, "/api/v1/code-deploys/sources", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}

	var sources []struct {
		Name       string `json:"name"`
		Prefix     string `json:"prefix"`
		Configured bool   `json:"configured"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sources); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	// Configured sources come first and in declaration order; history
	// may add more behind them, so this is a prefix assertion, not an
	// exact-length one.
	if len(sources) < 2 {
		t.Fatalf("got %d sources, want at least 2: %s", len(sources), rec.Body)
	}
	if sources[0].Name != DefaultSourceName || sources[0].Prefix != "" || !sources[0].Configured {
		t.Errorf("sources[0] = %+v, want the unprefixed control source, configured", sources[0])
	}
	if sources[1].Name != "team_a" || sources[1].Prefix != "team_a_" || !sources[1].Configured {
		t.Errorf("sources[1] = %+v, want team_a with a team_a_ prefix, configured", sources[1])
	}
	// Anything beyond the configured ones came from deploy history and
	// must be flagged as not deployable.
	for _, s := range sources[2:] {
		if s.Configured {
			t.Errorf("history-only source %+v is flagged configured", s)
		}
	}

	// Secrets and key paths are configuration; no console user needs
	// them, and the UI only ever wants a name to filter by.
	for _, leaked := range []string{"team-a-secret", "global-secret", "private_key", "remote"} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Errorf("response leaks %q: %s", leaked, rec.Body)
		}
	}
}

func TestListDeploys_FiltersBySource(t *testing.T) {
	store := testStore(t)
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	t.Cleanup(bus.Close)

	ctx := context.Background()
	targetID, err := store.CreateDeploy(ctx, "codemanager_handlers_src_a", "codemanager-handlers-source-filter", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	if _, err := store.CreateDeploy(ctx, "codemanager_handlers_src_b", "codemanager-handlers-source-filter", "actor"); err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}

	h := NewHandlers(
		NewDeployer(Config{}), store, bus, "unused", slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(*http.Request, string, string) {}, noopActor, noopRecordAudit, noopRecordAudit,
	)

	rec := httptest.NewRecorder()
	h.listDeploys(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/code-deploys?source=codemanager_handlers_src_a&ref=codemanager-handlers-source-filter", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var page struct {
		Items []Deploy `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	found := false
	for _, d := range page.Items {
		if d.Source != "codemanager_handlers_src_a" {
			t.Errorf("source filter returned a deploy from %q", d.Source)
		}
		if d.ID == targetID {
			found = true
		}
	}
	if !found {
		t.Error("source filter excluded the matching deploy")
	}
}

func TestRegister_RoutePermissions(t *testing.T) {
	// listSources is reached through authorize in Register, so calling
	// the handler directly (as the test above does) proves nothing
	// about who may call it. This asserts the wiring instead.
	h := multiSourceHandlers(t, "global-secret")

	granted := map[string]string{}
	authorize := func(permission string, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			granted[r.Method+" "+r.URL.Path] = permission
			next(w, r)
		}
	}

	mux := http.NewServeMux()
	h.Register(mux, authorize)

	for _, tc := range []struct {
		method, path, want string
	}{
		{http.MethodGet, "/api/v1/code-deploys/sources", "code:read"},
		{http.MethodGet, "/api/v1/code-repositories", "code:read"},
		{http.MethodGet, "/api/v1/code-deploys", "code:read"},
		{http.MethodGet, "/api/v1/code-deploys/refs", "code:read"},
		{http.MethodPost, "/api/v1/code-deploys", "code:deploy"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if got := granted[tc.method+" "+tc.path]; got != tc.want {
			t.Errorf("%s %s required %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}

	// Both webhook routes verify their own signature instead: a git
	// host has no console token to present, so they must NOT be behind
	// authorize.
	for _, path := range []string{
		"/api/v1/code-deploys/webhook",
		"/api/v1/code-deploys/webhook/team_a",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}")))
		if permission, wrapped := granted[http.MethodPost+" "+path]; wrapped {
			t.Errorf("%s is behind the %q permission, but a git host has no token to present", path, permission)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401 for an unsigned request", path, rec.Code)
		}
	}
}

// repositoriesHandlers builds Handlers over two sources - one that has
// deployed, one that never has - with the given usage resolver.
func repositoriesHandlers(t *testing.T, usage *UsageResolver) (*Handlers, string, string) {
	t.Helper()
	store := testStore(t)

	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	t.Cleanup(bus.Close)

	// Unique per run so assertions survive the shared dev database.
	deployed := "codemanager_repo_deployed_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	never := "codemanager_repo_never_" + strconv.FormatInt(time.Now().UnixNano(), 36)

	deployer := NewDeployer(Config{
		G10KBinPath: "/nonexistent/g10k",
		Sources: Sources{
			{Name: deployed, Remote: "https://x-access-token:ghp_SUPERSECRET@github.com/org/deployed.git"},
			{Name: never, Remote: "git@github.com:org/never.git", Prefix: prefixTrue, PrivateKey: "/etc/keys/never"},
		},
		CodeDirPath: t.TempDir(),
	})

	h := NewHandlers(
		deployer, store, bus, "unused", slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(*http.Request, string, string) {}, noopActor, noopRecordAudit, noopRecordAudit,
	)
	h.SetUsageResolver(usage)

	// Give the first source a real successful deploy to summarise.
	size := int64(2048)
	id, err := store.CreateDeploy(context.Background(), deployed, "abc123", "actor")
	if err != nil {
		t.Fatalf("CreateDeploy() error: %v", err)
	}
	env := deployed + "_production"
	if err := store.CompleteDeploy(context.Background(), id, StatusSucceeded, "", &env, &size); err != nil {
		t.Fatalf("CompleteDeploy() error: %v", err)
	}

	return h, deployed, never
}

func repositoriesResponse(t *testing.T, h *Handlers) RepositoriesResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	h.listRepositories(rec, httptest.NewRequest(http.MethodGet, "/api/v1/code-repositories", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var response RepositoriesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return response
}

func findRepo(t *testing.T, response RepositoriesResponse, name string) Repository {
	t.Helper()
	for _, repo := range response.Repositories {
		if repo.Name == name {
			return repo
		}
	}
	t.Fatalf("repository %q not in response %+v", name, response.Repositories)
	return Repository{}
}

func TestListRepositories_ReportsDeployedAndNeverDeployed(t *testing.T) {
	h, deployed, never := repositoriesHandlers(t, nil)
	response := repositoriesResponse(t, h)

	got := findRepo(t, response, deployed)
	if len(got.Environments) != 1 || got.Environments[0] != deployed+"_production" {
		t.Errorf("Environments = %v, want the one it deployed", got.Environments)
	}
	if got.SizeBytes == nil || *got.SizeBytes != 2048 {
		t.Errorf("SizeBytes = %v, want 2048", got.SizeBytes)
	}
	if got.LastDeployedAt == nil {
		t.Error("LastDeployedAt is nil for a repository that deployed")
	}

	// The distinction the whole nullable design exists for: never
	// deployed must read as absent, not as a zero-byte repository that
	// deployed at the epoch.
	fresh := findRepo(t, response, never)
	if fresh.SizeBytes != nil {
		t.Errorf("SizeBytes = %v for a never-deployed repository, want absent", *fresh.SizeBytes)
	}
	if fresh.LastDeployedAt != nil {
		t.Errorf("LastDeployedAt = %v for a never-deployed repository, want absent", *fresh.LastDeployedAt)
	}
	if len(fresh.Environments) != 0 {
		t.Errorf("Environments = %v for a never-deployed repository, want empty", fresh.Environments)
	}
	if fresh.Prefix != never+"_" {
		t.Errorf("Prefix = %q, want the source's effective prefix", fresh.Prefix)
	}
}

func TestListRepositories_RedactsRemoteAndLeaksNothing(t *testing.T) {
	h, _, _ := repositoriesHandlers(t, nil)

	rec := httptest.NewRecorder()
	h.listRepositories(rec, httptest.NewRequest(http.MethodGet, "/api/v1/code-repositories", nil))
	body := rec.Body.String()

	if strings.Contains(body, "ghp_SUPERSECRET") {
		t.Errorf("response leaks the remote's embedded token: %s", body)
	}
	if !strings.Contains(body, "github.com/org/deployed.git") {
		t.Errorf("redaction destroyed the repository's identity: %s", body)
	}
	// A private key path is configuration, not something a code:read
	// user needs.
	if strings.Contains(body, "/etc/keys/never") {
		t.Errorf("response leaks a private key path: %s", body)
	}
}

func TestListRepositories_NodeCountsUnavailableDegradeOnlyThemselves(t *testing.T) {
	// openvoxdb down must not blank the remote, prefix or last deploy,
	// all of which come from config and Postgres.
	failing := NewUsageResolver(
		func(context.Context) ([]NodeEnvironment, error) { return nil, errors.New("openvoxdb unreachable") },
		func(context.Context) ([]NodeEnvironment, error) { return nil, errors.New("openvoxdb unreachable") },
	)
	h, deployed, _ := repositoriesHandlers(t, failing)

	response := repositoriesResponse(t, h)
	if response.NodeCountsAvailable {
		t.Error("NodeCountsAvailable is true despite the node source failing")
	}

	got := findRepo(t, response, deployed)
	if got.AssignedNodes != nil || got.ReportingNodes != nil {
		t.Errorf("counts = %v/%v, want absent (unavailable), not zero", got.AssignedNodes, got.ReportingNodes)
	}
	if got.LastDeployedAt == nil || got.SizeBytes == nil {
		t.Error("a node-source failure blanked fields that do not depend on it")
	}
	if !strings.Contains(got.Remote, "github.com/org/deployed.git") {
		t.Errorf("Remote = %q after a node-source failure", got.Remote)
	}
}

func TestListRepositories_CountsAndUnattributed(t *testing.T) {
	var h *Handlers
	var deployed, never string

	// Built in two steps: the source names are generated inside the
	// helper, and the usage readings have to reference the deployed
	// one's environment.
	h, deployed, never = repositoriesHandlers(t, nil)
	env := deployed + "_production"

	h.SetUsageResolver(NewUsageResolver(
		envList("a", env, "b", env, "c", "an_environment_nothing_deployed", "d", nil),
		envList("a", env, "b", "an_environment_nothing_deployed", "c", "an_environment_nothing_deployed", "d", nil),
	))

	response := repositoriesResponse(t, h)
	if !response.NodeCountsAvailable {
		t.Fatal("NodeCountsAvailable is false with a working resolver")
	}

	got := findRepo(t, response, deployed)
	if got.AssignedNodes == nil || *got.AssignedNodes != 2 {
		t.Errorf("AssignedNodes = %v, want 2", got.AssignedNodes)
	}
	// Divergence preserved, not reconciled.
	if got.ReportingNodes == nil || *got.ReportingNodes != 1 {
		t.Errorf("ReportingNodes = %v, want 1", got.ReportingNodes)
	}

	// A repository that has deployed nothing owns no environments, so
	// it must not absorb the unattributed nodes.
	fresh := findRepo(t, response, never)
	if fresh.AssignedNodes == nil || *fresh.AssignedNodes != 0 {
		t.Errorf("never-deployed AssignedNodes = %v, want 0", fresh.AssignedNodes)
	}

	// Environments no configured repository deployed are reported on
	// their own rather than folded into anyone's count.
	if response.UnattributedAssigned == nil || *response.UnattributedAssigned != 1 {
		t.Errorf("UnattributedAssigned = %v, want 1", response.UnattributedAssigned)
	}
	if response.UnattributedReporting == nil || *response.UnattributedReporting != 2 {
		t.Errorf("UnattributedReporting = %v, want 2", response.UnattributedReporting)
	}
	if response.NodesWithoutEnvironment == nil || *response.NodesWithoutEnvironment != 1 {
		t.Errorf("NodesWithoutEnvironment = %v, want 1", response.NodesWithoutEnvironment)
	}
}

// A deploy of an environment already being deployed elsewhere is refused
// with 409, not run concurrently against the same directory tree. web
// mode carries the code manager, so two web instances can each receive a
// webhook for the same branch at the same time.
func TestTriggerDeploy_RefusesWhenTheEnvironmentIsAlreadyDeploying(t *testing.T) {
	codeDir := t.TempDir()
	h, _, _, _ := realHandlers(t, codeDir)

	// Stands in for the lease being held by another instance: Hold
	// reports "not acquired" and never runs the work.
	h.SetDeployLeases(heldElsewhere{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-deploys", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.triggerDeploy(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d when another instance is deploying", rec.Code, http.StatusConflict)
	}
}

// With the lease free, the deploy runs exactly as it does unleased -
// the lease must not change the normal path.
func TestTriggerDeploy_RunsWhenTheLeaseIsFree(t *testing.T) {
	codeDir := t.TempDir()
	h, _, _, _ := realHandlers(t, codeDir)

	h.SetDeployLeases(alwaysFree{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/code-deploys", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.triggerDeploy(rec, req)

	if rec.Code == http.StatusConflict {
		t.Error("a deploy was refused as already-running while the lease was free")
	}
}

// Deploys are leased per environment, so two different environments are
// never serialized against each other.
func TestDeployLeaseNameIsPerEnvironment(t *testing.T) {
	if DeployLeaseName("production") == DeployLeaseName("staging") {
		t.Error("two environments share one deploy lease name; deploying one would block the other")
	}
}

type heldElsewhere struct{}

func (heldElsewhere) Hold(context.Context, string, func(context.Context) error) (bool, error) {
	return false, nil
}

type alwaysFree struct{}

func (alwaysFree) Hold(ctx context.Context, _ string, fn func(context.Context) error) (bool, error) {
	return true, fn(ctx)
}
