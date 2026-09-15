package nodetransport

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// internalAdminConnName identifies the console's own internal client used
// to consume $SYS.> connect/disconnect events, distinguishing it (via the
// NATS CONNECT protocol's "name" field) from the console's other,
// unprivileged in-process client (see server.go) - auth.go's Check grants
// only a connection with this exact name access to the system account.
const internalAdminConnName = "nodetransport-internal-admin"

// Registry tracks which nodes currently hold a live connection, fed by
// the embedded NATS server's $SYS.ACCOUNT.<account>.CONNECT/.DISCONNECT
// events.
//
// The connected-state *map* is observability only (a
// console_node_agent_connected-style metric, and this package's own
// Lookup/Len) - dispatch does not consult it (see dispatch.go: it relies
// on NATS's own no-responders signal instead), so a brief disagreement
// between this map and reality during a connect/disconnect race is never
// load-bearing for dispatch correctness.
//
// Connect *notifications* are different, and are load-bearing: the same
// CONNECT events also drive Config.OnNodeConnect, which triggers a
// node's initial Puppet run. That path is deliberately built to tolerate
// what this source can do to it - the same notification arriving twice,
// or not arriving at all - by claiming each certname atomically in
// Postgres before dispatching, and by accepting that a node which
// connects while the console is down simply waits for its own scheduled
// run. Do not add a consumer here that needs exactly-once delivery.
type Registry struct {
	mu        sync.Mutex
	connected map[string]bool
	history   map[string]nodeHistory
}

// nodeHistory is deliberately just the two most recent timestamps, not a
// full connect/disconnect log - see design.md in
// add-node-connectivity-timestamp-and-cert-status's Non-Goals.
type nodeHistory struct {
	lastConnected    *time.Time
	lastDisconnected *time.Time
}

func newRegistry() *Registry {
	return &Registry{connected: map[string]bool{}, history: map[string]nodeHistory{}}
}

// Lookup reports whether certname currently holds a live connection.
func (r *Registry) Lookup(certname string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.connected[certname]
}

// Len reports the current connected-node count.
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.connected)
}

// KnownCertnames returns every certname this Registry has any knowledge
// of - currently connected, or with connect/disconnect history from
// earlier in this instance's lifetime - in no particular order. A
// certname this instance has never observed a connection from at all is
// not included (there is nothing to report for it - see
// add-node-connectivity-timestamp-and-cert-status's design.md).
func (r *Registry) KnownCertnames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := make(map[string]struct{}, len(r.connected)+len(r.history))
	for certname := range r.connected {
		seen[certname] = struct{}{}
	}
	for certname := range r.history {
		seen[certname] = struct{}{}
	}
	certnames := make([]string, 0, len(seen))
	for certname := range seen {
		certnames = append(certnames, certname)
	}
	return certnames
}

// LastConnected reports the timestamp of certname's most recent
// connection to this instance, or nil if it has never connected.
func (r *Registry) LastConnected(certname string) *time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.history[certname].lastConnected
}

// LastDisconnected reports the timestamp of certname's most recent
// disconnection from this instance, or nil if it has never disconnected
// (either it has never connected, or it is still connected right now).
func (r *Registry) LastDisconnected(certname string) *time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.history[certname].lastDisconnected
}

func (r *Registry) markConnected(certname string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connected[certname] = true
	now := time.Now()
	h := r.history[certname]
	h.lastConnected = &now
	r.history[certname] = h
}

func (r *Registry) markDisconnected(certname string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.connected, certname)
	now := time.Now()
	h := r.history[certname]
	h.lastDisconnected = &now
	r.history[certname] = h
}

// notifyConnect delivers a connect notification without letting the
// observer affect the transport. Two protections, both deliberate:
//
//   - its own goroutine, so an observer that blocks cannot stall this
//     NATS callback, and through it the subscription that feeds the
//     connected-state map for every other node;
//   - a recover, because a panic on a NATS callback goroutine would
//     otherwise take the whole console process down over a bug in a
//     consumer of an advisory notification.
//
// A panicking observer is a bug worth surfacing, but this package has no
// logger, and the callers that matter log for themselves - so it is
// swallowed here rather than printed to stderr from library code.
func notifyConnect(onConnect func(certname string), certname string) {
	if onConnect == nil {
		return
	}
	go func() {
		defer func() { _ = recover() }()
		onConnect(certname)
	}()
}

// connectEvent/disconnectEvent decode just the field this package needs
// from nats-server's server.ConnectEventMsg/DisconnectEventMsg - the
// certname a node authenticated as, carried in ClientInfo.User because
// auth.go's Check registers each node with Username: certname.
type systemEvent struct {
	Client struct {
		User string `json:"user"`
	} `json:"client"`
}

// startRegistry subscribes an internal client (connected as the system
// account, via auth.go's special-casing of internalAdminConnName, so it
// can see $SYS.> events that a regular global-account connection cannot)
// to connect/disconnect events for accName - the account nodes actually
// connect into (the default global account) - feeding a Registry.
func startRegistry(ns *server.Server, accName string, onConnect func(certname string)) (*Registry, *nats.Conn, error) {
	conn, err := nats.Connect("", nats.InProcessServer(ns), nats.Name(internalAdminConnName))
	if err != nil {
		return nil, nil, fmt.Errorf("connect internal registry client: %w", err)
	}

	reg := newRegistry()

	if _, err := conn.Subscribe(fmt.Sprintf("$SYS.ACCOUNT.%s.CONNECT", accName), func(msg *nats.Msg) {
		var evt systemEvent
		if json.Unmarshal(msg.Data, &evt) == nil && evt.Client.User != "" {
			reg.markConnected(evt.Client.User)
			notifyConnect(onConnect, evt.Client.User)
		}
	}); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("subscribe to node connect events: %w", err)
	}

	if _, err := conn.Subscribe(fmt.Sprintf("$SYS.ACCOUNT.%s.DISCONNECT", accName), func(msg *nats.Msg) {
		var evt systemEvent
		if json.Unmarshal(msg.Data, &evt) == nil && evt.Client.User != "" {
			reg.markDisconnected(evt.Client.User)
		}
	}); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("subscribe to node disconnect events: %w", err)
	}

	return reg, conn, nil
}
