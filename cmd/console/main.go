// Command console is the OpenVox Console: a single binary serving the
// control plane (dashboard, classification, RBAC, code deployment,
// orchestration) alongside the reused openvox-server/openvoxdb data plane.
// Phase 0 wires up the foundations every later phase builds on: config,
// logging, health checks, Postgres, embedded NATS, and the frontend shell.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/voxpupuli/enterprise-console/internal/activity"
	"github.com/voxpupuli/enterprise-console/internal/agentdist"
	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/certstatus"
	"github.com/voxpupuli/enterprise-console/internal/classifier"
	"github.com/voxpupuli/enterprise-console/internal/codemanager"
	"github.com/voxpupuli/enterprise-console/internal/encapi"
	"github.com/voxpupuli/enterprise-console/internal/groupnodes"
	"github.com/voxpupuli/enterprise-console/internal/infracert"
	"github.com/voxpupuli/enterprise-console/internal/initialrun"
	"github.com/voxpupuli/enterprise-console/internal/inventory"
	"github.com/voxpupuli/enterprise-console/internal/messaging"
	"github.com/voxpupuli/enterprise-console/internal/nodeconnectivity"
	"github.com/voxpupuli/enterprise-console/internal/nodetransport"
	"github.com/voxpupuli/enterprise-console/internal/openvoxdb"
	"github.com/voxpupuli/enterprise-console/internal/orchestrator"
	"github.com/voxpupuli/enterprise-console/internal/packageinventory"
	"github.com/voxpupuli/enterprise-console/internal/persistence"
	"github.com/voxpupuli/enterprise-console/internal/rbac"
	"github.com/voxpupuli/enterprise-console/internal/reporting"
	"github.com/voxpupuli/enterprise-console/internal/runtime"
	"github.com/voxpupuli/enterprise-console/internal/sealer"
	"github.com/voxpupuli/enterprise-console/internal/vulnerability"
	"github.com/voxpupuli/enterprise-console/internal/vulnerability/osv"
	"github.com/voxpupuli/enterprise-console/internal/vulnerability/tenable"
	"github.com/voxpupuli/enterprise-console/internal/web"
)

// allPermissions is granted to the bootstrap admin role - see
// bootstrapAdmin. Every other role is configured by an admin afterward.
var allPermissions = []string{"nodes:read", "nodes:certs:manage", "nodes:manage", "classifier:read", "classifier:write", "enc:read", "rbac:admin", "activity:read", "code:deploy", "code:read", "orchestrator:read", "orchestrator:run", "vulnerabilities:read", "vulnerabilities:manage"}

// runHealthCheck implements the container healthcheck. The image has no
// shell and no wget - it is distroless, so a HEALTHCHECK expressed as a
// shell pipeline can never run and the container reports unhealthy
// forever while serving traffic perfectly well. The binary is the only
// executable present, so it has to check itself.
//
// CONSOLE_HTTP_ADDR is the address the server binds, so dialling its port
// on the loopback checks the same listener a request would reach.
func runHealthCheck() int {
	addr := os.Getenv("CONSOLE_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		fmt.Fprintf(os.Stderr, "health-check: cannot parse CONSOLE_HTTP_ADDR %q\n", addr)
		return 1
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "health-check: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "health-check: HTTP %d\n", resp.StatusCode)
		return 1
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		fmt.Fprintf(os.Stderr, "health-check: %v\n", err)
		return 1
	}
	if body.Status != "ok" {
		fmt.Fprintf(os.Stderr, "health-check: status %q\n", body.Status)
		return 1
	}
	return 0
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--health-check" {
		os.Exit(runHealthCheck())
	}

	logger := runtime.NewLogger(os.Stdout)

	// .env is a local development convenience (see internal/runtime.Config)
	// and is entirely optional: it's fine, and expected, for it not to
	// exist (e.g. in production, where real env vars are set directly).
	// Existing environment variables always take precedence over it.
	if _, statErr := os.Stat(".env"); statErr == nil {
		if err := godotenv.Load(); err != nil {
			logger.Error("failed to load .env file", "error", err)
			os.Exit(1)
		}
	}

	cfg, err := runtime.LoadConfig(os.Getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	// Embedded NATS has no external dependency to fail on; a startup
	// failure here is unexpected and fatal.
	bus, err := messaging.Start()
	if err != nil {
		logger.Error("failed to start embedded NATS server", "error", err)
		os.Exit(1)
	}
	defer bus.Close()

	db, err := persistence.Connect(context.Background(), cfg.PostgresDSN)
	if err != nil {
		logger.Error("failed to create postgres connection pool", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Postgres may legitimately be unreachable at startup (see
	// service-runtime spec); log it and keep running. The health check
	// endpoint reports live status on every request.
	migrationsOK := true
	if err := persistence.Migrate(cfg.PostgresDSN); err != nil {
		logger.Warn("could not apply database migrations at startup; will keep running and retry is manual", "error", err)
		migrationsOK = false
	} else {
		logger.Info("database migrations up to date")
	}

	webHandler, err := web.Handler()
	if err != nil {
		logger.Error("failed to initialize embedded frontend handler", "error", err)
		os.Exit(1)
	}

	openvoxdbClient, err := openvoxdb.NewClient(cfg.OpenvoxdbURL, cfg.OpenvoxdbCertFile, cfg.OpenvoxdbKeyFile, cfg.OpenvoxdbCAFile)
	if err != nil {
		logger.Error("failed to initialize openvoxdb client", "error", err)
		os.Exit(1)
	}

	classifierStore := classifier.NewStore(db.Pool)

	activityStore := activity.NewStore(db.Pool)
	activityRecorder := activity.NewRecorder(activityStore, logger)
	if migrationsOK {
		if err := activityRecorder.Start(bus); err != nil {
			logger.Error("failed to start activity recorder", "error", err)
			os.Exit(1)
		}
	}
	classifierActivityPublisher := activity.NewPublisher(bus, logger, "classifier")
	rbacActivityPublisher := activity.NewPublisher(bus, logger, "rbac")
	codemanagerActivityPublisher := activity.NewPublisher(bus, logger, "codemanager")

	// actorFromRequest extracts the acting user's identity from a
	// request's verified claims - shared by recordActivity below and
	// codemanager's own "triggered by" tracking, so neither package
	// needs to import internal/rbac itself.
	actorFromRequest := func(r *http.Request) string {
		if claims, ok := rbac.ClaimsFromContext(r.Context()); ok {
			return claims.Subject
		}
		return "system"
	}

	// recordActivity publishes under the given publisher's fixed
	// category - see design.md's "injected closure, mirroring
	// authorize" decision: classifier, rbac, and codemanager never
	// import internal/activity themselves.
	recordActivity := func(pub *activity.Publisher) func(r *http.Request, action, summary string) {
		return func(r *http.Request, action, summary string) {
			pub.Publish(action, actorFromRequest(r), summary)
		}
	}

	// Audit event emission (configurable-audit-logging) - a synchronous,
	// separate path from the activity feed above (see that change's
	// design.md for why). auditWrite/auditRead build category-bound
	// closures the same shape as recordActivity, so consuming packages
	// never hold the *auditlog.Emitter itself.
	auditEmitter := auditlog.NewEmitter(auditlog.Config{
		auditlog.CategoryNodes:           cfg.AuditNodes,
		auditlog.CategoryClassifier:      cfg.AuditClassifier,
		auditlog.CategoryRBAC:            cfg.AuditRBAC,
		auditlog.CategoryAuth:            cfg.AuditAuth,
		auditlog.CategoryCode:            cfg.AuditCode,
		auditlog.CategoryOrchestrator:    cfg.AuditOrchestrator,
		auditlog.CategoryVulnerabilities: cfg.AuditVulnerabilities,
	}, runtime.NewLogger(openAuditOutput(logger, cfg.AuditLogPath)))
	auditWrite := func(category auditlog.Category) func(r *http.Request, event auditlog.Event) {
		return func(r *http.Request, event auditlog.Event) {
			event.Actor = actorFromRequest(r)
			auditEmitter.Write(category, event)
		}
	}
	auditRead := func(category auditlog.Category) func(r *http.Request, event auditlog.Event) {
		return func(r *http.Request, event auditlog.Event) {
			event.Actor = actorFromRequest(r)
			auditEmitter.Read(category, event)
		}
	}
	// AuthService/OIDCService/orchestrator.Dispatcher have no *http.Request
	// in scope (login establishes the identity itself; a job's trigger/
	// completion audit fires well outside any request's lifetime - see
	// AuditRecorder) - they record the actor themselves, so these
	// closures take a plain auditlog.Event.
	auditWriteNoRequest := func(category auditlog.Category) func(auditlog.Event) {
		return func(event auditlog.Event) {
			auditEmitter.Write(category, event)
		}
	}
	auditWriteAuthNoRequest := auditWriteNoRequest(auditlog.CategoryAuth)

	signingKey, err := rbac.LoadSigningKey(cfg.RBACSigningKeyFile)
	if err != nil {
		logger.Error("failed to load RBAC signing key", "error", err)
		os.Exit(1)
	}
	verificationKeys, err := rbac.LoadVerificationKeys(cfg.RBACVerificationKeysDir)
	if err != nil {
		logger.Error("failed to load RBAC verification keys", "error", err)
		os.Exit(1)
	}
	// The active signer's own key is always in the verification set,
	// regardless of whether it's also present (e.g. symlinked) under
	// RBACVerificationKeysDir - a token it just issued must always verify.
	verificationKeys[cfg.RBACSigningKeyID] = &signingKey.PublicKey
	rbacStore := rbac.NewStore(db.Pool)
	revoker := rbac.NewRevoker(bus, rbacStore)
	if migrationsOK {
		if err := revoker.Start(context.Background()); err != nil {
			logger.Error("failed to start token revocation tracking", "error", err)
			os.Exit(1)
		}
	}
	issuer := rbac.NewIssuer(signingKey, cfg.RBACSigningKeyID)
	verifier := rbac.NewVerifier(verificationKeys, cfg.RBACSigningKeyID, revoker)
	authService := rbac.NewAuthService(rbacStore, issuer, verifier, revoker, auditWriteAuthNoRequest)
	oidcService := rbac.NewOIDCService(context.Background(), logger, rbacStore, issuer, rbac.OIDCConfig{
		Issuer:        cfg.OIDCIssuer,
		ClientID:      cfg.OIDCClientID,
		ClientSecret:  cfg.OIDCClientSecret,
		RedirectURL:   cfg.OIDCRedirectURL,
		Scopes:        cfg.OIDCScopes,
		UsernameClaim: cfg.OIDCUsernameClaim,
		RoleClaim:     cfg.OIDCRoleClaim,
		RoleMapping:   cfg.OIDCRoleMapping,
	}, func(action, summary string) {
		rbacActivityPublisher.Publish(action, "system", summary)
	}, auditWriteAuthNoRequest)
	rbacHandlers := rbac.NewHandlers(
		authService, rbacStore, verifier, revoker, issuer, oidcService, recordActivity(rbacActivityPublisher),
		auditWrite(auditlog.CategoryAuth), auditWrite(auditlog.CategoryRBAC), auditRead(auditlog.CategoryRBAC),
	)

	codemanagerStore := codemanager.NewStore(db.Pool)
	codemanagerDeployer := codemanager.NewDeployer(codemanager.Config{
		G10KBinPath:    cfg.G10KBinPath,
		ControlRepoURL: cfg.ControlRepoURL,
		CodeDirPath:    cfg.CodeDirPath,
	})
	codemanagerHandlers := codemanager.NewHandlers(
		codemanagerDeployer, codemanagerStore, bus, cfg.CodeWebhookSecret, logger,
		recordActivity(codemanagerActivityPublisher), actorFromRequest,
		auditWrite(auditlog.CategoryCode), auditRead(auditlog.CategoryCode),
	)

	// nodeTransport is nil when CONSOLE_NODE_TRANSPORT_ADDR is unset -
	// orchestrator endpoints still work in that case (jobs can be
	// created), but every target fails immediately with "not connected"
	// (see disabledTransport below). Matches G10K's "never fatal at
	// startup" posture (see runtime.Config).
	orchestratorStore := orchestrator.NewStore(db.Pool)

	// The initial-run trigger needs the dispatcher, the dispatcher needs
	// the transport, and the transport is where connect notifications
	// come from - so the observer is wired now and filled in below, once
	// the trigger exists. Atomic because a node can connect the moment
	// the transport binds, which is before that assignment. A node that
	// connects inside that window misses its automatic run, which the
	// design already tolerates (see design.md's Risks).
	var initialRunTrigger atomic.Pointer[initialrun.Trigger]

	var nodeTransport *nodetransport.Server
	if cfg.NodeTransportAddr != "" {
		nodeTransport, err = nodetransport.New(nodetransport.Config{
			ListenAddr: cfg.NodeTransportAddr,
			CertFile:   cfg.NodeTransportCertFile,
			KeyFile:    cfg.NodeTransportKeyFile,
			CAFile:     cfg.NodeTransportCAFile,
			OnNodeConnect: func(certname string) {
				if t := initialRunTrigger.Load(); t != nil {
					t.Notify(certname)
				}
			},
		})
		if err != nil {
			logger.Error("failed to initialize node transport", "error", err)
			os.Exit(1)
		}
		logger.Info("node transport ready", "addr", nodeTransport.Addr())
	} else {
		logger.Warn("CONSOLE_NODE_TRANSPORT_ADDR not set; node-agent connections are disabled")
	}
	if nodeTransport != nil {
		defer nodeTransport.Close()
	}

	// transport is disabledTransport (every dispatch fails immediately
	// with "not connected") when nodeTransport is nil - orchestrator
	// endpoints still work in that case (jobs can be created), just with
	// every target failing immediately.
	var transport orchestrator.Transport = disabledTransport{}
	if nodeTransport != nil {
		transport = nodeTransport
	}

	orchestratorActivityPublisher := activity.NewPublisher(bus, logger, "orchestrator")
	dispatcher := orchestrator.NewDispatcher(orchestratorStore, transport, logger)
	dispatcher.SetReportCorrelator(orchestrator.NewReportCorrelator(orchestratorStore, openvoxdbClient, logger))
	dispatcher.SetActivityRecorder(func(action, actor, summary string) {
		orchestratorActivityPublisher.Publish(action, actor, summary)
	})
	dispatcher.SetAuditRecorder(auditWriteNoRequest(auditlog.CategoryOrchestrator))
	orchestratorHandlers := orchestrator.NewHandlers(orchestratorStore, dispatcher, actorFromRequest, auditRead(auditlog.CategoryOrchestrator))

	// A node that enrols and connects has nothing in openvoxdb until it
	// runs, so the console would show it as an empty row until its own
	// scheduled run came around. Dispatch that first run for it. Inert
	// without a node transport, since there would be no connections to
	// observe. The job is recorded and audited like any other.
	//
	// Constructed here but NOT started: its queue accepts notifications
	// immediately, so a node connecting during the rest of startup is
	// buffered rather than dropped, while no dispatch happens until the
	// workers start below - after dispatcher.Run, which is the only
	// consumer of the dispatcher's unbuffered outcomes channel. A
	// dispatch before that blocks with nobody receiving, leaving its job
	// stuck at "running".
	var initialRunTriggerImpl *initialrun.Trigger
	if nodeTransport != nil {
		initialRunTriggerImpl = initialrun.NewTrigger(
			initialrun.NewStore(db.Pool),
			func(ctx context.Context, certname string) (bool, error) {
				_, err := openvoxdbClient.NodeByCertname(ctx, certname)
				switch {
				case errors.Is(err, openvoxdb.ErrNodeNotFound):
					return false, nil
				case err != nil:
					return false, err
				default:
					return true, nil
				}
			},
			func(ctx context.Context, certname string) error {
				job, err := orchestratorStore.CreateJob(ctx, orchestrator.JobKindRun, "", "", nil,
					[]string{certname}, initialrun.TriggeredBy)
				if err != nil {
					return err
				}
				dispatcher.DispatchRun(ctx, job)
				return nil
			},
			logger,
		)
		initialRunTrigger.Store(initialRunTriggerImpl)
	}

	agentDistHandlers := agentdist.NewHandlers(agentdist.Config{
		ConsoleBaseURL:   cfg.ConsoleBaseURL,
		TransportAddr:    cfg.NodeTransportPublicAddr,
		PuppetServerAddr: cfg.PuppetServerPublicAddr,
	})

	// connRegistry stays a nil nodeconnectivity.Registry (not a typed-nil
	// *nodetransport.Registry) when nodeTransport itself is nil, so
	// Handlers.listConnected's nil check works correctly rather than
	// panicking on a nil receiver.
	var connRegistry nodeconnectivity.Registry
	if nodeTransport != nil {
		connRegistry = nodeTransport.Registry()
	}

	// caClient is nil when CONSOLE_CA_CLIENT_URL is unset - certificate
	// status then simply reports "unknown" for every node, rather than
	// the console failing to start (same posture as node transport
	// above). See operations.md for why this credential is unusually
	// privileged and kept in its own config/package.
	var caClient nodeconnectivity.CertStatusClient
	if cfg.CAClientURL != "" {
		client, err := certstatus.New(certstatus.Config{
			URL:      cfg.CAClientURL,
			CertFile: cfg.CAClientCertFile,
			KeyFile:  cfg.CAClientKeyFile,
			CAFile:   cfg.CAClientCAFile,
		})
		if err != nil {
			// Degrade, don't die. Certificate reporting is optional and
			// already has a defined "off" state - every node reports
			// status "unknown" - so an unreadable or missing credential
			// must not take down classification, orchestration and the
			// dashboards with it. Exiting here also produced a crash
			// loop that hid the real cause behind restart spam.
			logger.Error("failed to initialize certificate status client; "+
				"certificate status reporting is disabled", "error", err)
		} else {
			caClient = client
		}
	} else {
		logger.Warn("CONSOLE_CA_CLIENT_URL not set; certificate status reporting is disabled")
	}

	// infraCerts is derived entirely from cert files/URLs already
	// configured above - no new configuration. Each derivation is
	// best-effort: a failure just means that one certname won't be
	// flagged as infrastructure, logged as a warning rather than
	// failing console startup, matching connRegistry/caClient's own
	// nil-safe posture. See design.md in add-infrastructure-cert-detection.
	infraCerts := &infracert.Set{}
	addSelfCertname := func(label, certFile string) {
		if certFile == "" {
			return
		}
		name, err := infracert.SelfCertname(certFile)
		if err != nil {
			// label is a sentence fragment meant to be interpolated -
			// logged bare it reads as nonsense, so interpolate it here
			// the same way Add does below.
			logger.Warn("failed to determine infrastructure certname",
				"certificate", "the certificate this console uses "+label,
				"file", certFile, "error", err)
			return
		}
		infraCerts.Add(name, "the certificate this console uses "+label)
	}
	addSelfCertname("to connect to openvoxdb", cfg.OpenvoxdbCertFile)

	// Only when the CA-client credential is actually in use. Its file
	// paths are configured unconditionally (so the feature switches on
	// with one variable), but with no URL set nothing reads them - and
	// warning about a file we will never open is pure noise.
	caClientCertFile := cfg.CAClientCertFile
	if cfg.CAClientURL == "" {
		caClientCertFile = ""
	}
	addSelfCertname("as its CA-client credential for certificate status/management", caClientCertFile)
	addSelfCertname("for its node transport's own server identity", cfg.NodeTransportCertFile)

	addPeerCertname := func(label, addr, certFile, keyFile, caFile string) {
		if addr == "" {
			return
		}
		name, err := infracert.PeerCertname(context.Background(), addr, certFile, keyFile, caFile)
		if err != nil {
			logger.Warn("failed to determine infrastructure certname", "server", label, "error", err)
			return
		}
		infraCerts.Add(name, label+"'s own server certificate")
	}
	addPeerCertname("openvoxdb", cfg.OpenvoxdbURL, cfg.OpenvoxdbCertFile, cfg.OpenvoxdbKeyFile, cfg.OpenvoxdbCAFile)
	addPeerCertname("openvoxserver", cfg.CAClientURL, cfg.CAClientCertFile, cfg.CAClientKeyFile, cfg.CAClientCAFile)

	nodeConnectivityHandlers := nodeconnectivity.NewHandlers(connRegistry, caClient, infraCerts, auditWrite(auditlog.CategoryNodes), auditRead(auditlog.CategoryNodes), logger)

	if migrationsOK {
		bootstrapAdmin(context.Background(), logger, rbacStore, cfg)
	}

	metrics := runtime.NewMetrics()
	metrics.GaugeFunc("console_node_agent_connected", "Nodes currently connected to this instance's node transport.",
		func() float64 {
			if nodeTransport == nil {
				return 0
			}
			return float64(nodeTransport.Registry().Len())
		})
	metrics.GaugeFunc("console_orchestrator_pending_dispatches", "Dispatch requests this instance has sent and is awaiting a response for.",
		func() float64 { return float64(dispatcher.PendingCount()) })
	metrics.GaugeFunc("console_rbac_revoked_tokens", "Currently-tracked revoked token count.",
		func() float64 { return float64(revoker.Len()) })

	// Vulnerability tracking (add-vulnerability-tracking). Provider types
	// are compiled in and registered explicitly here; instances are
	// configured at runtime. Credential fields need
	// CONSOLE_SECRETS_KEY_FILE - without it, providers without credentials
	// (OSV) still work and the rest are refused at configuration time.
	var secretsSealer *sealer.Sealer
	if cfg.SecretsKey != nil {
		secretsSealer, err = sealer.New(cfg.SecretsKey)
		if err != nil {
			logger.Error("invalid CONSOLE_SECRETS_KEY_FILE", "error", err)
			os.Exit(1)
		}
	}
	vulnRegistry := vulnerability.NewRegistry()
	if err := vulnRegistry.Register(osv.Type, tenable.Type); err != nil {
		logger.Error("failed to register vulnerability provider types", "error", err)
		os.Exit(1)
	}
	vulnStore := vulnerability.NewStore(db.Pool, vulnRegistry, secretsSealer)
	vulnEngine := vulnerability.NewEngine(db.Pool)
	vulnScheduler := vulnerability.NewScheduler(vulnStore, vulnEngine,
		vulnerability.NewLeases(db.Pool, vulnerability.DefaultLeaseTTL), openvoxdbClient,
		vulnerability.Deps{Pool: db.Pool, HTTPClient: &http.Client{}, Logger: logger}, logger)

	mux := http.NewServeMux()
	mux.Handle("/health", runtime.HealthHandler(db, bus))
	mux.Handle("/metrics", metrics.Handler())
	// Unauthenticated, like /health - the version string isn't sensitive,
	// and the footer that displays it (see layout.html.tmpl) renders
	// before login on every page.
	mux.HandleFunc("/api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"version": runtime.Version})
	})
	rbacHandlers.Register(mux)
	inventory.NewHandlers(openvoxdbClient, caClient, auditWrite(auditlog.CategoryNodes), auditRead(auditlog.CategoryNodes)).Register(mux, verifier.Authorize)
	nodeConnectivityHandlers.Register(mux, verifier.Authorize)
	packageinventory.NewHandlers(openvoxdbClient, transport, auditWrite(auditlog.CategoryNodes), auditRead(auditlog.CategoryNodes)).Register(mux, verifier.Authorize)
	reporting.NewHandlers(openvoxdbClient, auditRead(auditlog.CategoryNodes)).Register(mux, verifier.Authorize)
	classifier.NewHandlers(
		classifierStore, recordActivity(classifierActivityPublisher),
		auditWrite(auditlog.CategoryClassifier), auditRead(auditlog.CategoryClassifier),
	).Register(mux, verifier.Authorize)
	encapi.NewHandlers(classifierStore, openvoxdbClient).Register(mux, verifier.Authorize)
	groupnodes.NewHandlers(classifierStore, openvoxdbClient, auditRead(auditlog.CategoryClassifier)).Register(mux, verifier.Authorize)
	vulnerability.NewHandlers(vulnStore, vulnEngine, vulnScheduler, openvoxdbClient,
		auditWrite(auditlog.CategoryVulnerabilities), auditRead(auditlog.CategoryVulnerabilities)).Register(mux, verifier.Authorize)
	activity.NewHandlers(activityStore).Register(mux, verifier.Authorize)
	codemanagerHandlers.Register(mux, verifier.Authorize)
	orchestratorHandlers.Register(mux, verifier.Authorize)
	agentDistHandlers.Register(mux)
	mux.Handle("/", webHandler)

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: runtime.WrapMux(mux, metrics),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go runtime.PollDependencyHealth(ctx, metrics, db, bus)
	go dispatcher.Run(ctx)

	// Workers start only now: see the construction comment above.
	if initialRunTriggerImpl != nil {
		initialRunTriggerImpl.Start(ctx)
		defer initialRunTriggerImpl.Stop()
	}
	// The scheduler's tables come from migrations; with them unapplied it
	// would only log failures every tick. Syncs stop when ctx is cancelled
	// (their leases simply expire if the process exits first).
	if migrationsOK {
		go vulnScheduler.Run(ctx)
	}

	go func() {
		<-ctx.Done()
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("error during HTTP server shutdown", "error", err)
		}
	}()

	logger.Info("console ready", "addr", cfg.HTTPAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("HTTP server error", "error", err)
		os.Exit(1)
	}
}

// disabledTransport is the orchestrator.Transport used when
// CONSOLE_NODE_TRANSPORT_ADDR is unset - every dispatch fails immediately
// as "not connected" rather than the console failing to start (same
// "never fatal at startup" posture as G10K/OIDC above).
type disabledTransport struct{}

func (disabledTransport) Dispatch(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
	return nil, nodetransport.ErrNodeNotConnected
}

// bootstrapAdmin creates the very first admin user (with every permission)
// when the users table is empty and CONSOLE_BOOTSTRAP_ADMIN_USERNAME/
// _PASSWORD are set. It's a no-op, not a fatal error, in every other case
// - a database that already has users, or one where the operator hasn't
// (yet) provided bootstrap credentials.
func bootstrapAdmin(ctx context.Context, logger *slog.Logger, store *rbac.Store, cfg runtime.Config) {
	empty, err := store.UsersEmpty(ctx)
	if err != nil {
		logger.Warn("could not check for existing users; skipping admin bootstrap", "error", err)
		return
	}
	if !empty {
		return
	}
	if cfg.BootstrapAdminUsername == "" || cfg.BootstrapAdminPassword == "" {
		logger.Warn("no users exist yet and CONSOLE_BOOTSTRAP_ADMIN_USERNAME/PASSWORD are not set; nobody can log in until a user is created")
		return
	}

	role, err := store.CreateRole(ctx, "admin", allPermissions)
	if err != nil {
		logger.Error("failed to bootstrap admin role", "error", err)
		return
	}
	user, err := store.CreateUser(ctx, cfg.BootstrapAdminUsername, cfg.BootstrapAdminPassword)
	if err != nil {
		logger.Error("failed to bootstrap admin user", "error", err)
		return
	}
	if err := store.AssignRole(ctx, user.ID, role.ID); err != nil {
		logger.Error("failed to assign admin role to bootstrap user", "error", err)
		return
	}
	logger.Info("bootstrapped first admin user", "username", cfg.BootstrapAdminUsername)
}

// openAuditOutput returns where audit events should be written: stdout
// when path is empty, or the opened file at path. Never fatal - matches
// this project's "never fatal at startup" posture (same as the node
// transport, OIDC discovery, see runtime.Config) - a bad path degrades
// to the default rather than refusing to start.
func openAuditOutput(logger *slog.Logger, path string) io.Writer {
	if path == "" {
		return os.Stdout
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		logger.Warn("could not open configured audit log path; falling back to stdout", "path", path, "error", err)
		return os.Stdout
	}
	return f
}
