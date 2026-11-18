package nodeagent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
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

	// afterReply, when set, is asked after every reply for something to
	// do once that reply is on its way - see SetAfterReply.
	afterReply func() func(ctx context.Context)
	// flush pushes pending replies to the server. Set by Run.
	flush func()
}

// replyFlushTimeout bounds waiting for a reply to reach the server
// before acting on it.
const replyFlushTimeout = 5 * time.Second

// SetAfterReply registers take, which is called after each reply is
// sent and may return work that must only happen once the console has
// its answer - an upgraded client restarting itself, say. That work
// runs with the client still marked busy, so no other request is
// accepted while it does. Call before Run.
func (c *Client) SetAfterReply(take func() func(ctx context.Context)) {
	c.afterReply = take
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
	c.flush = func() {
		if err := conn.FlushTimeout(replyFlushTimeout); err != nil {
			c.logger.Warn("failed to flush reply", "error", err)
		}
	}

	// Each request is handled on its own goroutine, not on the
	// subscription's. NATS delivers to a subscription one message at a
	// time, so handling a request where it is delivered would hold
	// every later one in the client's queue until it finished: a
	// second request during a ten-minute task would not be refused as
	// busy, it would wait ten minutes - long after whoever sent it had
	// given up - and then run anyway. Off the delivery goroutine, the
	// busy check in dispatch is reached at once and does its job.
	sub, err := conn.Subscribe(nodetransport.DispatchSubject(name), func(msg *nats.Msg) {
		go c.dispatch(ctx, msg)
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
//
// A request that only reads the client's own build is the exception:
// it is answered whatever else is running, and never makes the client
// busy. It changes nothing on the node, and the console sends it the
// moment a node connects - alongside a newly enrolled node's first
// Puppet run, which must not be refused because the two crossed.
func (c *Client) dispatch(ctx context.Context, msg *nats.Msg) {
	if answeredWhileBusy(msg.Data) {
		if err := msg.Respond(c.handler(ctx, msg.Data)); err != nil {
			c.logger.Warn("failed to send response", "error", err)
		}
		return
	}

	if !c.busy.CompareAndSwap(false, true) {
		if err := msg.Respond(errorResponse("node-agent is busy executing another request")); err != nil {
			c.logger.Warn("failed to send busy response", "error", err)
		}
		return
	}
	release := true
	defer func() {
		if release {
			c.busy.Store(false)
		}
	}()

	resp := c.handler(ctx, msg.Data)
	if err := msg.Respond(resp); err != nil {
		c.logger.Warn("failed to send response", "error", err)
	}

	if c.afterReply == nil {
		return
	}
	after := c.afterReply()
	if after == nil {
		return
	}
	if c.flush != nil {
		c.flush()
	}
	// Still busy, on a goroutine of its own so that dispatch returns
	// once the reply is sent: requests arriving during the wait are
	// refused at once and the console can try again, rather than being
	// started only to be lost to the restart.
	release = false
	go func() {
		defer c.busy.Store(false)
		after(ctx)
	}()
}

// answeredWhileBusy reports whether payload is a request the client
// answers regardless of other work in progress - see dispatch. A
// payload that cannot be read is not one: it goes the ordinary way and
// the handler reports what is wrong with it.
func answeredWhileBusy(payload []byte) bool {
	var req requestData
	return json.Unmarshal(payload, &req) == nil && (req.Action == actionAgentStatus || req.Action == actionPatchStatus)
}
