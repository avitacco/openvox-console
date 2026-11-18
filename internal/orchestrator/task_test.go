package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
)

// fakeCatalog serves a few tasks from "production" (and "staging"),
// shaped like openvoxserver's own (see internal/taskcatalog's recorded
// fixtures).
type fakeCatalog struct{}

func fakeTaskFile(name string) taskcatalog.File {
	var f taskcatalog.File
	f.Filename = name
	f.SHA256 = strings.Repeat("a", 64)
	f.SizeBytes = 10
	f.URI.Path = "/puppet/v3/file_content/tasks/mod/" + name
	f.URI.Params = map[string]string{"environment": "production"}
	return f
}

func (fakeCatalog) Task(_ context.Context, environment, name string) (taskcatalog.Task, error) {
	if environment != "production" && environment != "staging" {
		return taskcatalog.Task{}, &taskcatalog.NotFoundError{Kind: "environment", Name: environment}
	}
	switch name {
	case "mod::echo", "verify", "package":
		return taskcatalog.Task{
			Name: name, Environment: environment,
			Metadata: taskcatalog.Metadata{Parameters: map[string]taskcatalog.Parameter{
				"message": {Type: "Optional[String]"},
				"name":    {Type: "Optional[String]"},
				"count":   {Type: "Integer", Default: json.RawMessage(`1`)},
			}},
			Files: []taskcatalog.File{fakeTaskFile("echo.sh")},
		}, nil
	case "mod::secret":
		return taskcatalog.Task{
			Name: name, Environment: environment,
			Metadata: taskcatalog.Metadata{Parameters: map[string]taskcatalog.Parameter{
				"user":     {Type: "String"},
				"password": {Type: "String[1]", Sensitive: true},
			}},
			Files: []taskcatalog.File{fakeTaskFile("secret.sh")},
		}, nil
	}
	return taskcatalog.Task{}, &taskcatalog.NotFoundError{Kind: "task", Name: name}
}

// recordingTransport captures every dispatch's payload and answers with
// respond's result.
type recordingTransport struct {
	mu       sync.Mutex
	payloads [][]byte
	respond  func(req requestData) ([]byte, error)
}

func (rt *recordingTransport) Dispatch(_ context.Context, _ string, payload []byte, _ time.Duration) ([]byte, error) {
	rt.mu.Lock()
	rt.payloads = append(rt.payloads, payload)
	rt.mu.Unlock()
	var req requestData
	_ = json.Unmarshal(payload, &req)
	return rt.respond(req)
}

func (rt *recordingTransport) all() [][]byte {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return append([][]byte(nil), rt.payloads...)
}

// taskResponse builds a node agent's reply to a task request.
func taskResponse(stdout, stderr string, exitCode int, result string) func(requestData) ([]byte, error) {
	return func(requestData) ([]byte, error) {
		return json.Marshal(wireResponse{Result: &responseData{
			Output: outputData{Stdout: stdout, Stderr: stderr, ExitCode: exitCode},
			Result: json.RawMessage(result),
		}})
	}
}

// taskHarness is Handlers and a Dispatcher over a real Postgres store, a
// fakeCatalog and a recordingTransport, with every activity and audit
// event captured. actor is unique per harness, so a test can count the
// jobs it created.
type taskHarness struct {
	store     *Store
	handlers  *Handlers
	transport *recordingTransport
	actor     string

	mu       sync.Mutex
	activity []string
	audit    []auditlog.Event
}

func newTaskHarness(t *testing.T, respond func(requestData) ([]byte, error)) *taskHarness {
	t.Helper()
	h := &taskHarness{
		store:     testStore(t),
		transport: &recordingTransport{respond: respond},
		actor:     "task-test-" + time.Now().Format("150405.000000000"),
	}
	d := NewDispatcher(h.store, h.transport, nil)
	d.SetActivityRecorder(func(action, actor, summary string) {
		h.mu.Lock()
		h.activity = append(h.activity, action+" "+summary)
		h.mu.Unlock()
	})
	d.SetAuditRecorder(func(e auditlog.Event) {
		h.mu.Lock()
		h.audit = append(h.audit, e)
		h.mu.Unlock()
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.Run(ctx)
	h.handlers = NewHandlers(h.store, d, func(*http.Request) string { return h.actor }, noopRecordAuditRead)
	h.handlers.SetTaskCatalog(fakeCatalog{})
	return h
}

func (h *taskHarness) post(t *testing.T, handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(data)))
	return rec
}

func (h *taskHarness) jobCount(t *testing.T) int {
	t.Helper()
	_, total, err := h.store.ListJobs(context.Background(), 1, 10, JobFilter{TriggeredBy: h.actor})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

func decodeJob(t *testing.T, rec *httptest.ResponseRecorder) Job {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var job Job
	if err := json.NewDecoder(rec.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	return job
}

func TestTriggerTask_ResolvesFromProductionByDefault(t *testing.T) {
	h := newTaskHarness(t, taskResponse(`{"ok":true}`, "", 0, `{"ok":true}`))
	job := decodeJob(t, h.post(t, h.handlers.triggerTask, triggerTaskRequest{
		Targets: []string{"web01.example.com"}, Task: "mod::echo", Params: json.RawMessage(`{"message":"hi"}`),
	}))
	if job.Environment != "production" {
		t.Errorf("Environment = %q, want production", job.Environment)
	}
	waitForJobStatus(t, h.store, job.ID, StatusSucceeded)

	payloads := h.transport.all()
	if len(payloads) != 1 {
		t.Fatalf("got %d dispatches, want 1", len(payloads))
	}
	var req requestData
	_ = json.Unmarshal(payloads[0], &req)
	var sent taskcatalog.Resolved
	if err := json.Unmarshal(req.Params, &sent); err != nil {
		t.Fatalf("decode dispatched task: %v", err)
	}
	if req.Action != actionRunTask || sent.Task != "mod::echo" || sent.Environment != "production" ||
		len(sent.Files) != 1 || len(sent.Implementations) != 1 || sent.TimeoutSeconds != int(DefaultTaskTimeout/time.Second) {
		t.Errorf("dispatched = %s %+v", req.Action, sent)
	}
	if sent.Params["message"] != "hi" || sent.Params["count"] != float64(1) {
		t.Errorf("dispatched params = %v, want the given message and the declared default count", sent.Params)
	}
}

func TestTriggerTask_NamedEnvironment(t *testing.T) {
	h := newTaskHarness(t, taskResponse(`{}`, "", 0, `{}`))
	job := decodeJob(t, h.post(t, h.handlers.triggerTask, triggerTaskRequest{
		Targets: []string{"web01.example.com"}, Task: "mod::echo", Environment: "staging",
	}))
	stored, err := h.store.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Environment != "staging" {
		t.Errorf("stored Environment = %q, want staging", stored.Environment)
	}
}

func TestTriggerTask_RefusedRequestsCreateNoJob(t *testing.T) {
	for name, tc := range map[string]struct {
		req        triggerTaskRequest
		wantStatus int
		wantInBody string
	}{
		"unknown task": {
			triggerTaskRequest{Targets: []string{"a"}, Task: "mod::nope"},
			http.StatusBadRequest, "mod::nope",
		},
		"unknown environment": {
			triggerTaskRequest{Targets: []string{"a"}, Task: "mod::echo", Environment: "nope"},
			http.StatusBadRequest, "nope",
		},
		"missing required parameter": {
			triggerTaskRequest{Targets: []string{"a"}, Task: "mod::secret", Params: json.RawMessage(`{"user":"x"}`)},
			http.StatusBadRequest, `"fields":{"password":"required"}`,
		},
		"wrong type": {
			triggerTaskRequest{Targets: []string{"a"}, Task: "mod::echo", Params: json.RawMessage(`{"count":"three"}`)},
			http.StatusBadRequest, `"fields":{"count":`,
		},
		"timeout too long": {
			triggerTaskRequest{Targets: []string{"a"}, Task: "mod::echo", TimeoutSeconds: int(MaxTaskTimeout/time.Second) + 1},
			http.StatusBadRequest, "timeoutSeconds",
		},
		"negative timeout": {
			triggerTaskRequest{Targets: []string{"a"}, Task: "mod::echo", TimeoutSeconds: -5},
			http.StatusBadRequest, "timeoutSeconds",
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newTaskHarness(t, taskResponse(`{}`, "", 0, `{}`))
			rec := h.post(t, h.handlers.triggerTask, tc.req)
			if rec.Code != tc.wantStatus || !strings.Contains(rec.Body.String(), tc.wantInBody) {
				t.Errorf("status %d body %s, want %d mentioning %s", rec.Code, rec.Body, tc.wantStatus, tc.wantInBody)
			}
			if n := h.jobCount(t); n != 0 {
				t.Errorf("%d jobs created, want none", n)
			}
		})
	}
}

func TestTriggerTask_NotConfigured(t *testing.T) {
	h := newTaskHarness(t, taskResponse(`{}`, "", 0, `{}`))
	var none *taskcatalog.Client
	h.handlers.SetTaskCatalog(none)
	rec := h.post(t, h.handlers.triggerTask, triggerTaskRequest{Targets: []string{"a"}, Task: "mod::echo"})
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "not configured") {
		t.Errorf("status %d body %s, want 503 not configured", rec.Code, rec.Body)
	}
	h.handlers.SetTaskCatalog(nil)
	if rec := h.post(t, h.handlers.triggerTask, triggerTaskRequest{Targets: []string{"a"}, Task: "mod::echo"}); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d with no catalogue at all, want 503", rec.Code)
	}
	if n := h.jobCount(t); n != 0 {
		t.Errorf("%d jobs created, want none", n)
	}
}

func TestTriggerTask_SensitiveValueIsNeverStored(t *testing.T) {
	const secret = "hunter2-very-secret"
	h := newTaskHarness(t, taskResponse(`{"ok":true}`, "", 0, `{"ok":true}`))
	rec := h.post(t, h.handlers.triggerTask, triggerTaskRequest{
		Targets: []string{"web01.example.com"}, Task: "mod::secret",
		Params: json.RawMessage(`{"user":"admin","password":"` + secret + `"}`),
	})
	if strings.Contains(rec.Body.String(), secret) {
		t.Errorf("trigger response contains the secret: %s", rec.Body)
	}
	job := decodeJob(t, rec)
	waitForJobStatus(t, h.store, job.ID, StatusSucceeded)

	// The node got the real value.
	payloads := h.transport.all()
	if len(payloads) != 1 || !strings.Contains(string(payloads[0]), secret) {
		t.Fatalf("dispatched payload does not carry the real value: %s", payloads)
	}

	// Stored and served params are redacted.
	stored, err := h.store.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	var params map[string]any
	_ = json.Unmarshal(stored.Params, &params)
	if params["password"] != RedactedValue || params["user"] != "admin" {
		t.Errorf("stored params = %v", params)
	}
	getRec := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getReq.SetPathValue("id", jsonInt(job.ID))
	h.handlers.getJob(getRec, getReq)
	if strings.Contains(getRec.Body.String(), secret) || !strings.Contains(getRec.Body.String(), RedactedValue) {
		t.Errorf("GET job = %s, want redacted", getRec.Body)
	}

	// Nor in the database at all, in any column.
	var hits int
	if err := h.store.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM jobs j JOIN job_targets jt ON jt.job_id = j.id
		 WHERE j.id = $1 AND (j::text LIKE '%' || $2 || '%' OR jt::text LIKE '%' || $2 || '%')`,
		job.ID, secret,
	).Scan(&hits); err != nil {
		t.Fatal(err)
	}
	if hits != 0 {
		t.Errorf("the secret appears in %d stored rows", hits)
	}

	// Nor in activity or audit.
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, a := range h.activity {
		if strings.Contains(a, secret) {
			t.Errorf("activity entry contains the secret: %s", a)
		}
	}
	for _, e := range h.audit {
		if b, _ := json.Marshal(e); strings.Contains(string(b), secret) {
			t.Errorf("audit event contains the secret: %s", b)
		}
	}
}

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestTaskResults(t *testing.T) {
	for name, tc := range map[string]struct {
		stdout, stderr string
		exit           int
		result         string
		wantStatus     string
		wantDetail     string
	}{
		"json result": {
			`{"status":"ok"}`, "", 0, `{"status":"ok"}`, StatusSucceeded, "",
		},
		"plain-text result": {
			"done\n", "", 0, `{"_output":"done\n"}`, StatusSucceeded, "",
		},
		"error result": {
			`{"_error":{"msg":"it broke","kind":"x"}}`, "trace", 1, `{"_error":{"msg":"it broke","kind":"x"}}`, StatusFailed, "it broke\ntrace",
		},
		"_error with exit zero": {
			`{"_error":{"msg":"soft failure"}}`, "", 0, `{"_error":{"msg":"soft failure"}}`, StatusFailed, "soft failure",
		},
		"non-zero exit without _error": {
			"oops", "", 3, `{"_output":"oops"}`, StatusFailed, "task exited with code 3",
		},
		"exit 2 is a failure for a task": {
			`{}`, "", 2, `{}`, StatusFailed, "task exited with code 2",
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newTaskHarness(t, taskResponse(tc.stdout, tc.stderr, tc.exit, tc.result))
			job := decodeJob(t, h.post(t, h.handlers.triggerTask, triggerTaskRequest{
				Targets: []string{"web01.example.com"}, Task: "mod::echo",
			}))
			final := waitForJobStatus(t, h.store, job.ID, tc.wantStatus)
			target := final.Targets[0]
			if target.Status != tc.wantStatus || target.ErrorDetail != tc.wantDetail {
				t.Errorf("target = %s %q, want %s %q", target.Status, target.ErrorDetail, tc.wantStatus, tc.wantDetail)
			}
			var got, want any
			_ = json.Unmarshal(target.Result, &got)
			_ = json.Unmarshal([]byte(tc.result), &want)
			if gb, _ := json.Marshal(got); string(gb) != mustMarshal(want) {
				t.Errorf("result = %s, want %s", target.Result, tc.result)
			}
			if target.ExitCode == nil || *target.ExitCode != tc.exit {
				t.Errorf("exit code = %v, want %d", target.ExitCode, tc.exit)
			}
		})
	}
}

func mustMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestTaskBusyRejectionFailsTheTarget(t *testing.T) {
	h := newTaskHarness(t, func(requestData) ([]byte, error) {
		return json.Marshal(wireResponse{Error: "node-agent is busy executing another request"})
	})
	job := decodeJob(t, h.post(t, h.handlers.triggerTask, triggerTaskRequest{Targets: []string{"a.example.com"}, Task: "mod::echo"}))
	final := waitForJobStatus(t, h.store, job.ID, StatusFailed)
	if !strings.Contains(final.Targets[0].ErrorDetail, "busy") {
		t.Errorf("error detail = %q", final.Targets[0].ErrorDetail)
	}
}

func TestTriggerPlan_InvalidTaskStepCreatesNothing(t *testing.T) {
	h := newTaskHarness(t, taskResponse(`{}`, "", 0, `{}`))
	rec := h.post(t, h.handlers.triggerPlan, triggerPlanRequest{
		Name: "bad-plan", Targets: []string{"a.example.com"},
		Steps: []PlanStep{
			{Kind: JobKindTask, TaskName: "mod::echo"},
			{Kind: JobKindTask, TaskName: "mod::nope"},
		},
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "step 2") || !strings.Contains(rec.Body.String(), "mod::nope") {
		t.Errorf("status %d body %s, want 400 naming step 2", rec.Code, rec.Body)
	}
	if n := h.jobCount(t); n != 0 {
		t.Errorf("%d jobs created, want none", n)
	}
	if len(h.transport.all()) != 0 {
		t.Error("something was dispatched for a refused plan")
	}
}

func TestTriggerPlan_TaskStepsGetResolvedPayloadsInOrder(t *testing.T) {
	const secret = "plan-secret-value"
	h := newTaskHarness(t, func(req requestData) ([]byte, error) {
		if req.Action == actionRun {
			return json.Marshal(wireResponse{Result: &responseData{Output: outputData{ExitCode: 0}}})
		}
		return taskResponse(`{}`, "", 0, `{}`)(req)
	})
	plan := decodeJob(t, h.post(t, h.handlers.triggerPlan, triggerPlanRequest{
		Name: "good-plan", Targets: []string{"a.example.com"},
		Steps: []PlanStep{
			{Kind: JobKindTask, TaskName: "mod::echo", Params: json.RawMessage(`{"message":"first"}`)},
			{Kind: JobKindRun},
			{Kind: JobKindTask, TaskName: "mod::secret", Params: json.RawMessage(`{"user":"u","password":"` + secret + `"}`)},
		},
	}))
	waitForJobStatus(t, h.store, plan.ID, StatusSucceeded)

	payloads := h.transport.all()
	if len(payloads) != 3 {
		t.Fatalf("got %d dispatches, want 3", len(payloads))
	}
	order := []string{}
	for _, p := range payloads {
		var req requestData
		_ = json.Unmarshal(p, &req)
		if req.Action == actionRun {
			order = append(order, "run")
			continue
		}
		var sent taskcatalog.Resolved
		_ = json.Unmarshal(req.Params, &sent)
		order = append(order, sent.Task)
	}
	if strings.Join(order, ",") != "mod::echo,run,mod::secret" {
		t.Errorf("dispatch order = %v", order)
	}
	if !strings.Contains(string(payloads[2]), secret) {
		t.Error("the plan's sensitive value did not reach the node")
	}

	steps, err := h.store.GetPlanSteps(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].Environment != "production" || strings.Contains(string(steps[2].Params), secret) {
		t.Errorf("steps = %+v", steps)
	}
}

func TestPlanFailingFastDropsLaterTaskPayloads(t *testing.T) {
	h := newTaskHarness(t, taskResponse(`{}`, "", 1, `{}`))
	plan := decodeJob(t, h.post(t, h.handlers.triggerPlan, triggerPlanRequest{
		Name: "fail-plan", Targets: []string{"a.example.com"},
		Steps: []PlanStep{
			{Kind: JobKindTask, TaskName: "mod::echo"},
			{Kind: JobKindTask, TaskName: "mod::secret", Params: json.RawMessage(`{"user":"u","password":"p"}`)},
		},
	}))
	waitForJobStatus(t, h.store, plan.ID, StatusFailed)
	deadline := time.Now().Add(setupWait)
	for time.Now().Before(deadline) {
		h.handlers.dispatcher.mu.Lock()
		left := len(h.handlers.dispatcher.taskPayloads)
		h.handlers.dispatcher.mu.Unlock()
		if left == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("task payloads are still held after the plan failed")
}

// Guard against the payload map leaking on the ordinary path too.
func TestCompletedTaskDropsItsPayload(t *testing.T) {
	h := newTaskHarness(t, taskResponse(`{}`, "", 0, `{}`))
	job := decodeJob(t, h.post(t, h.handlers.triggerTask, triggerTaskRequest{Targets: []string{"a.example.com"}, Task: "mod::echo"}))
	waitForJobStatus(t, h.store, job.ID, StatusSucceeded)
	// The payload is dropped just after the job is marked complete.
	deadline := time.Now().Add(setupWait)
	for time.Now().Before(deadline) {
		h.handlers.dispatcher.mu.Lock()
		_, held := h.handlers.dispatcher.taskPayloads[job.ID]
		h.handlers.dispatcher.mu.Unlock()
		if !held {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("payload still held after the job completed")
}

// testPayload is a minimal resolved task, for tests that dispatch a plan
// directly rather than through triggerPlan.
func testPayload(name string) taskcatalog.Resolved {
	return taskcatalog.Resolved{
		Task: name, Environment: "production",
		Implementations: []taskcatalog.ResolvedImplementation{{Name: "task.sh"}},
		Files:           []taskcatalog.ResolvedFile{{Filename: "task.sh", Path: "/puppet/v3/file_content/tasks/mod/task.sh", SHA256: strings.Repeat("a", 64)}},
		Params:          map[string]any{},
		TimeoutSeconds:  60,
	}
}
