## Context

See proposal.md - Why for motivation.

The constraint shaping every decision below is the shape of
`cmd/console/main.go`: roughly 650 lines of straight-line wiring that
constructs every subsystem unconditionally, in dependency order, into one
`http.ServeMux` and one `http.Server`. There is no composition boundary to
hang a mode off. Several constructions are already conditional
(`nodeTransport` on `CONSOLE_NODE_TRANSPORT_ADDR`, `caClient` on
`CONSOLE_CA_CLIENT_URL`, `secretsSealer` on `CONSOLE_SECRETS_KEY_FILE`), and
they establish the house posture this design follows: an absent optional
dependency degrades to a documented "off" state rather than failing startup.

Two NATS servers exist today and stay separate (architecture-summary.md
sections 4 and 7): `internal/messaging.Bus` for internal events, started with
`DontListen: true`, and `internal/nodetransport.Server` for node connections,
with a real mTLS listener.

Relevant facts confirmed against the vendored dependencies rather than
assumed:

- `nats-server` v2.14.5 authenticates route connections through
  `Options.CustomRouterAuthentication` and `ClusterOpts`
  (`Username`/`Password`/`TLSConfig`), which is a different path from the
  `Options.CustomClientAuthentication` hook `nodetransport/auth.go`
  implements. Cluster routes therefore never reach that hook's nil-TLS
  branch.
- `golang-migrate` v4.19.1's Postgres driver takes a `pg_advisory_lock`
  around a migration run, so several instances starting at once serialize
  rather than racing.
- `internal/vulnerability.Leases` already implements Postgres-backed,
  database-clock-judged leasing with renewal and takeover on expiry.

## Goals / Non-Goals

**Goals:**

- One composition point where the mode-to-surface mapping is readable as
  data, not as conditionals spread through startup.
- Multi-instance correctness for state that clustering newly exposes -
  particularly anything that subscribes to an internal event and then
  *writes*, which becomes a duplicate-write bug the moment two instances
  subscribe.
- `all` mode byte-for-byte equivalent in behavior to today, so this change is
  inert for existing single-instance deployments.

**Non-Goals:**

- Merging the two NATS servers into one with accounts (see Decisions).
- Any change to the ENC endpoint's query cost - deferred, per proposal.md.
- Autoscaling, service discovery, or mode-aware load balancer configuration.
  This change makes the topology possible and documents it; wiring it into a
  particular orchestration platform is the operator's.
- Splitting the binary. One binary, one image, mode by configuration.

## Decisions

### Mode is configuration, not a subcommand

`CONSOLE_RUN_MODE`, read by `runtime.LoadConfig` like everything else, rather
than `console serve web`.

Every other operational knob in this system is an environment variable, and
the deployment targets (Docker Compose, Kubernetes) set environment variables
far more naturally than they override entrypoint arguments. A subcommand
would also collide awkwardly with the existing `--health-check` argument that
`main` handles before config loading.

*Alternative considered:* a `--mode` flag. Same effect, but splits
configuration across two mechanisms for no gain.

### A mode table, not scattered conditionals

Introduce `internal/app` holding a composition root. Startup becomes three
phases:

1. Build the shared substrate every mode needs (config, logger, Postgres
   pool, internal bus, openvoxdb client, RBAC verifier, audit emitter).
2. Consult a mode table - a single `map[Mode]Surface` where `Surface` names
   the HTTP route groups, network listeners, and background workers that mode
   activates.
3. Construct and register only what the table names.

The table is the design's centrepiece: the spec's "Mode-determined service
surface" requirement is then one readable value, testable directly, and a new
subsystem has exactly one place to declare where it belongs. Scattering `if
mode == ...` through the existing 650 lines would satisfy the same
requirement while making the mapping unreadable and its drift undetectable.

*Alternative considered:* separate `main` functions per mode. Maximally
explicit, but duplicates the substrate wiring five times and guarantees
divergence.

### Two clusters, mirroring the two servers

The internal bus and the node transport each get their own peer listener and
their own credentials: the bus clusters on one port, the transport on
another.

This preserves architecture-summary.md section 7's reasoning - the servers
are separate specifically so node traffic can never collide with or observe
internal subjects "without needing per-subject access control between the two
concerns." Clustering them separately keeps that property. The cost is two
peer listeners and two credential sets to configure, which the operator-facing
documentation has to carry.

*Alternative considered, and deliberately rejected:* one NATS server with two
accounts (internal, nodes). NATS accounts are the idiomatic isolation
primitive and would give one cluster, one listener, one credential set -
genuinely simpler to operate, and arguably stronger isolation than two
servers. It is rejected here because it reverses a documented architectural
decision and rewrites the node-facing security model as a side effect of a
scaling change. If the two-listener configuration proves burdensome in
practice, that consolidation is the right follow-up change, made on its own
merits.

### Core modes route; `enc` attaches as a leaf

`web`, `orchestrator`, `worker`, and `all` full-mesh via routes.
`enc` attaches to the internal bus as a leaf node.

`enc` mode's only cross-instance need is inbound: revocation events, so it
can reject a revoked service token. It publishes nothing and terminates no
node connections. A leaf connection is outbound-only from the `enc` instance,
so core instances need no network path back to it - which is what makes the
compiler-colocated deployment practical, where ENC instances sit wherever the
compilers sit and the console core does not reach into that network.

`enc` mode therefore joins only the internal bus cluster, never the node
transport cluster. The node transport gets routes only, no leaf nodes, which
keeps `nodetransport/auth.go`'s nil-TLS branch exactly as exposed as it is
today.

*Alternative considered:* `enc` as a full peer. One mechanism instead of two,
but every core instance would need to reach every ENC instance, and an
instance on a compiler would gain the ability to publish onto the internal
bus, which it has no reason to do.

### Event subscribers that write must use queue groups

This is the correctness trap clustering introduces, and it is not obvious.

`internal/activity.Recorder` subscribes to activity events and persists each
one to a single table. Today one process subscribes, so one row is written.
Cluster three instances that each run the recorder and every action produces
three identical activity rows.

The fix is a NATS queue subscription: subscribers sharing a queue group
receive each message exactly once between them, rather than each receiving a
copy. Every subscriber that has a side effect beyond updating instance-local
memory moves to a queue group.

Fan-out subscribers stay plain subscriptions, because they *want* every
instance to receive a copy: `rbac.Revoker`'s revocation listener updates
per-instance in-memory state, and is correct precisely because every instance
gets it.

The distinction - fan-out for instance-local state, queue group for shared
writes - is the rule to apply to every existing and future subscriber, and
belongs in the messaging package's own documentation.

### Reuse the existing lease for singleton work

`vulnerability.Leases` already does exactly what the new "Background work
runs once across the cluster" requirement describes, including judging expiry
by the database clock so skewed instance clocks still agree. Generalize it to
`internal/leases` and have `worker` mode's schedulers hold a named lease.

Code deployment gets the same treatment. `web` mode carries the code manager,
so two web instances can receive deploy webhooks concurrently and run g10k
against the same code directory. A lease named for the deploy target
serializes them; the loser reports that a deploy is already running rather
than corrupting the directory.

*Alternative considered:* NATS-based leader election. It would avoid a
Postgres round trip, but adds a consensus mechanism where a database this
system already depends on, and an implementation already written and tested
in-repo, does the job.

### Revocation needs no new mechanism

`rbac.Revoker` already publishes to `RevocationSubject` on every revocation,
already persists to Postgres as the durable source of truth, and `Start`
already hydrates in-memory state from Postgres at boot. The reason
revocation does not propagate today is solely that the bus it publishes to
has no listener and no peers.

Clustering the bus makes the existing code correct. The work is in
`internal/messaging`, not `internal/rbac` - plus a test that actually asserts
cross-instance propagation, which is what would have caught this.

### Health reports the active mode's dependencies

`runtime.HealthHandler(db, bus)` takes a fixed dependency list today. It
becomes mode-derived: the same checker interface, with the active mode
contributing its own checkers, and the response naming the mode.

This matters operationally rather than cosmetically: an `enc` instance has no
node transport, and a health endpoint that reported one would either lie or
fail, and a load balancer would drain a healthy instance.

## Risks / Trade-offs

- **A subscriber that writes is missed when converting to queue groups** →
  duplicate rows appear only under multi-instance load, which single-instance
  testing never reaches. Mitigation: audit every `bus.Subscribe` call site as
  an explicit task, classify each as fan-out or queue, and record the
  classification in the messaging package docs. The audit is small today -
  the call sites are few - and is much cheaper now than after more accrue.

- **`all` mode regresses during the wiring split** → the split touches every
  line of a 650-line function that currently has no direct test.
  Mitigation: treat `all` as the equivalence baseline - assert that the route
  set, listener set, and worker set it produces are identical before and
  after, and do the mechanical extraction before adding any mode.

- **Operators run two `all` instances and hit the duplicate-work bug** → `all`
  is the default and the obvious thing to scale. Mitigation: `all` must be
  correct under clustering, not merely correct alone; it takes leases and
  queue subscriptions like any other mode. The lease and queue work is
  therefore not optional polish for `worker` mode - it is what makes the
  default safe.

- **Cluster peer credentials are a new secret to manage** → a weak or shared
  credential on a reachable listener is a path onto the internal event bus.
  Mitigation: peer listeners bind to a private interface by default, the
  credentials are separate from all node-facing material, and startup refuses
  a configured peer listener with no credentials rather than defaulting to
  open.

- **The node transport's registry fires connect notifications on every
  instance** → with `$SYS` events propagating cluster-wide, `OnNodeConnect`
  fires N times for one connection, so `initialrun` attempts N claims.
  Mitigation: none needed - `initialrun` already claims each certname
  atomically in Postgres, and `node-transport`'s existing spec already
  requires observers to tolerate duplicate notification, naming "more than
  one console instance observes the same event" as a case. This is a
  designed-for outcome, called out here so it is not mistaken for a defect.

- **Two peer listeners is more operational surface than one** → accepted, with
  the account-based consolidation documented above as the follow-up if it
  proves painful.

## Migration Plan

The change is inert until configured. `all` with no cluster peers is the
default and is today's behavior, so an existing deployment upgrades with no
configuration change and no clustering.

Adopting the topology is incremental:

1. Upgrade every instance to the new image while still single-instance.
   Nothing changes.
2. Configure cluster peers on the existing instance and start a second `all`
   instance. This exercises clustering, lease coordination, and queue
   subscriptions before any mode split.
3. Split roles as load demands: move background work to `worker`, node
   connections to `orchestrator`, then add `enc` instances alongside the
   compilers.

Rollback at any step is setting `CONSOLE_RUN_MODE=all` and removing peer
configuration; no schema change gates the rollback, and the lease table is
additive.

## Open Questions

- Whether `enc` mode should serve the classifier's own read endpoints as well
  as the ENC endpoint. The ENC bridge does not need them, but an operator
  debugging classification against an ENC instance might. Deferrable: it adds
  routes to one mode's entry in the mode table and changes no requirement.
- Whether the `worker` mode's lease names should be per-unit (one per
  scheduler) or per-mode (one worker leader for everything). Per-unit is the
  shape `vulnerability.Leases` already has; the question is only whether any
  future worker needs finer granularity. Does not affect the specs.
