package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRun_ProducesValidExecTerminusYAML(t *testing.T) {
	var gotAuthHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/enc/web01" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"classes": {
				"common": {},
				"ntp": {"ntpserver": "0.pool.ntp.org"}
			},
			"parameters": {"mail_server": "mail.example.com"},
			"environment": "production"
		}`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	if err := run(srv.URL, "test-service-token", "web01", &buf); err != nil {
		t.Fatalf("run() error: %v", err)
	}
	if gotAuthHeader != "Bearer test-service-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuthHeader, "Bearer test-service-token")
	}

	var doc map[string]any
	if err := yaml.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid YAML: %v\noutput:\n%s", err, buf.String())
	}

	classes, ok := doc["classes"].(map[string]any)
	if !ok {
		t.Fatalf("classes = %v, want a map", doc["classes"])
	}
	if _, ok := classes["common"]; !ok {
		t.Error("expected non-parameterized class 'common' present")
	}
	if classes["common"] != nil {
		t.Errorf("common = %v, want nil (null), matching Puppet's documented format", classes["common"])
	}
	ntp, ok := classes["ntp"].(map[string]any)
	if !ok || ntp["ntpserver"] != "0.pool.ntp.org" {
		t.Errorf("ntp = %v", classes["ntp"])
	}

	params, ok := doc["parameters"].(map[string]any)
	if !ok || params["mail_server"] != "mail.example.com" {
		t.Errorf("parameters = %v", doc["parameters"])
	}

	if doc["environment"] != "production" {
		t.Errorf("environment = %v, want production", doc["environment"])
	}
}

func TestRun_EmptyClassificationProducesEmptyOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"classes": {}, "parameters": {}}`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	if err := run(srv.URL, "", "unclassified-node", &buf); err != nil {
		t.Fatalf("run() error: %v", err)
	}

	// Per Puppet's ENC docs, an ENC "must return either nothing or a YAML
	// hash containing at least one of classes/parameters" - an empty
	// classification should produce an empty (or `{}`) document, not
	// invent placeholder keys.
	trimmed := strings.TrimSpace(buf.String())
	if trimmed != "" && trimmed != "{}" {
		t.Errorf("output = %q, want empty or {}", trimmed)
	}
}

func TestRun_NonOKStatusReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	if err := run(srv.URL, "", "web01", &buf); err == nil {
		t.Error("run() error = nil, want an error for a non-200 response")
	}
}

func TestClassesToYAML_EmptyMapBecomesNull(t *testing.T) {
	got := classesToYAML(map[string]map[string]any{"common": {}})
	if v, ok := got["common"]; !ok || v != nil {
		t.Errorf("classesToYAML(empty params) = %v, want common: nil", got)
	}
}

func TestClassesToYAML_NoClassesReturnsNil(t *testing.T) {
	if got := classesToYAML(map[string]map[string]any{}); got != nil {
		t.Errorf("classesToYAML(none) = %v, want nil", got)
	}
}
