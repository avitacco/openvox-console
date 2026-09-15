// Package nodetransport implements the console-side connection point for
// managed nodes: a NATS server (TLS, mutual-TLS authenticated against the
// OpenVox CA) that node-agent instances connect to for on-demand
// orchestration (see openspec/specs/node-transport).
//
// This runs as its own embedded NATS server instance, separate from
// internal/messaging.Bus's in-process-only server used for the console's
// own internal pub/sub (RBAC revocation, activity, audit log) - keeping
// them separate guarantees node traffic can never collide with or observe
// internal subjects, without needing per-subject access control between
// the two concerns.
package nodetransport

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// Config configures the node transport's mutual-TLS NATS listener.
type Config struct {
	ListenAddr string // host:port to bind, e.g. ":7422"
	CertFile   string // this console's own server certificate
	KeyFile    string
	CAFile     string // CA used to verify a node's client certificate

	// OnNodeConnect, when set, is called with a node's certname each
	// time that node connects. Optional; nil disables notification.
	//
	// It is called on its own goroutine and its panics are recovered,
	// so a slow or broken observer cannot delay a connection or bring
	// down the transport. In exchange, the observer gets no delivery
	// guarantee: it may be called more than once for what is logically
	// one connection (a reconnect during a partition, or two console
	// instances observing the same event), and a notification can be
	// missed entirely if the console is not running. Observers must be
	// idempotent and must not treat a missed call as impossible.
	OnNodeConnect func(certname string)
}

// Server is the console-side NATS server managed nodes connect to.
type Server struct {
	ns   *server.Server
	conn *nats.Conn // internal client for the console's own use (dispatch)

	registry     *Registry
	registryConn *nats.Conn
}

// New starts a NATS server bound to cfg.ListenAddr, requiring and
// verifying node client certificates against cfg.CAFile, mirroring
// the same certificate loading pattern used elsewhere. It blocks until the
// server is ready or the timeout elapses.
func New(cfg Config) (*Server, error) {
	tlsConfig, err := loadServerTLSConfig(cfg)
	if err != nil {
		return nil, err
	}

	host, port, err := splitHostPort(cfg.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("parse node transport listen address %q: %w", cfg.ListenAddr, err)
	}

	// auth's ns field is set below, between NewServer and Start - Check
	// (see auth.go) is only ever invoked for an actual connection, which
	// can't happen before Start, so this has no initialization race.
	auth := &authenticator{}
	ns, err := server.NewServer(&server.Options{
		Host:      host,
		Port:      port,
		TLSConfig: tlsConfig,
		TLSVerify: true,
		// See auth.go: scopes each connection's NATS permissions to only
		// its own subjects, based on its already-verified client cert.
		CustomClientAuthentication: auth,
		// See PingInterval/MaxPingsOut's own doc comment (subjects.go) -
		// nats-server's defaults outlast most NAT/firewall/load-balancer
		// idle timeouts, so a node-agent behind one goes silently stale
		// well before either side's ping cycle would notice.
		PingInterval: PingInterval,
		MaxPingsOut:  MaxPingsOut,
	})
	if err != nil {
		return nil, fmt.Errorf("create node transport NATS server: %w", err)
	}
	auth.ns = ns

	ns.Start()
	// Generous: only affects how long a slow/contended start-up can take
	// before New gives up, not steady-state behavior - a real server is
	// normally ready in milliseconds.
	if !ns.ReadyForConnections(90 * time.Second) {
		return nil, fmt.Errorf("node transport NATS server did not become ready in time")
	}

	// The console's own internal client connects in-process (no network,
	// no TLS negotiation needed) regardless of the external TLS listener
	// configured above - see nats.InProcessServer's doc comment.
	conn, err := nats.Connect("", nats.InProcessServer(ns))
	if err != nil {
		ns.Shutdown()
		return nil, fmt.Errorf("connect internal client to node transport server: %w", err)
	}

	registry, registryConn, err := startRegistry(ns, server.DEFAULT_GLOBAL_ACCOUNT, cfg.OnNodeConnect)
	if err != nil {
		conn.Close()
		ns.Shutdown()
		return nil, fmt.Errorf("start node connection registry: %w", err)
	}

	return &Server{ns: ns, conn: conn, registry: registry, registryConn: registryConn}, nil
}

// Registry returns the server's node connection registry - see
// registry.go.
func (s *Server) Registry() *Registry {
	return s.registry
}

func loadServerTLSConfig(cfg Config) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load node transport server certificate: %w", err)
	}

	caPEM, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read node transport CA certificate: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no certificates found in node transport CA file %s", cfg.CAFile)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caPool,
		// The TLS handshake itself fails a connection whose client
		// certificate isn't signed by ClientCAs, before any
		// application-level auth (see auth.go) ever runs.
		ClientAuth: tls.RequireAndVerifyClientCert,
	}, nil
}

// splitHostPort parses "host:port" (e.g. ":7422" or "0.0.0.0:7422") into
// nats-server's separate Host/Port fields - nats-server passes Host
// straight through to its own internal net.Listen call, so an empty host
// binds all interfaces exactly as it would for a plain net.Listen.
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

// Addr returns the server's actual listening address, valid once New has
// returned successfully.
func (s *Server) Addr() string {
	return s.ns.Addr().String()
}

// CloseAllConnections forcibly disconnects every currently-connected
// node (not the console's own internal connections - see registry.go/
// dispatch.go's use of nats.InProcessServer, which never appears in
// Connz). Each disconnected node-agent is expected to reconnect on its
// own (see internal/nodeagent's automatic reconnection).
func (s *Server) CloseAllConnections() error {
	connz, err := s.ns.Connz(nil)
	if err != nil {
		return fmt.Errorf("list node transport connections: %w", err)
	}
	for _, c := range connz.Conns {
		if err := s.ns.DisconnectClientByID(c.Cid); err != nil {
			return fmt.Errorf("disconnect connection %d: %w", c.Cid, err)
		}
	}
	return nil
}

// Close drains the internal connections and shuts down the node transport
// server.
func (s *Server) Close() {
	if s.registryConn != nil {
		s.registryConn.Close()
	}
	if s.conn != nil {
		s.conn.Close()
	}
	if s.ns != nil {
		s.ns.Shutdown()
		s.ns.WaitForShutdown()
	}
}
