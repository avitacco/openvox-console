package nodeagent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/voxpupuli/enterprise-console/internal/nodetransport"
)

// reconnectWait bounds how long nats.go waits between reconnection
// attempts (see Run's doc comment) - short enough that a brief network
// blip recovers quickly.
const reconnectWait = time.Second

// Client connects to the console's node transport and dispatches
// incoming requests to Handler.
type Client struct {
	cfg     Config
	handler Handler
	logger  *slog.Logger
	busy    atomic.Bool
}

// New builds a Client. logger may be nil (slog.Default() is used).
func New(cfg Config, handler Handler, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{cfg: cfg, handler: handler, logger: logger}
}

// LoadTLSConfig loads the node's certificate/key/CA from cfg's
// configured paths. It fails fast (not attempting any enrollment) if
// the certificate is missing - see the package doc and design.md.
func LoadTLSConfig(cfg Config) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load node-agent certificate (no Puppet certificate found at %s - run the Puppet agent to enroll first): %w", cfg.CertFile, err)
	}

	caPEM, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read node-agent CA certificate: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no certificates found in CA file %s", cfg.CAFile)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
	}, nil
}

// certname extracts this node's own identity from its leaf certificate -
// the exact same Subject.CommonName the console's node transport itself
// derives from the same certificate during the mTLS handshake (see
// internal/nodetransport/auth.go), so both sides agree on which subject
// this node's requests/replies live under without either side needing
// to be told the certname separately.
func certname(tlsConfig *tls.Config) (string, error) {
	if len(tlsConfig.Certificates) == 0 || len(tlsConfig.Certificates[0].Certificate) == 0 {
		return "", fmt.Errorf("no leaf certificate loaded")
	}
	leaf, err := x509.ParseCertificate(tlsConfig.Certificates[0].Certificate[0])
	if err != nil {
		return "", fmt.Errorf("parse leaf certificate: %w", err)
	}
	return leaf.Subject.CommonName, nil
}

// Run connects to the console's node transport and processes dispatch
// requests until ctx is cancelled. Unlike a hand-rolled
// reconnect loop (raw websockets have no such feature built in), this
// relies on nats.go's own automatic reconnection - configured here with
// unlimited retries - which also automatically re-establishes this
// client's subscription once reconnected, with no extra code needed.
func (c *Client) Run(ctx context.Context) error {
	tlsConfig, err := LoadTLSConfig(c.cfg)
	if err != nil {
		return err
	}
	name, err := certname(tlsConfig)
	if err != nil {
		return fmt.Errorf("determine this node's certname: %w", err)
	}

	conn, err := nats.Connect("tls://"+c.cfg.TransportAddr,
		nats.Secure(tlsConfig),
		nats.MaxReconnects(-1), // unlimited - keep trying until ctx is cancelled
		nats.ReconnectWait(reconnectWait),
		// Matches the server's own PingInterval/MaxPingsOut (see that
		// constant's doc comment in internal/nodetransport) - nats.go's
		// default 2-minute ping interval outlasts most NAT/firewall/
		// load-balancer idle timeouts a node-agent's long-lived outbound
		// connection might traverse, so without this the connection can
		// go silently stale on the middlebox well before nats.go's own
		// ping cycle would notice and trigger a reconnect.
		nats.PingInterval(nodetransport.PingInterval),
		nats.MaxPingsOutstanding(nodetransport.MaxPingsOut),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				c.logger.Warn("node transport connection lost, reconnecting", "error", err)
			}
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			c.logger.Info("reconnected to node transport")
		}),
	)
	if err != nil {
		return fmt.Errorf("connect to node transport: %w", err)
	}
	defer conn.Close()

	sub, err := conn.Subscribe(nodetransport.DispatchSubject(name), func(msg *nats.Msg) {
		c.dispatch(ctx, msg)
	})
	if err != nil {
		return fmt.Errorf("subscribe to dispatch subject: %w", err)
	}
	defer sub.Unsubscribe()

	<-ctx.Done()
	return ctx.Err()
}

// dispatch handles one incoming request: if the client is already
// executing a request, it replies with a busy error immediately
// (rejected, not queued - see specs/node-agent's "Single in-flight
// request per client" requirement); otherwise it runs Handler and
// replies with its response.
func (c *Client) dispatch(ctx context.Context, msg *nats.Msg) {
	if !c.busy.CompareAndSwap(false, true) {
		if err := msg.Respond(errorResponse("node-agent is busy executing another request")); err != nil {
			c.logger.Warn("failed to send busy response", "error", err)
		}
		return
	}
	defer c.busy.Store(false)

	resp := c.handler(ctx, msg.Data)
	if err := msg.Respond(resp); err != nil {
		c.logger.Warn("failed to send response", "error", err)
	}
}
