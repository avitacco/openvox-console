package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// consoleClient talks to the console's own HTTP API as the bootstrap
// admin. Everything the console owns rather than openvoxdb - groups,
// users, roles, tokens, deployments - is written through here, so the
// seed exercises the same endpoints the UI does and cannot drift from
// them without failing.
type consoleClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func newConsoleClient(ctx context.Context, opts options) (*consoleClient, error) {
	if opts.adminPass == "" {
		return nil, fmt.Errorf("no console admin password: pass --admin-password or set CONSOLE_BOOTSTRAP_ADMIN_PASSWORD")
	}

	c := &consoleClient{
		baseURL: strings.TrimSuffix(opts.consoleURL, "/"),
		http:    &http.Client{Timeout: 60 * time.Second},
	}

	var resp struct {
		AccessToken string `json:"accessToken"`
	}
	err := c.do(ctx, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": opts.adminUser,
		"password": opts.adminPass,
	}, &resp)
	if err != nil {
		return nil, fmt.Errorf("logging in to the console as %q: %w", opts.adminUser, err)
	}
	c.token = resp.AccessToken
	return c, nil
}

// do issues one API call, decoding a JSON response into out when out is
// non-nil.
func (c *consoleClient) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("console unreachable: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out == nil {
		return nil
	}
	if len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

// isConflict reports whether err is the console refusing to create
// something that already exists. The seed converges rather than
// accumulating, so an existing record is a success, not a failure - see
// the repeatability requirement in specs/demo-data-seeding.
func isConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "status 409") ||
		strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "duplicate")
}
