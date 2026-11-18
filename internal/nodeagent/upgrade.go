package nodeagent

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Actions added by add-agent-auto-upgrade. A client from before that
// change answers both with `unsupported action`, which is how the
// console recognizes a client it cannot upgrade.
const (
	actionAgentStatus  = "agent_status"
	actionAgentUpgrade = "agent_upgrade"
)

// How this client was installed, which decides how it is upgraded.
const (
	installMethodDeb     = "deb"
	installMethodRPM     = "rpm"
	installMethodBinary  = "binary"
	installMethodUnknown = "unknown"
)

const (
	// maxArtifactSize refuses an upgrade request declaring an absurd
	// size before anything is downloaded.
	maxArtifactSize = 512 << 20
	// downloadTimeout bounds one artifact download.
	downloadTimeout = 10 * time.Minute
	// validateTimeout bounds running the candidate with --build-info.
	validateTimeout = 30 * time.Second
	// installGrace is how long the client refuses other work after
	// handing a package to the installer, waiting to be restarted by
	// it. Long enough for a slow package manager; if it passes, the
	// install did not happen and the client carries on as it was.
	installGrace = 3 * time.Minute
	// installDelay gives the "started" reply time to leave before the
	// package's own scripts restart this service.
	installDelay = "2"

	upgradeUnit = "node-agent-client-upgrade"
)

// agentStatus is the reply to an agent_status request - mirrored by
// internal/agentupgrade on the console.
type agentStatus struct {
	Digest          string `json:"digest"`
	BuildTime       string `json:"buildTime"`
	Version         string `json:"version"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	InstallMethod   string `json:"installMethod"`
	AcceptsUpgrades bool   `json:"acceptsUpgrades"`
}

// upgradeParams is an agent_upgrade request. Digest and BuildTime name
// the client build to end up running; Artifact is what to download to
// get it, which on Linux is a package containing that client.
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

// upgradeResponse is the reply to an agent_upgrade request that was
// accepted. A refusal or failure is a wireResponse error instead.
type upgradeResponse struct {
	Started bool `json:"started"`
}

// Upgrader answers agent_status and performs agent_upgrade. Everything
// it touches outside the process is a field, so tests drive it without
// a package manager, a service manager or a network.
type Upgrader struct {
	cfg    Config
	goos   string
	goarch string
	exe    string
	info   BuildInfo
	logger *slog.Logger

	runner CommandRunner
	// download opens the artifact at url.
	download func(ctx context.Context, url string) (io.ReadCloser, error)
	// lookPath finds an executable, as exec.LookPath does.
	lookPath func(string) (string, error)
	// isDir reports whether path is a directory.
	isDir func(string) bool
	// detached starts a command that outlives this process.
	detached func(name string, args ...string) error
	// exit ends the process.
	exit func(code int)
	pid  int

	methodOnce sync.Once
	method     string

	mu         sync.Mutex
	afterReply func(ctx context.Context)
}

// NewUpgrader builds the Upgrader for this process: it identifies the
// running executable and hashes it once.
func NewUpgrader(cfg Config, runner CommandRunner, logger *slog.Logger) (*Upgrader, error) {
	if logger == nil {
		logger = slog.Default()
	}
	exe, err := executablePath()
	if err != nil {
		return nil, fmt.Errorf("locate this executable: %w", err)
	}
	info, err := CurrentBuildInfo()
	if err != nil {
		return nil, err
	}
	return &Upgrader{
		cfg:      cfg,
		goos:     runtime.GOOS,
		goarch:   runtime.GOARCH,
		exe:      exe,
		info:     info,
		logger:   logger,
		runner:   runner,
		download: httpDownload,
		lookPath: exec.LookPath,
		isDir: func(p string) bool {
			st, err := os.Stat(p)
			return err == nil && st.IsDir()
		},
		detached: startDetached,
		exit:     os.Exit,
		pid:      os.Getpid(),
	}, nil
}

// httpDownload fetches url. The connection is deliberately not
// verified: the console's package routes are public, and nothing
// fetched here is trusted until it matches the digest that arrived over
// the mutually authenticated node transport. Verifying TLS would add a
// way for an upgrade to fail (a console certificate the node's trust
// store does not hold) without adding any protection.
func httpDownload(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // integrity comes from the digest, see above
		Proxy:           http.ProxyFromEnvironment,
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("the console answered %s", resp.Status)
	}
	return resp.Body, nil
}

// CleanupAfterUpgrade removes what an upgrade leaves behind: the staged
// artifact, and on Windows the previous executable, which could not be
// deleted while it was still running. Called once at start.
func (u *Upgrader) CleanupAfterUpgrade() {
	_ = os.RemoveAll(u.cfg.UpgradeDir)
	if u.goos == "windows" {
		_ = os.Remove(u.exe + ".old")
	}
}

// TakeAfterReply returns, once, what has to happen after the reply to
// the request just handled has been sent - restarting into a new build,
// or waiting for the installer to do it. Nil when there is nothing.
func (u *Upgrader) TakeAfterReply() func(ctx context.Context) {
	u.mu.Lock()
	defer u.mu.Unlock()
	f := u.afterReply
	u.afterReply = nil
	return f
}

func (u *Upgrader) setAfterReply(f func(ctx context.Context)) {
	u.mu.Lock()
	u.afterReply = f
	u.mu.Unlock()
}

// installMethod reports how this client was installed, asking the
// package managers which of them owns the running executable. Decided
// once: it cannot change while the process runs.
func (u *Upgrader) installMethod(ctx context.Context) string {
	u.methodOnce.Do(func() {
		u.method = u.detectInstallMethod(ctx)
	})
	return u.method
}

func (u *Upgrader) detectInstallMethod(ctx context.Context) string {
	if u.goos != "linux" {
		// Windows and macOS are only ever installed as a bare binary.
		return installMethodBinary
	}
	owns := func(name string, args ...string) bool {
		if _, err := u.lookPath(name); err != nil {
			return false
		}
		stdout, _, code, err := u.runner(ctx, name, args...)
		return err == nil && code == 0 && strings.Contains(stdout, "node-agent-client")
	}
	if owns("dpkg-query", "-S", u.exe) {
		return installMethodDeb
	}
	if owns("rpm", "-qf", u.exe) {
		return installMethodRPM
	}
	// A Linux binary no package owns: a pre-packaging, script-managed
	// install, which the install script migrates to a package.
	return installMethodUnknown
}

// Status answers an agent_status request. It changes nothing.
func (u *Upgrader) Status(ctx context.Context) []byte {
	return encodeResult(&responseData{Agent: &agentStatus{
		Digest:          u.info.Digest,
		BuildTime:       u.info.BuildTime,
		Version:         u.info.Version,
		OS:              u.goos,
		Arch:            u.goarch,
		InstallMethod:   u.installMethod(ctx),
		AcceptsUpgrades: u.cfg.AcceptUpgrades,
	}})
}

// Upgrade answers an agent_upgrade request: it downloads the artifact,
// proves it is the requested build, and hands it to the installer. Any
// failure before the installer takes over leaves this client exactly as
// it was.
func (u *Upgrader) Upgrade(ctx context.Context, raw json.RawMessage) []byte {
	if !u.cfg.AcceptUpgrades {
		return errorResponse("upgrades are declined on this node (NODE_AGENT_ACCEPT_UPGRADES is off)")
	}
	var p upgradeParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return errorResponse(fmt.Sprintf("invalid upgrade request: %v", err))
	}
	if err := validateUpgradeParams(p); err != nil {
		return errorResponse(fmt.Sprintf("invalid upgrade request: %v", err))
	}
	if p.Digest == u.info.Digest {
		return errorResponse("this node already runs the requested client build")
	}

	method := u.installMethod(ctx)
	if method == installMethodUnknown {
		return errorResponse("this client was not installed from a package and cannot upgrade itself: re-run the install script on this node")
	}
	if p.Artifact.Format != method {
		return errorResponse(fmt.Sprintf("the upgrade artifact is a %s but this client was installed as a %s", p.Artifact.Format, method))
	}

	if err := makePrivateDir(u.cfg.UpgradeDir); err != nil {
		return errorResponse(fmt.Sprintf("could not prepare the upgrade directory: %v", err))
	}
	// Removed on every path that does not hand the directory to the
	// package installer, which removes it itself when it finishes.
	keepDir := false
	defer func() {
		if !keepDir {
			_ = os.RemoveAll(u.cfg.UpgradeDir)
		}
	}()

	artifact := filepath.Join(u.cfg.UpgradeDir, "artifact")
	if err := u.fetchVerified(ctx, p.Artifact, artifact); err != nil {
		return errorResponse(err.Error())
	}

	if method == installMethodBinary {
		after, err := u.installBinary(ctx, p, artifact)
		if err != nil {
			return errorResponse(err.Error())
		}
		u.setAfterReply(after)
		return encodeResult(&responseData{Upgrade: &upgradeResponse{Started: true}})
	}

	candidate := filepath.Join(u.cfg.UpgradeDir, "node-agent-client")
	if err := extractPackagedBinary(method, artifact, candidate); err != nil {
		return errorResponse(fmt.Sprintf("the upgrade artifact could not be unpacked: %v", err))
	}
	if err := u.validateCandidate(ctx, candidate, p); err != nil {
		return errorResponse(err.Error())
	}
	if err := u.launchPackageInstall(ctx, method, artifact); err != nil {
		return errorResponse(fmt.Sprintf("could not start the package installation: %v", err))
	}
	keepDir = true
	u.logger.Info("client upgrade handed to the package manager", "format", method, "targetDigest", p.Digest)
	u.setAfterReply(u.awaitRestart)
	return encodeResult(&responseData{Upgrade: &upgradeResponse{Started: true}})
}

func validateUpgradeParams(p upgradeParams) error {
	isDigest := func(s string) bool {
		if len(s) != sha256.Size*2 {
			return false
		}
		_, err := hex.DecodeString(s)
		return err == nil
	}
	switch {
	case !isDigest(p.Digest):
		return errors.New("digest is not a SHA-256")
	case !isDigest(p.Artifact.SHA256):
		return errors.New("artifact digest is not a SHA-256")
	case p.Artifact.Size <= 0 || p.Artifact.Size > maxArtifactSize:
		return fmt.Errorf("artifact size %d is out of range", p.Artifact.Size)
	}
	parsed, err := url.Parse(p.Artifact.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("artifact URL is not an http(s) URL")
	}
	return nil
}

// fetchVerified downloads the artifact to dst and checks its size and
// digest. It never reads more than the declared size, so a wrong or
// hostile server cannot fill the disk.
func (u *Upgrader) fetchVerified(ctx context.Context, a upgradeArtifact, dst string) error {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	body, err := u.download(ctx, a.URL)
	if err != nil {
		return fmt.Errorf("could not download the upgrade artifact from %s: %v", a.URL, err)
	}
	defer body.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("could not store the upgrade artifact: %v", err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(body, a.Size+1))
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("could not download the upgrade artifact from %s: %v", a.URL, err)
	}
	if n != a.Size {
		if n > a.Size {
			return fmt.Errorf("the downloaded artifact does not match the request: it is larger than the expected %d bytes", a.Size)
		}
		return fmt.Errorf("the downloaded artifact does not match the request: %d bytes, expected %d", n, a.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
		return fmt.Errorf("the downloaded artifact does not match the request: digest %s, expected %s", got, a.SHA256)
	}
	return nil
}

// validateCandidate runs the client about to be installed and requires
// it to identify itself as the requested build. This is what stops an
// artifact for the wrong platform, or one that cannot start, from
// replacing a working client - there is no automatic rollback after.
func (u *Upgrader) validateCandidate(ctx context.Context, candidate string, p upgradeParams) error {
	ctx, cancel := context.WithTimeout(ctx, validateTimeout)
	defer cancel()

	stdout, stderr, code, err := u.runner(ctx, candidate, "--build-info")
	if err != nil {
		return fmt.Errorf("the new client does not run on this node: %v", err)
	}
	if code != 0 {
		return fmt.Errorf("the new client does not run on this node: exit code %d: %s", code, strings.TrimSpace(stderr))
	}
	var info BuildInfo
	if err := json.Unmarshal([]byte(stdout), &info); err != nil {
		return errors.New("the new client did not report its build")
	}
	if info.Digest != p.Digest || info.BuildTime != p.BuildTime {
		return fmt.Errorf("the new client is not the requested build: it reports %s built %q, expected %s built %q",
			info.Digest, info.BuildTime, p.Digest, p.BuildTime)
	}
	return nil
}

// packageInstallScript runs the package manager, removes the staged
// files whatever the outcome, and - only when no service manager will
// restart the client - stops this process so its supervisor starts the
// new one. $1 is the package, $2 the upgrade directory, $3 the client's
// pid or empty.
func packageInstallScript(method string) string {
	install := `dpkg -i "$1"`
	if method == installMethodRPM {
		// --oldpackage and --replacepkgs make the install succeed
		// whatever is there now: the console decides the target build,
		// not rpm's idea of newer.
		install = `rpm -U --oldpackage --replacepkgs "$1"`
	}
	return `sleep ` + installDelay + `; ` + install + `; rc=$?; rm -rf "$2"; ` +
		`if [ -n "$3" ] && [ "$rc" -eq 0 ]; then kill "$3"; fi; exit $rc`
}

// launchPackageInstall starts the package installation somewhere it
// will survive this service being restarted. The package's own scripts
// restart the service, and systemd stops everything in the service's
// control group when it does - including a package manager the client
// had started itself. systemd-run puts the installation in a transient
// unit of its own. Without systemd, a new session is the best
// available, and the script stops the client afterwards.
func (u *Upgrader) launchPackageInstall(ctx context.Context, method, artifact string) error {
	script := packageInstallScript(method)
	if _, err := u.lookPath("systemd-run"); err == nil && u.isDir("/run/systemd/system") {
		_, stderr, code, err := u.runner(ctx, "systemd-run",
			"--unit="+upgradeUnit, "--collect", "--quiet",
			"--description=Upgrade node-agent-client",
			"--setenv=DEBIAN_FRONTEND=noninteractive",
			"/bin/sh", "-c", script, "sh", artifact, u.cfg.UpgradeDir, "")
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("systemd-run exited %d: %s", code, strings.TrimSpace(stderr))
		}
		return nil
	}
	return u.detached("/bin/sh", "-c", script, "sh", artifact, u.cfg.UpgradeDir, strconv.Itoa(u.pid))
}

// awaitRestart holds the client busy while the package manager works,
// so no run or task is started only to be killed by the restart. If the
// restart never comes the installation failed, and the client resumes;
// the console sees the old build and records the failure.
func (u *Upgrader) awaitRestart(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-time.After(installGrace):
		u.logger.Warn("the package installation did not restart this client; continuing on the existing build",
			"unit", upgradeUnit)
	}
}

// installBinary replaces the running executable with the verified
// artifact, for the platforms installed as a bare binary. The new file
// is written next to the installed one - a directory only an
// administrator can write - and checked again as it is written, so what
// is installed is exactly what was verified.
func (u *Upgrader) installBinary(ctx context.Context, p upgradeParams, artifact string) (func(context.Context), error) {
	candidate := u.exe + ".new"
	if u.goos == "windows" {
		// Kept executable by name: Windows decides by extension.
		candidate = strings.TrimSuffix(u.exe, ".exe") + ".new.exe"
	}
	_ = os.Remove(candidate)
	if err := copyVerified(artifact, candidate, p.Artifact.SHA256); err != nil {
		_ = os.Remove(candidate)
		return nil, fmt.Errorf("could not stage the new client: %v", err)
	}
	if err := u.validateCandidate(ctx, candidate, p); err != nil {
		_ = os.Remove(candidate)
		return nil, err
	}
	if err := replaceExecutable(u.goos, u.exe, candidate); err != nil {
		_ = os.Remove(candidate)
		return nil, fmt.Errorf("could not replace the installed client: %v", err)
	}
	u.logger.Info("client replaced; restarting into the new build", "targetDigest", p.Digest)

	// The service manager starts the new binary once this process is
	// gone. launchd restarts on any exit; the Windows Service Control
	// Manager only runs its recovery action for an exit it was not
	// told about, so that one has to look like a failure.
	code := 0
	if u.goos == "windows" {
		code = 1
	}
	return func(context.Context) { u.exit(code) }, nil
}

// copyVerified copies src to a new executable dst and fails unless the
// bytes written have the expected digest.
func copyVerified(src, dst, wantSHA256 string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != wantSHA256 {
		return errors.New("the staged file changed after it was verified")
	}
	return nil
}

// replaceExecutable puts candidate in exe's place.
//
// Everywhere but Windows a rename over the running executable is atomic
// and safe: the running process keeps the file it opened. The mode is
// set to what an installed client has first.
//
// Windows will not replace a running executable but will rename it, so
// the current one is moved aside to <exe>.old (deleted at the next
// start) and put back if the new one cannot be moved into place.
func replaceExecutable(goos, exe, candidate string) error {
	if goos != "windows" {
		if err := os.Chmod(candidate, 0o755); err != nil { //nolint:gosec // an installed executable
			return err
		}
		return os.Rename(candidate, exe)
	}
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(candidate, exe); err != nil {
		if restoreErr := os.Rename(old, exe); restoreErr != nil {
			return fmt.Errorf("%v (and the previous client could not be put back: %v)", err, restoreErr)
		}
		return err
	}
	return nil
}
