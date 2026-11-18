// Package taskscope decides which tasks a caller may run, and on which
// nodes.
//
// A role (or a service token) carries a list of task patterns: a task
// name, "module::*", or "*". An empty list allows every task. A role is
// also granted on node groups, and the two belong together: a role that
// allows "patch::*" on the web tier says nothing about the database
// tier, and another role of the same user must not widen it. So what a
// caller holds is a list of grants, one per role, each pairing patterns
// with node groups, and the nodes a task may run on are the union of
// the groups of the grants whose patterns match it.
//
// The package is a leaf: rbac validates and issues grants, the
// orchestrator checks them, and neither may import the other.
package taskscope

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/voxpupuli/enterprise-console/internal/nodescope"
)

// Limits on one role's or token's list. The grants travel in the access
// token, which has to stay small enough for the proxies in front of the
// console, so the worst case is bounded here rather than left to grow.
const (
	MaxPatterns      = 20
	MaxPatternLength = 64
)

// Everything is the pattern allowing every task.
const Everything = "*"

var (
	// A task is "module" (a module's init task) or "module::task",
	// each part in the form Puppet gives module and task names.
	exactPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*(::[a-z][a-z0-9_]*)?$`)
	modulePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*::\*$`)
)

// PatternError is a pattern the console will not accept.
type PatternError struct {
	Pattern string
	Reason  string
}

func (e *PatternError) Error() string {
	if e.Pattern == "" {
		return e.Reason
	}
	return fmt.Sprintf("task pattern %q: %s", e.Pattern, e.Reason)
}

// ValidatePatterns refuses a list with too many patterns, or a pattern
// that is not a task name, "module::*" or "*".
func ValidatePatterns(patterns []string) error {
	if len(patterns) > MaxPatterns {
		return &PatternError{Reason: fmt.Sprintf("at most %d patterns are allowed", MaxPatterns)}
	}
	for _, p := range patterns {
		switch {
		case p == "":
			return &PatternError{Reason: "a pattern is empty"}
		case len(p) > MaxPatternLength:
			return &PatternError{Pattern: p, Reason: fmt.Sprintf("longer than %d characters", MaxPatternLength)}
		case p == Everything, exactPattern.MatchString(p), modulePattern.MatchString(p):
		default:
			return &PatternError{Pattern: p, Reason: `must be a task name, "module::*" or "*"`}
		}
	}
	return nil
}

// Unique returns patterns without repeats, in first-seen order, never nil.
func Unique(patterns []string) []string {
	seen := make(map[string]bool, len(patterns))
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// Matches reports whether pattern allows task. Matching is exact and
// case-sensitive. "module::*" allows the module's own task as well as
// each "module::task".
func Matches(pattern, task string) bool {
	switch {
	case pattern == Everything:
		return true
	case strings.HasSuffix(pattern, "::*"):
		module := strings.TrimSuffix(pattern, "::*")
		return task == module || strings.HasPrefix(task, module+"::")
	default:
		return pattern == task
	}
}

// Allows reports whether a list of patterns allows task. An empty list
// allows every task.
func Allows(patterns []string, task string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if Matches(p, task) {
			return true
		}
	}
	return false
}

// Grant is one role's (or one token's) allowlist with the node groups it
// applies on. Fleet is for a grant that applies to every node: a
// service token's, which has no node groups.
type Grant struct {
	Tasks  []string `json:"tasks"`
	Groups []int64  `json:"groups,omitempty"`
	Fleet  bool     `json:"fleet,omitempty"`
}

// Groups resolves node group ids to the nodes in them.
type Groups interface {
	For(ctx context.Context, groupIDs []int64) (nodescope.Scope, error)
}

type resolved struct {
	tasks []string
	nodes nodescope.Scope
}

// Scope is what a caller may run: grants with their node groups already
// resolved to nodes. The zero Scope allows nothing; Unrestricted allows
// every task wherever the caller's node scope reaches.
type Scope struct {
	unrestricted bool
	grants       []resolved
}

// Unrestricted is the scope of a caller whose roles carry no allowlist:
// every task, on every node their node groups cover.
func Unrestricted() Scope { return Scope{unrestricted: true} }

// Restricted reports whether any task is withheld from the caller.
func (s Scope) Restricted() bool { return !s.unrestricted }

// Resolve builds a Scope from grants. A nil list is unrestricted: it is
// what a token written before task permissions existed, or one whose
// roles carry no allowlist, states. An empty non-nil list allows
// nothing.
func Resolve(ctx context.Context, grants []Grant, groups Groups) (Scope, error) {
	if grants == nil {
		return Unrestricted(), nil
	}
	out := Scope{grants: make([]resolved, 0, len(grants))}
	for _, g := range grants {
		nodes := nodescope.Everything()
		if !g.Fleet {
			var err error
			if nodes, err = groups.For(ctx, g.Groups); err != nil {
				return Scope{}, err
			}
		}
		out.grants = append(out.grants, resolved{tasks: g.Tasks, nodes: nodes})
	}
	return out, nil
}

// Allows reports whether the caller may run task on at least one node
// (or, when unrestricted, whether any limit applies at all). It is the
// check that needs no node, and is made before the task is looked up.
func (s Scope) Allows(task string) bool {
	if s.unrestricted {
		return true
	}
	for _, g := range s.grants {
		if Allows(g.tasks, task) {
			return true
		}
	}
	return false
}

// AllowsAnywhere reports whether the caller may run task on at least one
// node: the question a task picker asks. A task whose grants cover no
// node at all (a role with no node groups) is not offered, though
// Allows, which needs no node, accepts it.
func (s Scope) AllowsAnywhere(task string) bool {
	if s.unrestricted {
		return true
	}
	nodes := s.NodesFor(task)
	return nodes.All() || len(nodes.Certnames()) > 0
}

// NodesFor returns the nodes task may be run on: the union of the
// nodes of every grant that allows it, and everything when
// unrestricted. The caller intersects it with their node scope for
// orchestrator:run, which is the other half of the check.
func (s Scope) NodesFor(task string) nodescope.Scope {
	if s.unrestricted {
		return nodescope.Everything()
	}
	nodes := nodescope.Of()
	for _, g := range s.grants {
		if Allows(g.tasks, task) {
			nodes = nodes.Union(g.nodes)
		}
	}
	return nodes
}

// Func is how a handler set asks for its caller's task scope, as
// nodescope.Func is for node scope: handler sets hold one of these so
// they can be built, and tested, without the machinery behind it.
type Func func(req *http.Request) (Scope, error)

// UnrestrictedFunc is the Func of a handler set nobody has given one:
// every caller may run every task, which is what each handler set did
// before task permissions existed. internal/app replaces it on every
// handler set that starts, saves or lists tasks, and a test there fails
// for any it misses.
func UnrestrictedFunc(*http.Request) (Scope, error) { return Unrestricted(), nil }
