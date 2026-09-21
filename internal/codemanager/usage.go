package codemanager

import (
	"context"
	"errors"
)

// errUsageUnavailable reports that node usage could not be computed
// because the data it needs is not wired up - code deployment running
// without an openvoxdb client, for instance.
var errUsageUnavailable = errors.New("node usage data is unavailable")

// NodeEnvironment is one node and the environment it belongs to under
// whichever of the two readings the caller is supplying. Environment is
// nil for a node that has none - never reported, or unclassified.
type NodeEnvironment struct {
	Certname    string
	Environment *string
}

// NodeEnvironmentsFunc returns one such entry per node.
//
// A function rather than an interface so that neither internal/openvoxdb
// nor internal/classifier is imported here: both readings are assembled
// by the caller and handed in, the same way recordActivity and actor
// are (see NewHandlers). It also means the two readings fail
// independently of each other at the point they are produced.
type NodeEnvironmentsFunc func(ctx context.Context) ([]NodeEnvironment, error)

// NodeUsage is the two per-environment tallies the repository overview
// reports, kept separate because their divergence is the point - a
// repository with assigned nodes and no reporting ones has deployed
// code nothing has run yet; the reverse means nodes are running code no
// group directs them to. See design.md in add-code-repository-overview.
type NodeUsage struct {
	Assigned  EnvironmentCounts
	Reporting EnvironmentCounts
}

// UsageResolver computes both tallies from the two readings.
type UsageResolver struct {
	assigned  NodeEnvironmentsFunc
	reporting NodeEnvironmentsFunc
}

// NewUsageResolver builds a UsageResolver.
//
// assigned should supply each node's *effective* classification
// environment - the result of merging every matching group by priority
// - so that a node matching several groups is counted once, against the
// one environment it would actually be sent to. reporting should supply
// the environment each node's most recent report ran in.
func NewUsageResolver(assigned, reporting NodeEnvironmentsFunc) *UsageResolver {
	return &UsageResolver{assigned: assigned, reporting: reporting}
}

// Usage returns both tallies.
//
// An error here is never fatal to the overview: the caller reports the
// counts as unavailable and still returns each repository's remote,
// prefix, size and last deploy, none of which depend on node data.
func (u *UsageResolver) Usage(ctx context.Context) (NodeUsage, error) {
	if u == nil || u.assigned == nil || u.reporting == nil {
		return NodeUsage{}, errUsageUnavailable
	}

	assigned, err := u.assigned(ctx)
	if err != nil {
		return NodeUsage{}, err
	}
	reporting, err := u.reporting(ctx)
	if err != nil {
		return NodeUsage{}, err
	}

	var usage NodeUsage
	for _, node := range assigned {
		usage.Assigned.tally(node.Environment)
	}
	for _, node := range reporting {
		usage.Reporting.tally(node.Environment)
	}
	return usage, nil
}
