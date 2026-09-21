// Package openvoxdb is the console's only path to openvoxdb: it executes
// PQL queries and submits commands over openvoxdb's HTTP API,
// authenticated with a client certificate issued by the OpenVox CA, and
// returns typed results.
//
// It is deliberately thin - a query/response and command-submission
// client for the specific entities and operations the console needs
// (nodes, facts, reports, events; deactivating a node), not a
// general-purpose PQL engine or full openvoxdb API client. See design.md
// in the change that introduced this package for the query-side
// rationale, and design.md in add-node-deletion for why command
// submission was added alongside it rather than as a separate client.
package openvoxdb

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Client queries an openvoxdb instance over its PQL query API.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient builds a Client authenticated to baseURL with the client
// certificate at certFile/keyFile, trusting the CA at caFile.
func NewClient(baseURL, certFile, keyFile, caFile string) (*Client, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load openvoxdb client certificate: %w", err)
	}

	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read openvoxdb CA certificate: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no certificates found in openvoxdb CA file %s", caFile)
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      caPool,
		},
	}

	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http:    &http.Client{Transport: transport},
	}, nil
}

// Query executes a raw PQL query against openvoxdb and returns each result
// row as a generic map, for callers that don't need a typed result.
func (c *Client) Query(ctx context.Context, pql string) ([]map[string]any, error) {
	var rows []map[string]any
	if err := c.query(ctx, pql, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// query executes pql and decodes the response body into out, which must be
// a pointer to a slice.
func (c *Client) query(ctx context.Context, pql string, out any) error {
	if err := validatePQL(pql); err != nil {
		return err
	}

	body, err := json.Marshal(map[string]string{"query": pql})
	if err != nil {
		return fmt.Errorf("encode PQL query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/pdb/query/v4", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build openvoxdb query request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("openvoxdb unreachable: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("openvoxdb unreachable: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openvoxdb query failed (status %d): %s", resp.StatusCode, describeError(respBody))
	}

	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("parse openvoxdb response: %w", err)
	}
	return nil
}

// Command submits a command to openvoxdb's command API - the write-side
// counterpart to query. commandName and version identify the command
// (e.g. "deactivate node", 3); payload is that command's own JSON body,
// separate from the certname/command/version query parameters openvoxdb
// expects on the URL itself. Confirmed live against a real openvoxdb
// instance (see design.md in add-node-deletion): a 200 response only
// means the command was accepted onto the queue, not that it was
// necessarily applied - openvoxdb can still silently ignore a command it
// considers stale (an old producer_timestamp), which is why callers like
// DeactivateNode always generate a current one rather than accepting it
// from outside.
func (c *Client) Command(ctx context.Context, commandName string, version int, certname string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode openvoxdb command payload: %w", err)
	}

	reqURL := fmt.Sprintf("%s/pdb/cmd/v1?command=%s&version=%d&certname=%s",
		c.baseURL, url.QueryEscape(commandName), version, url.QueryEscape(certname))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build openvoxdb command request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("openvoxdb unreachable: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("openvoxdb unreachable: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("openvoxdb command %q failed (status %d): %s", commandName, resp.StatusCode, describeError(respBody))
	}
	return nil
}

// describeError extracts a human-readable message from an openvoxdb error
// response body, falling back to the raw body if it isn't the expected
// {"error": "..."} shape.
func describeError(body []byte) string {
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		return errResp.Error
	}
	return strings.TrimSpace(string(body))
}
