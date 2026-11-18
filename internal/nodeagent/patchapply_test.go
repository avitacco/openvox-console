package nodeagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeApt is an apt node: a package list that an install empties.
type fakeApt struct {
	mu          sync.Mutex
	pending     []string // sim lines still pending
	installCode int
	reboot      bool
	calls       []string
}

func (f *fakeApt) run(_ context.Context, name string, args ...string) (string, string, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	switch {
	case strings.Contains(call, "apt-get update"):
		return "", "", 0, nil
	case strings.Contains(call, "-s -q dist-upgrade"):
		return "NOTE: This is only a simulation!\n" + strings.Join(f.pending, "\n") + "\n", "", 0, nil
	case strings.HasPrefix(call, "/usr/bin/env DEBIAN_FRONTEND=noninteractive"):
		if f.installCode != 0 {
			return "partial", "E: broken", f.installCode, nil
		}
		keep := f.pending[:0]
		for _, l := range f.pending {
			name := strings.Fields(l)[1]
			installed := strings.Contains(call, "dist-upgrade") || strings.Contains(call, " "+name)
			if !installed {
				keep = append(keep, l)
			}
		}
		f.pending = keep
		return "done", "", 0, nil
	}
	return "", "", 0, nil
}

func (f *fakeApt) called(sub string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

const (
	libssl = "Inst libssl3 [3.0.2-0ubuntu1.10] (3.0.2-0ubuntu1.12 Ubuntu:22.04/jammy-updates, Ubuntu:22.04/jammy-security [amd64])"
	curl   = "Inst curl [7.81.0-1ubuntu1.14] (7.81.0-1ubuntu1.15 Ubuntu:22.04/jammy-updates [amd64])"
)

func newTestPatcher(t *testing.T, f *fakeApt) *Patcher {
	t.Helper()
	exists := func(path string) bool {
		return path == aptGet || (path == "/var/run/reboot-required" && f.reboot)
	}
	p := NewPatcher(f.run, exists, filepath.Join(t.TempDir(), "patch-run.json"), nil)
	p.grace = 0
	p.sleep = func(context.Context, time.Duration) {}
	return p
}

func apply(t *testing.T, p *Patcher, req patchApplyParams) *patchRunStatus {
	t.Helper()
	raw, _ := json.Marshal(req)
	resp := scanResult(t, p.Apply(context.Background(), raw))
	if resp.Error != "" {
		t.Fatalf("Apply refused: %s", resp.Error)
	}
	if resp.Result == nil || resp.Result.PatchRun == nil || resp.Result.PatchRun.State != patchRunning {
		t.Fatalf("Apply reply = %+v, want a running run", resp.Result)
	}
	// What the client does once the reply is sent.
	p.TakeAfterReply()(context.Background())
	return statusOf(t, p)
}

func statusOf(t *testing.T, p *Patcher) *patchRunStatus {
	t.Helper()
	resp := scanResult(t, p.Status(context.Background()))
	if resp.Result == nil || resp.Result.PatchRun == nil {
		t.Fatalf("status = %+v", resp)
	}
	return resp.Result.PatchRun
}

func outcomes(st *patchRunStatus) map[string]string {
	m := map[string]string{}
	for _, r := range st.Packages {
		m[r.Name] = r.Outcome
	}
	return m
}

func TestPatchApply_SecurityOnlyInstallsOnlySecurity(t *testing.T) {
	f := &fakeApt{pending: []string{libssl, curl}}
	st := apply(t, newTestPatcher(t, f), patchApplyParams{Scope: "security"})
	if st.State != patchDone {
		t.Fatalf("state = %s (%s)", st.State, st.Error)
	}
	if !f.called("install --only-upgrade libssl3") || f.called("dist-upgrade -y") {
		t.Errorf("calls = %v", f.calls)
	}
	if f.called("--only-upgrade libssl3 curl") {
		t.Error("a non-security package was installed")
	}
	got := outcomes(st)
	if len(got) != 1 || got["libssl3"] != pkgUpgraded {
		t.Errorf("outcomes = %v", got)
	}
	if st.Packages[0].From != "3.0.2-0ubuntu1.10" || st.Packages[0].To != "3.0.2-0ubuntu1.12" {
		t.Errorf("versions = %+v", st.Packages[0])
	}
	// The scan taken afterwards is what is still pending.
	if st.Scan == nil || len(st.Scan.Updates) != 1 || st.Scan.Updates[0].Name != "curl" {
		t.Errorf("scan = %+v", st.Scan)
	}
}

func TestPatchApply_AllUsesDistUpgrade(t *testing.T) {
	f := &fakeApt{pending: []string{libssl, curl}}
	st := apply(t, newTestPatcher(t, f), patchApplyParams{Scope: "all"})
	if st.State != patchDone || !f.called("DEBIAN_FRONTEND=noninteractive /usr/bin/apt-get -y -q") || !f.called("dist-upgrade") {
		t.Errorf("state %s, calls %v", st.State, f.calls)
	}
	if len(st.Scan.Updates) != 0 {
		t.Errorf("still pending: %+v", st.Scan.Updates)
	}
}

func TestPatchApply_DryRunInstallsNothing(t *testing.T) {
	f := &fakeApt{pending: []string{libssl, curl}, reboot: true}
	st := apply(t, newTestPatcher(t, f), patchApplyParams{Scope: "all", DryRun: true})
	if st.State != patchDone {
		t.Fatalf("state = %s", st.State)
	}
	if f.called("DEBIAN_FRONTEND") || f.called("reboot") {
		t.Errorf("a dry run changed the node: %v", f.calls)
	}
	if got := outcomes(st); got["libssl3"] != pkgWouldUpgrade || got["curl"] != pkgWouldUpgrade {
		t.Errorf("outcomes = %v", got)
	}
}

func TestPatchApply_NamedPackageWithNothingPending(t *testing.T) {
	f := &fakeApt{pending: []string{curl}}
	st := apply(t, newTestPatcher(t, f), patchApplyParams{Scope: "packages", Packages: []string{"vim"}})
	if st.State != patchDone || outcomes(st)["vim"] != pkgNothing || f.called("DEBIAN_FRONTEND") {
		t.Errorf("state %s, outcomes %v, calls %v", st.State, outcomes(st), f.calls)
	}
}

func TestPatchApply_PackageManagerFailure(t *testing.T) {
	f := &fakeApt{pending: []string{libssl}, installCode: 100}
	st := apply(t, newTestPatcher(t, f), patchApplyParams{Scope: "all"})
	if st.State != patchFailed || st.ExitCode != 100 || !strings.Contains(st.Output, "E: broken") || !strings.Contains(st.Error, "100") {
		t.Errorf("status = %+v", st)
	}
	if outcomes(st)["libssl3"] != pkgNotUpgraded {
		t.Errorf("outcomes = %v", outcomes(st))
	}
}

func TestPatchApply_Reboot(t *testing.T) {
	cases := []struct {
		name   string
		req    patchApplyParams
		reboot bool
		code   int
		want   bool
	}{
		{"asked and required", patchApplyParams{Scope: "all", Reboot: true}, true, 0, true},
		{"not asked", patchApplyParams{Scope: "all"}, true, 0, false},
		{"not required", patchApplyParams{Scope: "all", Reboot: true}, false, 0, false},
		{"failed run", patchApplyParams{Scope: "all", Reboot: true}, true, 100, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeApt{pending: []string{libssl}, reboot: c.reboot, installCode: c.code}
			st := apply(t, newTestPatcher(t, f), c.req)
			if f.called("reboot") != c.want || st.Rebooting != c.want {
				t.Errorf("rebooted %v, status rebooting %v, want %v", f.called("reboot"), st.Rebooting, c.want)
			}
			if c.reboot && c.code == 0 && !c.req.Reboot && !st.RebootRequired {
				t.Error("a needed reboot is not reported")
			}
		})
	}
}

func TestPatchApply_SecurityOnDnf(t *testing.T) {
	var calls []string
	runner := func(_ context.Context, name string, args ...string) (string, string, int, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch {
		case strings.Contains(call, "check-update"):
			return "openssl.x86_64  1:3.0.7-25.el9_3  baseos\n", "", 100, nil
		case strings.Contains(call, "updateinfo"):
			return "ALSA-2024:1 Moderate/Sec.  openssl-1:3.0.7-25.el9_3.x86_64\n", "", 0, nil
		}
		return "", "", 0, nil
	}
	p := NewPatcher(runner, has(dnfBin), "", nil)
	p.sleep = func(context.Context, time.Duration) {}
	raw, _ := json.Marshal(patchApplyParams{Scope: "security"})
	p.Apply(context.Background(), raw)
	p.TakeAfterReply()(context.Background())
	if !strings.Contains(strings.Join(calls, "\n"), dnfBin+" -y -q upgrade --security") {
		t.Errorf("calls = %v", calls)
	}
}

func TestPatchApply_RefusesBadRequests(t *testing.T) {
	p := newTestPatcher(t, &fakeApt{})
	for name, req := range map[string]patchApplyParams{
		"bad scope":   {Scope: "everything"},
		"no packages": {Scope: "packages"},
		"option-like": {Scope: "packages", Packages: []string{"--allow-downgrades"}},
		"dry reboot":  {Scope: "all", DryRun: true, Reboot: true},
		"shell word":  {Scope: "packages", Packages: []string{"curl;reboot"}},
	} {
		raw, _ := json.Marshal(req)
		if resp := scanResult(t, p.Apply(context.Background(), raw)); resp.Error == "" {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestPatchApply_UnsupportedNode(t *testing.T) {
	p := NewPatcher((&fakeApt{}).run, has(), "", nil)
	raw, _ := json.Marshal(patchApplyParams{Scope: "all"})
	if resp := scanResult(t, p.Apply(context.Background(), raw)); !strings.Contains(resp.Error, "no package manager") {
		t.Errorf("error = %q", resp.Error)
	}
}

func TestPatchApply_RefusesASecondRunWhileOneIsRunning(t *testing.T) {
	p := newTestPatcher(t, &fakeApt{pending: []string{curl}})
	raw, _ := json.Marshal(patchApplyParams{Scope: "all"})
	p.Apply(context.Background(), raw) // accepted, not yet run
	if resp := scanResult(t, p.Apply(context.Background(), raw)); !strings.Contains(resp.Error, "busy") {
		t.Errorf("second request error = %q", resp.Error)
	}
	if st := statusOf(t, p); st.State != patchRunning {
		t.Errorf("status = %s", st.State)
	}
}

func TestPatcher_ARestartMidRunIsReportedInterrupted(t *testing.T) {
	file := filepath.Join(t.TempDir(), "patch-run.json")
	data, _ := json.Marshal(patchRunStatus{State: patchRunning, Phase: "installing"})
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	p := NewPatcher((&fakeApt{}).run, has(aptGet), file, nil)
	if st := statusOf(t, p); st.State != patchInterrupted || st.Error == "" {
		t.Errorf("status = %+v", st)
	}
	// A finished run survives a restart as it was.
	data, _ = json.Marshal(patchRunStatus{State: patchDone})
	_ = os.WriteFile(file, data, 0o600)
	if st := statusOf(t, NewPatcher((&fakeApt{}).run, has(aptGet), file, nil)); st.State != patchDone {
		t.Errorf("status = %s", st.State)
	}
}

// Over the real transport: apply answers at once, the run goes on with the
// client busy, status is answered meanwhile, and a second apply is refused.
func TestPatchApply_OverTheTransport(t *testing.T) {
	transport, ca := startTestTransport(t)
	certPath, keyPath := ca.Issue(t, "web09.example.com", false)

	release := make(chan struct{})
	installing := make(chan struct{})
	var once sync.Once
	base := &fakeApt{pending: []string{libssl}}
	runner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		if strings.HasPrefix(name+" "+strings.Join(args, " "), "/usr/bin/env DEBIAN_FRONTEND") {
			once.Do(func() { close(installing) })
			<-release
		}
		return base.run(ctx, name, args...)
	}
	p := NewPatcher(runner, func(path string) bool { return path == aptGet }, "", nil)
	handler := NewHandlerWithPatches(RunCommand, "puppet", t.TempDir(), FileExists, nil, nil, p)
	client := New(Config{TransportAddr: transport.Addr(), CertFile: certPath, KeyFile: keyPath, CAFile: ca.PEMFile(t)}, handler, nil)
	client.SetAfterReply(p.TakeAfterReply)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go client.Run(ctx)

	dispatch := func(action string, params any) wireResponse {
		var raw json.RawMessage
		if params != nil {
			raw, _ = json.Marshal(params)
		}
		resp, err := transport.Dispatch(context.Background(), "web09.example.com", requestPayload(t, "puppet", action, raw), 5*time.Second)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		return decodeWireResponse(t, resp)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := transport.Dispatch(context.Background(), "web09.example.com", requestPayload(t, "puppet", actionPatchStatus, nil), time.Second); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	begun := time.Now()
	if wr := dispatch(actionPatchApply, patchApplyParams{Scope: "all"}); wr.Error != "" || wr.Result.PatchRun.State != patchRunning {
		t.Fatalf("apply = %+v %q", wr.Result, wr.Error)
	}
	if time.Since(begun) > 2*time.Second {
		t.Error("apply was not answered at once")
	}
	select {
	case <-installing:
	case <-time.After(3 * time.Second):
		t.Fatal("the run never reached the install")
	}
	if wr := dispatch(actionPatchStatus, nil); wr.Error != "" || wr.Result.PatchRun.State != patchRunning || wr.Result.PatchRun.Phase != "installing" {
		t.Errorf("status during the run = %+v %q", wr.Result, wr.Error)
	}
	if wr := dispatch(actionPatchApply, patchApplyParams{Scope: "all"}); !strings.Contains(wr.Error, "busy") {
		t.Errorf("a second apply = %q, want a busy refusal", wr.Error)
	}
	close(release)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if wr := dispatch(actionPatchStatus, nil); wr.Result != nil && wr.Result.PatchRun.State == patchDone {
			return
		}
	}
	t.Fatal("the run never finished")
}
