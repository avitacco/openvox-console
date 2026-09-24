// Package testapp starts real console instances in-process so a test can
// exercise behavior that only appears with more than one of them running:
// cross-instance event delivery, token revocation, dispatch routing to a
// node connected elsewhere, and singleton background work.
//
// It exists because none of that is observable from a unit test of any
// single package. The interesting failures - a revoked token still
// accepted on the other instance, one activity event persisted twice, two
// schedulers syncing the same provider - are properties of a *set* of
// instances, and only show up when a set of them is actually running.
//
// Instances are started by calling app.Run on a goroutine, exactly as the
// binary does, rather than by reimplementing its wiring here. A harness
// that built its own approximation of the composition root would happily
// pass while the real one was broken.
//
// It's a normal (non-_test.go) package specifically so its helpers can be
// imported by other packages' tests - never imported by any production
// code path.
package testapp

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/voxpupuli/enterprise-console/internal/app"
	"github.com/voxpupuli/enterprise-console/internal/persistence"
	"github.com/voxpupuli/enterprise-console/internal/runtime"
	"github.com/voxpupuli/enterprise-console/internal/testca"
)

// startTimeout bounds how long an instance may take to come up, and
// stopTimeout how long to shut down.
//
// Generous on purpose, for the same reason internal/messaging's
// readiness bound is: a test starting a whole topology runs eight full
// consoles - each with an embedded NATS server, a connection pool and an
// HTTP stack - while the rest of the suite tests in parallel. The
// instances do start under that load; they just take longer than a
// comfortable-looking bound would allow, and failing them for it makes
// the suite flaky rather than making it correct.
const (
	startTimeout = 90 * time.Second
	stopTimeout  = 60 * time.Second
)

// Instance is one running console.
type Instance struct {
	// Mode is the run mode this instance was started in.
	Mode runtime.Mode

	// BaseURL is the instance's own HTTP address, e.g.
	// "http://127.0.0.1:41234".
	BaseURL string

	// ClusterAddr is the address other instances should peer with, valid
	// when the instance was started with a peer listener.
	ClusterAddr string

	cancel   context.CancelFunc
	done     chan error
	stopOnce sync.Once
}

// Config describes one instance to start.
type Config struct {
	// Mode defaults to runtime.ModeAll.
	Mode runtime.Mode

	// Env supplies or overrides any CONSOLE_* setting for this instance,
	// so a test can configure a subsystem the harness does not know
	// about without this package growing a field per setting.
	Env map[string]string
}

// Cluster is a set of instances sharing one Postgres database.
type Cluster struct {
	t   *testing.T
	dsn string

	// ca issues every instance's client certificates, so that instances
	// in one cluster trust each other's material.
	ca *testca.CA

	// instanceDSN is dsn with a small connection pool, handed to the
	// instances themselves.
	//
	// pgxpool defaults to max(4, NumCPU) connections - 12 on a typical
	// developer machine. A test starting eight instances would reach for
	// ~96 connections against a Postgres whose default max_connections
	// is 100, and starve every other test package running in parallel.
	// Production wants the large pool; a test process running a whole
	// topology at once does not.
	instanceDSN string

	// keyFile is one RBAC signing key shared by every instance in the
	// cluster. A key per instance would mean a token issued by one
	// instance failed to verify on any other - which is not how a
	// load-balanced deployment is configured, and would make every
	// cross-instance authentication test meaningless.
	keyFile string

	mu        sync.Mutex
	instances []*Instance
}

// NewCluster prepares a cluster backed by the database named by
// CONSOLE_TEST_POSTGRES_DSN, skipping the test when that is unset - the
// same convention internal/testdb follows, so a developer without a test
// database sees skips rather than failures.
//
// Migrations are applied before any instance starts.
func NewCluster(t *testing.T) *Cluster {
	t.Helper()

	dsn := os.Getenv("CONSOLE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONSOLE_TEST_POSTGRES_DSN not set; skipping multi-instance test")
	}

	// Each cluster gets its own database.
	//
	// These instances run the real background workers, and those workers
	// act on whatever rows they find: the vulnerability scheduler syncs
	// every enabled provider in the database it is pointed at. Sharing
	// one database with the rest of the suite meant a console started
	// here would pick up another package's test fixtures and race that
	// package's own scheduler for the lease. Isolation is cheaper to
	// reason about than coordinating every package against live workers.
	dsn = createDatabase(t, dsn)

	if err := persistence.Migrate(dsn); err != nil {
		t.Fatalf("apply migrations to test database: %v", err)
	}

	return &Cluster{
		t: t, dsn: dsn, ca: testca.NewCA(t),
		keyFile:     signingKeyFile(t, runtime.DefaultSigningKeyID),
		instanceDSN: limitPoolSize(dsn),
	}
}

// DSN returns the shared Postgres connection string, for a test that also
// wants to read or write the database directly.
func (c *Cluster) DSN() string { return c.dsn }

// CA returns the cluster's certificate authority, for a test that needs
// to issue further certificates trusted by these instances (a node
// client certificate, say).
func (c *Cluster) CA() *testca.CA { return c.ca }

// Start launches one instance and blocks until it is serving, failing the
// test if it does not come up. The instance is stopped when the test
// finishes.
func (c *Cluster) Start(cfg Config) *Instance {
	c.t.Helper()

	if cfg.Mode == "" {
		cfg.Mode = runtime.ModeAll
	}

	httpAddr := freeAddr(c.t)
	env := c.baseEnv(cfg.Mode, httpAddr)
	for k, v := range cfg.Env {
		if v == "" {
			delete(env, k)
			continue
		}
		env[k] = v
	}

	runtimeCfg, err := runtime.LoadConfig(func(name string) string { return env[name] })
	if err != nil {
		c.t.Fatalf("load config for %s instance: %v", cfg.Mode, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	inst := &Instance{
		Mode:        cfg.Mode,
		BaseURL:     "http://" + httpAddr,
		ClusterAddr: env["CONSOLE_CLUSTER_ADDR"],
		cancel:      cancel,
		done:        make(chan error, 1),
	}

	// Discarded rather than routed to t.Logf: an instance outlives the
	// test function body by however long shutdown takes, and logging
	// after a test completes panics.
	logger := runtime.NewLogger(io.Discard)

	go func() { inst.done <- app.Run(ctx, runtimeCfg, logger) }()

	c.mu.Lock()
	c.instances = append(c.instances, inst)
	c.mu.Unlock()
	c.t.Cleanup(func() { inst.Stop(c.t) })

	inst.waitUntilServing(c.t)
	return inst
}

// TryStart attempts to start an instance and returns the error that
// prevented it, rather than failing the test. It is for asserting that a
// misconfiguration is refused - the case Start cannot express, since
// Start treats a failure to come up as a test failure.
//
// A nil return means the instance started; it is stopped before
// returning, since a test asserting refusal has no use for a running
// instance.
func (c *Cluster) TryStart(cfg Config) error {
	c.t.Helper()

	if cfg.Mode == "" {
		cfg.Mode = runtime.ModeAll
	}

	httpAddr := freeAddr(c.t)
	env := c.baseEnv(cfg.Mode, httpAddr)
	for k, v := range cfg.Env {
		if v == "" {
			delete(env, k)
			continue
		}
		env[k] = v
	}

	runtimeCfg, err := runtime.LoadConfig(func(name string) string { return env[name] })
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, runtimeCfg, runtime.NewLogger(io.Discard)) }()

	// Either Run fails quickly, or the instance comes up. Polling for
	// "serving" distinguishes the two without a fixed sleep.
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(startTimeout)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			if err == nil {
				return errors.New("instance exited without serving and without an error")
			}
			return err
		default:
		}

		resp, err := client.Get("http://" + httpAddr + "/health")
		if err == nil {
			resp.Body.Close()
			cancel()
			<-done
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("instance neither started serving nor failed within the start timeout")
}

// baseEnv is the minimum configuration an instance needs to start:
// the shared database, generated credentials, and its own HTTP address.
func (c *Cluster) baseEnv(mode runtime.Mode, httpAddr string) map[string]string {
	c.t.Helper()

	certPath, keyPath := c.ca.Issue(c.t, "console-test", false)

	// Written as "<kid>.pem" in its own directory so the same file serves
	// as both the signing key and the verification-key set - which is
	// exactly how `make rbac-keys` lays it out.
	keyFile := c.keyFile

	env := map[string]string{
		"CONSOLE_RUN_MODE":     string(mode),
		"CONSOLE_HTTP_ADDR":    httpAddr,
		"CONSOLE_POSTGRES_DSN": c.instanceDSN,

		// openvoxdb is not reachable in these tests. The client is
		// constructed from real certificate material so startup
		// succeeds; any request through it fails, which is correct for
		// tests that do not exercise openvoxdb-backed endpoints.
		"CONSOLE_OPENVOXDB_URL":       "https://127.0.0.1:1",
		"CONSOLE_OPENVOXDB_CERT_FILE": certPath,
		"CONSOLE_OPENVOXDB_KEY_FILE":  keyPath,
		"CONSOLE_OPENVOXDB_CA_FILE":   c.ca.PEMFile(c.t),

		// Both are supplied for every mode so a test can start any mode
		// without knowing which credential that mode requires. Which one
		// the instance actually reads is the behavior under test, not
		// something the harness should pre-empt: LoadConfig requires the
		// signing key only for an issuing mode, and app.Run opens it only
		// then.
		"CONSOLE_BOOTSTRAP_ADMIN_USERNAME": BootstrapAdminUser,
		"CONSOLE_BOOTSTRAP_ADMIN_PASSWORD": BootstrapAdminPassword,

		"CONSOLE_RBAC_SIGNING_KEY_FILE":      keyFile,
		"CONSOLE_RBAC_VERIFICATION_KEYS_DIR": filepath.Dir(keyFile),
	}

	// orchestrator mode refuses to start without a node transport
	// listener - holding node connections is the whole reason the mode
	// exists. Supply one so a test does not have to configure mTLS
	// material just to assert something unrelated; a test that cares
	// about the transport overrides these through Config.Env.
	if mode == runtime.ModeOrchestrator {
		transportCert, transportKey := c.ca.Issue(c.t, "console-transport", true)
		env["CONSOLE_NODE_TRANSPORT_ADDR"] = freeAddr(c.t)
		env["CONSOLE_NODE_TRANSPORT_CERT_FILE"] = transportCert
		env["CONSOLE_NODE_TRANSPORT_KEY_FILE"] = transportKey
		env["CONSOLE_NODE_TRANSPORT_CA_FILE"] = c.ca.PEMFile(c.t)
	}

	return env
}

// Stop shuts the instance down and waits for it to finish, failing the
// test if it does not exit cleanly. Safe to call more than once.
func (i *Instance) Stop(t *testing.T) {
	t.Helper()

	i.stopOnce.Do(func() {
		i.cancel()
		select {
		case err := <-i.done:
			// A cancelled context is the ordinary shutdown path.
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("%s instance exited with an error: %v", i.Mode, err)
			}
		case <-time.After(stopTimeout):
			t.Errorf("%s instance did not shut down within %s", i.Mode, stopTimeout)
		}
	})
}

// Get issues a GET against this instance's own HTTP surface. The caller
// closes the response body.
func (i *Instance) Get(t *testing.T, path string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, i.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("build request for %s: %v", path, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s on the %s instance: %v", path, i.Mode, err)
	}
	return resp
}

// StatusOf returns the status code this instance answers path with,
// closing the body. Convenient for asserting that a mode does or does not
// serve a route.
func (i *Instance) StatusOf(t *testing.T, path string) int {
	t.Helper()

	resp := i.Get(t, path)
	defer resp.Body.Close()
	return resp.StatusCode
}

// waitUntilServing polls the instance's health endpoint until it answers
// at all. It deliberately does not require a *healthy* answer: these
// tests point at no openvoxdb and sometimes no node transport, so an
// instance can legitimately report a degraded dependency while being
// perfectly able to serve the requests the test cares about.
func (i *Instance) waitUntilServing(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(startTimeout)
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		select {
		case err := <-i.done:
			t.Fatalf("%s instance exited before serving: %v", i.Mode, err)
		default:
		}

		resp, err := client.Get(i.BaseURL + "/health")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s instance did not start serving on %s within %s", i.Mode, i.BaseURL, startTimeout)
}

// freeAddr reserves a loopback port by binding and immediately releasing
// it. There is an inherent race between release and the instance's own
// bind, but the alternative - a fixed port - fails far more often, both
// against a developer's own running console and against a second instance
// in the same test.
func freeAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("release reserved port: %v", err)
	}
	return addr
}

// FreeAddr exposes freeAddr so a test can reserve an address for a
// listener the harness does not configure itself (a node transport or
// cluster peer listener, say).
func FreeAddr(t *testing.T) string { return freeAddr(t) }

// signingKeyFile writes a fresh ES256 private key in the PEM form
// rbac.LoadSigningKey reads, so an instance starts with its own generated
// key rather than a checked-in fixture.
func signingKeyFile(t *testing.T, kid string) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate RBAC signing key: %v", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal RBAC signing key: %v", err)
	}

	path := filepath.Join(t.TempDir(), kid+".pem")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create RBAC signing key file: %v", err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}); err != nil {
		t.Fatalf("write RBAC signing key: %v", err)
	}
	return path
}

// Logger returns a logger discarding output, for a test that constructs
// something needing one alongside an instance.
func Logger() *slog.Logger { return runtime.NewLogger(io.Discard) }

// PostJSON issues a POST with a JSON body and an optional bearer token
// against this instance. The caller closes the response body.
func (i *Instance) PostJSON(t *testing.T, path, token string, body any) *http.Response {
	t.Helper()

	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode request body: %v", err)
		}
		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(http.MethodPost, i.BaseURL+path, payload)
	if err != nil {
		t.Fatalf("build request for %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s on the %s instance: %v", path, i.Mode, err)
	}
	return resp
}

// GetWithToken issues an authenticated GET against this instance and
// returns the status code, closing the body.
func (i *Instance) GetWithToken(t *testing.T, path, token string) int {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, i.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("build request for %s: %v", path, err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s on the %s instance: %v", path, i.Mode, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// Login authenticates against this instance and returns the access
// token, failing the test if login does not succeed.
func (i *Instance) Login(t *testing.T, username, password string) string {
	t.Helper()

	resp := i.PostJSON(t, "/api/v1/auth/login", "", map[string]string{
		"username": username,
		"password": password,
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login on the %s instance: status %d", i.Mode, resp.StatusCode)
	}

	var body struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if body.AccessToken == "" {
		t.Fatal("login returned no access token")
	}
	return body.AccessToken
}

// BootstrapAdmin is the admin credential every cluster's instances are
// configured to create on first start, for tests that need to
// authenticate.
const (
	BootstrapAdminUser     = "harness-admin"
	BootstrapAdminPassword = "harness-admin-password"
)

// limitPoolSize adds a small pool cap to dsn. pgx reads pool_max_conns
// from the connection string itself, so this needs no production
// configuration knob.
func limitPoolSize(dsn string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "pool_max_conns=4"
}

// createDatabase makes a fresh, empty database for one cluster and
// returns a DSN pointing at it. It is dropped when the test finishes.
//
// Named after the test plus a random suffix so a leftover database from
// a killed run is identifiable rather than mysterious.
func createDatabase(t *testing.T, adminDSN string) string {
	t.Helper()

	name := "testapp_" + strings.ToLower(nonAlphanum.ReplaceAllString(t.Name(), "_")) +
		"_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if len(name) > 60 {
		name = name[:60]
	}

	admin, err := pgxpool.New(context.Background(), adminDSN)
	if err != nil {
		t.Fatalf("connect to create test database: %v", err)
	}
	defer admin.Close()

	// Identifiers cannot be parameterised, hence the interpolation; name
	// is built from the test name and a uuid, never from input.
	if _, err := admin.Exec(context.Background(), `CREATE DATABASE "`+name+`"`); err != nil {
		t.Fatalf("create test database %q: %v", name, err)
	}

	t.Cleanup(func() {
		cleanup, err := pgxpool.New(context.Background(), adminDSN)
		if err != nil {
			return
		}
		defer cleanup.Close()
		// FORCE because an instance's pool may not have finished closing
		// when the test ends; without it the drop fails and the database
		// is left behind.
		_, _ = cleanup.Exec(context.Background(), `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
	})

	return replaceDatabase(adminDSN, name)
}

// nonAlphanum matches anything not usable in a database identifier.
var nonAlphanum = regexp.MustCompile(`[^A-Za-z0-9]+`)

// replaceDatabase swaps the database name in a postgres URL.
func replaceDatabase(dsn, name string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	u.Path = "/" + name
	return u.String()
}
