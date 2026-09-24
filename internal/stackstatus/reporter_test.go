package stackstatus_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/stackstatus"
)

type fakeChecker struct {
	name  string
	err   error
	delay time.Duration
}

func (f fakeChecker) Name() string { return f.name }

func (f fakeChecker) Check(ctx context.Context) error {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

func baseConfig() stackstatus.Config {
	return stackstatus.Config{
		Mode:    "web",
		Address: "10.0.0.1:8080",
		Version: "v1.2.3",
		Workers: []string{"dependency-health", "status-responder"},
		View: func() stackstatus.ClusterView {
			return stackstatus.ClusterView{
				Attachment:      stackstatus.AttachmentRouted,
				RoutedPeers:     2,
				LeafConnections: 1,
			}
		},
	}
}

// Every field the spec names must be populated - a status page missing
// any of them sends the reader back to the deployment config.
func TestReportPopulatesEveryField(t *testing.T) {
	r := stackstatus.NewReporter(baseConfig())
	got := r.Report(context.Background())

	if got.ID == "" {
		t.Error("ID is empty")
	}
	if got.Hostname == "" {
		t.Error("Hostname is empty")
	}
	if got.Mode != "web" {
		t.Errorf("Mode = %q, want %q", got.Mode, "web")
	}
	if got.Address != "10.0.0.1:8080" {
		t.Errorf("Address = %q", got.Address)
	}
	if got.Version != "v1.2.3" {
		t.Errorf("Version = %q", got.Version)
	}
	if got.StartedAt.IsZero() {
		t.Error("StartedAt is zero")
	}
	if len(got.Workers) != 2 {
		t.Errorf("Workers = %v, want two", got.Workers)
	}
	if got.Cluster.Attachment != stackstatus.AttachmentRouted ||
		got.Cluster.RoutedPeers != 2 || got.Cluster.LeafConnections != 1 {
		t.Errorf("Cluster = %+v, want the reported view", got.Cluster)
	}
	if got.Health != stackstatus.HealthOK {
		t.Errorf("Health = %q with no dependencies, want %q", got.Health, stackstatus.HealthOK)
	}
}

// Each instance gets its own identity, so two on one host with the same
// address are still distinguishable.
func TestEachReporterHasItsOwnID(t *testing.T) {
	if stackstatus.NewReporter(baseConfig()).ID() == stackstatus.NewReporter(baseConfig()).ID() {
		t.Error("two reporters share an ID")
	}
}

func TestReportMarksAnUnreachableDependency(t *testing.T) {
	cfg := baseConfig()
	cfg.Dependencies = []stackstatus.Checker{fakeChecker{name: "openvoxdb", err: errors.New("connection refused")}}
	cfg.Targets = map[string]string{"openvoxdb": "https://openvoxdb:8081"}

	got := stackstatus.NewReporter(cfg).Report(context.Background())

	if got.Health != stackstatus.HealthDegraded {
		t.Errorf("Health = %q with an unreachable dependency, want %q", got.Health, stackstatus.HealthDegraded)
	}
	if len(got.Dependencies) != 1 {
		t.Fatalf("Dependencies = %v, want one", got.Dependencies)
	}
	dep := got.Dependencies[0]
	if dep.Health != stackstatus.HealthUnreachable {
		t.Errorf("dependency health = %q, want %q", dep.Health, stackstatus.HealthUnreachable)
	}
	if dep.Target != "https://openvoxdb:8081" {
		t.Errorf("dependency target = %q, want the configured target so a misconfiguration is distinguishable from an outage", dep.Target)
	}
	if dep.Detail == "" {
		t.Error("an unreachable dependency carries no detail")
	}
}

// Not-configured is a deployment choice, not a fault, and must not make
// the instance look unhealthy.
func TestUnconfiguredDependencyDoesNotDegradeTheInstance(t *testing.T) {
	cfg := baseConfig()
	cfg.Dependencies = []stackstatus.Checker{fakeChecker{name: "ca-client", err: errors.New("never called")}}
	cfg.Targets = map[string]string{} // not configured

	got := stackstatus.NewReporter(cfg).Report(context.Background())

	if got.Health != stackstatus.HealthOK {
		t.Errorf("Health = %q with only an unconfigured dependency, want %q", got.Health, stackstatus.HealthOK)
	}
	if got.Dependencies[0].Health != stackstatus.HealthNotConfigured {
		t.Errorf("dependency health = %q, want %q", got.Dependencies[0].Health, stackstatus.HealthNotConfigured)
	}
	if got.Dependencies[0].Target != "" {
		t.Errorf("an unconfigured dependency reports target %q, want none", got.Dependencies[0].Target)
	}
}

// The case that matters most: a hanging dependency must yield a degraded
// instance, not a silent one. Silence would remove the instance from the
// page entirely, which is strictly worse than reporting it unwell.
func TestASlowDependencyYieldsADegradedInstanceNotSilence(t *testing.T) {
	cfg := baseConfig()
	cfg.Dependencies = []stackstatus.Checker{fakeChecker{name: "postgres", delay: 30 * time.Second}}
	cfg.Targets = map[string]string{"postgres": "postgres://db/console"}

	start := time.Now()
	got := stackstatus.NewReporter(cfg).Report(context.Background())
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("Report() took %s; a hanging dependency blocked the reply past any sane collection window", elapsed)
	}
	if got.Health != stackstatus.HealthDegraded {
		t.Errorf("Health = %q with a hanging dependency, want %q", got.Health, stackstatus.HealthDegraded)
	}
	if got.ID == "" {
		t.Error("the instance did not describe itself at all")
	}
}

// Workers are copied, so a caller mutating the slice it passed cannot
// change what later reports say.
func TestReportedWorkersAreNotAliased(t *testing.T) {
	cfg := baseConfig()
	workers := []string{"dependency-health"}
	cfg.Workers = workers

	r := stackstatus.NewReporter(cfg)
	workers[0] = "mutated"

	if got := r.Report(context.Background()).Workers[0]; got != "dependency-health" {
		t.Errorf("reported worker = %q; the reporter aliased its caller's slice", got)
	}
}
