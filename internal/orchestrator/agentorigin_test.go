package orchestrator

import (
	"context"
	"testing"
)

func TestJobs_RecordTheAgentRequestBehindThem(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	node := uniq("agent-origin")

	from, err := s.CreateRunJobFrom(ctx, RunOptions{}, []string{node}, "bot", JobOrigin{Agent: &AgentRef{RequestID: 7, Token: "bot", ApprovedBy: "alice"}})
	if err != nil {
		t.Fatal(err)
	}
	closeAfter(t, s, from.ID)
	plain, err := s.CreateRunJobFrom(ctx, RunOptions{}, []string{node}, "alice", JobOrigin{})
	if err != nil {
		t.Fatal(err)
	}
	closeAfter(t, s, plain.ID)

	got, err := s.GetJob(ctx, from.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent == nil || *got.Agent != (AgentRef{RequestID: 7, Token: "bot", ApprovedBy: "alice"}) {
		t.Errorf("agent = %+v", got.Agent)
	}
	if got, _ := s.GetJob(ctx, plain.ID); got.Agent != nil {
		t.Errorf("a job a person started shows an agent origin: %+v", got.Agent)
	}

	// The list shows it, and can be limited to such jobs.
	jobs, _, err := s.ListJobs(ctx, 1, 100, JobFilter{Target: node, Agent: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != from.ID || jobs[0].Agent == nil || jobs[0].Agent.ApprovedBy != "alice" {
		t.Errorf("agent jobs = %+v", jobs)
	}
	all, _, _ := s.ListJobs(ctx, 1, 100, JobFilter{Target: node})
	if len(all) != 2 {
		t.Errorf("unfiltered list has %d jobs, want 2", len(all))
	}
}
