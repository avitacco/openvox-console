package agentless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
	"github.com/voxpupuli/enterprise-console/internal/taskexec"
)

// Stage says where in reaching a target something failed, so a failure
// names what to fix.
type Stage string

// Stages of an SSH session, in order.
const (
	StageConnect      Stage = "connect"
	StageHostKey      Stage = "host key"
	StageAuthenticate Stage = "authenticate"
	StageBecome       Stage = "become"
	StageTransfer     Stage = "transfer"
	StageTask         Stage = "task"
)

// StageError is a failure at one stage. Its message never contains a
// secret: see redact.
type StageError struct {
	Stage  Stage
	Reason string
	// Presented is the host key's fingerprint, for a host key failure.
	Presented string
}

func (e *StageError) Error() string {
	switch e.Stage {
	case StageConnect:
		return "could not connect: " + e.Reason
	case StageHostKey:
		return "host key: " + e.Reason
	case StageAuthenticate:
		return "authentication failed: " + e.Reason
	case StageBecome:
		return "could not run as the run-as user: " + e.Reason
	case StageTransfer:
		return "file transfer failed: " + e.Reason
	}
	return e.Reason
}

// Limits.
const (
	// ConnectTimeout bounds dialing and the SSH handshake.
	ConnectTimeout = 15 * time.Second
	// MaxConcurrent is how many SSH task sessions run across all targets.
	MaxConcurrent = 20
	// maxCachedFiles / maxCachedBytes bound the task file cache.
	maxCachedFiles = 64
	maxCachedBytes = 32 << 20
)

// Registry is what the executor needs of the store.
type Registry interface {
	Connection(ctx context.Context, name string) (Connection, error)
	ProposeKey(ctx context.Context, id int64, wire []byte) (string, error)
	FlagChangedKey(ctx context.Context, id int64, wire []byte) (string, error)
}

// FileSource downloads a task's files, verified. *taskcatalog.Client is one.
type FileSource interface {
	File(ctx context.Context, f taskcatalog.ResolvedFile) ([]byte, error)
}

// Executor runs tasks on agentless targets over SSH and checks them. It is
// the console's counterpart to a node agent for a host that has none.
type Executor struct {
	registry Registry
	files    FileSource

	connectTimeout time.Duration
	global         chan struct{}

	mu    sync.Mutex
	busy  map[string]bool
	cache map[string][]byte
	size  int
}

// NewExecutor builds an Executor.
func NewExecutor(registry Registry, files FileSource) *Executor {
	return &Executor{
		registry: registry, files: files, connectTimeout: ConnectTimeout,
		global: make(chan struct{}, MaxConcurrent),
		busy:   map[string]bool{}, cache: map[string][]byte{},
	}
}

// redact removes every secret of conn from s. Errors from the SSH library
// do not carry them, but nothing recorded about a target may, so this is
// belt and braces.
func redact(s string, conn Connection) string {
	for _, secret := range []string{conn.Password, string(conn.Passphrase)} {
		if len(secret) >= 4 {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}

// session is one authenticated SSH connection.
type session struct {
	client *ssh.Client
	conn   Connection
}

func (s *session) close() { _ = s.client.Close() }

// hostKeyProbe is what the host key callback saw.
type hostKeyProbe struct {
	wire    []byte
	changed bool
}

// connect dials and authenticates. When the target has no pinned key it
// never authenticates: the key is offered to the registry as proposed and
// the connection is abandoned, so no credential is sent to a host nobody
// has vouched for. A pinned target that presents another key is refused
// the same way and flagged.
func (e *Executor) connect(ctx context.Context, conn Connection, authenticate bool) (*session, *hostKeyProbe, error) {
	probe := &hostKeyProbe{}
	var pinned ssh.PublicKey
	var algos []string
	if len(conn.PinnedKey) > 0 {
		var err error
		if pinned, err = ssh.ParsePublicKey(conn.PinnedKey); err != nil {
			return nil, nil, &StageError{Stage: StageHostKey, Reason: "the pinned host key could not be read; replace it"}
		}
		algos = hostKeyAlgorithms(pinned)
	}
	cfg := &ssh.ClientConfig{
		User:              conn.Username,
		Timeout:           e.connectTimeout,
		HostKeyAlgorithms: algos,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			probe.wire = key.Marshal()
			if pinned == nil {
				return errors.New("host key not trusted")
			}
			if !bytes.Equal(key.Marshal(), pinned.Marshal()) {
				probe.changed = true
				return errors.New("host key changed")
			}
			return nil
		},
	}
	if authenticate {
		if len(conn.PrivateKey) > 0 {
			var signer ssh.Signer
			var err error
			if len(conn.Passphrase) > 0 {
				signer, err = ssh.ParsePrivateKeyWithPassphrase(conn.PrivateKey, conn.Passphrase)
			} else {
				signer, err = ssh.ParsePrivateKey(conn.PrivateKey)
			}
			if err != nil {
				return nil, nil, &StageError{Stage: StageAuthenticate, Reason: "the stored private key could not be read"}
			}
			cfg.Auth = append(cfg.Auth, ssh.PublicKeys(signer))
		}
		if conn.Password != "" {
			cfg.Auth = append(cfg.Auth, ssh.Password(conn.Password))
		}
	}

	addr := net.JoinHostPort(conn.Address, strconv.Itoa(conn.Port))
	dialer := net.Dialer{Timeout: e.connectTimeout}
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, &StageError{Stage: StageConnect, Reason: redact(simplify(err), conn)}
	}
	// The handshake has its own deadline; it is lifted once it is done.
	_ = raw.SetDeadline(time.Now().Add(e.connectTimeout))
	c, chans, reqs, err := ssh.NewClientConn(raw, addr, cfg)
	if err != nil {
		raw.Close()
		switch {
		case pinned == nil && probe.wire != nil:
			return nil, probe, &StageError{Stage: StageHostKey, Reason: "the host key has not been trusted yet", Presented: fingerprintOf(probe.wire)}
		case probe.changed:
			return nil, probe, &StageError{Stage: StageHostKey, Reason: "the host key is not the pinned one; the target was not logged in to", Presented: fingerprintOf(probe.wire)}
		}
		if strings.Contains(err.Error(), "unable to authenticate") || strings.Contains(err.Error(), "no supported methods") {
			return nil, probe, &StageError{Stage: StageAuthenticate, Reason: "the target refused the login"}
		}
		return nil, probe, &StageError{Stage: StageConnect, Reason: redact(simplify(err), conn)}
	}
	_ = raw.SetDeadline(time.Time{})
	return &session{client: ssh.NewClient(c, chans, reqs), conn: conn}, probe, nil
}

func simplify(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 && strings.Contains(msg, "dial tcp") {
		return msg[i+2:]
	}
	return msg
}

func fingerprintOf(wire []byte) string {
	fp, err := PublicKeyFingerprint(wire)
	if err != nil {
		return ""
	}
	return fp
}

// hostKeyAlgorithms are the signature algorithms to accept for a pinned
// key's type, so a host with several keys is asked for the one pinned.
func hostKeyAlgorithms(k ssh.PublicKey) []string {
	if k.Type() == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}
	return []string{k.Type()}
}

// result of running a command.
type cmdResult struct {
	stdout, stderr string
	exit           int
	timedOut       bool
}

// run runs command on the session, with stdin, until it ends or ctx does.
// A command that outlives ctx is sent KILL; timedOut says so.
func (s *session) run(ctx context.Context, command string, stdin []byte, stdoutLimit, stderrLimit int) (cmdResult, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return cmdResult{}, err
	}
	defer sess.Close()
	out, errb := taskexec.NewCapped(stdoutLimit), taskexec.NewCapped(stderrLimit)
	sess.Stdout, sess.Stderr = out, errb
	if stdin != nil {
		sess.Stdin = bytes.NewReader(stdin)
	}
	if err := sess.Start(command); err != nil {
		return cmdResult{}, err
	}
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case werr := <-done:
		return finish(out, errb, werr)
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		_ = sess.Close()
		<-done
		return cmdResult{stdout: out.String(), stderr: errb.String(), exit: -1, timedOut: true}, nil
	}
}

func finish(out, errb *taskexec.Capped, werr error) (cmdResult, error) {
	r := cmdResult{stdout: out.String(), stderr: errb.String()}
	if werr == nil {
		return r, nil
	}
	var exit *ssh.ExitError
	if errors.As(werr, &exit) {
		r.exit = exit.ExitStatus()
		return r, nil
	}
	var missing *ssh.ExitMissingError
	if errors.As(werr, &missing) {
		r.exit = -1
		return r, nil
	}
	return r, werr
}

// quote single-quotes s for a POSIX shell.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

var remoteDirPattern = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)

// Wire shapes: the JSON is the contract with the dispatcher, as for an
// agent. See internal/orchestrator.
type requestData struct {
	Module string          `json:"module"`
	Action string          `json:"action"`
	Params json.RawMessage `json:"params,omitempty"`
}

type outputData struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitcode"`
}

type responseData struct {
	Output outputData      `json:"output"`
	Result json.RawMessage `json:"result,omitempty"`
}

type wireResponse struct {
	Result *responseData `json:"result,omitempty"`
	Error  string        `json:"error,omitempty"`
}

func errorResponse(msg string) []byte {
	b, _ := json.Marshal(wireResponse{Error: msg})
	return b
}

// Dispatch implements the orchestrator's Transport for an agentless
// target: only a task can be run. The answer is the JSON an agent would
// give, so a target's result is recorded by the one path.
func (e *Executor) Dispatch(ctx context.Context, name string, payload []byte, timeout time.Duration) ([]byte, error) {
	var req requestData
	if err := json.Unmarshal(payload, &req); err != nil {
		return errorResponse("invalid request data"), nil
	}
	if req.Action != "run_task" {
		return errorResponse("agentless targets can only run tasks"), nil
	}
	var task taskcatalog.Resolved
	if err := json.Unmarshal(req.Params, &task); err != nil {
		return errorResponse("invalid task request"), nil
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return e.runTask(ctx, name, task), nil
}

// acquire marks a target busy and takes a place among the concurrent
// sessions, waiting for one. It returns what to call when done, or
// ok=false when the target is already running a task.
func (e *Executor) acquire(ctx context.Context, name string) (release func(), ok bool, err error) {
	e.mu.Lock()
	if e.busy[name] {
		e.mu.Unlock()
		return nil, false, nil
	}
	e.busy[name] = true
	e.mu.Unlock()
	select {
	case e.global <- struct{}{}:
	case <-ctx.Done():
		e.mu.Lock()
		delete(e.busy, name)
		e.mu.Unlock()
		return nil, true, ctx.Err()
	}
	return func() {
		<-e.global
		e.mu.Lock()
		delete(e.busy, name)
		e.mu.Unlock()
	}, true, nil
}

// fetch returns a task's files by name, verified, from the cache when the
// same content was fetched lately.
func (e *Executor) fetch(ctx context.Context, task taskcatalog.Resolved, names []string) (map[string][]byte, error) {
	byName := map[string]taskcatalog.ResolvedFile{}
	for _, f := range task.Files {
		byName[f.Filename] = f
	}
	out := map[string][]byte{}
	for _, name := range names {
		f, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("task request lists no file %s", name)
		}
		e.mu.Lock()
		data, hit := e.cache[f.SHA256]
		e.mu.Unlock()
		if !hit {
			var err error
			if data, err = e.files.File(ctx, f); err != nil {
				return nil, err
			}
			e.mu.Lock()
			if len(e.cache) >= maxCachedFiles || e.size+len(data) > maxCachedBytes {
				e.cache, e.size = map[string][]byte{}, 0
			}
			e.cache[f.SHA256] = data
			e.size += len(data)
			e.mu.Unlock()
		}
		out[name] = data
	}
	return out, nil
}

// features are what an agentless target provides: a shell, and no Puppet.
var features = []string{"shell"}

// runTask runs task on the named target and returns the wire response.
func (e *Executor) runTask(ctx context.Context, name string, task taskcatalog.Resolved) []byte {
	impl, err := taskexec.Choose(toImpls(task.Implementations), features)
	if err != nil {
		return errorResponse(err.Error())
	}
	module, _, _ := strings.Cut(task.Task, "::")
	if !taskexec.SafeRelPath(module) || strings.Contains(module, "/") || !taskexec.SafeRelPath(impl.Name) || strings.Contains(impl.Name, "/") {
		return errorResponse(fmt.Sprintf("invalid task name or file: %s/%s", module, impl.Name))
	}
	for _, f := range impl.Files {
		if !taskexec.SafeRelPath(f) {
			return errorResponse(fmt.Sprintf("invalid supporting file path %q", f))
		}
	}
	needed := append([]string{impl.Name}, impl.Files...)
	// Everything is fetched and verified before the host is touched, so a
	// file that does not match its checksum costs nothing there.
	data, err := e.fetch(ctx, task, needed)
	if err != nil {
		return errorResponse(err.Error())
	}

	release, ok, err := e.acquire(ctx, name)
	if !ok {
		return errorResponse("this target is already running a task")
	}
	if err != nil {
		return errorResponse("waiting for a free SSH session: " + err.Error())
	}
	defer release()

	conn, err := e.registry.Connection(ctx, name)
	if err != nil {
		return errorResponse(err.Error())
	}
	if len(conn.PinnedKey) == 0 {
		return errorResponse((&StageError{Stage: StageHostKey, Reason: "the host key of this target has not been trusted: check it and accept its fingerprint first"}).Error())
	}
	sess, probe, err := e.connect(ctx, conn, true)
	if err != nil {
		e.noteKey(ctx, conn, probe)
		return errorResponse(err.Error())
	}
	defer sess.close()

	// The task's own timeout covers its run; transfer and cleanup have
	// their own.
	return e.execute(ctx, sess, task, impl, module, data)
}

func (e *Executor) noteKey(ctx context.Context, conn Connection, probe *hostKeyProbe) {
	if probe == nil || probe.wire == nil || !probe.changed {
		return
	}
	_, _ = e.registry.FlagChangedKey(ctx, conn.ID, probe.wire)
}

func toImpls(in []taskcatalog.ResolvedImplementation) []taskexec.Implementation {
	out := make([]taskexec.Implementation, 0, len(in))
	for _, i := range in {
		out = append(out, taskexec.Implementation{Name: i.Name, Requirements: i.Requirements, InputMethod: i.InputMethod, Files: i.Files})
	}
	return out
}

// execute does the work on an open session: a private directory, the
// files, the run, and removing the directory in every outcome.
func (e *Executor) execute(ctx context.Context, s *session, task taskcatalog.Resolved, impl taskexec.Implementation, module string, data map[string][]byte) []byte {
	const step = 60 * time.Second
	stage := func(st Stage, format string, a ...any) []byte {
		return errorResponse((&StageError{Stage: st, Reason: redact(fmt.Sprintf(format, a...), s.conn)}).Error())
	}

	mk, err := s.runFor(ctx, step, `mktemp -d "${TMPDIR:-/tmp}/ovc-task.XXXXXXXXXX"`, nil, 4096, 4096)
	root := strings.TrimSpace(mk.stdout)
	if err != nil || mk.timedOut || mk.exit != 0 || !remoteDirPattern.MatchString(root) {
		return stage(StageTransfer, "could not create a private directory on the target (%s)", firstLine(mk.stderr, err))
	}
	// Removing the directory is attempted however the task ends, on a
	// context of its own: the task's may be the one that ran out.
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), step)
		defer cancel()
		_, _ = s.run(cctx, "rm -rf -- "+quote(root), nil, 1024, 1024)
	}()

	runAs := s.conn.RunAs
	// Another user must be able to read the task; the files are code, not
	// secrets: parameters never touch the disk.
	dirMode, fileMode := "700", "700"
	if runAs != "" && runAs != "root" {
		dirMode, fileMode = "755", "755"
	}
	if _, err := s.runFor(ctx, step, "chmod "+dirMode+" "+quote(root), nil, 1024, 1024); err != nil {
		return stage(StageTransfer, "%v", err)
	}
	place := func(name, rel string) []byte {
		dst := root + "/" + rel
		cmd := "mkdir -p -- " + quote(dst[:strings.LastIndex(dst, "/")]) + " && cat > " + quote(dst) + " && chmod " + fileMode + " " + quote(dst)
		r, err := s.runFor(ctx, step, cmd, data[name], 4096, 4096)
		if err != nil || r.timedOut || r.exit != 0 {
			return stage(StageTransfer, "could not write %s (%s)", name, firstLine(r.stderr, err))
		}
		return nil
	}
	executable := root + "/" + module + "/tasks/" + impl.Name
	if fail := place(impl.Name, module+"/tasks/"+impl.Name); fail != nil {
		return fail
	}
	for _, f := range impl.Files {
		if fail := place(f, f); fail != nil {
			return fail
		}
	}

	method := taskexec.InputMethodFor(task.InputMethod, impl)
	installDir := ""
	if len(impl.Files) > 0 {
		installDir = root
	}
	stdin, env, err := taskexec.Input(method, task.Params, task.Task, installDir)
	if err != nil {
		return stage(StageTask, "%v", err)
	}
	cmd, input := buildCommand(root, executable, runAs, stdin, env)

	timeout := time.Duration(task.TimeoutSeconds) * time.Second
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	r, err := s.run(runCtx, cmd, input, taskexec.MaxStdout, taskexec.MaxStderr)
	switch {
	case err != nil:
		return stage(StageTask, "failed to run %s: %v", impl.Name, err)
	case r.timedOut && ctx.Err() == nil:
		return errorResponse(fmt.Sprintf("task timed out after %s and was stopped", timeout))
	case r.timedOut:
		return errorResponse("the task was stopped before it finished")
	}
	if runAs != "" && becomeFailed(r) {
		return stage(StageBecome, "%s", strings.TrimSpace(firstLine(r.stderr, nil)))
	}
	b, _ := json.Marshal(wireResponse{Result: &responseData{
		Output: outputData{Stdout: redact(r.stdout, s.conn), Stderr: redact(r.stderr, s.conn), ExitCode: r.exit},
		Result: taskexec.ParseOutput(redact(r.stdout, s.conn)),
	}})
	return b
}

// buildCommand is the command line that runs the task and the bytes to
// send it on stdin. Parameters given as environment variables must not be
// on a command line, where any user of the host can read them: they are
// sent ahead of the task's own input as shell assignments, and a small
// bootstrap reads exactly that many bytes and evaluates them before it
// replaces itself with the task. With run-as, the bootstrap runs under
// sudo, so the variables survive its environment reset.
func buildCommand(root, executable, runAs string, stdin []byte, env []string) (string, []byte) {
	prefix := "cd " + quote(root) + " && "
	sudo := ""
	if runAs != "" {
		sudo = "sudo -n -u " + quote(runAs) + " -- "
	}
	if len(env) == 0 {
		// sudo runs a program, not a shell builtin: exec only without it.
		if sudo != "" {
			return prefix + sudo + quote(executable), stdin
		}
		return prefix + "exec " + quote(executable), stdin
	}
	var exports bytes.Buffer
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		exports.WriteString("export " + k + "=" + quote(v) + "\n")
	}
	n := exports.Len()
	input := append(exports.Bytes(), stdin...)
	boot := fmt.Sprintf(`eval "$(dd bs=1 count=%d 2>/dev/null)" && exec "$0"`, n)
	return prefix + sudo + "sh -c " + quote(boot) + " " + quote(executable), input
}

// becomeFailed recognises sudo refusing to switch user, which is not the
// task failing.
func becomeFailed(r cmdResult) bool {
	if r.exit == 0 {
		return false
	}
	s := r.stderr
	return strings.Contains(s, "sudo: a password is required") || strings.Contains(s, "is not in the sudoers file") ||
		strings.Contains(s, "is not allowed to run sudo") || strings.Contains(s, "sudo: no tty present") ||
		strings.Contains(s, "may not run sudo") || strings.Contains(s, "sudo: command not found")
}

func firstLine(s string, err error) string {
	if line, _, _ := strings.Cut(strings.TrimSpace(s), "\n"); line != "" {
		return line
	}
	if err != nil {
		return err.Error()
	}
	return "no detail"
}

// runFor is run with its own time limit.
func (s *session) runFor(ctx context.Context, d time.Duration, command string, stdin []byte, stdoutLimit, stderrLimit int) (cmdResult, error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return s.run(ctx, command, stdin, stdoutLimit, stderrLimit)
}

var _ io.Writer = (*taskexec.Capped)(nil)

// CheckResult is what a connectivity check found.
type CheckResult struct {
	OK     bool
	Stage  Stage
	Reason string
	// LoggedInAs is the user the target authenticated as; RunAs the one
	// tasks will run as, when set.
	LoggedInAs string
	RunAs      string
	// Presented is a host key fingerprint to accept or investigate.
	Presented string
	// NewKey is set when an untrusted key was seen and recorded.
	NewKey bool
}

// Check connects to the named target, verifies its host key, logs in and
// runs `id -un`, as the run-as user too when one is set. A target with no
// pinned key is not logged in to: its key is recorded as proposed, to be
// accepted.
func (e *Executor) Check(ctx context.Context, name string) (CheckResult, error) {
	conn, err := e.registry.Connection(ctx, name)
	if err != nil {
		return CheckResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*e.connectTimeout)
	defer cancel()
	sess, probe, err := e.connect(ctx, conn, true)
	if err != nil {
		var se *StageError
		if !errors.As(err, &se) {
			return CheckResult{}, err
		}
		res := CheckResult{Stage: se.Stage, Reason: se.Error(), Presented: se.Presented}
		if probe != nil && probe.wire != nil {
			if probe.changed {
				_, _ = e.registry.FlagChangedKey(ctx, conn.ID, probe.wire)
			} else if len(conn.PinnedKey) == 0 {
				if _, perr := e.registry.ProposeKey(ctx, conn.ID, probe.wire); perr == nil {
					res.NewKey = true
				}
			}
		}
		return res, nil
	}
	defer sess.close()
	r, err := sess.run(ctx, "id -un", nil, 1024, 1024)
	if err != nil || r.exit != 0 {
		return CheckResult{Stage: StageTask, Reason: "logged in, but could not run a command: " + firstLine(r.stderr, err)}, nil
	}
	res := CheckResult{OK: true, LoggedInAs: strings.TrimSpace(r.stdout)}
	if conn.RunAs != "" {
		r, err := sess.run(ctx, "sudo -n -u "+quote(conn.RunAs)+" -- id -un", nil, 1024, 1024)
		if err != nil || r.exit != 0 {
			reason := firstLine(r.stderr, err)
			return CheckResult{Stage: StageBecome, Reason: "could not run as " + conn.RunAs + ": " + reason, LoggedInAs: res.LoggedInAs}, nil
		}
		res.RunAs = strings.TrimSpace(r.stdout)
	}
	return res, nil
}
