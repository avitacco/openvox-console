package nodescope

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestScope_ZeroValueAllowsNothing(t *testing.T) {
	var s Scope
	if s.All() || s.Allows("web1") {
		t.Error("the zero Scope must allow nothing")
	}
	if got := s.Certnames(); got == nil || len(got) != 0 {
		t.Errorf("Certnames() = %#v, want empty and non-nil", got)
	}
}

func TestScope_Everything(t *testing.T) {
	s := Everything()
	if !s.All() || !s.Allows("anything") {
		t.Error("Everything() must allow every node")
	}
	if s.Certnames() != nil {
		t.Error("Certnames() of a fleet-wide scope must be nil (no restriction)")
	}
}

func TestScope_OfAndFilter(t *testing.T) {
	s := Of("web2", "web1")
	if !s.Allows("web1") || s.Allows("db1") {
		t.Error("Of() allows exactly the named nodes")
	}
	if got := s.Certnames(); !reflect.DeepEqual(got, []string{"web1", "web2"}) {
		t.Errorf("Certnames() = %v, want sorted [web1 web2]", got)
	}
	all := []string{"db1", "web2", "web1"}
	same := func(c string) string { return c }
	if got := Filter(s, all, same); !reflect.DeepEqual(got, []string{"web2", "web1"}) {
		t.Errorf("Filter() = %v, want [web2 web1] in original order", got)
	}
	if got := Filter(Everything(), all, same); !reflect.DeepEqual(got, all) {
		t.Errorf("Filter() with a fleet-wide scope = %v, want everything", got)
	}
}

func TestScope_IntersectCannotWiden(t *testing.T) {
	scoped := Of("web1", "web2").Intersect([]string{"web1", "db1"})
	if !scoped.Allows("web1") || scoped.Allows("web2") || scoped.Allows("db1") {
		t.Errorf("intersection = %v, want only web1", scoped.Certnames())
	}
	fleet := Everything().Intersect([]string{"db1"})
	if fleet.All() || !fleet.Allows("db1") || fleet.Allows("web1") {
		t.Errorf("fleet-wide ∩ [db1] = %v (all=%v), want only db1", fleet.Certnames(), fleet.All())
	}
	if none := Of("web1").Intersect([]string{}); none.Allows("web1") || none.Certnames() == nil {
		t.Error("intersecting with nothing must allow nothing, as an empty non-nil list")
	}
}

type fakeGroups struct {
	calls   int
	members map[int64][]string
	root    int64
	err     error
}

func (f *fakeGroups) resolve(_ context.Context, ids []int64) (bool, []string, error) {
	f.calls++
	if f.err != nil {
		return false, nil, f.err
	}
	var out []string
	for _, id := range ids {
		if id == f.root {
			return true, nil, nil
		}
		out = append(out, f.members[id]...)
	}
	return false, out, nil
}

func TestResolver_NoGroupsIsNoNodesWithoutAsking(t *testing.T) {
	groups := &fakeGroups{}
	scope, err := NewResolver(groups.resolve).For(context.Background(), nil)
	if err != nil || scope.All() || scope.Allows("web1") {
		t.Errorf("For(nil) = %+v, %v; want a scope allowing nothing", scope, err)
	}
	if groups.calls != 0 {
		t.Errorf("resolved %d times for an empty grant, want 0", groups.calls)
	}
}

func TestResolver_UnionAndRoot(t *testing.T) {
	groups := &fakeGroups{root: 1, members: map[int64][]string{2: {"web1"}, 3: {"db1"}}}
	r := NewResolver(groups.resolve)

	union, err := r.For(context.Background(), []int64{2, 3})
	if err != nil || !union.Allows("web1") || !union.Allows("db1") || union.Allows("other") || union.All() {
		t.Errorf("For([2 3]) = %v, %v; want web1 and db1 only", union.Certnames(), err)
	}
	fleet, err := r.For(context.Background(), []int64{2, 1})
	if err != nil || !fleet.All() {
		t.Errorf("For() with the root = all:%v, %v; want fleet-wide", fleet.All(), err)
	}
}

func TestResolver_CachesByGroupSetUntilExpiry(t *testing.T) {
	groups := &fakeGroups{members: map[int64][]string{2: {"web1"}, 3: {"db1"}}}
	r := NewResolver(groups.resolve)
	now := time.Now()
	r.now = func() time.Time { return now }
	ctx := context.Background()

	_, _ = r.For(ctx, []int64{2, 3})
	_, _ = r.For(ctx, []int64{3, 2, 2}) // same set, different order and a repeat
	if groups.calls != 1 {
		t.Errorf("resolved %d times for the same group set, want 1", groups.calls)
	}
	_, _ = r.For(ctx, []int64{2})
	if groups.calls != 2 {
		t.Errorf("a different group set must resolve separately (calls = %d)", groups.calls)
	}

	// Membership changes; the cached answer holds until it expires.
	groups.members[2] = []string{"web1", "web9"}
	if stale, _ := r.For(ctx, []int64{2}); stale.Allows("web9") {
		t.Error("cache should still hold the old membership inside the TTL")
	}
	now = now.Add(CacheTTL)
	if fresh, _ := r.For(ctx, []int64{2}); !fresh.Allows("web9") {
		t.Error("after the TTL the new membership must be seen")
	}
	if len(r.cache) != 1 {
		t.Errorf("expired entries should be dropped; cache holds %d", len(r.cache))
	}
}

func TestResolver_ErrorIsNeverWidenedOrCached(t *testing.T) {
	groups := &fakeGroups{err: errors.New("openvoxdb down"), members: map[int64][]string{2: {"web1"}}}
	r := NewResolver(groups.resolve)

	scope, err := r.For(context.Background(), []int64{2})
	if err == nil {
		t.Fatal("want the resolver's error")
	}
	if scope.All() || scope.Allows("web1") {
		t.Error("a failed resolution must not allow anything")
	}
	groups.err = nil
	if scope, err := r.For(context.Background(), []int64{2}); err != nil || !scope.Allows("web1") {
		t.Errorf("after recovery: %v, %v; the failure must not have been cached", scope.Certnames(), err)
	}
}

func TestFromRequest(t *testing.T) {
	groups := &fakeGroups{members: map[int64][]string{2: {"web1"}}}
	r := NewResolver(groups.resolve)
	req := httptest.NewRequest("GET", "/", nil)

	// No grant recorded: the route was wired without the middleware.
	if scope, err := r.FromRequest(req); err != nil || scope.All() || scope.Allows("web1") {
		t.Errorf("no grant: %+v, %v; want nothing allowed", scope, err)
	}
	fleet, _ := r.FromRequest(req.WithContext(WithGrant(req.Context(), nil, true)))
	if !fleet.All() {
		t.Error("a token stating no scope is fleet-wide")
	}
	scoped, _ := r.FromRequest(req.WithContext(WithGrant(req.Context(), []int64{2}, false)))
	if scoped.All() || !scoped.Allows("web1") || scoped.Allows("db1") {
		t.Errorf("scoped grant = %v, want only web1", scoped.Certnames())
	}
	none, _ := r.FromRequest(req.WithContext(WithGrant(req.Context(), nil, false)))
	if none.All() || none.Allows("web1") {
		t.Error("a permission with no groups allows nothing")
	}
}

func TestScope_Outside(t *testing.T) {
	s := Of("web1", "web2")
	if got := s.Outside([]string{"db2", "web1", "db1", "db2"}); !reflect.DeepEqual(got, []string{"db2", "db1"}) {
		t.Errorf("Outside() = %v, want [db2 db1]: given order, each once", got)
	}
	if got := s.Outside([]string{"web1", "web2"}); len(got) != 0 {
		t.Errorf("Outside() = %v, want none", got)
	}
	if got := Everything().Outside([]string{"anything"}); got != nil {
		t.Errorf("fleet-wide Outside() = %v, want nil", got)
	}
	if got := (Scope{}).Outside([]string{"web1"}); !reflect.DeepEqual(got, []string{"web1"}) {
		t.Errorf("empty scope Outside() = %v, want everything named", got)
	}
}

func TestPermit(t *testing.T) {
	scoped := func(*http.Request) (Scope, error) { return Of("web1"), nil }
	req := httptest.NewRequest("POST", "/", nil)

	rec := httptest.NewRecorder()
	if !Permit(rec, req, scoped, "web1") || rec.Body.Len() != 0 {
		t.Errorf("in-scope action should be permitted with nothing written (wrote %q)", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	if Permit(rec, req, scoped, "web1", "db1", "db2") {
		t.Fatal("an action naming out-of-scope nodes was permitted")
	}
	var refusal Refusal
	if err := json.Unmarshal(rec.Body.Bytes(), &refusal); err != nil {
		t.Fatalf("decode refusal %q: %v", rec.Body.String(), err)
	}
	if rec.Code != 403 || !reflect.DeepEqual(refusal.OutOfScope, []string{"db1", "db2"}) {
		t.Errorf("refusal = %d %+v, want 403 naming db1 and db2", rec.Code, refusal)
	}

	// An unknown scope is never "allowed".
	rec = httptest.NewRecorder()
	failing := func(*http.Request) (Scope, error) { return Scope{}, errors.New("openvoxdb down") }
	if Permit(rec, req, failing, "web1") || rec.Code != 502 {
		t.Errorf("scope error: permitted or status %d, want refused with 502", rec.Code)
	}
}

func TestScope_UnionAndWithin(t *testing.T) {
	a, b := Of("x", "y"), Of("y", "z")
	if got := a.Union(b).Certnames(); len(got) != 3 {
		t.Errorf("union = %v, want x y z", got)
	}
	if got := a.Within(b).Certnames(); len(got) != 1 || got[0] != "y" {
		t.Errorf("within = %v, want y", got)
	}
	if !a.Union(Everything()).All() || !Everything().Union(a).All() {
		t.Error("the union with everything is not everything")
	}
	if got := Everything().Within(a).Certnames(); len(got) != 2 {
		t.Errorf("everything within a = %v, want a", got)
	}
	if got := a.Within(Everything()).Certnames(); len(got) != 2 {
		t.Errorf("a within everything = %v, want a", got)
	}
	if Of().Union(Of()).Allows("x") {
		t.Error("the union of nothing allows a node")
	}
}
