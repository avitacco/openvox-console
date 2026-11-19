package agentapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var gated = regexp.MustCompile(`"((?:GET|POST|PUT|PATCH|DELETE)) (/api/v1/[^"]+)",\s*(?:h\.verifier\.Authorize|authorize)\("([^"]+)"`)

// Every permission-gated route in the console is either in the OpenAPI
// document or left out on purpose, so a new endpoint cannot be forgotten.
func TestOpenAPI_NoRouteIsForgotten(t *testing.T) {
	files, err := filepath.Glob("../*/*.go")
	if err != nil {
		t.Fatal(err)
	}
	var seen int
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range gated.FindAllSubmatch(src, -1) {
			seen++
			method, path, perm := string(m[1]), string(m[2]), string(m[3])
			why := Undocumented(method, path)
			if why == "?" {
				t.Errorf("%s %s (%s) is in neither the OpenAPI document nor the list of routes left out on purpose: add it to documented or to undocumented in internal/agentapi/openapi.go", method, path, perm)
			}
			if why == "" {
				// A documented route states the permission it needs.
				for _, r := range documented {
					if r.method == method && r.path == path && r.permission != perm {
						t.Errorf("%s %s is documented as needing %s but is gated by %s", method, path, r.permission, perm)
					}
				}
			}
		}
	}
	if seen < 100 {
		t.Fatalf("found only %d routes in the source; the pattern no longer matches how they are registered", seen)
	}
	// And every documented route exists.
	registered := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, _ := os.ReadFile(file)
		for _, m := range gated.FindAllSubmatch(src, -1) {
			registered[string(m[1])+" "+string(m[2])] = true
		}
	}
	for _, key := range sortedPaths() {
		if !registered[key] {
			t.Errorf("the OpenAPI document describes %s, which is not a route", key)
		}
	}
}

func TestOpenAPI_DocumentIsValidAndComplete(t *testing.T) {
	doc := OpenAPI("test")
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	if back["openapi"] != "3.0.3" {
		t.Errorf("openapi = %v", back["openapi"])
	}
	paths := back["paths"].(map[string]any)
	run := paths["/api/v1/orchestrator/tasks"].(map[string]any)["post"].(map[string]any)
	if run["x-required-permission"] != "orchestrator:run" || run["requestBody"] == nil {
		t.Errorf("task route = %v", run)
	}
	node := paths["/api/v1/nodes/{name}"].(map[string]any)["get"].(map[string]any)
	params := node["parameters"].([]any)
	if len(params) != 1 || params[0].(map[string]any)["in"] != "path" || params[0].(map[string]any)["required"] != true {
		t.Errorf("node parameters = %v", params)
	}
	if Undocumented("GET", "/api/v1/brand-new-endpoint") != "?" || Undocumented("POST", "/api/v1/groups") == "?" {
		t.Error("Undocumented does not tell a forgotten route from one left out on purpose")
	}
	if _, ok := paths["/api/v1/users"]; ok {
		t.Error("administration is documented")
	}
}
