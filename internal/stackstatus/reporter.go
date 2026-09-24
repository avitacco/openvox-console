package stackstatus

import (
	"context"
	"os"
	"time"

	"github.com/google/uuid"
)

// checkTimeout bounds each dependency check an instance runs while
// describing itself.
//
// Deliberately shorter than the collection window the aggregator allows.
// An instance whose Postgres is hanging must still answer - reporting
// itself degraded - rather than blocking past the deadline and being
// reported as not replying at all. "Degraded" and "silent" mean very
// different things to an operator, and a slow dependency should produce
// the first.
const checkTimeout = 400 * time.Millisecond

// Reporter builds this instance's own status.
type Reporter struct {
	id        string
	hostname  string
	mode      string
	address   string
	version   string
	workers   []string
	startedAt time.Time

	deps    []Checker
	targets map[string]string

	// view returns this instance's live cluster view. A function rather
	// than a value because peers come and go while the process runs.
	view func() ClusterView

	// now and checkTimeout are overridable in tests.
	now   func() time.Time
	limit time.Duration
}

// Config describes the instance a Reporter reports on.
type Config struct {
	Mode    string
	Address string
	Version string
	Workers []string

	// Dependencies are the external services to check. Their Name()
	// keys into Targets.
	Dependencies []Checker

	// Targets maps a dependency's name to what this instance is
	// configured to reach it at. A name absent from this map, or mapped
	// to an empty string, is reported as not configured.
	Targets map[string]string

	// View returns the live cluster view.
	View func() ClusterView
}

// NewReporter builds a Reporter for this process. The instance id is
// generated once per process.
func NewReporter(cfg Config) *Reporter {
	hostname, _ := os.Hostname()

	workers := make([]string, len(cfg.Workers))
	copy(workers, cfg.Workers)

	view := cfg.View
	if view == nil {
		view = func() ClusterView { return ClusterView{Attachment: AttachmentStandalone} }
	}

	return &Reporter{
		id:        uuid.NewString(),
		hostname:  hostname,
		mode:      cfg.Mode,
		address:   cfg.Address,
		version:   cfg.Version,
		workers:   workers,
		startedAt: time.Now().UTC(),
		deps:      cfg.Dependencies,
		targets:   cfg.Targets,
		view:      view,
		now:       func() time.Time { return time.Now().UTC() },
		limit:     checkTimeout,
	}
}

// ID returns this instance's generated identity.
func (r *Reporter) ID() string { return r.id }

// Report describes this instance right now, running every dependency
// check.
//
// It always returns an Instance. A failing or hanging dependency makes
// the instance degraded; it never makes the instance silent, because an
// instance that cannot describe itself is absent from the stack's status
// while still running - the most misleading answer available.
func (r *Reporter) Report(ctx context.Context) Instance {
	inst := Instance{
		ID:        r.id,
		Hostname:  r.hostname,
		Mode:      r.mode,
		Address:   r.address,
		Health:    HealthOK,
		StartedAt: r.startedAt,
		Version:   r.version,
		Workers:   r.workers,
		Cluster:   r.view(),
	}

	for _, dep := range r.deps {
		d := r.check(ctx, dep)
		inst.Dependencies = append(inst.Dependencies, d)

		// Not-configured is a deployment choice, not a fault, so it
		// does not degrade the instance.
		if d.Health == HealthUnreachable {
			inst.Health = HealthDegraded
		}
	}

	return inst
}

// check runs one dependency check under its own bounded timeout.
func (r *Reporter) check(ctx context.Context, dep Checker) Dependency {
	name := dep.Name()
	target := r.targets[name]

	if target == "" {
		return Dependency{Name: name, Health: HealthNotConfigured}
	}

	checkCtx, cancel := context.WithTimeout(ctx, r.limit)
	defer cancel()

	if err := dep.Check(checkCtx); err != nil {
		return Dependency{
			Name:   name,
			Target: target,
			Health: HealthUnreachable,
			Detail: err.Error(),
		}
	}
	return Dependency{Name: name, Target: target, Health: HealthOK}
}
