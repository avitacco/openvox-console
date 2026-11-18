package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/nodeagent"
	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

func TestDecodeRunOptions(t *testing.T) {
	valid := map[string]RunOptions{
		``:                            {},
		`null`:                        {},
		`{}`:                          {},
		`{"noop":true}`:               {Noop: true},
		`{"environment":"feature_x"}`: {Environment: "feature_x"},
		`{"debug":true,"noop":false}`: {Debug: true},
		`{"noop":true,"environment":"Prod_2","debug":true}`: {Noop: true, Environment: "Prod_2", Debug: true},
	}
	for raw, want := range valid {
		got, err := decodeRunOptions(json.RawMessage(raw))
		if err != nil || got != want {
			t.Errorf("%s: got %+v, %v; want %+v", raw, got, err, want)
		}
	}
	invalid := []string{
		`{"environment":"feature-x"}`,
		`{"environment":"a b"}`,
		`{"environment":"x; rm -rf /"}`,
		`{"environment":"--noop"}`,
		`{"trace":true}`, // an option this console does not know
		`{"noop":"yes"}`,
		`[]`,
	}
	for _, raw := range invalid {
		_, err := decodeRunOptions(json.RawMessage(raw))
		var invalid *InvalidRunOptionsError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want InvalidRunOptionsError", raw, err)
		}
	}
}

func TestRunOptions_Describe(t *testing.T) {
	cases := map[RunOptions]string{
		{}:           "",
		{Noop: true}: "no-op",
		{Noop: true, Environment: "feature_x", Debug: true}: "no-op, environment feature_x, debug",
	}
	for o, want := range cases {
		if got := o.describe(); got != want {
			t.Errorf("%+v: %q, want %q", o, got, want)
		}
	}
}

// optionsTransport stands in for the node transport: it records every
// request and answers per certname.
type optionsTransport struct {
	mu       sync.Mutex
	requests map[string][]byte
	answer   func(certname string, req requestData) []byte
}

func (r *optionsTransport) Dispatch(_ context.Context, certname string, payload []byte, _ time.Duration) ([]byte, error) {
	var req requestData
	_ = json.Unmarshal(payload, &req)
	r.mu.Lock()
	if r.requests == nil {
		r.requests = map[string][]byte{}
	}
	r.requests[certname] = payload
	r.mu.Unlock()
	return r.answer(certname, req), nil
}

func (r *optionsTransport) sent(certname string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.requests[certname])
}

func runAnswer(stdout string) []byte {
	data, _ := json.Marshal(wireResponse{Result: &responseData{Output: outputData{Stdout: stdout}}})
	return data
}

// currentClient answers as a client that knows run options does.
func currentClient(_ string, req requestData) []byte {
	return runAnswer("ran " + req.Action)
}

// oldClient answers as a client that predates run options: a plain run
// works, anything else is an action it has never heard of - the real
// client's own wording.
func oldClient(_ string, req requestData) []byte {
	if req.Action == actionRun {
		return runAnswer("ran run")
	}
	data, _ := json.Marshal(wireResponse{Error: `unsupported action "` + req.Action + `"`})
	return data
}

type recordedSummaries struct {
	mu        sync.Mutex
	summaries []string
}

func (r *recordedSummaries) record(_, _, summary string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.summaries = append(r.summaries, summary)
}

// triggered is the summary recorded when the run was triggered - the
// first entry; its outcome is recorded after it.
func (r *recordedSummaries) triggered() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.summaries) == 0 {
		return ""
	}
	return r.summaries[0]
}

type listedEnvironments struct {
	names []string
	err   error
}

func (f listedEnvironments) Environments(context.Context) ([]string, error) { return f.names, f.err }

func optionHandlers(t *testing.T, transport *optionsTransport) (*Handlers, *recordedSummaries) {
	t.Helper()
	store := testStore(t)
	dispatcher := NewDispatcher(store, transport, nil)
	activity := &recordedSummaries{}
	dispatcher.SetActivityRecorder(activity.record)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go dispatcher.Run(ctx)
	return NewHandlers(store, dispatcher, noopActor, noopRecordAuditRead), activity
}

func runBody(targets []string, options string) map[string]any {
	body := map[string]any{"targets": targets}
	if options != "" {
		body["options"] = json.RawMessage(options)
	}
	return body
}

func runJob(t *testing.T, rec *httptest.ResponseRecorder) Job {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var job Job
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	return job
}

// A run with no options is, byte for byte, the request every client
// version has always been sent.
func TestTriggerRun_NoOptionsSendsThePlainRequest(t *testing.T) {
	const node = "runopts-plain.example.com"
	for _, options := range []string{"", "null", "{}", `{"noop":false,"debug":false,"environment":""}`} {
		transport := &optionsTransport{answer: oldClient}
		h, activity := optionHandlers(t, transport)
		job := runJob(t, post(h.triggerRun, runBody([]string{node}, options)))
		if job.RunOptions != nil {
			t.Errorf("options %q: job has run options %+v", options, job.RunOptions)
		}
		done := waitForJobStatus(t, h.store, job.ID, StatusSucceeded)
		if got := transport.sent(node); got != `{"module":"puppet","action":"run"}` {
			t.Errorf("options %q: sent %s", options, got)
		}
		if len(done.Params) != 0 || done.Environment != "" {
			t.Errorf("options %q: stored params %s environment %q", options, done.Params, done.Environment)
		}
		if got := activity.triggered(); got != "triggered a run against "+node {
			t.Errorf("activity = %q", got)
		}
	}
}

func TestTriggerRun_WithOptions(t *testing.T) {
	const node = "runopts-noop.example.com"
	transport := &optionsTransport{answer: func(string, requestData) []byte {
		return runAnswer("Notice: /Stage[main]/Ntp/File[/etc/ntp.conf]/content: current_value '{md5}a', should be '{md5}b' (noop)")
	}}
	h, activity := optionHandlers(t, transport)

	job := runJob(t, post(h.triggerRun, runBody([]string{node}, `{"noop":true,"environment":"feature_x","debug":true}`)))
	want := RunOptions{Noop: true, Environment: "feature_x", Debug: true}
	if job.RunOptions == nil || *job.RunOptions != want || job.Environment != "feature_x" {
		t.Fatalf("job options = %+v, environment %q", job.RunOptions, job.Environment)
	}

	done := waitForJobStatus(t, h.store, job.ID, StatusSucceeded)
	var sent requestData
	if err := json.Unmarshal([]byte(transport.sent(node)), &sent); err != nil {
		t.Fatal(err)
	}
	var sentOptions RunOptions
	_ = json.Unmarshal(sent.Params, &sentOptions)
	if sent.Action != actionRunWithOptions || sentOptions != want {
		t.Errorf("sent action %q params %s", sent.Action, sent.Params)
	}
	// What the run would have changed is on the job.
	if !strings.Contains(done.Targets[0].Output, "(noop)") {
		t.Errorf("output = %q", done.Targets[0].Output)
	}
	if got := activity.triggered(); got != "triggered a run (no-op, environment feature_x, debug) against "+node {
		t.Errorf("activity = %q", got)
	}

	// The jobs list carries the options too, without the raw params.
	jobs, _, err := h.store.ListJobs(context.Background(), 1, 5, JobFilter{Target: node})
	if err != nil || len(jobs) == 0 {
		t.Fatalf("ListJobs: %v (%d)", err, len(jobs))
	}
	if jobs[0].RunOptions == nil || *jobs[0].RunOptions != want || jobs[0].Params != nil {
		t.Errorf("listed job options = %+v params %s", jobs[0].RunOptions, jobs[0].Params)
	}

	rec := httptest.NewRecorder()
	h.listJobs(rec, httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/jobs?target="+node, nil))
	var page pagination.Page[Job]
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Items) == 0 || page.Items[0].RunOptions == nil {
		t.Errorf("list response: %v %s", err, rec.Body)
	}
}

// A client that predates run options refuses the request. The target
// fails saying so, and - the point of the separate action - Puppet is
// not run in any form.
func TestTriggerRun_OptionsAgainstAnOldClientFailWithoutRunning(t *testing.T) {
	const current, old = "runopts-current.example.com", "runopts-old.example.com"
	var ranOnOld []string
	var mu sync.Mutex
	transport := &optionsTransport{answer: func(certname string, req requestData) []byte {
		if certname == old {
			mu.Lock()
			ranOnOld = append(ranOnOld, req.Action)
			mu.Unlock()
			return oldClient(certname, req)
		}
		return currentClient(certname, req)
	}}
	h, _ := optionHandlers(t, transport)

	job := runJob(t, post(h.triggerRun, runBody([]string{current, old}, `{"noop":true}`)))
	done := waitForJobStatus(t, h.store, job.ID, StatusFailed)

	byName := map[string]JobTarget{}
	for _, target := range done.Targets {
		byName[target.Certname] = target
	}
	if byName[current].Status != StatusSucceeded {
		t.Errorf("current client: %+v", byName[current])
	}
	if got := byName[old]; got.Status != StatusFailed || got.ErrorDetail != clientTooOld || got.ExitCode != nil {
		t.Errorf("old client: %+v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ranOnOld) != 1 || ranOnOld[0] != actionRunWithOptions {
		t.Errorf("old client was sent %v; it must only ever see the action it refuses", ranOnOld)
	}
}

// Against the real client: the options reach Puppet's command line.
func TestTriggerRun_OptionsReachTheRealClient(t *testing.T) {
	const node = "runopts-real.example.com"
	var mu sync.Mutex
	var argv []string
	runner := nodeagent.CommandRunner(func(_ context.Context, name string, args ...string) (string, string, int, error) {
		mu.Lock()
		argv = append([]string{name}, args...)
		mu.Unlock()
		return "Notice: would have changed (noop)", "", 0, nil
	})
	store := testStore(t)
	broker, stop := startRealAgent(t, node, runner)
	t.Cleanup(stop)
	dispatcher := NewDispatcher(store, broker, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go dispatcher.Run(ctx)
	h := NewHandlers(store, dispatcher, noopActor, noopRecordAuditRead)

	job := runJob(t, post(h.triggerRun, runBody([]string{node}, `{"noop":true,"environment":"feature_x"}`)))
	done := waitForJobStatus(t, store, job.ID, StatusSucceeded)
	mu.Lock()
	got := strings.Join(argv, " ")
	mu.Unlock()
	if got != "puppet agent --onetime --no-daemonize --detailed-exitcodes --noop --environment feature_x --verbose --color=false" {
		t.Errorf("argv = %q", got)
	}
	if done.Targets[0].Output != "Notice: would have changed (noop)" {
		t.Errorf("output = %q", done.Targets[0].Output)
	}
}

func TestTriggerRun_InvalidOptionsCreateNothing(t *testing.T) {
	const node = "runopts-invalid.example.com"
	transport := &optionsTransport{answer: currentClient}
	h, _ := optionHandlers(t, transport)
	h.SetEnvironments(listedEnvironments{names: []string{"production", "feature_x"}})
	before := jobsNaming(t, h, node)

	for _, options := range []string{
		`{"environment":"feature-x"}`, // not a valid name
		`{"environment":"nosuch"}`,    // valid, but the server has no such environment
		`{"trace":true}`,              // an option this console does not know
	} {
		rec := post(h.triggerRun, runBody([]string{node}, options))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (%s)", options, rec.Code, rec.Body)
		}
	}
	if rec := post(h.triggerRun, runBody([]string{node}, `{"environment":"nosuch"}`)); !strings.Contains(rec.Body.String(), "nosuch") {
		t.Errorf("the refusal should name the environment: %s", rec.Body)
	}
	if after := jobsNaming(t, h, node); after != before {
		t.Errorf("jobs naming the node went from %d to %d", before, after)
	}
	if transport.sent(node) != "" {
		t.Errorf("a refused run was dispatched: %s", transport.sent(node))
	}

	// An environment the server has is accepted.
	runJob(t, post(h.triggerRun, runBody([]string{node}, `{"environment":"feature_x"}`)))
}

// When the server's environments cannot be listed the name is taken on
// trust, rather than runs being refused because a list is unavailable.
func TestTriggerRun_EnvironmentNotCheckedWhenTheListIsUnavailable(t *testing.T) {
	const node = "runopts-nolist.example.com"
	h, _ := optionHandlers(t, &optionsTransport{answer: currentClient})
	h.SetEnvironments(listedEnvironments{err: errors.New("unreachable")})
	runJob(t, post(h.triggerRun, runBody([]string{node}, `{"environment":"anything"}`)))

	h, _ = optionHandlers(t, &optionsTransport{answer: currentClient})
	runJob(t, post(h.triggerRun, runBody([]string{node}, `{"environment":"anything"}`)))
}

// Options change nothing about scope: the refusal is the one a plain
// run gets, and it comes before the options are even looked at.
func TestTriggerRun_OptionsDoNotChangeTheScopeRefusal(t *testing.T) {
	const allowed, hidden = "runopts-allowed.example.com", "runopts-hidden.example.com"
	transport := &optionsTransport{answer: currentClient}
	h, _ := optionHandlers(t, transport)
	h.SetScope(scopedTo(allowed))

	plain := post(h.triggerRun, runBody([]string{allowed, hidden}, ""))
	for _, options := range []string{`{"noop":true}`, `{"environment":"not-valid!"}`, `{"trace":true}`} {
		rec := post(h.triggerRun, runBody([]string{allowed, hidden}, options))
		if rec.Code != http.StatusForbidden || rec.Body.String() != plain.Body.String() {
			t.Errorf("%s: %d %s; want the plain run's refusal %s", options, rec.Code, rec.Body, plain.Body)
		}
	}
	if transport.sent(allowed) != "" || transport.sent(hidden) != "" {
		t.Error("a refused run was dispatched")
	}
}

func TestGetJob_ReportEnvironment(t *testing.T) {
	const used, other, pending = "runopts-env-used.example.com", "runopts-env-other.example.com", "runopts-env-pending.example.com"
	h, _ := optionHandlers(t, &optionsTransport{answer: currentClient})
	ctx := context.Background()

	job := runJob(t, post(h.triggerRun, runBody([]string{used, other, pending}, `{"environment":"feature_x"}`)))
	waitForJobStatus(t, h.store, job.ID, StatusSucceeded)
	if err := h.store.SetTargetReport(ctx, job.ID, used, "hash-used"); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetTargetReport(ctx, job.ID, other, "hash-other"); err != nil {
		t.Fatal(err)
	}

	var asked []string
	h.SetReportEnvironments(func(_ context.Context, hashes []string) (map[string]string, error) {
		asked = hashes
		return map[string]string{"hash-used": "feature_x", "hash-other": "production"}, nil
	})

	get := func(id int64) map[string]string {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetPathValue("id", strconv.FormatInt(id, 10))
		rec := httptest.NewRecorder()
		h.getJob(rec, req)
		envs := map[string]string{}
		for _, target := range runJob(t, rec).Targets {
			envs[target.Certname] = target.ReportEnvironment
		}
		return envs
	}

	envs := get(job.ID)
	if envs[used] != "feature_x" || envs[other] != "production" || envs[pending] != "" {
		t.Errorf("report environments = %v", envs)
	}
	if len(asked) != 2 {
		t.Errorf("asked about %v; a target with no report has nothing to look up", asked)
	}

	// A run that requested no environment has nothing to compare, so
	// the reports are not read at all.
	asked = nil
	plain := runJob(t, post(h.triggerRun, runBody([]string{used}, `{"noop":true}`)))
	waitForJobStatus(t, h.store, plain.ID, StatusSucceeded)
	_ = h.store.SetTargetReport(ctx, plain.ID, used, "hash-used")
	if envs := get(plain.ID); envs[used] != "" || asked != nil {
		t.Errorf("a run with no requested environment: %v, asked %v", envs, asked)
	}

	// Failing to read the reports leaves the job readable.
	h.SetReportEnvironments(func(context.Context, []string) (map[string]string, error) {
		return nil, errors.New("openvoxdb is down")
	})
	if envs := get(job.ID); envs[used] != "" {
		t.Errorf("after a failed lookup: %v", envs)
	}
}
