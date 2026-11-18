package codeplans

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// codeTree builds a code directory with one environment, reached through a
// symlink as a deploy leaves it.
func codeTree(t *testing.T) (code, env string) {
	t.Helper()
	code = t.TempDir()
	deploy := filepath.Join(code, ".deploys", "production", "20260101T000000", "basedir", "production")
	write(t, filepath.Join(deploy, "environment.conf"), "# comment\nmodulepath = modules:site # trailing\n")
	write(t, filepath.Join(deploy, "modules", "ops", "plans", "patch.yaml"), "steps: []\n")
	write(t, filepath.Join(deploy, "modules", "ops", "plans", "init.yaml"), "steps: []\n")
	write(t, filepath.Join(deploy, "site", "ops", "plans", "patch.yaml"), "description: shadowed\nsteps: []\n")
	write(t, filepath.Join(deploy, "site", "web", "plans", "restart.yaml"), "steps: []\n")
	write(t, filepath.Join(deploy, "modules", "ops", "plans", "notes.txt"), "not a plan")
	write(t, filepath.Join(deploy, "modules", "Bad-Name", "plans", "x.yaml"), "steps: []\n")
	if err := os.MkdirAll(filepath.Join(code, "environments"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(deploy, filepath.Join(code, "environments", "production")); err != nil {
		t.Fatal(err)
	}
	return code, deploy
}

func names(files []File) string {
	var n []string
	for _, f := range files {
		n = append(n, f.Name)
	}
	return strings.Join(n, ",")
}

func TestFiles_NamesModulePathAndShadowing(t *testing.T) {
	code, _ := codeTree(t)
	files, err := NewLoader(code).Files("production")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(files); got != "ops,ops::patch,web::restart" {
		t.Errorf("plans = %s", got)
	}
	// The first module path entry wins: the one in modules, not site.
	f, err := NewLoader(code).File("production", "ops::patch")
	if err != nil || strings.Contains(string(f.Data), "shadowed") || f.Path != "modules/ops/plans/patch.yaml" {
		t.Errorf("file = %+v, %v", f, err)
	}
}

func TestFiles_DefaultModulePathWithoutEnvironmentConf(t *testing.T) {
	code, deploy := codeTree(t)
	if err := os.Remove(filepath.Join(deploy, "environment.conf")); err != nil {
		t.Fatal(err)
	}
	files, _ := NewLoader(code).Files("production")
	if got := names(files); got != "ops,ops::patch" {
		t.Errorf("plans = %s (site is not on the default module path)", got)
	}
}

func TestFiles_Unavailable(t *testing.T) {
	if _, err := NewLoader("").Files("production"); !errors.Is(err, ErrNotAvailable) {
		t.Errorf("no code dir = %v", err)
	}
	if _, err := NewLoader(t.TempDir()).Files("production"); !errors.Is(err, ErrNotAvailable) {
		t.Errorf("a code dir without environments = %v", err)
	}
	code, _ := codeTree(t)
	for _, env := range []string{"nope", "../production", "prod uction", ""} {
		if _, err := NewLoader(code).Files(env); !errors.Is(err, ErrEnvironmentNotFound) {
			t.Errorf("environment %q = %v", env, err)
		}
	}
	if _, err := NewLoader(code).File("production", "ops::missing"); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("missing plan = %v", err)
	}
	if _, err := NewLoader(code).File("production", "../../etc/passwd"); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("an unsafe name = %v", err)
	}
}

func TestFiles_NothingOutsideTheEnvironmentIsRead(t *testing.T) {
	code, deploy := codeTree(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "plans", "stolen.yaml"), "steps: []\n")
	// A module whose plans directory, and one whose file, link out of the environment.
	if err := os.MkdirAll(filepath.Join(deploy, "modules", "linked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "plans"), filepath.Join(deploy, "modules", "linked", "plans")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(outside, "secret.yaml"), "steps: []\n")
	if err := os.Symlink(filepath.Join(outside, "secret.yaml"), filepath.Join(deploy, "modules", "ops", "plans", "leak.yaml")); err != nil {
		t.Fatal(err)
	}
	// A module path entry that leaves the environment.
	if err := os.Symlink(outside, filepath.Join(deploy, "escape")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(deploy, "environment.conf"), "modulepath = modules:escape:/etc:../x\n")
	files, err := NewLoader(code).Files("production")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(files); got != "ops,ops::patch" {
		t.Errorf("plans = %s, want only what the environment ships", got)
	}
}

func TestFiles_AnOversizeFileIsFlaggedNotRead(t *testing.T) {
	code, deploy := codeTree(t)
	write(t, filepath.Join(deploy, "modules", "ops", "plans", "huge.yaml"), strings.Repeat("a", MaxPlanFileBytes+10))
	f, err := NewLoader(code).File("production", "ops::huge")
	if err != nil || !f.Oversize || len(f.Data) != 0 {
		t.Errorf("file = %+v, %v", f, err)
	}
}
