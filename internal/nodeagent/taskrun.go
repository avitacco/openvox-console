package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/taskexec"
)

// taskRequest is a run_task request's params: a task the console resolved
// and validated - mirrors internal/taskcatalog's Resolved (the wire
// contract is shared, not the Go type).
type taskRequest struct {
	Task            string               `json:"task"`
	Environment     string               `json:"environment"`
	InputMethod     string               `json:"input_method,omitempty"`
	Implementations []taskImplementation `json:"implementations"`
	Files           []taskFile           `json:"files"`
	Params          map[string]any       `json:"params"`
	TimeoutSeconds  int                  `json:"timeout_seconds"`
}

// taskImplementation is a task's implementation; shared with the
// agentless executor (see internal/taskexec).
type taskImplementation = taskexec.Implementation

// Output caps. A reply travels as one NATS message (1 MB by default) and
// carries stdout twice - raw, and as the result - so each stream is
// bounded well inside that.
const (
	maxTaskStdout = taskexec.MaxStdout
	maxTaskStderr = taskexec.MaxStderr
)

// featuresFor is what a node provides for implementation requirements
// (design decision 5): every node runs the agent, and each has the shell
// native to its OS.
func featuresFor(goos string) []string {
	if goos == "windows" {
		return []string{"powershell", "puppet-agent"}
	}
	return []string{"shell", "puppet-agent"}
}

// chooseImplementation returns the first implementation whose
// requirements the node meets.
func chooseImplementation(impls []taskImplementation, goos string) (taskImplementation, error) {
	return taskexec.Choose(impls, featuresFor(goos))
}

// agentPaths locates the executables a task may be run through.
type agentPaths struct {
	puppet     string // the puppet binary, for .pp tasks
	ruby       string // the agent's bundled Ruby, for .rb tasks
	powershell string
	cmd        string
}

// defaultAgentPaths are the standard install locations on goos.
func defaultAgentPaths(puppetBin, goos string, getenv func(string) string) agentPaths {
	if goos == "windows" {
		programFiles := getenv("ProgramFiles")
		if programFiles == "" {
			programFiles = `C:\Program Files`
		}
		systemRoot := getenv("SystemRoot")
		if systemRoot == "" {
			systemRoot = `C:\Windows`
		}
		return agentPaths{
			puppet:     puppetBin,
			ruby:       programFiles + `\Puppet Labs\Puppet\puppet\bin\ruby.exe`,
			powershell: systemRoot + `\System32\WindowsPowerShell\v1.0\powershell.exe`,
			cmd:        systemRoot + `\System32\cmd.exe`,
		}
	}
	return agentPaths{puppet: puppetBin, ruby: "/opt/puppetlabs/puppet/bin/ruby"}
}

// taskCommand is one process to start for a task.
type taskCommand struct {
	Name    string
	Args    []string
	Dir     string
	Env     []string // added to the agent's own environment
	Stdin   []byte
	Timeout time.Duration
}

// commandFor is how executable is run on goos (design decision 5).
// wrapper, when set, is a PowerShell script to run instead of the task
// itself - the powershell input method's parameter splatting.
func commandFor(executable, wrapper, goos string, paths agentPaths) (string, []string, error) {
	ext := strings.ToLower(filepath.Ext(executable))
	switch {
	case ext == ".rb":
		return paths.ruby, []string{executable}, nil
	case ext == ".pp":
		return paths.puppet, []string{"apply", executable}, nil
	case goos == "windows" && ext == ".ps1":
		script := executable
		if wrapper != "" {
			script = wrapper
		}
		return paths.powershell, []string{"-NoProfile", "-NonInteractive", "-NoLogo", "-ExecutionPolicy", "Bypass", "-File", script}, nil
	case goos == "windows" && (ext == ".bat" || ext == ".cmd"):
		return paths.cmd, []string{"/c", executable}, nil
	case goos == "windows" && ext == ".exe":
		return executable, nil, nil
	case goos == "windows":
		return "", nil, fmt.Errorf("cannot run %s on Windows: unsupported file type %q", filepath.Base(executable), ext)
	default:
		// Its shebang decides.
		return executable, nil, nil
	}
}

// effectiveInputMethod is the input method an implementation runs with:
// its own, else the task's, else the default - powershell for a
// PowerShell script, as Bolt does, and both for anything else.
func effectiveInputMethod(req taskRequest, impl taskImplementation) string {
	return taskexec.InputMethodFor(req.InputMethod, impl)
}

// taskInput builds what a task is given: its parameters (plus _task, and
// _installdir when it has supporting files) on stdin as JSON and/or as
// PT_ environment variables, per method. The powershell method sends
// JSON on stdin too - the wrapper turns it into named parameters.
func taskInput(method string, params map[string]any, taskName, installDir string) (stdin []byte, env []string, err error) {
	return taskexec.Input(method, params, taskName, installDir)
}

// powershellWrapper returns a script that reads the task's JSON input and
// calls script with each parameter it declares as a named parameter. The
// metaparameters (_task, _installdir) are passed only if declared, as
// Bolt does, since an undeclared named parameter fails to bind.
func powershellWrapper(script string) string {
	quoted := "'" + strings.ReplaceAll(script, "'", "''") + "'"
	return `$ErrorActionPreference = 'Stop'
$script = ` + quoted + `
$json = [Console]::In.ReadToEnd()
$params = @{}
if ($json) {
  ($json | ConvertFrom-Json).psobject.properties | ForEach-Object { $params[$_.Name] = $_.Value }
}
$declared = (Get-Command -Name $script).Parameters.Keys
$pass = @{}
foreach ($name in $params.Keys) {
  if ($declared -contains $name) { $pass[$name] = $params[$name] }
}
& $script @pass
if ($null -ne $LASTEXITCODE) { exit $LASTEXITCODE }
`
}

// parseTaskOutput is a task's structured result: its stdout when that is
// a JSON object, and otherwise the text under _output, as Bolt and PE
// report it.
func parseTaskOutput(stdout string) json.RawMessage { return taskexec.ParseOutput(stdout) }

// TaskRunner starts a task's process and waits for it, killing the
// process and everything it started if cmd.Timeout passes. timedOut
// reports that it did. err is only for a process that could not run at
// all - a non-zero exit is an ordinary outcome.
type TaskRunner func(ctx context.Context, cmd taskCommand) (stdout, stderr string, exitCode int, timedOut bool, err error)

// taskExecutor is the real TaskExecutor: it downloads, verifies and runs
// tasks as described in design.md in add-task-execution.
type taskExecutor struct {
	cache   *TaskCache
	runsDir string
	server  func(ctx context.Context) (string, error)
	runner  TaskRunner
	paths   agentPaths
	goos    string
}

// NewTaskExecutor builds the TaskExecutor for this node from cfg. command
// runs `puppet config print` when the Puppet server has to be looked up.
func NewTaskExecutor(cfg Config, command CommandRunner) (TaskExecutor, error) {
	cache, err := NewTaskCache(cfg.TaskCacheDir, cfg.CertFile, cfg.KeyFile, cfg.CAFile)
	if err != nil {
		return nil, err
	}
	server := func(ctx context.Context) (string, error) {
		if cfg.PuppetServer != "" {
			return cfg.PuppetServer, nil
		}
		return PuppetServerFromConfig(ctx, command, cfg.PuppetBinPath)
	}
	return &taskExecutor{
		cache: cache,
		// Next to the cache rather than in the system temp directory,
		// which is often mounted noexec.
		runsDir: filepath.Join(filepath.Dir(cfg.TaskCacheDir), "task-runs"),
		server:  server,
		runner:  RunTaskProcess,
		paths:   defaultAgentPaths(cfg.PuppetBinPath, runtime.GOOS, os.Getenv),
		goos:    runtime.GOOS,
	}, nil
}

// RunTask implements TaskExecutor.
func (e *taskExecutor) RunTask(ctx context.Context, params json.RawMessage) []byte {
	return e.runTask(ctx, params)
}

// errNoResolvedFiles is what an older console's bare task name gets: it
// never worked, and the fix is on the console's side.
var errNoResolvedFiles = errors.New("task request is missing resolved files; upgrade the console")

// runTask executes one task request and returns the response payload.
func (e *taskExecutor) runTask(ctx context.Context, raw json.RawMessage) []byte {
	var req taskRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return errorResponse(fmt.Sprintf("invalid task request: %v", err))
	}
	if req.Task == "" {
		return errorResponse("task request missing required \"task\" field")
	}
	if len(req.Files) == 0 || len(req.Implementations) == 0 {
		return errorResponse(errNoResolvedFiles.Error())
	}
	if req.TimeoutSeconds <= 0 {
		return errorResponse("task request has no timeout")
	}
	module, _, _ := strings.Cut(req.Task, "::")

	impl, err := chooseImplementation(req.Implementations, e.goos)
	if err != nil {
		return errorResponse(err.Error())
	}

	e.cache.Expire()
	server, err := e.server(ctx)
	if err != nil {
		return errorResponse(fmt.Sprintf("cannot find this node's Puppet server: %v", err))
	}
	byName := map[string]taskFile{}
	for _, f := range req.Files {
		byName[f.Filename] = f
	}
	needed := append([]string{impl.Name}, impl.Files...)
	// Every name is checked before anything is downloaded, so a
	// malformed request costs nothing.
	for _, name := range needed {
		if _, ok := byName[name]; !ok {
			return errorResponse(fmt.Sprintf("task request lists no file %s", name))
		}
	}
	cached := map[string]string{}
	for _, name := range needed {
		path, err := e.cache.Fetch(ctx, server, byName[name])
		if err != nil {
			return errorResponse(err.Error())
		}
		cached[name] = path
	}

	dir, err := buildInstallDir(e.runsDir, module, impl.Name, impl.Files, cached)
	if err != nil {
		return errorResponse(err.Error())
	}
	defer dir.remove()

	method := effectiveInputMethod(req, impl)
	installDir := ""
	if len(impl.Files) > 0 {
		installDir = dir.root
	}
	stdin, env, err := taskInput(method, req.Params, req.Task, installDir)
	if err != nil {
		return errorResponse(err.Error())
	}

	wrapper := ""
	if method == "powershell" && strings.EqualFold(filepath.Ext(impl.Name), ".ps1") {
		wrapper = filepath.Join(dir.root, "wrapper.ps1")
		if err := os.WriteFile(wrapper, []byte(powershellWrapper(dir.executable)), 0o700); err != nil {
			return errorResponse(fmt.Sprintf("write PowerShell wrapper: %v", err))
		}
	}
	name, args, err := commandFor(dir.executable, wrapper, e.goos, e.paths)
	if err != nil {
		return errorResponse(err.Error())
	}

	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	stdout, stderr, exitCode, timedOut, err := e.runner(ctx, taskCommand{
		Name: name, Args: args, Dir: dir.root, Env: env, Stdin: stdin, Timeout: timeout,
	})
	if err != nil {
		return errorResponse(fmt.Sprintf("failed to execute %s: %v", impl.Name, err))
	}
	if timedOut {
		return errorResponse(fmt.Sprintf("task timed out after %s and was stopped", timeout))
	}
	return encodeResult(&responseData{
		Output: outputData{Stdout: stdout, Stderr: stderr, ExitCode: exitCode},
		Result: parseTaskOutput(stdout),
	})
}
