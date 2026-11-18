package nodeagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goreleaser/nfpm/v2"
	"github.com/goreleaser/nfpm/v2/files"

	_ "github.com/goreleaser/nfpm/v2/deb"
	_ "github.com/goreleaser/nfpm/v2/rpm"
)

const testBuildTime = "2026-11-11T09:30:00Z"

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// call records one command the Upgrader ran.
type call struct {
	name string
	args []string
}

// upgradeHarness is an Upgrader wired to fakes, plus what they saw.
type upgradeHarness struct {
	*Upgrader
	calls   []call
	spawned []call
	exited  []int
	served  map[string][]byte // url -> body
	// buildInfo is what a candidate reports for --build-info; nil makes
	// it fail to run.
	buildInfo *BuildInfo
	// owner is what dpkg-query / rpm say about the executable.
	dpkgOwns, rpmOwns bool
}

func newUpgradeHarness(t *testing.T, goos string) *upgradeHarness {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "node-agent-client")
	if goos == "windows" {
		exe += ".exe"
	}
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old client"), 0o755); err != nil {
		t.Fatal(err)
	}

	h := &upgradeHarness{served: map[string][]byte{}}
	h.Upgrader = &Upgrader{
		cfg:    Config{AcceptUpgrades: true, UpgradeDir: filepath.Join(dir, "upgrade")},
		goos:   goos,
		goarch: "amd64",
		exe:    exe,
		info:   BuildInfo{Version: "v1", BuildTime: "2026-01-01T00:00:00Z", Digest: digestOf([]byte("old client"))},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		runner: func(_ context.Context, name string, args ...string) (string, string, int, error) {
			h.calls = append(h.calls, call{name, args})
			switch {
			case name == "dpkg-query":
				if h.dpkgOwns {
					return "node-agent-client: " + exe + "\n", "", 0, nil
				}
				return "", "no path found", 1, nil
			case name == "rpm":
				if h.rpmOwns {
					return "node-agent-client-0.1.0-1.x86_64\n", "", 0, nil
				}
				return "file is not owned by any package", "", 1, nil
			case name == "systemd-run":
				return "", "", 0, nil
			case len(args) == 1 && args[0] == "--build-info":
				if h.buildInfo == nil {
					return "", "", 0, errors.New("exec format error")
				}
				out, _ := json.Marshal(h.buildInfo)
				return string(out), "", 0, nil
			}
			return "", "", 0, errors.New("unexpected command " + name)
		},
		download: func(_ context.Context, url string) (io.ReadCloser, error) {
			body, ok := h.served[url]
			if !ok {
				return nil, errors.New("the console answered 404 Not Found")
			}
			return io.NopCloser(bytes.NewReader(body)), nil
		},
		lookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
		isDir:    func(string) bool { return true },
		detached: func(name string, args ...string) error {
			h.spawned = append(h.spawned, call{name, args})
			return nil
		},
		exit: func(code int) { h.exited = append(h.exited, code) },
		pid:  4242,
	}
	return h
}

// request builds an agent_upgrade request for body served at a test
// URL, targeting the build the harness's candidate reports.
func (h *upgradeHarness) request(format string, body []byte) json.RawMessage {
	const url = "http://console.example.com/packages/artifact"
	h.served[url] = body
	target := BuildInfo{Version: "v2", BuildTime: testBuildTime, Digest: digestOf([]byte("new client"))}
	if h.buildInfo == nil {
		h.buildInfo = &target
	}
	raw, _ := json.Marshal(upgradeParams{
		Digest:    target.Digest,
		BuildTime: target.BuildTime,
		Artifact:  upgradeArtifact{URL: url, Size: int64(len(body)), SHA256: digestOf(body), Format: format},
	})
	return raw
}

func decode(t *testing.T, raw []byte) wireResponse {
	t.Helper()
	var resp wireResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("response is not JSON: %v: %s", err, raw)
	}
	return resp
}

func wantError(t *testing.T, raw []byte, contains string) {
	t.Helper()
	resp := decode(t, raw)
	if resp.Error == "" || !strings.Contains(resp.Error, contains) {
		t.Fatalf("error = %q, want one containing %q", resp.Error, contains)
	}
}

func wantStarted(t *testing.T, raw []byte) {
	t.Helper()
	resp := decode(t, raw)
	if resp.Error != "" || resp.Result == nil || resp.Result.Upgrade == nil || !resp.Result.Upgrade.Started {
		t.Fatalf("upgrade was not started: %s", raw)
	}
}

func (h *upgradeHarness) wantUnchanged(t *testing.T) {
	t.Helper()
	got, err := os.ReadFile(h.exe)
	if err != nil || string(got) != "old client" {
		t.Errorf("installed client changed: %q, %v", got, err)
	}
	if _, err := os.Stat(h.cfg.UpgradeDir); !os.IsNotExist(err) {
		t.Errorf("upgrade directory was left behind (stat error: %v)", err)
	}
	if h.TakeAfterReply() != nil {
		t.Error("a restart was scheduled for an upgrade that did not start")
	}
	for _, c := range append(h.calls, h.spawned...) {
		if c.name == "systemd-run" || c.name == "/bin/sh" {
			t.Errorf("an installation was launched: %v", c)
		}
	}
}

// buildTestPackage builds a real .deb or .rpm, the way
// cmd/build-agent-packages does, holding client as the packaged binary.
func buildTestPackage(t *testing.T, format string, client []byte) []byte {
	t.Helper()
	src := filepath.Join(t.TempDir(), "client")
	if err := os.WriteFile(src, client, 0o755); err != nil {
		t.Fatal(err)
	}
	info := nfpm.WithDefaults(&nfpm.Info{
		Name: "node-agent-client", Arch: "amd64", Platform: "linux", Version: "0.1.0",
		Maintainer: "test", Description: "test",
		Overridables: nfpm.Overridables{
			Contents: files.Contents{
				{Source: src, Destination: "/usr/lib/systemd/system/node-agent-client.service"},
				{Source: src, Destination: "/usr/bin/node-agent-client", FileInfo: &files.ContentFileInfo{Mode: 0o755}},
			},
			Deb: nfpm.Deb{Compression: "gzip"},
			RPM: nfpm.RPM{Compression: "gzip"},
		},
	})
	packager, err := nfpm.Get(format)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := packager.Package(info, &buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstallMethodDetection(t *testing.T) {
	for _, c := range []struct {
		name              string
		goos              string
		dpkgOwns, rpmOwns bool
		want              string
	}{
		{"deb package", "linux", true, false, installMethodDeb},
		{"rpm package", "linux", false, true, installMethodRPM},
		{"script-managed Linux binary", "linux", false, false, installMethodUnknown},
		{"macOS", "darwin", false, false, installMethodBinary},
		{"Windows", "windows", false, false, installMethodBinary},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newUpgradeHarness(t, c.goos)
			h.dpkgOwns, h.rpmOwns = c.dpkgOwns, c.rpmOwns
			if got := h.installMethod(context.Background()); got != c.want {
				t.Errorf("installMethod = %q, want %q", got, c.want)
			}
		})
	}
}

func TestInstallMethod_NoPackageManagersPresent(t *testing.T) {
	h := newUpgradeHarness(t, "linux")
	h.dpkgOwns = true
	h.lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if got := h.installMethod(context.Background()); got != installMethodUnknown {
		t.Errorf("installMethod = %q, want unknown when neither dpkg-query nor rpm exists", got)
	}
	if len(h.calls) != 0 {
		t.Errorf("ran %v without the tool being present", h.calls)
	}
}

func TestStatus(t *testing.T) {
	h := newUpgradeHarness(t, "linux")
	h.dpkgOwns = true
	handler := NewHandlerWithUpgrades(nil, "puppet", t.TempDir(), noFileExists, nil, h.Upgrader)

	resp := decode(t, handler(context.Background(), []byte(`{"module":"puppet","action":"agent_status"}`)))
	if resp.Result == nil || resp.Result.Agent == nil {
		t.Fatalf("no agent status in %+v", resp)
	}
	got := *resp.Result.Agent
	want := agentStatus{
		Digest: digestOf([]byte("old client")), BuildTime: "2026-01-01T00:00:00Z", Version: "v1",
		OS: "linux", Arch: "amd64", InstallMethod: "deb", AcceptsUpgrades: true,
	}
	if got != want {
		t.Errorf("status = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(h.cfg.UpgradeDir); !os.IsNotExist(err) {
		t.Error("a status request created the upgrade directory")
	}
}

func TestStatus_UnstampedBuildAndDeclinedUpgrades(t *testing.T) {
	h := newUpgradeHarness(t, "darwin")
	h.info.BuildTime, h.info.Version = "", ""
	h.cfg.AcceptUpgrades = false

	resp := decode(t, h.Status(context.Background()))
	got := resp.Result.Agent
	if got.BuildTime != "" || got.Digest == "" {
		t.Errorf("unstamped build reported buildTime %q, digest %q", got.BuildTime, got.Digest)
	}
	if got.AcceptsUpgrades {
		t.Error("acceptsUpgrades = true on a node that declines upgrades")
	}
}

func TestHandler_AgentActionsUnsupportedWithoutUpgrader(t *testing.T) {
	handler := NewHandler(nil, "puppet", t.TempDir(), noFileExists)
	for _, action := range []string{"agent_status", "agent_upgrade"} {
		wantError(t, handler(context.Background(), []byte(`{"action":"`+action+`"}`)), `unsupported action "`+action+`"`)
	}
}

func TestUpgrade_Declined(t *testing.T) {
	h := newUpgradeHarness(t, "linux")
	h.dpkgOwns = true
	h.cfg.AcceptUpgrades = false
	wantError(t, h.Upgrade(context.Background(), h.request("deb", []byte("x"))), "declined")
	h.wantUnchanged(t)
}

func TestUpgrade_UnknownInstallMethod(t *testing.T) {
	h := newUpgradeHarness(t, "linux")
	wantError(t, h.Upgrade(context.Background(), h.request("deb", []byte("x"))), "re-run the install script")
	h.wantUnchanged(t)
}

func TestUpgrade_ArtifactFormatDoesNotMatchInstall(t *testing.T) {
	h := newUpgradeHarness(t, "linux")
	h.rpmOwns = true
	wantError(t, h.Upgrade(context.Background(), h.request("deb", []byte("x"))), "installed as a rpm")
	h.wantUnchanged(t)
}

func TestUpgrade_AlreadyOnRequestedBuild(t *testing.T) {
	h := newUpgradeHarness(t, "linux")
	h.dpkgOwns = true
	h.info.Digest = digestOf([]byte("new client"))
	wantError(t, h.Upgrade(context.Background(), h.request("deb", []byte("x"))), "already runs")
}

func TestUpgrade_InvalidRequests(t *testing.T) {
	h := newUpgradeHarness(t, "linux")
	h.dpkgOwns = true
	good := digestOf([]byte("x"))
	for name, p := range map[string]upgradeParams{
		"bad digest":   {Digest: "nope", Artifact: upgradeArtifact{URL: "http://c/x", Size: 1, SHA256: good}},
		"bad artifact": {Digest: good, Artifact: upgradeArtifact{URL: "http://c/x", Size: 1, SHA256: "zz"}},
		"zero size":    {Digest: good, Artifact: upgradeArtifact{URL: "http://c/x", Size: 0, SHA256: good}},
		"huge size":    {Digest: good, Artifact: upgradeArtifact{URL: "http://c/x", Size: maxArtifactSize + 1, SHA256: good}},
		"file url":     {Digest: good, Artifact: upgradeArtifact{URL: "file:///etc/passwd", Size: 1, SHA256: good}},
	} {
		raw, _ := json.Marshal(p)
		wantError(t, h.Upgrade(context.Background(), raw), "invalid upgrade request")
		if _, err := os.Stat(h.cfg.UpgradeDir); !os.IsNotExist(err) {
			t.Errorf("%s: the upgrade directory was created", name)
		}
	}
}

func TestUpgrade_DownloadFails(t *testing.T) {
	h := newUpgradeHarness(t, "linux")
	h.dpkgOwns = true
	raw := h.request("deb", []byte("x"))
	h.served = map[string][]byte{}
	wantError(t, h.Upgrade(context.Background(), raw), "could not download")
	h.wantUnchanged(t)
}

func TestUpgrade_DigestMismatch(t *testing.T) {
	h := newUpgradeHarness(t, "linux")
	h.dpkgOwns = true
	raw := h.request("deb", []byte("genuine"))
	for url := range h.served {
		h.served[url] = []byte("tamperd") // same length, different bytes
	}
	wantError(t, h.Upgrade(context.Background(), raw), "does not match the request: digest")
	h.wantUnchanged(t)
}

func TestUpgrade_SizeMismatch(t *testing.T) {
	for name, body := range map[string][]byte{"oversized": []byte("genuine and then some"), "truncated": []byte("gen")} {
		t.Run(name, func(t *testing.T) {
			h := newUpgradeHarness(t, "linux")
			h.dpkgOwns = true
			raw := h.request("deb", []byte("genuine"))
			for url := range h.served {
				h.served[url] = body
			}
			wantError(t, h.Upgrade(context.Background(), raw), "does not match the request")
			h.wantUnchanged(t)
		})
	}
}

func TestFetchVerified_NeverReadsPastTheDeclaredSize(t *testing.T) {
	h := newUpgradeHarness(t, "linux")
	if err := makePrivateDir(h.cfg.UpgradeDir); err != nil {
		t.Fatal(err)
	}
	h.served["http://c/big"] = bytes.Repeat([]byte("x"), 1<<20)
	dst := filepath.Join(h.cfg.UpgradeDir, "artifact")
	err := h.fetchVerified(context.Background(), upgradeArtifact{URL: "http://c/big", Size: 100, SHA256: digestOf([]byte("x"))}, dst)
	if err == nil {
		t.Fatal("an oversized body was accepted")
	}
	if st, statErr := os.Stat(dst); statErr != nil || st.Size() > 101 {
		t.Errorf("wrote %d bytes for a 100-byte artifact", st.Size())
	}
}

func TestMakePrivateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "node-agent", "upgrade")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "planted"), []byte("x"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := makePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dir)
	if err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("mode = %v, %v; want 0700", st.Mode().Perm(), err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a file planted beforehand survived: %v", entries)
	}
}

func TestExtractPackagedBinary(t *testing.T) {
	client := bytes.Repeat([]byte("a real client binary "), 5000)
	for _, format := range []string{"deb", "rpm"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			pkg := filepath.Join(dir, "pkg")
			if err := os.WriteFile(pkg, buildTestPackage(t, format, client), 0o600); err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(dir, "out")
			if err := extractPackagedBinary(format, pkg, dst); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(dst)
			if !bytes.Equal(got, client) {
				t.Errorf("extracted %d bytes that are not the packaged client (%d bytes)", len(got), len(client))
			}
			if st, _ := os.Stat(dst); st.Mode().Perm() != 0o700 {
				t.Errorf("extracted mode = %v, want 0700", st.Mode().Perm())
			}
		})
	}
}

func TestExtractPackagedBinary_NotAPackage(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "pkg")
	if err := os.WriteFile(pkg, []byte("this is not a package at all, just some text"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"deb", "rpm"} {
		if err := extractPackagedBinary(format, pkg, filepath.Join(dir, "out-"+format)); err == nil {
			t.Errorf("%s: unpacked something that is not a package", format)
		}
	}
}

func TestUpgrade_PackageIsNotAPackage(t *testing.T) {
	h := newUpgradeHarness(t, "linux")
	h.dpkgOwns = true
	wantError(t, h.Upgrade(context.Background(), h.request("deb", []byte("not a deb"))), "could not be unpacked")
	h.wantUnchanged(t)
}

func TestUpgrade_CandidateDoesNotRun(t *testing.T) {
	h := newUpgradeHarness(t, "linux")
	h.dpkgOwns = true
	raw := h.request("deb", buildTestPackage(t, "deb", []byte("new client")))
	h.buildInfo = nil
	wantError(t, h.Upgrade(context.Background(), raw), "does not run on this node")
	h.wantUnchanged(t)
}

func TestUpgrade_CandidateIsTheWrongBuild(t *testing.T) {
	h := newUpgradeHarness(t, "linux")
	h.dpkgOwns = true
	h.buildInfo = &BuildInfo{Version: "v3", BuildTime: "2027-01-01T00:00:00Z", Digest: digestOf([]byte("some other client"))}
	wantError(t, h.Upgrade(context.Background(), h.request("deb", buildTestPackage(t, "deb", []byte("new client")))), "not the requested build")
	h.wantUnchanged(t)
}

func TestUpgrade_LinuxPackageUnderSystemd(t *testing.T) {
	for format, install := range map[string]string{"deb": `dpkg -i "$1"`, "rpm": `rpm -U --oldpackage --replacepkgs "$1"`} {
		t.Run(format, func(t *testing.T) {
			h := newUpgradeHarness(t, "linux")
			h.dpkgOwns, h.rpmOwns = format == "deb", format == "rpm"
			wantStarted(t, h.Upgrade(context.Background(), h.request(format, buildTestPackage(t, format, []byte("new client")))))

			var run *call
			for i := range h.calls {
				if h.calls[i].name == "systemd-run" {
					run = &h.calls[i]
				}
			}
			if run == nil {
				t.Fatalf("systemd-run was not used: %v", h.calls)
			}
			args := strings.Join(run.args, " ")
			// Its own unit, so the service restart the package performs
			// does not take the package manager down with it.
			for _, want := range []string{"--unit=node-agent-client-upgrade", "--collect", "/bin/sh -c", install} {
				if !strings.Contains(args, want) {
					t.Errorf("systemd-run arguments lack %q: %s", want, args)
				}
			}
			n := len(run.args)
			if run.args[n-3] != filepath.Join(h.cfg.UpgradeDir, "artifact") || run.args[n-2] != h.cfg.UpgradeDir || run.args[n-1] != "" {
				t.Errorf("installer got artifact/dir/pid = %q", run.args[n-3:])
			}
			if len(h.spawned) != 0 {
				t.Errorf("also launched a detached process: %v", h.spawned)
			}

			// The verified package is still there for the installer, the
			// client itself was not touched, and it now waits to be
			// restarted instead of exiting.
			if _, err := os.Stat(filepath.Join(h.cfg.UpgradeDir, "artifact")); err != nil {
				t.Errorf("the package was removed before the installer could use it: %v", err)
			}
			if got, _ := os.ReadFile(h.exe); string(got) != "old client" {
				t.Error("the client replaced its own executable on a package install")
			}
			if h.TakeAfterReply() == nil {
				t.Error("nothing holds the client busy while the package manager runs")
			}
			if len(h.exited) != 0 {
				t.Errorf("the client exited (%v) instead of waiting for the package's restart", h.exited)
			}
		})
	}
}

func TestUpgrade_LinuxPackageWithoutSystemd(t *testing.T) {
	h := newUpgradeHarness(t, "linux")
	h.dpkgOwns = true
	h.isDir = func(string) bool { return false }
	wantStarted(t, h.Upgrade(context.Background(), h.request("deb", buildTestPackage(t, "deb", []byte("new client")))))

	if len(h.spawned) != 1 || h.spawned[0].name != "/bin/sh" {
		t.Fatalf("detached = %v, want one /bin/sh", h.spawned)
	}
	args := h.spawned[0].args
	if args[len(args)-1] != "4242" {
		t.Errorf("the installer was not given the client's pid to stop afterwards: %q", args)
	}
	for _, c := range h.calls {
		if c.name == "systemd-run" {
			t.Error("systemd-run was used on a node without systemd")
		}
	}
}

func TestUpgrade_InstallerCannotBeLaunched(t *testing.T) {
	h := newUpgradeHarness(t, "linux")
	h.dpkgOwns = true
	h.isDir = func(string) bool { return false }
	h.detached = func(string, ...string) error { return errors.New("fork failed") }
	wantError(t, h.Upgrade(context.Background(), h.request("deb", buildTestPackage(t, "deb", []byte("new client")))), "could not start the package installation")
	if _, err := os.Stat(h.cfg.UpgradeDir); !os.IsNotExist(err) {
		t.Error("upgrade directory left behind")
	}
}

func TestPackageInstallScript_IsValidShell(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	for _, method := range []string{installMethodDeb, installMethodRPM} {
		_, stderr, code, err := RunCommand(context.Background(), "/bin/sh", "-n", "-c", packageInstallScript(method))
		if err != nil || code != 0 {
			t.Errorf("%s script does not parse: %v %s", method, err, stderr)
		}
	}
}

func TestUpgrade_BinaryInstall(t *testing.T) {
	for goos, wantExit := range map[string]int{"darwin": 0, "windows": 1} {
		t.Run(goos, func(t *testing.T) {
			h := newUpgradeHarness(t, goos)
			wantStarted(t, h.Upgrade(context.Background(), h.request("binary", []byte("new client"))))

			if got, _ := os.ReadFile(h.exe); string(got) != "new client" {
				t.Errorf("installed client = %q, want the new one", got)
			}
			old, err := os.ReadFile(h.exe + ".old")
			if goos == "windows" {
				if err != nil || string(old) != "old client" {
					t.Errorf("the running executable was not moved aside: %q, %v", old, err)
				}
			} else if !os.IsNotExist(err) {
				t.Error("a .old file was left on a platform that does not need one")
			}
			if _, err := os.Stat(h.cfg.UpgradeDir); !os.IsNotExist(err) {
				t.Error("the staged artifact was left behind")
			}
			entries, _ := os.ReadDir(filepath.Dir(h.exe))
			for _, e := range entries {
				if strings.Contains(e.Name(), ".new") {
					t.Errorf("staging file %s left beside the client", e.Name())
				}
			}

			// The restart happens only after the reply has gone.
			if len(h.exited) != 0 {
				t.Fatal("the client exited before its reply was sent")
			}
			after := h.TakeAfterReply()
			if after == nil {
				t.Fatal("no restart scheduled")
			}
			after(context.Background())
			if len(h.exited) != 1 || h.exited[0] != wantExit {
				t.Errorf("exit codes = %v, want [%d]", h.exited, wantExit)
			}
			if h.TakeAfterReply() != nil {
				t.Error("the restart can be taken twice")
			}

			h.CleanupAfterUpgrade()
			if _, err := os.Stat(h.exe + ".old"); !os.IsNotExist(err) {
				t.Error("the previous executable survived the next start")
			}
		})
	}
}

func TestUpgrade_BinaryThatDoesNotRunIsNotInstalled(t *testing.T) {
	for _, goos := range []string{"darwin", "windows"} {
		h := newUpgradeHarness(t, goos)
		raw := h.request("binary", []byte("new client"))
		h.buildInfo = nil
		wantError(t, h.Upgrade(context.Background(), raw), "does not run on this node")
		h.wantUnchanged(t)
		entries, _ := os.ReadDir(filepath.Dir(h.exe))
		if len(entries) != 1 {
			t.Errorf("%s: files left beside the client: %v", goos, entries)
		}
	}
}

func TestReplaceExecutable_WindowsPutsThePreviousClientBack(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "node-agent-client.exe")
	if err := os.WriteFile(exe, []byte("old client"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The candidate does not exist, so moving it into place fails.
	if err := replaceExecutable("windows", exe, filepath.Join(dir, "missing.exe")); err == nil {
		t.Fatal("replaceExecutable succeeded with no candidate")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old client" {
		t.Errorf("the installed client was not restored: %q", got)
	}
}

func TestLoadConfig_UpgradeSettings(t *testing.T) {
	cfg, err := loadConfig(requiredAgentEnv(nil), "linux")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AcceptUpgrades || cfg.UpgradeDir != "/opt/puppetlabs/node-agent/upgrade" {
		t.Errorf("defaults: AcceptUpgrades=%v UpgradeDir=%q", cfg.AcceptUpgrades, cfg.UpgradeDir)
	}

	cfg, _ = loadConfig(requiredAgentEnv(map[string]string{"ProgramData": `D:\ProgramData`}), "windows")
	if cfg.UpgradeDir != `D:\ProgramData\PuppetLabs\node-agent\upgrade` {
		t.Errorf("Windows UpgradeDir = %q", cfg.UpgradeDir)
	}

	for value, want := range map[string]bool{"false": false, "FALSE": false, "0": false, "no": false, "off": false, "true": true, "1": true, "": true, "flase": true} {
		cfg, _ = loadConfig(requiredAgentEnv(map[string]string{"NODE_AGENT_ACCEPT_UPGRADES": value}), "linux")
		if cfg.AcceptUpgrades != want {
			t.Errorf("NODE_AGENT_ACCEPT_UPGRADES=%q: AcceptUpgrades = %v, want %v", value, cfg.AcceptUpgrades, want)
		}
	}

	cfg, _ = loadConfig(requiredAgentEnv(map[string]string{"NODE_AGENT_UPGRADE_DIR": "/var/tmp/up"}), "linux")
	if cfg.UpgradeDir != "/var/tmp/up" {
		t.Errorf("explicit UpgradeDir = %q", cfg.UpgradeDir)
	}
}
