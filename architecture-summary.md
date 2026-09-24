# OpenVox Console: Architecture Summary

## Purpose

A Puppet Enterprise console equivalent for the OpenVox project, written in Go as a
single binary. It reuses openvox-server and openvoxdb for the data and compilation
plane, and builds the control plane (dashboard, classification, access control,
code deployment, orchestration) fresh, informed by where Puppet Enterprise's
architecture reflects historical retrofits rather than deliberate design.

## Goals

- Feature parity with Puppet Enterprise's console: node inventory and reporting,
  node classification, role-based access control, code deployment, and on-demand
  orchestration (jobs, tasks, plans).
- Single Go binary per component, trivially containerized.
- External contract compatibility where OpenVox agents or tooling depend on it
  (the ENC API), even where internal implementation departs from Puppet
  Enterprise's approach. Orchestration is the one area where this project
  does *not* speak an external agent's native protocol - see "7. Node
  transport and orchestrator" below for why.

## Non-goals (for v1)

- Full Ruby-language compatibility in Puppetfile parsing. Support a well-defined
  grammar covering real-world usage (variables, string interpolation, both hash
  syntaxes, module declarations), not arbitrary embedded Ruby logic.
- Multi-broker orchestration federation. Design the interface for it; defer the
  implementation until a real deployment needs more than one broker can handle.
- LDAP/SAML directory sync for RBAC. Local user/role management only.
- Encrypted (JWE) tokens. Signed (JWS) tokens are sufficient; see RBAC section.

## Reused components

| Component | Source | Role |
|---|---|---|
| openvox-server | OpenVoxProject fork of puppetserver | Catalog compilation, CA |
| openvoxdb | OpenVoxProject fork of puppetdb | Data warehouse, PQL queries, reporting |
| g10k (vendored) | voxpupuli/g10k, in-process | Puppetfile-based code deployment |

## New components and key decisions

### 1. Service topology: one binary, not many services

Puppet Enterprise runs console services, RBAC, activity, node classifier,
orchestration, and code manager as separate JVM processes coordinating over
localhost HTTP with token-passing between hops. This is a natural byproduct of
Clojure/trapperkeeper's service-per-process pattern and years of incremental
addition, not a deliberate scaling decision.

**Decision:** all of the above are internal Go packages within a single binary,
communicating via direct function calls. No internal HTTP hops, no internal
token handshakes. This is the single largest structural simplification versus
Puppet Enterprise and falls out naturally from the single-binary goal.

### 2. Node Classifier

Implements the ENC (External Node Classifier) HTTP contract that openvox-server's
classifier terminus expects, so this must remain externally compatible.
Internally, Puppet Enterprise's group-inheritance-plus-rule-evaluation-plus-pinning
model is a known source of user confusion. The internal merge/precedence logic
should be redesigned for clarity as long as it produces correct classification
output against the same external contract.

### 3. RBAC: signed JWTs, not encrypted, short-lived, with fast revocation

- Use signed JWTs (JWS), not encrypted JWTs (JWE). Encryption addresses
  confidentiality, which is not the problem here (TLS handles transport
  confidentiality, and token bearers already legitimately know their own
  claims). Encryption only adds key management and configuration surface
  without a corresponding benefit for this use case.
- Asymmetric signing (ES256). Only the issuing component holds the private key;
  any instance verifying a token needs only the public key, so verification
  scales horizontally without a shared secret or a call back to a central
  service.
- Short expiry (10 to 15 minutes) with refresh, to bound how long a compromised
  or stale token stays valid.
- `jti`-based revocation list for immediate "kill it now" cases, kept warm via
  NATS-published revocation events rather than a database check per request.
- Use an established Go JOSE library (`golang-jwt/jwt` for JWS; `lestrrat-go/jwx`
  if JWE is ever genuinely needed). Do not hand-roll token parsing or validation.

### 4. Internal messaging: embedded NATS

Puppet Enterprise's PCP broker maintains connection state and routing in-process,
which becomes a scaling constraint addressed later by a hand-built broker-to-broker
relay and distributed routing table (the compiler-federation model). That relay
logic, and its associated partition/reconnect handling, is a generic distributed
pub/sub problem, not a Puppet-specific one, and Go has mature, embeddable, already
production-hardened tooling for exactly this: NATS.

**Decision:** embed a NATS server directly in the binary (no separate process,
preserving the single-binary goal). Use NATS for:

- **Node transport for orchestration.** Managed nodes connect directly to this
  embedded NATS server (mutual TLS, the same OpenVox CA agents already hold)
  rather than to a hand-built broker speaking a separate wire protocol - see
  "7. Node transport and orchestrator" below for why this isn't PCP/PXP. In
  unclustered (single-instance) mode this is a thin, low-cost pub/sub layer.
  Clustering (or leaf nodes, for a compiler-relaying-to-primary topology) is
  available if and when multi-instance deployments are needed, without
  redesigning the delivery model - see the federation note under "Deferred"
  below.
- **Cross-instance notification.** Token revocations, code-deploy-ready
  events, and the stack status page's fleet-wide question.
- **Code deployment notification.** See Code Manager section below.

The two uses live in separate NATS **accounts** on the one server -
`CONSOLE` for the internal bus, `NODES` for the node transport - so node
traffic and internal events share no subject namespace, with no
per-subject access control needed between them.

Core NATS is at-most-once, and the design leans on that deliberately
rather than adopting JetStream: nothing durable depends on the bus alone.
A dispatch request-reply's own bounded wait defines its expiration (see
"7. Node transport and orchestrator" below); a revocation is persisted in
Postgres first and each instance periodically re-reads it, so an event the
bus loses delays enforcement by seconds rather than dropping it. The
activity log was once a NATS subscriber too, and lost events whenever no
recorder was running or keeping up; it now writes directly to Postgres
from the instance where the action happened.

### 5. Code Manager: g10k in-process, not r10k

- Use g10k, vendored and called in-process, rather than r10k. r10k is a Ruby
  gem; shelling out to it would require a Ruby runtime alongside the Go binary,
  undermining the single-binary and easy-containerization goals.
- g10k's Puppetfile parser has known gaps versus r10k's true Ruby-eval
  semantics (variable interpolation, some hash-syntax and forge/git module name
  disambiguation edge cases). Rather than working around these privately,
  contribute a proper lexer/parser (bounded grammar, not arbitrary Ruby)
  upstream to g10k. Validate compatibility with a real-world Puppetfile corpus,
  diffed against actual r10k output as an oracle.
- If g10k's current structure doesn't expose a clean importable API (typical
  for CLI-first tools), contributing that refactor upstream is itself valuable,
  not just internal plumbing.

### 6. Code distribution to compilers: event-driven, not file sync

Puppet Enterprise's file sync service stages code, then syncs it to compilers on
a five-second poll, pausing Puppet Server mid-sync to avoid serving inconsistent
catalogs. This is itself a retrofit; Puppet Enterprise's messaging layer has
already changed once before (ActiveMQ, predating PCP/websockets).

**Decision:** when the primary finishes a g10k deploy, publish a "deployment
ready" event on NATS. Each compiler subscribes and runs its own g10k deploy
against the same control repo state, in parallel, push-triggered rather than
polled. Consistency during deploy is handled via atomic directory swap
(deploy to a new versioned directory, symlink-swap on completion) rather than
pausing the server.

### 7. Node transport and orchestrator

**Decision:** managed nodes connect directly to the console's
already-embedded NATS server, rather than this project implementing the
PCP (Puppet Communications Protocol) and PXP (execution layer)
specifications.

The case for PCP/PXP would be wire compatibility with an existing agent,
but that benefit does not materialise here: the upstream `pxp-agent` is
unmaintained, so this project ships its own on-node execution client
(`node-agent-client`) either way. With both ends of the connection
shipped from this repository, there is no external consumer to be
compatible *with* - and so no reason to take on a bespoke broker, a
second wire protocol, and their maintenance.

What NATS gives instead:

- **No new broker to build or operate.** The console already embeds a
  NATS server for its internal event bus (see "4. Internal messaging"),
  so the node transport reuses a component that is already present,
  already configured, and already exercised.
- **Mature, production-hardened transport.** Reconnection, backoff,
  request-reply correlation, and TLS are solved by the library rather
  than hand-rolled per protocol.
- **A federation story that already exists.** NATS clustering and leaf
  nodes are a supported deployment topology, so multi-instance routing
  becomes configuration rather than a routing rewrite.
- **One technology to reason about.** Internal events and node
  dispatch share a single transport, its operational behavior, and its
  failure modes.

- Managed nodes connect to the console's embedded NATS server (mutual TLS,
  the same OpenVox CA agents already hold, checked against the CA's CRL) -
  see "4. Internal messaging: embedded NATS" above. A custom `nats-server`
  authentication hook maps each connection's verified certificate to a NATS
  identity in the `NODES` account, scoped to subscribing on that node's own
  subjects and to publishing nothing but replies to requests it received
  (NATS response permissions) - so one node can never observe or spoof
  another's job traffic. Per-node isolation is not something NATS has on
  its own, so the hook supplies it. A certificate revoked through the
  console is announced cluster-wide, and every instance drops that node's
  connections at once.
- `node-agent-client` is this project's own binary (cross-compiled at
  console build time and served via the agent-distribution install
  script), not a reused upstream component.
- Dispatch goes through a small interface (`given a node identity,
  deliver this request and wait for its response`), implemented as plain
  NATS request-reply. Federation turned out not to need a new
  implementation of it at all: clustering routes a dispatch to whichever
  instance holds the target node's connection, because the subject
  namespace is already per-node - and any routed instance can dispatch,
  whether or not it terminates node connections itself. See "9. Run modes
  and clustering" below.
- Job orchestration: translate console/API job requests into dispatch
  requests over a node's own NATS subject (a run/task's module+action+
  params payload), track status via NATS request-reply, correlate
  completed runs with reports pulled from openvoxdb.

### 8. High availability and failover

Puppet Enterprise's primary/replica model requires manual promotion and
demotion, largely because its JVM services carry meaningful local state.
Designing the Go services to be largely stateless, reading from Postgres (which
already has mature native replication, and is already a dependency via
openvoxdb), makes failover closer to "point a load balancer at a healthy
instance" than a scripted promotion procedure. This is a design principle to
hold throughout, not a separate component.

### 9. Run modes and clustering

The console started as one process that ran every subsystem. At fleet
sizes where the ENC endpoint alone answers tens of requests per second
while the background schedulers run a few syncs a day, welding those
together forces vertical scaling of the whole thing.

**Decision:** one image and one binary, with `CONSOLE_RUN_MODE`
selecting which subsystems an instance activates - `all` (the default,
and identical to the previous behavior), `web`, `enc`, `orchestrator`,
`worker`. What each mode activates is a single table in `internal/app`
rather than conditionals spread through startup, so the mapping is one
readable value and can be asserted on directly.

Instances form one NATS cluster, carrying both accounts:

- **Core modes mesh as routed peers**, over mutual TLS with a shared
  cluster secret. Routes carry the internal bus - which is what makes
  token revocation take effect fleet-wide, as the RBAC design assumed -
  and the node transport, which removes the sticky-routing constraint on
  orchestration: a dispatch published by any instance reaches a node
  connected to any other. A node's CA-issued certificate is not enough to
  join: the secret is required too, and a dialled peer must present a
  certificate valid for the name it was dialled by.
- **`enc` instances attach as leaf nodes**, outbound-only, so the core
  needs no network path to an instance sitting next to a compiler. A
  leaf is bound to the `CONSOLE` account alone and authenticates with
  its own secret, so an edge host can neither reach a node nor join as a
  full peer.

This was two separate servers and clusters until the node transport was
folded in as an account - one listener set, one cluster, one place TLS
and authentication are configured, and NATS's own isolation boundary in
place of process separation.

Two consequences of clustering had to be handled rather than assumed
away:

- A subscriber with an effect outside its own instance becomes a
  duplicate-effect bug once several instances subscribe. Such
  subscribers must use a NATS queue group (exactly one member handles
  each message); subscribers whose effect is confined to their own
  instance stay fan-out. The rule is documented in `internal/messaging`.
- Scheduled work that must not overlap takes a Postgres lease
  (`internal/leases`), judged by the database clock so skewed instance
  clocks still agree.

## Deferred / explicitly out of scope for v1

- ~~Multi-instance orchestration federation~~ - **implemented**; see "9. Run
  modes and clustering" below.
- LDAP/SAML sync for RBAC.
- Encrypted (JWE) tokens.
- Full Ruby-semantics Puppetfile compatibility (conditionals, loops, arbitrary
  method calls).

## Open questions to resolve during build

- Exact scope of the ENC HTTP API surface to target for classifier compatibility
  testing against a real openvox-server instance.
- Corpus sources for Puppetfile compatibility testing (public control repos,
  Forge-listed modules, r10k's own test fixtures).
