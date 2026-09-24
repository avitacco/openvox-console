# Operations

Operator-facing runbooks for running this console in production: what
survives a failover, how to fail over Postgres, and how to rotate the
JWT signing key. See architecture-summary.md for the design principles
behind these (in particular "8. High availability and failover") and
phased-build-plan.md's Phase 8 for why this document exists.

## Statelessness boundary: what survives routing a request to a different instance

The console is designed to be stateless wherever possible - every piece
of durable state lives in Postgres, and cross-instance coordination (where
needed) goes through NATS - specifically so a load balancer can route a
request to any healthy instance without a scripted promotion/demotion
step. That holds for almost everything. Three pieces of in-memory state
are the exceptions worth knowing about:

| State | Where | Survives routing to a different instance? |
| --- | --- | --- |
| RBAC token revocation cache | `internal/rbac.Revoker` | **Yes, when clustered.** In-memory only for zero-latency checks, but Postgres is the durable source of truth and a revocation is published over NATS (`rbac.revoked` subject) to every other running instance's in-memory cache immediately, with no restart. A request rejected for a revoked token on instance A is rejected identically on instance B. This requires the instances to be peered (`CONSOLE_CLUSTER_*`): the internal bus opens no listener at all when unclustered, so a second instance that is not peered would keep accepting a token revoked on the first until that token expired. Instances sharing a database but not a cluster are not a supported configuration for this reason. |
| Node transport connections | `internal/nodetransport.Registry` | **Yes, when the transport is clustered.** A node's connection is still a single TCP connection terminated at one instance, but with the transports peered (`CONSOLE_NODE_TRANSPORT_CLUSTER_*`) a dispatch published by any instance is routed to whichever one holds that connection, and the connection registry reflects connects and disconnects cluster-wide. Unclustered, this remains instance-local: only the instance a node connected to can reach it. |
| Orchestrator in-flight dispatch tracking | `internal/orchestrator.Dispatcher.pending` | **Instance-local, but no longer load-bearing.** A dispatch's response still returns to the instance that published it, so that instance tracks it to completion. What changed is that *any* instance can publish the dispatch, so triggering a run no longer depends on reaching a particular one. If the dispatching instance stops before a job reaches a terminal state, the stale-job reaper (`worker`/`all` mode, leased) records that job as failed rather than leaving it "running" forever. |

Everything else - job/deploy/report history, users/roles/permissions,
node classification, activity log, audit log emission - reads and writes
Postgres (and, where multiple instances need to agree on something live,
NATS) directly, with no other in-memory state a request depends on. A
request for any of that can be safely routed to any healthy instance.

**Practical implication for a load-balanced deployment:** with the
instances clustered (see "Run modes" below), all of this is safe to
load-balance freely, on-demand orchestration included - a dispatch
published by any instance reaches a node whose connection is terminated
at another. Without clustering, only one instance is supported: an
unclustered second instance neither learns about token revocations nor
can reach the first's connected nodes.

## Run modes

One image, one binary. `CONSOLE_RUN_MODE` selects which parts of the
console an instance runs, so components with very different load
profiles can be scaled and placed independently. Unset means `all`,
which is exactly the behavior the console had before run modes existed -
an existing single-instance deployment needs no configuration change.

| Mode | Serves | Listens | Runs |
| --- | --- | --- | --- |
| `all` (default) | everything | HTTP, node transport | every background worker |
| `web` | console UI, REST API, code-deploy webhooks, ENC | HTTP | none |
| `enc` | ENC endpoint only, plus health/metrics | HTTP | none |
| `orchestrator` | health/metrics only | HTTP, node transport | dispatcher, initial-run trigger |
| `worker` | health/metrics only | HTTP | activity recorder, vulnerability scheduler, job reaper |

Health and metrics are served in every mode, so any instance can be
health-checked without the checker knowing its mode. `GET /health`
reports the active mode.

Two choices in that table are deliberate and worth stating:

- **`web` serves ENC as well.** `enc` mode exists so ENC *can* be scaled
  and placed on its own - next to a compiler, typically - not so that it
  is unavailable everywhere else. A `web`/`orchestrator`/`worker` split
  that silently stopped answering classification would be a far worse
  failure than `web` carrying some ENC load.
- **`orchestrator` serves no REST API.** It holds node connections and
  runs the dispatcher; the job-triggering endpoints live in `web`, which
  reaches those connections over the cluster.

### Required configuration per mode

Each mode requires only what it uses:

- Every mode: `CONSOLE_POSTGRES_DSN`, the `CONSOLE_OPENVOXDB_*` settings.
- `all` and `web`: `CONSOLE_RBAC_SIGNING_KEY_FILE`. These are the only
  modes that issue tokens.
- `enc`, `orchestrator`, `worker`: `CONSOLE_RBAC_VERIFICATION_KEYS_DIR`
  instead. They verify tokens but never mint them, so the private
  signing key is never read - and never needs to be present on, say, an
  ENC instance sitting next to a compiler outside the console's own
  network.
- `orchestrator`: `CONSOLE_NODE_TRANSPORT_ADDR`. Starting this mode
  without a listener would leave every dispatch failing "not connected"
  on an instance that otherwise looked healthy, so it is refused.

## Stack status page

`/status.html` shows every running console instance - its run mode,
address, health, uptime, version and background workers - grouped by
mode with a count per mode, alongside the external services the console
depends on (Postgres, the embedded NATS bus, openvoxdb, and the
openvoxserver CA). `GET /api/v1/status` returns the same thing as JSON.

Both are gated on the **`status:read`** permission.

**After upgrading an existing deployment, nobody has it yet.** A freshly
bootstrapped administrator gets it automatically, but an existing role
does not: permissions are never added to existing roles silently,
because that would widen access to deployment topology without anyone
deciding to. Grant it on the Roles page to whichever roles should see
the page; until then the page and its navigation link are simply not
shown.

### What the page can and cannot tell you

The picture is gathered live: the instance serving the request asks
every other instance over the internal bus and collects replies for one
second. Nothing is stored, so what you see is what answered just now.

That has one consequence worth understanding before you rely on it. **An
instance that is running but cannot reach the bus cannot be discovered**
- there is nothing to ask it through. The page does not pretend
otherwise: it works out how many instances *should* have answered by
combining the cluster view each replying instance reports, and if fewer
replied than expected it says so, in a banner above the instance list:

> This picture may be incomplete. 1 of 4 instances did not reply within 1s.

Treat that banner as significant. A missing instance is usually a wedged
one, which is exactly when the count matters.

Combining views rather than trusting the serving instance's own is what
makes this work for `enc` instances. They attach as leaves, and a leaf is
visible only to the peer it attached to - so a status request served by a
different peer would otherwise have no idea it exists, and would report a
complete picture with that instance missing.

Dependencies are reported **as the serving instance sees them**, because
"openvoxdb is unreachable from `web-2`" is a more useful statement than
"openvoxdb is unreachable". When instances disagree about a dependency,
that dependency is flagged `differs by instance` - usually the most
informative thing on the page, since it means partial connectivity
rather than an outage.

A dependency that was never configured reads `Not configured`, distinct
from `Unreachable`: the first is a deployment choice, the second is a
fault.

The page does not auto-refresh. One page load asks every instance, so an
auto-refreshing page would turn a browser tab left open into steady
fleet-wide traffic. Reload it when you want a fresh answer.

## Multi-instance topology

Instances coordinate over two separate NATS clusters, mirroring the two
separate embedded NATS servers (see architecture-summary.md sections 4
and 7): the internal event bus, and the node transport.

### Internal event bus

Carries token revocations, activity events, and code-deployment
notifications.

- `CONSOLE_CLUSTER_ADDR` - this instance's peer listener, e.g. `:6222`.
- `CONSOLE_CLUSTER_PEERS` - comma-separated `host:port` peers.
- `CONSOLE_CLUSTER_SECRET` - required whenever either of the above is
  set. Startup is refused without it: an unauthenticated peer listener
  would put anything that can reach the port onto the internal bus.
- `CONSOLE_CLUSTER_MODE` - `route` (default) or `leaf`.
- `CONSOLE_CLUSTER_LEAF_ADDR` - opens a listener for leaf instances,
  e.g. `:6223`. Only needed on the instances your `enc` instances are
  pointed at; unset, no leaf listener is opened. A leaf's
  `CONSOLE_CLUSTER_PEERS` names this address, not the route listener.

Core modes (`all`, `web`, `orchestrator`, `worker`) mesh as routed
peers. `enc` instances attach as **leaf** nodes: the connection is
outbound-only, so the console core needs no network path back to them.
That is what makes the compiler-colocated deployment practical, where
ENC instances live wherever the compilers live.

```
  web-1 ──┐
  web-2 ──┼── routed peers (:6222)
  orch-1 ─┤
  worker ─┘
      ▲
      │ leaf connections, outbound only
      │ (to CONSOLE_CLUSTER_LEAF_ADDR, e.g. :6223)
      │
  enc-1, enc-2  (alongside the compilers)
```

A leaf attaches to the address a peer publishes as
`CONSOLE_CLUSTER_LEAF_ADDR`. That address is configured explicitly
rather than derived from the route port: deriving it would mean binding
a port the operator never chose, and anything already holding it would
leave the console hanging at startup with no useful error.

### Node transport

Carries orchestration dispatches to managed nodes. Clustering it is what
removes the sticky-routing constraint: a dispatch published by any
instance reaches a node connected to any other.

- `CONSOLE_NODE_TRANSPORT_CLUSTER_ADDR`
- `CONSOLE_NODE_TRANSPORT_CLUSTER_PEERS`
- `CONSOLE_NODE_TRANSPORT_CLUSTER_SECRET`

This secret is deliberately **not** the node-facing mTLS material.
Routes authenticate with their own credential, so a node certificate can
never be used to join the cluster as a peer and observe or inject every
node's traffic. Startup is refused if the transport is clustered without
it.

### Which modes may be run as multiple instances

Every mode may. Nothing a request depends on is instance-local once the
instances are clustered:

- Durable state is in Postgres.
- Revocations fan out to every instance, so each one's in-memory cache
  converges.
- Work that must happen once - the vulnerability sync, a code deploy of
  a given environment, the stale-job reaper - is serialized by a
  Postgres lease (`singleton_leases`), so running several `worker` or
  `all` instances does not duplicate it.
- Subscribers that write use a NATS queue group, so an activity event
  published once is persisted once however many instances subscribe.

### Migration path

The change is inert until configured, so adoption is incremental:

1. Upgrade every instance to the new image while still single-instance.
   Nothing changes: `all` with no cluster configuration is exactly the
   previous behavior.
2. Set `CONSOLE_CLUSTER_*` on the existing instance and start a second
   `all` instance peered with it. This exercises clustering, lease
   coordination and queue subscriptions before any role split.
3. Split roles as load demands: move background work to `worker`, node
   connections to `orchestrator`, then add `enc` instances alongside the
   compilers.

Rollback at any step is setting `CONSOLE_RUN_MODE=all` and removing the
cluster configuration. No schema change gates it; the `singleton_leases`
table is additive and unused by an unclustered instance.

## Postgres failover runbook

The console's only durable state is Postgres, so a primary failure is
recovered by promoting a streaming-replication standby and repointing
the console at it. This isn't new retry machinery in the application -
it's a documented operational procedure, exercised against a real
primary+standby pair.

A local fixture for testing this procedure lives in `docker-compose.yml`,
started by default (`make up`) alongside everything else - `make
postgres-replication-down` / `make postgres-replication-up` stop/restart
just this fixture, backed by `postgres-replication-init/`. It brings up a
real `postgres-primary`
(port 5433) and `postgres-standby` (port 5434), the latter populated via
`pg_basebackup -R` against the primary (the standby's data directory is
otherwise empty on first start, so this only runs once - see
`postgres-replication-init/standby-entrypoint.sh`). This fixture is for
exercising failover, not everyday dev work - `console-postgres` (the
default `make up` target) is unaffected and unrelated.

**To fail over (promote the standby and cut over):**

1. Confirm the standby is actually replicating before you need it:
   ```
   docker exec <standby-container> psql -U console -d console -c "SELECT pg_is_in_recovery();"
   ```
   should report `t`. Writes against the standby should be rejected
   (`cannot execute INSERT in a read-only transaction`) until promoted -
   that rejection is expected and confirms it's still a replica, not a
   sign of misconfiguration.

2. Promote the standby:
   ```
   docker exec <standby-container> psql -U console -d console -c "SELECT pg_promote();"
   ```
   `pg_is_in_recovery()` flips to `f` once promotion completes (a few
   seconds). The promoted instance is now an independent, writable
   Postgres - it does not automatically resume replication from the old
   primary if that primary comes back, so treat the old primary as
   retired once this step is taken (rebuild it as a new standby of the
   promoted instance if you want redundancy restored, following the same
   `pg_basebackup -R` procedure the fixture's standby used).

3. Repoint the console at the promoted instance: update
   `CONSOLE_POSTGRES_DSN` to the promoted instance's host/port and
   restart the console process(es). The console does not hot-swap its
   connection pool's target mid-process (see `internal/persistence` -
   it opens one pool for the DSN it was started with), so this step
   requires a restart, not just a config reload.

4. Confirm recovery: `/health` reports Postgres reachable again, and a
   real write-path request (e.g. logging in, which writes a session/
   token record) succeeds against the promoted instance.

This procedure was verified live against the `postgres-replication`
fixture: a row written on the primary appeared on the standby via
streaming replication, the standby rejected a write attempt while still
a replica, `pg_promote()` flipped it to writable, and a write against
the newly-promoted instance succeeded immediately - all without
restarting either Postgres container.

## JWT signing key rotation

The console signs its own JWTs (`internal/rbac.Issuer`) and verifies them
itself (`internal/rbac.Verifier`) with an ES256 key pair - see
architecture-summary.md. Every issued token's JWT header carries a `kid`
(key ID) identifying which key signed it; `Verifier` holds a small set of
keys (`kid -> public key`) rather than just one, so a token signed by a
since-retired key keeps verifying through its natural expiry while a new
key takes over signing - no forced logout, no downtime.

**Configuration:**

- `CONSOLE_RBAC_SIGNING_KEY_FILE` - the active signer's private key (as
  today).
- `CONSOLE_RBAC_SIGNING_KEY_ID` - the `kid` tagged onto tokens this
  instance issues. Optional; defaults to a fixed value (`"default"`) if
  unset, so an existing single-key deployment's already-issued tokens
  (which predate this feature and carry no `kid` at all) keep verifying
  unchanged - a missing `kid` falls back to whatever `CONSOLE_RBAC_
  SIGNING_KEY_ID` is currently configured as.
- `CONSOLE_RBAC_VERIFICATION_KEYS_DIR` - optional directory of additional
  `<kid>.pem` files (same private-key PEM format `CONSOLE_RBAC_
  SIGNING_KEY_FILE` uses; only the public half is used for verification)
  - these are retired/rotating-out keys kept around only so their
    already-issued tokens keep verifying, not active signers. The active
    signer's own key is always implicitly in the verification set under
    its own `kid`, whether or not it's also present in this directory.

**To rotate:**

1. Generate a new key without touching the active one:
   ```
   make rbac-rotate-key KID=<new-kid>
   ```
   (`KID` defaults to today's date if omitted.) This writes
   `certs/rbac-verification-keys/<new-kid>.pem` and leaves
   `certs/rbac-signing-key.pem` untouched.

2. Preserve the *current* active key under its own `kid` in the
   verification directory too (e.g. `cp certs/rbac-signing-key.pem
   certs/rbac-verification-keys/<current-kid>.pem`), so it stays
   verifiable once it's no longer the signer. If the currently active
   deployment has never set `CONSOLE_RBAC_SIGNING_KEY_ID`, its `kid` is
   the default `"default"`.

3. Promote the new key: point `CONSOLE_RBAC_SIGNING_KEY_FILE` at the new
   key file, set `CONSOLE_RBAC_SIGNING_KEY_ID` to `<new-kid>`, and set
   `CONSOLE_RBAC_VERIFICATION_KEYS_DIR` to `certs/rbac-verification-keys`
   (if not already set). Restart the console. From this point, newly
   issued tokens carry `<new-kid>`; tokens issued under the old key keep
   verifying because step 2 kept it in the verification set.

4. Once the old key's longest-lived outstanding token would have expired
   (refresh tokens live `RefreshTokenTTL` = 24h - see
   `internal/rbac/tokens.go` - so 24h after step 3 is a safe floor),
   delete its file from the verification directory and restart. Tokens
   signed by the retired key are rejected from this point on.

This procedure was verified live end to end: logged in under the
original (`"default"`) key, promoted a newly-generated key as the active
signer while keeping the old one in the verification set, confirmed the
pre-rotation token still verified (`GET /api/v1/me` → 200) and a freshly
issued token's header carried the new `kid`, then removed the old key
from the verification set and confirmed the pre-rotation token was then
rejected (`401 invalid or expired token`) - all against a real running
console instance, not a unit test.

## Certificate status reporting (CA-client credential)

The Nodes page's "Cert status" column (`signed`/`requested`/`revoked`/
`unknown`) is populated by `internal/certstatus`, which polls openvoxserver's
CA API directly:

```
GET /puppet-ca/v1/certificate_statuses/<anything>
```

(the trailing path segment is ignored by Puppet Server; the endpoint always
returns the full bulk list). This is disabled by default - if
`CONSOLE_CA_CLIENT_URL` is unset, every node's cert status reports
`"unknown"` and the rest of the console (connectivity, dispatch, everything
else) is unaffected. Turning it on requires minting a dedicated client
certificate first.

**The privilege this credential grants is broader than "read cert status".**
Puppet Server's CA API gates this endpoint (and every other CA endpoint -
sign, revoke, clean) behind a single authorization extension,
`pp_cli_auth`, on the client certificate. There is no scoped, read-only
variant of this credential in Puppet Server: any certificate minted with
`--ca-client` can sign, revoke, or clean *any* node's certificate through
the same API, not just read statuses. Treat this credential as full CA
admin, and scope who/what can read its key file accordingly - it should
live only where the console process itself can read it, never checked into
version control or shared beyond that.

**Generating the credential** requires running `puppetserver ca generate`
directly against the CA's on-disk state, which the running `puppetserver`
process also writes to - so it must not race an active server. Stop
openvoxserver first, run the generate command against the same image and
volumes without starting the full server process, then restart:

```bash
docker compose stop openvoxserver

docker compose run --rm --no-deps --entrypoint "" openvoxserver \
  puppetserver ca generate --ca-client --certname console-ca-client

docker compose start openvoxserver
# health-poll (e.g. curl the console's own health endpoint, or
# `docker compose ps` until openvoxserver reports healthy) before
# resuming normal traffic
```

This writes the new cert/key under the CA's `ca-client` output location
inside the `openvoxserver` container/volume (check the `puppetserver ca
generate --help` output for the exact path, which is
version-dependent) - copy the cert, key, and the CA bundle it was signed
against out to wherever the console process can read them.

**Config wiring** - all four are required together to enable the feature;
any one left unset disables it (reports `"unknown"` for every node) rather
than erroring:

| Variable | Purpose |
|---|---|
| `CONSOLE_CA_CLIENT_URL` | Base URL of openvoxserver's CA API (e.g. `https://openvoxserver:8140`) |
| `CONSOLE_CA_CLIENT_CERT_FILE` | Path to the CA-client certificate minted above |
| `CONSOLE_CA_CLIENT_KEY_FILE` | Path to that certificate's private key |
| `CONSOLE_CA_CLIENT_CA_FILE` | Path to the CA bundle to verify openvoxserver's TLS certificate against |

**Retiring the credential** - because this grants full CA admin, revoke it
as soon as it's no longer needed (a decommissioned console instance, a
rotation, or just a scratch credential used for testing):

```bash
docker compose run --rm --no-deps --entrypoint "" openvoxserver \
  puppetserver ca clean --certname console-ca-client
```

then unset the four `CONSOLE_CA_CLIENT_*` variables (or remove the cert/key
files) on any console instance still configured to use it - a revoked
certificate will simply fail TLS handshakes against the CA API going
forward, degrading gracefully back to `"unknown"` cert statuses rather than
taking down connectivity reporting.

### Acting on a certificate: sign/revoke/clean and the `nodes:certs:manage` permission

Once the CA-client credential above is configured, the console can also
*act* on a node's certificate, not just read its status - `internal/
certstatus.Client`'s `Sign`, `Revoke`, and `Clean` methods, exposed as:

```
POST   /api/v1/nodes/{certname}/cert/sign
POST   /api/v1/nodes/{certname}/cert/revoke
DELETE /api/v1/nodes/{certname}/cert
```

and as Sign/Revoke/Clean buttons on the Nodes page itself. Each of these
calls exercises the same CA-client credential's `PUT`/`DELETE
/puppet-ca/v1/certificate_status/:certname` API - **granting a user or
role the ability to call these endpoints is equivalent to giving them
shell-level CA admin access on openvoxserver.** There is no way to grant
"sign but not revoke", or "act on this node but not that one" - the
underlying credential doesn't distinguish, so the console's own
`nodes:certs:manage` permission is the only control point. Grant it as
narrowly as you would grant shell access to the CA itself: to specific
trusted operators, not broadly to every user who merely needs to view
node status (that's the separate, much lower-privilege `nodes:read`
permission, which the Nodes page's Cert status column already requires
and which does **not** imply `nodes:certs:manage`).

A rejected action (wrong certificate state for the requested transition,
an unknown certname, or the CA-client credential not configured at all)
returns a clear error and is never audit-logged as a success - only a
completed sign/revoke/clean emits an audit entry
(`node.certificate.signed`/`.revoked`/`.cleaned`, under the `nodes`
audit category). Revoking or cleaning a certificate has the same
real-world effect as running `puppetserver ca revoke`/`clean` directly:
revoking immediately invalidates that node's ability to authenticate on
its next connection attempt (an already-open connection isn't forcibly
torn down, but nothing further will succeed once it reconnects), and
cleaning removes the CA's record entirely, so the certname must submit a
brand new certificate request - matching the same signed/revoked/clean
distinctions Puppet's own CA has always had, just reachable from the
console UI now instead of only a shell on the openvoxserver host.

This procedure was verified live end to end, twice: first with a scratch
credential against a scratch console instance (revoked after verification),
then for real - a `console-ca-client` credential minted against the real
`openvoxserver` container (with a brief, health-polled stop/restart), its
cert/key copied to `certs/ca-client-{cert,key}.pem`, and wired into this
dev environment's actual `.env`/running console via the four
`CONSOLE_CA_CLIENT_*` variables above. The real Nodes page now reports real
`signed` cert statuses for every real node. The sign/revoke/clean actions
were separately verified live: a scratch node's certificate request was
signed through a real click on the Nodes page's Sign button (state
`requested` → `signed`, confirmed via the API), and revoke/clean were
each verified against real signed certificates - once as a `nodes:certs:
manage`-holding admin (all three actions available), and once as a
scratch user holding only `nodes:read` (zero action buttons rendered,
confirming the permission gate).

## Infrastructure certs on the Nodes page

Because the Nodes page lists every certname the OpenVox CA knows about
(not just openvoxdb-managed nodes - see the certificate-status section
above for why), it also picks up certs that were never going to become
managed nodes: this console's own service-to-service TLS identities,
and the servers it connects to. In this dev environment that's `console`
(the console's own openvoxdb client cert), `console-ca-client` (the
CA-client credential above), `openvoxdb` and `openvoxserver` (their own
server certs), and `node-transport` (the node transport's server cert).

The console identifies these automatically, with **no configuration** -
`internal/infracert` derives the set at startup from cert files/URLs
already configured (`CONSOLE_OPENVOXDB_CERT_FILE`,
`CONSOLE_CA_CLIENT_CERT_FILE`, `CONSOLE_NODE_TRANSPORT_CERT_FILE`, and
the peer certificate presented by whatever's at `CONSOLE_OPENVOXDB_URL`/
`CONSOLE_CA_CLIENT_URL`). The Nodes page hides these by default behind
a "Show infrastructure certs" checkbox, and when shown, badges them and
gives Revoke/Clean a reason-specific confirmation naming the real
consequence, instead of the generic node-focused wording.

**Stated limitation, not a bug**: this can only flag a certname the
console has an actual channel to observe. `openvoxserver`'s own
certname specifically requires `CONSOLE_CA_CLIENT_*` to be configured -
that's the only outbound connection this console makes to it. And the
node transport's certname is caught only because its cert file is
configured as `CONSOLE_NODE_TRANSPORT_CERT_FILE` - there's no general
"this was formerly used for X" detection; if that cert file were ever
swapped for a differently-named one, the *old* name would correctly
stop being flagged (it would no longer be an active infrastructure
identity, just a genuinely stale leftover cert like any other) and the
new one would start being flagged automatically instead. This is
exactly what happened when the node transport cert was renamed to
`node-transport` (see the "Real-node install bugs" section below) - no
code change was needed for the Nodes page to catch up.

This was verified live end to end: with all five of this dev
environment's real infrastructure certs present, the real Nodes page
hid all five by default (only the three real managed nodes visible),
correctly flagged all five (including `openvoxserver`'s own certname)
with accurate, specific reasons once revealed, and showed the
reason-specific confirmation text on a real Revoke click against one of
them - while a real managed node's Revoke confirmation stayed
unchanged.

## Dashboard fleet-status stats: the "Corrected" limitation

The Dashboard's four fleet-status stats (`GET /api/v1/nodes/summary`,
rendered by `frontend/src/index.js`) are: **Failed**, **Corrected**,
**Intentional changes**, **Unchanged** - every node with a report is
classified into exactly one, based on `latest_report_status` plus (for
a `changed` report) whether openvoxdb's `latest_report_corrective_change`
field confirms the change was Puppet correcting unexpected drift, versus
an intentional catalog change.

**"Corrected" will read 0 in this dev environment, permanently, until
the vendored openvoxserver/openvoxdb images change** - not a bug in this
project's code. Puppet's agent genuinely detects corrective changes on
its own (confirmed live: a real drift-and-correct cycle produced
`corrective_change: true` in the agent's own `last_run_report.yaml` and
an explicit `(corrective)` note in its log), but that flag never
reaches openvoxdb: `openvoxserver` submits reports using PDB command
`store_report` version 8, which doesn't carry `corrective_change`
through, even though openvoxdb's own query schema already has a column
for it. Confirmed via direct PQL queries at the event, report, and node
level, with a wait to rule out ingestion lag - always `null`.

An upgrade to the newest published tags at the time
(`openvoxserver:8.8.1-latest`, `openvoxdb:8.9.1-latest`) was tried and
reverted - it did not resolve the command-version gap, and introduced a
different break first: that build's entrypoint regenerates `puppet.conf`
*after* this project's ENC-configuration init script
(`openvoxserver-init/10-configure-enc.sh`) runs rather than before, so
`node_terminus`/`external_nodes` silently stop being set and ENC
classification breaks server-side. If a future upgrade is attempted,
expect to need that ordering issue fixed too, and budget time to verify
the `store_report` command version actually changes (checkable via
`openvoxdb`'s access log - look for `command=store_report&version=`)
before assuming it's fixed.

**A real, unrelated bug surfaced during this investigation and is worth
knowing about separately**: `openvox-code/environments/production` (the
symlink code-manager deploys point at the active code) has been
observed created with a **host-absolute path**
(`/home/.../openvox-code/.deploys/...`) rather than a path relative to
itself. That resolves fine from the host, but is unresolvable from
*inside* the `openvoxserver` container (which only has `./openvox-code`
mounted at `/etc/puppetlabs/code`, not the host's full path) - every
node's catalog compile fails with "Could not find class ... " until the
symlink is manually fixed to a relative target
(`../.deploys/production/<timestamp>/basedir/production`). This
recurred on a fresh deploy during this session, so it's a deploy-time
bug in whatever creates the symlink (`internal/codemanager`), not a
one-off - worth fixing at the source if code-manager deploys keep
breaking catalog compilation.

## Package inventory: solved for Linux, static-only for Windows/macOS

**This is now solved for Linux** (`add-package-inventory-reporting`):
each install script's `install.sh` drops a Facter external fact
(`/opt/puppetlabs/facter/facts.d/package_inventory.sh`) that reports
real installed packages via `dpkg-query` (Debian family) or `rpm -qa`
(RedHat family) on every Facter run. Verified live, end to end, on
genuinely fresh containers for both families: after a real
`puppet agent -t` run, `package_inventory { certname = "..." }`
returned real rows in openvoxdb, and both the node detail page's
Packages section and the fleet-wide Packages page rendered that real
data - the first time either had shown anything but the empty state.

The original investigation (kept below for how the mechanism was
found) confirmed there's no agent-side *setting* for this - unlike
Puppet Enterprise's own equivalent, which is enabled via a
`puppet_enterprise::profile::agent` classification parameter that (per
its own real fact source) just drops a marker file, this project's
version needs no marker file at all: the install script running on a
node *is* the per-node opt-in, so the dropped fact script unconditionally
computes and emits the package list every time. See design.md in
`add-package-inventory-reporting` for the full mechanism (Facter
external facts specifically because Puppet's pluginsync purges a plain
custom fact dropped the same way) and per-platform enumeration choices.

**Windows and macOS remain static-verification-only** - `install.ps1`
(`Get-Package`) and `install-macos.sh` (`pkgutil --pkgs`/`--pkg-info`)
both got the same treatment as their openvoxagent-install counterparts:
real syntax/lint checks (`pwsh`'s own parser + PSScriptAnalyzer for
PowerShell, `shellcheck` for the macOS script) and line-by-line review
against real documentation - which caught and resolved one real
ambiguity (confirming `pkgutil --pkg-info`'s version field is
`version:`, not `pkg-version:` as an initial search suggested) - but
neither could be run against a real host, since none exists in this
dev environment. If a real Windows or macOS node is ever available,
verifying `package_inventory` rows actually appear for it (the same
proof already done for Linux) is the natural next check, not an
assumption to make from the static review alone.

**Linux package data carries full RPM versions and apt source packages**
(`fix-package-inventory-fact-fidelity`), because the original facts
dropped information any version-accurate consumer (vulnerability
matching in particular) needs:

- **RPM versions include the epoch when a package has one** -
  `openssl-libs` on Rocky 9 reports `1:3.5.5-2.el9_8`, while epoch-less
  `glibc` still reports `2.34-266.el9_8`, exactly as before (confirmed
  live: 13 of 165 packages on a stock Rocky 9 container carry an epoch).
  This is RPM's own `epoch:version-release` form. **Consequence for the
  fleet-wide Packages search:** an exact-version search for an
  epoch-bearing RPM package now has to include the epoch
  (`1:3.5.5-2.el9_8`); the epoch-less string it used to match was never
  the version the package manager itself reports. Searches by name alone
  are unaffected.
- **A companion fact, `console_package_inventory`, rides alongside
  `_puppet_inventory_1`** - an ordinary fact, since openvoxdb's
  package tuples have no room for extra fields. On every Linux node it
  carries `format: 2`; a node without it (format 1, implicit) is still
  running an older fact script, so a consumer can tell old data from
  corrected data instead of silently trusting either. On apt nodes it
  also carries `sources`, mapping each binary package whose source
  package differs in name or version to that source (for example
  `libssl3t64` → `openssl`) - a binary absent from the map is its own
  source. The `_puppet_inventory_1` tuples themselves are unchanged for
  apt, so searching still works by the binary name `dpkg -l` shows. The
  node detail page shows a "from <source> <version>" line under packages
  whose source differs.
- **Upgrading node-agent-client is enough to fix a node that already
  reports package inventory.** On start, the client compares an existing
  `facts.d/package_inventory.sh` with the script built into it and
  rewrites it if they differ - no toggle needed, no Puppet run triggered
  (the next scheduled run sends corrected data), and a node with
  reporting disabled stays disabled. Confirmed live with a real package
  upgrade from a pre-change build: the service restart rewrote the
  script, no new report arrived until the next `puppet agent -t`, and
  that run produced `format: 2` data. The comparison is for inequality,
  not newer-ness, so downgrading the client puts the older script back on
  its next start.
- **Only packages that are actually installed are reported.** `dpkg-query
  -W` also lists packages in dpkg's `config-files` state - `dpkg -l` shows
  these as `rc`: removed, but their configuration files retained - and the
  fact used to emit them as though they were installed. On a node that has
  been upgraded for a while this is mostly old kernels, which match every
  advisory fixed after their version, so it produced findings for software
  that was not on the machine at all. The fact now filters on install
  status, covering both the `_puppet_inventory_1` tuples and the
  `console_package_inventory` sources map. It filters on install status
  and deliberately not on selection state: a held package's selection is
  `hold` rather than `install` while the package is still installed, so
  filtering on selection would drop it and hide it from vulnerability
  scanning. On rpm nodes the equivalent cleanup is dropping `gpg-pubkey`
  entries - the repository signing keys `rpm -qa` lists alongside real
  packages - and nothing more, since erasing an rpm package removes its
  rpmdb entry outright and leaves no residual state to filter.
- **A failed package query now reports nothing rather than an empty
  inventory** - both scripts exit non-zero with no output if
  `dpkg-query`/`rpm` fails, since an empty `_puppet_inventory_1` would
  make openvoxdb drop every package row for that node.

**Original investigation, for context**: checked for an agent-side
setting analogous to how `corrective_change` detection is a genuine
agent-side behavior (`puppet config print all` against the real
`openvox-testing-agent` image, Puppet 8.28.1) - none exists; the only
match for `package`/`inventory` across every setting was
`cert_inventory`, an unrelated CA setting. This is what led to tracing
the real mechanism (a `_puppet_inventory_1` Facter fact, confirmed by
reading openvoxdb's own facts terminus source) instead of guessing at
a module or setting that didn't exist.

## Multi-platform agent install: what's live-verified and what isn't

`add-multi-platform-agent-install` widened `internal/agentdist`'s
install scripts from Debian/Ubuntu-only to five platforms: Linux
(Debian and RedHat families, amd64/arm64), Windows (amd64 only - see
below), and macOS (amd64/arm64). What actually got verified differs
sharply by platform, since this project's dev environment is
Linux-container-only:

- **Linux (both families): fully live-verified.** Beyond the existing
  `make openvox-test` regression check, the real `/packages/install.sh`
  was run end to end against genuinely fresh, never-provisioned
  containers for both families - a plain `ubuntu:24.04` (apt-get branch:
  real `openvox-agent` 8.29.0 installed from `apt.voxpupuli.org`) and a
  plain `rockylinux/rockylinux:9` (yum branch: real
  `openvox-agent-8.29.0-1.el9.x86_64` installed via the real
  `openvox8-release-el-9.noarch.rpm`). Both correctly stopped at "no
  signed Puppet certificate found" - the expected, correct behavior for
  a node that was never enrolled, not a bug. **node-agent-client itself
  is now delivered as a native `.deb`/`.rpm`, not a raw binary** - see
  "Native packaging for node-agent-client (Linux only)" below.

- **Windows: static verification only, no real Windows host exists
  anywhere in this project.** `install.ps1`'s syntax was verified for
  real, though: a self-contained `pwsh` 7.6.5 build (no system install
  needed - Arch Linux, this project's dev host, has no `apt`) parsed the
  rendered script with zero errors via
  `[System.Management.Automation.Language.Parser]::ParseFile()`, and
  PSScriptAnalyzer (installed via `Install-Module`) found only stylistic
  `PSAvoidUsingWriteHost` warnings (intentional - this is a
  status-printing script run interactively, not reusable pipeline code).
  Its `msiexec`/`New-Service` syntax was cross-checked against OpenVox's
  own real `puppet-openvox_bootstrap` module
  (`tasks/install_windows.ps1`) rather than guessed. **Windows is
  amd64-only**: OpenVox publishes no Windows ARM64 `openvoxagent`
  package at all (confirmed against `downloads.voxpupuli.org/windows/`'s
  real directory listing). **The openvoxagent MSI version is hardcoded**
  (`openvoxAgentWindowsVersion` in `install_script_win.go`, currently
  `8.29.0`) and needs periodic manual bumping - there is no "latest"
  alias to resolve dynamically, a gap OpenVox's own bootstrap module has
  too (its own code has an identical hardcoded value with a `# XXX:`
  comment acknowledging the same problem). **Also unverified**: whether
  a Machine-scope environment variable set right before `New-Service`
  is actually visible to that freshly-started service before a reboot -
  a real, well-known Windows quirk (services.exe's environment block is
  cached at boot) this project could not test. If the service starts but
  can't connect, a reboot is the documented workaround in the script
  itself.

- **macOS: static verification only, no real macOS host exists either.**
  OpenVox's own bootstrap module has zero macOS support to check against
  (an explicit TODO in its own README), so `install-macos.sh` was
  written from scratch and checked against real Apple documentation
  command-by-command (`hdiutil`, `installer`, `launchctl`) - this
  process caught one real bug before it shipped: `-quiet` is `hdiutil`'s
  *global* flag (must come before the verb: `hdiutil -quiet attach`),
  not a per-verb option. The actual `.dmg` internal layout was confirmed
  by downloading a real one and inspecting it with `7z l` (available on
  this Linux dev host) rather than assumed - it's a versioned folder
  containing a directly-installable flat `.pkg`, which `hdiutil attach`
  + `find *.pkg` + `installer -pkg ... -target /` handles correctly.
  **No hardcoded version at all** (unlike Windows): macOS builds are
  split by *both* macOS major version and architecture with
  inconsistent coverage per combination (confirmed live: macOS 15 has an
  `arm64` build directory but no `x86_64` one at all), so the script
  reads the real directory listing for the node's actual
  version+architecture combination at install time and picks the newest
  non-release-candidate build found there, rather than guessing a
  version that might not exist for that specific combination.
  `shellcheck` ran clean (0 findings) against the rendered output.

**Neither Windows nor macOS binaries are code-signed or notarized** -
both platforms' OS-level security features (Windows SmartScreen, macOS
Gatekeeper) will very likely warn on, or outright block, an unsigned
binary fetched over the network. Fixing this needs a paid code-signing
identity this project doesn't have; out of scope for this change,
documented here so it doesn't read as a broken installer to a future
reader who hits it.

## Real-node install bugs found via an actual VM (not a container)

A real end-to-end run against a genuine Ubuntu VM (not a Docker
container - every install-script test up to this point, including this
project's own live-verification tasks, had used containers on this same
Docker host) surfaced two real bugs neither container-based test could
have caught:

- **The "run puppet agent -t" error message omitted the full path.**
  `/opt/puppetlabs/bin` isn't on `PATH` by default right after a fresh
  agent install (a new shell session picks it up via the package's own
  profile.d script, but the current one doesn't) - the message now
  prints the full path (`${PUPPET_BIN}`/`$PuppetBin`) in all three
  install scripts, so there's nothing to guess.

- **The node never showed as connected, silently.** All three install
  scripts baked in `CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR=host.docker.internal:8142`
  - a Docker-only DNS name that only resolves inside containers on this
    compose network (via `extra_hosts: host.docker.internal:host-gateway`,
    present on every container this project's own tests happened to
    use). A real VM has no such entry and can't resolve it at all, so
    `node-agent-client` could never even attempt a connection -
    `systemctl enable --now` still reported success regardless, since
    that only confirms the process launched, not that it's working.
    Fixed by switching to `node-transport` (a name already present as a
    SAN on a freshly-regenerated transport cert - see below) as the
    canonical address: `docker-compose.yml`'s `openvox-testing-agent`
    now maps `node-transport` to `host-gateway` alongside the
    pre-existing `host.docker.internal` entry, and any node *outside*
    this compose network (a real VM, a container on a different
    network) needs `node-transport` mapped to this host's real LAN IP
    in its own `/etc/hosts` - the exact workaround this file already
    documented for `host.docker.internal` above, now pointed at a name
    that's actually resolvable outside Docker too.

  Verified for real, end to end, for the first time in this project:
  ran the full flow against a fresh, persistent (non-`--rm`) Ubuntu
  container with `node-transport`/`host.docker.internal` both mapped to
  `host-gateway` - installed `openvox-agent` from scratch, ran
  `puppet agent -t` (autosign is enabled in this dev environment, so
  no manual signing step was needed - matching what the report that
  found this bug actually experienced), re-ran `install.sh` to
  completion, started `node-agent-client` manually (no systemd in a
  plain container - the script's own documented fallback, exactly as
  designed), and confirmed via `GET /api/v1/node-connectivity` that it
  showed `"connected": true` - the first real proof this project has
  that `node-agent-client` actually connects through the real install
  flow, not just via a container that happened to share Docker's own
  `host.docker.internal` resolution.

- **The transport's TLS certificate carried a stale, misleading name**,
  and the fix above was about to make that worse by promoting it to the
  canonical *hostname* purely because it already happened to be a valid
  SAN. Fixed at the source instead: regenerated the cert with an
  accurate name (`puppetserver ca generate --certname node-transport
  --subject-alt-names host.docker.internal,localhost,puppet` - see
  `.env`), named for the Go package that owns it
  (`internal/nodetransport`) rather than for the transport technology
  underneath, so the name stays accurate even if that technology ever
  changes. `CONSOLE_NODE_TRANSPORT_CERT_FILE`/`_KEY_FILE` now point at
  `certs/node-transport-{cert,key}.pem`, and the "Infrastructure certs
  on the Nodes page" section above needs no special-case caveat - the
  console picks up whatever CN that file actually has, automatically,
  exactly as designed there.

- **A third bug, found the same way (a real `curl | bash` one-liner,
  not the two-step `curl -o file && bash file` pattern every prior
  live-verification task had used): re-running the install script while
  node-agent-client was already running as a systemd service failed
  with `curl: (23) client returned ERROR on write`.** Root cause:
  Linux refuses to open a currently-executing binary for writing
  (`ETXTBSY`); the script's `curl -o` onto that same running path hit
  it, and `set -e` killed the script before it reached the systemd
  registration step. Reproduced exactly (byte-identical error text) by
  `curl -o`-ing onto any running process's own executable path, in or
  out of this project. Fixed at the time with a temp-file-then-rename
  dance (POSIX allows renaming over a running binary's path even though
  it disallows opening it for writing) - **since fully superseded**:
  `add-native-agent-packaging` replaced the whole raw-binary-plus-
  script-written-unit approach with a native `.deb`/`.rpm` for exactly
  this reason (see below); dpkg/rpm handle upgrading a running service
  correctly by design, with no hand-written workaround needed at all.

## Native packaging for node-agent-client (Linux only)

`add-native-agent-packaging` replaced `install.sh`'s raw
`curl -o /opt/openvox-console/bin/node-agent-client` download (and its
hand-written systemd unit and package-inventory fact heredocs) with a
real `.deb`/`.rpm` built by `cmd/build-agent-packages` (via
[`nfpm`](https://github.com/goreleaser/nfpm), a pure-Go package
builder - no `dpkg-deb`/`rpmbuild` needed on this project's own build
machine, confirmed live via a throwaway spike before committing to it)
and served from a minimal self-hosted apt/yum repository
(`internal/agentdist/repo.go` hand-generates `Packages`/`Release` and
`repomd.xml`/`primary.xml` - deliberately not signed; the repo is
marked `[trusted=yes]`/`gpgcheck=0`, no weaker a trust boundary than the
install script itself already crossed). `install.sh` now just adds that
repo and runs `apt-get install`/`yum install node-agent-client` - the
package's own postinst re-derives everything node-specific (Puppet cert
paths) and console-specific (the transport address, written by the
script to `/etc/node-agent-client/console.env` right before installing,
since that's runtime config the package can't have baked in at build
time) on every install *and* upgrade.

**Live-verified end to end on real systemd containers** (not plain
`docker exec` sessions, which never actually run the service - the gap
that let the original `curl: (23)` bug go uncaught for as long as it
did): fresh installs via both `apt-get install` and `yum install`
against the console's own live-served repo; re-running the install
script while node-agent-client was actively running (the exact bug
scenario above) upgraded it cleanly with no error and a new PID,
confirming dpkg/rpm handle this correctly by design; and a simulated
node still on the old script-managed install (hand-written unit + raw
binary, actually running) migrated cleanly to the package-managed
install with no dpkg file-conflict, confirmed via `dpkg -S` showing the
package now owns the binary's path.

**Windows and macOS deliberately did not get the same treatment** - a
real `.msi`/`.pkg` was researched, not assumed impossible: an `.msi` is
technically buildable from Linux (`wixl`/`msitools`), but far less
proven than `nfpm` and doesn't clearly beat the fix already shipped for
the running-service case (stopping the service before download, since
Windows locks a running `.exe` outright rather than allowing the
POSIX rename trick). A real `.pkg` needs `pkgbuild`/`productbuild` and,
more importantly, code signing and notarization to avoid Gatekeeper
friction - infrastructure (a Mac, an Apple Developer account) this
project has never had, same gap already noted for the raw binaries
above. Both platforms stay on the binary-plus-script approach - already
fixed for the running-service bug - documented here explicitly so this
reads as a deliberate, researched decision, not an oversight.

## Node deletion: deactivate + clean cert, not a data purge

`add-node-deletion` added a "Delete" action on the Nodes page. What it
actually does, and why, based on real findings against this project's
own running openvoxdb and CA - not assumed from PuppetDB's general
reputation:

- **It deactivates the node in openvoxdb** (`POST /pdb/cmd/v1`,
  `deactivate node` version 3) and **cleans its CA certificate record**
  (the same operation the existing "Clean certificate" action already
  performs) - both, in one action, gated by a new `nodes:manage`
  permission (distinct from `nodes:certs:manage`, which stays scoped to
  operator-invoked cert lifecycle actions in their own right).
- **Both halves matter for the node to actually disappear from the
  page.** This was a real gap caught live, not anticipated in the
  original design: the Nodes page's list is a union of openvoxdb's
  inventory (`GET /api/v1/nodes`) and the separate connectivity/CA
  registry (`GET /api/v1/node-connectivity`) - deactivating a node in
  openvoxdb alone left it still rendering on the page via its lingering
  certificate entry. A node deleted with no CA client configured
  (`CONSOLE_CA_CLIENT_URL` unset) only gets the openvoxdb half done, and
  may keep appearing - consistent with every other cert-related feature
  on this page already degrading the same way when unconfigured.
- **No immediate data purge, deliberately.** Deletion does not force
  removal of the node's historical facts/catalogs/reports - openvoxdb's
  own background garbage collection (`node-purge-ttl`-driven) is
  responsible for that, on its own schedule (left at this project's
  default, unconfigured). An admin `POST /pdb/admin/v1/clean`
  `purge_nodes` operation looked plausible for forcing this
  immediately, but was never actually tested against a real instance -
  don't assume it works as expected without live verification first, if
  that need ever comes up.
- **No "show inactive" list, and none is planned.** Confirmed live and
  against PuppetDB's own documentation: once a node is deactivated,
  openvoxdb's `nodes` query entity excludes it unconditionally, with no
  documented override - not even an explicit
  `nodes { deactivated is not null }` filter returns it. The only place
  a deactivated node's record is still reachable is a direct single-node
  lookup by its exact certname (`GET /pdb/query/v4/nodes/<certname>`),
  which is how the delete endpoint itself tells "unknown certname" apart
  from "already deleted" (see `internal/openvoxdb.Client.NodeByCertname`
  vs. `Nodes`) - but there's no way to list every deactivated node back
  out. Building a "recently deleted" view would mean the console
  inventing and maintaining its own separate record of what it has
  deleted, which was deliberately ruled out - a deleted node is simply
  gone from this console's UI once its certname is no longer
  remembered, same as it would be from raw `openvoxdb`/PuppetDB itself.

## Package-inventory reporting is now a per-node toggle, not a package default

`add-package-inventory-toggle` moved package-inventory reporting from
static content baked into every node-agent-client `.deb`/`.rpm`
(`add-native-agent-packaging`) to something an operator turns on or off
per node, from the node detail page's Packages section - flipping the
switch dispatches a request to that node over the existing NATS-based
node transport (the same mechanism orchestrator runs already use),
which writes or removes `/opt/puppetlabs/facter/facts.d/package_inventory.sh`
and immediately runs `puppet agent -t`, so the effect (or its removal)
shows up in openvoxdb right away rather than waiting for that node's
next scheduled run.

**A real, deliberate side effect of this change: reporting resets to
off for every node that upgrades from the old package-bundled fact.**
Confirmed live (a small `nfpm` spike, not assumed): when a package no
longer declares a file the previous installed version did, `dpkg`
removes it as a normal part of the upgrade transaction - so a node that
already had this fact from the old always-on package loses it the
moment it upgrades to a package built after this change, unless an
operator explicitly turns it back on via the toggle. There is no
practical way to distinguish "this node's operator wants reporting kept
on" from "this node never had it," so no auto-re-enable is attempted -
this is a one-time, expected transition, not a bug, but worth knowing
before wondering why a previously-reporting fleet suddenly went quiet
after an upgrade.

**The toggle requires the node to be currently connected** - it's a
live dispatch, not a queued/deferred setting, so the control disables
itself (and both API endpoints return 409) whenever the node is
offline. Toggling to the state a node is already in is a deliberate
no-op: no filesystem write, no Puppet run, and the response omits the
`output` field entirely (a `ran: true`/`false` flag internally
distinguishes "no run happened" from "a run happened and legitimately
produced empty output with exit code 0" - Output's zero value alone
can't tell those apart, confirmed by writing a test for it after
noticing the ambiguity while implementing).

**A real architectural finding surfaced while testing the existing
single-in-flight busy guard against these new actions**: `nats.go`
delivers messages to a single subscription strictly serially - a
node-agent-client's dispatch subscription callback never receives a
second message until the first one's handler returns. This means two
requests to the *same* node can never actually race through the real
transport; the `busy` guard in `internal/nodeagent/client.go` only
matters if something calls its dispatch method directly, out of band.
Not a risk with this client's current single-subscription design, but
worth knowing if that ever changes.

## Node group match-rule editor: structured fields, with a JSON escape hatch

The group page's match-rule field (`factPath`/`operator`/`value`
conditions) is a structured per-condition builder, not a raw JSON
textarea - a real live bug this project hit: an operator typo'd an
empty `factPath` into the old textarea and the group silently matched
zero nodes, with no error anywhere. The builder now blocks saving a
condition with an empty fact path, or a `~` (regex) condition whose
value doesn't compile as a regular expression, and shows which
condition is the problem.

**This validation is frontend-only.** `internal/classifier`'s matching
behavior is unchanged: a condition with an empty `factPath` or an
invalid regex still just evaluates to "never matches," silently, with
no backend-side error. A group saved via the raw API, or via the
editor's own "Edit as JSON" toggle (kept specifically as an escape
hatch for pasting a rule or a shape the builder doesn't model), can
still produce a rule that can never match - the builder only prevents
that mistake for someone using its structured fields.

**The JSON view is a one-way projection of the builder's state, not a
second live copy.** Switching to JSON view regenerates the textarea
from current row state; switching back parses the textarea and rebuilds
the rows. Only one representation is ever "live" at a time, so there's
no way for the two to silently drift out of sync - see design.md in
`add-match-rule-builder-ui` for the reasoning.

## Node enrollment: the install script gets a node its own certificate

The install script enrolls a node that has no signed Puppet certificate
rather than stopping and telling the operator to run `puppet agent -t`
themselves and start over. A brand-new node goes from nothing to
enrolled, installed, and connected in one run of one script.

**`CONSOLE_PUPPET_SERVER_PUBLIC_ADDR` is the address a node enrolls
against**, and it is node-facing - the same distinction
`CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR` already draws between what the
console binds and what a remote node dials. It is deliberately *not*
derived from `CONSOLE_CA_CLIENT_URL`: that value is this console's own
client view of the CA API and is routinely `localhost` or a container
name, which would be actively wrong written into a remote node's
`puppet.conf`. Whatever name is used has to be in openvoxserver's cert
SANs or the node's TLS verification fails - in this dev setup that is
`localhost,puppet` (docker-compose.yml's `DNS_ALT_NAMES`), so `puppet`
is the usable one, with a node outside the compose network needing
`puppet` pointed at this host's LAN IP in its own `/etc/hosts`.

**Leaving it unset means the script does not touch the node's Puppet
server configuration at all.** That keeps this change additive: a node
already pointed at a server by other means (a golden image, config
management, DNS) still enrolls against it, and an unconfigured console
never silently redirects nodes somewhere they cannot reach.

**What the script actually does when a node has no certificate:** it
points the node at the configured server, runs `puppet ssl bootstrap`
(deliberately not `puppet agent -t` - a catalog run can fail for reasons
that have nothing to do with enrollment, and would then be reported as
an enrollment failure), and waits for the request to be signed, polling
every 15 seconds for up to 5 minutes total. Those are two distinct
Puppet settings and it matters: `waitforcert` is the poll *interval*
and `maxwaitforcert` is the total wait, defaulting to `unlimited`.
Passing only the first is how you accidentally build a script that hangs
forever - which is exactly what the first version of this did, caught in
live testing when a run kept waiting long after the certificate had been
signed.

**Three outcomes, told apart by whether the certificate now exists**,
not by exit code - `puppet ssl bootstrap` exits 1 both for a CA it
cannot reach and for a request still waiting, so the code carries no
information:

| Outcome | What the operator sees |
| --- | --- |
| Signed while waiting | The run continues and finishes the install - no second invocation |
| Still unsigned when the wait expires | Exit 1, naming this console's Nodes page to sign it, and saying the script can simply be re-run afterwards |
| CA unreachable/unresolvable/refusing | Exit 1, reported as a connectivity or TLS problem, explicitly *not* as a pending signature, with the underlying error above it |

Note that an unreachable CA is reported only after the wait expires, not
immediately - the agent keeps retrying for the whole window, which is
the right behavior for a transient network or DNS blip but does mean a
genuinely wrong address takes the full 5 minutes to report.

**Autosign in this dev environment:** `docker-compose.yml` sets
`AUTOSIGN` on openvoxserver, defaulting to `true` - which is what makes
`make openvox-test` and an unattended install script run work without a
human. To exercise the *not*-autosigned path, recreate the service with
it off:

```
OPENVOXSERVER_AUTOSIGN=false docker compose up -d openvoxserver
```

Puppet Server reads that setting once at startup, so editing
`puppet.conf` inside the running container does nothing - the container
has to be recreated (and the entrypoint rewrites `puppet.conf` on every
start anyway).

**macOS and Windows enrollment is static-verified only** - the same flow
is implemented in each script's own idiom, their rendered output is
asserted in `internal/agentdist/handlers_test.go`, and the PowerShell
one is parse-checked with a real `pwsh`, but this project's dev
environment has no macOS or Windows host to run either on. Same posture
as the rest of the multi-platform agent work.

## Postgres 18 data layout, and upgrading a pre-18 deployment

Both databases default to `postgres:18-alpine`, and both volumes are
mounted at `/var/lib/postgresql` - **not** `/var/lib/postgresql/data`.
Postgres 18+ images put the cluster in a major-version subdirectory
(`/var/lib/postgresql/18/docker`) so `pg_upgrade --link` can run across
versions without crossing a mount boundary.

An 18+ image started against a `.../data` mount that already holds
pre-18 data refuses to start, with:

```
Error: in 18+, these Docker images are configured to store database data in a
       format which is compatible with "pg_ctlcluster" ...
       Counter to that, there appears to be PostgreSQL data in:
         /var/lib/postgresql/data (unused mount/volume)
```

This is a refusal, not corruption - the old data is untouched.

openvoxdb 8.15.0 is verified against PostgreSQL 18.6: all 62 schema
migrations apply and the service reaches `status=running`.

### Upgrading an existing 17 deployment

Dump and restore. `pg_upgrade` would need both major versions present in
one image, which these images do not provide.

```sh
# 1. Stop the writers, leaving the databases up.
docker compose -f docker-compose.yml stop console openvoxdb

# 2. Dump each database from the still-running 17 containers.
docker compose -f docker-compose.yml exec -T openvoxdb-postgres \
  pg_dumpall -U openvoxdb > openvoxdb-17.sql
docker compose -f docker-compose.yml exec -T console-postgres \
  pg_dumpall -U console > console-17.sql

# 3. Check both dumps are non-empty BEFORE destroying anything.
ls -l openvoxdb-17.sql console-17.sql

# 4. Remove the old volumes. This deletes the 17 data - the dumps above
#    are now the only copy, so do not skip step 3.
docker compose -f docker-compose.yml down
docker volume rm <project>_openvoxdb-postgres-data <project>_console-postgres-data

# 5. Start just the databases on 18 - they initialise empty, and
#    openvoxdb-postgres gets pg_trgm from the compose file's config.
docker compose -f docker-compose.yml up -d openvoxdb-postgres console-postgres

# 6. Restore.
docker compose -f docker-compose.yml exec -T openvoxdb-postgres \
  psql -U openvoxdb < openvoxdb-17.sql
docker compose -f docker-compose.yml exec -T console-postgres \
  psql -U console < console-17.sql

# 7. Bring the stack back up.
docker compose -f docker-compose.yml up -d
```

Staying on 17 is also supported, but takes two changes rather than one -
set `OPENVOXDB_POSTGRES_VERSION=17-alpine` **and** move that service's
volume mount back to `/var/lib/postgresql/data`. Changing only the
version reproduces the error above.

### pg_trgm

openvoxdb exits at startup with `PuppetDB requires the PostgreSQL
`pg_trgm` extension` if it is missing. `docker-compose.yml` supplies it
as an inline config mounted into `/docker-entrypoint-initdb.d`, which
Postgres runs **only while initialising an empty data directory**. A
database that already exists needs it created by hand:

```sh
docker compose -f docker-compose.yml exec openvoxdb-postgres \
  psql -U openvoxdb -c 'CREATE EXTENSION IF NOT EXISTS pg_trgm;'
```

## Vulnerability tracking: providers, credentials, mirrors, and limits

`add-vulnerability-tracking` reports which nodes are affected by which
vulnerabilities, merged across one or more *providers*. A provider type is
compiled into the console (today `osv` and `tenable`); administrators create
instances of them on the Vulnerabilities → Providers page, and any number can
be enabled at once. Each vulnerability on a node is one finding, listing every
enabled provider that reports it with that provider's own severity, package,
and fixed version.

**Existing deployments must grant the new permissions.** `vulnerabilities:read`
(the Vulnerabilities pages, the node page's Vulnerabilities section, their API)
and `vulnerabilities:manage` (the Providers page and its API) are only given to
the bootstrap admin role of a *fresh* install - on an upgraded deployment no role
has them until an administrator adds them on Admin → Roles, the same as
`nodes:certs:manage` before them. `nodes:read` alone is deliberately not enough:
findings are more sensitive than inventory. The new `CONSOLE_AUDIT_VULNERABILITIES`
category (default `writes`) audits provider changes and manual syncs; audit
events name changed fields and which credentials are set, never credential
values. At `full` it also audits views.

**`CONSOLE_SECRETS_KEY_FILE` holds the key that seals provider credentials** in
Postgres (AES-256-GCM, bound to the provider's id). Generate one with
`openssl rand -base64 32 > secrets.key` (mode 0600) and point the variable at it -
or set `CONSOLE_SECRETS_KEY` directly, though a file keeps it out of the process
environment. It is optional: without it, OSV works normally and any provider
with credentials (Tenable) is refused at configuration time with an error naming
the setting. **Back the key up with the database backups but store it
separately.** Losing it doesn't lose findings, but every stored credential
becomes unreadable: those providers' syncs fail with a decryption error until an
administrator re-enters their credentials under a new key. The API never returns
credential values, only whether each is set; leaving a credential field empty
when editing keeps the stored value.

**Outbound network access**, per provider - nothing leaves the console until an
administrator enables one:
- `osv`: HTTPS to its data location, by default
  `storage.googleapis.com/osv-vulnerabilities` - `all.zip` per distribution
  directory on the first sync (Debian ~70 MB, Rocky Linux ~5 MB, AlmaLinux ~6 MB,
  Red Hat ~26 MB, Ubuntu ~700 MB, streamed to a temporary file and filtered to
  the releases the fleet runs), then `modified_id.csv` and changed records.
  Only directories for distributions present in the fleet are downloaded.
- `tenable`: HTTPS to `cloud.tenable.com` (or the configured API location).

**Air-gapped sites point an OSV instance at an internal mirror** (its "Data
location"). The mirror must reproduce OSV's bucket layout for each distribution
the fleet runs: `<Directory>/all.zip`, `<Directory>/modified_id.csv`
(`<modified>,<id>` lines, newest first), and `<Directory>/<id>.json` for records
listed there as changed - a record that 404s is treated as withdrawn and removed.
`gsutil -m rsync -r gs://osv-vulnerabilities/Debian ./Debian` (per directory)
produces exactly that. Directory names contain spaces (`Rocky Linux`, `Red Hat`).
A plain static web server is enough. Two OSV instances with different data
locations keep separate mirrors and work side by side.

**Coverage is explicit.** A node no enabled provider assessed is shown as *not
assessed*, with each provider's reason - never as clean:
- `unsupported_os`: OSV covers Debian, Ubuntu, AlmaLinux, Rocky Linux, and RHEL
  7-10; Windows, macOS, SUSE, and others aren't assessed.
- `no_package_data`: the node reports no package inventory (enable it on the
  node page).
- `agent_upgrade_required`: the node's package data comes from a package-inventory
  fact older than `fix-package-inventory-fact-fidelity` (no epochs, no apt source
  packages), which would give wrong answers; upgrading node-agent-client fixes it
  on the node's next Puppet run.
- `not_seen_by_tenable`: no Tenable asset correlated to the node.
- `not_yet_synced`: the provider hasn't completed a sync.

**OSV limits to know:**
- RHEL matching uses the mainline repositories only (RHEL 7 server/workstation/
  client/computenode; 8 and 9 BaseOS/AppStream/CRB; 10.x every minor release up
  to the node's). Nodes on EUS/AUS/E4S or add-on repositories are matched against
  mainline data and can be over-reported. Ubuntu Pro, FIPS, and Realtime streams
  aren't used either.
- Severity is the highest CVSS v3 base score in the advisory, falling back to the
  distribution's own rating (Ubuntu priority, Debian urgency). This makes Debian
  and Ubuntu nodes look noisy: many CVEs the distribution rates "unimportant" or
  has decided not to fix still carry a high CVSS score and appear as open findings
  with "No fix released" - on an Ubuntu 24.04 node, thousands of them, mostly
  CVEs Canonical has marked ignored, deferred, or needs-triage (a status OSV's
  feed does not carry, so the console cannot tell them apart). The fleet list
  therefore defaults its Fix filter to "Fix available", showing what patching
  would actually change; set Fix to "All" to see everything, including findings
  with no fix released. Severity prefers the distribution's own rating
  (Ubuntu priority, Debian urgency) over CVSS on Debian and Ubuntu, since a CVE
  the distribution calls negligible should not read as critical; the Red Hat
  family keeps CVSS first, as that is the distribution's own assessment there.
  AlmaLinux publishes no severity, so its findings show "Unknown".
- Findings refresh when each provider syncs (default hourly for OSV), not on every
  Puppet run.

**Findings against packages that were never installed.** Until this was fixed,
the apt fact reported packages in dpkg's `config-files` state (removed, but their
configuration files retained) as installed, so the console raised findings against
software that was not on the node - overwhelmingly old kernels, which match every
advisory fixed after their version. On one three-node Ubuntu 24.04 fleet that
accounted for roughly 3,192 findings with a fix available, which was the entire
fix-available list. This is a different problem from the "No fix released" noise
above: that noise is real CVEs the distribution has chosen not to fix, whereas
these were findings for packages that did not exist on the machine. Two things to
know when looking at a console that has not converged yet:
- The findings clear on convergence, not immediately. Upgrading node-agent-client
  rewrites the fact in place (see the package inventory section above - no Puppet
  run is triggered, and a node with reporting disabled stays disabled), the node's
  next scheduled Puppet run republishes corrected inventory, and findings whose
  packages are no longer reported close on the provider's next sync.
- To clear the stale entries from the node itself, purge them: `apt purge '~c'`.
  `apt autoremove` does not touch them and reports nothing to remove, because the
  packages are already removed - only their configuration files remain, which is
  exactly why they stay invisible to an operator while still being reported.

**Tenable correlation.** A full sync (the first, then daily) exports every host
asset and every open or reopened finding; syncs in between export changes since the
last one, including fixed findings, which close. Info-severity plugins aren't
imported - they are detections, not vulnerabilities. Each asset is matched to a
node by comparing, case-insensitively, its FQDNs and then its hostnames against
every node's certname and reported FQDN. An asset whose names match no node, or
more than one, is not guessed at: its findings are skipped and it is counted in
the provider's "unmatched assets" statistic - a growing count usually means
certnames that differ from what Tenable sees (short hostnames, a different
domain). A scanned node with no findings still counts as assessed.

**What was verified, and how** (evidence in the `add-vulnerability-tracking`
change's `evidence/` directory under `openspec/changes/`, or its dated copy under
`openspec/changes/archive/` once archived):
- OSV, live: real Debian 12 and Rocky 9 systemd containers enrolled through the
  install script - source-package matching, epoch-aware RPM matching, a finding
  closing as fixed after a real package upgrade while the same finding stayed
  open on another node, disable/re-enable keeping the original first-seen time,
  `agent_upgrade_required` and `no_package_data` coverage, and two OSV instances
  (public bucket and a local static mirror) enabled together, with the mirror
  instance's syncs making no connection beyond the mirror. A first sync of
  Debian 12 plus Rocky 9 took ~17 s and mirrored ~50,000 advisories.
- Tenable, fake-backed: no Tenable tenant exists in this environment, so the
  provider ran inside a real console process against a standalone fake of
  Tenable's export API (HTTPS, key-checked) with assets named after the live
  nodes - full then incremental syncs with the expected export filters, a CVE
  merged into the same finding OSV reports on that node, a plugin-only finding,
  hostname-only correlation, an unmatched asset counted and skipped, and
  credentials absent from every API response and log line. Behaviour against a
  real tenant (export timings, rate limiting, data quirks) is untested.
- Not live: the Ubuntu import (~700 MB download) was only exercised with small
  fixture archives, not against the real directory.
