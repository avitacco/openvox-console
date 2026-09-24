// Package stackstatus reports the live state of the running stack: every
// console instance with its role, location, health and background
// workers, plus the external services the console depends on.
//
// It exists because a multi-instance deployment is otherwise invisible
// from the console itself. Which modes are running, how many of each,
// where they are and whether each is healthy were answerable only by
// reading deployment configuration and probing each instance in turn -
// which requires already knowing every instance's address, the thing you
// were trying to find out.
//
// The picture is gathered live over the internal bus rather than read
// from a stored record, because the question is "what is running right
// now" and a stored record answers "what wrote a row recently" - the
// same thing only until it matters.
package stackstatus

import (
	"context"
	"net/url"
	"strings"
	"time"
)

// Health is an instance's or dependency's condition.
type Health string

const (
	// HealthOK means every check this instance runs is passing.
	HealthOK Health = "healthy"

	// HealthDegraded means the instance is serving but at least one of
	// its dependencies is failing. Deliberately distinct from
	// unreachable: the instance is answering, which is itself
	// information.
	HealthDegraded Health = "degraded"

	// HealthUnreachable means a dependency could not be reached at all.
	HealthUnreachable Health = "unreachable"

	// HealthNotConfigured means an optional dependency was never
	// configured. Distinct from unreachable because they mean different
	// things to an operator: the first is a deployment choice, the
	// second is a fault.
	HealthNotConfigured Health = "not-configured"
)

// Attachment is how an instance is joined to the others.
type Attachment string

const (
	// AttachmentStandalone is an instance with no peers configured.
	AttachmentStandalone Attachment = "standalone"

	// AttachmentRouted is a full cluster peer, meshed with every other
	// routed instance.
	AttachmentRouted Attachment = "routed"

	// AttachmentLeaf is an edge instance attached outbound to one peer.
	// Visible only to that peer, which is why ClusterView exists.
	AttachmentLeaf Attachment = "leaf"
)

// ClusterView is one instance's view of the cluster from where it sits.
//
// No single instance sees the whole cluster: a leaf is visible only to
// the peer it attached to. Each view is a piece of the picture, and the
// aggregator combines them - see ExpectedInstances.
type ClusterView struct {
	Attachment Attachment `json:"attachment"`

	// RoutedPeers is how many distinct peer servers this instance is
	// routed to, not how many connections it holds to them.
	RoutedPeers int `json:"routedPeers"`

	// LeafConnections is how many leaf instances are attached to this
	// instance specifically.
	LeafConnections int `json:"leafConnections"`
}

// Dependency is an external service the console relies on.
type Dependency struct {
	Name string `json:"name"`

	// Target is what this instance is configured to reach the
	// dependency at, so an operator can tell a misconfiguration from an
	// outage. Empty when the dependency is not configured.
	Target string `json:"target,omitempty"`

	Health Health `json:"health"`

	// Detail carries the failure when Health is not OK.
	Detail string `json:"detail,omitempty"`
}

// Instance is one console instance's report of itself.
type Instance struct {
	// ID is generated per process, so two instances on one host with the
	// same address are still distinguishable.
	ID string `json:"id"`

	Hostname string `json:"hostname"`

	// Mode is the run mode, as the run-modes capability defines it.
	Mode string `json:"mode"`

	// Address is where this instance serves HTTP. What you reach it at,
	// not what it is - hence ID above.
	Address string `json:"address"`

	Health Health `json:"health"`

	// StartedAt is when the process started. A timestamp rather than a
	// computed duration: a duration computed on the sender is wrong by
	// however long the reply took, and timestamps survive being passed
	// around. It also makes clock skew between instances visible rather
	// than hiding it.
	StartedAt time.Time `json:"startedAt"`

	Version string `json:"version"`

	// Workers are the background workers this instance runs.
	Workers []string `json:"workers"`

	// Cluster is this instance's own view, used to work out who else
	// should have answered.
	Cluster ClusterView `json:"cluster"`

	// Dependencies are the external services as this instance sees
	// them. Reported per-instance because "openvoxdb is unreachable from
	// web-2" is a more useful statement than "openvoxdb is unreachable".
	Dependencies []Dependency `json:"dependencies"`
}

// Checker is the dependency-check interface, matching
// internal/runtime.Checker so the same implementations serve both the
// health endpoint and this package without either diverging from the
// other.
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

// RedactTarget removes any credential from a connection target before it
// is reported.
//
// This exists because the first thing the status page displayed was the
// Postgres DSN verbatim - password included - to every holder of
// status:read. A target is shown so an operator can tell a
// misconfiguration from an outage, which needs the host, port and
// database name and nothing else.
//
// Both DSN shapes Postgres accepts are handled: the URL form
// (postgres://user:password@host/db) and the keyword form
// (host=... password=...).
func RedactTarget(target string) string {
	if target == "" {
		return ""
	}

	if u, err := url.Parse(target); err == nil && u.Scheme != "" && u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "REDACTED")
		}
		return u.String()
	}

	// Keyword form: rewrite any password=... field, leaving the rest.
	if strings.Contains(target, "password=") {
		fields := strings.Fields(target)
		for i, f := range fields {
			if strings.HasPrefix(f, "password=") {
				fields[i] = "password=REDACTED"
			}
		}
		return strings.Join(fields, " ")
	}

	return target
}
