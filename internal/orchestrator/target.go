package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
)

// Target says which nodes a job runs on: a list of nodes by name, a PQL
// query, or a node group. Exactly one is set.
//
// The difference matters for node scope. Names are all or nothing: one
// outside the caller's node groups refuses the request, naming it - the
// caller typed it, so that reveals nothing. A query or a group is a
// question, and its answer is narrowed to the caller's nodes without
// saying what else matched; refusing instead would hand the caller the
// names of other teams' nodes that match an arbitrary fact.
type Target struct {
	Nodes []string `json:"nodes,omitempty"`
	Query string   `json:"query,omitempty"`
	Group *int64   `json:"group,omitempty"`
}

// Target source kinds.
const (
	TargetKindNodes = "nodes"
	TargetKindQuery = "query"
	TargetKindGroup = "group"
)

// Kind reports which of the three t is, or an InvalidTargetError when it
// is none or more than one.
func (t Target) Kind() (string, error) {
	var kinds []string
	if len(t.Nodes) > 0 {
		kinds = append(kinds, TargetKindNodes)
	}
	if strings.TrimSpace(t.Query) != "" {
		kinds = append(kinds, TargetKindQuery)
	}
	if t.Group != nil {
		kinds = append(kinds, TargetKindGroup)
	}
	switch len(kinds) {
	case 0:
		return "", &InvalidTargetError{Reason: "at least one target is required: give a list of nodes, a query, or a node group"}
	case 1:
		return kinds[0], nil
	}
	return "", &InvalidTargetError{Reason: "give only one of a list of nodes, a query, or a node group"}
}

// TargetSource records, on a job, the query or node group its targets
// were resolved from. A job given a list of nodes has none.
type TargetSource struct {
	Kind string `json:"kind"`
	// Query is left out for a reader who is not shown every node the job
	// ran on - see Store.GetJobInScope.
	Query     string `json:"query,omitempty"`
	GroupID   int64  `json:"groupId,omitempty"`
	GroupName string `json:"groupName,omitempty"`
}

// InvalidTargetError is a target the console refused: malformed, a query
// that cannot be used, or a node group that does not exist.
type InvalidTargetError struct {
	Reason string
}

func (e *InvalidTargetError) Error() string { return e.Reason }

// OutOfScopeError is a list of nodes naming some outside the caller's
// node groups. Nothing is done for any of them.
type OutOfScopeError struct {
	Outside []string
	// Task is set when the nodes are outside what the caller may run
	// this task on, though they may be inside their node groups: another
	// of their roles covers them but does not allow the task there.
	Task string
}

func (e *OutOfScopeError) Error() string {
	if e.Task != "" {
		if len(e.Outside) == 1 {
			return fmt.Sprintf("your roles do not allow the task %q on this node: %s", e.Task, e.Outside[0])
		}
		return fmt.Sprintf("your roles do not allow the task %q on these nodes: %s", e.Task, strings.Join(e.Outside, ", "))
	}
	if len(e.Outside) == 1 {
		return "this node is not in the node groups the caller can act on: " + e.Outside[0]
	}
	return "these nodes are not in the node groups the caller can act on: " + strings.Join(e.Outside, ", ")
}

// ErrNoNodesMatched is a query or a node group that resolved to no
// nodes the caller may act on. It is deliberately the same whether
// nothing matched or only other teams' nodes did.
var ErrNoNodesMatched = &InvalidTargetError{Reason: "no nodes matched"}

// ErrGroupNotFound is what a GroupLookup returns for a group that does
// not exist.
var ErrGroupNotFound = errors.New("node group not found")

// QueryFunc runs a PQL query. *openvoxdb.Client's Query is one.
type QueryFunc func(ctx context.Context, pql string) ([]map[string]any, error)

// GroupLookup names a node group and the nodes in it now, or returns
// ErrGroupNotFound.
type GroupLookup func(ctx context.Context, id int64) (name string, certnames []string, err error)

// queryTimeout bounds a user's query.
const queryTimeout = 30 * time.Second

// TargetResolver turns a Target into the nodes a job runs on. It is the
// one place that happens - for an on-demand request, the preview and a
// scheduled firing alike - so the three cannot disagree about what a
// target means or what a caller may reach.
type TargetResolver struct {
	query QueryFunc
	group GroupLookup
}

// NewTargetResolver builds a TargetResolver. Either source may be nil,
// in which case targets of that kind are refused as not configured.
func NewTargetResolver(query QueryFunc, group GroupLookup) *TargetResolver {
	return &TargetResolver{query: query, group: group}
}

// Resolve returns the nodes target names for a caller with scope,
// sorted, and for a query or a group where they came from. An empty
// result is not an error here: the caller decides what nothing means.
//
// Refusals are *InvalidTargetError or *OutOfScopeError. Any other error
// is a failure to find out (openvoxdb or Postgres unreachable).
func (r *TargetResolver) Resolve(ctx context.Context, target Target, scope nodescope.Scope) ([]string, *TargetSource, error) {
	kind, err := target.Kind()
	if err != nil {
		return nil, nil, err
	}
	switch kind {
	case TargetKindNodes:
		if outside := scope.Outside(target.Nodes); len(outside) > 0 {
			return nil, nil, &OutOfScopeError{Outside: outside}
		}
		return distinct(target.Nodes), nil, nil
	case TargetKindQuery:
		certnames, err := r.runQuery(ctx, target.Query, scope)
		if err != nil {
			return nil, nil, err
		}
		return narrow(certnames, scope), &TargetSource{Kind: TargetKindQuery, Query: target.Query}, nil
	default:
		if r == nil || r.group == nil {
			return nil, nil, &InvalidTargetError{Reason: "targeting a node group is not available on this console"}
		}
		name, certnames, err := r.group(ctx, *target.Group)
		if errors.Is(err, ErrGroupNotFound) {
			return nil, nil, &InvalidTargetError{Reason: fmt.Sprintf("node group %d no longer exists", *target.Group)}
		}
		if err != nil {
			return nil, nil, fmt.Errorf("resolve node group: %w", err)
		}
		return narrow(certnames, scope), &TargetSource{Kind: TargetKindGroup, GroupID: *target.Group, GroupName: name}, nil
	}
}

// runQuery checks pql, runs it, and reduces its rows to certnames.
func (r *TargetResolver) runQuery(ctx context.Context, pql string, scope nodescope.Scope) ([]string, error) {
	if r == nil || r.query == nil {
		return nil, &InvalidTargetError{Reason: "targeting a query is not available on this console: openvoxdb is not configured"}
	}
	var invalid *openvoxdb.InvalidQueryError
	if err := openvoxdb.CheckUserQuery(pql); errors.As(err, &invalid) {
		return nil, &InvalidTargetError{Reason: "invalid query: " + invalid.Message}
	}
	// Narrowing the answer is not enough for a caller limited to some
	// nodes: a subquery lets a query about their own node depend on a
	// fact of someone else's, one bit at a time. Refused before it is
	// sent, so nothing about other nodes is even computed.
	if !scope.All() && openvoxdb.HasSubquery(pql) {
		return nil, &InvalidTargetError{Reason: "invalid query: subqueries need access to every node, and your role is limited to some node groups; query one entity, such as inventory[certname] { facts.os.family = \"Debian\" }"}
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	rows, err := r.query(ctx, pql)
	if errors.As(err, &invalid) {
		return nil, &InvalidTargetError{Reason: "invalid query: " + invalid.Message}
	}
	if err != nil {
		return nil, err
	}
	certnames := make([]string, 0, len(rows))
	for _, row := range rows {
		certname, ok := row["certname"].(string)
		if !ok || certname == "" {
			return nil, &InvalidTargetError{Reason: "invalid query: it must return certname, as in nodes[certname] { ... }"}
		}
		certnames = append(certnames, certname)
	}
	return certnames, nil
}

// narrow keeps the certnames in scope, each once, sorted.
func narrow(certnames []string, scope nodescope.Scope) []string {
	kept := make([]string, 0, len(certnames))
	for _, c := range certnames {
		if scope.Allows(c) {
			kept = append(kept, c)
		}
	}
	out := distinct(kept)
	sort.Strings(out)
	return out
}

// distinct returns certnames with repeats removed, order kept.
func distinct(certnames []string) []string {
	seen := make(map[string]bool, len(certnames))
	out := make([]string, 0, len(certnames))
	for _, c := range certnames {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}
