package app_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/runtime"
	"github.com/voxpupuli/enterprise-console/internal/testapp"
)

// A route the active mode does not serve must be answered as not-found,
// not as an internal failure - see the run-modes capability's "A mode
// does not serve another mode's routes" scenario.
//
// The console UI is the sharpest case: it is registered at "/", the
// catch-all, so a mode that still registered it would answer 200 for
// every unmatched path rather than 404.
func TestENCModeDoesNotServeTheConsoleUI(t *testing.T) {
	c := testapp.NewCluster(t)
	enc := c.Start(testapp.Config{Mode: runtime.ModeENC})

	got := enc.StatusOf(t, "/")
	if got != http.StatusNotFound {
		t.Errorf("GET / on an enc-mode instance = %d, want %d", got, http.StatusNotFound)
	}
	if got >= 500 {
		t.Errorf("GET / on an enc-mode instance = %d, which reports an internal failure rather than an absent route", got)
	}
}

// The other half of the same requirement: a mode does serve its own
// routes. A registered-but-unauthenticated route answers 401, never 404,
// so "not 404" is what distinguishes registered from absent here.
func TestENCModeServesTheENCRoute(t *testing.T) {
	c := testapp.NewCluster(t)
	enc := c.Start(testapp.Config{Mode: runtime.ModeENC})

	if got := enc.StatusOf(t, "/api/v1/enc/node.example.com"); got == http.StatusNotFound {
		t.Error("the enc-mode instance does not serve the ENC route")
	}
}

// Routes belonging to other modes are absent from enc mode entirely.
func TestENCModeDoesNotServeOtherModesRoutes(t *testing.T) {
	c := testapp.NewCluster(t)
	enc := c.Start(testapp.Config{Mode: runtime.ModeENC})

	for _, path := range []string{
		"/api/v1/nodes",
		"/api/v1/groups",
		"/api/v1/audit-log",
		"/api/v1/orchestrator/jobs",
	} {
		if got := enc.StatusOf(t, path); got != http.StatusNotFound {
			t.Errorf("GET %s on an enc-mode instance = %d, want %d", path, got, http.StatusNotFound)
		}
	}
}

// Health and metrics answer in every mode, so an instance of any mode can
// be checked and scraped without the checker knowing its mode - see
// service-runtime's "Health is available in every mode" scenario.
func TestOperationalRoutesServedInEveryMode(t *testing.T) {
	c := testapp.NewCluster(t)

	for _, mode := range runtime.Modes() {
		t.Run(string(mode), func(t *testing.T) {
			inst := c.Start(testapp.Config{Mode: mode})

			if got := inst.StatusOf(t, "/health"); got == http.StatusNotFound {
				t.Errorf("mode %q does not serve /health", mode)
			}
			if got := inst.StatusOf(t, "/metrics"); got != http.StatusOK {
				t.Errorf("mode %q: GET /metrics = %d, want 200", mode, got)
			}
			if got := inst.StatusOf(t, "/api/v1/version"); got != http.StatusOK {
				t.Errorf("mode %q: GET /api/v1/version = %d, want 200", mode, got)
			}
		})
	}
}

// web mode serves the console UI; orchestrator and worker do not.
func TestWebUIServedOnlyByModesThatNameIt(t *testing.T) {
	c := testapp.NewCluster(t)

	web := c.Start(testapp.Config{Mode: runtime.ModeWeb})
	if got := web.StatusOf(t, "/"); got != http.StatusOK {
		t.Errorf("GET / on a web-mode instance = %d, want 200", got)
	}

	for _, mode := range []runtime.Mode{runtime.ModeOrchestrator, runtime.ModeWorker} {
		inst := c.Start(testapp.Config{Mode: mode})
		if got := inst.StatusOf(t, "/"); got != http.StatusNotFound {
			t.Errorf("GET / on a %s-mode instance = %d, want %d", mode, got, http.StatusNotFound)
		}
	}
}

// all mode is the default and must keep serving everything it did before
// run modes existed - the end-to-end counterpart to the surface-level
// equivalence baseline.
func TestAllModeServesEverySurface(t *testing.T) {
	c := testapp.NewCluster(t)
	all := c.Start(testapp.Config{Mode: runtime.ModeAll})

	for _, path := range []string{
		"/health",
		"/metrics",
		"/api/v1/version",
		"/", // console UI
	} {
		if got := all.StatusOf(t, path); got == http.StatusNotFound {
			t.Errorf("all mode does not serve %s", path)
		}
	}
	// Authenticated routes answer 401, not 404, when registered.
	for _, path := range []string{
		"/api/v1/enc/node.example.com",
		"/api/v1/groups",
		"/api/v1/audit-log",
		"/api/v1/nodes",
		"/api/v1/orchestrator/jobs",
	} {
		if got := all.StatusOf(t, path); got == http.StatusNotFound {
			t.Errorf("all mode does not serve %s", path)
		}
	}
}

// An enc instance starts with no signing key configured at all, and
// still serves the ENC route - see the run-modes capability's "A mode
// starts without configuration it does not use".
func TestENCModeStartsWithoutASigningKey(t *testing.T) {
	c := testapp.NewCluster(t)

	enc := c.Start(testapp.Config{
		Mode: runtime.ModeENC,
		// An empty value removes the setting from the instance's
		// environment entirely, rather than blanking it.
		Env: map[string]string{"CONSOLE_RBAC_SIGNING_KEY_FILE": ""},
	})

	if got := enc.StatusOf(t, "/api/v1/enc/node.example.com"); got == http.StatusNotFound {
		t.Error("the enc-mode instance does not serve the ENC route")
	}
	if got := enc.StatusOf(t, "/health"); got == http.StatusNotFound {
		t.Error("the enc-mode instance does not serve /health")
	}
}

// The stronger property: a non-issuing mode does not read the signing
// key even when one is configured. Pointing the setting at a path that
// cannot be opened would fail startup if the file were read - so a
// healthy instance proves it was not.
func TestNonIssuingModesDoNotReadTheSigningKey(t *testing.T) {
	c := testapp.NewCluster(t)

	for _, mode := range []runtime.Mode{runtime.ModeENC, runtime.ModeWorker, runtime.ModeOrchestrator} {
		t.Run(string(mode), func(t *testing.T) {
			inst := c.Start(testapp.Config{
				Mode: mode,
				Env: map[string]string{
					"CONSOLE_RBAC_SIGNING_KEY_FILE": "/nonexistent/signing-key.pem",
				},
			})
			if got := inst.StatusOf(t, "/api/v1/version"); got != http.StatusOK {
				t.Errorf("mode %q did not start with an unreadable signing key configured, so it read it", mode)
			}
		})
	}
}

// The complement: an issuing mode does read it, and fails loudly when it
// cannot. Without this, the test above would also pass if the key were
// never read by anyone.
func TestIssuingModeFailsOnAnUnreadableSigningKey(t *testing.T) {
	c := testapp.NewCluster(t)

	cfg := testapp.Config{
		Mode: runtime.ModeWeb,
		Env:  map[string]string{"CONSOLE_RBAC_SIGNING_KEY_FILE": "/nonexistent/signing-key.pem"},
	}
	if err := c.TryStart(cfg); err == nil {
		t.Error("web mode started with an unreadable signing key, so it never read it")
	}
}

// Every instance reports which mode it is running, so an operator or
// load balancer checking one learns what it serves without knowing how
// it was configured - see service-runtime's "Health names the active
// mode" scenario.
func TestHealthNamesTheActiveMode(t *testing.T) {
	c := testapp.NewCluster(t)

	for _, mode := range runtime.Modes() {
		t.Run(string(mode), func(t *testing.T) {
			inst := c.Start(testapp.Config{Mode: mode})

			resp := inst.Get(t, "/health")
			defer resp.Body.Close()

			var body struct {
				Mode   string            `json:"mode"`
				Checks map[string]string `json:"checks"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode health response: %v", err)
			}
			if body.Mode != string(mode) {
				t.Errorf("health reports mode %q, want %q", body.Mode, mode)
			}

			// No mode reports a node transport dependency: it is not one
			// of the checkers any mode registers, so an instance without
			// a transport is never reported unhealthy for lacking one.
			if _, ok := body.Checks["node-transport"]; ok {
				t.Errorf("mode %q reports a node transport health dependency", mode)
			}
		})
	}
}
