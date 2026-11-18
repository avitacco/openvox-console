package classcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
)

// recorded is the response of a real openvoxserver serving
// testdata/modules/class_fixture in "production".
func recorded(t *testing.T) *taskcatalog.EnvironmentClasses {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "api", "environment_classes_production.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out taskcatalog.EnvironmentClasses
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return &out
}

// fakeSource answers like openvoxserver, counting reads.
type fakeSource struct {
	mu    sync.Mutex
	reads int
	etags []string
	resp  *taskcatalog.EnvironmentClasses
	err   error
	block chan struct{}
}

func (f *fakeSource) Environments(context.Context) ([]string, error) {
	return []string{"production", "staging"}, f.err
}

func (f *fakeSource) EnvironmentClasses(_ context.Context, env, etag string) (*taskcatalog.EnvironmentClasses, bool, error) {
	f.mu.Lock()
	f.reads++
	f.etags = append(f.etags, etag)
	resp, err, block := f.resp, f.err, f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	if err != nil {
		return nil, false, err
	}
	if etag != "" && resp != nil && etag == resp.ETag {
		return nil, true, nil
	}
	return resp, false, nil
}

func (f *fakeSource) set(resp *taskcatalog.EnvironmentClasses, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resp, f.err = resp, err
}

func (f *fakeSource) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newCatalog(t *testing.T, src *fakeSource) (*Catalog, *clock) {
	t.Helper()
	cl := &clock{t: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)}
	c := New(src)
	c.now = cl.now
	return c, cl
}

var unreachable = &taskcatalog.UnreachableError{Err: errors.New("connection refused")}

func TestClasses_FromTheRecordedResponse(t *testing.T) {
	c, _ := newCatalog(t, &fakeSource{resp: recorded(t)})
	env, err := c.Classes(context.Background(), "production")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(env.Classes); i++ {
		if env.Classes[i-1].Name > env.Classes[i].Name {
			t.Fatalf("classes not sorted: %q before %q", env.Classes[i-1].Name, env.Classes[i].Name)
		}
	}
	class, ok := env.Class("class_fixture")
	if !ok {
		t.Fatal("class_fixture missing")
	}
	byName := map[string]Parameter{}
	for _, p := range class.Parameters {
		byName[p.Name] = p
	}
	if p := byName["owner"]; !p.Required || p.Type != "String" {
		t.Errorf("owner = %+v, want required String", p)
	}
	if p := byName["port"]; p.Required || string(p.Default) != "8080" || p.DefaultSource != "8080" {
		t.Errorf("port = %+v", p)
	}
	if p := byName["greeting"]; p.Required || p.Default != nil || p.DefaultSource == "" {
		t.Errorf("greeting = %+v, want an expression default", p)
	}
	if _, ok := env.Class("class_fixture::worker"); !ok {
		t.Error("class_fixture::worker missing")
	}
	// The manifest that does not parse is reported, by its path inside
	// the environment, and every other manifest's classes are still here.
	if len(env.Errors) != 1 || env.Errors[0].File != "site/class_fixture/manifests/broken.pp" || env.Errors[0].Error == "" {
		t.Errorf("errors = %+v", env.Errors)
	}
	if env.Stale || env.FetchedAt.IsZero() {
		t.Errorf("stale %v, fetchedAt %v", env.Stale, env.FetchedAt)
	}
}

func TestClasses_ReusedForFiveMinutes(t *testing.T) {
	src := &fakeSource{resp: recorded(t)}
	c, cl := newCatalog(t, src)
	ctx := context.Background()
	c.Classes(ctx, "production")
	cl.advance(Freshness - time.Second)
	c.Classes(ctx, "production")
	if src.count() != 1 {
		t.Fatalf("reads = %d, want 1 within the freshness window", src.count())
	}
	cl.advance(2 * time.Second)
	c.Classes(ctx, "production")
	if src.count() != 2 {
		t.Fatalf("reads = %d, want 2 once the window has passed", src.count())
	}
}

func TestClasses_RevalidatesWithTheServersTag(t *testing.T) {
	resp := recorded(t)
	resp.ETag = `"abc"`
	src := &fakeSource{resp: resp}
	c, cl := newCatalog(t, src)
	ctx := context.Background()
	first, _ := c.Classes(ctx, "production")
	cl.advance(Freshness + time.Second)
	second, err := c.Classes(ctx, "production")
	if err != nil {
		t.Fatal(err)
	}
	if src.etags[1] != `"abc"` {
		t.Errorf("second read sent tag %q", src.etags[1])
	}
	if len(second.Classes) != len(first.Classes) || !second.FetchedAt.After(first.FetchedAt) {
		t.Errorf("a not-modified answer should keep the classes and move fetchedAt on")
	}
	// And it is fresh again.
	c.Classes(ctx, "production")
	if src.count() != 2 {
		t.Errorf("reads = %d, want 2", src.count())
	}
}

func TestExpireAll_ReadsAgain(t *testing.T) {
	src := &fakeSource{resp: recorded(t)}
	c, _ := newCatalog(t, src)
	ctx := context.Background()
	c.Classes(ctx, "production")
	c.Classes(ctx, "staging")
	c.ExpireAll()
	c.Classes(ctx, "production")
	c.Classes(ctx, "staging")
	if src.count() != 4 {
		t.Fatalf("reads = %d, want 4: both environments read again after a deploy", src.count())
	}
}

func TestRefresh_ReadsNow(t *testing.T) {
	src := &fakeSource{resp: recorded(t)}
	c, _ := newCatalog(t, src)
	ctx := context.Background()
	c.Classes(ctx, "production")
	src.set(&taskcatalog.EnvironmentClasses{Name: "production"}, nil)
	env, err := c.Refresh(ctx, "production")
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Classes) != 0 || src.count() != 2 {
		t.Fatalf("refresh returned %d classes after %d reads; want the new, empty answer", len(env.Classes), src.count())
	}
	// The refreshed answer is what is now held.
	env, _ = c.Classes(ctx, "production")
	if len(env.Classes) != 0 || src.count() != 2 {
		t.Errorf("the refreshed answer was not kept")
	}
}

func TestClasses_StaleWhenTheServerGoesAway(t *testing.T) {
	src := &fakeSource{resp: recorded(t)}
	c, cl := newCatalog(t, src)
	ctx := context.Background()
	first, _ := c.Classes(ctx, "production")

	src.set(nil, unreachable)
	cl.advance(Freshness + time.Second)
	env, err := c.Classes(ctx, "production")
	if err != nil {
		t.Fatalf("want the earlier answer, got %v", err)
	}
	if !env.Stale || !env.FetchedAt.Equal(first.FetchedAt) || len(env.Classes) != len(first.Classes) {
		t.Errorf("stale %v fetchedAt %v classes %d", env.Stale, env.FetchedAt, len(env.Classes))
	}
	// The failure is remembered briefly: no second read straight away.
	reads := src.count()
	c.Classes(ctx, "production")
	if src.count() != reads {
		t.Errorf("a failed read was retried at once")
	}
	// Once the server is back the answer is fresh again.
	src.set(recorded(t), nil)
	cl.advance(failureMemo + time.Second)
	env, err = c.Classes(ctx, "production")
	if err != nil || env.Stale {
		t.Errorf("after recovery: stale %v err %v", env.Stale, err)
	}
}

func TestClasses_UnavailableReasons(t *testing.T) {
	cases := []struct {
		err    error
		reason string
	}{
		{taskcatalog.ErrNotConfigured, ReasonNotConfigured},
		{&taskcatalog.ForbiddenError{Path: "/puppet/v3/environment_classes"}, ReasonForbidden},
		{unreachable, ReasonUnreachable},
	}
	for _, tc := range cases {
		c, _ := newCatalog(t, &fakeSource{err: tc.err})
		_, err := c.Classes(context.Background(), "production")
		if Reason(err) != tc.reason {
			t.Errorf("%v: reason = %q, want %q", tc.err, Reason(err), tc.reason)
		}
	}
}

// A server that starts refusing has answered; the earlier classes are
// not served in its place.
func TestClasses_ForbiddenDropsTheEarlierAnswer(t *testing.T) {
	src := &fakeSource{resp: recorded(t)}
	c, cl := newCatalog(t, src)
	ctx := context.Background()
	c.Classes(ctx, "production")
	src.set(nil, &taskcatalog.ForbiddenError{Path: "/puppet/v3/environment_classes"})
	cl.advance(Freshness + time.Second)
	if _, err := c.Classes(ctx, "production"); Reason(err) != ReasonForbidden {
		t.Fatalf("err = %v, want forbidden", err)
	}
}

func TestClasses_ConcurrentRequestsShareOneRead(t *testing.T) {
	src := &fakeSource{resp: recorded(t), block: make(chan struct{})}
	c, _ := newCatalog(t, src)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Classes(context.Background(), "production"); err != nil {
				t.Error(err)
			}
		}()
	}
	// Let every goroutine reach the catalogue before the read returns.
	deadline := time.Now().Add(2 * time.Second)
	for src.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(src.block)
	wg.Wait()
	if src.count() != 1 {
		t.Fatalf("reads = %d, want 1", src.count())
	}
}

func authorizeAs(held ...string) func(string, http.HandlerFunc) http.HandlerFunc {
	return func(permission string, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			for _, p := range held {
				if p == permission {
					next(w, r)
					return
				}
			}
			w.WriteHeader(http.StatusForbidden)
		}
	}
}

func serve(t *testing.T, src *fakeSource, held ...string) (*http.ServeMux, *fakeSource) {
	t.Helper()
	c, _ := newCatalog(t, src)
	mux := http.NewServeMux()
	NewHandlers(c).Register(mux, authorizeAs(held...))
	return mux, src
}

func do(mux *http.ServeMux, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestHandlers(t *testing.T) {
	mux, src := serve(t, &fakeSource{resp: recorded(t)}, "classifier:read", "classifier:write")

	rec := do(mux, "GET", "/api/v1/classes/environments")
	if rec.Code != 200 || rec.Body.String() != "{\"environments\":[\"production\",\"staging\"]}\n" {
		t.Errorf("environments: %d %s", rec.Code, rec.Body)
	}

	rec = do(mux, "GET", "/api/v1/classes?environment=production")
	var env Environment
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || rec.Code != 200 {
		t.Fatalf("classes: %d %v", rec.Code, err)
	}
	if _, ok := env.Class("class_fixture"); !ok || env.Environment != "production" {
		t.Errorf("classes response = %+v", env)
	}

	do(mux, "GET", "/api/v1/classes?environment=production")
	if src.count() != 1 {
		t.Errorf("reads = %d, want 1", src.count())
	}
	if rec = do(mux, "POST", "/api/v1/classes/refresh?environment=production"); rec.Code != 200 || src.count() != 2 {
		t.Errorf("refresh: %d after %d reads", rec.Code, src.count())
	}
}

func TestHandlers_Permissions(t *testing.T) {
	mux, _ := serve(t, &fakeSource{resp: recorded(t)}, "classifier:read")
	if rec := do(mux, "POST", "/api/v1/classes/refresh"); rec.Code != http.StatusForbidden {
		t.Errorf("refresh with read only: %d, want 403", rec.Code)
	}
	mux, _ = serve(t, &fakeSource{resp: recorded(t)}, "nodes:read")
	for _, path := range []string{"/api/v1/classes", "/api/v1/classes/environments"} {
		if rec := do(mux, "GET", path); rec.Code != http.StatusForbidden {
			t.Errorf("%s without classifier:read: %d, want 403", path, rec.Code)
		}
	}
}

func TestHandlers_Unavailable(t *testing.T) {
	cases := []struct {
		err    error
		status int
		reason string
	}{
		{taskcatalog.ErrNotConfigured, 503, ReasonNotConfigured},
		{&taskcatalog.ForbiddenError{Path: "/puppet/v3/environment_classes"}, 503, ReasonForbidden},
		{unreachable, 503, ReasonUnreachable},
		{&taskcatalog.NotFoundError{Kind: "environment", Name: "nosuch"}, 404, ""},
	}
	for _, tc := range cases {
		mux, _ := serve(t, &fakeSource{err: tc.err}, "classifier:read")
		rec := do(mux, "GET", "/api/v1/classes?environment=nosuch")
		var body struct{ Error, Reason string }
		json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != tc.status || body.Reason != tc.reason || body.Error == "" {
			t.Errorf("%v: %d %+v, want %d reason %q", tc.err, rec.Code, body, tc.status, tc.reason)
		}
	}
}
