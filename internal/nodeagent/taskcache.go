package nodeagent

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// taskFile is one file of a task request - mirrors internal/taskcatalog's
// ResolvedFile (the wire contract is shared, not the Go type).
type taskFile struct {
	Filename string            `json:"filename"`
	Path     string            `json:"path"`
	Params   map[string]string `json:"params,omitempty"`
	SHA256   string            `json:"sha256"`
	Size     int64             `json:"size"`
}

// ChecksumMismatchError reports a downloaded file whose SHA-256 is not the
// one the console resolved. Nothing is run when this happens.
type ChecksumMismatchError struct {
	Filename  string
	Want, Got string
}

func (e *ChecksumMismatchError) Error() string {
	return fmt.Sprintf("checksum mismatch for %s: expected sha256 %s, downloaded %s", e.Filename, e.Want, e.Got)
}

// taskCacheExpiry is how long an unused cache entry is kept.
const taskCacheExpiry = 14 * 24 * time.Hour

// TaskCache downloads task files from the node's Puppet server and keeps
// them by checksum (design decision 7 in add-task-execution). An entry
// is only ever created by renaming a fully downloaded file whose SHA-256
// was verified, so every file in the cache is one the console vouched
// for.
type TaskCache struct {
	dir    string
	client *http.Client
	now    func() time.Time
}

// NewTaskCache builds a cache in dir that downloads with the node's own
// certificate - the same one its agent and the node transport use.
func NewTaskCache(dir, certFile, keyFile, caFile string) (*TaskCache, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load node certificate: %w", err)
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no certificates found in CA file %s", caFile)
	}
	return newTaskCache(dir, &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      pool,
		}},
	}), nil
}

func newTaskCache(dir string, client *http.Client) *TaskCache {
	return &TaskCache{dir: dir, client: client, now: time.Now}
}

// validSHA256 accepts exactly 64 lowercase hex digits - the checksum is
// also a directory name, so nothing else may reach the filesystem.
func validSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (c *TaskCache) entryPath(sha string) string {
	return filepath.Join(c.dir, sha, "content")
}

// Fetch returns the path of f's verified content, downloading it from
// server ("host:port") unless it is already cached.
func (c *TaskCache) Fetch(ctx context.Context, server string, f taskFile) (string, error) {
	if !validSHA256(f.SHA256) {
		return "", fmt.Errorf("%s: invalid sha256 %q", f.Filename, f.SHA256)
	}
	path := c.entryPath(f.SHA256)
	if _, err := os.Stat(path); err == nil {
		// Mark it used, for expiry.
		now := c.now()
		_ = os.Chtimes(filepath.Dir(path), now, now)
		return path, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create task cache: %w", err)
	}
	u := url.URL{Scheme: "https", Host: server, Path: f.Path}
	if len(f.Params) > 0 {
		q := url.Values{}
		for k, v := range f.Params {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("%s: %w", f.Filename, err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s from %s: %w", f.Filename, server, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s from %s: %s", f.Filename, server, resp.Status)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "download-*")
	if err != nil {
		return "", fmt.Errorf("create task cache file: %w", err)
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed into place
	hash := sha256.New()
	// One byte past the declared size is enough to tell that the file is
	// not the one resolved, without reading an arbitrarily large body.
	_, copyErr := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(resp.Body, f.Size+1))
	closeErr := tmp.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return "", fmt.Errorf("download %s from %s: %w", f.Filename, server, err)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != f.SHA256 {
		return "", &ChecksumMismatchError{Filename: f.Filename, Want: f.SHA256, Got: got}
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", fmt.Errorf("store %s in task cache: %w", f.Filename, err)
	}
	return path, nil
}

// Expire removes cache entries unused for taskCacheExpiry. Errors are
// ignored: a stale entry left behind costs disk space, not correctness.
func (c *TaskCache) Expire() {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	cutoff := c.now().Add(-taskCacheExpiry)
	for _, e := range entries {
		if !e.IsDir() || !validSHA256(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(c.dir, e.Name()))
		}
	}
}
