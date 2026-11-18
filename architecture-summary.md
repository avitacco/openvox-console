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

**Decision (revised by `add-group-hierarchy`):** groups form a single tree under
a built-in root, "All Nodes". The first design was flat groups ordered by
explicit priority alone, to avoid Puppet Enterprise's confusing inheritance.
That was reversed because access to nodes is to be granted per group and
inherited down the tree, which needs a child's nodes to be a subset of its
parent's. What came back is the tree, not the confusion:

- **Membership narrows downward.** A group matches a node only if its parent
  does, for pinned nodes as well as rule matches. A rule is a flat list of fact
  conditions, all of which or any of which must hold.
- **A child always wins.** When matching groups set the same class parameter,
  top-scope parameter or environment, a descendant's value beats its
  ancestors' whatever their priorities; a parent can never silently override
  its own child. Priority decides between groups that are not ancestor and
  descendant.
- **One total order.** "Child wins, priority otherwise" loops on its own (parent
  10, child 1, unrelated 5), so each group ranks as its own priority or its
  parent's rank if higher, and groups merge in order of rank, then depth, then
  priority. The root ranks below everything and has no priority.
- **The override is reported.** When a child wins over a higher-priority
  ancestor, the console warns on the group, in the list, in the activity log
  and in the server log. It never fails a save or a classification, and the
  ENC response carries no trace of it.

Ancestry is a `parent_id` column resolved in memory by `classifier.Tree`, which
every caller (the ENC endpoint, node counts, a group's node list, job targets)
uses, so they cannot disagree about who is in a group.

**Class discovery (added by `add-class-discovery-and-run-options`):** the
group page offers the classes each environment's code defines, with their
parameters, types and defaults.

- **The source is openvoxserver**, `GET /puppet/v3/environment_classes`, over
  the connection tasks already use. It is the only source that is both
  correct (Puppet's own parser) and complete; the console parses no Puppet
  code. The endpoint needs an `auth.conf` rule the default server lacks, which
  installation adds for the console's certificate.
- **Held in memory per instance** (`internal/classcatalog`), five minutes
  fresh, expired on every instance by `codemanager.deployed`, refreshed on
  request. It is derived data the server can reproduce, so nothing is stored
  or synchronised. A server that cannot be reached leaves the last answer
  standing, marked stale.
- **Advice, never a gate.** A group naming a class, parameter or value that
  does not match the code is marked, saved unchanged, and classified
  unchanged. Nothing on the classification path imports the catalogue, and a
  test holds that import boundary, so discovery cannot affect an ENC answer.
- Unavailable discovery - no connection, the rule missing, the server down -
  degrades to typed class names and marks nothing.

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

**Node group scope (added by `add-group-scoped-node-access`):** a role's
permissions apply to the node groups it names and, by inheritance, to every
group beneath them; the root group is fleet-wide.

- The token carries, per permission, the *group ids* that permission applies
  to - not node names. Membership follows facts and must not be frozen at
  login, so groups are resolved to nodes when a request is served
  (`internal/nodescope`, cached for 30 seconds per set of groups). A change to
  a role takes effect at the next token refresh, like a permission change.
- One rule decides which scope an endpoint uses: the scope of the permission
  that gates it. There is no separate table of which permission governs which
  data.
- A token that states no scope at all - a service token, or an access token
  issued before this existed - is fleet-wide. A user token always states one,
  even when it is empty.
- A node outside the caller's scope answers exactly as a node that does not
  exist, and every count is taken over in-scope nodes only.
- Actions follow the same rule (added by `add-group-scoped-node-actions`): a
  run, task, plan, upgrade, removal or certificate action is refused with a 403
  unless every node it names is in the caller's scope for that action's
  permission. The check comes before any lookup, so a hidden node and an
  unknown name are refused identically, and a multi-node action is refused
  whole. Read and action scope are independent because scope is per
  permission.
- Jobs are scoped as the record of an action: listed when any of their nodes is
  in scope, showing only those nodes.
- Not scoped: the activity log (the audit surface), group definitions, service
  tokens, code deploys, the ENC endpoint, and the console's own automatic
  client upgrades.

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
but that benefit does not materialize here: the upstream `pxp-agent` is
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
- Run options (no-op, environment, debug) are a **separate request**,
  `run_with_options`, not parameters on `run`. Every client that predates
  them ignores parameters on `run` and would carry out a no-op request as an
  enforcing run; an action it does not know, it refuses. So a plain run is
  byte-for-byte what it always was, on every client version, and a run with
  options either happens as asked or does not happen. Both ends decode the
  options strictly, so a future option is likewise never silently dropped.
- A requested environment is a request: when classification sets one, Puppet
  uses that. The console does not make the ENC answer differently for one
  run; it reads the environment each run's report records and marks the
  nodes where it differs.

**Targets and schedules** (`add-pql-targeting-and-scheduled-jobs`). A job's
nodes are a list, a PQL query, or a node group, resolved by one
`TargetResolver` whether the request came over HTTP, from the preview, or
from a schedule. For node scope, names are all or nothing while a query or a
group is narrowed to the caller's nodes; a caller who is not fleet-wide may
not use a subquery, which would let a query about their own node depend on
someone else's facts.

A schedule (`internal/scheduler`) adds only "when" and "as whom". It fires
through the same `orchestrator.Starter` as an HTTP request, with its owner's
permission and node scope read at that moment. Firing is a compare-and-set on
the schedule's next start rather than a lease, so each due time is taken by
exactly one instance without a leader, and it is at most once: the claim is
committed before the job is created, and a firing lost to a crash is not
retried. A firing found more than 15 minutes late is recorded as missed.

A plan is an ordered list of run and task steps on one set of nodes,
started through the same `Starter` and stopped at the first step that
fails; the steps after it are closed as not run. A saved plan
(`internal/plans`) is only a template: it names no nodes and holds no
sensitive value, and running one means posting its steps as an ordinary
plan request, so there is one start path and the caller sends exactly
what will run. A scheduled plan holds its own copy of the steps rather
than a reference, because a schedule fires with its owner's access while
any operator may edit a saved plan.

Local accounts are governed by one account policy stored in Postgres
(`internal/rbac`): password rules applied wherever a password is set, and
a temporary lockout after repeated failed password logins, counted on the
user row with a single atomic update so that every instance counts
together. A locked account is refused in exactly the words a wrong
password is, so the login endpoint still does not reveal which usernames
exist.

Which tasks a caller may run is a second limit on `orchestrator:run`
(`internal/taskscope`). A role, or a service token, carries a list of task
patterns; a caller's token carries one grant per role, pairing that role's
patterns with the role's own node groups, so that a narrow role on one
tier is not widened by a broad role on another. The check sits in
`orchestrator.Starter`, which every start, plan step, saved-plan save and
schedule firing passes through, and runs on the task's name before the
catalogue is read, so a task a caller may not run and one that does not
exist are refused alike.

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
  join: the secret is required too, and a dialed peer must present a
  certificate valid for the name it was dialed by.
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
