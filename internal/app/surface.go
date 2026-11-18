package app

import (
	"fmt"
	"slices"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/runtime"
	"github.com/voxpupuli/enterprise-console/internal/stackstatus"
)

// Route group, listener, and worker names. These are the vocabulary the
// mode table is written in: a name here is a unit a mode either activates
// or does not.
//
// They are named rather than referenced as functions so the table stays a
// value that can be read and asserted on directly, without constructing
// any of the subsystems behind them - see design.md's "A mode table, not
// scattered conditionals".
const (
	// routeOperational is health, metrics, and the version endpoint:
	// every mode serves it, so that an instance of any mode can be
	// health-checked and scraped without the checker knowing its mode.
	routeOperational   = "operational"
	routeRBAC          = "rbac"
	routeInventory     = "inventory"
	routeNodeConnect   = "node-connectivity"
	routePackages      = "package-inventory"
	routeReporting     = "reporting"
	routeClassifier    = "classifier"
	routeENC           = "enc"
	routeGroupNodes    = "group-nodes"
	routeVulnerability = "vulnerability"
	routeActivity      = "activity"
	routeCodeManager   = "code-manager"
	routeOrchestrator  = "orchestrator"
	routeAgentDist     = "agent-distribution"
	routeWebUI         = "web-ui"
	routeStatus        = "status"

	listenerHTTP          = "http"
	listenerNodeTransport = "node-transport"

	workerDependencyHealth = "dependency-health"
	workerDispatcher       = "orchestrator-dispatcher"
	workerInitialRun       = "initial-run-trigger"
	// workerAgentUpgrade records each connecting node's
	// node-agent-client build and upgrades out-of-date clients. It runs
	// where node connections are, like the initial-run trigger: both
	// are driven by connect notifications.
	workerAgentUpgrade  = "agent-upgrade-coordinator"
	workerVulnScheduler = "vulnerability-scheduler"
	// workerPatchScanner scans connected nodes for pending updates on
	// an interval. Any instance can dispatch, and a lease keeps one
	// scanning at a time, so it sits with the other leased workers.
	workerPatchScanner = "patch-scanner"
	workerJobReaper    = "orchestrator-job-reaper"
	// workerScheduler fires scheduled jobs. It runs where the
	// dispatcher does, because the job it starts is dispatched by the
	// same instance: a task's resolved payload lives in that
	// instance's memory, and its results come back to it.
	workerScheduler       = "orchestrator-scheduler"
	workerStatusResponder = "status-responder"
)

// Surface is what one run mode activates.
type Surface struct {
	Routes    []string
	Listeners []string
	Workers   []string
}

// Has reports whether name appears in the given set.
func has(set []string, name string) bool { return slices.Contains(set, name) }

// HasRoute reports whether this surface registers the named route group.
func (s Surface) HasRoute(name string) bool { return has(s.Routes, name) }

// HasListener reports whether this surface opens the named listener.
func (s Surface) HasListener(name string) bool { return has(s.Listeners, name) }

// HasWorker reports whether this surface runs the named worker.
func (s Surface) HasWorker(name string) bool { return has(s.Workers, name) }

// allRoutes is every route group, in the order the binary registered
// them before run modes existed. ModeAll's surface is defined as exactly
// this, which is what keeps the default mode identical to the previous
// behavior.
var allRoutes = []string{
	routeOperational,
	routeRBAC,
	routeInventory,
	routeNodeConnect,
	routePackages,
	routeReporting,
	routeClassifier,
	routeENC,
	routeGroupNodes,
	routeVulnerability,
	routeActivity,
	routeCodeManager,
	routeOrchestrator,
	routeAgentDist,
	routeStatus,
	// Registered last because it is the catch-all "/" pattern.
	routeWebUI,
}

// modeSurfaces is the mode table: the single place that decides what each
// mode runs.
//
// Two choices in here are worth stating explicitly, because neither is
// forced by the mode names:
//
//   - web serves the ENC route as well as the rest of the REST API. enc
//     mode exists so ENC *can* be scaled and placed on its own (next to a
//     compiler, typically), not so that it is unavailable everywhere
//     else - a web/orchestrator/worker split that silently stopped
//     answering classification would be a much worse failure than web
//     carrying some ENC load.
//   - orchestrator serves no REST API. It holds node connections and
//     starts the jobs nobody asked for over the API (scheduled jobs, a
//     new node's first run). The job-triggering endpoints live in web,
//     which reaches those connections over the cluster.
//   - the dispatcher runs wherever a job can be started - web as well as
//     orchestrator - because a job is followed to its end by the
//     instance that started it. It is not scheduled work: it only
//     handles the answers to requests this instance itself sent.
var modeSurfaces = map[runtime.Mode]Surface{
	runtime.ModeAll: {
		Routes:    allRoutes,
		Listeners: []string{listenerHTTP, listenerNodeTransport},
		Workers: []string{
			workerDependencyHealth,
			workerDispatcher,
			workerInitialRun,
			workerAgentUpgrade,
			workerVulnScheduler,
			workerPatchScanner,
			workerJobReaper,
			workerScheduler,
			workerStatusResponder,
		},
	},

	runtime.ModeWeb: {
		Routes: []string{
			routeOperational,
			routeRBAC,
			routeInventory,
			routeNodeConnect,
			routePackages,
			routeReporting,
			routeClassifier,
			routeENC,
			routeGroupNodes,
			routeVulnerability,
			routeActivity,
			routeCodeManager,
			routeOrchestrator,
			routeAgentDist,
			routeStatus,
			routeWebUI,
		},
		Listeners: []string{listenerHTTP},
		// web starts jobs, so it runs the dispatcher: the loop that
		// records what each node answered. Without it a job started
		// here is sent but never finishes.
		Workers: []string{workerDependencyHealth, workerDispatcher, workerStatusResponder},
	},

	runtime.ModeENC: {
		Routes:    []string{routeOperational, routeENC},
		Listeners: []string{listenerHTTP},
		Workers:   []string{workerDependencyHealth, workerStatusResponder},
	},

	runtime.ModeOrchestrator: {
		Routes:    []string{routeOperational},
		Listeners: []string{listenerHTTP, listenerNodeTransport},
		Workers: []string{
			workerDependencyHealth,
			workerDispatcher,
			workerInitialRun,
			workerAgentUpgrade,
			workerScheduler,
			workerStatusResponder,
		},
	},

	runtime.ModeWorker: {
		Routes:    []string{routeOperational},
		Listeners: []string{listenerHTTP},
		Workers: []string{
			workerDependencyHealth,
			workerVulnScheduler,
			workerPatchScanner,
			workerJobReaper,
			workerStatusResponder,
		},
	},
}

// surfaceFor returns the surface for mode. ParseMode has already refused
// anything unrecognized by the time this runs, so a miss here means a
// mode was added to mode.go without a row in the table above - a
// programming error, not an operator one.
func surfaceFor(mode runtime.Mode) (Surface, error) {
	s, ok := modeSurfaces[mode]
	if !ok {
		return Surface{}, fmt.Errorf("no surface defined for run mode %q", mode)
	}
	return s, nil
}

// jobReaperInterval is how often the stale-job reaper runs, and
// jobReaperLease the lease it holds while doing so.
//
// Infrequent by design: it only ever catches jobs whose dispatching
// instance stopped, and the staleness threshold it applies is measured
// in hours, so checking more often would find nothing new.
const (
	jobReaperInterval = 5 * time.Minute
	jobReaperLease    = "orchestrator-job-reaper"
)

// attachmentFor maps this instance's cluster configuration to how it
// presents itself in the stack status.
//
// Reported by the instance itself rather than inferred by whoever reads
// the status: only this instance knows how it was configured to attach,
// and a leaf is invisible to every peer but the one it attached to.
func attachmentFor(cfg runtime.Config) stackstatus.Attachment {
	switch cfg.EffectiveClusterMode() {
	case runtime.ClusterModeLeaf:
		return stackstatus.AttachmentLeaf
	case runtime.ClusterModeRoute:
		return stackstatus.AttachmentRouted
	default:
		return stackstatus.AttachmentStandalone
	}
}
