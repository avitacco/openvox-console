package taskcatalog

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// environmentCachePath is openvoxserver's admin endpoint that drops its
// in-memory copy of an environment's code.
const environmentCachePath = "/puppet-admin-api/v1/environment-cache"

// FlushEnvironmentCache tells openvoxserver to drop its cached copy of
// environment, so the next compile (and the next class read) loads the
// code now on disk. With environment_timeout = unlimited - the container
// image's setting - the server otherwise keeps compiling the code it
// first loaded until it restarts, however many deploys happen meanwhile.
//
// The stock auth.conf allows any authenticated certificate to make this
// request ("puppet admin api clear caches"); a server whose auth.conf is
// stricter answers with a ForbiddenError.
func (c *Client) FlushEnvironmentCache(ctx context.Context, environment string) error {
	if c == nil {
		return ErrNotConfigured
	}
	u := c.baseURL + environmentCachePath + "?" + url.Values{"environment": {environment}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &UnreachableError{Err: err}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponse))
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		return nil
	case http.StatusForbidden:
		return &ForbiddenError{Path: environmentCachePath, Method: http.MethodDelete}
	default:
		return &UnreachableError{Err: fmt.Errorf("DELETE %s: %s", environmentCachePath, resp.Status)}
	}
}
