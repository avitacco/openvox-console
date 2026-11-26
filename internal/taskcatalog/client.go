// Package taskcatalog reads the Puppet tasks available in each code
// environment from openvoxserver's own API (/puppet/v3/environments and
// /puppet/v3/tasks), validates parameters against a task's metadata, and
// resolves a task into the payload a node needs to run it - see design.md
// in add-task-execution.
//
// Nothing here is stored: every call reads openvoxserver, so the catalogue
// always matches the code currently deployed. The credential is an
// ordinary CA-issued certificate (the openvoxdb one by default) - never
// the CA-client one, whose CA admin rights reading tasks has no use for.
package taskcatalog

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
)

// ErrNotConfigured is returned by every method of a nil *Client: the
// console was started without CONSOLE_PUPPET_SERVER_URL, so tasks are
// switched off rather than the console failing to start.
var ErrNotConfigured = errors.New("the task catalogue is not configured (CONSOLE_PUPPET_SERVER_URL is unset)")

// NotFoundError reports that openvoxserver does not serve the named
// environment, or has no such task in it. Kind is "environment" or "task".
type NotFoundError struct {
	Kind string
	Name string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s %q not found", e.Kind, e.Name)
}

// UnreachableError reports that openvoxserver could not be asked at all,
// or answered with something other than the API's own responses.
type UnreachableError struct {
	Err error
}

func (e *UnreachableError) Error() string {
	return fmt.Sprintf("openvoxserver is unreachable: %v", e.Err)
}

func (e *UnreachableError) Unwrap() error { return e.Err }

// Config configures the connection to openvoxserver's /puppet/v3 API.
type Config struct {
	URL      string // e.g. "https://openvoxserver:8140"
	CertFile string
	KeyFile  string
	CAFile   string // verifies openvoxserver's own server certificate
}

// Client reads tasks from openvoxserver. A nil *Client is valid and
// reports ErrNotConfigured from every method, so callers need no separate
// "is it configured" check.
type Client struct {
	baseURL string
	http    *http.Client
}

// New builds a Client from cfg, loading its TLS credentials.
func New(cfg Config) (*Client, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load puppet server client certificate: %w", err)
	}
	caPEM, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read puppet server CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no certificates found in puppet server CA file %s", cfg.CAFile)
	}
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      pool,
			},
		},
	}
	return newClient(cfg.URL, httpClient), nil
}

func newClient(baseURL string, httpClient *http.Client) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}
}

// WithURL returns a Client for the Puppet server at baseURL that uses
// this one's credential and connection pool - for reaching another
// server of the same CA, such as a compile server. A nil Client stays
// nil.
func (c *Client) WithURL(baseURL string) *Client {
	if c == nil {
		return nil
	}
	return newClient(baseURL, c.http)
}

// maxResponse bounds how much of a response is read. Task metadata is a
// few kilobytes; an environment with thousands of tasks is still well
// under this.
const maxResponse = 16 << 20

// get fetches path with query and decodes a 200 response into out. A 404
// is returned as a *NotFoundError, classified from openvoxserver's own
// message ("Could not find environment ..." or "Could not find task ...").
func (c *Client) get(ctx context.Context, path string, query url.Values, notFound func(body string) error, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return &UnreachableError{Err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return &UnreachableError{Err: fmt.Errorf("read response: %w", err)}
	}
	switch {
	case resp.StatusCode == http.StatusNotFound && notFound != nil:
		return notFound(string(body))
	case resp.StatusCode != http.StatusOK:
		return &UnreachableError{Err: fmt.Errorf("%s %s: %s", req.Method, path, resp.Status)}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &UnreachableError{Err: fmt.Errorf("decode %s: %w", path, err)}
	}
	return nil
}

// Environments returns the names of the environments openvoxserver
// serves, sorted.
func (c *Client) Environments(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	var resp struct {
		Environments map[string]json.RawMessage `json:"environments"`
	}
	if err := c.get(ctx, "/puppet/v3/environments", nil, nil, &resp); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(resp.Environments))
	for name := range resp.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// TaskSummary is one entry of an environment's task list.
type TaskSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Private tasks are hidden from task pickers (as Bolt and PE do), but
	// can still be run by name.
	Private bool `json:"private"`
}

// Tasks lists the tasks in environment, sorted by name.
func (c *Client) Tasks(ctx context.Context, environment string) ([]TaskSummary, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	var tasks []TaskSummary
	notFound := func(string) error { return &NotFoundError{Kind: "environment", Name: environment} }
	if err := c.get(ctx, "/puppet/v3/tasks", url.Values{"environment": {environment}}, notFound, &tasks); err != nil {
		return nil, err
	}
	if tasks == nil {
		tasks = []TaskSummary{}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Name < tasks[j].Name })
	return tasks, nil
}

// File is one file a task can need, as openvoxserver describes it.
// Filename is the task file's own name for a file in the task's
// directory, and a module-relative path ("mod/lib/x.rb") for a
// supporting file.
type File struct {
	Filename  string `json:"filename"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	URI       struct {
		Path   string            `json:"path"`
		Params map[string]string `json:"params"`
	} `json:"uri"`
}

// Task is one task's metadata and files in an environment.
type Task struct {
	Name        string
	Environment string
	Metadata    Metadata
	Files       []File
}

// splitTaskName maps a task's full name to its module and task parts: a
// bare module name is that module's init task.
func splitTaskName(name string) (module, task string, err error) {
	module, task, found := strings.Cut(name, "::")
	if !found {
		task = "init"
	}
	if !validNamePart(module) || !validNamePart(task) || strings.Contains(task, "::") {
		return "", "", fmt.Errorf("invalid task name %q: want module::task, or a module name for its init task", name)
	}
	return module, task, nil
}

// validNamePart accepts what Puppet allows in a module or task name:
// lowercase letters, digits and underscores, starting with a letter.
func validNamePart(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

// Task fetches one task's metadata and files from environment. name is
// the task's full name ("mod::task", or "mod" for its init task).
func (c *Client) Task(ctx context.Context, environment, name string) (Task, error) {
	if c == nil {
		return Task{}, ErrNotConfigured
	}
	module, task, err := splitTaskName(name)
	if err != nil {
		return Task{}, &NotFoundError{Kind: "task", Name: name}
	}
	var resp struct {
		Name     string          `json:"name"`
		Metadata json.RawMessage `json:"metadata"`
		Files    []File          `json:"files"`
	}
	notFound := func(body string) error {
		if strings.Contains(body, "environment") {
			return &NotFoundError{Kind: "environment", Name: environment}
		}
		return &NotFoundError{Kind: "task", Name: name}
	}
	path := "/puppet/v3/tasks/" + url.PathEscape(module) + "/" + url.PathEscape(task)
	if err := c.get(ctx, path, url.Values{"environment": {environment}}, notFound, &resp); err != nil {
		return Task{}, err
	}
	md, err := ParseMetadata(resp.Metadata)
	if err != nil {
		return Task{}, &UnreachableError{Err: fmt.Errorf("task %s: %w", name, err)}
	}
	return Task{Name: resp.Name, Environment: environment, Metadata: md, Files: resp.Files}, nil
}
