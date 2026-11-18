package nodeagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestChooseImplementation(t *testing.T) {
	shell := taskImplementation{Name: "t.sh", Requirements: []string{"shell"}}
	ps := taskImplementation{Name: "t.ps1", Requirements: []string{"powershell"}}
	rb := taskImplementation{Name: "t.rb", Requirements: []string{"puppet-agent"}}
	none := taskImplementation{Name: "t"}

	for name, tc := range map[string]struct {
		impls []taskImplementation
		goos  string
		want  string
	}{
		"shell on linux":          {[]taskImplementation{shell, ps}, "linux", "t.sh"},
		"powershell on windows":   {[]taskImplementation{shell, ps}, "windows", "t.ps1"},
		"first match wins":        {[]taskImplementation{rb, shell}, "linux", "t.rb"},
		"puppet-agent everywhere": {[]taskImplementation{ps, rb}, "darwin", "t.rb"},
		"no requirements":         {[]taskImplementation{none}, "windows", "t"},
	} {
		got, err := chooseImplementation(tc.impls, tc.goos)
		if err != nil || got.Name != tc.want {
			t.Errorf("%s: got %q, %v; want %q", name, got.Name, err, tc.want)
		}
	}

	_, err := chooseImplementation([]taskImplementation{ps}, "linux")
	if err == nil || !strings.Contains(err.Error(), "no implementation") || !strings.Contains(err.Error(), "t.ps1") {
		t.Errorf("PowerShell-only task on Linux: err = %v", err)
	}
}

func TestCommandFor(t *testing.T) {
	unix := defaultAgentPaths("/opt/puppetlabs/bin/puppet", "linux", func(string) string { return "" })
	win := defaultAgentPaths(`C:\Program Files\Puppet Labs\Puppet\bin\puppet.bat`, "windows", func(string) string { return "" })
	for name, tc := range map[string]struct {
		exe, wrapper, goos string
		paths              agentPaths
		want               string
	}{
		"shell script":       {"/r/m/tasks/t.sh", "", "linux", unix, "/r/m/tasks/t.sh"},
		"python by shebang":  {"/r/m/tasks/t.py", "", "darwin", unix, "/r/m/tasks/t.py"},
		"ruby via the agent": {"/r/m/tasks/t.rb", "", "linux", unix, "/opt/puppetlabs/puppet/bin/ruby /r/m/tasks/t.rb"},
		"manifest":           {"/r/m/tasks/t.pp", "", "linux", unix, "/opt/puppetlabs/bin/puppet apply /r/m/tasks/t.pp"},
		"powershell":         {`C:\r\t.ps1`, "", "windows", win, `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -NonInteractive -NoLogo -ExecutionPolicy Bypass -File C:\r\t.ps1`},
		"powershell wrapper": {`C:\r\t.ps1`, `C:\r\wrapper.ps1`, "windows", win, `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -NoProfile -NonInteractive -NoLogo -ExecutionPolicy Bypass -File C:\r\wrapper.ps1`},
		"batch file":         {`C:\r\t.bat`, "", "windows", win, `C:\Windows\System32\cmd.exe /c C:\r\t.bat`},
		"ruby on windows":    {`C:\r\t.rb`, "", "windows", win, `C:\Program Files\Puppet Labs\Puppet\puppet\bin\ruby.exe C:\r\t.rb`},
		"exe on windows":     {`C:\r\t.exe`, "", "windows", win, `C:\r\t.exe`},
	} {
		cmd, args, err := commandFor(tc.exe, tc.wrapper, tc.goos, tc.paths)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got := strings.Join(append([]string{cmd}, args...), " "); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, tc.want)
		}
	}
	if _, _, err := commandFor(`C:\r\t.sh`, "", "windows", win); err == nil {
		t.Error("a shell script was accepted on Windows")
	}
}

func TestEffectiveInputMethod(t *testing.T) {
	for name, tc := range map[string]struct {
		req  taskRequest
		impl taskImplementation
		want string
	}{
		"default":                 {taskRequest{}, taskImplementation{Name: "t.sh"}, "both"},
		"task level":              {taskRequest{InputMethod: "stdin"}, taskImplementation{Name: "t.sh"}, "stdin"},
		"implementation wins":     {taskRequest{InputMethod: "environment"}, taskImplementation{Name: "t.ps1", InputMethod: "powershell"}, "powershell"},
		"ps1 defaults powershell": {taskRequest{}, taskImplementation{Name: "t.PS1"}, "powershell"},
	} {
		if got := effectiveInputMethod(tc.req, tc.impl); got != tc.want {
			t.Errorf("%s: got %s, want %s", name, got, tc.want)
		}
	}
}

func TestTaskInputEncoding(t *testing.T) {
	params := map[string]any{"name": "web", "count": float64(2), "tags": []any{"a"}, "on": true}
	stdin, env, err := taskInput("both", params, "mod::t", "/run/x")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	_ = json.Unmarshal(stdin, &decoded)
	if decoded["name"] != "web" || decoded["_task"] != "mod::t" || decoded["_installdir"] != "/run/x" {
		t.Errorf("stdin = %s", stdin)
	}
	want := []string{"PT__installdir=/run/x", "PT__task=mod::t", "PT_count=2", "PT_name=web", "PT_on=true", `PT_tags=["a"]`}
	if strings.Join(env, "|") != strings.Join(want, "|") {
		t.Errorf("env = %v, want %v", env, want)
	}

	stdin, env, _ = taskInput("environment", params, "mod::t", "")
	if stdin != nil || len(env) != 5 {
		t.Errorf("environment method: stdin %s, env %v", stdin, env)
	}
	stdin, env, _ = taskInput("stdin", params, "mod::t", "")
	if stdin == nil || env != nil || strings.Contains(string(stdin), "_installdir") {
		t.Errorf("stdin method: stdin %s, env %v", stdin, env)
	}
	if stdin, env, _ = taskInput("powershell", params, "mod::t", ""); stdin == nil || env != nil {
		t.Errorf("powershell method: stdin %s, env %v", stdin, env)
	}
}

func TestPowershellWrapper(t *testing.T) {
	w := powershellWrapper(`C:\runs\task-1\mod\tasks\it's.ps1`)
	for _, want := range []string{
		`$script = 'C:\runs\task-1\mod\tasks\it''s.ps1'`, // single quotes doubled, nothing else interpreted
		`[Console]::In.ReadToEnd()`,
		`ConvertFrom-Json`,
		`(Get-Command -Name $script).Parameters.Keys`,
		`& $script @pass`,
		`exit $LASTEXITCODE`,
	} {
		if !strings.Contains(w, want) {
			t.Errorf("wrapper lacks %s:\n%s", want, w)
		}
	}
}

func TestParseTaskOutput(t *testing.T) {
	for in, want := range map[string]string{
		`{"status":"ok"}`:        `{"status":"ok"}`,
		"  {\"a\":1}\n":          `{"a":1}`,
		"done\n":                 `{"_output":"done\n"}`,
		`[1,2]`:                  `{"_output":"[1,2]"}`,
		`{"broken"`:              `{"_output":"{\"broken\""}`,
		"":                       `{"_output":""}`,
		`{"_error":{"msg":"x"}}`: `{"_error":{"msg":"x"}}`,
	} {
		if got := string(parseTaskOutput(in)); got != want {
			t.Errorf("parseTaskOutput(%q) = %s, want %s", in, got, want)
		}
	}
}

// --- running real tasks (Unix) ---

func skipOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("these tasks are shell scripts")
	}
}

// testExecutor is a real taskExecutor whose Puppet server is a local
// fileServer serving files.
func testExecutor(t *testing.T, files map[string]string) (*taskExecutor, *fileServer) {
	t.Helper()
	served := map[string]string{}
	for name, content := range files {
		served["/puppet/v3/file_content/tasks/"+name] = content
	}
	fs := startFileServer(t, served)
	return &taskExecutor{
		cache:   fs.cache(t),
		runsDir: filepath.Join(t.TempDir(), "task-runs"),
		server:  func(context.Context) (string, error) { return fs.addr, nil },
		runner:  RunTaskProcess,
		paths:   defaultAgentPaths("puppet", runtime.GOOS, os.Getenv),
		goos:    runtime.GOOS,
	}, fs
}

func fileFor(name, content string) taskFile {
	return taskFile{
		Filename: name,
		Path:     "/puppet/v3/file_content/tasks/" + name,
		Params:   map[string]string{"environment": "production"},
		SHA256:   sha(content),
		Size:     int64(len(content)),
	}
}

func runTestTask(t *testing.T, e *taskExecutor, req taskRequest) wireResponse {
	t.Helper()
	if req.TimeoutSeconds == 0 {
		req.TimeoutSeconds = 30
	}
	raw, _ := json.Marshal(req)
	return decodeWireResponse(t, e.RunTask(context.Background(), raw))
}

func TestRunTaskInputMethods(t *testing.T) {
	skipOnWindows(t)
	// Prints what it received both ways, as one JSON object.
	const script = `#!/bin/sh
input=$(cat)
printf '{"stdin":%s,"env_name":"%s","env_task":"%s"}\n' "${input:-null}" "$PT_name" "$PT__task"
`
	for method, check := range map[string]func(map[string]any) bool{
		"stdin": func(r map[string]any) bool {
			in, _ := r["stdin"].(map[string]any)
			return in["name"] == "web" && in["_task"] == "mod::show" && r["env_name"] == ""
		},
		"environment": func(r map[string]any) bool {
			return r["stdin"] == nil && r["env_name"] == "web" && r["env_task"] == "mod::show"
		},
		"both": func(r map[string]any) bool {
			in, _ := r["stdin"].(map[string]any)
			return in["name"] == "web" && r["env_name"] == "web"
		},
	} {
		t.Run(method, func(t *testing.T) {
			e, _ := testExecutor(t, map[string]string{"show.sh": script})
			wr := runTestTask(t, e, taskRequest{
				Task: "mod::show", InputMethod: method,
				Implementations: []taskImplementation{{Name: "show.sh"}},
				Files:           []taskFile{fileFor("show.sh", script)},
				Params:          map[string]any{"name": "web"},
			})
			if wr.Error != "" || wr.Result == nil {
				t.Fatalf("response = %+v", wr)
			}
			var result map[string]any
			if err := json.Unmarshal(wr.Result.Result, &result); err != nil {
				t.Fatalf("result %s: %v", wr.Result.Result, err)
			}
			if !check(result) {
				t.Errorf("result = %s", wr.Result.Result)
			}
		})
	}
}

func TestRunTaskSupportingFilesAndInstallDir(t *testing.T) {
	skipOnWindows(t)
	const script = `#!/bin/sh
. "$PT__installdir/task_fixture/files/helper.sh"
greet "$PT_name"
`
	const helper = "greet() {\n  printf '{\"greeting\":\"hello %s\"}\\n' \"$1\"\n}\n"
	e, _ := testExecutor(t, map[string]string{"multi.sh": script, "task_fixture/files/helper.sh": helper})
	wr := runTestTask(t, e, taskRequest{
		Task: "task_fixture::multi", InputMethod: "environment",
		Implementations: []taskImplementation{
			{Name: "multi.ps1", Requirements: []string{"powershell"}},
			{Name: "multi.sh", Requirements: []string{"shell"}, Files: []string{"task_fixture/files/helper.sh"}},
		},
		Files: []taskFile{
			fileFor("multi.sh", script),
			fileFor("task_fixture/files/helper.sh", helper),
		},
		Params: map[string]any{"name": "web"},
	})
	if wr.Error != "" || wr.Result == nil || string(wr.Result.Result) != `{"greeting":"hello web"}` {
		t.Fatalf("response = %+v (result %s)", wr, func() string {
			if wr.Result != nil {
				return string(wr.Result.Result)
			}
			return ""
		}())
	}
	// The run directory is gone afterwards.
	entries, _ := os.ReadDir(e.runsDir)
	if len(entries) != 0 {
		t.Errorf("%d run directories left behind", len(entries))
	}
}

func TestRunTaskErrorAndPlainOutput(t *testing.T) {
	skipOnWindows(t)
	const failing = "#!/bin/sh\necho '{\"_error\":{\"msg\":\"nope\"}}'\necho oops >&2\nexit 1\n"
	const plain = "#!/bin/sh\necho done\n"
	e, _ := testExecutor(t, map[string]string{"fail.sh": failing, "plain.sh": plain})

	wr := runTestTask(t, e, taskRequest{Task: "mod::fail", Implementations: []taskImplementation{{Name: "fail.sh"}}, Files: []taskFile{fileFor("fail.sh", failing)}})
	if wr.Result == nil || wr.Result.Output.ExitCode != 1 || string(wr.Result.Result) != `{"_error":{"msg":"nope"}}` || wr.Result.Output.Stderr != "oops\n" {
		t.Errorf("failing task: %+v", wr.Result)
	}

	wr = runTestTask(t, e, taskRequest{Task: "mod::plain", Implementations: []taskImplementation{{Name: "plain.sh"}}, Files: []taskFile{fileFor("plain.sh", plain)}})
	if wr.Result == nil || string(wr.Result.Result) != `{"_output":"done\n"}` || wr.Result.Output.ExitCode != 0 {
		t.Errorf("plain task: %+v", wr.Result)
	}
}

func TestRunTaskChecksumMismatchRunsNothing(t *testing.T) {
	skipOnWindows(t)
	marker := filepath.Join(t.TempDir(), "ran")
	script := "#!/bin/sh\ntouch " + marker + "\n"
	e, _ := testExecutor(t, map[string]string{"t.sh": script})
	f := fileFor("t.sh", script)
	f.SHA256 = sha("what the console resolved")
	wr := runTestTask(t, e, taskRequest{Task: "mod::t", Implementations: []taskImplementation{{Name: "t.sh"}}, Files: []taskFile{f}})
	if !strings.Contains(wr.Error, "checksum mismatch for t.sh") {
		t.Errorf("Error = %q", wr.Error)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the task ran despite the mismatch")
	}
}

func TestRunTaskSecondRunUsesCache(t *testing.T) {
	skipOnWindows(t)
	const script = "#!/bin/sh\necho '{}'\n"
	e, fs := testExecutor(t, map[string]string{"t.sh": script})
	req := taskRequest{Task: "mod::t", Implementations: []taskImplementation{{Name: "t.sh"}}, Files: []taskFile{fileFor("t.sh", script)}}
	runTestTask(t, e, req)
	runTestTask(t, e, req)
	if n := fs.requests.Load(); n != 1 {
		t.Errorf("%d downloads for two runs, want 1", n)
	}
}

func TestRunTaskRefusals(t *testing.T) {
	e, fs := testExecutor(t, nil)
	for name, tc := range map[string]struct {
		req  taskRequest
		want string
	}{
		"bare task name from an old console": {taskRequest{Task: "package"}, "upgrade the console"},
		"no suitable implementation": {taskRequest{Task: "mod::t",
			Implementations: []taskImplementation{{Name: "t.ps1", Requirements: []string{"powershell"}}, {Name: "t.x", Requirements: []string{"bolt"}}},
			Files:           []taskFile{fileFor("t.ps1", "x")}}, "no implementation"},
		"unlisted file": {taskRequest{Task: "mod::t",
			Implementations: []taskImplementation{{Name: "t.sh", Files: []string{"mod/lib/x"}}},
			Files:           []taskFile{fileFor("t.sh", "x")}}, "lists no file mod/lib/x"},
	} {
		if runtime.GOOS == "windows" && name == "no suitable implementation" {
			continue
		}
		wr := runTestTask(t, e, tc.req)
		if !strings.Contains(wr.Error, tc.want) {
			t.Errorf("%s: Error = %q, want it to mention %q", name, wr.Error, tc.want)
		}
	}
	if fs.requests.Load() != 0 {
		t.Error("files were downloaded for a refused request")
	}
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func TestRunTaskTimeoutKillsTheWholeTree(t *testing.T) {
	skipOnWindows(t)
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	// Starts a child that would outlive the task, then waits on it.
	script := "#!/bin/sh\nsleep 60 &\necho $! > " + pidFile + "\nwait\n"
	e, _ := testExecutor(t, map[string]string{"sleep.sh": script})

	// Through the real client's dispatch, so the busy flag is exercised.
	client := &Client{logger: slog.Default(), handler: NewHandlerWithTasks(fakeRunner("", "", 0, nil), "puppet", t.TempDir(), noFileExists, e)}
	req, _ := json.Marshal(taskRequest{
		Task: "mod::sleep", TimeoutSeconds: 1,
		Implementations: []taskImplementation{{Name: "sleep.sh"}},
		Files:           []taskFile{fileFor("sleep.sh", script)},
	})
	payload := requestPayload(t, "puppet", actionRunTask, json.RawMessage(req))

	start := time.Now()
	resp := client.handler(context.Background(), payload)
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("took %s to give up on a 1s task", elapsed)
	}
	wr := decodeWireResponse(t, resp)
	if !strings.Contains(wr.Error, "timed out") {
		t.Errorf("Error = %q, want a timeout", wr.Error)
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the task never started its child: %v", err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	deadline := time.Now().Add(5 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if processAlive(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("the task's child process %d outlived the timeout", pid)
	}

	// And the agent is free for the next request.
	client.dispatch(context.Background(), &nats.Msg{Subject: "test.dispatch", Data: requestPayload(t, "puppet", actionRun, nil)})
	if client.busy.Load() {
		t.Error("the client is still busy after the timed-out task")
	}
}

func TestRunTaskProcessPassesEnvironmentAndDirectory(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	stdout, _, code, timedOut, err := RunTaskProcess(context.Background(), taskCommand{
		Name: sh, Args: []string{"-c", `printf '%s %s ' "$PT_x" "$(pwd)"; cat; exit 3`},
		Dir: dir, Env: []string{"PT_x=1"}, Stdin: []byte("in"), Timeout: 10 * time.Second,
	})
	if err != nil || timedOut || code != 3 {
		t.Fatalf("code %d, timedOut %v, err %v", code, timedOut, err)
	}
	realDir, _ := filepath.EvalSymlinks(dir)
	if stdout != "1 "+realDir+" in" && stdout != "1 "+dir+" in" {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestRunTaskRubyThroughTheAgent(t *testing.T) {
	skipOnWindows(t)
	ruby, err := exec.LookPath("ruby")
	if err != nil {
		if _, statErr := os.Stat("/opt/puppetlabs/puppet/bin/ruby"); statErr != nil {
			t.Skip("no Ruby on this machine")
		}
		ruby = "/opt/puppetlabs/puppet/bin/ruby"
	}
	const script = "require 'json'\nparams = JSON.parse(STDIN.read)\nputs({ 'shout' => params['word'].upcase }.to_json)\n"
	e, _ := testExecutor(t, map[string]string{"shout.rb": script})
	e.paths.ruby = ruby
	wr := runTestTask(t, e, taskRequest{Task: "mod::shout", Implementations: []taskImplementation{{Name: "shout.rb"}}, Files: []taskFile{fileFor("shout.rb", script)}, Params: map[string]any{"word": "hi"}})
	if wr.Result == nil || string(wr.Result.Result) != `{"shout":"HI"}` {
		t.Errorf("response = %+v", wr)
	}
}
