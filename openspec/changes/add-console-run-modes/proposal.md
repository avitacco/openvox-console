## Why

The console runs today as one process that starts every subsystem
unconditionally: the HTTP/UI surface, the ENC endpoint, the node transport
listener, the orchestrator dispatcher, and every background worker. A
deployment managing 30,000 nodes cannot scale that shape without scaling the
host vertically, because the components with wildly different load profiles
are welded together. The ENC endpoint alone absorbs roughly 17 requests per
second at that fleet size, while the vulnerability scheduler runs a handful of
syncs per day.

Running more than one instance is not a workaround today, for two reasons.
Node transport connections and orchestrator dispatch tracking are pinned to
whichever instance a node happens to connect to, so on-demand orchestration
is only reliable behind sticky routing. And token revocation does not
propagate: `internal/messaging` starts its NATS server with `DontListen:
true`, so the `rbac.revoked` event never leaves the process that published it.
A token revoked on one instance stays valid on every other until it expires,
which makes a second instance a security regression rather than an
improvement.

## What Changes

- Introduce a **run mode** selected at startup (`CONSOLE_RUN_MODE`), so the
  same container image starts as one of several roles rather than always
  starting everything. Modes: `all` (default), `web`, `enc`, `orchestrator`,
  `worker`.
- `all` remains the default and preserves today's single-process behavior
  exactly, so existing deployments are unaffected. **Not breaking.**
- Split `cmd/console/main.go`'s wiring so each subsystem is constructed only
  when the active mode needs it, instead of the current unconditional
  construction of all of them.
- Cluster the internal NATS bus across instances: core modes (`web`,
  `orchestrator`, `worker`) full-mesh via routes, and edge modes (`enc`)
  attach as leaf nodes. This replaces the current unclustered, listener-less
  embedded server.
- Cluster the node transport's NATS server so a dispatch published by any
  instance reaches a node holding its connection to a different instance,
  removing the sticky-routing constraint on orchestration.
- Make RBAC token revocation propagate across instances, satisfying the
  revocation requirement the `rbac` capability already states but the current
  implementation cannot meet with more than one instance running.
- Report health and readiness per mode, so a load balancer is not told an
  `enc` instance is unhealthy because it has no node transport.

Explicitly **deferred** to a follow-up change: the ENC endpoint's own query
cost (the nested N+1 in `classifier.Store.ListAllGroups`, group caching, and
`pgxpool`/openvoxdb transport tuning). That work makes a single instance
faster; this change makes many instances possible. They are independent, and
bundling them would produce one change touching most of `internal/`.

## Capabilities

### New Capabilities

- `run-modes`: Selecting a run mode at startup; which HTTP routes, listeners,
  and background workers each mode activates; how an invalid or
  under-configured mode is refused; and how health and readiness are reported
  per mode.

### Modified Capabilities

- `messaging`: The existing requirement states the embedded NATS server runs
  "in unclustered mode". That becomes a clustered-capable server with a real
  listener, plus a new requirement that an event published on one instance is
  delivered to subscribers on every other instance in the cluster.
- `node-transport`: The connection lookup requirement already specifies an
  interface that "does not assume a single-instance deployment"; this adds the
  requirement that a dispatch actually routes to a node connected to a
  different instance, and that connection state is observable cluster-wide.
- `orchestrator`: Dispatching a run, task, or plan must succeed regardless of
  which instance holds the target node's transport connection, rather than
  reporting the node as not connected.
- `service-runtime`: Configuration loading must accept and validate the run
  mode; the health endpoint must report only the dependencies the active mode
  actually uses; and the operator-facing statelessness documentation must be
  updated, since two of its three "does not survive" entries are resolved by
  this change.

## Impact

- **Code**: `cmd/console/main.go` (the wiring split is the bulk of the work),
  `internal/runtime/config.go` (mode and cluster configuration),
  `internal/messaging` (listener, routes, leaf nodes),
  `internal/nodetransport` (cluster routes and their own authentication),
  `internal/runtime/health.go` (per-mode dependencies).
- **Operations**: `operations.md`'s statelessness table, new
  cluster/peer configuration, and a documented multi-instance topology.
  `docker-compose.yml` gains a multi-instance fixture to exercise it.
- **Security**: closes the cross-instance revocation gap. Peer listeners on
  both NATS servers are new attack surface and need their own credentials,
  distinct from the node-facing mTLS material, so that a node certificate can
  never be used to join a cluster as a peer.
- **Dependencies**: none new — NATS clustering and leaf nodes are already
  present in the vendored `nats-server`.
- **Compatibility**: no external contract changes. The ENC HTTP API, the node
  transport wire protocol, and `node-agent-client` are all untouched.
