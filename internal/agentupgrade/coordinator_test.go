package agentupgrade

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/nodetransport"
)

// memStore is Store in memory, with the same rules as PgStore's SQL.
type memStore struct {
	mu   sync.Mutex
	rows map[string]*Record
}

func newMemStore() *memStore { return &memStore{rows: map[string]*Record{}} }

func (s *memStore) RecordStatus(_ context.Context, certname string, status *Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rows[certname]
	if r == nil {
		r = &Record{Certname: certname, UpgradeState: UpgradeIdle}
		s.rows[certname] = r
	}
	r.Capable = status != nil
	r.Status = Status{AcceptsUpgrades: true}
	if status != nil {
		r.Status = *status
	}
	if r.TargetDigest != "" && r.Digest == r.TargetDigest {
		r.UpgradeState, r.Attempts, r.LastError, r.RequestedAt, r.NextAttemptAt = UpgradeIdle, 0, "", nil, nil
	}
	return nil
}

func (s *memStore) Claim(_ context.Context, certname, target string, limit int, force bool, now time.Time) (bool, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rows[certname]
	if r == nil || r.UpgradeState == UpgradeRequested {
		return false, 0, nil
	}
	if !force {
		if r.TargetDigest == target && (r.UpgradeState == UpgradeExhausted || (r.NextAttemptAt != nil && r.NextAttemptAt.After(now))) {
			return false, 0, nil
		}
		requested := 0
		for _, other := range s.rows {
			if other.UpgradeState == UpgradeRequested {
				requested++
			}
		}
		if requested >= limit {
			return false, 0, nil
		}
	}
	if force || r.TargetDigest != target {
		r.Attempts = 0
	}
	r.UpgradeState, r.TargetDigest, r.RequestedAt, r.NextAttemptAt = UpgradeRequested, target, &now, nil
	return true, r.Attempts, nil
}

func (s *memStore) Release(_ context.Context, certname string, retryAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.rows[certname]; r != nil && r.UpgradeState == UpgradeRequested {
		r.UpgradeState, r.RequestedAt, r.NextAttemptAt = UpgradeIdle, nil, &retryAt
	}
	return nil
}

func (s *memStore) Fail(_ context.Context, certname, reason string, exhausted bool, retryAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rows[certname]
	if r == nil || r.UpgradeState != UpgradeRequested {
		return nil
	}
	r.UpgradeState = UpgradeFailed
	if exhausted {
		r.UpgradeState = UpgradeExhausted
	}
	r.Attempts++
	r.LastError, r.RequestedAt, r.NextAttemptAt = reason, nil, &retryAt
	return nil
}

func (s *memStore) Get(_ context.Context, certname string) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.rows[certname]; r != nil {
		copied := *r
		return &copied, nil
	}
	return nil, nil
}

func (s *memStore) List(context.Context) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, *r)
	}
	return out, nil
}

func (s *memStore) row(t *testing.T, certname string) Record {
	t.Helper()
	r, _ := s.Get(context.Background(), certname)
	if r == nil {
		t.Fatalf("no record for %s", certname)
	}
	return *r
}

// fakeNode is one node's client as the transport sees it.
type fakeNode struct {
	connected bool
	old       bool // predates agent_status
	busy      bool // executing something else
	// deaf is how many more requests find nobody listening although
	// the node is connected: its client has not subscribed yet.
	deaf     int
	digest   string // what it currently runs
	built    string
	os       string
	method   string
	declines bool
	// upgradeError, when set, is the client's answer to agent_upgrade.
	// Otherwise it answers "started" and, if adopts is set, runs the
	// requested build from then on.
	upgradeError string
	adopts       bool
	silent       bool // never answers agent_upgrade
}

type fakeTransport struct {
	mu       sync.Mutex
	nodes    map[string]*fakeNode
	upgrades map[string][]upgradeParams
	asked    map[string]int
}

func (f *fakeTransport) Dispatch(_ context.Context, certname string, payload []byte, _ time.Duration) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.nodes[certname]
	if n == nil || !n.connected {
		return nil, nodetransport.ErrNodeNotConnected
	}
	if n.deaf > 0 {
		// Connected, but its client has not subscribed yet: the
		// transport finds nobody listening.
		n.deaf--
		return nil, nodetransport.ErrNodeNotConnected
	}
	var req wireRequest
	_ = json.Unmarshal(payload, &req)
	fail := func(msg string) ([]byte, error) {
		out, _ := json.Marshal(map[string]string{"error": msg})
		return out, nil
	}
	// A real client answers a build-status request whatever it is
	// doing; busy refuses everything else.
	if n.busy && req.Action != actionAgentStatus {
		return fail("node-agent is busy executing another request")
	}
	if n.old {
		return fail(`unsupported action "` + req.Action + `"`)
	}
	switch req.Action {
	case actionAgentStatus:
		f.asked[certname]++
		out, _ := json.Marshal(map[string]any{"result": map[string]any{"agent": map[string]any{
			"digest": n.digest, "buildTime": n.built, "version": "v1", "os": n.os, "arch": "amd64",
			"installMethod": n.method, "acceptsUpgrades": !n.declines,
		}}})
		return out, nil
	case actionAgentUpgrade:
		var p upgradeParams
		_ = json.Unmarshal(req.Params, &p)
		f.upgrades[certname] = append(f.upgrades[certname], p)
		if n.silent {
			return nil, errors.New("wait for node-agent response: context deadline exceeded")
		}
		if n.upgradeError != "" {
			return fail(n.upgradeError)
		}
		if n.adopts {
			n.digest, n.built = p.Digest, p.BuildTime
		}
		return []byte(`{"result":{"upgrade":{"started":true}}}`), nil
	}
	return fail(`unsupported action "` + req.Action + `"`)
}

func (f *fakeTransport) sent(certname string) []upgradeParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]upgradeParams(nil), f.upgrades[certname]...)
}

type harness struct {
	*Coordinator
	store     *memStore
	transport *fakeTransport
	clock     time.Time
	catalog   fakeCatalog
}

func oldNode() *fakeNode {
	return &fakeNode{connected: true, digest: "old", built: consoleBuilt.Add(-24 * time.Hour).Format(time.RFC3339), os: "linux", method: "deb", adopts: true}
}

func newHarness(auto bool, nodes map[string]*fakeNode) *harness {
	h := &harness{
		store:     newMemStore(),
		transport: &fakeTransport{nodes: nodes, upgrades: map[string][]upgradeParams{}, asked: map[string]int{}},
		clock:     consoleBuilt.Add(48 * time.Hour),
		catalog:   stampedCatalog(),
	}
	h.rebuild(auto)
	return h
}

// rebuild makes a fresh Coordinator over the same store and nodes - a
// console restart, possibly onto a different client build.
func (h *harness) rebuild(auto bool) {
	h.Coordinator = NewCoordinator(Config{
		Store: h.store, Transport: h.transport, Catalog: h.catalog,
		Connected: func() []string {
			h.transport.mu.Lock()
			defer h.transport.mu.Unlock()
			var out []string
			for name, n := range h.transport.nodes {
				if n.connected {
					out = append(out, name)
				}
			}
			return out
		},
		BaseURL: "https://console.example.com/", AutoUpgrade: auto,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:    func() time.Time { return h.clock },
	})
	// No waiting in tests; the ones about a node that is not listening
	// yet set their own.
	h.Coordinator.settleDelays = nil
}

// connect delivers a connect notification and waits for everything it
// set off, the way Start's worker would.
func (h *harness) connect(certname string) {
	h.onConnect(context.Background(), certname)
	h.wg.Wait()
}

func (h *harness) sweep() {
	h.Sweep(context.Background())
	h.wg.Wait()
}

func TestConnect_RecordsBuild(t *testing.T) {
	h := newHarness(false, map[string]*fakeNode{"n1": oldNode()})
	h.connect("n1")

	r := h.store.row(t, "n1")
	if !r.Capable || r.Digest != "old" || r.OS != "linux" || r.InstallMethod != "deb" || r.BuildTime == nil || !r.AcceptsUpgrades {
		t.Errorf("recorded %+v", r)
	}
}

func TestConnect_ClientThatPredatesBuildReporting(t *testing.T) {
	n := oldNode()
	n.old = true
	h := newHarness(true, map[string]*fakeNode{"n1": n})
	h.connect("n1")

	r := h.store.row(t, "n1")
	if r.Capable {
		t.Error("a client answering `unsupported action` was recorded as able to report")
	}
	if a := Assess(&r, h.catalog); a.State != StateUnknown || a.Blocked != BlockedInstallScript {
		t.Errorf("assessment = %+v, want unknown / install-script", a)
	}
	if len(h.transport.sent("n1")) != 0 {
		t.Error("an upgrade was sent to a client that cannot upgrade")
	}
}

func TestConnect_OutOfDateNodeIsUpgradedAndConfirmed(t *testing.T) {
	h := newHarness(true, map[string]*fakeNode{"n1": oldNode()})
	h.connect("n1")

	sent := h.transport.sent("n1")
	if len(sent) != 1 {
		t.Fatalf("upgrade requests = %d, want 1", len(sent))
	}
	p := sent[0]
	if p.Digest != "console-build" || p.BuildTime != consoleBuilt.Format(time.RFC3339) ||
		p.Artifact.URL != "https://console.example.com/packages/deb" || p.Artifact.SHA256 != "artifact-deb" ||
		p.Artifact.Size != 10 || p.Artifact.Format != "deb" {
		t.Errorf("upgrade request = %+v", p)
	}
	// Accepted is not confirmed: only the node reporting the build is.
	if r := h.store.row(t, "n1"); r.UpgradeState != UpgradeRequested {
		t.Errorf("state after an accepted upgrade = %s, want requested", r.UpgradeState)
	}

	h.connect("n1") // the client restarts and reconnects on the new build
	r := h.store.row(t, "n1")
	if r.UpgradeState != UpgradeIdle || r.Digest != "console-build" || r.LastError != "" {
		t.Errorf("after reconnecting on the new build: %+v", r)
	}
	if a := Assess(&r, h.catalog); a.State != StateCurrent {
		t.Errorf("state = %s, want current", a.State)
	}
	if len(h.transport.sent("n1")) != 1 {
		t.Error("a current node was sent another upgrade")
	}
}

func TestAutoUpgradeOff_RecordsButNeverUpgrades(t *testing.T) {
	h := newHarness(false, map[string]*fakeNode{"n1": oldNode()})
	h.connect("n1")
	h.sweep()

	if len(h.transport.sent("n1")) != 0 {
		t.Error("an upgrade was sent with automatic upgrades off")
	}
	r := h.store.row(t, "n1")
	if a := Assess(&r, h.catalog); a.State != StateOutdated {
		t.Errorf("state = %s; an out-of-date node must still be reported as such", a.State)
	}
}

func TestNewerAndCurrentClientsAreLeftAlone(t *testing.T) {
	newer := oldNode()
	newer.digest, newer.built = "newer", consoleBuilt.Add(time.Hour).Format(time.RFC3339)
	current := oldNode()
	current.digest, current.built = "console-build", consoleBuilt.Format(time.RFC3339)
	h := newHarness(true, map[string]*fakeNode{"newer": newer, "current": current})
	h.connect("newer")
	h.connect("current")
	h.sweep()

	if len(h.transport.sent("newer"))+len(h.transport.sent("current")) != 0 {
		t.Error("a current or newer client was told to upgrade")
	}
}

func TestUnstampedConsoleUpgradesNobody(t *testing.T) {
	h := newHarness(true, map[string]*fakeNode{"n1": oldNode()})
	h.catalog.build.BuildTime = time.Time{}
	h.rebuild(true)
	h.connect("n1")
	h.sweep()
	if len(h.transport.sent("n1")) != 0 {
		t.Error("a console with no stamped client build sent an upgrade")
	}
}

func TestBusyNodeIsRetriedWithoutCountingAnAttempt(t *testing.T) {
	n := oldNode()
	h := newHarness(true, map[string]*fakeNode{"n1": n})
	h.connect("n1")
	// Put it back to out of date and busy for the next try.
	h.store.rows["n1"].UpgradeState = UpgradeIdle
	n.busy = true

	h.sweep()
	r := h.store.row(t, "n1")
	if r.UpgradeState != UpgradeIdle || r.Attempts != 0 || r.LastError != "" {
		t.Errorf("after a busy reply: %+v, want idle with no attempt and no error", r)
	}
	if r.NextAttemptAt == nil || !r.NextAttemptAt.Equal(h.clock.Add(time.Minute)) {
		t.Errorf("next attempt = %v, want one minute on", r.NextAttemptAt)
	}

	// Not before it is due...
	n.busy = false
	h.sweep()
	if got := len(h.transport.sent("n1")); got != 1 {
		t.Errorf("upgrade requests = %d before the retry was due, want 1", got)
	}
	// ...and then it goes through.
	h.clock = h.clock.Add(time.Minute)
	h.sweep()
	if got := len(h.transport.sent("n1")); got != 2 {
		t.Errorf("upgrade requests = %d after the retry was due, want 2", got)
	}
}

// A client answers a build-status request whatever it is doing, so a
// node that is busy when it connects has its build read straight away.
// Only the upgrade is refused, and that waits for the work to end.
func TestBusyOnConnect_BuildIsReadAndTheUpgradeWaits(t *testing.T) {
	n := oldNode()
	n.busy = true
	h := newHarness(true, map[string]*fakeNode{"n1": n})
	h.connect("n1")

	r := h.store.row(t, "n1")
	if !r.Capable || r.Digest != "old" {
		t.Fatalf("after connecting busy: %+v, want the build read", r)
	}
	if r.UpgradeState != UpgradeIdle || r.Attempts != 0 || r.LastError != "" {
		t.Errorf("after a refused upgrade: %+v, want idle with no attempt and no error", r)
	}
	if n.digest != "old" {
		t.Error("a busy node was upgraded")
	}

	n.busy = false
	h.clock = h.clock.Add(time.Minute)
	h.sweep()
	if r := h.store.row(t, "n1"); r.UpgradeState != UpgradeRequested {
		t.Errorf("once the node was free and the retry due: %+v, want the upgrade requested", r)
	}
	if n.digest == "old" {
		t.Error("the node was not upgraded once it was free")
	}
}

func TestFailure_RecordsReasonBacksOffAndExhausts(t *testing.T) {
	n := oldNode()
	n.upgradeError = "the downloaded artifact does not match the request: digest aaa, expected bbb"
	h := newHarness(true, map[string]*fakeNode{"n1": n})
	h.connect("n1")

	wantWaits := []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute}
	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		r := h.store.row(t, "n1")
		if r.Attempts != attempt || !strings.Contains(r.LastError, "does not match the request") {
			t.Fatalf("after attempt %d: attempts=%d error=%q", attempt, r.Attempts, r.LastError)
		}
		if r.Digest != "old" {
			t.Fatal("a failed upgrade changed the recorded build")
		}
		if attempt == MaxAttempts {
			if r.UpgradeState != UpgradeExhausted {
				t.Fatalf("state after %d failures = %s, want exhausted", attempt, r.UpgradeState)
			}
			break
		}
		if r.UpgradeState != UpgradeFailed {
			t.Fatalf("state after attempt %d = %s, want failed", attempt, r.UpgradeState)
		}
		wait := wantWaits[attempt-1]
		if !r.NextAttemptAt.Equal(h.clock.Add(wait)) {
			t.Fatalf("attempt %d: next attempt in %s, want %s", attempt, r.NextAttemptAt.Sub(h.clock), wait)
		}

		// A sweep just short of the wait does nothing.
		h.clock = h.clock.Add(wait - time.Second)
		h.sweep()
		if got := len(h.transport.sent("n1")); got != attempt {
			t.Fatalf("retried before the backoff elapsed (%d requests after %d attempts)", got, attempt)
		}
		h.clock = h.clock.Add(time.Second)
		h.sweep()
	}

	// Exhausted: no amount of waiting retries it.
	h.clock = h.clock.Add(7 * 24 * time.Hour)
	h.sweep()
	if got := len(h.transport.sent("n1")); got != MaxAttempts {
		t.Errorf("upgrade requests = %d, want %d and no more once exhausted", got, MaxAttempts)
	}
}

func TestExhausted_NewConsoleBuildStartsAgain(t *testing.T) {
	n := oldNode()
	n.upgradeError = "could not download the upgrade artifact"
	h := newHarness(true, map[string]*fakeNode{"n1": n})
	h.connect("n1")
	for range MaxAttempts {
		h.clock = h.clock.Add(7 * time.Hour)
		h.sweep()
	}
	if r := h.store.row(t, "n1"); r.UpgradeState != UpgradeExhausted {
		t.Fatalf("state = %s, want exhausted", r.UpgradeState)
	}
	before := len(h.transport.sent("n1"))

	// The console is replaced by one serving a different, newer build.
	h.catalog.build.Digest = "console-build-2"
	h.catalog.build.BuildTime = consoleBuilt.Add(time.Hour)
	h.rebuild(true)
	n.upgradeError = ""
	h.sweep()

	sent := h.transport.sent("n1")
	if len(sent) != before+1 || sent[len(sent)-1].Digest != "console-build-2" {
		t.Fatalf("a new console build did not restart the upgrade: %d requests", len(sent)-before)
	}
	if r := h.store.row(t, "n1"); r.Attempts != 0 || r.UpgradeState != UpgradeRequested {
		t.Errorf("after the new build's first attempt: %+v", r)
	}
}

func TestAcceptedButNeverReportsTheNewBuild(t *testing.T) {
	n := oldNode()
	n.adopts = false // says "started" and keeps running the old build
	h := newHarness(true, map[string]*fakeNode{"n1": n})
	h.connect("n1")

	h.clock = h.clock.Add(ConfirmTimeout - time.Second)
	h.connect("n1") // reconnects, still on the old build
	h.sweep()
	if r := h.store.row(t, "n1"); r.UpgradeState != UpgradeRequested {
		t.Fatalf("timed out early: %+v", r)
	}

	h.clock = h.clock.Add(time.Second)
	h.sweep()
	r := h.store.row(t, "n1")
	if r.UpgradeState != UpgradeFailed || r.Attempts != 1 || !strings.Contains(r.LastError, "did not report the new build") {
		t.Errorf("after the confirmation time ran out: %+v", r)
	}
}

func TestNoReplyIsLeftToTheConfirmationTimeout(t *testing.T) {
	n := oldNode()
	n.silent = true
	h := newHarness(true, map[string]*fakeNode{"n1": n})
	h.connect("n1")
	if r := h.store.row(t, "n1"); r.UpgradeState != UpgradeRequested || r.Attempts != 0 {
		t.Fatalf("after no reply: %+v, want still requested", r)
	}
	h.clock = h.clock.Add(ConfirmTimeout)
	h.sweep()
	if r := h.store.row(t, "n1"); r.UpgradeState != UpgradeFailed || r.Attempts != 1 {
		t.Errorf("after the confirmation time ran out: %+v", r)
	}
}

func TestTimeout_AnyInstanceExpiresANodeItCannotSee(t *testing.T) {
	n := oldNode()
	n.adopts = false
	h := newHarness(true, map[string]*fakeNode{"n1": n})
	h.connect("n1")
	n.connected = false // gone, and this instance has no connection to it

	h.clock = h.clock.Add(ConfirmTimeout)
	h.sweep()
	if r := h.store.row(t, "n1"); r.UpgradeState != UpgradeFailed {
		t.Errorf("a disconnected node's unconfirmed upgrade was not timed out: %+v", r)
	}
}

func TestManyOutOfDateNodes_BoundedAtOnce(t *testing.T) {
	nodes := map[string]*fakeNode{}
	for i := range MaxConcurrent + 15 {
		n := oldNode()
		n.adopts = false // every upgrade stays unconfirmed, holding its slot
		nodes["n"+string(rune('a'+i/26))+string(rune('a'+i%26))] = n
	}
	h := newHarness(true, nodes)
	for name := range nodes {
		h.connect(name)
	}
	h.sweep()

	requested := 0
	for name := range nodes {
		if got := len(h.transport.sent(name)); got > 1 {
			t.Errorf("%s was instructed %d times", name, got)
		} else if got == 1 {
			requested++
		}
	}
	if requested != MaxConcurrent {
		t.Errorf("%d nodes instructed at once, want exactly %d", requested, MaxConcurrent)
	}
}

func TestTwoInstances_OneNodeIsInstructedOnce(t *testing.T) {
	n := oldNode()
	n.adopts = false
	h := newHarness(true, map[string]*fakeNode{"n1": n})
	h.connect("n1")

	// A second instance over the same store sees the same node.
	other := NewCoordinator(Config{
		Store: h.store, Transport: h.transport, Catalog: h.catalog,
		Connected: func() []string { return []string{"n1"} },
		BaseURL:   "https://console.example.com", AutoUpgrade: true,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:    func() time.Time { return h.clock },
	})
	other.Sweep(context.Background())
	other.wg.Wait()
	other.onConnect(context.Background(), "n1")
	other.wg.Wait()

	if got := len(h.transport.sent("n1")); got != 1 {
		t.Errorf("the node was instructed %d times across two instances, want 1", got)
	}
}

func TestDeclinedAndUnupgradableNodesAreNotInstructed(t *testing.T) {
	declines := oldNode()
	declines.declines = true
	scriptManaged := oldNode()
	scriptManaged.method = "unknown"
	h := newHarness(true, map[string]*fakeNode{"declines": declines, "script": scriptManaged})
	h.connect("declines")
	h.connect("script")
	h.sweep()
	if len(h.transport.sent("declines"))+len(h.transport.sent("script")) != 0 {
		t.Error("a node that declines upgrades or cannot upgrade in place was instructed")
	}
}

func TestUpgradeNow(t *testing.T) {
	ctx := context.Background()

	t.Run("upgrades with automatic upgrades off", func(t *testing.T) {
		h := newHarness(false, map[string]*fakeNode{"n1": oldNode()})
		if err := h.UpgradeNow(ctx, "n1"); err != nil {
			t.Fatal(err)
		}
		h.wg.Wait()
		if len(h.transport.sent("n1")) != 1 {
			t.Error("no upgrade was sent")
		}
	})

	t.Run("retries a node whose automatic attempts were exhausted", func(t *testing.T) {
		n := oldNode()
		n.upgradeError = "could not download"
		h := newHarness(true, map[string]*fakeNode{"n1": n})
		h.connect("n1")
		for range MaxAttempts {
			h.clock = h.clock.Add(7 * time.Hour)
			h.sweep()
		}
		n.upgradeError = ""
		before := len(h.transport.sent("n1"))
		if err := h.UpgradeNow(ctx, "n1"); err != nil {
			t.Fatal(err)
		}
		h.wg.Wait()
		if len(h.transport.sent("n1")) != before+1 {
			t.Error("an operator's retry of an exhausted node sent nothing")
		}
		if r := h.store.row(t, "n1"); r.Attempts != 0 || r.UpgradeState != UpgradeRequested {
			t.Errorf("after an operator's retry: %+v", r)
		}
	})

	t.Run("not connected", func(t *testing.T) {
		n := oldNode()
		n.connected = false
		h := newHarness(true, map[string]*fakeNode{"n1": n})
		if err := h.UpgradeNow(ctx, "n1"); !errors.Is(err, ErrNotConnected) {
			t.Errorf("err = %v, want ErrNotConnected", err)
		}
	})

	// The client answers the build-status request although it is busy,
	// and refuses the upgrade. The operator is told so, the claim is
	// given back, and nothing counts as a failed attempt.
	t.Run("busy", func(t *testing.T) {
		n := oldNode()
		n.busy = true
		h := newHarness(true, map[string]*fakeNode{"n1": n})
		if err := h.UpgradeNow(ctx, "n1"); !errors.Is(err, ErrBusy) {
			t.Errorf("err = %v, want ErrBusy", err)
		}
		h.wg.Wait()
		r := h.store.row(t, "n1")
		if r.UpgradeState != UpgradeIdle || r.Attempts != 0 || r.LastError != "" {
			t.Errorf("after a refused manual upgrade: %+v, want idle with no attempt and no error", r)
		}
		if n.digest != "old" {
			t.Error("a busy node was upgraded")
		}
	})

	t.Run("already current", func(t *testing.T) {
		n := oldNode()
		n.digest, n.built = "console-build", consoleBuilt.Format(time.RFC3339)
		h := newHarness(true, map[string]*fakeNode{"n1": n})
		if err := h.UpgradeNow(ctx, "n1"); !errors.Is(err, ErrCurrent) {
			t.Errorf("err = %v, want ErrCurrent", err)
		}
	})

	t.Run("client cannot upgrade itself", func(t *testing.T) {
		n := oldNode()
		n.old = true
		h := newHarness(true, map[string]*fakeNode{"n1": n})
		var cannot *CannotUpgradeError
		if err := h.UpgradeNow(ctx, "n1"); !errors.As(err, &cannot) || cannot.Blocked != BlockedInstallScript {
			t.Errorf("err = %v, want CannotUpgradeError(install-script)", err)
		}
		if len(h.transport.sent("n1")) != 0 {
			t.Error("an upgrade was sent")
		}
	})

	t.Run("upgrade already in progress", func(t *testing.T) {
		n := oldNode()
		n.adopts = false
		h := newHarness(true, map[string]*fakeNode{"n1": n})
		h.connect("n1")
		if err := h.UpgradeNow(ctx, "n1"); !errors.Is(err, ErrInProgress) {
			t.Errorf("err = %v, want ErrInProgress", err)
		}
	})
}

func TestNotify_NeverBlocks(t *testing.T) {
	h := newHarness(true, map[string]*fakeNode{})
	done := make(chan struct{})
	go func() {
		for range queueDepth + 50 {
			h.Notify("n1")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Notify blocked on a full queue")
	}
}

func TestStartStop_HandlesAConnect(t *testing.T) {
	h := newHarness(true, map[string]*fakeNode{"n1": oldNode()})
	h.Start(context.Background())
	h.Notify("n1")

	deadline := time.Now().Add(3 * time.Second)
	for len(h.transport.sent("n1")) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	// Stop alone has to end the sweep: the caller's context may never
	// be cancelled if startup fails part-way.
	h.Stop()
	if len(h.transport.sent("n1")) != 1 {
		t.Errorf("upgrade requests = %d, want 1", len(h.transport.sent("n1")))
	}
}

// A connect is announced before the node's client has subscribed, so
// the first request can find nobody listening. That must not be taken
// for the node having gone: it is asked again, and its build is read.
func TestOnConnect_NodeNotListeningYetIsAskedAgain(t *testing.T) {
	node := oldNode()
	node.deaf = 2
	h := newHarness(false, map[string]*fakeNode{"n1": node})
	h.settleDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}

	h.connect("n1")

	rec, _ := h.store.Get(context.Background(), "n1")
	if rec == nil || rec.Digest != "old" {
		t.Fatalf("record = %+v, want the node's build read once it was listening", rec)
	}
}

// A node slower to subscribe than the settle window is read by the next
// sweep - even though it already has a record, which the sweep would
// otherwise take as "nothing to ask".
func TestOnConnect_NodeStillNotListeningIsReadAtTheNextSweep(t *testing.T) {
	node := oldNode()
	h := newHarness(false, map[string]*fakeNode{"n1": node})
	h.connect("n1") // an earlier connect left a record of the old build

	// It restarts on a new build and reconnects, slow to subscribe.
	h.transport.mu.Lock()
	node.digest = "new"
	node.deaf = 2
	h.transport.mu.Unlock()
	h.settleDelays = []time.Duration{time.Millisecond}
	h.connect("n1")

	if rec, _ := h.store.Get(context.Background(), "n1"); rec == nil || rec.Digest != "old" {
		t.Fatalf("record = %+v, want the old build still recorded while the node is not listening", rec)
	}

	h.Sweep(context.Background())
	h.wg.Wait()
	if rec, _ := h.store.Get(context.Background(), "n1"); rec == nil || rec.Digest != "new" {
		t.Errorf("record = %+v, want the new build read at the sweep", rec)
	}
}

// A node that really has gone is not chased: the sweep asks only nodes
// that are connected.
func TestOnConnect_NodeGoneIsNotAskedAtTheSweep(t *testing.T) {
	node := oldNode()
	node.connected = false
	h := newHarness(false, map[string]*fakeNode{"n1": node})
	h.settleDelays = []time.Duration{time.Millisecond}

	h.connect("n1")
	h.Sweep(context.Background())
	h.wg.Wait()

	if rec, _ := h.store.Get(context.Background(), "n1"); rec != nil {
		t.Errorf("record = %+v, want nothing recorded for a node that is not connected", rec)
	}
	if asked := h.transport.asked["n1"]; asked != 0 {
		t.Errorf("the node answered %d status requests, want none", asked)
	}
}

// A node that reconnected before the coordinator was listening for
// connects - every node, right after a console restart - is asked for
// its build when the coordinator starts, not left with a stale record.
func TestStart_AsksNodesThatWereAlreadyConnected(t *testing.T) {
	node := oldNode()
	h := newHarness(false, map[string]*fakeNode{"n1": node})
	h.connect("n1")

	// The console restarts; meanwhile the node moved to another build
	// and reconnected with no connect notification delivered.
	h.transport.mu.Lock()
	node.digest = "changed-while-console-was-down"
	h.transport.mu.Unlock()
	h.rebuild(false)

	h.Start(context.Background())
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if rec, _ := h.store.Get(context.Background(), "n1"); rec != nil && rec.Digest == "changed-while-console-was-down" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.Stop()

	rec, _ := h.store.Get(context.Background(), "n1")
	if rec == nil || rec.Digest != "changed-while-console-was-down" {
		t.Errorf("record = %+v, want the build the node runs now", rec)
	}
}
