// Package nodescope limits the node data a caller sees to the nodes in
// the node groups their roles grant (add-group-scoped-node-access).
//
// A role names node groups; a token carries, per permission, the groups
// that permission applies to; and each endpoint that returns node data
// filters it by the caller's groups for the permission that gates that
// endpoint. This package is the last step: it turns "these group ids"
// into "these nodes" at request time, because membership follows facts
// and must not be frozen at login.
//
// It deliberately imports neither the classifier nor rbac. Working out
// which nodes are in a group is internal/groupnodes' job and is handed
// in as a function; reading the caller's groups off their token is done
// where routes are wired (internal/app). That keeps packages which must
// not depend on the classifier - packageinventory, inventory - free to
// take a Scope.
package nodescope

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Scope is the set of nodes a caller may see for one permission.
// The zero Scope allows nothing.
type Scope struct {
	all   bool
	nodes map[string]bool
}

// Everything is the scope of a fleet-wide caller.
func Everything() Scope { return Scope{all: true} }

// Of is the scope of exactly these nodes.
func Of(certnames ...string) Scope {
	nodes := make(map[string]bool, len(certnames))
	for _, c := range certnames {
		nodes[c] = true
	}
	return Scope{nodes: nodes}
}

// All reports whether the scope covers every node, so the caller's
// results need no filtering at all.
func (s Scope) All() bool { return s.all }

// Allows reports whether certname is in scope.
func (s Scope) Allows(certname string) bool { return s.all || s.nodes[certname] }

// Certnames lists the nodes in scope, sorted. It is nil for a scope
// covering every node - "no restriction" - and empty, never nil, for a
// scope covering none, matching what openvoxdb.PackageFilter and the
// SQL filters built from it expect.
func (s Scope) Certnames() []string {
	if s.all {
		return nil
	}
	certnames := make([]string, 0, len(s.nodes))
	for c := range s.nodes {
		certnames = append(certnames, c)
	}
	sort.Strings(certnames)
	return certnames
}

// Intersect narrows the scope to the nodes also in certnames: a
// request's own group or node filter applied on top of the caller's
// scope, never instead of it.
func (s Scope) Intersect(certnames []string) Scope {
	kept := make(map[string]bool, len(certnames))
	for _, c := range certnames {
		if s.Allows(c) {
			kept[c] = true
		}
	}
	return Scope{nodes: kept}
}

// Union returns the scope allowing every node either allows. It is how
// the nodes one task may be run on are gathered from several grants.
func (s Scope) Union(other Scope) Scope {
	if s.all || other.all {
		return Everything()
	}
	nodes := make(map[string]bool, len(s.nodes)+len(other.nodes))
	for c := range s.nodes {
		nodes[c] = true
	}
	for c := range other.nodes {
		nodes[c] = true
	}
	return Scope{nodes: nodes}
}

// Within returns the nodes both scopes allow.
func (s Scope) Within(other Scope) Scope {
	if s.all {
		return other
	}
	if other.all {
		return s
	}
	kept := map[string]bool{}
	for c := range s.nodes {
		if other.nodes[c] {
			kept[c] = true
		}
	}
	return Scope{nodes: kept}
}

// Filter returns the items whose node is in scope, in their original
// order. For a scope covering everything it returns items itself.
func Filter[T any](s Scope, items []T, certname func(T) string) []T {
	if s.all {
		return items
	}
	kept := make([]T, 0, len(items))
	for _, item := range items {
		if s.nodes[certname(item)] {
			kept = append(kept, item)
		}
	}
	return kept
}

// GroupNodes answers which nodes are in any of the given node groups or
// their descendants, as the groups stand now. fleetWide is true when
// the groups include the root, in which case certnames is not needed
// and may be nil. Group ids that do not exist contribute nothing.
type GroupNodes func(ctx context.Context, groupIDs []int64) (fleetWide bool, certnames []string, err error)

// CacheTTL is how long a resolved scope is reused. It bounds how stale
// a scoped caller's view can be after a node changes group, and is the
// reason a scoped page load costs one fleet fact query rather than one
// per request the page makes.
const CacheTTL = 30 * time.Second

// Resolver turns granted node groups into a Scope, caching the answer
// briefly. The cache is derived and disposable - every instance can
// rebuild it from Postgres and openvoxdb - so it does not make an
// instance stateful.
type Resolver struct {
	groupNodes GroupNodes
	ttl        time.Duration
	now        func() time.Time

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	scope   Scope
	expires time.Time
}

// NewResolver builds a Resolver over groupNodes.
func NewResolver(groupNodes GroupNodes) *Resolver {
	return &Resolver{groupNodes: groupNodes, ttl: CacheTTL, now: time.Now, cache: map[string]cached{}}
}

// For resolves the scope of a caller granted groupIDs. No groups is no
// nodes, without asking anything. An error is returned as an error:
// a caller must fail the request, never fall back to showing
// everything.
func (r *Resolver) For(ctx context.Context, groupIDs []int64) (Scope, error) {
	if len(groupIDs) == 0 {
		return Scope{}, nil
	}
	key := cacheKey(groupIDs)
	now := r.now()

	r.mu.Lock()
	if hit, ok := r.cache[key]; ok && now.Before(hit.expires) {
		r.mu.Unlock()
		return hit.scope, nil
	}
	r.mu.Unlock()

	fleetWide, certnames, err := r.groupNodes(ctx, groupIDs)
	if err != nil {
		return Scope{}, err
	}
	scope := Of(certnames...)
	if fleetWide {
		scope = Everything()
	}

	r.mu.Lock()
	for k, entry := range r.cache {
		if !now.Before(entry.expires) {
			delete(r.cache, k)
		}
	}
	r.cache[key] = cached{scope: scope, expires: now.Add(r.ttl)}
	r.mu.Unlock()
	return scope, nil
}

func cacheKey(groupIDs []int64) string {
	ids := append([]int64(nil), groupIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	parts := make([]string, 0, len(ids))
	for i, id := range ids {
		if i > 0 && id == ids[i-1] {
			continue
		}
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ",")
}

type grantKey struct{}

// grant is what a caller's token says about one permission.
type grant struct {
	fleetWide bool
	groupIDs  []int64
}

// WithGrant records on ctx the node groups the caller holds for the
// permission that gated this request. fleetWide is for a token that
// states no scope at all (a service token, or one issued before node
// scope existed).
func WithGrant(ctx context.Context, groupIDs []int64, fleetWide bool) context.Context {
	return context.WithValue(ctx, grantKey{}, grant{fleetWide: fleetWide, groupIDs: groupIDs})
}

// FromRequest resolves the scope of the request's caller, from the
// grant WithGrant recorded. A request with no grant recorded gets a
// scope allowing nothing: a route wired without the scoping middleware
// must fail closed, not open.
func (r *Resolver) FromRequest(req *http.Request) (Scope, error) {
	g, ok := req.Context().Value(grantKey{}).(grant)
	if !ok {
		return Scope{}, nil
	}
	if g.fleetWide {
		return Everything(), nil
	}
	return r.For(req.Context(), g.groupIDs)
}

// Func is how a handler set asks for its caller's scope. Handler sets
// hold one of these rather than a *Resolver so they can be built, and
// tested, without one.
type Func func(req *http.Request) (Scope, error)

// Unscoped is the Func of a handler set nobody has given a resolver:
// every caller sees everything, which is what each handler set did
// before node scope existed. internal/app replaces it on every node-data
// handler set, and the route sweep test fails for any it misses.
func Unscoped(*http.Request) (Scope, error) { return Everything(), nil }

// Outside returns the certnames that are not in scope, in the order
// given, each once. It is the question every action asks before doing
// anything: an action may proceed only when this is empty.
func (s Scope) Outside(certnames []string) []string {
	if s.all {
		return nil
	}
	var outside []string
	seen := map[string]bool{}
	for _, c := range certnames {
		if !s.nodes[c] && !seen[c] {
			seen[c] = true
			outside = append(outside, c)
		}
	}
	return outside
}

// Refusal is the body of the 403 an action answers when it names nodes
// outside the caller's scope.
type Refusal struct {
	Error      string   `json:"error"`
	OutOfScope []string `json:"outOfScope"`
}

// Refuse answers an action that named nodes outside the caller's node
// groups: 403, naming them.
//
// It is the same answer whether a named node exists or not. A name no
// node has is in no group, so for a scoped caller it is outside their
// scope exactly as another team's node is - which is why callers check
// scope before looking anything up, and why this reveals nothing.
func Refuse(w http.ResponseWriter, outside []string) {
	message := "this node is not in the node groups your role can act on: " + outside[0]
	if len(outside) > 1 {
		message = "these nodes are not in the node groups your role can act on: " + strings.Join(outside, ", ")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(Refusal{Error: message, OutOfScope: outside})
}

// Permit is what an action handler calls first. It reports whether the
// request may act on certnames: true when every one is in the caller's
// scope. Otherwise it has already written the response - 403 naming
// the nodes outside scope, or 502 when the scope could not be worked
// out, since an action must never proceed on an unknown scope - and
// the handler returns without doing anything else.
func Permit(w http.ResponseWriter, r *http.Request, scope Func, certnames ...string) bool {
	s, err := scope(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "could not determine which nodes you may act on: " + err.Error()})
		return false
	}
	if outside := s.Outside(certnames); len(outside) > 0 {
		Refuse(w, outside)
		return false
	}
	return true
}
