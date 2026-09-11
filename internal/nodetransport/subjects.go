package nodetransport

import "time"

// PingInterval and MaxPingsOut tune how often each side of a node-agent's
// connection pings the other, and how many unanswered pings it tolerates
// before treating the connection as dead - shared by the server (see
// server.go's Options) and internal/nodeagent's client so both directions
// generate keepalive traffic at the same cadence, exported here for the
// same reason DispatchSubject is: one definition rather than each side
// reimplementing it and risking drift.
//
// nats-server/nats.go's own defaults (2min interval, 2 max pings out - up
// to ~4min of total silence before a dead peer is even detected) outlast
// the idle-connection timeout of most NAT gateways, firewalls, and load
// balancers a node-agent's long-lived outbound connection might traverse
// (commonly 60-350s, sometimes lower on stricter corporate firewalls) -
// the connection goes silently stale on the middlebox well before either
// side's ping cycle would ever notice. 30s comfortably undercuts that
// range without pinging so often it's a meaningful cost even across a
// large fleet.
const (
	PingInterval = 30 * time.Second
	MaxPingsOut  = 3
)

// Every subject a given node may ever publish or subscribe to lives under
// its own "node.<certname>." prefix (both the dispatch subject the console
// publishes requests to, and the per-request reply subject the node
// responds on - see dispatch.go) - this is what auth.go's per-node
// Permissions restrict access to, giving the "a node can only ever
// be addressed as itself" property without ever needing to special-case a
// shared reply-subject namespace (e.g. NATS's default "_INBOX.>") that
// every node would otherwise need some access to.

func nodePrefix(certname string) string {
	return "node." + certname + "."
}

// DispatchSubject returns the subject the console publishes dispatch
// requests to for certname, and the subject a node-agent (see
// internal/nodeagent) subscribes to for its own incoming requests -
// exported so both sides share this exact naming scheme rather than
// each independently reimplementing it (unlike the wire payload shapes,
// where each side deliberately has its own Go type - see
// internal/orchestrator/wire.go).
func DispatchSubject(certname string) string {
	return nodePrefix(certname) + "dispatch"
}

// ReplySubject returns the subject a node-agent replies with its
// response on for one dispatch request - see DispatchSubject.
func ReplySubject(certname, requestID string) string {
	return nodePrefix(certname) + "reply." + requestID
}
