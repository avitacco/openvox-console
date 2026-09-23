package orchestrator

import (
	"context"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

// Job history is durable, not instance-local: a job dispatched through
// one instance reads identically from any other, because both read the
// same rows. This is what lets a load balancer route job-history
// requests freely.
func TestJobHistoryReadsIdenticallyFromAnyInstance(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()

	// Two stores over one database stand in for two instances: each
	// instance builds its own Store over the shared pool.
	instanceA := NewStore(pool)
	instanceB := NewStore(pool)

	job, err := instanceA.CreateJob(ctx, JobKindRun, "", "", nil,
		[]string{"a.example.com", "b.example.com"}, "cluster-history-test")
	if err != nil {
		t.Fatalf("CreateJob() on instance A: %v", err)
	}

	fromA, err := instanceA.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() on instance A: %v", err)
	}
	fromB, err := instanceB.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() on instance B: %v", err)
	}

	if fromA.Status != fromB.Status {
		t.Errorf("status differs between instances: A=%q B=%q", fromA.Status, fromB.Status)
	}
	if len(fromA.Targets) != len(fromB.Targets) {
		t.Fatalf("target count differs between instances: A=%d B=%d", len(fromA.Targets), len(fromB.Targets))
	}
	for i := range fromA.Targets {
		if fromA.Targets[i].Certname != fromB.Targets[i].Certname ||
			fromA.Targets[i].Status != fromB.Targets[i].Status {
			t.Errorf("target %d differs between instances: A=%+v B=%+v", i, fromA.Targets[i], fromB.Targets[i])
		}
	}

	// A terminal outcome recorded by one instance is visible to the
	// other - the case that matters when the instance that dispatched a
	// job is not the one an operator's request reaches.
	if err := instanceA.RecordTargetResult(ctx, job.ID, "a.example.com", StatusSucceeded, nil, "done", ""); err != nil {
		t.Fatalf("RecordTargetResult() on instance A: %v", err)
	}
	fromB, err = instanceB.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetJob() on instance B after completion: %v", err)
	}
	for _, target := range fromB.Targets {
		if target.Certname == "a.example.com" && target.Status != StatusSucceeded {
			t.Errorf("instance B reports target status %q after instance A completed it, want %q",
				target.Status, StatusSucceeded)
		}
	}
}
