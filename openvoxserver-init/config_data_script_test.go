// Package openvoxserverinit holds only tests: they run the boot scripts
// in this directory against scratch directories.
package openvoxserverinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type env struct {
	t     *testing.T
	root  string
	hiera string
	gems  string
	src   string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	root := t.TempDir()
	e := &env{t: t, root: root, hiera: filepath.Join(root, "puppet", "hiera.yaml"), gems: filepath.Join(root, "gems"), src: filepath.Join(root, "console_config_data.rb")}
	if err := os.WriteFile(e.src, []byte("# the function\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) gem(version string) string {
	e.t.Helper()
	dir := filepath.Join(e.gems, "openvox-"+version, "lib", "puppet", "functions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	return dir
}

// run runs the script and returns what it printed. It must always exit 0:
// a console without this feature still has to start.
func (e *env) run() string {
	e.t.Helper()
	cmd := exec.Command("sh", "30-configure-config-data.sh")
	cmd.Env = append(os.Environ(),
		"CONFIG_DATA_FUNCTION_SRC="+e.src, "CONFIG_DATA_JRUBY_GEMS="+e.gems, "CONFIG_DATA_HIERA="+e.hiera)
	out, err := cmd.CombinedOutput()
	if err != nil {
		e.t.Fatalf("script failed: %v\n%s", err, out)
	}
	return string(out)
}

func (e *env) read(path string) string {
	e.t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func TestFirstStart_WritesHieraAndInstallsTheFunction(t *testing.T) {
	e := newEnv(t)
	fdir := e.gem("8.29.0")

	out := e.run()

	if !strings.Contains(out, "wrote "+e.hiera) {
		t.Errorf("output = %q, want it to say it wrote hiera.yaml", out)
	}
	got := e.read(e.hiera)
	for _, want := range []string{"# Managed by OpenVox Console", "version: 5", "data_hash: console_config_data"} {
		if !strings.Contains(got, want) {
			t.Errorf("hiera.yaml lacks %q:\n%s", want, got)
		}
	}
	if first, _, _ := strings.Cut(got, "\n"); first != "# Managed by OpenVox Console" {
		t.Errorf("the first line is %q: the marker must be first", first)
	}
	if e.read(filepath.Join(fdir, "console_config_data.rb")) != "# the function\n" {
		t.Error("the function was not installed")
	}
}

func TestAnOperatorsHieraIsLeftAlone(t *testing.T) {
	e := newEnv(t)
	e.gem("8.29.0")
	mine := "---\nversion: 5\nhierarchy:\n  - name: mine\n    path: mine.yaml\n"
	if err := os.MkdirAll(filepath.Dir(e.hiera), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.hiera, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	out := e.run()

	if e.read(e.hiera) != mine {
		t.Errorf("an operator's hiera.yaml was changed:\n%s", e.read(e.hiera))
	}
	if !strings.Contains(out, "data_hash: console_config_data") || !strings.Contains(out, "left alone") {
		t.Errorf("output = %q, want it to name the entry to add", out)
	}
	// Still installed: the operator only has to add the entry.
	if _, err := os.Stat(filepath.Join(e.gems, "openvox-8.29.0", "lib", "puppet", "functions", "console_config_data.rb")); err != nil {
		t.Errorf("the function was not installed: %v", err)
	}
}

func TestAnOperatorsHieraThatAlreadyNamesTheFunctionIsSilent(t *testing.T) {
	e := newEnv(t)
	e.gem("8.29.0")
	mine := "---\nversion: 5\nhierarchy:\n  - name: c\n    data_hash: console_config_data\n  - name: x\n    path: x.yaml\n"
	_ = os.MkdirAll(filepath.Dir(e.hiera), 0o755)
	_ = os.WriteFile(e.hiera, []byte(mine), 0o644)

	out := e.run()

	if e.read(e.hiera) != mine {
		t.Error("the file was changed")
	}
	if strings.Contains(out, "WARNING") || strings.Contains(out, "wrote") {
		t.Errorf("output = %q, want nothing to say", out)
	}
}

func TestASecondRunChangesNothing(t *testing.T) {
	e := newEnv(t)
	fdir := e.gem("8.29.0")
	e.run()
	hieraBefore := e.read(e.hiera)
	hieraInfo, _ := os.Stat(e.hiera)
	fnInfo, _ := os.Stat(filepath.Join(fdir, "console_config_data.rb"))

	out := e.run()

	if strings.TrimSpace(out) != "" {
		t.Errorf("second run printed %q, want nothing", out)
	}
	if e.read(e.hiera) != hieraBefore {
		t.Error("hiera.yaml changed")
	}
	hieraAfter, _ := os.Stat(e.hiera)
	fnAfter, _ := os.Stat(filepath.Join(fdir, "console_config_data.rb"))
	if !hieraAfter.ModTime().Equal(hieraInfo.ModTime()) || !fnAfter.ModTime().Equal(fnInfo.ModTime()) {
		t.Error("a file was rewritten although nothing changed")
	}
}

func TestARecreatedContainerGetsTheFunctionAgain(t *testing.T) {
	e := newEnv(t)
	fdir := e.gem("8.29.0")
	e.run()
	// A recreate loses the gem directory's contents; hiera.yaml (in a
	// restarted container) stays.
	if err := os.Remove(filepath.Join(fdir, "console_config_data.rb")); err != nil {
		t.Fatal(err)
	}
	e.run()
	if e.read(filepath.Join(fdir, "console_config_data.rb")) != "# the function\n" {
		t.Error("the function was not installed again")
	}
}

func TestAChangedFunctionIsReplacedAndAVersionBumpIsFollowed(t *testing.T) {
	e := newEnv(t)
	old := e.gem("8.29.0")
	e.run()
	if err := os.WriteFile(e.src, []byte("# a newer function\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bumped := e.gem("8.30.1")
	e.run()
	for _, dir := range []string{old, bumped} {
		if e.read(filepath.Join(dir, "console_config_data.rb")) != "# a newer function\n" {
			t.Errorf("%s does not have the current function", dir)
		}
	}
}

func TestOurOwnHieraIsRefreshedAndNoGemIsNotFatal(t *testing.T) {
	e := newEnv(t)
	_ = os.MkdirAll(filepath.Dir(e.hiera), 0o755)
	_ = os.WriteFile(e.hiera, []byte("# Managed by OpenVox Console\n---\nversion: 5\nhierarchy: []\n"), 0o644)

	out := e.run() // no gem directory at all

	if !strings.Contains(out, "ERROR: no openvox gem found") {
		t.Errorf("output = %q, want a clear error about the missing gem", out)
	}
	if !strings.Contains(e.read(e.hiera), "data_hash: console_config_data") {
		t.Errorf("our own hiera.yaml was not refreshed:\n%s", e.read(e.hiera))
	}
}

func TestAMissingFunctionSourceIsReportedNotFatal(t *testing.T) {
	e := newEnv(t)
	e.gem("8.29.0")
	if err := os.Remove(e.src); err != nil {
		t.Fatal(err)
	}
	if out := e.run(); !strings.Contains(out, "ERROR:") || !strings.Contains(out, "not found") {
		t.Errorf("output = %q, want an error naming the missing source", out)
	}
}
