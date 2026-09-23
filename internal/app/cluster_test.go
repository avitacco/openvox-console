package app_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	"github.com/voxpupuli/enterprise-console/internal/activity"
	"github.com/voxpupuli/enterprise-console/internal/messaging"
	"github.com/voxpupuli/enterprise-console/internal/rbac"
	"github.com/voxpupuli/enterprise-console/internal/runtime"
	"github.com/voxpupuli/enterprise-console/internal/testapp"
)

// clusterOf starts n instances of the given mode, peered together. The
// first listens; the rest dial it.
func clusterOf(t *testing.T, c *testapp.Cluster, mode runtime.Mode, n int) []*testapp.Instance {
	t.Helper()

	const secret = "test-cluster-secret"
	seedAddr := testapp.FreeAddr(t)

	instances := make([]*testapp.Instance, 0, n)
	instances = append(instances, c.Start(testapp.Config{
		Mode: mode,
		Env: map[string]string{
			"CONSOLE_CLUSTER_ADDR":   seedAddr,
			"CONSOLE_CLUSTER_SECRET": secret,
		},
	}))
	for i := 1; i < n; i++ {
		instances = append(instances, c.Start(testapp.Config{
			Mode: mode,
			Env: map[string]string{
				"CONSOLE_CLUSTER_ADDR":   testapp.FreeAddr(t),
				"CONSOLE_CLUSTER_PEERS":  seedAddr,
				"CONSOLE_CLUSTER_SECRET": secret,
			},
		}))
	}
	return instances
}

// An activity event published once must be persisted once, however many
// instances are running. Before the recorder joined a queue group this
// wrote one identical row per instance - the duplicate-write bug
// clustering introduces and queue groups remove.
func TestActivityEventPersistedOnceAcrossInstances(t *testing.T) {
	c := testapp.NewCluster(t)
	instances := clusterOf(t, c, runtime.ModeAll, 2)

	pool, err := pgxpool.New(context.Background(), c.DSN())
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	defer pool.Close()

	// A marker unique to this run, so the assertion is unaffected by
	// anything else in the shared table.
	summary := "cluster-dedup-probe-" + time.Now().Format("20060102150405.000000000")

	// Published through a bus peered with the running instances, exactly
	// as a live request on a third instance would.
	pub, err := messaging.StartWith(messaging.Config{
		ListenAddr: testapp.FreeAddr(t),
		Peers:      []string{instances[0].ClusterAddr},
		Secret:     "test-cluster-secret",
	})
	if err != nil {
		t.Fatalf("start publishing bus: %v", err)
	}
	defer pub.Close()

	publisher := activity.NewPublisher(pub, testapp.Logger(), "classifier")

	// Retried until a row appears: the route and the queue-group
	// interest both propagate asynchronously, and core NATS drops a
	// publish that arrives before any subscriber is known.
	deadline := time.Now().Add(20 * time.Second)
	var count int
	for time.Now().Before(deadline) {
		publisher.Publish("cluster-dedup-test", "tester", summary)
		time.Sleep(300 * time.Millisecond)

		if err := pool.QueryRow(context.Background(),
			`SELECT count(*) FROM activity_log WHERE summary = $1`, summary).Scan(&count); err != nil {
			t.Fatalf("count activity events: %v", err)
		}
		if count > 0 {
			break
		}
	}
	if count == 0 {
		t.Fatal("the activity event was never persisted by any instance")
	}

	// Let any duplicate land before asserting. A second row would mean
	// each instance persisted its own copy.
	time.Sleep(time.Second)
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM activity_log WHERE summary = $1`, summary).Scan(&count); err != nil {
		t.Fatalf("count activity events: %v", err)
	}

	// One publish per loop iteration, so the count must equal the number
	// of publishes that happened, not exceed it. The loop breaks on the
	// first success, so exactly one publish produced a row.
	if count != 1 {
		t.Errorf("one published activity event produced %d rows; each instance persisted its own copy", count)
	}
}

// Revocation is the opposite case: every instance must learn about it,
// so the subscription stays fan-out. Asserted at the bus level, since
// what matters is that every subscriber receives a copy.
func TestRevocationReachesEveryInstance(t *testing.T) {
	const secret = "test-cluster-secret"
	seedAddr := testapp.FreeAddr(t)

	a, err := messaging.StartWith(messaging.Config{ListenAddr: seedAddr, Secret: secret})
	if err != nil {
		t.Fatalf("start bus A: %v", err)
	}
	defer a.Close()

	buses := []*messaging.Bus{a}
	for i := 0; i < 2; i++ {
		b, err := messaging.StartWith(messaging.Config{
			ListenAddr: testapp.FreeAddr(t),
			Peers:      []string{seedAddr},
			Secret:     secret,
		})
		if err != nil {
			t.Fatalf("start bus %d: %v", i, err)
		}
		defer b.Close()
		buses = append(buses, b)
	}

	// Every bus subscribes the way rbac.Revoker does.
	seen := make(chan int, len(buses))
	for i, bus := range buses {
		i := i
		if _, err := bus.Subscribe(rbac.RevocationSubject, func(*nats.Msg) {
			seen <- i
		}); err != nil {
			t.Fatalf("subscribe on bus %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(20 * time.Second)
	got := map[int]bool{}
	for time.Now().Before(deadline) && len(got) < len(buses) {
		if err := a.Publish(rbac.RevocationSubject, []byte(`{"jti":"probe","expires_at":"2030-01-01T00:00:00Z"}`)); err != nil {
			t.Fatalf("publish revocation: %v", err)
		}
		timeout := time.After(300 * time.Millisecond)
		for len(got) < len(buses) {
			select {
			case i := <-seen:
				got[i] = true
			case <-timeout:
				goto next
			}
		}
	next:
	}

	if len(got) != len(buses) {
		t.Errorf("a revocation reached %d of %d instances; it must reach every one", len(got), len(buses))
	}
}

// The requirement the rbac capability already states but the
// implementation could not meet before clustering: "revocation SHALL
// take effect on every running instance without restarting any of them".
//
// Exercised through the real HTTP stack on two instances, because that
// is where it matters: a token revoked by logging out of the instance
// behind one load-balancer backend must not still work on another.
func TestRevokedTokenIsRejectedOnEveryInstance(t *testing.T) {
	c := testapp.NewCluster(t)
	instances := clusterOf(t, c, runtime.ModeAll, 2)
	a, b := instances[0], instances[1]

	token := a.Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)

	// The token works on both instances to begin with - otherwise the
	// assertion after revocation would prove nothing.
	if got := a.GetWithToken(t, "/api/v1/me", token); got != http.StatusOK {
		t.Fatalf("GET /api/v1/me on the issuing instance = %d, want 200", got)
	}
	if got := b.GetWithToken(t, "/api/v1/me", token); got != http.StatusOK {
		t.Fatalf("GET /api/v1/me on the other instance = %d, want 200; the instances do not share a verification key", got)
	}

	// Revoke by logging out of instance A.
	resp := a.PostJSON(t, "/api/v1/auth/logout", token, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout on instance A = %d, want 204", resp.StatusCode)
	}

	if got := a.GetWithToken(t, "/api/v1/me", token); got == http.StatusOK {
		t.Error("the revoking instance still accepts the revoked token")
	}

	// The revocation event propagates over the cluster asynchronously,
	// so poll rather than assert on the first request. Neither instance
	// is restarted.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if b.GetWithToken(t, "/api/v1/me", token) != http.StatusOK {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("a token revoked on instance A is still accepted on instance B")
}

// The whole topology, running at once: web, enc, orchestrator and worker
// instances clustered together, each serving its own surface. This is
// step 3 of operations.md's migration path, and the case an operator
// actually deploys.
func TestSplitTopologyStartsAndServesEachSurface(t *testing.T) {
	c := testapp.NewCluster(t)

	const secret = "test-cluster-secret"
	seedAddr := testapp.FreeAddr(t)

	// web seeds the cluster; the others peer with it.
	web := c.Start(testapp.Config{
		Mode: runtime.ModeWeb,
		Env: map[string]string{
			"CONSOLE_CLUSTER_ADDR":   seedAddr,
			"CONSOLE_CLUSTER_SECRET": secret,
		},
	})
	peer := func(mode runtime.Mode) *testapp.Instance {
		return c.Start(testapp.Config{
			Mode: mode,
			Env: map[string]string{
				"CONSOLE_CLUSTER_ADDR":   testapp.FreeAddr(t),
				"CONSOLE_CLUSTER_PEERS":  seedAddr,
				"CONSOLE_CLUSTER_SECRET": secret,
			},
		})
	}
	orchestrator := peer(runtime.ModeOrchestrator)
	worker := peer(runtime.ModeWorker)

	// enc attaches as a leaf rather than a routed peer: outbound-only,
	// so the core needs no path back to an instance living next to a
	// compiler.
	enc := c.Start(testapp.Config{
		Mode: runtime.ModeENC,
		Env: map[string]string{
			"CONSOLE_CLUSTER_PEERS":  seedAddr,
			"CONSOLE_CLUSTER_MODE":   "leaf",
			"CONSOLE_CLUSTER_SECRET": secret,
			"CONSOLE_CLUSTER_ADDR":   "",
		},
	})

	// Each mode serves its own surface...
	if got := web.StatusOf(t, "/"); got != http.StatusOK {
		t.Errorf("web mode: GET / = %d, want 200", got)
	}
	if got := enc.StatusOf(t, "/api/v1/enc/node.example.com"); got == http.StatusNotFound {
		t.Error("enc mode does not serve the ENC route")
	}

	// ...and not another's.
	for _, inst := range []*testapp.Instance{enc, orchestrator, worker} {
		if got := inst.StatusOf(t, "/"); got != http.StatusNotFound {
			t.Errorf("%s mode: GET / = %d, want 404", inst.Mode, got)
		}
	}

	// Every mode is health-checkable and names itself.
	for _, inst := range []*testapp.Instance{web, enc, orchestrator, worker} {
		if got := inst.StatusOf(t, "/metrics"); got != http.StatusOK {
			t.Errorf("%s mode: GET /metrics = %d, want 200", inst.Mode, got)
		}
	}

	// Revocation crosses the whole topology, including to the leaf: an
	// enc instance must reject a service token revoked on web, or a
	// compiler would keep classifying with a dead credential.
	token := web.Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)
	if got := enc.GetWithToken(t, "/api/v1/enc/node.example.com", token); got == http.StatusUnauthorized {
		t.Fatalf("the enc instance rejected a valid token (%d); it does not share verification keys", got)
	}

	resp := web.PostJSON(t, "/api/v1/auth/logout", token, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout on web = %d, want 204", resp.StatusCode)
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if enc.GetWithToken(t, "/api/v1/enc/node.example.com", token) == http.StatusUnauthorized {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("a token revoked on the web instance is still accepted by the enc leaf instance")
}
