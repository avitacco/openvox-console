package app

import (
	"fmt"
	"slices"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/runtime"
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

	listenerHTTP          = "http"
	listenerNodeTransport = "node-transport"

	workerDependencyHealth = "dependency-health"
	workerActivityRecorder = "activity-recorder"
	workerDispatcher       = "orchestrator-dispatcher"
	workerInitialRun       = "initial-run-trigger"
	workerVulnScheduler    = "vulnerability-scheduler"
	workerJobReaper        = "orchestrator-job-reaper"
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
//   - orchestrator serves no REST API. It holds node connections and runs
//     the dispatcher; the job-triggering endpoints live in web, which
//     reaches those connections over the cluster.
var modeSurfaces = map[runtime.Mode]Surface{
	runtime.ModeAll: {
		Routes:    allRoutes,
		Listeners: []string{listenerHTTP, listenerNodeTransport},
		Workers: []string{
			workerDependencyHealth,
			workerActivityRecorder,
			workerDispatcher,
			workerInitialRun,
			workerVulnScheduler,
			workerJobReaper,
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
			routeWebUI,
		},
		Listeners: []string{listenerHTTP},
		Workers:   []string{workerDependencyHealth},
	},

	runtime.ModeENC: {
		Routes:    []string{routeOperational, routeENC},
		Listeners: []string{listenerHTTP},
		Workers:   []string{workerDependencyHealth},
	},

	runtime.ModeOrchestrator: {
		Routes:    []string{routeOperational},
		Listeners: []string{listenerHTTP, listenerNodeTransport},
		Workers: []string{
			workerDependencyHealth,
			workerDispatcher,
			workerInitialRun,
		},
	},

	runtime.ModeWorker: {
		Routes:    []string{routeOperational},
		Listeners: []string{listenerHTTP},
		Workers: []string{
			workerDependencyHealth,
			workerActivityRecorder,
			workerVulnScheduler,
			workerJobReaper,
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
