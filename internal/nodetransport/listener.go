package nodetransport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/voxpupuli/enterprise-console/internal/messaging"
)

// Config configures the node listener.
type Config struct {
	ListenAddr string // host:port to bind, e.g. ":7422"
	CertFile   string // this console's own server certificate
	KeyFile    string
	CAFile     string // CA used to verify a node's client certificate

	// CRLFile, when set, is a CRL for the CA above, re-read whenever it
	// changes. It must be readable and valid at startup: a revocation
	// list that is configured but not in force is worse than none,
	// because it looks like one is.
	CRLFile string

	// FetchCRL, used when CRLFile is not set, fetches the current CRL
	// from the CA. A failure at startup is logged, not fatal - the CA
	// being briefly unreachable must not stop the console starting -
	// and retried every refresh.
	FetchCRL func(ctx context.Context) ([]byte, error)

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

	// Logger receives CRL refresh failures and revocation enforcement.
	// Nil discards them.
	Logger *slog.Logger
}

// Listener is the console-side connection point for managed nodes: the
// mutual-TLS node listener on the embedded NATS server (see
// messaging.NodeListener), the per-node authorization behind it, CRL
// enforcement, and the registry of which nodes are connected.
//
// It is built in two steps because nats-server fixes its listeners at
// start: NewListener prepares the listener configuration, which is
// handed to messaging.StartWith, and Start then attaches everything that
// needs the running server.
type Listener struct {
	cfg    Config
	logger *slog.Logger
	tls    *tls.Config

	crl       crlState
	crlSource crlSource
	cas       []*x509.Certificate

	sessions sessions

	bus          *messaging.Bus
	registry     *Registry
	registryConn *nats.Conn
	revokeSub    *nats.Subscription
	stop         context.CancelFunc
	done         chan struct{}
}

// NewListener loads the listener's TLS material and initial CRL.
func NewListener(cfg Config) (*Listener, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	l := &Listener{cfg: cfg, logger: logger, sessions: sessions{certs: map[string]*x509.Certificate{}}}

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
	if l.cas, err = parseCertificates(caPEM); err != nil {
		return nil, err
	}

	l.tls = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caPool,
		// The TLS handshake itself fails a connection whose client
		// certificate isn't signed by ClientCAs, or has been revoked,
		// before any application-level auth (see auth.go) ever runs.
		ClientAuth:       tls.RequireAndVerifyClientCert,
		VerifyConnection: l.crl.verifyConnection,
	}

	switch {
	case cfg.CRLFile != "":
		l.crlSource = &fileCRLSource{path: cfg.CRLFile}
		if err := l.refreshCRL(context.Background()); err != nil {
			return nil, fmt.Errorf("load node transport CRL: %w", err)
		}
	case cfg.FetchCRL != nil:
		l.crlSource = fetchCRLSource{fetch: cfg.FetchCRL}
		if err := l.refreshCRL(context.Background()); err != nil {
			logger.Error("could not fetch the CRL from the CA; revoked node certificates are accepted until a fetch succeeds",
				"error", err, "retry", crlRefreshInterval)
		}
	default:
		logger.Warn("no CRL source configured for the node transport; a revoked node certificate can still connect " +
			"(set CONSOLE_NODE_TRANSPORT_CRL_FILE, or CONSOLE_CA_CLIENT_URL to fetch it from the CA)")
	}

	return l, nil
}

// NodeListener returns the listener configuration to start the embedded
// server with.
func (l *Listener) NodeListener() *messaging.NodeListener {
	return &messaging.NodeListener{
		Addr:      l.cfg.ListenAddr,
		TLS:       l.tls,
		Authorize: l.authorize,
		// See PingInterval's own doc comment (subjects.go) - nats-server's
		// defaults outlast most NAT/firewall/load-balancer idle timeouts,
		// so a node-agent behind one goes silently stale well before
		// either side's ping cycle would notice.
		PingInterval: PingInterval,
		MaxPingsOut:  MaxPingsOut,
	}
}

// Start attaches the listener to bus, whose server must have been
// started with NodeListener: it begins tracking connections, enforcing
// revocations announced by any instance, and refreshing the CRL.
func (l *Listener) Start(bus *messaging.Bus) error {
	l.bus = bus

	registry, registryConn, err := startRegistry(bus, l.cfg.OnNodeConnect)
	if err != nil {
		return fmt.Errorf("start node connection registry: %w", err)
	}
	l.registry, l.registryConn = registry, registryConn

	// Fan-out, not a queue group: each instance can only disconnect the
	// connections terminated at itself, so every instance must hear
	// every revocation. The effect is confined to this instance.
	l.revokeSub, err = bus.Subscribe(CertificateRevokedSubject, func(msg *nats.Msg) {
		var evt certificateRevokedEvent
		if json.Unmarshal(msg.Data, &evt) != nil || evt.Certname == "" {
			return
		}
		// Off the NATS callback: a CRL fetch is a network call.
		go l.enforceRevocation(evt.Certname)
	})
	if err != nil {
		registryConn.Close()
		return fmt.Errorf("subscribe to certificate revocations: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	l.stop, l.done = cancel, make(chan struct{})
	go l.refreshLoop(ctx)
	return nil
}

// Registry returns the listener's node connection registry - see
// registry.go.
func (l *Listener) Registry() *Registry { return l.registry }

// Addr returns the node listener's bound address.
func (l *Listener) Addr() string { return l.bus.NodeAddr() }

// Close stops the listener's background work. The node connections
// themselves close with the server.
func (l *Listener) Close() {
	if l.stop != nil {
		l.stop()
		<-l.done
	}
	if l.revokeSub != nil {
		_ = l.revokeSub.Unsubscribe()
	}
	if l.registryConn != nil {
		l.registryConn.Close()
	}
}

func (l *Listener) refreshLoop(ctx context.Context) {
	defer close(l.done)
	if l.crlSource == nil {
		<-ctx.Done()
		return
	}
	ticker := time.NewTicker(crlRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := l.refreshCRL(ctx); err != nil {
				l.logger.Warn("failed to refresh the node transport CRL; keeping the previous one",
					"source", l.crlSource.describe(), "error", err)
				continue
			}
			l.disconnectRevokedSessions()
		}
	}
}

// refreshCRL loads the CRL from its source and, if it parses and
// verifies, makes it current. On any failure the previous list stays in
// force.
func (l *Listener) refreshCRL(ctx context.Context) error {
	data, changed, err := l.crlSource.load(ctx)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	list, err := parseRevocationList(data, l.cas)
	if err != nil {
		return err
	}
	l.crl.current.Store(list)
	return nil
}

// enforceRevocation reacts to a revocation made through the console: it
// fetches the new CRL, so the node cannot simply reconnect, then drops
// the node's current connections.
//
// The refresh comes first on purpose. Disconnecting first would invite
// the node's immediate reconnect to be checked against the old CRL.
func (l *Listener) enforceRevocation(certname string) {
	if l.crlSource != nil {
		ctx, cancel := context.WithTimeout(context.Background(), crlFetchTimeout)
		if err := l.refreshCRL(ctx); err != nil {
			l.logger.Warn("failed to refresh the CRL after a revocation; the node may reconnect until the next refresh",
				"certname", certname, "error", err)
		}
		cancel()
	}
	if n := l.disconnect(certname); n > 0 {
		l.logger.Info("disconnected node after certificate revocation", "certname", certname, "connections", n)
	}
}

// disconnectRevokedSessions drops every connection whose certificate
// the current CRL revokes - one revoked outside the console, noticed by
// a refresh.
func (l *Listener) disconnectRevokedSessions() {
	for _, certname := range l.sessions.revoked(&l.crl) {
		if n := l.disconnect(certname); n > 0 {
			l.logger.Info("disconnected node whose certificate is now revoked", "certname", certname, "connections", n)
		}
	}
}

// disconnect closes every connection certname holds to this instance,
// returning how many it closed.
func (l *Listener) disconnect(certname string) int {
	ns := l.bus.Server()
	connz, err := ns.Connz(&server.ConnzOptions{Account: messaging.AccountNodes, User: certname})
	if err != nil {
		l.logger.Warn("failed to list node connections", "certname", certname, "error", err)
		return 0
	}
	n := 0
	for _, c := range connz.Conns {
		if err := ns.DisconnectClientByID(c.Cid); err == nil {
			n++
		}
	}
	l.sessions.forget(certname)
	return n
}

// CloseAllConnections forcibly disconnects every node currently
// connected to this instance - not the console's own in-process
// connections into the account. Each disconnected node-agent is expected
// to reconnect on its own (see internal/nodeagent's automatic
// reconnection).
func (l *Listener) CloseAllConnections() error {
	ns := l.bus.Server()
	connz, err := ns.Connz(&server.ConnzOptions{Account: messaging.AccountNodes, Username: true, Limit: 1 << 20})
	if err != nil {
		return fmt.Errorf("list node transport connections: %w", err)
	}
	for _, c := range connz.Conns {
		if messaging.IsInProcessUser(c.AuthorizedUser) {
			continue
		}
		if err := ns.DisconnectClientByID(c.Cid); err != nil {
			return fmt.Errorf("disconnect connection %d: %w", c.Cid, err)
		}
	}
	return nil
}

// sessions remembers which certificate each connected node presented,
// so a CRL refresh can tell which live connections it now revokes.
// Only the most recent certificate per certname is kept: a node holds
// one certificate at a time, and a stale entry costs at most one
// unnecessary Connz lookup.
type sessions struct {
	mu    sync.Mutex
	certs map[string]*x509.Certificate // certname -> certificate presented
}

func (s *sessions) record(certname string, cert *x509.Certificate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.certs[certname] = cert
}

func (s *sessions) forget(certname string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.certs, certname)
}

// revoked returns the certnames whose recorded certificate crl revokes.
func (s *sessions) revoked(crl *crlState) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for certname, cert := range s.certs {
		if crl.revoked(cert) {
			out = append(out, certname)
		}
	}
	return out
}

// certificateRevokedEvent is the payload on CertificateRevokedSubject.
type certificateRevokedEvent struct {
	Certname string `json:"certname"`
}

// publisher is the subset of *messaging.Bus AnnounceCertificateRevoked
// needs.
type publisher interface {
	Publish(subject string, data []byte) error
}

// AnnounceCertificateRevoked tells every instance that certname's
// certificate has been revoked (or cleaned), so each drops that node's
// connections and refreshes its CRL rather than waiting for the next
// scheduled refresh. Call it after the CA has accepted the revocation.
func AnnounceCertificateRevoked(bus publisher, certname string) error {
	data, err := json.Marshal(certificateRevokedEvent{Certname: certname})
	if err != nil {
		return err
	}
	return bus.Publish(CertificateRevokedSubject, data)
}
