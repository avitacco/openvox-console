package nodeagent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func cachedFiles(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	out := map[string]string{}
	i := 0
	for name, content := range files {
		p := filepath.Join(dir, "c", string(rune('a'+i)))
		i++
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		out[name] = p
	}
	return out
}

func TestBuildInstallDirLayout(t *testing.T) {
	cached := cachedFiles(t, map[string]string{
		"multi.sh":                                "#!/bin/sh\n",
		"task_fixture/files/helper.sh":            "greet() { :; }\n",
		"task_fixture/lib/task_fixture/helper.rb": "module X; end\n",
	})
	base := filepath.Join(t.TempDir(), "runs")
	dir, err := buildInstallDir(base, "task_fixture", "multi.sh",
		[]string{"task_fixture/files/helper.sh", "task_fixture/lib/task_fixture/helper.rb"}, cached)
	if err != nil {
		t.Fatalf("buildInstallDir: %v", err)
	}
	if dir.executable != filepath.Join(dir.root, "task_fixture", "tasks", "multi.sh") {
		t.Errorf("executable = %s", dir.executable)
	}
	for rel, want := range map[string]string{
		"task_fixture/tasks/multi.sh":             "#!/bin/sh\n",
		"task_fixture/files/helper.sh":            "greet() { :; }\n",
		"task_fixture/lib/task_fixture/helper.rb": "module X; end\n",
	} {
		got, err := os.ReadFile(filepath.Join(dir.root, filepath.FromSlash(rel)))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", rel, got, err)
		}
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(dir.root)
		if info.Mode().Perm() != 0o700 {
			t.Errorf("run directory mode = %v, want 0700", info.Mode().Perm())
		}
		info, _ = os.Stat(dir.executable)
		if info.Mode().Perm()&0o100 == 0 {
			t.Errorf("executable mode = %v, want owner-executable", info.Mode().Perm())
		}
	}

	dir.remove()
	if _, err := os.Stat(dir.root); !os.IsNotExist(err) {
		t.Error("run directory survived remove")
	}
}

func TestBuildInstallDirRejectsEscapes(t *testing.T) {
	cached := cachedFiles(t, map[string]string{"t.sh": "x", "../../etc/cron.d/evil": "x"})
	base := filepath.Join(t.TempDir(), "runs")
	for name, tc := range map[string]struct {
		module, impl string
		support      []string
	}{
		"parent in support":    {"mod", "t.sh", []string{"../../etc/cron.d/evil"}},
		"absolute support":     {"mod", "t.sh", []string{"/etc/passwd"}},
		"backslash support":    {"mod", "t.sh", []string{`mod\..\..\x`}},
		"parent in module":     {"..", "t.sh", nil},
		"slash in impl":        {"mod", "../t.sh", nil},
		"drive letter support": {"mod", "t.sh", []string{"C:/Windows/x"}},
	} {
		if _, err := buildInstallDir(base, tc.module, tc.impl, tc.support, cached); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Nothing is left behind by a refused build.
	entries, _ := os.ReadDir(base)
	if len(entries) != 0 {
		t.Errorf("refused builds left %d directories", len(entries))
	}
}

func TestBuildInstallDirMissingFile(t *testing.T) {
	if _, err := buildInstallDir(filepath.Join(t.TempDir(), "runs"), "mod", "t.sh", nil, map[string]string{}); err == nil {
		t.Error("built a run directory without the executable")
	}
}
