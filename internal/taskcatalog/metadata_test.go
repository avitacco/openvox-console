package taskcatalog

import (
	"encoding/json"
	"testing"
)

// recordedMetadata returns the metadata object from one of the recorded
// task responses.
func recordedMetadata(t *testing.T, task string) Metadata {
	t.Helper()
	var resp struct {
		Metadata json.RawMessage `json:"metadata"`
	}
	if err := json.Unmarshal(fixture(t, "task_"+task+".json"), &resp); err != nil {
		t.Fatal(err)
	}
	md, err := ParseMetadata(resp.Metadata)
	if err != nil {
		t.Fatalf("ParseMetadata(%s): %v", task, err)
	}
	return md
}

func TestParseMetadataEcho(t *testing.T) {
	md := recordedMetadata(t, "echo")
	if md.Description != "Echoes its parameters back as JSON" || md.InputMethod != "stdin" || md.SupportsNoop {
		t.Errorf("metadata = %+v", md)
	}
	if len(md.Parameters) != 4 {
		t.Fatalf("got %d parameters, want 4", len(md.Parameters))
	}
	count := md.Parameters["count"]
	if count.Type != "Optional[Integer[1, 10]]" || string(count.Default) != "1" || count.Description != "How many times to repeat it" {
		t.Errorf("count = %+v", count)
	}
}

func TestParseMetadataSensitive(t *testing.T) {
	md := recordedMetadata(t, "secret")
	if !md.Parameters["password"].Sensitive {
		t.Error("password is not marked sensitive")
	}
}

func TestParseMetadataImplementations(t *testing.T) {
	md := recordedMetadata(t, "multi")
	if md.InputMethod != "environment" || len(md.Implementations) != 2 {
		t.Fatalf("metadata = %+v", md)
	}
	sh, ps := md.Implementations[0], md.Implementations[1]
	if sh.Name != "multi.sh" || len(sh.Requirements) != 1 || sh.Requirements[0] != "shell" || len(sh.Files) != 1 || sh.Files[0] != "task_fixture/files/helper.sh" {
		t.Errorf("shell implementation = %+v", sh)
	}
	if ps.Name != "multi.ps1" || ps.InputMethod != "powershell" {
		t.Errorf("powershell implementation = %+v", ps)
	}
}

func TestParseMetadataSupportingFiles(t *testing.T) {
	md := recordedMetadata(t, "ruby")
	if len(md.Files) != 1 || md.Files[0] != "task_fixture/lib/task_fixture/helper.rb" {
		t.Errorf("files = %v", md.Files)
	}
}

func TestParseMetadataNoParametersKey(t *testing.T) {
	// plain.sh has no metadata file at all: openvoxserver returns {}.
	md := recordedMetadata(t, "plain")
	if md.Parameters != nil {
		t.Errorf("Parameters = %v, want nil for a task with no parameters key", md.Parameters)
	}
}

func TestParseMetadataEmptyParameters(t *testing.T) {
	md := recordedMetadata(t, "init")
	if md.Parameters == nil || len(md.Parameters) != 0 {
		t.Errorf("Parameters = %#v, want an empty, non-nil map", md.Parameters)
	}
}

func TestParseMetadataNull(t *testing.T) {
	for _, raw := range []string{"", "null"} {
		md, err := ParseMetadata(json.RawMessage(raw))
		if err != nil || md.Parameters != nil {
			t.Errorf("ParseMetadata(%q) = %+v, %v", raw, md, err)
		}
	}
}

func TestParseMetadataRejectsUnknownInputMethod(t *testing.T) {
	for _, raw := range []string{
		`{"input_method":"telepathy"}`,
		`{"implementations":[{"name":"x.sh","input_method":"telepathy"}]}`,
		`{"implementations":[{"requirements":["shell"]}]}`,
	} {
		if _, err := ParseMetadata(json.RawMessage(raw)); err == nil {
			t.Errorf("ParseMetadata(%s) succeeded, want an error", raw)
		}
	}
}
