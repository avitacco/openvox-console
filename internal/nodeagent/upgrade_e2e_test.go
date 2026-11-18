package nodeagent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/agentdist"
	"github.com/voxpupuli/enterprise-console/internal/agentupgrade"
	"github.com/voxpupuli/enterprise-console/internal/nodetransport"
)

// e2eStore is agentupgrade.Store in memory - just enough of it for one
// node to go through an upgrade.
type e2eStore struct {
	mu  sync.Mutex
	rec *agentupgrade.Record
}

func (s *e2eStore) RecordStatus(_ context.Context, certname string, status *agentupgrade.Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec == nil {
		s.rec = &agentupgrade.Record{Certname: certname, UpgradeState: agentupgrade.UpgradeIdle}
	}
	s.rec.Capable = status != nil
	if status != nil {
		s.rec.Status = *status
	}
	if s.rec.TargetDigest != "" && s.rec.Digest == s.rec.TargetDigest {
		s.rec.UpgradeState, s.rec.LastError = agentupgrade.UpgradeIdle, ""
	}
	return nil
}

func (s *e2eStore) Claim(_ context.Context, _, target string, _ int, _ bool, now time.Time) (bool, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec == nil || s.rec.UpgradeState != agentupgrade.UpgradeIdle {
		return false, 0, nil
	}
	s.rec.UpgradeState, s.rec.TargetDigest, s.rec.RequestedAt = agentupgrade.UpgradeRequested, target, &now
	return true, 0, nil
}

func (s *e2eStore) Release(context.Context, string, time.Time) error { return nil }

func (s *e2eStore) Fail(_ context.Context, _, reason string, _ bool, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rec.UpgradeState, s.rec.LastError = agentupgrade.UpgradeFailed, reason
	return nil
}

func (s *e2eStore) Get(context.Context, string) (*agentupgrade.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec == nil {
		return nil, nil
	}
	copied := *s.rec
	return &copied, nil
}

func (s *e2eStore) List(ctx context.Context) ([]agentupgrade.Record, error) {
	r, _ := s.Get(ctx, "")
	if r == nil {
		return nil, nil
	}
	return []agentupgrade.Record{*r}, nil
}

// e2eCatalog is a console serving one client build as a bare binary.
type e2eCatalog struct {
	build    agentdist.Build
	artifact agentdist.Artifact
}

func (c e2eCatalog) Build(string, string) (agentdist.Build, bool) { return c.build, true }
func (c e2eCatalog) Artifact(string, string, string) (agentdist.Artifact, bool) {
	return c.artifact, true
}

// The whole loop over a real node transport: the console's coordinator
// reads a connecting client's build, finds it older than the build it
// serves, and tells it to upgrade; the real client downloads the
// artifact over HTTP, verifies and installs it, replies, and restarts;
// on reconnecting it reports the new build and the console marks it
// current. Only the process boundary is simulated - "restarting" swaps
// the client's reported build, since a test cannot re-exec itself.
//
// What this pins down that the two sides' own tests cannot is the wire
// contract between them, which each encodes with its own copy of the
// types.
func TestEndToEnd_ConsoleUpgradesAnOutOfDateClient(t *testing.T) {
	const certname = "web01.example.com"
	oldClient, newClient := []byte("old client"), []byte("new client build")
	target := BuildInfo{Version: "v2", BuildTime: "2026-11-11T09:30:00Z", Digest: digestOf(newClient)}
	targetTime, _ := time.Parse(time.RFC3339, target.BuildTime)

	// The console's unauthenticated package route.
	artifacts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/packages/node-agent-client" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(newClient)
	}))
	defer artifacts.Close()

	// The node.
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "node-agent-client")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, oldClient, 0o755); err != nil {
		t.Fatal(err)
	}
	restarted := make(chan int, 1)
	upgrader := &Upgrader{
		cfg:    Config{AcceptUpgrades: true, UpgradeDir: filepath.Join(dir, "upgrade")},
		goos:   "darwin", // installed as a bare binary, replaced in place
		goarch: "arm64",
		exe:    exe,
		info:   BuildInfo{Version: "v1", BuildTime: "2026-01-01T00:00:00Z", Digest: digestOf(oldClient)},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		// Stands in for executing the candidate: reports the build of
		// whatever file it was pointed at.
		runner: func(_ context.Context, name string, _ ...string) (string, string, int, error) {
			digest, _, err := fileSHA256(name)
			if err != nil {
				return "", "", 0, err
			}
			out, _ := json.Marshal(BuildInfo{Version: target.Version, BuildTime: target.BuildTime, Digest: digest})
			return string(out), "", 0, nil
		},
		download: httpDownload,
	}
	var infoMu sync.Mutex
	upgrader.exit = func(code int) {
		// The service manager starts the new binary: from here on the
		// client is whatever is installed.
		digest, _, _ := fileSHA256(exe)
		infoMu.Lock()
		upgrader.info = BuildInfo{Version: target.Version, BuildTime: target.BuildTime, Digest: digest}
		infoMu.Unlock()
		restarted <- code
	}
	// Status reads upgrader.info; serialize it against the "restart".
	agent := agentActionsFunc{
		status: func(ctx context.Context) []byte {
			infoMu.Lock()
			defer infoMu.Unlock()
			return upgrader.Status(ctx)
		},
		upgrade: upgrader.Upgrade,
	}

	transport, ca := startTestTransport(t)
	certPath, keyPath := ca.Issue(t, certname, false)
	client := New(Config{TransportAddr: transport.Addr(), CertFile: certPath, KeyFile: keyPath, CAFile: ca.PEMFile(t)},
		NewHandlerWithUpgrades(nil, "puppet", t.TempDir(), noFileExists, nil, agent), nil)
	client.SetAfterReply(upgrader.TakeAfterReply)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.Run(ctx)
	waitFor(t, "the client to connect", func() bool { return transport.Registry().Lookup(certname) })

	// The console.
	dispatcher, err := nodetransport.NewDispatcher(transport.Bus())
	if err != nil {
		t.Fatal(err)
	}
	defer dispatcher.Close()
	store := &e2eStore{}
	coordinator := agentupgrade.NewCoordinator(agentupgrade.Config{
		Store:     store,
		Transport: dispatcher,
		Catalog: e2eCatalog{
			build: agentdist.Build{Digest: target.Digest, BuildTime: targetTime, Version: target.Version},
			artifact: agentdist.Artifact{
				Path: "/packages/node-agent-client", Size: int64(len(newClient)),
				SHA256: digestOf(newClient), Format: agentdist.FormatBinary,
			},
		},
		Connected:   func() []string { return []string{certname} },
		BaseURL:     artifacts.URL,
		AutoUpgrade: true,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	coordinator.Start(ctx)
	defer coordinator.Stop()

	coordinator.Notify(certname) // the node connected

	select {
	case code := <-restarted:
		if code != 0 {
			t.Errorf("the client exited %d, want a clean exit for launchd to restart", code)
		}
	case <-time.After(10 * time.Second):
		rec, _ := store.Get(ctx, certname)
		t.Fatalf("the client never restarted into the new build; console record: %+v", rec)
	}
	if got, _ := os.ReadFile(exe); string(got) != string(newClient) {
		t.Fatalf("installed client = %q, want the new build", got)
	}
	if _, err := os.Stat(upgrader.cfg.UpgradeDir); !os.IsNotExist(err) {
		t.Error("the staged artifact was left on the node")
	}

	// Accepted is not yet confirmed.
	rec, _ := store.Get(ctx, certname)
	if rec.UpgradeState != agentupgrade.UpgradeRequested || rec.Digest != digestOf(oldClient) {
		t.Errorf("before the node reports back: %+v", rec)
	}
	view := agentupgrade.View(rec, e2eCatalogOf(coordinatorCatalog(target, targetTime, newClient)))
	if view.State != agentupgrade.StateOutdated {
		t.Errorf("state before confirmation = %s, want outdated", view.State)
	}

	// The restarted client reconnects. The upgrade's own reply is still
	// on its way when the client restarts, so wait for the console to
	// have recorded it before simulating the reconnect.
	waitFor(t, "the busy hold to end", func() bool { return !client.busy.Load() })
	coordinator.Notify(certname)
	waitFor(t, "the console to record the new build", func() bool {
		rec, _ := store.Get(ctx, certname)
		return rec != nil && rec.Digest == target.Digest && rec.UpgradeState == agentupgrade.UpgradeIdle
	})
	rec, _ = store.Get(ctx, certname)
	if rec.Version != "v2" || rec.OS != "darwin" || rec.Arch != "arm64" || rec.InstallMethod != "binary" ||
		rec.BuildTime == nil || !rec.BuildTime.Equal(targetTime) || !rec.AcceptsUpgrades || rec.LastError != "" {
		t.Errorf("recorded build after the upgrade: %+v", rec)
	}
}

func coordinatorCatalog(target BuildInfo, targetTime time.Time, newClient []byte) e2eCatalog {
	return e2eCatalog{
		build:    agentdist.Build{Digest: target.Digest, BuildTime: targetTime, Version: target.Version},
		artifact: agentdist.Artifact{Path: "/packages/node-agent-client", Size: int64(len(newClient)), SHA256: digestOf(newClient), Format: agentdist.FormatBinary},
	}
}

func e2eCatalogOf(c e2eCatalog) agentupgrade.Catalog { return c }

type agentActionsFunc struct {
	status  func(ctx context.Context) []byte
	upgrade func(ctx context.Context, params json.RawMessage) []byte
}

func (a agentActionsFunc) Status(ctx context.Context) []byte { return a.status(ctx) }
func (a agentActionsFunc) Upgrade(ctx context.Context, p json.RawMessage) []byte {
	return a.upgrade(ctx, p)
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
