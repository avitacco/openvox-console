// Package app is the console's composition root: it builds every
// subsystem the active run mode calls for and runs them until the given
// context is cancelled.
//
// It exists so that what a mode starts is decided in one place rather
// than spread through the binary's entrypoint - see the run-modes
// capability, and design.md's "A mode table, not scattered conditionals"
// in openspec/changes/add-console-run-modes.
package app

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"

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
	"github.com/voxpupuli/enterprise-console/internal/leases"
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

// Run builds and starts the subsystems the configured run mode calls
// for, and blocks until ctx is cancelled or a subsystem fails.
//
// Startup failures are returned rather than exiting the process, so that
// every deferred shutdown below actually runs and so a test can start an
// instance without taking the test binary down with it.
func Run(ctx context.Context, cfg runtime.Config, logger *slog.Logger) error {
	// Resolved first: everything below consults it to decide whether to
	// build and start a given subsystem at all.
	surface, err := surfaceFor(cfg.Mode)
	if err != nil {
		return err
	}

	// Embedded NATS has no external dependency to fail on; a startup
	// failure here is unexpected and fatal.
	//
	// With no peers configured this is exactly what it always was: an
	// in-process server with no listener. Peers turn it into one member
	// of a cluster, which is what carries revocation and activity events
	// between instances.
	bus, err := messaging.StartWith(messaging.Config{
		ListenAddr:     cfg.ClusterAddr,
		LeafListenAddr: cfg.ClusterLeafAddr,
		Peers:          cfg.PeerList(),
		Leaf:           cfg.EffectiveClusterMode() == runtime.ClusterModeLeaf,
		Secret:         cfg.ClusterSecret,
	})
	if err != nil {
		return fmt.Errorf("failed to start embedded NATS server: %w", err)
	}
	defer bus.Close()
	if cfg.Clustered() {
		logger.Info("internal bus clustered",
			"mode", cfg.EffectiveClusterMode(), "listen", bus.ClusterAddr(),
			"leaf", bus.LeafAddr(), "peers", cfg.PeerList())
	}

	db, err := persistence.Connect(context.Background(), cfg.PostgresDSN)
	if err != nil {
		return fmt.Errorf("failed to create postgres connection pool: %w", err)
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
		return fmt.Errorf("failed to initialize embedded frontend handler: %w", err)
	}

	openvoxdbClient, err := openvoxdb.NewClient(cfg.OpenvoxdbURL, cfg.OpenvoxdbCertFile, cfg.OpenvoxdbKeyFile, cfg.OpenvoxdbCAFile)
	if err != nil {
		return fmt.Errorf("failed to initialize openvoxdb client: %w", err)
	}

	classifierStore := classifier.NewStore(db.Pool)

	activityStore := activity.NewStore(db.Pool)
	activityRecorder := activity.NewRecorder(activityStore, logger)
	// Only modes that name this worker subscribe. Every mode still
	// *publishes* activity events - a classification change on a web
	// instance must be recorded - but exactly one subscriber persists
	// them, which is what stops a clustered deployment writing one row
	// per running instance.
	if migrationsOK && surface.HasWorker(workerActivityRecorder) {
		if err := activityRecorder.Start(bus); err != nil {
			return fmt.Errorf("failed to start activity recorder: %w", err)
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

	// The signing key is read only by a mode that issues tokens. Every
	// other mode verifies with public keys alone, and never opens the
	// private key at all - see the run-modes capability's "An unused
	// credential is not loaded".
	var signingKey *ecdsa.PrivateKey
	if cfg.Mode.IssuesTokens() {
		signingKey, err = rbac.LoadSigningKey(cfg.RBACSigningKeyFile)
		if err != nil {
			return fmt.Errorf("failed to load RBAC signing key: %w", err)
		}
	}
	verificationKeys, err := rbac.LoadVerificationKeys(cfg.RBACVerificationKeysDir)
	if err != nil {
		return fmt.Errorf("failed to load RBAC verification keys: %w", err)
	}
	// The active signer's own key is always in the verification set,
	// regardless of whether it's also present (e.g. symlinked) under
	// RBACVerificationKeysDir - a token it just issued must always verify.
	//
	// A non-issuing mode has no signer of its own, so its verification
	// set comes entirely from RBACVerificationKeysDir - which LoadConfig
	// requires for exactly that reason.
	if signingKey != nil {
		verificationKeys[cfg.RBACSigningKeyID] = &signingKey.PublicKey
	}
	rbacStore := rbac.NewStore(db.Pool)
	revoker := rbac.NewRevoker(bus, rbacStore)
	if migrationsOK {
		if err := revoker.Start(context.Background()); err != nil {
			return fmt.Errorf("failed to start token revocation tracking: %w", err)
		}
	}
	verifier := rbac.NewVerifier(verificationKeys, cfg.RBACSigningKeyID, revoker)

	// Everything from here to rbacHandlers exists only to issue and
	// manage tokens, which is the rbac route group's job. A mode without
	// that group leaves these nil; its route builder is never invoked.
	var rbacHandlers *rbac.Handlers
	if surface.HasRoute(routeRBAC) {
		issuer := rbac.NewIssuer(signingKey, cfg.RBACSigningKeyID)
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
		rbacHandlers = rbac.NewHandlers(
			authService, rbacStore, verifier, revoker, issuer, oidcService, recordActivity(rbacActivityPublisher),
			auditWrite(auditlog.CategoryAuth), auditWrite(auditlog.CategoryRBAC), auditRead(auditlog.CategoryRBAC),
		)
	}

	codemanagerStore := codemanager.NewStore(db.Pool)
	// A malformed sources file IS fatal, unlike an absent one: the
	// operator asked for those repos, and starting with code deployment
	// quietly inactive would look exactly like a working console until
	// the first push failed to deploy. LoadConfig has already refused
	// the case where both forms are set, so at most one branch applies.
	codeSources, err := loadCodeSources(cfg)
	if err != nil {
		return fmt.Errorf("invalid code source configuration: %w", err)
	}
	codemanagerDeployer := codemanager.NewDeployer(codemanager.Config{
		G10KBinPath: cfg.G10KBinPath,
		Sources:     codeSources,
		CodeDirPath: cfg.CodeDirPath,
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
	if cfg.NodeTransportAddr != "" && surface.HasListener(listenerNodeTransport) {
		nodeTransport, err = nodetransport.New(nodetransport.Config{
			ListenAddr: cfg.NodeTransportAddr,
			CertFile:   cfg.NodeTransportCertFile,
			KeyFile:    cfg.NodeTransportKeyFile,
			CAFile:     cfg.NodeTransportCAFile,

			ClusterAddr:   cfg.NodeTransportClusterAddr,
			ClusterPeers:  cfg.NodeTransportPeerList(),
			ClusterSecret: cfg.NodeTransportClusterSecret,
			OnNodeConnect: func(certname string) {
				if t := initialRunTrigger.Load(); t != nil {
					t.Notify(certname)
				}
			},
		})
		if err != nil {
			return fmt.Errorf("failed to initialize node transport: %w", err)
		}
		logger.Info("node transport ready", "addr", nodeTransport.Addr())
		if cfg.NodeTransportClustered() {
			logger.Info("node transport clustered",
				"listen", nodeTransport.ClusterAddr(), "peers", cfg.NodeTransportPeerList())
		}
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
			return fmt.Errorf("invalid CONSOLE_SECRETS_KEY_FILE: %w", err)
		}
	}
	// One Leases per instance, shared by every singleton it runs: each
	// unit of work takes its own named lease, so one instance can hold
	// the vulnerability sync while another holds a code deploy.
	singletonLeases := leases.New(db.Pool, leases.DefaultTTL)

	vulnRegistry := vulnerability.NewRegistry()
	if err := vulnRegistry.Register(osv.Type, tenable.Type); err != nil {
		return fmt.Errorf("failed to register vulnerability provider types: %w", err)
	}
	vulnStore := vulnerability.NewStore(db.Pool, vulnRegistry, secretsSealer)
	vulnEngine := vulnerability.NewEngine(db.Pool)
	vulnScheduler := vulnerability.NewScheduler(vulnStore, vulnEngine,
		singletonLeases, openvoxdbClient,
		vulnerability.Deps{Pool: db.Pool, HTTPClient: &http.Client{}, Logger: logger}, logger)

	// Route registration is expressed as one builder per named group, so
	// that which groups run is decided by the mode table (see surface.go)
	// rather than by the order of statements here. A group whose mode does
	// not name it is never registered, and a request for it falls through
	// to the mux's own not-found - see the run-modes capability's
	// "Mode-determined service surface".
	groupResolver := groupnodes.NewResolver(classifierStore, openvoxdbClient)
	packageHandlers := packageinventory.NewHandlers(openvoxdbClient, transport, auditWrite(auditlog.CategoryNodes), auditRead(auditlog.CategoryNodes))
	// The package catalogue's group filter needs the classifier, which
	// packageinventory deliberately doesn't import. It also has to
	// enforce classifier:read itself: the catalogue route is gated on
	// nodes:read, and filtering packages by group would otherwise tell
	// a caller without it which nodes are in that group.
	packageHandlers.SetGroupResolver(func(r *http.Request, groupID int64) ([]string, error) {
		claims, ok := rbac.ClaimsFromContext(r.Context())
		if !ok || !claims.HasPermission("classifier:read") {
			return nil, packageinventory.ErrGroupForbidden
		}
		return groupResolver.GroupCertnames(r.Context(), groupID)
	})
	// The repository overview's node counts need openvoxdb and the
	// classifier, neither of which codemanager imports. Both readings
	// are assembled here and handed in, the same shape as
	// packageHandlers.SetGroupResolver above.
	// Serializes deploys of the same environment across instances: web
	// mode carries the code manager, so two web instances can receive
	// deploy webhooks concurrently and would otherwise run g10k against
	// the same directory at the same time.
	codemanagerHandlers.SetDeployLeases(singletonLeases)
	codemanagerHandlers.SetUsageResolver(codemanager.NewUsageResolver(
		// Assigned: what classification would send each node. The
		// resolver merges every matching group by priority, so a node
		// matching several groups yields the one environment it would
		// actually be sent to rather than one entry per group.
		func(ctx context.Context) ([]codemanager.NodeEnvironment, error) {
			assigned, err := groupResolver.AssignedEnvironments(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]codemanager.NodeEnvironment, 0, len(assigned))
			for _, node := range assigned {
				out = append(out, codemanager.NodeEnvironment{
					Certname:    node.Certname,
					Environment: node.Environment,
				})
			}
			return out, nil
		},
		// Reporting: what each node last actually ran. The nodes entity
		// already carries report_environment, so this is one query.
		func(ctx context.Context) ([]codemanager.NodeEnvironment, error) {
			nodes, err := openvoxdbClient.Nodes(ctx)
			if err != nil {
				return nil, err
			}
			reporting := make([]codemanager.NodeEnvironment, 0, len(nodes))
			for _, node := range nodes {
				reporting = append(reporting, codemanager.NodeEnvironment{
					Certname:    node.Certname,
					Environment: node.ReportEnvironment,
				})
			}
			return reporting, nil
		},
	))

	routeBuilders := map[string]func(mux *http.ServeMux){
		routeOperational: func(mux *http.ServeMux) {
			mux.Handle("/health", runtime.HealthHandler(cfg.Mode, db, bus))
			mux.Handle("/metrics", metrics.Handler())
			// Unauthenticated, like /health - the version string isn't sensitive,
			// and the footer that displays it (see layout.html.tmpl) renders
			// before login on every page.
			mux.HandleFunc("/api/v1/version", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"version": runtime.Version})
			})
		},
		routeRBAC: func(mux *http.ServeMux) {
			rbacHandlers.Register(mux)
		},
		routeInventory: func(mux *http.ServeMux) {
			inventory.NewHandlers(openvoxdbClient, caClient, auditWrite(auditlog.CategoryNodes), auditRead(auditlog.CategoryNodes)).Register(mux, verifier.Authorize)
		},
		routeNodeConnect: func(mux *http.ServeMux) {
			nodeConnectivityHandlers.Register(mux, verifier.Authorize)
		},
		routePackages: func(mux *http.ServeMux) {
			packageHandlers.Register(mux, verifier.Authorize)
		},
		routeReporting: func(mux *http.ServeMux) {
			reporting.NewHandlers(openvoxdbClient, auditRead(auditlog.CategoryNodes)).Register(mux, verifier.Authorize)
		},
		routeClassifier: func(mux *http.ServeMux) {
			classifier.NewHandlers(
				classifierStore, recordActivity(classifierActivityPublisher),
				auditWrite(auditlog.CategoryClassifier), auditRead(auditlog.CategoryClassifier),
			).Register(mux, verifier.Authorize)
		},
		routeENC: func(mux *http.ServeMux) {
			encapi.NewHandlers(classifierStore, openvoxdbClient).Register(mux, verifier.Authorize)
		},
		routeGroupNodes: func(mux *http.ServeMux) {
			groupnodes.NewHandlers(classifierStore, openvoxdbClient, auditRead(auditlog.CategoryClassifier)).Register(mux, verifier.Authorize)
		},
		routeVulnerability: func(mux *http.ServeMux) {
			vulnerability.NewHandlers(vulnStore, vulnEngine, vulnScheduler, openvoxdbClient,
				auditWrite(auditlog.CategoryVulnerabilities), auditRead(auditlog.CategoryVulnerabilities)).Register(mux, verifier.Authorize)
		},
		routeActivity: func(mux *http.ServeMux) {
			activity.NewHandlers(activityStore).Register(mux, verifier.Authorize)
		},
		routeCodeManager: func(mux *http.ServeMux) {
			codemanagerHandlers.Register(mux, verifier.Authorize)
		},
		routeOrchestrator: func(mux *http.ServeMux) {
			orchestratorHandlers.Register(mux, verifier.Authorize)
		},
		routeAgentDist: func(mux *http.ServeMux) {
			agentDistHandlers.Register(mux)
		},
		routeWebUI: func(mux *http.ServeMux) {
			mux.Handle("/", webHandler)
		},
	}

	mux := http.NewServeMux()
	// Registered in allRoutes order, not map order: "/" is a catch-all and
	// must come last. Ranging over routeBuilders directly would be
	// nondeterministic.
	for _, name := range allRoutes {
		if !surface.HasRoute(name) {
			continue
		}
		build, ok := routeBuilders[name]
		if !ok {
			return fmt.Errorf("run mode %q names route group %q, which has no builder", cfg.Mode, name)
		}
		build(mux)
	}
	logger.Info("routes registered", "mode", cfg.Mode, "groups", surface.Routes)

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: runtime.WrapMux(mux, metrics),
	}

	// Signal handling belongs to the entrypoint, not here: ctx arrives
	// already wired to it, so a test can drive shutdown by cancelling
	// its own context instead of raising a signal at the test binary.

	if surface.HasWorker(workerDependencyHealth) {
		go runtime.PollDependencyHealth(ctx, metrics, db, bus)
	}
	if surface.HasWorker(workerDispatcher) {
		go dispatcher.Run(ctx)
	}

	// Workers start only now: see the construction comment above.
	if initialRunTriggerImpl != nil && surface.HasWorker(workerInitialRun) {
		initialRunTriggerImpl.Start(ctx)
		defer initialRunTriggerImpl.Stop()
	}
	// The scheduler's tables come from migrations; with them unapplied it
	// would only log failures every tick. Syncs stop when ctx is cancelled
	// (their leases simply expire if the process exits first).
	if migrationsOK && surface.HasWorker(workerVulnScheduler) {
		go vulnScheduler.Run(ctx)
	}
	// Fails jobs left running by an instance that stopped before its
	// dispatches finished. Leased, so exactly one instance reaps even
	// though several may run this worker.
	if migrationsOK && surface.HasWorker(workerJobReaper) {
		reaper := orchestrator.NewReaper(db.Pool, logger)
		go func() {
			ticker := time.NewTicker(jobReaperInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if _, err := singletonLeases.Hold(ctx, jobReaperLease, func(ctx context.Context) error {
						_, err := reaper.ReapOnce(ctx)
						return err
					}); err != nil {
						logger.Warn("failed to reap stale jobs", "error", err)
					}
				}
			}
		}()
	}

	logger.Info("workers started", "mode", cfg.Mode, "workers", surface.Workers)

	go func() {
		<-ctx.Done()
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("error during HTTP server shutdown", "error", err)
		}
	}()

	logger.Info("console ready", "addr", cfg.HTTPAddr, "mode", cfg.Mode)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server error: %w", err)
	}

	// ErrServerClosed is the ordinary shutdown path: the goroutine above
	// called Shutdown because ctx was cancelled.
	return nil
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

// loadCodeSources resolves the two mutually exclusive ways of declaring
// what the code manager deploys from into the single list the Deployer
// takes: a sources file naming any number of control repos, or
// CONSOLE_CONTROL_REPO_URL naming one.
//
// The single-repo form becomes a source named "control" with no prefix.
// Both halves of that matter for an upgrade: the name is what existing
// deploy history rows already carry, and the absent prefix is what
// keeps its branches deploying to environments/<branch> rather than
// moving every environment openvoxserver is compiling from.
//
// Neither configured is not an error - code deployment is simply
// inactive, and the deploy endpoints say so per request (see
// runtime.Config's "never fatal at startup" posture).
func loadCodeSources(cfg runtime.Config) (codemanager.Sources, error) {
	if cfg.CodeSourcesPath != "" {
		return codemanager.LoadSourcesFile(cfg.CodeSourcesPath)
	}
	if cfg.ControlRepoURL != "" {
		return codemanager.Sources{{
			Name:   codemanager.DefaultSourceName,
			Remote: cfg.ControlRepoURL,
		}}, nil
	}
	return nil, nil
}
