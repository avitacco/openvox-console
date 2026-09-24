// Package certstatus queries openvoxserver's real Puppet Server CA API
// for per-node certificate status (signed/requested/revoked).
//
// This is deliberately its own package, not folded into
// internal/nodeconnectivity or anywhere else: the credential it uses
// (a "CA-client" certificate carrying the pp_cli_auth authorization
// extension) grants full CA admin access on openvoxserver - the
// ability to sign or revoke *any* node's certificate, not a narrower
// read-only cert-status role, because openvoxserver's own auth.conf has
// no such narrower grant. Keeping the credential's use isolated to the
// smallest, most obviously-named package possible is a deliberate
// mitigation for that - see design.md in
// add-node-connectivity-timestamp-and-cert-status. It also implements
// sign/revoke/clean (see design.md in add-node-certificate-management)
// - the credential already grants that, so isolating its *use* to this
// one package is the actual mitigation, not artificially withholding
// capability the credential already has.
package certstatus

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
)

// Config configures the CA-client connection to openvoxserver's CA API.
type Config struct {
	URL      string // e.g. "https://openvoxserver:8140"
	CertFile string // CA-client certificate (pp_cli_auth extension)
	KeyFile  string
	CAFile   string // CA used to verify openvoxserver's own server cert
}

// Client queries certificate status from openvoxserver's CA API.
type Client struct {
	baseURL string
	http    *http.Client
}

// New builds a Client from cfg, loading its TLS credentials.
func New(cfg Config) (*Client, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load CA-client certificate: %w", err)
	}
	caPEM, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read CA-client CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no certificates found in CA-client CA file %s", cfg.CAFile)
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

// newClient builds a Client directly from an already-configured
// http.Client - split out from New so tests can inject a plain-HTTP
// httptest.Server without needing a real mTLS handshake (the TLS
// wiring itself is standard library behavior; what this package's own
// tests need to cover is response parsing).
func newClient(baseURL string, httpClient *http.Client) *Client {
	return &Client{baseURL: baseURL, http: httpClient}
}

type certStatusEntry struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

// Statuses returns a certname -> state ("signed", "requested",
// "revoked") map for every certificate the CA knows about, fetched in
// one bulk request - openvoxserver's own API is already bulk-shaped, so
// this avoids an N+1 request pattern the upstream API doesn't require.
func (c *Client) Statuses(ctx context.Context) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/puppet-ca/v1/certificate_statuses/all", nil)
	if err != nil {
		return nil, fmt.Errorf("build cert status request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request cert statuses: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cert status request failed: %s", resp.Status)
	}

	var entries []certStatusEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode cert statuses: %w", err)
	}

	statuses := make(map[string]string, len(entries))
	for _, e := range entries {
		statuses[e.Name] = e.State
	}
	return statuses, nil
}

// CRL returns the CA's current certificate revocation list, PEM-encoded,
// as openvoxserver serves it to agents. The endpoint needs no
// authorization, but is fetched with this client's credentials all the
// same - they are what verify openvoxserver's own certificate.
func (c *Client) CRL(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/puppet-ca/v1/certificate_revocation_list/ca", nil)
	if err != nil {
		return nil, fmt.Errorf("build CRL request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request CRL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CRL request failed: %s", resp.Status)
	}
	// A CRL is small; anything past a few megabytes is not one.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("read CRL: %w", err)
	}
	return data, nil
}

// WrongStateError reports that a sign or revoke action was rejected
// because the target certificate is not currently in the state that
// action requires (e.g. signing a cert that isn't "requested").
type WrongStateError struct {
	Certname      string
	CurrentState  string
	RequiredState string
}

func (e *WrongStateError) Error() string {
	return fmt.Sprintf("certificate %s is %s, not %s", e.Certname, e.CurrentState, e.RequiredState)
}

// NotFoundError reports that the CA has no certificate record at all
// for the given certname.
type NotFoundError struct {
	Certname string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no certificate record for %s", e.Certname)
}

// status fetches a single node's current certificate state from the
// per-node status endpoint, used by Sign/Revoke to validate the current
// state before attempting the transition, and by Clean to distinguish
// "nothing to clean" from a generic request failure.
func (c *Client) status(ctx context.Context, certname string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/puppet-ca/v1/certificate_status/"+url.PathEscape(certname), nil)
	if err != nil {
		return "", fmt.Errorf("build cert status request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("request cert status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", &NotFoundError{Certname: certname}
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cert status request failed: %s", resp.Status)
	}

	var entry certStatusEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return "", fmt.Errorf("decode cert status: %w", err)
	}
	return entry.State, nil
}

// setState issues the PUT that transitions certname to desiredState -
// the actual sign/revoke action, once the caller has already validated
// the current state permits it.
func (c *Client) setState(ctx context.Context, certname, desiredState string) error {
	body, err := json.Marshal(map[string]string{"desired_state": desiredState})
	if err != nil {
		return fmt.Errorf("encode cert %s request: %w", desiredState, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/puppet-ca/v1/certificate_status/"+url.PathEscape(certname), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build cert %s request: %w", desiredState, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request cert %s: %w", desiredState, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("cert %s request failed: %s", desiredState, resp.Status)
	}
	return nil
}

// Sign approves certname's pending certificate request. It fails with a
// *WrongStateError if the certificate is not currently "requested", and
// a *NotFoundError if the CA has no record of certname.
func (c *Client) Sign(ctx context.Context, certname string) error {
	state, err := c.status(ctx, certname)
	if err != nil {
		return err
	}
	if state != "requested" {
		return &WrongStateError{Certname: certname, CurrentState: state, RequiredState: "requested"}
	}
	return c.setState(ctx, certname, "signed")
}

// Revoke revokes certname's currently signed certificate. It fails with
// a *WrongStateError if the certificate is not currently "signed", and
// a *NotFoundError if the CA has no record of certname.
func (c *Client) Revoke(ctx context.Context, certname string) error {
	state, err := c.status(ctx, certname)
	if err != nil {
		return err
	}
	if state != "signed" {
		return &WrongStateError{Certname: certname, CurrentState: state, RequiredState: "signed"}
	}
	return c.setState(ctx, certname, "revoked")
}

// Clean revokes certname's certificate if still signed, then removes
// the CA's record of it entirely, freeing the certname to submit a new,
// unsigned request. It fails with a *NotFoundError if the CA has no
// record of certname to clean.
func (c *Client) Clean(ctx context.Context, certname string) error {
	if _, err := c.status(ctx, certname); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/puppet-ca/v1/certificate_status/"+url.PathEscape(certname), nil)
	if err != nil {
		return fmt.Errorf("build cert clean request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request cert clean: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("cert clean request failed: %s", resp.Status)
	}
	return nil
}
