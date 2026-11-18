package taskcatalog

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/testca"
)

// fixture reads one of the responses recorded from a real openvoxserver
// serving testdata/modules/task_fixture (see task 1.3 in add-task-execution).
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "api", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// fakePuppetServer answers like openvoxserver for the task_fixture module
// in "production", from the recorded responses.
func fakePuppetServer(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env := r.URL.Query().Get("environment")
		switch {
		case r.URL.Path == "/puppet/v3/environments":
			w.Write(fixture(t, "environments.json"))
		case env != "production":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Could not find environment '" + env + "'"))
		case r.URL.Path == "/puppet/v3/tasks":
			w.Write(fixture(t, "tasks.json"))
		case strings.HasPrefix(r.URL.Path, "/puppet/v3/tasks/task_fixture/"):
			name := strings.TrimPrefix(r.URL.Path, "/puppet/v3/tasks/task_fixture/")
			data, err := os.ReadFile(filepath.Join("testdata", "api", "task_"+name+".json"))
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte("Could not find task '" + name + "'"))
				return
			}
			w.Write(data)
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Could not find module"))
		}
	})
}

// newTLSClient starts handler behind mutual TLS issued by a throwaway CA
// and returns a Client built by New, so the credential loading is real.
func newTLSClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	ca := testca.NewCA(t)
	serverCert, serverKey := ca.Issue(t, "localhost", true)
	clientCert, clientKey := ca.Issue(t, "console", false)

	pair, err := tls.LoadX509KeyPair(serverCert, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{pair},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	c, err := New(Config{URL: srv.URL, CertFile: clientCert, KeyFile: clientKey, CAFile: ca.PEMFile(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestEnvironments(t *testing.T) {
	c := newTLSClient(t, fakePuppetServer(t))
	envs, err := c.Environments(context.Background())
	if err != nil {
		t.Fatalf("Environments: %v", err)
	}
	if len(envs) != 1 || envs[0] != "production" {
		t.Errorf("environments = %v, want [production]", envs)
	}
}

func TestTasks(t *testing.T) {
	c := newTLSClient(t, fakePuppetServer(t))
	tasks, err := c.Tasks(context.Background(), "production")
	if err != nil {
		t.Fatalf("Tasks: %v", err)
	}
	byName := map[string]TaskSummary{}
	for _, task := range tasks {
		byName[task.Name] = task
	}
	if len(tasks) != 8 {
		t.Errorf("got %d tasks, want 8: %v", len(tasks), tasks)
	}
	if got := byName["task_fixture::echo"].Description; got != "Echoes its parameters back as JSON" {
		t.Errorf("echo description = %q", got)
	}
	if _, ok := byName["task_fixture"]; !ok {
		t.Error("the module's init task is missing, want it listed as task_fixture")
	}
	for i := 1; i < len(tasks); i++ {
		if tasks[i-1].Name > tasks[i].Name {
			t.Errorf("tasks not sorted: %q before %q", tasks[i-1].Name, tasks[i].Name)
		}
	}
}

func TestTasksUnknownEnvironment(t *testing.T) {
	c := newTLSClient(t, fakePuppetServer(t))
	_, err := c.Tasks(context.Background(), "nope")
	var nf *NotFoundError
	if !errors.As(err, &nf) || nf.Kind != "environment" {
		t.Fatalf("err = %v, want an environment NotFoundError", err)
	}
}

func TestTask(t *testing.T) {
	c := newTLSClient(t, fakePuppetServer(t))
	task, err := c.Task(context.Background(), "production", "task_fixture::multi")
	if err != nil {
		t.Fatalf("Task: %v", err)
	}
	if task.Name != "task_fixture::multi" || task.Environment != "production" {
		t.Errorf("task = %s in %s", task.Name, task.Environment)
	}
	if len(task.Files) != 3 {
		t.Fatalf("got %d files, want 3", len(task.Files))
	}
	helper := task.Files[2]
	if helper.Filename != "task_fixture/files/helper.sh" || helper.URI.Path != "/puppet/v3/file_content/modules/task_fixture/helper.sh" || len(helper.SHA256) != 64 {
		t.Errorf("helper file = %+v", helper)
	}
	if helper.URI.Params["environment"] != "production" {
		t.Errorf("helper params = %v", helper.URI.Params)
	}
}

func TestTaskInitByModuleName(t *testing.T) {
	c := newTLSClient(t, fakePuppetServer(t))
	task, err := c.Task(context.Background(), "production", "task_fixture")
	if err != nil {
		t.Fatalf("Task: %v", err)
	}
	if task.Name != "task_fixture" || task.Files[0].Filename != "init.sh" {
		t.Errorf("task = %+v", task)
	}
}

func TestTaskNotFound(t *testing.T) {
	c := newTLSClient(t, fakePuppetServer(t))
	for _, tc := range []struct {
		env, name, kind string
	}{
		{"production", "task_fixture::nope", "task"},
		{"production", "Not A Task", "task"},
		{"nope", "task_fixture::echo", "environment"},
	} {
		_, err := c.Task(context.Background(), tc.env, tc.name)
		var nf *NotFoundError
		if !errors.As(err, &nf) || nf.Kind != tc.kind {
			t.Errorf("Task(%s, %s): err = %v, want a %s NotFoundError", tc.env, tc.name, err, tc.kind)
		}
	}
}

func TestUnreachable(t *testing.T) {
	c := newTLSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	_, err := c.Tasks(context.Background(), "production")
	var ue *UnreachableError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %v, want an UnreachableError for a 500", err)
	}

	// Nothing listening at all.
	dead := newClient("https://127.0.0.1:1", http.DefaultClient)
	if _, err := dead.Environments(context.Background()); !errors.As(err, &ue) {
		t.Fatalf("err = %v, want an UnreachableError when nothing listens", err)
	}
}

func TestNilClientIsNotConfigured(t *testing.T) {
	var c *Client
	ctx := context.Background()
	if _, err := c.Environments(ctx); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Environments err = %v", err)
	}
	if _, err := c.Tasks(ctx, "production"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Tasks err = %v", err)
	}
	if _, err := c.Task(ctx, "production", "a::b"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Task err = %v", err)
	}
}

func TestClientRejectsUntrustedServer(t *testing.T) {
	// A server whose certificate the configured CA did not issue must be
	// refused, not merely reported as an odd response.
	srv := httptest.NewTLSServer(fakePuppetServer(t))
	t.Cleanup(srv.Close)
	ca := testca.NewCA(t)
	cert, key := ca.Issue(t, "console", false)
	c, err := New(Config{URL: srv.URL, CertFile: cert, KeyFile: key, CAFile: ca.PEMFile(t)})
	if err != nil {
		t.Fatal(err)
	}
	var ue *UnreachableError
	if _, err := c.Environments(context.Background()); !errors.As(err, &ue) {
		t.Fatalf("err = %v, want an UnreachableError for an untrusted server", err)
	}
}

func TestFile(t *testing.T) {
	body := []byte("#!/bin/sh\necho hi\n")
	sum := sha256.Sum256(body)
	good := ResolvedFile{Filename: "t.sh", Path: "/puppet/v3/file_content/tasks/m/t.sh", Params: map[string]string{"environment": "production"},
		SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body))}
	c := newTLSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != good.Path || r.URL.Query().Get("environment") != "production" {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	ctx := context.Background()
	got, err := c.File(ctx, good)
	if err != nil || string(got) != string(body) {
		t.Fatalf("File = %q, %v", got, err)
	}

	bad := good
	bad.SHA256 = strings.Repeat("0", 64)
	var mismatch *ChecksumMismatchError
	if _, err := c.File(ctx, bad); !errors.As(err, &mismatch) {
		t.Errorf("wrong checksum = %v", err)
	}
	long := good
	long.Size = 3 // the server sends more than was listed
	if _, err := c.File(ctx, long); !errors.As(err, &mismatch) {
		t.Errorf("wrong size = %v", err)
	}
	missing := good
	missing.Path = "/nope"
	var unreachable *UnreachableError
	if _, err := c.File(ctx, missing); !errors.As(err, &unreachable) {
		t.Errorf("missing file = %v", err)
	}
	if _, err := c.File(ctx, ResolvedFile{Filename: "x", SHA256: "short"}); err == nil {
		t.Error("an invalid checksum was accepted")
	}
	var none *Client
	if _, err := none.File(ctx, good); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("nil client = %v", err)
	}
}
