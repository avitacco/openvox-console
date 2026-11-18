package taskcatalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ChecksumMismatchError reports a downloaded file whose SHA-256 is not
// the one the Puppet server listed for it: it is not run, nor sent
// anywhere.
type ChecksumMismatchError struct {
	Filename  string
	Want, Got string
}

func (e *ChecksumMismatchError) Error() string {
	return fmt.Sprintf("%s does not match its checksum (expected %s, got %s)", e.Filename, e.Want, e.Got)
}

// File downloads the file f names from the Puppet server and returns its
// content, once its size and SHA-256 match what the server listed. It is
// for a caller that has to hand the file to a node itself - an agentless
// target - where a node with an agent would fetch it with its own
// certificate.
func (c *Client) File(ctx context.Context, f ResolvedFile) ([]byte, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	if len(f.SHA256) != 64 {
		return nil, fmt.Errorf("%s: invalid sha256 %q", f.Filename, f.SHA256)
	}
	u := c.baseURL + f.Path
	if len(f.Params) > 0 {
		q := url.Values{}
		for k, v := range f.Params {
			q.Set(k, v)
		}
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Filename, err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &UnreachableError{Err: fmt.Errorf("download %s: %w", f.Filename, err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &UnreachableError{Err: fmt.Errorf("download %s: %s", f.Filename, resp.Status)}
	}
	// One byte past the declared size is enough to tell that the file is
	// not the one resolved, without reading an arbitrarily large body.
	data, err := io.ReadAll(io.LimitReader(resp.Body, f.Size+1))
	if err != nil {
		return nil, &UnreachableError{Err: fmt.Errorf("download %s: %w", f.Filename, err)}
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != f.SHA256 || int64(len(data)) != f.Size {
		return nil, &ChecksumMismatchError{Filename: f.Filename, Want: f.SHA256, Got: got}
	}
	return data, nil
}
