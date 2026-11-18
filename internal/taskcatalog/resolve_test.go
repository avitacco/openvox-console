package taskcatalog

import (
	"encoding/json"
	"strings"
	"testing"
)

// recordedTask builds a Task from one of the recorded responses, the way
// Client.Task does.
func recordedTask(t *testing.T, name string) Task {
	t.Helper()
	var resp struct {
		Name  string `json:"name"`
		Files []File `json:"files"`
	}
	if err := json.Unmarshal(fixture(t, "task_"+name+".json"), &resp); err != nil {
		t.Fatal(err)
	}
	return Task{Name: resp.Name, Environment: "production", Metadata: recordedMetadata(t, name), Files: resp.Files}
}

func TestResolveSingleFile(t *testing.T) {
	r, err := Resolve(recordedTask(t, "echo"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Task != "task_fixture::echo" || r.Environment != "production" || r.InputMethod != "stdin" {
		t.Errorf("resolved = %+v", r)
	}
	if len(r.Implementations) != 1 || r.Implementations[0].Name != "echo.sh" || len(r.Implementations[0].Files) != 0 {
		t.Errorf("implementations = %+v", r.Implementations)
	}
	if len(r.Files) != 1 {
		t.Fatalf("files = %+v", r.Files)
	}
	f := r.Files[0]
	if f.Filename != "echo.sh" || f.Path != "/puppet/v3/file_content/tasks/task_fixture/echo.sh" || f.Params["environment"] != "production" || len(f.SHA256) != 64 || f.Size != 161 {
		t.Errorf("file = %+v", f)
	}
}

func TestResolveMultipleImplementations(t *testing.T) {
	r, err := Resolve(recordedTask(t, "multi"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(r.Implementations) != 2 {
		t.Fatalf("implementations = %+v", r.Implementations)
	}
	sh, ps := r.Implementations[0], r.Implementations[1]
	if sh.Name != "multi.sh" || strings.Join(sh.Requirements, ",") != "shell" || strings.Join(sh.Files, ",") != "task_fixture/files/helper.sh" {
		t.Errorf("shell = %+v", sh)
	}
	if ps.Name != "multi.ps1" || ps.InputMethod != "powershell" || len(ps.Files) != 0 {
		t.Errorf("powershell = %+v", ps)
	}
}

func TestResolveTaskLevelSupportingFiles(t *testing.T) {
	r, err := Resolve(recordedTask(t, "ruby"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := strings.Join(r.Implementations[0].Files, ","); got != "task_fixture/lib/task_fixture/helper.rb" {
		t.Errorf("files = %s", got)
	}
}

func TestResolveExpandsDirectories(t *testing.T) {
	task := recordedTask(t, "ruby")
	task.Metadata.Files = []string{"task_fixture/lib/"}
	r, err := Resolve(task)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := strings.Join(r.Implementations[0].Files, ","); got != "task_fixture/lib/task_fixture/helper.rb" {
		t.Errorf("files = %s", got)
	}
}

func TestResolveRejectsBrokenMetadata(t *testing.T) {
	missingImpl := recordedTask(t, "multi")
	missingImpl.Metadata.Implementations[0].Name = "gone.sh"
	missingFile := recordedTask(t, "ruby")
	missingFile.Metadata.Files = []string{"task_fixture/lib/gone.rb"}
	ambiguous := recordedTask(t, "multi")
	ambiguous.Metadata.Implementations = nil
	for name, task := range map[string]Task{"missing implementation": missingImpl, "missing supporting file": missingFile, "two executables": ambiguous} {
		if _, err := Resolve(task); err == nil {
			t.Errorf("%s: Resolve succeeded, want an error", name)
		}
	}
}

func TestResolvedWireShape(t *testing.T) {
	r, err := Resolve(recordedTask(t, "multi"))
	if err != nil {
		t.Fatal(err)
	}
	r.Params = map[string]any{"name": "web"}
	r.TimeoutSeconds = 1800
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"task":"task_fixture::multi"`, `"input_method":"environment"`, `"timeout_seconds":1800`, `"params":{"name":"web"}`, `"sha256":"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("payload %s lacks %s", data, want)
		}
	}
}
