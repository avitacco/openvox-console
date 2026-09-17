package nodeagent

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// factOutput is the JSON shape both Linux package-inventory fact scripts
// emit on stdout.
type factOutput struct {
	Inventory struct {
		Packages [][]string `json:"packages"`
	} `json:"_puppet_inventory_1"`
	Console struct {
		Format  int                 `json:"format"`
		Sources map[string][]string `json:"sources"`
	} `json:"console_package_inventory"`
}

// runFactScript writes family's embedded fact script and a stub named
// tool (printing stubOutput and exiting stubExit) into temp dirs, then
// runs the script with the stub first on PATH - so the script's own
// parsing and JSON assembly run for real, without dpkg or rpm installed.
func runFactScript(t *testing.T, family packageManagerFamily, tool, stubOutput string, stubExit int) (stdout string, exitErr error) {
	t.Helper()
	script, ok := packageInventoryFactScript(family)
	if !ok {
		t.Fatalf("no embedded fact script for family %v", family)
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "package_inventory.sh")
	if err := os.WriteFile(scriptPath, script, 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatalf("write script: %v", err)
	}

	stubDir := t.TempDir()
	outPath := filepath.Join(stubDir, "output")
	if err := os.WriteFile(outPath, []byte(stubOutput), 0o600); err != nil {
		t.Fatalf("write stub output: %v", err)
	}
	stub := "#!/bin/sh\ncat '" + outPath + "'\nexit " + string(rune('0'+stubExit)) + "\n"
	if err := os.WriteFile(filepath.Join(stubDir, tool), []byte(stub), 0o755); err != nil { //nolint:gosec // stub must be executable
		t.Fatalf("write stub: %v", err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Env = []string{"PATH=" + stubDir + ":/usr/bin:/bin"}
	out, err := cmd.Output()
	return string(out), err
}

func decodeFactOutput(t *testing.T, stdout string) factOutput {
	t.Helper()
	var got factOutput
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("fact output is not valid JSON: %v\noutput: %s", err, stdout)
	}
	return got
}

func TestRpmFactScript_QueriesEpoch(t *testing.T) {
	script, _ := packageInventoryFactScript(familyRPM)
	want := `%{NAME}\t%|EPOCH?{%{EPOCH}:}:{}|%{VERSION}-%{RELEASE}\n`
	if !strings.Contains(string(script), want) {
		t.Errorf("rpm fact script does not use queryformat %q", want)
	}
}

func TestRpmFactScript_Output(t *testing.T) {
	stdout, err := runFactScript(t, familyRPM, "rpm",
		"openssl\t1:3.0.7-24.el9\nglibc\t2.34-100.el9\n", 0)
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}
	got := decodeFactOutput(t, stdout)

	wantPackages := [][]string{
		{"openssl", "1:3.0.7-24.el9", "rpm"},
		{"glibc", "2.34-100.el9", "rpm"},
	}
	if !reflect.DeepEqual(got.Inventory.Packages, wantPackages) {
		t.Errorf("packages = %v, want %v", got.Inventory.Packages, wantPackages)
	}
	if got.Console.Format != 2 {
		t.Errorf("format = %d, want 2", got.Console.Format)
	}
	if got.Console.Sources != nil {
		t.Errorf("sources = %v, want absent for rpm", got.Console.Sources)
	}
}

func TestAptFactScript_Output(t *testing.T) {
	stdout, err := runFactScript(t, familyAPT, "dpkg-query", strings.Join([]string{
		"installed\topenssl\t3.0.11-1~deb12u1\topenssl\t3.0.11-1~deb12u1",
		"installed\tlibssl3\t3.0.11-1~deb12u1\topenssl\t3.0.11-1~deb12u1",
		"installed\tlibfoo1\t1.2-3+b1\tfoo\t1.2-3",
		"installed\tbash\t5.2.15-2+b2\tbash\t5.2.15-2",
		// A second architecture's copy of an already-listed package.
		"installed\tlibssl3\t3.0.11-1~deb12u1\topenssl\t3.0.11-1~deb12u1",
	}, "\n")+"\n", 0)
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}
	got := decodeFactOutput(t, stdout)

	if len(got.Inventory.Packages) != 5 {
		t.Errorf("got %d package tuples, want 5 (tuples unchanged from dpkg-query output)", len(got.Inventory.Packages))
	}
	if want := []string{"libssl3", "3.0.11-1~deb12u1", "apt"}; !reflect.DeepEqual(got.Inventory.Packages[1], want) {
		t.Errorf("packages[1] = %v, want %v (binary name and version)", got.Inventory.Packages[1], want)
	}
	if got.Console.Format != 2 {
		t.Errorf("format = %d, want 2", got.Console.Format)
	}
	wantSources := map[string][]string{
		"libssl3": {"openssl", "3.0.11-1~deb12u1"},
		"libfoo1": {"foo", "1.2-3"},
		"bash":    {"bash", "5.2.15-2"},
	}
	if !reflect.DeepEqual(got.Console.Sources, wantSources) {
		t.Errorf("sources = %v, want %v", got.Console.Sources, wantSources)
	}
}

func TestAptFactScript_NoDifferingSources(t *testing.T) {
	stdout, err := runFactScript(t, familyAPT, "dpkg-query", "installed\topenssl\t3.0.11-1\topenssl\t3.0.11-1\n", 0)
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}
	got := decodeFactOutput(t, stdout)
	if got.Console.Sources == nil || len(got.Console.Sources) != 0 {
		t.Errorf("sources = %v, want present and empty", got.Console.Sources)
	}
}

// A failed package query must not produce an empty inventory - Facter
// would report it and openvoxdb would drop every package row for the
// node. The script has to fail with no output instead.
func TestFactScripts_QueryFailureEmitsNothing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		family packageManagerFamily
		tool   string
	}{
		{"apt", familyAPT, "dpkg-query"},
		{"rpm", familyRPM, "rpm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, err := runFactScript(t, tc.family, tc.tool, "", 1)
			if err == nil {
				t.Error("script succeeded, want a nonzero exit when the package query fails")
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
		})
	}
}

// dpkg-query -W lists packages in config-files state (dpkg -l shows them
// as rc: removed, but their configuration files retained) alongside
// installed ones. Those are not installed and must not reach either
// emitted structure - reporting them matched advisories against software
// that was not on the node at all.
//
// The first row is deliberately a dropped one: the packages separator has
// to count emitted rows rather than NR, or a leading comma lands in the
// JSON and decodeFactOutput fails outright.
func TestAptFactScript_SkipsPackagesThatAreNotInstalled(t *testing.T) {
	stdout, err := runFactScript(t, familyAPT, "dpkg-query", strings.Join([]string{
		"config-files\tcron\t3.0pl1-162\tcron\t3.0pl1-162",
		"installed\tbash\t5.2.15-2+b2\tbash\t5.2.15-2",
		// A removed kernel whose source package differs, so it would
		// otherwise reach the sources map as well as the package list.
		"config-files\tlinux-image-6.8.0-64-generic\t6.8.0-64.67\tlinux-signed\t6.8.0-64.67",
		"installed\tlibssl3\t3.0.11-1~deb12u1\topenssl\t3.0.11-1~deb12u1",
		// A second architecture's copy of an already-listed package.
		"installed\tlibssl3\t3.0.11-1~deb12u1\topenssl\t3.0.11-1~deb12u1",
	}, "\n")+"\n", 0)
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}
	got := decodeFactOutput(t, stdout)

	wantPackages := [][]string{
		{"bash", "5.2.15-2+b2", "apt"},
		{"libssl3", "3.0.11-1~deb12u1", "apt"},
		{"libssl3", "3.0.11-1~deb12u1", "apt"},
	}
	if !reflect.DeepEqual(got.Inventory.Packages, wantPackages) {
		t.Errorf("packages = %v, want %v (config-files rows dropped)", got.Inventory.Packages, wantPackages)
	}

	// The sources map is built from the same filtered rows, so a dropped
	// package must not appear there either even though its source package
	// differs from it.
	wantSources := map[string][]string{
		"bash":    {"bash", "5.2.15-2"},
		"libssl3": {"openssl", "3.0.11-1~deb12u1"},
	}
	if !reflect.DeepEqual(got.Console.Sources, wantSources) {
		t.Errorf("sources = %v, want %v (dropped rows absent, multi-arch copy deduped)", got.Console.Sources, wantSources)
	}
}

// A held package's selection state is "hold" rather than "install" while
// the package is still installed. Filtering on selection instead of
// install status would drop it, hiding a package that is present - and
// held packages are disproportionately outdated, so that would be a
// false negative in vulnerability scanning.
func TestAptFactScript_ReportsHeldPackages(t *testing.T) {
	stdout, err := runFactScript(t, familyAPT, "dpkg-query",
		"installed\tbash\t5.2.15-2+b13\tbash\t5.2.15-2\n", 0)
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}
	got := decodeFactOutput(t, stdout)

	wantPackages := [][]string{{"bash", "5.2.15-2+b13", "apt"}}
	if !reflect.DeepEqual(got.Inventory.Packages, wantPackages) {
		t.Errorf("packages = %v, want %v (an installed package is reported whatever its selection state)", got.Inventory.Packages, wantPackages)
	}
}

// rpm -qa lists imported repository signing keys as gpg-pubkey rows once
// any key has been imported, which is the normal state of a real system.
// They are keys, not installed software. The signing key is listed first
// so a separator keyed on NR would corrupt the JSON.
func TestRpmFactScript_SkipsSigningKeys(t *testing.T) {
	stdout, err := runFactScript(t, familyRPM, "rpm", strings.Join([]string{
		"gpg-pubkey\t350d275d-6279464b",
		"openssl\t1:3.0.7-24.el9",
		"glibc\t2.34-100.el9",
	}, "\n")+"\n", 0)
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}
	got := decodeFactOutput(t, stdout)

	wantPackages := [][]string{
		{"openssl", "1:3.0.7-24.el9", "rpm"},
		{"glibc", "2.34-100.el9", "rpm"},
	}
	if !reflect.DeepEqual(got.Inventory.Packages, wantPackages) {
		t.Errorf("packages = %v, want %v (signing keys dropped, epoch formatting unchanged)", got.Inventory.Packages, wantPackages)
	}
}
