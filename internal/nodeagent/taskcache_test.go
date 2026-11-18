package nodeagent

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/testca"
)

func sha(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// fileServer serves files (path -> content) like openvoxserver's
// file_content endpoint, behind mutual TLS, counting requests. It
// returns the cache's credential paths too.
type fileServer struct {
	addr     string
	requests atomic.Int32
	lastEnv  atomic.Value
	cert     string
	key      string
	ca       string
}

func startFileServer(t *testing.T, files map[string]string) *fileServer {
	t.Helper()
	ca := testca.NewCA(t)
	serverCert, serverKey := ca.Issue(t, "localhost", true)
	fs := &fileServer{ca: ca.PEMFile(t)}
	fs.cert, fs.key = ca.Issue(t, "node.example.com", false)

	pair, err := tls.LoadX509KeyPair(serverCert, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.requests.Add(1)
		fs.lastEnv.Store(r.URL.Query().Get("environment"))
		content, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(content))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	fs.addr = u.Host
	return fs
}

func (fs *fileServer) cache(t *testing.T) *TaskCache {
	t.Helper()
	c, err := NewTaskCache(filepath.Join(t.TempDir(), "cache"), fs.cert, fs.key, fs.ca)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTaskCacheDownloadsAndVerifies(t *testing.T) {
	const content = "#!/bin/sh\necho hi\n"
	fs := startFileServer(t, map[string]string{"/puppet/v3/file_content/tasks/mod/hi.sh": content})
	c := fs.cache(t)
	f := taskFile{Filename: "hi.sh", Path: "/puppet/v3/file_content/tasks/mod/hi.sh", Params: map[string]string{"environment": "staging"}, SHA256: sha(content), Size: int64(len(content))}

	path, err := c.Fetch(context.Background(), fs.addr, f)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != content {
		t.Errorf("cached content = %q", got)
	}
	if env := fs.lastEnv.Load(); env != "staging" {
		t.Errorf("environment sent = %v, want staging", env)
	}

	// A second fetch is served from the cache.
	if _, err := c.Fetch(context.Background(), fs.addr, f); err != nil {
		t.Fatal(err)
	}
	if n := fs.requests.Load(); n != 1 {
		t.Errorf("%d downloads, want 1", n)
	}
}

func TestTaskCacheChecksumMismatchStoresNothing(t *testing.T) {
	fs := startFileServer(t, map[string]string{"/f": "tampered"})
	c := fs.cache(t)
	f := taskFile{Filename: "f.sh", Path: "/f", SHA256: sha("original"), Size: 8}

	_, err := c.Fetch(context.Background(), fs.addr, f)
	var mismatch *ChecksumMismatchError
	if !errors.As(err, &mismatch) || mismatch.Filename != "f.sh" {
		t.Fatalf("err = %v, want a ChecksumMismatchError naming f.sh", err)
	}
	if _, err := os.Stat(c.entryPath(f.SHA256)); !os.IsNotExist(err) {
		t.Error("a mismatched download was left in the cache")
	}
	entries, _ := os.ReadDir(filepath.Dir(c.entryPath(f.SHA256)))
	if len(entries) != 0 {
		t.Errorf("temporary files left behind: %v", entries)
	}
}

func TestTaskCacheOversizedBodyIsAMismatch(t *testing.T) {
	fs := startFileServer(t, map[string]string{"/f": "abc" + strings.Repeat("x", 1000)})
	c := fs.cache(t)
	_, err := c.Fetch(context.Background(), fs.addr, taskFile{Filename: "f", Path: "/f", SHA256: sha("abc"), Size: 3})
	var mismatch *ChecksumMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("err = %v, want a ChecksumMismatchError", err)
	}
}

func TestTaskCacheUnreachableServer(t *testing.T) {
	fs := startFileServer(t, nil)
	c := fs.cache(t)
	_, err := c.Fetch(context.Background(), "127.0.0.1:1", taskFile{Filename: "f", Path: "/f", SHA256: sha("x"), Size: 1})
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("err = %v, want a download error naming the server", err)
	}
	_, err = c.Fetch(context.Background(), fs.addr, taskFile{Filename: "gone", Path: "/gone", SHA256: sha("x"), Size: 1})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v, want a 404", err)
	}
}

func TestTaskCacheRejectsBadChecksums(t *testing.T) {
	fs := startFileServer(t, nil)
	c := fs.cache(t)
	for _, bad := range []string{"", "../../etc", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		if _, err := c.Fetch(context.Background(), fs.addr, taskFile{Filename: "f", Path: "/f", SHA256: bad}); err == nil {
			t.Errorf("sha256 %q accepted", bad)
		}
	}
	if fs.requests.Load() != 0 {
		t.Error("a download was attempted for an invalid checksum")
	}
}

func TestTaskCacheExpire(t *testing.T) {
	fs := startFileServer(t, map[string]string{"/old": "old", "/new": "new"})
	c := fs.cache(t)
	ctx := context.Background()
	oldFile := taskFile{Filename: "old", Path: "/old", SHA256: sha("old"), Size: 3}
	newFile := taskFile{Filename: "new", Path: "/new", SHA256: sha("new"), Size: 3}
	if _, err := c.Fetch(ctx, fs.addr, oldFile); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Fetch(ctx, fs.addr, newFile); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-taskCacheExpiry - time.Hour)
	if err := os.Chtimes(filepath.Dir(c.entryPath(oldFile.SHA256)), stale, stale); err != nil {
		t.Fatal(err)
	}

	c.Expire()
	if _, err := os.Stat(c.entryPath(oldFile.SHA256)); !os.IsNotExist(err) {
		t.Error("an entry unused for longer than the expiry was kept")
	}
	if _, err := os.Stat(c.entryPath(newFile.SHA256)); err != nil {
		t.Error("a recently used entry was removed")
	}
}

func TestTaskCacheHitRefreshesExpiry(t *testing.T) {
	fs := startFileServer(t, map[string]string{"/f": "f"})
	c := fs.cache(t)
	f := taskFile{Filename: "f", Path: "/f", SHA256: sha("f"), Size: 1}
	if _, err := c.Fetch(context.Background(), fs.addr, f); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-taskCacheExpiry - time.Hour)
	_ = os.Chtimes(filepath.Dir(c.entryPath(f.SHA256)), stale, stale)
	if _, err := c.Fetch(context.Background(), fs.addr, f); err != nil {
		t.Fatal(err)
	}
	c.Expire()
	if _, err := os.Stat(c.entryPath(f.SHA256)); err != nil {
		t.Error("an entry just used was expired")
	}
}
