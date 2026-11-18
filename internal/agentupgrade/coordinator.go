package agentupgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/nodetransport"
)

// Tuning. Constants rather than settings: nothing in the specs depends
// on the values, and nobody has yet needed to change them.
const (
	// MaxConcurrent is how many nodes may be mid-upgrade at once across
	// every console instance. It is what stops a client build that
	// cannot start from reaching the whole fleet: the nodes holding the
	// slots never report back, and the rollout stalls there.
	MaxConcurrent = 20
	// MaxAttempts is how many failures at one target build are retried
	// before the console stops and leaves it to an operator.
	MaxAttempts = 5
	// ConfirmTimeout is how long a node has, after accepting an
	// upgrade, to report the new build.
	ConfirmTimeout = 10 * time.Minute
	// SweepInterval is how often due upgrades are looked for.
	SweepInterval = time.Minute

	backoffBase = 5 * time.Minute
	backoffMax  = 6 * time.Hour
	// busyRetry is how soon to try again a node that was executing
	// something else. Not an attempt: nothing failed.
	busyRetry = time.Minute

	statusTimeout = 30 * time.Second
	// upgradeTimeout covers the download and the checks before the
	// client replies. Longer than the client's own download limit, so
	// the client's explanation arrives rather than a bare timeout.
	upgradeTimeout = 15 * time.Minute

	queueDepth = 1024
	workers    = 4
)

// Wire shapes - mirrors of internal/nodeagent's (the JSON contract is
// shared, not the Go types).
const (
	moduleName         = "agent"
	actionAgentStatus  = "agent_status"
	actionAgentUpgrade = "agent_upgrade"
)

type wireRequest struct {
	Module string          `json:"module"`
	Action string          `json:"action"`
	Params json.RawMessage `json:"params,omitempty"`
}

type wireResponse struct {
	Result *struct {
		Agent *struct {
			Digest          string `json:"digest"`
			BuildTime       string `json:"buildTime"`
			Version         string `json:"version"`
			OS              string `json:"os"`
			Arch            string `json:"arch"`
			InstallMethod   string `json:"installMethod"`
			AcceptsUpgrades bool   `json:"acceptsUpgrades"`
		} `json:"agent,omitempty"`
		Upgrade *struct {
			Started bool `json:"started"`
		} `json:"upgrade,omitempty"`
	} `json:"result,omitempty"`
	Error string `json:"error,omitempty"`
}

type upgradeParams struct {
	Digest    string          `json:"digest"`
	BuildTime string          `json:"buildTime"`
	Artifact  upgradeArtifact `json:"artifact"`
}

type upgradeArtifact struct {
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Format string `json:"format"`
}

// Transport sends a request to a node - *nodetransport.Dispatcher.
type Transport interface {
	Dispatch(ctx context.Context, certname string, payload []byte, timeout time.Duration) ([]byte, error)
}

// Errors UpgradeNow refuses with. Each says which reason applies, as
// the spec requires of the manual endpoint.
var (
	ErrNotConnected = errors.New("the node is not connected")
	ErrBusy         = errors.New("the node is busy with another request; try again when it has finished")
	ErrCurrent      = errors.New("the node's client is already current")
	ErrInProgress   = errors.New("an upgrade of this node is already in progress")
)

// CannotUpgradeError is UpgradeNow's refusal for a client the console
// has no way to upgrade.
type CannotUpgradeError struct{ Blocked string }

func (e *CannotUpgradeError) Error() string {
	switch e.Blocked {
	case BlockedDeclined:
		return "the node is configured to decline upgrades"
	case BlockedNoArtifact:
		return "this console has no client package for the node's platform"
	default:
		return "the node's client does not support upgrading itself; re-run the install script on the node"
	}
}

// Coordinator records each node's client build and upgrades clients
// older than the build this console serves.
type Coordinator struct {
	store     Store
	transport Transport
	catalog   Catalog
	// connected lists the nodes connected to this instance. Nil on an
	// instance with no node listener, which then sweeps nothing.
	connected func() []string
	baseURL   string
	auto      bool
	logger    *slog.Logger
	now       func() time.Time

	queue chan string
	wg    sync.WaitGroup
	// cancel stops the sweep started by Start.
	cancel context.CancelFunc
	// slots bounds this instance's own in-flight upgrade dispatches;
	// the cluster-wide bound is the store's.
	slots chan struct{}

	// refusalWindow is how long a manual upgrade waits for the client
	// to refuse it before reporting it as under way - see UpgradeNow.
	refusalWindow time.Duration

	// settleDelays are the waits before each further attempt to read
	// the build of a node that has just connected but does not answer
	// yet - see onConnect.
	settleDelays []time.Duration

	mu sync.Mutex
	// retryStatus holds nodes whose build could not be read on connect
	// (busy, timed out, not answering yet), to be asked again at the
	// next sweep if they are still connected then.
	retryStatus map[string]struct{}
	dropped     int
}

// defaultSettleDelays covers the gap between a node's connection being
// announced and its client subscribing for requests, which is a few
// milliseconds on a healthy node. Three tries inside five seconds; a
// node slower than that is picked up by the sweep.
var defaultSettleDelays = []time.Duration{250 * time.Millisecond, time.Second, 3 * time.Second}

// Config configures a Coordinator.
type Config struct {
	Store     Store
	Transport Transport
	Catalog   Catalog
	// Connected lists the nodes connected to this instance; nil when it
	// has no node listener.
	Connected func() []string
	// BaseURL is the console's externally reachable base URL, which
	// clients download upgrade artifacts from.
	BaseURL string
	// AutoUpgrade is the console-wide switch. Off, builds are still
	// recorded and reported; only the automatic upgrades stop.
	AutoUpgrade bool
	Logger      *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// NewCoordinator builds a Coordinator. Call Start to begin working.
func NewCoordinator(cfg Config) *Coordinator {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Coordinator{
		store:         cfg.Store,
		transport:     cfg.Transport,
		catalog:       cfg.Catalog,
		connected:     cfg.Connected,
		baseURL:       strings.TrimRight(cfg.BaseURL, "/"),
		auto:          cfg.AutoUpgrade,
		logger:        logger,
		now:           now,
		queue:         make(chan string, queueDepth),
		slots:         make(chan struct{}, MaxConcurrent),
		refusalWindow: 2 * time.Second,
		settleDelays:  defaultSettleDelays,
		retryStatus:   map[string]struct{}{},
	}
}

// AutoUpgrade reports whether automatic upgrades are on.
func (c *Coordinator) AutoUpgrade() bool { return c.auto }

// Notify records that certname connected. Safe to call from the node
// transport's connect notification: it never blocks. A notification
// shed for a full queue is picked up by the next sweep instead.
func (c *Coordinator) Notify(certname string) {
	select {
	case c.queue <- certname:
	default:
		c.mu.Lock()
		c.retryStatus[certname] = struct{}{}
		c.dropped++
		c.mu.Unlock()
	}
}

// Start launches the workers that answer connects and the sweep that
// finds due upgrades. Call Stop to shut down.
func (c *Coordinator) Start(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)
	for range workers {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			for certname := range c.queue {
				c.onConnect(ctx, certname)
			}
		}()
	}
	// The node listener accepts connections before this coordinator is
	// wired to its connect notifications, and nodes reconnect within a
	// second of a console restart - so some are already connected by
	// now and were never announced here. Ask them too. Without this a
	// node in that window keeps whatever build was recorded for it
	// before the restart, however it has changed since.
	if c.connected != nil {
		for _, certname := range c.connected() {
			c.Notify(certname)
		}
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		ticker := time.NewTicker(SweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.Sweep(ctx)
			}
		}
	}()
}

// Stop ends the sweep, closes the queue and waits for work in flight.
// Notify must not be called after it.
func (c *Coordinator) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	close(c.queue)
	c.wg.Wait()
}

// onConnect reads a newly connected node's build and, if it is behind,
// upgrades it straight away rather than waiting for the sweep.
//
// A connect is announced when the node's connection is accepted, which
// is before its client has subscribed for requests. A request sent in
// that gap finds nobody listening and comes back "not connected" about
// a node that is. So a "not connected" here is tried again a few times
// first; left alone, the build of a client that had just restarted -
// which is every client that has just been upgraded - could go unread,
// and the upgrade be counted as failed for never being confirmed.
func (c *Coordinator) onConnect(ctx context.Context, certname string) {
	recorded, notConnected := c.pollStatus(ctx, certname)
	for _, delay := range c.settleDelays {
		if !notConnected {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		recorded, notConnected = c.pollStatus(ctx, certname)
	}
	if !recorded || !c.auto {
		return
	}
	rec, err := c.store.Get(ctx, certname)
	if err != nil {
		c.logger.Warn("could not read a node's client build", "certname", certname, "error", err)
		return
	}
	c.upgradeIfDue(ctx, rec)
}

// pollStatus asks certname for its build and records the answer,
// reporting whether one was recorded and, if not, whether that was
// because the node did not appear to be connected.
func (c *Coordinator) pollStatus(ctx context.Context, certname string) (recorded, notConnected bool) {
	status, err := c.askStatus(ctx, certname)
	switch {
	case errors.Is(err, nodetransport.ErrNodeNotConnected):
		// Either gone again, in which case its next connect asks
		// again, or connected and not listening yet. The sweep tells
		// the two apart: it asks again only if the node is still
		// connected by then.
		c.mu.Lock()
		c.retryStatus[certname] = struct{}{}
		c.mu.Unlock()
		return false, true
	case errors.Is(err, errUnsupported):
		status = nil
	case err != nil:
		// Busy, timed out, or an answer that made no sense. Not
		// evidence of an old client, so nothing is recorded. Said in
		// the log, though: a node whose build silently never gets read
		// looks, from the console, exactly like one that is fine.
		if !errors.Is(err, errClientBusy) {
			c.logger.Warn("could not read a node's client build; will ask again at the next sweep", "certname", certname, "error", err)
		}
		c.mu.Lock()
		c.retryStatus[certname] = struct{}{}
		c.mu.Unlock()
		return false, false
	}
	c.forgetRetry(certname)
	if err := c.store.RecordStatus(ctx, certname, status); err != nil {
		c.logger.Warn("could not record a node's client build", "certname", certname, "error", err)
		return false, false
	}
	return true, false
}

func (c *Coordinator) forgetRetry(certname string) {
	c.mu.Lock()
	delete(c.retryStatus, certname)
	c.mu.Unlock()
}

var (
	// errUnsupported is a client that does not know the request: one
	// from before build reporting existed.
	errUnsupported = errors.New("the client does not support this request")
	errClientBusy  = errors.New("the client is busy")
)

// request sends one action and decodes the reply, turning the two
// replies that are not failures - "I don't know that action" and "I'm
// busy" - into their own errors.
func (c *Coordinator) request(ctx context.Context, certname, action string, params any, timeout time.Duration) (*wireResponse, error) {
	var raw json.RawMessage
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		raw = encoded
	}
	payload, err := json.Marshal(wireRequest{Module: moduleName, Action: action, Params: raw})
	if err != nil {
		return nil, err
	}
	reply, err := c.transport.Dispatch(ctx, certname, payload, timeout)
	if err != nil {
		return nil, err
	}
	var resp wireResponse
	if err := json.Unmarshal(reply, &resp); err != nil {
		return nil, fmt.Errorf("unreadable reply from the client: %w", err)
	}
	switch {
	case strings.HasPrefix(resp.Error, "unsupported action"):
		return nil, errUnsupported
	case strings.HasPrefix(resp.Error, "node-agent is busy"):
		return nil, errClientBusy
	case resp.Error != "":
		return nil, &clientError{resp.Error}
	}
	return &resp, nil
}

// clientError is a failure the client itself reported, in its words.
type clientError struct{ message string }

func (e *clientError) Error() string { return e.message }

func (c *Coordinator) askStatus(ctx context.Context, certname string) (*Status, error) {
	resp, err := c.request(ctx, certname, actionAgentStatus, nil, statusTimeout)
	if err != nil {
		return nil, err
	}
	if resp.Result == nil || resp.Result.Agent == nil {
		return nil, errors.New("the client's reply carried no build")
	}
	a := resp.Result.Agent
	status := &Status{
		Digest: a.Digest, Version: a.Version, OS: a.OS, Arch: a.Arch,
		InstallMethod: a.InstallMethod, AcceptsUpgrades: a.AcceptsUpgrades,
	}
	if a.BuildTime != "" {
		if t, err := time.Parse(time.RFC3339, a.BuildTime); err == nil {
			status.BuildTime = &t
		}
	}
	return status, nil
}

// Sweep times out upgrades that were never confirmed, re-asks nodes
// whose build could not be read, and upgrades connected nodes that are
// due. Called every SweepInterval by Start; exported for tests.
func (c *Coordinator) Sweep(ctx context.Context) {
	records, err := c.store.List(ctx)
	if err != nil {
		c.logger.Warn("could not list client builds; skipping this upgrade sweep", "error", err)
		return
	}
	now := c.now()
	byCertname := make(map[string]*Record, len(records))
	for i := range records {
		rec := &records[i]
		byCertname[rec.Certname] = rec

		// Any instance may time out any node's upgrade: the instance
		// that asked may be gone, and the node may have reconnected
		// somewhere else or nowhere.
		if rec.UpgradeState == UpgradeRequested && rec.RequestedAt != nil && now.Sub(*rec.RequestedAt) >= ConfirmTimeout {
			c.fail(ctx, rec.Certname, rec.Attempts, fmt.Sprintf(
				"the node did not report the new build within %s of being told to upgrade", ConfirmTimeout))
			rec.UpgradeState = UpgradeFailed
		}
	}

	if c.connected == nil {
		return
	}
	connected := c.connected()

	c.mu.Lock()
	retry := c.retryStatus
	c.retryStatus = map[string]struct{}{}
	c.mu.Unlock()
	for _, certname := range connected {
		// Also covers a node that connected before this instance could
		// record anything about it.
		_, wanted := retry[certname]
		if !wanted && byCertname[certname] != nil {
			continue
		}
		if recorded, _ := c.pollStatus(ctx, certname); recorded {
			if rec, err := c.store.Get(ctx, certname); err == nil && rec != nil {
				byCertname[certname] = rec
			}
		}
	}

	if !c.auto {
		return
	}
	for _, certname := range connected {
		if rec := byCertname[certname]; rec != nil {
			c.upgradeIfDue(ctx, rec)
		}
	}
}

// upgradeIfDue starts an upgrade of rec's node if it is out of date,
// can be upgraded, and no earlier failure says to wait.
func (c *Coordinator) upgradeIfDue(ctx context.Context, rec *Record) {
	if rec == nil || rec.UpgradeState == UpgradeRequested {
		return
	}
	a := Assess(rec, c.catalog)
	if !a.CanUpgrade {
		return
	}
	// Cheap checks the claim would make anyway, to avoid a transaction
	// per node per sweep for nodes that are simply waiting.
	if rec.TargetDigest == a.Target.Digest {
		if rec.UpgradeState == UpgradeExhausted {
			return
		}
		if rec.NextAttemptAt != nil && rec.NextAttemptAt.After(c.now()) {
			return
		}
	}

	select {
	case c.slots <- struct{}{}:
	default:
		return // this instance is at its limit; the next sweep retries
	}
	won, attempts, err := c.store.Claim(ctx, rec.Certname, a.Target.Digest, MaxConcurrent, false, c.now())
	if err != nil || !won {
		<-c.slots
		if err != nil {
			c.logger.Warn("could not claim a node's client upgrade", "certname", rec.Certname, "error", err)
		}
		return
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer func() { <-c.slots }()
		c.instruct(ctx, rec.Certname, a, attempts)
	}()
}

// instruct tells a claimed node to upgrade and records what came back.
// An accepted upgrade stays in the requested state: only the node
// reporting the new build completes it.
//
// It returns an error only when nothing was attempted and the claim was
// released - the client was busy, or the node had gone - so a caller
// with someone to tell can tell them. Every other outcome is recorded
// against the node and returns nil.
func (c *Coordinator) instruct(ctx context.Context, certname string, a Assessment, attempts int) error {
	params := upgradeParams{
		Digest:    a.Target.Digest,
		BuildTime: a.Target.BuildTime.UTC().Format(time.RFC3339),
		Artifact: upgradeArtifact{
			URL:    c.baseURL + a.Artifact.Path,
			Size:   a.Artifact.Size,
			SHA256: a.Artifact.SHA256,
			Format: a.Artifact.Format,
		},
	}
	resp, err := c.request(ctx, certname, actionAgentUpgrade, params, upgradeTimeout)

	var reported *clientError
	switch {
	case err == nil && resp.Result != nil && resp.Result.Upgrade != nil && resp.Result.Upgrade.Started:
		c.logger.Info("node-agent-client upgrade started", "certname", certname,
			"targetVersion", a.Target.Version, "targetDigest", a.Target.Digest)
	case err == nil:
		c.fail(ctx, certname, attempts, "the client's reply did not say the upgrade had started")
	case errors.Is(err, errClientBusy), errors.Is(err, nodetransport.ErrNodeNotConnected):
		// Nothing was attempted. The request in progress on the node
		// carries on undisturbed.
		if releaseErr := c.store.Release(context.WithoutCancel(ctx), certname, c.now().Add(busyRetry)); releaseErr != nil {
			c.logger.Warn("could not release a node's client upgrade", "certname", certname, "error", releaseErr)
		}
		return err
	case errors.As(err, &reported):
		c.fail(ctx, certname, attempts, reported.message)
	case errors.Is(err, errUnsupported):
		c.fail(ctx, certname, attempts, "the client does not support upgrading itself")
	case ctx.Err() != nil:
		// This console is shutting down mid-request. The node may well
		// be upgrading; leave the claim for the confirmation timeout.
	default:
		// No answer in time. The client may still be working, so this
		// too is left to the confirmation timeout rather than counted
		// twice.
		c.logger.Warn("no reply to a client upgrade request; waiting for the node to report its build",
			"certname", certname, "error", err)
	}
	return nil
}

// fail records a failed attempt, deciding from the attempts already
// made whether to retry later or stop.
func (c *Coordinator) fail(ctx context.Context, certname string, attemptsBefore int, reason string) {
	exhausted := attemptsBefore+1 >= MaxAttempts
	retryAt := c.now().Add(Backoff(attemptsBefore))
	// Recorded even if this console is shutting down: the alternative
	// is a failure that is forgotten and repeated.
	if err := c.store.Fail(context.WithoutCancel(ctx), certname, reason, exhausted, retryAt); err != nil {
		c.logger.Warn("could not record a failed client upgrade", "certname", certname, "error", err)
		return
	}
	c.logger.Warn("node-agent-client upgrade failed", "certname", certname,
		"attempt", attemptsBefore+1, "exhausted", exhausted, "reason", reason)
}

// Backoff is the wait before the next attempt after attemptsBefore
// earlier failures: five minutes, doubling, capped at six hours.
func Backoff(attemptsBefore int) time.Duration {
	d := backoffBase
	for range attemptsBefore {
		d *= 2
		if d >= backoffMax {
			return backoffMax
		}
	}
	return d
}

// UpgradeNow starts an upgrade of one node on an operator's request,
// whatever the automatic-upgrade setting and however many automatic
// attempts have failed. The upgrade itself continues in the background
// and its outcome appears in the node's status.
//
// The node is asked for its build first, which is how "not connected"
// is discovered. "Busy" is not: a client answers that question whatever
// it is doing, and refuses only the upgrade. So UpgradeNow waits a
// moment after sending the upgrade for that refusal, which comes back
// in milliseconds, and reports it - rather than telling the operator
// the node has been asked to upgrade when it has already said no.
func (c *Coordinator) UpgradeNow(ctx context.Context, certname string) error {
	status, err := c.askStatus(ctx, certname)
	switch {
	case errors.Is(err, nodetransport.ErrNodeNotConnected):
		return ErrNotConnected
	case errors.Is(err, errClientBusy):
		return ErrBusy
	case errors.Is(err, errUnsupported):
		status = nil
	case err != nil:
		return fmt.Errorf("could not read the node's client build: %w", err)
	}
	if err := c.store.RecordStatus(ctx, certname, status); err != nil {
		return err
	}
	rec, err := c.store.Get(ctx, certname)
	if err != nil {
		return err
	}

	a := Assess(rec, c.catalog)
	switch {
	case !a.OutOfDate:
		return ErrCurrent
	case !a.CanUpgrade:
		return &CannotUpgradeError{Blocked: a.Blocked}
	}

	won, attempts, err := c.store.Claim(ctx, certname, a.Target.Digest, MaxConcurrent, true, c.now())
	if err != nil {
		return err
	}
	if !won {
		return ErrInProgress
	}
	refused := make(chan error, 1)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		// Detached from the HTTP request that asked: the upgrade
		// outlives it.
		refused <- c.instruct(context.WithoutCancel(ctx), certname, a, attempts)
	}()

	select {
	case err := <-refused:
		switch {
		case errors.Is(err, errClientBusy):
			return ErrBusy
		case errors.Is(err, nodetransport.ErrNodeNotConnected):
			return ErrNotConnected
		}
		// Started, or failed for a reason now recorded on the node.
		return nil
	case <-time.After(c.refusalWindow):
		// No refusal: the client is downloading and checking the
		// artifact, which takes as long as it takes.
		return nil
	case <-ctx.Done():
		return nil
	}
}
