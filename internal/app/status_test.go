package app_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/runtime"
	"github.com/voxpupuli/enterprise-console/internal/stackstatus"
	"github.com/voxpupuli/enterprise-console/internal/testapp"
)

// readStatus fetches and decodes the stack status from an instance.
func readStatus(t *testing.T, inst *testapp.Instance, token string) stackstatus.Stack {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, inst.BaseURL+"/api/v1/status", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/status = %d, want 200", resp.StatusCode)
	}

	var stack stackstatus.Stack
	if err := json.NewDecoder(resp.Body).Decode(&stack); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	return stack
}

func TestStatusRequiresThePermission(t *testing.T) {
	c := testapp.NewCluster(t)
	inst := c.Start(testapp.Config{Mode: runtime.ModeAll})

	if got := inst.GetWithToken(t, "/api/v1/status", ""); got == http.StatusOK {
		t.Error("the status endpoint served an unauthenticated request")
	}
}

// A lone instance reports exactly itself, and is not marked incomplete -
// the single-instance deployment is a first-class case, not a degenerate
// one.
func TestStatusOnASingleInstance(t *testing.T) {
	c := testapp.NewCluster(t)
	inst := c.Start(testapp.Config{Mode: runtime.ModeAll})
	token := inst.Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)

	stack := readStatus(t, inst, token)

	if stack.Replied != 1 || stack.Expected != 1 {
		t.Errorf("Expected/Replied = %d/%d, want 1/1", stack.Expected, stack.Replied)
	}
	if stack.Incomplete {
		t.Errorf("a lone instance was reported incomplete: %s", stack.IncompleteReason)
	}
	if len(stack.Modes) != 1 || stack.Modes[0].Mode != string(runtime.ModeAll) {
		t.Fatalf("Modes = %+v, want one group for %q", stack.Modes, runtime.ModeAll)
	}

	self := stack.Modes[0].Instances[0]
	if self.ID == "" || self.Hostname == "" || self.Address == "" {
		t.Errorf("instance is missing identity fields: %+v", self)
	}
	if self.StartedAt.IsZero() {
		t.Error("instance reports no start time")
	}
	if len(self.Workers) == 0 {
		t.Error("instance reports no workers")
	}
	if len(stack.Dependencies) == 0 {
		t.Error("no dependencies reported")
	}
}

// Dependencies distinguish not-configured from unreachable: these mean
// different things to an operator, and conflating them turns a
// deployment choice into a phantom fault.
func TestStatusDistinguishesUnconfiguredFromUnreachable(t *testing.T) {
	c := testapp.NewCluster(t)
	// The harness configures no CA client, and points openvoxdb at a
	// closed port.
	inst := c.Start(testapp.Config{Mode: runtime.ModeAll})
	token := inst.Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)

	byName := map[string]stackstatus.Dependency{}
	for _, dep := range readStatus(t, inst, token).Dependencies {
		byName[dep.Name] = dep
	}

	ca, ok := byName[stackstatus.DepCAClient]
	if !ok {
		t.Fatalf("no CA client dependency reported; got %v", byName)
	}
	if ca.Health != stackstatus.HealthNotConfigured {
		t.Errorf("unconfigured CA client health = %q, want %q", ca.Health, stackstatus.HealthNotConfigured)
	}

	voxdb, ok := byName[stackstatus.DepOpenvoxdb]
	if !ok {
		t.Fatalf("no openvoxdb dependency reported; got %v", byName)
	}
	if voxdb.Health != stackstatus.HealthUnreachable {
		t.Errorf("unreachable openvoxdb health = %q, want %q", voxdb.Health, stackstatus.HealthUnreachable)
	}
	if voxdb.Target == "" {
		t.Error("unreachable openvoxdb reports no target, so a misconfiguration is indistinguishable from an outage")
	}
}

// Every instance of every mode appears, including the enc leaf that the
// serving instance cannot see directly.
func TestStatusReportsASplitTopology(t *testing.T) {
	c := testapp.NewCluster(t)

	const (
		secret     = "test-cluster-secret"
		leafSecret = "test-leaf-secret"
	)
	seedAddr := testapp.FreeAddr(t)
	seedLeafAddr := testapp.FreeAddr(t)

	web := c.Start(testapp.Config{
		Mode: runtime.ModeWeb,
		Env: map[string]string{
			"CONSOLE_CLUSTER_ADDR":        seedAddr,
			"CONSOLE_CLUSTER_LEAF_ADDR":   seedLeafAddr,
			"CONSOLE_CLUSTER_SECRET":      secret,
			"CONSOLE_CLUSTER_LEAF_SECRET": leafSecret,
		},
	})
	routedPeer := func(mode runtime.Mode) *testapp.Instance {
		return c.Start(testapp.Config{
			Mode: mode,
			Env: map[string]string{
				"CONSOLE_CLUSTER_ADDR":   testapp.FreeAddr(t),
				"CONSOLE_CLUSTER_PEERS":  seedAddr,
				"CONSOLE_CLUSTER_SECRET": secret,
			},
		})
	}
	routedPeer(runtime.ModeWorker)
	routedPeer(runtime.ModeOrchestrator)
	c.Start(testapp.Config{
		Mode: runtime.ModeENC,
		Env: map[string]string{
			"CONSOLE_CLUSTER_PEERS":       seedLeafAddr,
			"CONSOLE_CLUSTER_MODE":        "leaf",
			"CONSOLE_CLUSTER_LEAF_SECRET": leafSecret,
			"CONSOLE_CLUSTER_ADDR":        "",
		},
	})

	token := web.Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)

	// Retried: routes, leaf attachment and subscription interest all
	// propagate asynchronously after startup.
	var stack stackstatus.Stack
	for i := 0; i < 40; i++ {
		stack = readStatus(t, web, token)
		if stack.Replied == 4 && !stack.Incomplete {
			break
		}
	}

	if stack.Incomplete {
		t.Errorf("status reported incomplete: %s (expected %d, replied %d)",
			stack.IncompleteReason, stack.Expected, stack.Replied)
	}

	byMode := map[string]int{}
	for _, g := range stack.Modes {
		byMode[g.Mode] = g.Count
	}
	for _, mode := range []runtime.Mode{runtime.ModeWeb, runtime.ModeWorker, runtime.ModeOrchestrator, runtime.ModeENC} {
		if byMode[string(mode)] != 1 {
			t.Errorf("mode %q count = %d, want 1 (all modes: %v)", mode, byMode[string(mode)], byMode)
		}
	}
}

// Stopping an instance removes it from the next status request, and the
// survivors are still reported. The interesting half is the transition:
// immediately after the stop the remaining instances may still see the
// departed one as a peer, so the result is legitimately incomplete for a
// moment before settling.
func TestStatusAfterAnInstanceStops(t *testing.T) {
	c := testapp.NewCluster(t)

	const secret = "test-cluster-secret"
	seedAddr := testapp.FreeAddr(t)

	survivor := c.Start(testapp.Config{
		Mode: runtime.ModeAll,
		Env: map[string]string{
			"CONSOLE_CLUSTER_ADDR":   seedAddr,
			"CONSOLE_CLUSTER_SECRET": secret,
		},
	})
	doomed := c.Start(testapp.Config{
		Mode: runtime.ModeAll,
		Env: map[string]string{
			"CONSOLE_CLUSTER_ADDR":   testapp.FreeAddr(t),
			"CONSOLE_CLUSTER_PEERS":  seedAddr,
			"CONSOLE_CLUSTER_SECRET": secret,
		},
	})

	token := survivor.Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)

	// Both present first, or the assertion after the stop proves nothing.
	var before stackstatus.Stack
	for i := 0; i < 40; i++ {
		before = readStatus(t, survivor, token)
		if before.Replied == 2 {
			break
		}
	}
	if before.Replied != 2 {
		t.Fatalf("both instances never appeared: replied %d", before.Replied)
	}

	doomed.Stop(t)

	// Settles once the survivor's own cluster view drops the departed
	// peer, at which point one instance is both the whole expected set
	// and the whole reported set.
	for i := 0; i < 100; i++ {
		after := readStatus(t, survivor, token)
		if after.Replied == 1 && !after.Incomplete {
			if len(after.Modes) != 1 || after.Modes[0].Count != 1 {
				t.Errorf("survivor not reported correctly: %+v", after.Modes)
			}
			return
		}
	}
	t.Error("the status never settled to just the surviving instance")
}
