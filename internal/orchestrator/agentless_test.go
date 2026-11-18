package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// scriptedTransport answers every dispatch with a fixed task result and
// remembers whom it was asked.
type scriptedTransport struct {
	mu     sync.Mutex
	asked  []string
	stdout string
}

func (s *scriptedTransport) Dispatch(_ context.Context, certname string, _ []byte, _ time.Duration) ([]byte, error) {
	s.mu.Lock()
	s.asked = append(s.asked, certname)
	s.mu.Unlock()
	return json.Marshal(wireResponse{Result: &responseData{Output: outputData{Stdout: s.stdout}, Result: json.RawMessage(`{"from":"` + s.stdout + `"}`)}})
}

func (s *scriptedTransport) askedOf() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

func agentlessNames(names ...string) AgentlessLookup {
	return func(_ context.Context, certnames []string) []string {
		var out []string
		for _, c := range certnames {
			if slices.Contains(names, c) {
				out = append(out, c)
			}
		}
		return out
	}
}

func routedHandlers(t *testing.T, agentless ...string) (*Handlers, *scriptedTransport, *scriptedTransport) {
	t.Helper()
	store := testStore(t)
	agent, ssh := &scriptedTransport{stdout: "agent"}, &scriptedTransport{stdout: "ssh"}
	routed := NewRoutedTransport(agent, ssh, func(_ context.Context, c string) bool { return slices.Contains(agentless, c) })
	d := NewDispatcher(store, routed, nil)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.Run(ctx)
	h := NewHandlers(store, d, noopActor, noopRecordAuditRead)
	h.SetTaskCatalog(fakeCatalog{})
	h.SetAgentless(agentlessNames(agentless...))
	return h, agent, ssh
}

func TestAMixedTaskJobGivesEachTargetItsOwnResult(t *testing.T) {
	a, l := uniq("mix-agent"), uniq("mix-agentless")
	h, agent, ssh := routedHandlers(t, l)
	rec := post(h.triggerTask, triggerTaskRequest{Targets: []string{a, l}, Task: "mod::echo"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var job Job
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := h.store.GetJob(context.Background(), job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != StatusRunning {
			job = got
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if job.Status != StatusSucceeded {
		t.Fatalf("job = %s", job.Status)
	}
	if !slices.Equal(agent.askedOf(), []string{a}) || !slices.Equal(ssh.askedOf(), []string{l}) {
		t.Errorf("agent asked %v, ssh asked %v", agent.askedOf(), ssh.askedOf())
	}
	for _, tg := range job.Targets {
		want := `"agent"`
		if tg.Certname == l {
			want = `"ssh"`
		}
		if !strings.Contains(string(tg.Result), want) {
			t.Errorf("%s result = %s", tg.Certname, tg.Result)
		}
	}
}

func TestRequestsThatNeedTheAgentAreRefusedForAgentlessTargets(t *testing.T) {
	a, l := uniq("ref-agent"), uniq("ref-agentless")
	h, agent, ssh := routedHandlers(t, l)
	for name, rec := range map[string]func() *httptest.ResponseRecorder{
		"puppet run": func() *httptest.ResponseRecorder {
			return post(h.triggerRun, triggerRunRequest{Targets: []string{a, l}})
		},
		"patch run": func() *httptest.ResponseRecorder {
			return post(h.triggerPatch, triggerPatchRequest{Targets: []string{a, l}})
		},
		"plan with a run step": func() *httptest.ResponseRecorder {
			return post(h.triggerPlan, triggerPlanRequest{Targets: []string{a, l}, Name: "p", Steps: []PlanStep{{Kind: JobKindTask, TaskName: "mod::echo"}, {Kind: JobKindRun}}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := rec()
			var body map[string]any
			_ = json.Unmarshal(r.Body.Bytes(), &body)
			if r.Code != http.StatusBadRequest || !strings.Contains(r.Body.String(), l) || !strings.Contains(r.Body.String(), "only run tasks") {
				t.Fatalf("status %d: %s", r.Code, r.Body)
			}
			if strings.Contains(r.Body.String(), a) {
				t.Errorf("the refusal names a node that has an agent: %s", r.Body)
			}
		})
	}
	if jobsNaming(t, h, l) != 0 || jobsNaming(t, h, a) != 0 {
		t.Error("a refused request created a job")
	}
	if len(agent.askedOf())+len(ssh.askedOf()) != 0 {
		t.Error("something was sent for a refused request")
	}
}

func TestAPlanOfTasksRunsOnAnAgentlessTarget(t *testing.T) {
	l := uniq("plan-agentless")
	h, _, ssh := routedHandlers(t, l)
	rec := post(h.triggerPlan, triggerPlanRequest{Targets: []string{l}, Name: "p", Steps: []PlanStep{{Kind: JobKindTask, TaskName: "mod::echo"}}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(ssh.askedOf()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !slices.Equal(ssh.askedOf(), []string{l}) {
		t.Errorf("ssh asked %v", ssh.askedOf())
	}
}
