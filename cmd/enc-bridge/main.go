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
	// "enc-bridge --data <certname>" prints the node's configuration data
	// as JSON instead of the classification YAML. It is what the
	// console_config_data Hiera function (openvoxserver-init) runs, so that
	// it shares this program's address and token handling: openvoxserver
	// does not hand its own environment to Ruby code, but it does to the
	// programs it runs.
	dataMode := len(os.Args) == 3 && os.Args[1] == "--data"
	if len(os.Args) < 2 || (len(os.Args) != 2 && !dataMode) {
		fmt.Fprintln(os.Stderr, "usage: enc-bridge <certname>\n       enc-bridge --data <certname>")
		os.Exit(1)
	}
	certname := os.Args[len(os.Args)-1]

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

	do := run
	if dataMode {
		do = runData
	}
	if err := do(baseURL, token, certname, os.Stdout); err != nil {
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

// runData writes the node's merged configuration data, a JSON object, to
// out. The body is decoded and written again rather than copied, so
// anything that is not a JSON object is an error here and never reaches
// Hiera as data.
func runData(baseURL, token, certname string, out io.Writer) error {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/enc/"+url.PathEscape(certname)+"/data", nil)
	if err != nil {
		return fmt.Errorf("build configuration data request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request configuration data from %s: %w", baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the console at %s answered %d for the configuration data of %s", baseURL, resp.StatusCode, certname)
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("decode configuration data: %w", err)
	}
	if data == nil {
		data = map[string]any{}
	}
	return json.NewEncoder(out).Encode(data)
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
