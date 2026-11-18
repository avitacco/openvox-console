package nodeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// The patch requests. patch_apply starts a run and answers at once; the
// run goes on in the background with the client held busy, and the
// console follows it with patch_status, which is answered even then.
const (
	actionPatchApply  = "patch_apply"
	actionPatchStatus = "patch_status"
)

// Patch run states, as patch_status reports them.
const (
	patchIdle        = "idle"
	patchRunning     = "running"
	patchDone        = "done"
	patchFailed      = "failed"
	patchInterrupted = "interrupted"
)

// Per-package outcomes.
const (
	pkgUpgraded     = "upgraded"
	pkgWouldUpgrade = "would_upgrade"
	pkgNotUpgraded  = "not_upgraded"
	pkgNothing      = "nothing_to_apply"
)

// maxPatchOutput bounds the package manager output a run keeps: the end
// of it, which is where a failure is explained.
const maxPatchOutput = 64 << 10

// rebootGrace is how long a node waits, after its result is final,
// before rebooting: long enough for the console's next status request to
// collect that result.
const rebootGrace = 30 * time.Second

// patchApplyParams is requestData.Params' shape for actionPatchApply.
type patchApplyParams struct {
	Scope    string   `json:"scope"`
	Packages []string `json:"packages,omitempty"`
	DryRun   bool     `json:"dry_run,omitempty"`
	Reboot   bool     `json:"reboot,omitempty"`
}

// patchPackageResult is one package's outcome. From is empty when the
// package manager does not say what was installed.
type patchPackageResult struct {
	Name     string `json:"name"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Security bool   `json:"security,omitempty"`
	Outcome  string `json:"outcome"`
}

// patchRunStatus is what patch_apply (accepted) and patch_status report.
// The console holds a mirror of this shape; the JSON is the contract.
type patchRunStatus struct {
	State          string               `json:"state"`
	Phase          string               `json:"phase,omitempty"`
	Manager        string               `json:"manager,omitempty"`
	DryRun         bool                 `json:"dry_run,omitempty"`
	StartedAt      time.Time            `json:"started_at,omitempty"`
	FinishedAt     *time.Time           `json:"finished_at,omitempty"`
	Packages       []patchPackageResult `json:"packages,omitempty"`
	ExitCode       int                  `json:"exit_code"`
	Output         string               `json:"output,omitempty"`
	Error          string               `json:"error,omitempty"`
	RebootRequired bool                 `json:"reboot_required"`
	Rebooting      bool                 `json:"rebooting,omitempty"`
	// Scan is the node's pending updates once the run is over.
	Scan *patchScanResponse `json:"scan,omitempty"`
}

// PatchActions answers the patch requests - see Patcher.
type PatchActions interface {
	Apply(ctx context.Context, params json.RawMessage) []byte
	Status(ctx context.Context) []byte
}

// Patcher carries out patch runs. At most one runs at a time: the client
// is busy for the whole run, so a second request is refused before it
// reaches Apply, and Apply refuses one too.
type Patcher struct {
	runner     CommandRunner
	fileExists func(string) bool
	stateFile  string
	logger     *slog.Logger
	// grace is rebootGrace, overridable in tests.
	grace time.Duration
	// sleep waits for d or until ctx ends.
	sleep func(ctx context.Context, d time.Duration)

	mu         sync.Mutex
	status     patchRunStatus
	afterReply func(ctx context.Context)
}

// NewPatcher builds a Patcher. stateFile keeps the last run's status, so
// a run an agent restart cut short is reported as interrupted rather
// than forgotten, and a finished one can still be read after a reboot.
func NewPatcher(runner CommandRunner, fileExists func(string) bool, stateFile string, logger *slog.Logger) *Patcher {
	if logger == nil {
		logger = slog.Default()
	}
	p := &Patcher{
		runner: runner, fileExists: fileExists, stateFile: stateFile, logger: logger, grace: rebootGrace,
		sleep: func(ctx context.Context, d time.Duration) {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
			case <-t.C:
			}
		},
		status: patchRunStatus{State: patchIdle},
	}
	if data, err := os.ReadFile(stateFile); err == nil {
		var st patchRunStatus
		if json.Unmarshal(data, &st) == nil && st.State != "" {
			if st.State == patchRunning {
				st.State = patchInterrupted
				st.Error = "the node agent restarted while this patch run was in progress"
			}
			p.status = st
		}
	}
	return p
}

// PatchStateFile is where the patch run's status is kept for an agent
// configured with cfg: beside the task cache, in the agent's own
// directory.
func PatchStateFile(cfg Config) string {
	return filepath.Join(filepath.Dir(cfg.TaskCacheDir), "patch-run.json")
}

// TakeAfterReply returns, once, the work to do after the reply to the
// request just handled has been sent: the patch run itself.
func (p *Patcher) TakeAfterReply() func(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f := p.afterReply
	p.afterReply = nil
	return f
}

// Status reports the current or last run.
func (p *Patcher) Status(context.Context) []byte {
	p.mu.Lock()
	st := p.status
	p.mu.Unlock()
	return encodeResult(&responseData{PatchRun: &st})
}

// patchPackageName is what the agent accepts as a package name. It
// cannot begin with a dash, so a name is never read as an option.
var patchPackageName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+:_~-]*$`)

// Apply validates a request and accepts it: the run starts once the
// reply has been sent.
func (p *Patcher) Apply(_ context.Context, params json.RawMessage) []byte {
	var req patchApplyParams
	if err := json.Unmarshal(params, &req); err != nil {
		return errorResponse(fmt.Sprintf("invalid patch params: %v", err))
	}
	switch req.Scope {
	case "security", "all", "packages":
	default:
		return errorResponse(fmt.Sprintf("invalid patch scope %q", req.Scope))
	}
	if req.Scope == "packages" && len(req.Packages) == 0 {
		return errorResponse("a patch of named packages names none")
	}
	for _, name := range req.Packages {
		if !patchPackageName.MatchString(name) {
			return errorResponse(fmt.Sprintf("invalid package name %q", name))
		}
	}
	if req.DryRun && req.Reboot {
		return errorResponse("a dry run never reboots")
	}
	manager := p.manager()
	if manager == "" {
		return errorResponse("this node has no package manager the console can patch (apt, dnf or yum)")
	}

	p.mu.Lock()
	if p.status.State == patchRunning {
		p.mu.Unlock()
		return errorResponse("node-agent is busy executing another request")
	}
	p.status = patchRunStatus{State: patchRunning, Phase: "queued", Manager: manager, DryRun: req.DryRun, StartedAt: time.Now().UTC()}
	p.persistLocked()
	st := p.status
	p.afterReply = func(ctx context.Context) { p.run(ctx, manager, req) }
	p.mu.Unlock()
	return encodeResult(&responseData{PatchRun: &st})
}

// manager names the package manager this node patches with.
func (p *Patcher) manager() string {
	switch {
	case p.fileExists(aptGet):
		return "apt"
	case p.fileExists(dnfBin):
		return "dnf"
	case p.fileExists(yumBin):
		return "yum"
	}
	return ""
}

func (p *Patcher) update(f func(*patchRunStatus)) {
	p.mu.Lock()
	f(&p.status)
	p.persistLocked()
	p.mu.Unlock()
}

// persistLocked writes the status to the state file. A failure is only
// logged: it costs the restart-after-a-crash report, not the run.
func (p *Patcher) persistLocked() {
	if p.stateFile == "" {
		return
	}
	data, err := json.Marshal(p.status)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p.stateFile), 0o700); err != nil {
		p.logger.Warn("could not keep patch run state", "error", err)
		return
	}
	tmp := p.stateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		p.logger.Warn("could not keep patch run state", "error", err)
		return
	}
	if err := os.Rename(tmp, p.stateFile); err != nil {
		p.logger.Warn("could not keep patch run state", "error", err)
	}
}

func (p *Patcher) setPhase(phase string) { p.update(func(s *patchRunStatus) { s.Phase = phase }) }

func (p *Patcher) fail(message string) {
	now := time.Now().UTC()
	p.update(func(s *patchRunStatus) {
		s.State, s.Phase, s.Error, s.FinishedAt = patchFailed, "", message, &now
	})
}

// scan runs the manager's read-only scan.
func (p *Patcher) scan(ctx context.Context, manager string, refresh bool) (*patchScanResponse, error) {
	if manager == "apt" {
		return scanApt(ctx, p.runner, p.fileExists, refresh)
	}
	bin := dnfBin
	if manager == "yum" {
		bin = yumBin
	}
	return scanRPM(ctx, p.runner, p.fileExists, bin, manager, refresh)
}

// selectUpdates picks the pending updates a request applies and the
// named packages that had nothing pending.
func selectUpdates(pending []patchUpdate, req patchApplyParams) (selected []patchUpdate, nothing []string) {
	switch req.Scope {
	case "all":
		return pending, nil
	case "security":
		for _, u := range pending {
			if u.Security {
				selected = append(selected, u)
			}
		}
		return selected, nil
	}
	byName := map[string]patchUpdate{}
	for _, u := range pending {
		byName[u.Name] = u
	}
	asked := append([]string(nil), req.Packages...)
	sort.Strings(asked)
	for _, name := range asked {
		if u, ok := byName[name]; ok {
			selected = append(selected, u)
		} else {
			nothing = append(nothing, name)
		}
	}
	return selected, nothing
}

// installCommand is the package manager command that applies the
// selected updates, non-interactively.
func installCommand(manager string, req patchApplyParams, selected []patchUpdate) (string, []string) {
	names := make([]string, 0, len(selected))
	for _, u := range selected {
		names = append(names, u.Name)
	}
	if manager == "apt" {
		args := []string{"DEBIAN_FRONTEND=noninteractive", aptGet, "-y", "-q",
			"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"}
		if req.Scope == "all" {
			return "/usr/bin/env", append(args, "dist-upgrade")
		}
		return "/usr/bin/env", append(append(args, "install", "--only-upgrade"), names...)
	}
	bin := dnfBin
	if manager == "yum" {
		bin = yumBin
	}
	switch req.Scope {
	case "all":
		return bin, []string{"-y", "-q", "upgrade"}
	case "security":
		return bin, []string{"-y", "-q", "upgrade", "--security"}
	}
	return bin, append([]string{"-y", "-q", "upgrade"}, names...)
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "...\n" + s[len(s)-n:]
}

// run carries out one patch run. It records its progress as it goes and
// never panics the agent: whatever happens, the status ends final.
func (p *Patcher) run(ctx context.Context, manager string, req patchApplyParams) {
	p.setPhase("scanning")
	pre, err := p.scan(ctx, manager, true)
	if err != nil {
		p.fail("could not list the pending updates: " + err.Error())
		return
	}
	selected, nothing := selectUpdates(pre.Updates, req)

	results := make([]patchPackageResult, 0, len(selected)+len(nothing))
	for _, name := range nothing {
		results = append(results, patchPackageResult{Name: name, Outcome: pkgNothing})
	}
	if req.DryRun {
		for _, u := range selected {
			results = append(results, patchPackageResult{Name: u.Name, From: u.Installed, To: u.Available, Security: u.Security, Outcome: pkgWouldUpgrade})
		}
		now := time.Now().UTC()
		p.update(func(s *patchRunStatus) {
			s.State, s.Phase, s.Packages, s.FinishedAt, s.Scan, s.RebootRequired = patchDone, "", results, &now, pre, pre.RebootRequired
		})
		return
	}

	exit, output := 0, ""
	if len(selected) > 0 {
		p.setPhase("installing")
		name, args := installCommand(manager, req, selected)
		stdout, stderr, code, runErr := p.runner(ctx, name, args...)
		exit, output = code, tail(strings.TrimSpace(stdout+"\n"+stderr), maxPatchOutput)
		if runErr != nil {
			p.fail("the package manager could not run: " + runErr.Error())
			return
		}
	}

	p.setPhase("verifying")
	post, postErr := p.scan(ctx, manager, false)
	stillPending := map[string]bool{}
	if postErr == nil {
		for _, u := range post.Updates {
			stillPending[u.Name] = true
		}
	}
	failedPackages := 0
	for _, u := range selected {
		outcome := pkgUpgraded
		if (postErr == nil && stillPending[u.Name]) || (postErr != nil && exit != 0) {
			outcome = pkgNotUpgraded
			failedPackages++
		}
		results = append(results, patchPackageResult{Name: u.Name, From: u.Installed, To: u.Available, Security: u.Security, Outcome: outcome})
	}

	now := time.Now().UTC()
	reboot := post != nil && post.RebootRequired
	doReboot := req.Reboot && reboot && exit == 0 && failedPackages == 0
	p.update(func(s *patchRunStatus) {
		s.Phase, s.Packages, s.ExitCode, s.Output, s.FinishedAt = "", results, exit, output, &now
		s.Scan, s.RebootRequired, s.Rebooting = post, reboot, doReboot
		switch {
		case exit != 0:
			s.State, s.Error = patchFailed, fmt.Sprintf("the package manager exited with status %d", exit)
		case failedPackages > 0:
			s.State, s.Error = patchFailed, fmt.Sprintf("%d package(s) were not upgraded", failedPackages)
		default:
			s.State = patchDone
		}
	})
	if doReboot {
		// The result is final and persisted; give the console time to
		// collect it, then restart.
		p.sleep(ctx, p.grace)
		if ctx.Err() != nil {
			return
		}
		if _, _, code, err := p.runner(ctx, "/usr/bin/systemctl", "reboot"); err != nil || code != 0 {
			if _, _, code, err := p.runner(ctx, "/sbin/shutdown", "-r", "now"); err != nil || code != 0 {
				p.update(func(s *patchRunStatus) {
					s.Rebooting = false
					s.Error = strings.TrimSpace(s.Error + " The node could not be rebooted.")
				})
			}
		}
	}
}
