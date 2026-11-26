## 1. Mode configuration

- [x] 1.1 Add a `Mode` type to `internal/runtime` with the five supported
  values and a parser; verify unit tests cover each valid value, the empty
  value defaulting to `all`, and an unrecognized value returning an error
  naming the valid modes
- [x] 1.2 Read `CONSOLE_RUN_MODE` in `runtime.LoadConfig` and fail loading on
  an unrecognized value; verify a config test asserts the error names both
  the bad value and the valid set, and that absent means `all`
- [x] 1.3 Log the active mode at startup; verify by starting the binary in
  each mode and observing the mode in the structured startup log

## 2. Composition root extraction

- [x] 2.1 Add `internal/app` and move `cmd/console/main.go`'s wiring into it
  unchanged, with `main` reduced to config load plus a single call; verify
  the existing test suite passes and the binary still starts in `all` mode
  with no behavior difference
- [x] 2.2 Add an equivalence test that captures `all` mode's registered route
  set, listeners, and background workers; verify it passes against the
  pre-split behavior and is the baseline every later task must keep green
- [x] 2.3 Separate the wiring into a shared substrate (config, logger,
  Postgres pool, internal bus, openvoxdb client, RBAC verifier, audit
  emitter) and the per-subsystem construction that depends on it; verify the
  2.2 equivalence test still passes

## 3. Mode table and per-mode surfaces

- [x] 3.1 Define `Surface` (HTTP route groups, listeners, background workers)
  and the `map[Mode]Surface` table; verify a table-driven test asserts each
  mode's surface matches the spec's "Mode-determined service surface"
  requirement
- [x] 3.2 Construct and register only what the active mode's surface names;
  verify `all` still passes the 2.2 equivalence test and each other mode
  registers only its own routes
- [x] 3.3 Return not-found for routes the active mode does not serve; verify a
  test requests the console UI from an `enc`-mode instance and asserts 404
  rather than a 5xx
- [x] 3.4 Make required configuration mode-scoped, including not reading the
  RBAC signing key in modes that do not issue tokens; verify tests assert
  `enc` mode starts with no signing key configured and `orchestrator` mode
  refuses to start with no transport listener address
- [x] 3.5 Verify by inspection and test that no mode reads a credential it
  does not use, covering the spec's "An unused credential is not loaded"
  scenario

## 4. Multi-instance test harness

- [x] 4.1 Add an `internal/testapp` harness that starts one `app.Run`
  instance in-process against generated credentials and a caller-supplied
  Postgres DSN, returning its base URL and a stop function; verify a test
  starts a single `all`-mode instance and gets a healthy `/health` response
- [x] 4.2 Generate per-instance credentials in the harness (RBAC signing key,
  openvoxdb client certificate) using `internal/testca` rather than repo
  fixtures; verify an instance starts with no file from `certs/` on disk
- [x] 4.3 Support starting N instances against one shared Postgres schema in a
  single test process, each with its own mode, HTTP port, and cluster
  configuration; verify a test starts two `all`-mode instances concurrently
  and both serve `/health`
- [x] 4.4 Add helpers to address a chosen instance's HTTP surface and to stop
  one instance independently; verify a test stops one of two instances and
  the survivor keeps serving requests

## 5. Internal bus clustering

- [x] 5.1 Add cluster peer configuration (`CONSOLE_CLUSTER_*`: peer listen
  address, peer list, attach style, credentials) to `runtime.Config`; verify
  config tests cover a peerless default, a routed peer set, and a leaf
  attachment
- [x] 5.2 Replace `messaging.Start`'s unconditional `DontListen: true` with
  peer-aware options: no listener when no peers are configured, a route
  listener when peers are, and leaf attachment when configured as an edge;
  verify a test asserts a peerless instance opens no messaging listener
- [x] 5.3 Require peer credentials whenever a peer listener is configured and
  refuse startup otherwise; verify a test asserts startup fails rather than
  binding an unauthenticated listener
- [x] 5.4 Add a two-instance test that publishes on one and asserts delivery
  to a subscriber on the other; verify it fails against the current
  unclustered implementation and passes after 5.2
- [x] 5.5 Add a test asserting a peer presenting no or wrong credentials is
  refused and receives no events

## 6. Subscriber classification

- [x] 6.1 Audit every `bus.Subscribe` call site and classify each as fan-out
  (instance-local state) or queue (shared write); verify the classification
  is recorded in `internal/messaging`'s package documentation with the rule
  stated
- [x] 6.2 Add queue subscription support to `messaging.Bus`; verify a unit
  test asserts one message reaches exactly one of several subscribers in a
  queue group
- [x] 6.3 Convert `internal/activity.Recorder` to a queue subscription; verify
  a two-instance test publishes one activity event and asserts exactly one
  row is persisted
- [x] 6.4 Confirm `rbac.Revoker`'s revocation listener stays a fan-out
  subscription; verify a two-instance test asserts both instances' in-memory
  caches reject the revoked token

## 7. Cross-instance revocation

- [x] 7.1 Add a two-instance test that revokes a token on one instance and
  asserts the other rejects it on the next request without a restart; verify
  it covers the `rbac` capability's existing revocation requirement, which
  the current implementation cannot meet
- [x] 7.2 Correct `operations.md`'s statelessness table row for the RBAC
  revocation cache, which currently asserts cross-instance propagation that
  does not happen; verify the row matches observed behavior after 5.2

## 8. Singleton background work

- [x] 8.1 Generalize `internal/vulnerability.Leases` into `internal/leases`
  with a provider-agnostic key; verify the existing vulnerability lease tests
  pass against the moved implementation
- [x] 8.2 Hold a named lease around each `worker` mode scheduler; verify a
  two-instance test asserts a unit of scheduled work runs on exactly one
  instance at a time
- [x] 8.3 Verify by test that when the lease-holding instance stops, another
  instance acquires the lease after expiry with no operator action
- [x] 8.4 Take a deploy lease in the code manager so concurrent webhooks
  across `web` instances serialize; verify a test asserts the second
  concurrent deploy reports a deploy already running rather than writing the
  same code directory

## 9. Node transport clustering

- [x] 9.1 Add node transport cluster configuration (peer listen address, peer
  list, credentials), separate from the node-facing mTLS material; verify
  config tests assert the two credential sets are independent
- [x] 9.2 Configure `ClusterOpts` with route authentication on
  `nodetransport.Server`; verify a test asserts a peer with valid credentials
  routes and a client presenting a node certificate to the peer listener is
  refused
- [x] 9.3 Add a two-instance test dispatching from instance A to a node
  connected to instance B, asserting the node receives the request and A
  receives the response
- [x] 9.4 Verify by test that per-node subject isolation holds across
  instances: a node connected to A cannot subscribe to or publish on a
  subject scoped to a node connected to B
- [x] 9.5 Verify by test that a dispatch to a node connected to no instance
  reports not-connected promptly rather than after the full timeout, matching
  the single-instance `ErrNoResponders` behavior
- [x] 9.6 Verify by test that `Registry` reflects connects and disconnects
  cluster-wide, and that `OnNodeConnect` firing on multiple instances results
  in exactly one initial run per certname via the existing Postgres claim

## 10. Orchestrator across instances

- [x] 10.1 Verify by test that triggering a run through one instance against a
  node connected to another dispatches successfully rather than recording the
  job failed for want of a connection
- [x] 10.2 Verify by test that job status and per-target results read
  identically from any instance
- [x] 10.3 Record a job as failed with a "could not be tracked to completion"
  reason when its dispatching instance stops before terminal state; verify a
  test asserts the job does not remain recorded as running indefinitely

## 11. Health and readiness

- [x] 11.1 Make `runtime.HealthHandler` take the active mode's checkers rather
  than a fixed list, and include the active mode in the response; verify a
  test asserts an `enc`-mode instance reports no node transport dependency
  and names its mode
- [x] 11.2 Serve health and metrics in every mode; verify a test requests both
  endpoints in each of the five modes and asserts a response
- [x] 11.3 Verify by test that a mode is reported unhealthy when a dependency
  it does use is unreachable

## 12. Documentation and fixtures

- [x] 12.1 Document each mode, its surface, and its required configuration in
  `operations.md`; verify every mode in the table has an entry
- [x] 12.2 Document the supported multi-instance topology - which modes route,
  which attach as leaves, which must reach which - satisfying the
  `service-runtime` statelessness requirement's new scenario
- [x] 12.3 Update `operations.md`'s statelessness table rows for node
  transport connections and orchestrator dispatch tracking, which this change
  resolves; verify both rows match observed clustered behavior
- [x] 12.4 Update `architecture-summary.md` section 8 and the deferred-items
  list, which currently defer multi-instance federation
- [x] 12.5 Add a multi-instance fixture to `docker-compose.yml` covering at
  least two `all` instances clustered together; verify it starts and the
  cross-instance revocation and dispatch tests pass against it
- [x] 12.6 Document the migration path and rollback from design.md in
  `operations.md`; verify an operator can follow it from single-instance to
  split roles

## 13. Integration verification

- [x] 13.1 Verify the 2.2 equivalence test still passes, confirming `all` mode
  is unchanged end to end
- [x] 13.2 Verify a full split topology (`web`, `enc`, `orchestrator`,
  `worker`) starts, serves its surfaces, and passes the cross-instance
  revocation, dispatch, activity-deduplication, and lease tests
- [x] 13.3 Verify two concurrently starting instances serialize migrations via
  `golang-migrate`'s advisory lock rather than racing, and that the loser
  proceeds normally
- [x] 13.4 Run `openspec validate add-console-run-modes --strict` and the full
  test suite; verify both pass
