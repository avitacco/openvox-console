// Command enc-bridge is the local executable openvox-server's
// node_terminus = exec / external_nodes mechanism invokes. It calls the
// console's ENC HTTP endpoint (GET /api/v1/enc/{certname}) and writes the
// YAML document Puppet's exec-based classifier terminus requires - see
// design.md in the phase-2-node-classifier change for the verified format
// and why this is a small compiled binary using an established YAML
// library rather than a hand-formatted shell script.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type encResponse struct {
	Classes     map[string]map[string]any `json:"classes"`
	Parameters  map[string]any            `json:"parameters"`
	Environment *string                   `json:"environment,omitempty"`
}

// encDocument is the exec-terminus YAML shape. omitempty on Classes and
// Parameters means a node matched by no group (both empty) produces
// entirely empty output, which is the ENC spec's valid "no classification"
// case - not an error.
type encDocument struct {
	Classes     map[string]any `yaml:"classes,omitempty"`
	Parameters  map[string]any `yaml:"parameters,omitempty"`
	Environment *string        `yaml:"environment,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: enc-bridge <certname>")
		os.Exit(1)
	}
	certname := os.Args[1]

	baseURL := os.Getenv("ENC_BRIDGE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	// ENC_BRIDGE_TOKEN_FILE keeps the token out of the environment of a
	// process openvox-server spawns for every classification request,
	// matching the console's own <NAME>_FILE convention (see
	// internal/runtime.LoadConfig) and what Docker secrets present.
	token, err := envOrFile("ENC_BRIDGE_TOKEN")
	if err != nil {
		fmt.Fprintf(os.Stderr, "enc-bridge: %v\n", err)
		os.Exit(1)
	}

	if err := run(baseURL, token, certname, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "enc-bridge: %v\n", err)
		os.Exit(1)
	}
}

// envOrFile returns name's value, read from the file named by
// <name>_FILE when that is set. A named file that cannot be read is an
// error rather than an empty token: the resulting classification request
// would be rejected as unauthenticated, which is a much harder failure
// to trace back to a missing secret file.
func envOrFile(name string) (string, error) {
	path := os.Getenv(name + "_FILE")
	if path == "" {
		return os.Getenv(name), nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s_FILE: %w", name, err)
	}
	return strings.TrimSpace(string(contents)), nil
}

func run(baseURL, token, certname string, out io.Writer) error {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/enc/"+url.PathEscape(certname), nil)
	if err != nil {
		return fmt.Errorf("build classification request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request classification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("classification request returned status %d", resp.StatusCode)
	}

	var enc encResponse
	if err := json.NewDecoder(resp.Body).Decode(&enc); err != nil {
		return fmt.Errorf("decode classification response: %w", err)
	}

	doc := encDocument{
		Classes:     classesToYAML(enc.Classes),
		Parameters:  enc.Parameters,
		Environment: enc.Environment,
	}

	encoder := yaml.NewEncoder(out)
	defer encoder.Close()
	if err := encoder.Encode(doc); err != nil {
		return fmt.Errorf("encode YAML: %w", err)
	}
	return nil
}

// classesToYAML converts the ENC endpoint's class parameter maps into the
// hash form Puppet's exec terminus expects, rendering a non-parameterized
// class as a null value (`classname:`) rather than an empty map
// (`classname: {}`), matching Puppet's own documented example.
func classesToYAML(classes map[string]map[string]any) map[string]any {
	if len(classes) == 0 {
		return nil
	}
	out := make(map[string]any, len(classes))
	for name, params := range classes {
		if len(params) == 0 {
			out[name] = nil
		} else {
			out[name] = params
		}
	}
	return out
}
