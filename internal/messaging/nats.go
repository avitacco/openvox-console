// Package messaging embeds a NATS server in-process and exposes a thin
// publish/subscribe helper for internal components to communicate over,
// without a separately deployed broker.
//
// # Clustering, and the one rule subscribers must follow
//
// A single instance runs unclustered and opens no listener at all. When
// peers are configured, the embedded server opens a listener for *peer
// connections only* and events published anywhere in the cluster reach
// subscribers everywhere in it.
//
// That changes what a subscription means, and there are two kinds:
//
//   - Fan-out (Subscribe): every instance's subscriber receives a copy.
//     Correct when the handler updates state local to its own instance -
//     rbac.Revoker's in-memory revocation cache is the example: every
//     instance must learn about a revocation.
//
//   - Queue (QueueSubscribe): exactly one subscriber in the group
//     receives each message. Correct when the handler has an effect
//     beyond its own instance - writing a row, calling out to something.
//     activity.Recorder is the example: it persists each event to one
//     table, so N fan-out subscribers would write N identical rows.
//
// The rule, for every existing and future subscriber: **if the handler
// does anything other than update this instance's own memory, it must use
// a queue group.** Getting this wrong is invisible on a single instance
// and only shows up as duplicated work once a second one starts.
//
// There is a third pattern, for asking rather than telling:
//
//   - Ask-all (AskAll): publish a question with a reply path and collect
//     the answers within a bounded window. Correct when a caller needs an
//     answer *from* the fleet rather than an effect *on* it - the stack
//     status page asking every instance to describe itself.
//
// AskAll deliberately does not report whether it heard from everyone. It
// cannot: that is a question about how many instances exist, which the
// transport does not know. LocalView exposes what one server can see, and
// a caller that needs a completeness judgement must combine the views of
// the instances that answered - see internal/stackstatus.
package messaging

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// clusterName is shared by every peer: nats-server refuses to route
// between servers whose cluster names disagree, which turns a
// copy-paste configuration error into a clear startup failure rather
// than a silently partitioned bus.
const clusterName = "openvox-console"

// peerUser is the account name peer connections authenticate as. The
// password is the operator-supplied cluster secret.
const peerUser = "console-peer"

// readyTimeout bounds how long StartWith waits for the embedded server
// to become ready.
const readyTimeout = 90 * time.Second

// Config describes how this instance joins other instances' buses.
// The zero value is a single, unclustered, listener-less instance.
type Config struct {
	// ListenAddr is this instance's own peer listener ("host:port").
	// Empty means it does not accept peer connections - correct for a
	// leaf, which only dials out.
	ListenAddr string

	// LeafListenAddr, when set, opens a listener for edge instances
	// attaching as leaves. Left empty, no leafnode listener is opened at
	// all - most deployments have no leaves, and a listener nothing uses
	// is surface for no benefit.
	LeafListenAddr string

	// Peers are the addresses of other instances to connect to: peer
	// route listeners for a routed instance, peer *leaf* listeners for a
	// leaf.
	Peers []string

	// Leaf attaches this instance as an edge subscriber rather than a
	// full routed peer.
	Leaf bool

	// Secret authenticates peer connections. Required whenever
	// ListenAddr or Peers is set.
	Secret string
}

// clustered reports whether any peering is configured.
func (c Config) clustered() bool { return c.ListenAddr != "" || len(c.Peers) > 0 }

// Bus wraps an embedded NATS server and a client connection to it, used
// for internal publish/subscribe. When peers are configured, that bus
// spans every instance in the cluster.
type Bus struct {
	server   *server.Server
	conn     *nats.Conn
	leafAddr string
}

// Start boots an unclustered, listener-less embedded NATS server - the
// single-instance case, and the default.
func Start() (*Bus, error) {
	return StartWith(Config{})
}

// StartWith boots an embedded NATS server configured by cfg and connects
// an internal client to it. It blocks until the server is ready or the
// timeout elapses.
func StartWith(cfg Config) (*Bus, error) {
	opts, err := options(cfg)
	if err != nil {
		return nil, err
	}

	ns, err := server.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("create embedded NATS server: %w", err)
	}

	ns.Start()
	// Generous, for the same reason internal/nodetransport's is: this
	// only bounds how long a slow or contended start-up may take before
	// giving up, not any steady-state behavior. A clustered server has
	// listeners to bind and routes to establish where an unclustered one
	// had neither, so the old 10s - chosen when this server never
	// listened at all - is no longer the right bound.
	if !ns.ReadyForConnections(readyTimeout) {
		return nil, fmt.Errorf("embedded NATS server did not become ready in time")
	}

	// The console's own client always connects in-process, with no
	// network hop and no TLS negotiation, whether or not a peer listener
	// is open - see nats.InProcessServer. It still authenticates: a
	// clustered server requires credentials of every client, in-process
	// ones included.
	connOpts := []nats.Option{nats.InProcessServer(ns)}
	if cfg.clustered() {
		connOpts = append(connOpts, nats.UserInfo(peerUser, cfg.Secret))
	}
	conn, err := nats.Connect("", connOpts...)
	if err != nil {
		ns.Shutdown()
		return nil, fmt.Errorf("connect to embedded NATS server: %w", err)
	}

	return &Bus{server: ns, conn: conn, leafAddr: cfg.LeafListenAddr}, nil
}

// options builds the nats-server options for cfg.
func options(cfg Config) (*server.Options, error) {
	if !cfg.clustered() {
		// Unclustered: in-process only, no external listener of any
		// kind. This is what a single instance has always done.
		return &server.Options{DontListen: true}, nil
	}

	if cfg.Secret == "" {
		return nil, fmt.Errorf("cluster secret is required when peering is configured")
	}

	// A clustered server cannot use DontListen. nats-server starts its
	// routing goroutine only after the client accept loop signals that
	// it is up, and DontListen skips that accept loop entirely - so the
	// route listener would never open and the server would never become
	// ready. Verified against nats-server v2.14.5 (server.Start's
	// clientListenReady handoff).
	//
	// So the client port is opened, but bound to loopback on an
	// ephemeral port and protected by the same credentials peers use.
	// Nothing is expected to arrive on it: the console's own connection
	// is in-process, and peers use the route/leafnode listeners. The
	// credentials are what keep another process on the same host from
	// reading every internal event.
	opts := &server.Options{
		Host:     "127.0.0.1",
		Port:     server.RANDOM_PORT,
		Username: peerUser,
		Password: cfg.Secret,
	}

	routes, err := peerURLs(cfg.Peers, cfg.Secret, cfg.Leaf)
	if err != nil {
		return nil, err
	}

	if cfg.Leaf {
		// A leaf dials out and is not meshed: the core needs no route
		// back to it, which is what lets an edge instance live
		// somewhere the console core cannot reach.
		opts.LeafNode.Remotes = make([]*server.RemoteLeafOpts, 0, len(routes))
		for _, u := range routes {
			opts.LeafNode.Remotes = append(opts.LeafNode.Remotes, &server.RemoteLeafOpts{URLs: []*url.URL{u}})
		}
		return opts, nil
	}

	opts.Cluster = server.ClusterOpts{
		Name: clusterName,
		// Peer connections authenticate: without this anything able to
		// reach the port could join the bus, read every activity and
		// revocation event, and publish forged ones.
		Username: peerUser,
		Password: cfg.Secret,
	}
	if cfg.ListenAddr != "" {
		host, port, err := splitHostPort(cfg.ListenAddr)
		if err != nil {
			return nil, fmt.Errorf("parse cluster listen address %q: %w", cfg.ListenAddr, err)
		}
		opts.Cluster.Host = host
		opts.Cluster.Port = port
	}
	opts.Routes = routes

	// A leaf needs somewhere to attach, but only if there are leaves.
	// Opened at an explicitly configured address rather than derived
	// from the route port: deriving it (route port + 1) meant binding an
	// address the operator never chose, and if anything already held it
	// the listener silently failed to come up and the server never
	// became ready - a startup hang with no useful error.
	if cfg.LeafListenAddr != "" {
		host, port, err := splitHostPort(cfg.LeafListenAddr)
		if err != nil {
			return nil, fmt.Errorf("parse cluster leaf listen address %q: %w", cfg.LeafListenAddr, err)
		}
		opts.LeafNode.Host = host
		opts.LeafNode.Port = port
		opts.LeafNode.Username = peerUser
		opts.LeafNode.Password = cfg.Secret
	}

	return opts, nil
}

// peerURLs turns "host:port" peers into the authenticated URLs
// nats-server dials.
func peerURLs(peers []string, secret string, leaf bool) ([]*url.URL, error) {
	scheme := "nats-route"
	if leaf {
		scheme = "nats-leaf"
	}

	urls := make([]*url.URL, 0, len(peers))
	for _, p := range peers {
		host, port, err := net.SplitHostPort(p)
		if err != nil {
			return nil, fmt.Errorf("parse cluster peer %q: %w", p, err)
		}

		u := &url.URL{
			Scheme: scheme,
			User:   url.UserPassword(peerUser, secret),
			Host:   net.JoinHostPort(host, port),
		}
		urls = append(urls, u)
	}
	return urls, nil
}

func splitHostPort(addr string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port %q: %w", portStr, err)
	}
	return host, port, nil
}

// Name identifies this checker in the health check endpoint.
func (b *Bus) Name() string { return "nats" }

// Check reports whether the embedded NATS server is running and the
// internal client connection to it is healthy.
func (b *Bus) Check(_ context.Context) error {
	if !b.server.Running() {
		return fmt.Errorf("embedded NATS server is not running")
	}
	if !b.conn.IsConnected() {
		return fmt.Errorf("internal NATS connection is not connected")
	}
	return nil
}

// LeafAddr returns the address this instance accepts leaf connections
// on, or "" when it has no leaf listener.
//
// The configured value rather than the bound one: nats-server exposes no
// accessor for the leafnode listener's address, and this is only ever
// used for logging and for a test to know where to attach.
func (b *Bus) LeafAddr() string { return b.leafAddr }

// ClusterAddr returns the address this instance accepts peer connections
// on, or "" when it has no peer listener.
func (b *Bus) ClusterAddr() string {
	addr := b.server.ClusterAddr()
	if addr == nil {
		return ""
	}
	return addr.String()
}

// Publish sends data to subject on the internal NATS bus. In a cluster,
// subscribers on every instance receive it.
func (b *Bus) Publish(subject string, data []byte) error {
	return b.conn.Publish(subject, data)
}

// Subscribe registers handler to be called for every message published
// to subject - on every instance in the cluster.
//
// Use this only when the handler updates state local to its own
// instance. A handler with any effect beyond that (persisting a row,
// calling out to another system) must use QueueSubscribe instead, or it
// will do its work once per running instance. See the package doc.
func (b *Bus) Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error) {
	return b.conn.Subscribe(subject, handler)
}

// QueueSubscribe registers handler as a member of the named queue group:
// each message published to subject is delivered to exactly one member,
// whichever instance it is running on.
//
// This is what a subscriber with a side effect must use, so that the
// side effect happens once per message rather than once per instance.
func (b *Bus) QueueSubscribe(subject, queue string, handler nats.MsgHandler) (*nats.Subscription, error) {
	return b.conn.QueueSubscribe(subject, queue, handler)
}

// AskAll publishes data on subject and returns every reply that arrives
// before the window elapses.
//
// This is the bus's third delivery pattern, and it exists because the
// other two cannot express it: Subscribe fans a message out to every
// instance but carries no reply path, and QueueSubscribe deliberately
// reaches only one. Aggregating an answer from the whole fleet needs the
// fan-out and the replies together.
//
// The window always elapses - there is no early exit, because AskAll
// cannot know how many instances will answer. Callers should therefore
// keep it short: it is a floor on how long the call takes, not a
// ceiling.
//
// AskAll deliberately does not judge whether the set of replies is
// complete. It has no basis to: "everyone answered" is a statement about
// how many instances exist, which is the caller's question, not the
// transport's. See LocalView for the raw material a caller needs to
// decide that for itself.
//
// Responders answer with msg.Respond; see Subscribe.
func (b *Bus) AskAll(ctx context.Context, subject string, data []byte, window time.Duration) ([][]byte, error) {
	inbox := nats.NewInbox()

	sub, err := b.conn.SubscribeSync(inbox)
	if err != nil {
		return nil, fmt.Errorf("subscribe to reply inbox: %w", err)
	}
	defer sub.Unsubscribe()

	// Interest in the inbox must reach every peer before the request
	// does, or an instance could reply into a subject this server does
	// not yet route back here.
	if err := b.conn.Flush(); err != nil {
		return nil, fmt.Errorf("flush reply subscription: %w", err)
	}

	if err := b.conn.PublishRequest(subject, inbox, data); err != nil {
		return nil, fmt.Errorf("publish request: %w", err)
	}

	deadline := time.Now().Add(window)
	var replies [][]byte
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return replies, nil
		}
		msg, err := sub.NextMsg(remaining)
		if err != nil {
			// Timeout is the ordinary terminator: the window elapsed.
			// Any other error ends collection with what was gathered.
			return replies, nil
		}
		replies = append(replies, msg.Data)

		if ctx.Err() != nil {
			return replies, ctx.Err()
		}
	}
}

// LocalView is what one server can see of the cluster from where it
// sits.
type LocalView struct {
	// RoutedPeers is the number of distinct peer servers this one is
	// routed to - distinct servers, not connections, so route pooling
	// does not inflate it.
	RoutedPeers int

	// LeafConnections is the number of leaf instances attached to this
	// server specifically.
	LeafConnections int
}

// LocalView reports this server's own view of the cluster.
//
// It is a local view and nothing more: a leaf attached to a *different*
// peer does not appear here, because leaf connections are only visible
// to the server they are attached to. A caller working out how many
// instances should have answered must therefore combine the views of
// every instance that did, rather than trusting any one of them.
func (b *Bus) LocalView() LocalView {
	return LocalView{
		RoutedPeers:     b.server.NumRemotes(),
		LeafConnections: b.server.NumLeafNodes(),
	}
}

// Flush blocks until every message published so far has reached the
// server, so a caller that must not lose an event on shutdown can wait
// for it.
func (b *Bus) Flush() error { return b.conn.Flush() }

// Close drains the internal connection and shuts down the embedded server.
func (b *Bus) Close() {
	if b.conn != nil {
		b.conn.Close()
	}
	if b.server != nil {
		b.server.Shutdown()
		b.server.WaitForShutdown()
	}
}
