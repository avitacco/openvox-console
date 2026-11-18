package agentless

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
)

func dispatch(r *rig, name string, task taskcatalog.Resolved) wireResponse {
	r.t.Helper()
	params, _ := json.Marshal(task)
	payload, _ := json.Marshal(requestData{Module: "puppet", Action: "run_task", Params: params})
	raw, err := r.exec.Dispatch(context.Background(), name, payload, time.Minute)
	if err != nil {
		r.t.Fatalf("Dispatch: %v", err)
	}
	return decode(r.t, raw)
}

const echoScript = `#!/bin/sh
echo "{\"greeting\": \"$PT_greeting\", \"user\": \"$(id -un)\", \"cwd_ok\": true}"
`

func TestRunTask_ShellTaskResultIsRecordedAsForAnAgent(t *testing.T) {
	r := newRig(t)
	w := dispatch(r, "host1", r.task(echoScript, map[string]any{"greeting": "hello"}, 30))
	if w.Error != "" || w.Result == nil {
		t.Fatalf("response = %+v", w)
	}
	if w.Result.Output.ExitCode != 0 || !strings.Contains(w.Result.Output.Stdout, `"greeting": "hello"`) {
		t.Errorf("output = %+v", w.Result.Output)
	}
	var parsed map[string]any
	if err := json.Unmarshal(w.Result.Result, &parsed); err != nil || parsed["greeting"] != "hello" {
		t.Errorf("result = %s (%v)", w.Result.Result, err)
	}
	if left := r.ssh.leftovers(); len(left) != 0 {
		t.Errorf("the task directory was left behind: %v", left)
	}
}

func TestRunTask_AFailingTaskIsItsOwnResult(t *testing.T) {
	r := newRig(t)
	w := dispatch(r, "host1", r.task("#!/bin/sh\necho oops >&2\nexit 3\n", nil, 30))
	if w.Error != "" || w.Result.Output.ExitCode != 3 || !strings.Contains(w.Result.Output.Stderr, "oops") {
		t.Errorf("response = %+v / %q", w.Result, w.Error)
	}
}

func TestRunTask_ParametersNeverAppearOnACommandLine(t *testing.T) {
	r := newRig(t)
	const secret = "s3cr3t-Value-42"
	task := r.task("#!/bin/sh\necho \"len=${#PT_token}\"\ncat | grep -c 'token' \n", map[string]any{"token": secret, "other": "also-visible?"}, 30)
	w := dispatch(r, "host1", task)
	if w.Error != "" || !strings.Contains(w.Result.Output.Stdout, "len=15") {
		t.Fatalf("the task did not receive its parameter: %+v %q", w.Result, w.Error)
	}
	for _, c := range r.ssh.commandLog() {
		if strings.Contains(c, secret) || strings.Contains(c, "also-visible") {
			t.Errorf("a parameter is in a command line: %s", c)
		}
	}
	// The task directory held only the task, and is gone.
	if left := r.ssh.leftovers(); len(left) != 0 {
		t.Errorf("leftovers: %v", left)
	}
}

func TestRunTask_StdinMethod(t *testing.T) {
	r := newRig(t)
	task := r.task("#!/bin/sh\ncat\n", map[string]any{"a": 1}, 30)
	task.InputMethod = "stdin"
	w := dispatch(r, "host1", task)
	if !strings.Contains(w.Result.Output.Stdout, `"a":1`) || !strings.Contains(w.Result.Output.Stdout, `"_task":"demo::echo"`) {
		t.Errorf("stdout = %q", w.Result.Output.Stdout)
	}
	for _, c := range r.ssh.commandLog() {
		if strings.Contains(c, `"a":1`) {
			t.Errorf("stdin input on a command line: %s", c)
		}
	}
}

func TestRunTask_AnUntrustedHostKeyRunsNothing(t *testing.T) {
	r := newRig(t)
	c := r.conn(1, "ops", "right-password")
	c.PinnedKey = nil
	r.set("host1", c)
	w := dispatch(r, "host1", r.task(echoScript, nil, 30))
	if !strings.Contains(w.Error, "host key") || !strings.Contains(w.Error, "not been trusted") {
		t.Errorf("error = %q", w.Error)
	}
	if got := r.ssh.commandLog(); len(got) != 0 {
		t.Errorf("commands ran: %v", got)
	}
	if pw, key := r.ssh.attempts(); pw+key != 0 {
		t.Errorf("a credential was offered to an untrusted host (%d, %d)", pw, key)
	}
}

func TestRunTask_AChangedHostKeyIsRefusedBeforeAnyCredential(t *testing.T) {
	r := newRig(t)
	other, _ := newSigner(t)
	c := r.conn(1, "ops", "right-password")
	c.PinnedKey = other.PublicKey().Marshal() // the server presents its own, different key
	r.set("host1", c)
	w := dispatch(r, "host1", r.task(echoScript, nil, 30))
	if !strings.Contains(w.Error, "not the pinned one") {
		t.Errorf("error = %q", w.Error)
	}
	if pw, key := r.ssh.attempts(); pw+key != 0 {
		t.Errorf("a credential was sent to a host with a changed key (%d, %d)", pw, key)
	}
	if len(r.reg.flagged[1]) == 0 {
		t.Error("the changed key was not flagged")
	}
}

func TestRunTask_WrongPasswordNamesTheStageNotThePassword(t *testing.T) {
	r := newRig(t)
	r.set("host1", r.conn(1, "ops", "wrong-password-xyz"))
	w := dispatch(r, "host1", r.task(echoScript, nil, 30))
	if !strings.HasPrefix(w.Error, "authentication failed") || strings.Contains(w.Error, "wrong-password-xyz") {
		t.Errorf("error = %q", w.Error)
	}
	if got := r.ssh.commandLog(); len(got) != 0 {
		t.Errorf("commands ran after a failed login: %v", got)
	}
}

func TestRunTask_PublicKeyLogin(t *testing.T) {
	r := newRig(t)
	pemKey, _ := testKey(t, "")
	signer, err := parseSigner(pemKey)
	if err != nil {
		t.Fatal(err)
	}
	r.ssh.mu.Lock()
	r.ssh.authorized = signer.PublicKey()
	r.ssh.mu.Unlock()
	c := r.conn(1, "ops", "")
	c.PrivateKey = []byte(pemKey)
	r.set("host1", c)
	w := dispatch(r, "host1", r.task(echoScript, map[string]any{"greeting": "k"}, 30))
	if w.Error != "" || !strings.Contains(w.Result.Output.Stdout, `"greeting": "k"`) {
		t.Errorf("response = %+v %q", w.Result, w.Error)
	}
}

func TestRunTask_RunAs(t *testing.T) {
	r := newRig(t)
	c := r.conn(1, "ops", "right-password")
	c.RunAs = "deploy"
	r.set("host1", c)
	w := dispatch(r, "host1", r.task(echoScript, map[string]any{"greeting": "as-deploy"}, 30))
	if w.Error != "" || !strings.Contains(w.Result.Output.Stdout, "as-deploy") {
		t.Fatalf("response = %+v %q", w.Result, w.Error)
	}
	if got := strings.TrimSpace(r.ssh.sudoLog()); got != "deploy" {
		t.Errorf("sudo was asked for %q, want deploy", got)
	}
	// The parameter survived sudo's environment reset and is not on a command line.
	for _, cmd := range r.ssh.commandLog() {
		if strings.Contains(cmd, "as-deploy") {
			t.Errorf("a parameter is in a command line: %s", cmd)
		}
	}
}

func TestRunTask_SudoNeedingAPasswordIsABecomeFailure(t *testing.T) {
	r := newRig(t)
	c := r.conn(1, "ops", "right-password")
	c.RunAs = "root"
	r.set("host1", c)
	r.ssh.setSudoRefuses(true)
	w := dispatch(r, "host1", r.task(echoScript, nil, 30))
	if !strings.HasPrefix(w.Error, "could not run as the run-as user") || !strings.Contains(w.Error, "password is required") {
		t.Errorf("error = %q", w.Error)
	}
	if left := r.ssh.leftovers(); len(left) != 0 {
		t.Errorf("leftovers: %v", left)
	}
}

func TestRunTask_TimeoutStopsTheTaskAndCleansUp(t *testing.T) {
	r := newRig(t)
	begun := time.Now()
	w := dispatch(r, "host1", r.task("#!/bin/sh\nsleep 30\n", nil, 1))
	if !strings.Contains(w.Error, "timed out after 1s") {
		t.Errorf("error = %q", w.Error)
	}
	if time.Since(begun) > 10*time.Second {
		t.Errorf("took %s", time.Since(begun))
	}
	if left := r.ssh.leftovers(); len(left) != 0 {
		t.Errorf("the directory survived a timeout: %v", left)
	}
}

func TestRunTask_AChecksumMismatchTouchesNothing(t *testing.T) {
	r := newRig(t)
	task := r.task(echoScript, nil, 30)
	task.Files[0].SHA256 = strings.Repeat("a", 64) // not what the source holds
	w := dispatch(r, "host1", task)
	if !strings.Contains(w.Error, "checksum") {
		t.Errorf("error = %q", w.Error)
	}
	if got := r.ssh.commandLog(); len(got) != 0 {
		t.Errorf("commands ran: %v", got)
	}
}

func TestRunTask_NoImplementationSuits(t *testing.T) {
	r := newRig(t)
	task := r.task(echoScript, nil, 30)
	task.Implementations[0].Requirements = []string{"puppet-agent"}
	w := dispatch(r, "host1", task)
	if !strings.Contains(w.Error, "no implementation of this task suits") || !strings.Contains(w.Error, "puppet-agent") {
		t.Errorf("error = %q", w.Error)
	}
}

func TestRunTask_OutputIsCapped(t *testing.T) {
	r := newRig(t)
	w := dispatch(r, "host1", r.task("#!/bin/sh\nhead -c 400000 /dev/zero | tr '\\0' x\n", nil, 30))
	if w.Error != "" || len(w.Result.Output.Stdout) > 300<<10 || !strings.Contains(w.Result.Output.Stdout, "[output truncated") {
		t.Errorf("stdout length %d, error %q", len(w.Result.Output.Stdout), w.Error)
	}
}

func TestRunTask_SupportingFiles(t *testing.T) {
	r := newRig(t)
	task := r.task("#!/bin/sh\ncat \"$PT__installdir/demo/files/data.txt\"\n", nil, 30)
	support := "from the support file"
	sum := sha256Hex(support)
	r.files.content[sum] = []byte(support)
	task.Implementations[0].Files = []string{"demo/files/data.txt"}
	task.Files = append(task.Files, taskcatalog.ResolvedFile{Filename: "demo/files/data.txt", Path: "/f2", SHA256: sum, Size: int64(len(support))})
	w := dispatch(r, "host1", task)
	if w.Error != "" || strings.TrimSpace(w.Result.Output.Stdout) != support {
		t.Errorf("stdout = %q, error %q", w.Result.Output.Stdout, w.Error)
	}
}

func TestRunTask_UnsafeNamesAreRefused(t *testing.T) {
	r := newRig(t)
	task := r.task(echoScript, nil, 30)
	task.Implementations[0].Files = []string{"../../etc/passwd"}
	if w := dispatch(r, "host1", task); !strings.Contains(w.Error, "invalid supporting file path") {
		t.Errorf("error = %q", w.Error)
	}
	if got := r.ssh.commandLog(); len(got) != 0 {
		t.Errorf("commands ran: %v", got)
	}
}

func TestRunTask_OnlyTasks(t *testing.T) {
	r := newRig(t)
	payload, _ := json.Marshal(requestData{Module: "puppet", Action: "run"})
	raw, _ := r.exec.Dispatch(context.Background(), "host1", payload, time.Minute)
	if w := decode(t, raw); !strings.Contains(w.Error, "only run tasks") {
		t.Errorf("error = %q", w.Error)
	}
}

func TestRunTask_ASecondTaskOnABusyTargetFails(t *testing.T) {
	r := newRig(t)
	var wg sync.WaitGroup
	var first wireResponse
	wg.Add(1)
	go func() {
		defer wg.Done()
		first = dispatch(r, "host1", r.task("#!/bin/sh\nsleep 2\necho done\n", nil, 30))
	}()
	time.Sleep(700 * time.Millisecond)
	second := dispatch(r, "host1", r.task(echoScript, nil, 30))
	if !strings.Contains(second.Error, "already running a task") {
		t.Errorf("second error = %q", second.Error)
	}
	wg.Wait()
	if first.Error != "" || !strings.Contains(first.Result.Output.Stdout, "done") {
		t.Errorf("the first task was disturbed: %+v %q", first.Result, first.Error)
	}
}

func TestRunTask_TheGlobalCapQueuesTargets(t *testing.T) {
	r := newRig(t)
	r.exec.global = make(chan struct{}, 1)
	r.set("host2", func() Connection { c := r.conn(2, "ops", "right-password"); c.Name = "host2"; return c }())
	var wg sync.WaitGroup
	var firstDone, secondDone time.Time
	run := func(name string, script string, out *time.Time) {
		defer wg.Done()
		dispatch(r, name, r.task(script, nil, 30))
		*out = time.Now()
	}
	wg.Add(2)
	go run("host1", "#!/bin/sh\nsleep 1\n", &firstDone)
	time.Sleep(300 * time.Millisecond)
	go run("host2", "#!/bin/sh\ntrue\n", &secondDone)
	wg.Wait()
	if !secondDone.After(firstDone.Add(-50 * time.Millisecond)) {
		t.Errorf("the second target ran alongside the first despite a cap of one (%s vs %s)", firstDone, secondDone)
	}
}

func TestRunTask_TheFileCacheSavesDownloads(t *testing.T) {
	r := newRig(t)
	task := r.task(echoScript, nil, 30)
	dispatch(r, "host1", task)
	dispatch(r, "host1", task)
	if r.files.fetched != 1 {
		t.Errorf("fetched %d times, want once", r.files.fetched)
	}
}

func TestRunTask_ErrorsNeverContainSecrets(t *testing.T) {
	r := newRig(t)
	c := r.conn(1, "ops", "pa55w0rd-leak")
	c.Port = 1 // nothing listens there
	r.set("host1", c)
	w := dispatch(r, "host1", r.task(echoScript, nil, 30))
	if !strings.HasPrefix(w.Error, "could not connect") || strings.Contains(w.Error, "pa55w0rd-leak") {
		t.Errorf("error = %q", w.Error)
	}
}

func TestCheck(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	res, err := r.exec.Check(ctx, "host1")
	if err != nil || !res.OK || res.LoggedInAs == "" {
		t.Fatalf("check = %+v, %v", res, err)
	}

	// Run-as is proven too.
	c := r.conn(1, "ops", "right-password")
	c.RunAs = "deploy"
	r.set("host1", c)
	res, _ = r.exec.Check(ctx, "host1")
	if !res.OK || res.RunAs == "" {
		t.Errorf("run-as check = %+v", res)
	}
	r.ssh.setSudoRefuses(true)
	res, _ = r.exec.Check(ctx, "host1")
	if res.OK || res.Stage != StageBecome {
		t.Errorf("sudo refusing = %+v", res)
	}
	r.ssh.setSudoRefuses(false)

	// An unpinned target is not logged in to; its key is proposed.
	c = r.conn(1, "ops", "right-password")
	c.PinnedKey = nil
	r.set("host1", c)
	pw0, _ := r.ssh.attempts()
	res, _ = r.exec.Check(ctx, "host1")
	if res.OK || res.Stage != StageHostKey || !res.NewKey || res.Presented == "" {
		t.Errorf("first check = %+v", res)
	}
	if pw1, _ := r.ssh.attempts(); pw1 != pw0 {
		t.Error("a password was sent to a host whose key is not trusted")
	}
	if len(r.reg.proposed[1]) == 0 {
		t.Error("the key was not proposed")
	}

	// A wrong password is its own stage.
	r.set("host1", r.conn(1, "ops", "nope"))
	res, _ = r.exec.Check(ctx, "host1")
	if res.OK || res.Stage != StageAuthenticate || strings.Contains(res.Reason, "nope") {
		t.Errorf("wrong password = %+v", res)
	}
}

// sudo runs programs, not shell builtins: a task taking its input on stdin
// has no bootstrap, and must still run under run-as.
func TestRunTask_RunAsWithStdinInput(t *testing.T) {
	r := newRig(t)
	c := r.conn(1, "ops", "right-password")
	c.RunAs = "deploy"
	r.set("host1", c)
	task := r.task("#!/bin/sh\ncat\n", map[string]any{"a": 1}, 30)
	task.InputMethod = "stdin"
	w := dispatch(r, "host1", task)
	if w.Error != "" || !strings.Contains(w.Result.Output.Stdout, `"a":1`) || w.Result.Output.ExitCode != 0 {
		t.Fatalf("response = %+v %q", w.Result, w.Error)
	}
	if got := strings.TrimSpace(r.ssh.sudoLog()); got != "deploy" {
		t.Errorf("sudo log = %q", got)
	}
}
