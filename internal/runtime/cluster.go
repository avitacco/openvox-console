package runtime

import (
	"fmt"
	"strings"
)

// ClusterMode is how an instance joins the internal event bus of the
// other instances.
//
// The distinction exists because the core modes and the edge modes need
// different things from the cluster. A web or orchestrator instance both
// publishes and subscribes, and is reachable by its peers, so it is a
// full routed peer. An enc instance only needs to *receive* - revocation
// events, so it can reject a revoked service token - and typically runs
// somewhere the console core has no route back to, next to a compiler.
// Making that instance a full peer would require every core instance to
// reach it, for no benefit.
type ClusterMode string

const (
	// ClusterModeNone is a single, unclustered instance: no peer
	// listener, no peers. The default, and exactly the behavior the
	// binary had before run modes existed.
	ClusterModeNone ClusterMode = ""

	// ClusterModeRoute makes this instance a full cluster peer, meshed
	// with every other routed peer.
	ClusterModeRoute ClusterMode = "route"

	// ClusterModeLeaf attaches this instance to the cluster as an edge
	// subscriber. The connection is outbound-only, so the core needs no
	// network path back to it.
	ClusterModeLeaf ClusterMode = "leaf"
)

var clusterModes = []ClusterMode{ClusterModeRoute, ClusterModeLeaf}

// ClusterModes returns every configurable cluster mode. ClusterModeNone
// is excluded: it is the absence of configuration, not a value an
// operator sets.
func ClusterModes() []ClusterMode {
	out := make([]ClusterMode, len(clusterModes))
	copy(out, clusterModes)
	return out
}

// String returns the cluster mode's configuration spelling.
func (c ClusterMode) String() string { return string(c) }

// ParseClusterMode maps a CONSOLE_CLUSTER_MODE value to a ClusterMode.
// Empty means unclustered.
func ParseClusterMode(s string) (ClusterMode, error) {
	if s == "" {
		return ClusterModeNone, nil
	}
	for _, c := range clusterModes {
		if s == string(c) {
			return c, nil
		}
	}
	return "", &InvalidClusterModeError{Value: s}
}

// InvalidClusterModeError reports an unrecognized cluster mode string.
type InvalidClusterModeError struct {
	Value string
}

func (e *InvalidClusterModeError) Error() string {
	names := make([]string, len(clusterModes))
	for i, c := range clusterModes {
		names[i] = fmt.Sprintf("%q", c)
	}
	return fmt.Sprintf("invalid cluster mode %q: must be one of %s, or unset for a single instance",
		e.Value, strings.Join(names, ", "))
}

// PeerList splits ClusterPeers into individual "host:port" entries,
// ignoring empty entries so a trailing comma is not an error.
func (c Config) PeerList() []string {
	var peers []string
	for _, p := range strings.Split(c.ClusterPeers, ",") {
		if p = strings.TrimSpace(p); p != "" {
			peers = append(peers, p)
		}
	}
	return peers
}

// Clustered reports whether this instance participates in a cluster at
// all - either by listening for peers, or by dialling them.
func (c Config) Clustered() bool {
	return c.ClusterAddr != "" || c.ClusterLeafAddr != "" || len(c.PeerList()) > 0
}

// EffectiveClusterMode resolves how this instance joins, defaulting an
// unset CONSOLE_CLUSTER_MODE to "route" when clustering is configured at
// all. Routing is the right default: it is what the core modes need, and
// a leaf attachment is the deliberate exception an operator opts into for
// an edge instance.
func (c Config) EffectiveClusterMode() ClusterMode {
	if !c.Clustered() {
		return ClusterModeNone
	}
	if c.ClusterMode == ClusterModeNone {
		return ClusterModeRoute
	}
	return c.ClusterMode
}
