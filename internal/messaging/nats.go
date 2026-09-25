// Package messaging embeds a NATS server in-process and exposes a thin
// publish/subscribe helper for internal components to communicate over,
// without a separately deployed broker.
//
// # One server, two accounts
//
// Every instance runs exactly one embedded server, carrying two NATS
// accounts that share no subjects:
//
//   - AccountConsole is the console's own internal bus: revocation,
//     code-deploy and stack-status events. Everything Bus.Publish and
//     friends touch lives here.
//   - AccountNodes is the node transport (see internal/nodetransport):
//     managed nodes connect into it over mutual TLS, and the console
//     dispatches to them from inside it.
//
// Accounts are NATS's own isolation boundary - a subject in one does not
// exist in the other unless explicitly exported, and nothing here is -
// so node traffic can never observe or collide with internal subjects,
// without per-subject access control between the two. One server rather
// than one per concern means one set of listeners, one cluster, and one
// place TLS and authentication are configured.
//
// # Clustering, and the one rule subscribers must follow
//
// A single instance runs unclustered and opens no listener beyond the
// node listener, if it has one. When peers are configured, the server
// opens a listener for *peer connections only*, over mutual TLS, and
// events published anywhere in the cluster reach subscribers everywhere
// in it. Routes carry both accounts; leaf connections (edge instances,
// see Config.Leaf) are bound to AccountConsole alone, so an edge
// instance can never reach a node.
//
// That changes what a subscription means, and there are two kinds:
//
//   - Fan-out (Subscribe): every instance's subscriber receives a copy.
//     Correct when the handler's effect is confined to its own instance
//
//   - rbac.Revoker's in-memory revocation cache is the example: every
//     instance must learn about a revocation.
//
//   - Queue (QueueSubscribe): exactly one subscriber in the group
//     receives each message. Correct when the handler has an effect
//     beyond its own instance - writing a row, calling out to something
//
//   - which N fan-out subscribers would each do once.
//
// The rule, for every existing and future subscriber: **if the handler
// has any effect outside this instance, it must use a queue group.**
// Getting this wrong is invisible on a single instance and only shows up
// as duplicated work once a second one starts.
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
//
// # Delivery
//
// This is core NATS: at-most-once, with no persistence. A message
// published while a route is down, or to a subscriber that has fallen
// behind, is gone. Nothing that must not be lost may depend on the bus
// alone - see rbac.Revoker, which treats Postgres as the source of truth
// and the bus as a latency optimization over it.
package messaging

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// The two accounts every server carries - see the package doc.
const (
	AccountConsole = "CONSOLE"
	AccountNodes   = "NODES"
)

// clusterName is shared by every peer: nats-server refuses to route
// between servers whose cluster names disagree, which turns a
// copy-paste configuration error into a clear startup failure rather
// than a silently partitioned bus.
const clusterName = "openvox-console"

// peerUser is the identity route connections authenticate as, and
// leafUser the one leaf connections do. They are separate credentials
// because they grant very different things: a route carries every
// account, node dispatches included, while a leaf is bound to
// AccountConsole alone. An edge host holding the leaf secret must not be
// able to present it as a route.
const (
	peerUser = "console-peer"
	leafUser = "console-leaf"
)

// readyTimeout bounds how long StartWith waits for the embedded server
// to become ready.
const readyTimeout = 90 * time.Second

// tlsTimeout bounds a peer's TLS handshake. nats-server's own default
// (2s) is tuned for a dedicated broker; an instance starting under load
// - several in one test process, or a busy host - can take longer.
const tlsTimeout = 10 * time.Second

// drainTimeout bounds how long Close waits for in-flight messages to be
// handled before giving up on them.
const drainTimeout = 5 * time.Second

// inProcessNamePrefix marks the connection name of every in-process
// client this package opens: "<prefix><account>/<purpose>". The
// authenticator reads the account from it - see authenticator.Check for
// why a name is trustworthy on that path and nowhere else.
const inProcessNamePrefix = "in-process:"

// Config describes how this instance's server is set up. The zero value
// is a single, unclustered instance with no listener at all.
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

	// Secret authenticates route connections. Required whenever
	// ListenAddr is set or Peers is set on a routed instance.
	Secret string

	// LeafSecret authenticates leaf connections. Required whenever
	// LeafListenAddr is set, or Peers is set on a leaf. Must differ from
	// Secret - see peerUser.
	LeafSecret string

	// Advertise is the "host:port" other peers should use to reach this
	// one, when that differs from what they would discover. Peers learn
	// about each other by gossip, which by default carries IP addresses;
	// with TLS verifying the peer's hostname, a gossiped IP that is not
	// in the peer's certificate fails verification. Setting this to a
	// name in this instance's certificate avoids that.
	Advertise string

	// TLS secures every peer connection, route and leaf alike, with
	// mutual TLS. Required whenever any peering is configured: peer
	// connections carry revocation events and node dispatches, and the
	// secrets above travel over them.
	TLS *PeerTLS

	// Nodes, when set, opens the node listener managed nodes connect to.
	Nodes *NodeListener

	// Logger receives asynchronous client errors - a slow consumer
	// dropping messages, a permissions violation. Nil discards them.
	Logger *slog.Logger
}

// PeerTLS names the certificate material peer connections use: this
// instance's own certificate (presented both as a server, to peers
// dialing in, and as a client, when dialing out) and the CA a peer's
// certificate must chain to.
type PeerTLS struct {
	CertFile string
	KeyFile  string
	CAFile   string
}

// NodeListener configures the client listener managed nodes connect to
// (see internal/nodetransport, which builds one).
type NodeListener struct {
	// Addr is the "host:port" to bind.
	Addr string

	// TLS is the listener's server configuration. It must require and
	// verify a client certificate: Authorize is only ever consulted for
	// a connection whose certificate chain has already been verified.
	TLS *tls.Config

	// Authorize maps a verified node connection to the NATS user it
	// connects as, returning false to refuse it. The user is placed in
	// AccountNodes regardless of what Authorize sets.
	Authorize func(state *tls.ConnectionState) (*server.User, bool)

	// PingInterval and MaxPingsOut set the server's keepalive cadence.
	// They apply to every client, but only nodes connect over a network,
	// and their long-lived connections are what need it - see
	// internal/nodetransport's constants of the same names. Zero keeps
	// nats-server's defaults.
	PingInterval time.Duration
	MaxPingsOut  int
}

// clustered reports whether any peering is configured.
func (c Config) clustered() bool {
	return c.ListenAddr != "" || c.LeafListenAddr != "" || len(c.Peers) > 0
}

// Bus wraps an embedded NATS server and a client connection to its
// AccountConsole, used for internal publish/subscribe. When peers are
// configured, that bus spans every instance in the cluster.
type Bus struct {
	server   *server.Server
	conn     *nats.Conn
	leafAddr string
	logger   *slog.Logger

	// asyncErrors counts errors reported asynchronously on any of this
	// package's client connections - see errorHandler.
	asyncErrors atomic.Uint64
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
	auth := &authenticator{nodes: cfg.Nodes}
	opts, err := options(cfg, auth)
	if err != nil {
		return nil, err
	}

	ns, err := server.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("create embedded NATS server: %w", err)
	}
	// Set between NewServer and Start: Check only ever runs for an
	// actual connection, which cannot arrive before Start.
	auth.ns = ns

	ns.Start()
	// Generous: this only bounds how long a slow or contended start-up
	// may take before giving up, not any steady-state behavior.
	if !ns.ReadyForConnections(readyTimeout) {
		ns.Shutdown()
		return nil, fmt.Errorf("embedded NATS server did not become ready in time")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	b := &Bus{server: ns, leafAddr: cfg.LeafListenAddr, logger: logger}

	conn, err := b.Connect(AccountConsole, "bus")
	if err != nil {
		ns.Shutdown()
		return nil, err
	}
	b.conn = conn

	return b, nil
}

// options builds the nats-server options for cfg.
func options(cfg Config, auth *authenticator) (*server.Options, error) {
	opts := &server.Options{
		// Embedded, so the process's signals belong to the process.
		// Without this nats-server installs its own SIGINT/SIGTERM
		// handler, which shuts the server down and calls os.Exit -
		// skipping the console's graceful HTTP shutdown entirely.
		NoSigs: true,

		// Both accounts exist on every server, so routes can carry both
		// without either side having to agree on anything at runtime.
		Accounts: []*server.Account{
			server.NewAccount(AccountConsole),
			server.NewAccount(AccountNodes),
		},

		// Every client - in-process or a node - goes through one
		// authenticator, which is what places each connection in its
		// account. See authenticator.Check.
		CustomClientAuthentication: auth,
	}

	if n := cfg.Nodes; n != nil {
		if n.TLS == nil || n.TLS.ClientAuth != tls.RequireAndVerifyClientCert {
			return nil, fmt.Errorf("node listener must require and verify client certificates")
		}
		if n.Authorize == nil {
			return nil, fmt.Errorf("node listener has no authorizer")
		}
		host, port, err := splitHostPort(n.Addr)
		if err != nil {
			return nil, fmt.Errorf("parse node listener address %q: %w", n.Addr, err)
		}
		opts.Host = host
		opts.Port = port
		opts.TLSConfig = n.TLS
		opts.TLSVerify = true
		opts.TLSTimeout = tlsTimeout.Seconds()
		opts.PingInterval = n.PingInterval
		opts.MaxPingsOut = n.MaxPingsOut
	}

	if !cfg.clustered() {
		if cfg.Nodes == nil {
			// In-process only, no listener of any kind.
			opts.DontListen = true
		}
		return opts, nil
	}

	if cfg.TLS == nil {
		return nil, fmt.Errorf("peer TLS is required when peering is configured")
	}
	serverTLS, clientTLS, err := loadPeerTLS(*cfg.TLS)
	if err != nil {
		return nil, err
	}

	if cfg.Nodes == nil {
		// A clustered server cannot use DontListen. nats-server starts
		// its routing goroutine only after the client accept loop
		// signals that it is up, and DontListen skips that accept loop
		// entirely - so the route listener would never open and the
		// server would never become ready. Verified against
		// nats-server v2.14.5 (server.Start's clientListenReady
		// handoff).
		//
		// So the client port is opened, bound to loopback on an
		// ephemeral port. Nothing is expected to arrive on it, and
		// nothing arriving on it is accepted: the authenticator refuses
		// every network connection that is not a verified node, and
		// with no node listener configured there are none.
		opts.Host = "127.0.0.1"
		opts.Port = server.RANDOM_PORT
	}

	routes, err := peerURLs(cfg.Peers, cfg.Leaf, cfg.Secret, cfg.LeafSecret)
	if err != nil {
		return nil, err
	}

	if cfg.Leaf {
		if len(cfg.Peers) > 0 && cfg.LeafSecret == "" {
			return nil, fmt.Errorf("leaf secret is required to attach as a leaf")
		}
		// A leaf dials out and is not meshed: the core needs no route
		// back to it, which is what lets an edge instance live
		// somewhere the console core cannot reach. It carries
		// AccountConsole only - AccountNodes stays local, and empty.
		opts.LeafNode.Remotes = make([]*server.RemoteLeafOpts, 0, len(routes))
		for _, u := range routes {
			opts.LeafNode.Remotes = append(opts.LeafNode.Remotes, &server.RemoteLeafOpts{
				LocalAccount: AccountConsole,
				URLs:         []*url.URL{u},
				TLS:          true,
				TLSConfig:    clientTLS,
				TLSTimeout:   tlsTimeout.Seconds(),
			})
		}
		return opts, nil
	}

	if cfg.Secret == "" {
		return nil, fmt.Errorf("cluster secret is required when peering is configured")
	}

	opts.Cluster = server.ClusterOpts{
		Name: clusterName,
		// Peer connections authenticate: without this anything able to
		// reach the port with a CA-issued certificate - which every
		// managed node has - could join the bus, read every event and
		// dispatch to every node.
		Username:   peerUser,
		Password:   cfg.Secret,
		TLSConfig:  serverTLS,
		TLSTimeout: tlsTimeout.Seconds(),
		Advertise:  cfg.Advertise,
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
		if cfg.LeafSecret == "" {
			return nil, fmt.Errorf("leaf secret is required when a leaf listener is configured")
		}
		if cfg.LeafSecret == cfg.Secret {
			return nil, fmt.Errorf("leaf secret must differ from the cluster secret: an edge host holding it could otherwise join as a full peer")
		}
		host, port, err := splitHostPort(cfg.LeafListenAddr)
		if err != nil {
			return nil, fmt.Errorf("parse cluster leaf listen address %q: %w", cfg.LeafListenAddr, err)
		}
		opts.LeafNode.Host = host
		opts.LeafNode.Port = port
		opts.LeafNode.Username = leafUser
		opts.LeafNode.Password = cfg.LeafSecret
		// Bound to AccountConsole: whatever an edge instance can reach,
		// it is never the node transport.
		opts.LeafNode.Account = AccountConsole
		opts.LeafNode.TLSConfig = serverTLS
		opts.LeafNode.TLSTimeout = tlsTimeout.Seconds()
	}

	return opts, nil
}

// loadPeerTLS builds the server-side and client-side TLS configuration
// for peer connections from p.
//
// Both sides verify: a peer dialing in must present a certificate
// chaining to the CA, and a peer being dialed must present one valid
// for the name it was dialed by. That second check is what stops a
// managed node - which also holds a CA-issued certificate - from
// intercepting a route and reading the secret sent over it: its
// certificate is not valid for any console's name.
func loadPeerTLS(p PeerTLS) (serverTLS, clientTLS *tls.Config, err error) {
	cert, err := tls.LoadX509KeyPair(p.CertFile, p.KeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load peer TLS certificate: %w", err)
	}
	caPEM, err := os.ReadFile(p.CAFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read peer TLS CA certificate: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, nil, fmt.Errorf("no certificates found in peer TLS CA file %s", p.CAFile)
	}

	serverTLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		// Used by nats-server when this server dials a route: the same
		// configuration serves both directions.
		RootCAs: pool,
	}
	clientTLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
	}
	return serverTLS, clientTLS, nil
}

// peerURLs turns "host:port" peers into the authenticated URLs
// nats-server dials.
func peerURLs(peers []string, leaf bool, secret, leafSecret string) ([]*url.URL, error) {
	scheme, user, password := "nats-route", peerUser, secret
	if leaf {
		scheme, user, password = "nats-leaf", leafUser, leafSecret
	}

	urls := make([]*url.URL, 0, len(peers))
	for _, p := range peers {
		host, port, err := net.SplitHostPort(p)
		if err != nil {
			return nil, fmt.Errorf("parse cluster peer %q: %w", p, err)
		}
		urls = append(urls, &url.URL{
			Scheme: scheme,
			User:   url.UserPassword(user, password),
			Host:   net.JoinHostPort(host, port),
		})
	}
	return urls, nil
}

// splitHostPort parses "host:port" into nats-server's separate Host/Port
// fields - an empty host binds all interfaces, as for net.Listen.
func splitHostPort(addr string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port %q: %w", portStr, err)
	}
	// Port 0 conventionally means "any free port". nats-server reads 0
	// as "unset" and substitutes its own default instead, so ":0" would
	// quietly bind a fixed, probably-occupied port. RANDOM_PORT (-1) is
	// its spelling of ephemeral.
	if port == 0 {
		port = server.RANDOM_PORT
	}
	return host, port, nil
}

// authenticator is the server's single client authenticator. Every
// client connection passes through Check, which decides both whether it
// may connect and which account it lands in.
type authenticator struct {
	ns    *server.Server
	nodes *NodeListener
}

// Check implements server.Authentication.
//
// There are exactly two kinds of acceptable client:
//
//   - The console's own in-process clients (Bus.Connect). These arrive
//     over a net.Pipe that only code inside this process can create -
//     server.InProcessConn is the sole way to make one - so the pipe
//     itself is the credential, and the connection's self-declared name
//     can safely choose its account. Trusting a name anywhere else would
//     let any client that can reach a listener pick its own privileges.
//
//   - Managed nodes, on the node listener, presenting a client
//     certificate the TLS handshake has already verified.
//
// Everything else - a plain TCP client on the loopback port a clustered
// server has to open, or any connection when no node listener exists -
// is refused.
func (a *authenticator) Check(c server.ClientAuthentication) bool {
	if addr := c.RemoteAddress(); addr != nil && addr.Network() == "pipe" {
		return a.checkInProcess(c)
	}

	if a.nodes == nil {
		return false
	}
	state := c.GetTLSConnectionState()
	if state == nil || len(state.VerifiedChains) == 0 {
		return false
	}
	user, ok := a.nodes.Authorize(state)
	if !ok || user == nil {
		return false
	}
	acc, err := a.ns.LookupAccount(AccountNodes)
	if err != nil {
		return false
	}
	user.Account = acc
	c.RegisterUser(user)
	return true
}

// checkInProcess places an in-process client in the account its name
// declares. See Check for why the name is trusted here.
func (a *authenticator) checkInProcess(c server.ClientAuthentication) bool {
	name := strings.TrimPrefix(c.GetOpts().Name, inProcessNamePrefix)
	accName, _, _ := strings.Cut(name, "/")

	var acc *server.Account
	switch accName {
	case AccountConsole, AccountNodes:
		var err error
		if acc, err = a.ns.LookupAccount(accName); err != nil {
			return false
		}
	case server.DEFAULT_SYSTEM_ACCOUNT:
		acc = a.ns.SystemAccount()
	default:
		return false
	}
	if acc == nil {
		return false
	}
	c.RegisterUser(&server.User{Username: c.GetOpts().Name, Account: acc})
	return true
}

// Connect opens a new in-process client connection into account (one of
// AccountConsole, AccountNodes, or server.DEFAULT_SYSTEM_ACCOUNT for
// server events). purpose names it in server monitoring output.
//
// Its asynchronous errors are logged and counted with the Bus's own.
func (b *Bus) Connect(account, purpose string) (*nats.Conn, error) {
	conn, err := nats.Connect("",
		nats.InProcessServer(b.server),
		nats.Name(inProcessNamePrefix+account+"/"+purpose),
		nats.ErrorHandler(b.errorHandler(account+"/"+purpose)),
		// An in-process connection has nothing to reconnect to: if the
		// server goes, so has the process's reason to exist.
		nats.NoReconnect(),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to embedded NATS server as %s/%s: %w", account, purpose, err)
	}
	return conn, nil
}

// IsInProcessUser reports whether username - as it appears in server
// events and monitoring - belongs to one of the console's own
// in-process connections rather than to a network client. A consumer of
// connect events for an account uses it to tell the console's own
// connections apart from the clients it is tracking.
func IsInProcessUser(username string) bool {
	return strings.HasPrefix(username, inProcessNamePrefix)
}

// errorHandler reports asynchronous errors on a connection - above all
// nats.ErrSlowConsumer, which means a subscriber could not keep up and
// messages were dropped. Core NATS drops them silently otherwise; this
// is the only place that loss becomes visible.
func (b *Bus) errorHandler(conn string) nats.ErrHandler {
	return func(_ *nats.Conn, sub *nats.Subscription, err error) {
		// Counted after logging, so anything that sees the count also
		// finds the log line.
		defer b.asyncErrors.Add(1)
		attrs := []any{"connection", conn, "error", err}
		if sub != nil {
			attrs = append(attrs, "subject", sub.Subject)
			if dropped, derr := sub.Dropped(); derr == nil {
				attrs = append(attrs, "dropped", dropped)
			}
		}
		if errors.Is(err, nats.ErrSlowConsumer) {
			b.logger.Error("NATS subscriber fell behind; messages were dropped", attrs...)
			return
		}
		b.logger.Warn("NATS asynchronous error", attrs...)
	}
}

// AsyncErrors reports how many asynchronous client errors (slow
// consumers, permission violations) have occurred since start.
func (b *Bus) AsyncErrors() uint64 { return b.asyncErrors.Load() }

// Server returns the embedded server, for a package that must act on it
// directly - listing or disconnecting node connections.
func (b *Bus) Server() *server.Server { return b.server }

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

// NodeAddr returns the address the node listener is bound to. Only
// meaningful when Config.Nodes was set.
func (b *Bus) NodeAddr() string {
	addr := b.server.Addr()
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
// Use this only when the handler's effect is confined to its own
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
// before the window elapses or ctx ends, whichever is first.
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

	windowCtx, cancel := context.WithTimeout(ctx, window)
	defer cancel()

	var replies [][]byte
	for {
		msg, err := sub.NextMsgWithContext(windowCtx)
		if err != nil {
			// The window elapsing is the ordinary terminator. The
			// caller's own context ending is reported, with whatever
			// was gathered. Any other error ends collection too.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return replies, ctxErr
			}
			return replies, nil
		}
		replies = append(replies, msg.Data)
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

// Close drains the internal connection - letting handlers already
// running finish, and flushing anything published but not yet sent -
// then shuts down the embedded server.
//
// Connections opened with Connect belong to their callers, who close
// them first; any still open are cut off by the server's shutdown.
func (b *Bus) Close() {
	if b.conn != nil {
		drainAndWait(b.conn, drainTimeout)
	}
	if b.server != nil {
		b.server.Shutdown()
		b.server.WaitForShutdown()
	}
}

// drainAndWait drains conn and waits, up to timeout, for the drain to
// finish. Drain itself returns immediately; the connection closes once
// every subscription's pending messages are handled.
func drainAndWait(conn *nats.Conn, timeout time.Duration) {
	if err := conn.Drain(); err != nil {
		conn.Close()
		return
	}
	deadline := time.Now().Add(timeout)
	for !conn.IsClosed() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	conn.Close()
}

// DrainAndClose drains conn - see Close - bounded by the same timeout.
// For callers closing a connection they opened with Connect.
func DrainAndClose(conn *nats.Conn) {
	if conn != nil {
		drainAndWait(conn, drainTimeout)
	}
}
