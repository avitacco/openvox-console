package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runPostrm runs the package's removal script with arg, against a fake
// systemctl that records how it was called and an environment file the
// script would delete, and reports what happened to both.
func runPostrm(t *testing.T, arg string) (systemctlCalls string, envFileRemains bool) {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	fake := "#!/bin/sh\necho \"$@\" >> " + calls + "\n"
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}

	// The script names the environment file by absolute path, so point
	// a copy of it at a file under the test's directory instead.
	source, err := os.ReadFile(filepath.Join("..", "..", assetsDir, "postrm.sh"))
	if err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(dir, "environment")
	if err := os.WriteFile(envFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	const realEnvFile = "/etc/node-agent-client/environment"
	if !strings.Contains(string(source), realEnvFile) {
		t.Fatalf("postrm.sh no longer names %s; update this test", realEnvFile)
	}
	script := filepath.Join(dir, "postrm.sh")
	if err := os.WriteFile(script, []byte(strings.ReplaceAll(string(source), realEnvFile, envFile)), 0o755); err != nil {
		t.Fatal(err)
	}

	args := []string{script}
	if arg != "" {
		args = append(args, arg)
	}
	cmd := exec.Command("sh", args...)
	cmd.Env = []string{"PATH=" + dir + ":" + os.Getenv("PATH")}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("postrm.sh %s: %v\n%s", arg, err, out)
	}

	recorded, _ := os.ReadFile(calls)
	_, statErr := os.Stat(envFile)
	return string(recorded), statErr == nil
}

// Both package managers run the removal script while upgrading. It must
// leave the service and its environment file alone then: on rpm it runs
// after the new package has already restarted the service.
func TestPostrm_LeavesServiceAloneOnUpgrade(t *testing.T) {
	upgrades := map[string]string{
		"dpkg upgrade":        "upgrade",
		"dpkg failed-upgrade": "failed-upgrade",
		"dpkg abort-install":  "abort-install",
		"rpm upgrade, arg 1":  "1",
		"rpm upgrade, arg 2":  "2",
		"no argument":         "",
	}
	for name, arg := range upgrades {
		t.Run(name, func(t *testing.T) {
			calls, envRemains := runPostrm(t, arg)
			if calls != "" {
				t.Errorf("systemctl was called during an upgrade: %q", calls)
			}
			if !envRemains {
				t.Error("the environment file was deleted during an upgrade")
			}
		})
	}
}

func TestPostrm_TearsDownOnRealRemoval(t *testing.T) {
	removals := map[string]string{
		"dpkg remove":          "remove",
		"dpkg purge":           "purge",
		"rpm uninstall, arg 0": "0",
	}
	for name, arg := range removals {
		t.Run(name, func(t *testing.T) {
			calls, envRemains := runPostrm(t, arg)
			if !strings.Contains(calls, "stop node-agent-client") || !strings.Contains(calls, "disable node-agent-client") {
				t.Errorf("systemctl calls = %q, want the service stopped and disabled", calls)
			}
			if envRemains {
				t.Error("the environment file was left behind on removal")
			}
		})
	}
}

// An rpm must restart the service at the end of the transaction, after
// the old package's %postun, not in %post before it.
func TestScripts_RpmInstallsInPostTransDebInPostinst(t *testing.T) {
	deb := packageScripts("deb")
	if !strings.HasSuffix(deb.PostInstall, "postinst.sh") || !strings.HasSuffix(deb.PostRemove, "postrm.sh") {
		t.Errorf("deb scripts = %+v, want postinst.sh and postrm.sh", deb)
	}
	if got := rpmScripts("deb"); got.PostTrans != "" {
		t.Errorf("deb carries an rpm posttrans script: %q", got.PostTrans)
	}

	rpm := packageScripts("rpm")
	if rpm.PostInstall != "" {
		t.Errorf("rpm %%post = %q, want none: it would run before the old package's %%postun", rpm.PostInstall)
	}
	if !strings.HasSuffix(rpm.PostRemove, "postrm.sh") {
		t.Errorf("rpm %%postun = %q, want postrm.sh", rpm.PostRemove)
	}
	if got := rpmScripts("rpm"); !strings.HasSuffix(got.PostTrans, "postinst.sh") {
		t.Errorf("rpm %%posttrans = %q, want postinst.sh", got.PostTrans)
	}
}
