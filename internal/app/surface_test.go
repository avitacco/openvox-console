package app

import (
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/runtime"
)

// The baseline: exactly what the binary registered, listened on, and ran
// in the background before run modes existed, read off the pre-split
// cmd/console/main.go (git 8b10dba).
//
// ModeAll must stay equal to this forever. It is the default, so any
// drift here is a behavior change for every deployment that never asked
// for one - which is precisely what this test exists to catch. A change
// that genuinely intends to add something to every mode has to add it in
// both places, deliberately.
var preSplitBaseline = Surface{
	Routes: []string{
		// mux.Handle("/health"), mux.Handle("/metrics"),
		// mux.HandleFunc("GET /api/v1/version")
		"operational",
		// rbacHandlers.Register(mux)
		"rbac",
		// inventory.NewHandlers(...).Register(mux, verifier.Authorize)
		"inventory",
		// nodeConnectivityHandlers.Register(mux, verifier.Authorize)
		"node-connectivity",
		// packageHandlers.Register(mux, verifier.Authorize)
		"package-inventory",
		// reporting.NewHandlers(...).Register(mux, verifier.Authorize)
		"reporting",
		// classifier.NewHandlers(...).Register(mux, verifier.Authorize)
		"classifier",
		// encapi.NewHandlers(...).Register(mux, verifier.Authorize)
		"enc",
		// groupnodes.NewHandlers(...).Register(mux, verifier.Authorize)
		"group-nodes",
		// vulnerability.NewHandlers(...).Register(mux, verifier.Authorize)
		"vulnerability",
		// activity.NewHandlers(activityStore).Register(mux, verifier.Authorize)
		"activity",
		// codemanagerHandlers.Register(mux, verifier.Authorize)
		"code-manager",
		// orchestratorHandlers.Register(mux, verifier.Authorize)
		"orchestrator",
		// agentDistHandlers.Register(mux)
		"agent-distribution",
		// Added by this change: the stack status API.
		"status",
		// mux.Handle("/", webHandler)
		"web-ui",
	},
	Listeners: []string{
		// srv.ListenAndServe() on cfg.HTTPAddr
		"http",
		// nodetransport.New(...) when CONSOLE_NODE_TRANSPORT_ADDR is set
		"node-transport",
	},
	Workers: []string{
		// go runtime.PollDependencyHealth(ctx, metrics, db, bus)
		"dependency-health",
		// activity-recorder was here: activity events are now written
		// directly by the instance that records them, so no mode runs a
		// recorder worker.
		// go dispatcher.Run(ctx)
		"orchestrator-dispatcher",
		// initialRunTriggerImpl.Start(ctx)
		"initial-run-trigger",
		// go vulnScheduler.Run(ctx)
		"vulnerability-scheduler",
		// Added by this change: nothing reaped jobs before, because a
		// single instance never left one untracked.
		"orchestrator-job-reaper",
		// Added by this change: every instance must be able to describe
		// itself, in every mode.
		"status-responder",
	},
}

func TestModeAllMatchesPreSplitBaseline(t *testing.T) {
	got, err := surfaceFor(runtime.ModeAll)
	if err != nil {
		t.Fatalf("surfaceFor(ModeAll) error: %v", err)
	}
	assertSameSet(t, "routes", preSplitBaseline.Routes, got.Routes)
	assertSameSet(t, "listeners", preSplitBaseline.Listeners, got.Listeners)
	assertSameSet(t, "workers", preSplitBaseline.Workers, got.Workers)
}

// Route registration order is load-bearing for exactly one entry: the
// web UI's "/" is a catch-all, so anything registered after it that
// overlaps would be unreachable.
func TestModeAllRegistersWebUILast(t *testing.T) {
	got, err := surfaceFor(runtime.ModeAll)
	if err != nil {
		t.Fatalf("surfaceFor(ModeAll) error: %v", err)
	}
	if len(got.Routes) == 0 || got.Routes[len(got.Routes)-1] != routeWebUI {
		t.Errorf("last route group = %v, want %q registered last (it is the catch-all \"/\")",
			got.Routes[len(got.Routes)-1:], routeWebUI)
	}
}

// Every mode must be in the table. A mode that parses but has no surface
// would start and serve nothing.
func TestEveryModeHasASurface(t *testing.T) {
	for _, mode := range runtime.Modes() {
		if _, err := surfaceFor(mode); err != nil {
			t.Errorf("mode %q: %v", mode, err)
		}
	}
}

// assertSameSet compares two name sets order-independently, reporting
// what is missing and what is unexpected rather than just "not equal".
func assertSameSet(t *testing.T, label string, want, got []string) {
	t.Helper()

	wantSet := map[string]bool{}
	for _, n := range want {
		wantSet[n] = true
	}
	gotSet := map[string]bool{}
	for _, n := range got {
		gotSet[n] = true
	}

	for n := range wantSet {
		if !gotSet[n] {
			t.Errorf("%s: missing %q (present before run modes existed)", label, n)
		}
	}
	for n := range gotSet {
		if !wantSet[n] {
			t.Errorf("%s: unexpected %q (not present before run modes existed)", label, n)
		}
	}
}

// Each mode's surface, asserted against the run-modes capability's
// "Mode-determined service surface" requirement. The requirement is
// written as what a mode SHALL and SHALL NOT activate, so this asserts
// both directions - a mode quietly gaining a surface is as much a
// violation as one losing it.
func TestModeSurfacesMatchSpec(t *testing.T) {
	tests := []struct {
		mode           runtime.Mode
		mustHaveRoutes []string
		mustNotRoutes  []string
		mustHaveListen []string
		mustNotListen  []string
		mustHaveWorker []string
		mustNotWorker  []string
	}{
		{
			// "web SHALL serve the console UI and the REST API, including
			// code deployment endpoints and webhooks, and SHALL NOT
			// terminate node transport connections or run background
			// workers."
			mode:           runtime.ModeWeb,
			mustHaveRoutes: []string{routeWebUI, routeRBAC, routeCodeManager, routeOrchestrator, routeStatus},
			mustHaveListen: []string{listenerHTTP},
			mustNotListen:  []string{listenerNodeTransport},
			mustHaveWorker: []string{workerStatusResponder},
			mustNotWorker:  []string{workerDispatcher, workerInitialRun, workerVulnScheduler},
		},
		{
			// "enc SHALL serve only the ENC classification endpoint and
			// the system's own operational endpoints (health and
			// metrics), and SHALL NOT serve the console UI, the REST API,
			// node transport connections, or background workers."
			mode:           runtime.ModeENC,
			mustHaveRoutes: []string{routeOperational, routeENC},
			mustNotRoutes: []string{
				routeWebUI, routeRBAC, routeInventory, routeNodeConnect, routePackages,
				routeReporting, routeClassifier, routeGroupNodes, routeVulnerability,
				routeActivity, routeCodeManager, routeOrchestrator, routeAgentDist,
				routeStatus,
			},
			mustHaveListen: []string{listenerHTTP},
			mustNotListen:  []string{listenerNodeTransport},
			mustHaveWorker: []string{workerStatusResponder},
			mustNotWorker:  []string{workerDispatcher, workerInitialRun, workerVulnScheduler},
		},
		{
			// "orchestrator SHALL terminate node transport connections and
			// dispatch orchestration jobs, and SHALL NOT serve the console
			// UI or the REST API."
			mode:           runtime.ModeOrchestrator,
			mustHaveRoutes: []string{routeOperational},
			mustNotRoutes:  []string{routeWebUI, routeRBAC, routeENC, routeCodeManager, routeOrchestrator, routeStatus},
			mustHaveListen: []string{listenerHTTP, listenerNodeTransport},
			mustHaveWorker: []string{workerDispatcher, workerInitialRun, workerStatusResponder},
			mustNotWorker:  []string{workerVulnScheduler},
		},
		{
			// "worker SHALL run background workers, and SHALL NOT serve
			// the console UI, the REST API, or node transport
			// connections."
			mode:           runtime.ModeWorker,
			mustHaveRoutes: []string{routeOperational},
			mustNotRoutes:  []string{routeWebUI, routeRBAC, routeENC, routeCodeManager, routeOrchestrator, routeStatus},
			mustHaveListen: []string{listenerHTTP},
			mustNotListen:  []string{listenerNodeTransport},
			mustHaveWorker: []string{workerVulnScheduler, workerStatusResponder},
			mustNotWorker:  []string{workerDispatcher, workerInitialRun},
		},
	}

	for _, tc := range tests {
		t.Run(string(tc.mode), func(t *testing.T) {
			s, err := surfaceFor(tc.mode)
			if err != nil {
				t.Fatalf("surfaceFor(%q) error: %v", tc.mode, err)
			}
			for _, n := range tc.mustHaveRoutes {
				if !s.HasRoute(n) {
					t.Errorf("mode %q must serve route group %q", tc.mode, n)
				}
			}
			for _, n := range tc.mustNotRoutes {
				if s.HasRoute(n) {
					t.Errorf("mode %q must NOT serve route group %q", tc.mode, n)
				}
			}
			for _, n := range tc.mustHaveListen {
				if !s.HasListener(n) {
					t.Errorf("mode %q must open listener %q", tc.mode, n)
				}
			}
			for _, n := range tc.mustNotListen {
				if s.HasListener(n) {
					t.Errorf("mode %q must NOT open listener %q", tc.mode, n)
				}
			}
			for _, n := range tc.mustHaveWorker {
				if !s.HasWorker(n) {
					t.Errorf("mode %q must run worker %q", tc.mode, n)
				}
			}
			for _, n := range tc.mustNotWorker {
				if s.HasWorker(n) {
					t.Errorf("mode %q must NOT run worker %q", tc.mode, n)
				}
			}
		})
	}
}

// Health and metrics are served in every mode, so that a load balancer
// or orchestration platform can check any instance without knowing which
// mode it runs - see service-runtime's "Health is available in every
// mode" scenario.
func TestEveryModeServesOperationalRoutes(t *testing.T) {
	for _, mode := range runtime.Modes() {
		s, err := surfaceFor(mode)
		if err != nil {
			t.Fatalf("surfaceFor(%q) error: %v", mode, err)
		}
		if !s.HasRoute(routeOperational) {
			t.Errorf("mode %q does not serve the operational routes", mode)
		}
		if !s.HasListener(listenerHTTP) {
			t.Errorf("mode %q opens no HTTP listener, so it cannot be health-checked", mode)
		}
	}
}

// A mode that does not register the web UI registers no "/" pattern, so
// an unmatched path falls through to the mux's own not-found rather than
// to the SPA shell - which is what makes the "A mode does not serve
// another mode's routes" scenario answer 404 rather than 200.
func TestModesWithoutWebUIHaveNoCatchAll(t *testing.T) {
	for _, mode := range []runtime.Mode{runtime.ModeENC, runtime.ModeOrchestrator, runtime.ModeWorker} {
		s, err := surfaceFor(mode)
		if err != nil {
			t.Fatalf("surfaceFor(%q) error: %v", mode, err)
		}
		if s.HasRoute(routeWebUI) {
			t.Errorf("mode %q registers the catch-all web UI route, so unmatched paths would not 404", mode)
		}
	}
}

// Every mode runs the status responder. An instance that cannot describe
// itself is absent from the stack status page while still running -
// worse than being reported unhealthy, because nothing shows it is
// missing.
func TestEveryModeRunsTheStatusResponder(t *testing.T) {
	for _, mode := range runtime.Modes() {
		s, err := surfaceFor(mode)
		if err != nil {
			t.Fatalf("surfaceFor(%q) error: %v", mode, err)
		}
		if !s.HasWorker(workerStatusResponder) {
			t.Errorf("mode %q does not run the status responder, so it would be invisible on the status page", mode)
		}
	}
}
