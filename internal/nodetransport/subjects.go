package nodetransport

import "time"

// PingInterval and MaxPingsOut tune how often each side of a node-agent's
// connection pings the other, and how many unanswered pings it tolerates
// before treating the connection as dead - shared by the server (see
// listener.go's NodeListener) and internal/nodeagent's client so both directions
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

// Every subject a node may subscribe to lives under its own
// "node.<certname>." prefix - this is what auth.go's per-node
// Permissions restrict it to, giving the "a node can only ever be
// addressed as itself" property. A node publishes nothing at all except
// replies to requests it received, which NATS's response permissions
// (see auth.go) allow without any access to a shared reply namespace.

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

// ResponseWindow is how long a node may take to answer a dispatch. A
// node's permission to publish a reply is granted per request and
// expires after this long, so no Dispatch timeout may exceed it - the
// reply would be refused by the server and the dispatch would wait out
// its timeout for an answer that can never arrive. Comfortably above
// the orchestrator's own longest dispatch timeout.
const ResponseWindow = 2 * time.Hour

// CertificateRevokedSubject is published on the console's internal bus
// (messaging.AccountConsole) when the console revokes or cleans a node's
// certificate, so every instance drops that node's connections - see
// AnnounceCertificateRevoked.
const CertificateRevokedSubject = "nodetransport.certificate.revoked"
