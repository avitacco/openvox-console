# Design

## Context

See proposal.md for motivation. The constraints that shape the approach:

- **The client has no identity.** `cmd/node-agent-client` is built with a
  plain `go build`; nothing is stamped in, and the console records
  nothing about which build a node runs.
- **A node cannot speak first.** Node transport permissions let a node
  subscribe to `node.<certname>.dispatch` and publish replies to requests
  it received, nothing else (`internal/nodetransport/subjects.go`,
  `auth.go`). Anything the console learns from a client is the answer to
  a dispatch.
- **One request at a time.** `Client.dispatch` rejects a second request
  with a busy error while one is running.
- **Only the transport is authenticated.** `/packages/*` is deliberately
  unauthenticated, and the apt/yum repositories are `trusted=yes` /
  `gpgcheck=0`. The NATS connection is mutual TLS against the Puppet CA.
- **Linux is package-managed, the rest is not.** Linux clients are a
  `.deb`/`.rpm` whose postinst rewrites the environment file and runs
  `systemctl restart node-agent-client`. Windows installs a binary under
  `C:\Program Files\OpenVox Console` as a service; macOS installs a
  binary run by a launchd job with `KeepAlive`.
- **Package versions do not sort.** `VERSION` is `git describe --tags
  --always --dirty`, and with no tags `sanitizeVersions` yields
  `0.0.0+<sha>` (deb) and `<sha>` (rpm).
- **Connect notifications already exist.** `initialrun.Trigger` is fed by
  the node listener's `onConnect` and is the pattern to follow.
- **Uncommitted work in the same files.** `add-task-execution` has
  changes in `internal/nodeagent/handler.go`, `config.go` and
  `internal/orchestrator` that are not committed. This change is written
  on top of them.

## Goals / Non-Goals

**Goals:**
- A fix in the client reaches every connected node after a console
  deployment, with no action on the node.
- The console always knows, and shows, which nodes are behind.
- A bad or tampered artifact is never installed, and a failed upgrade
  leaves the old client running.
- Existing clients have a working one-time path onto an upgrade-capable
  build.

**Non-Goals:**
- Upgrading openvox-agent itself.
- Signing the apt/yum repositories or authenticating `/packages/*`
  (unchanged; integrity for upgrades comes from the digest sent over the
  transport).
- Automatic downgrade when a console is rolled back.
- Staged or scheduled rollouts (canary groups, maintenance windows).
  Bounded concurrency is the only pacing.
- Automatic rollback to the previous client after a new one starts and
  then misbehaves. Pre-install validation is the safeguard.
- Recording upgrades in the Activity log; they are logged and shown per
  node.
- Upgrading a pre-packaging, script-managed Linux install in place; the
  install script already migrates those.

## Decisions

### 1. Build identity is the executable's digest plus a stamped build time

The client computes the SHA-256 of `os.Executable()` once at start. The
console computes the SHA-256 of each embedded `bin/node-agent-client-*`
at start. Equal digests mean "same build" with no possibility of drift
between a version string and the actual bytes; the `.deb`/`.rpm` contain
the same binary file, so a package-installed client hashes identically.

The digest answers "same or different" but not "which is newer", and
"different" alone is not safe: during a rolling deployment of a console
cluster, or after a rollback, a node may be connected to an instance
serving an older client and would be flapped back and forth. So the
build time (UTC, second precision) and version are stamped into the
client with `-ldflags -X` in `make agent-binaries` and the Dockerfile,
and written next to the binaries as `bin/builds.json` for the console to
embed. A node is out of date only when the digests differ **and** the
console's build time is later; an unstamped console build (plain `go
build` in development) never upgrades anyone.

*Alternatives:* a version string alone (drifts from the bytes, and
`git describe` output does not order); "differs means upgrade" (simple,
honours "binary reflects the running console build" literally, but
flaps in a cluster and silently downgrades on rollback).

### 2. Two new dispatch actions; the console always asks

- `agent_status` - reply: `{digest, buildTime, version, os, arch,
  installMethod: deb|rpm|binary|unknown, acceptsUpgrades}`. Read-only.
- `agent_upgrade` - params: `{digest, buildTime, artifact: {url, size,
  sha256, format}}`. Reply: started, or an error string.

Both go through the existing dispatch subject and `requestData` /
`wireResponse` shapes, so no transport permission changes. A client that
predates this change answers `unsupported action "agent_status"`, which
the console records as an unknown build - exactly the signal needed, with
no version negotiation.

Because the upgrade is an ordinary request, the existing single-in-flight
rule gives "never interrupt work" for free: a busy client answers busy,
and the coordinator reschedules.

*Alternative:* the client polls an HTTP endpoint. Rejected: the client
knows only the transport address, the HTTP surface is unauthenticated,
and it would add a second, weaker channel for a root-level action.

### 3. Download over HTTP, trust from the transport

The upgrade request carries the artifact URL (built from
`CONSOLE_BASE_URL`), size and SHA-256. The client downloads with a size
cap equal to the declared size, into a 0700 directory beside the task
cache (`/opt/puppetlabs/node-agent/upgrade`, `%ProgramData%\PuppetLabs\
node-agent\upgrade`), and compares size and digest before anything else
touches the file. The download itself needs no authentication or TLS
verification to be safe: its integrity is established by a digest that
arrived over mutual TLS from the console, which can already run tasks as
root on the node, so no new trust is extended.

Linux packages are served from the existing apt/yum package routes.
Windows and macOS use `GET /packages/node-agent-client?os=&arch=`.

*Alternative:* chunking 13 MB over NATS - needs a new subject, raised
payload limits and reassembly, for no gain in integrity.

### 4. Validate before installing

After the digest check the client executes the downloaded binary with a
new `--build-info` flag (for a package, the binary is first extracted
from the verified archive) and requires it to exit zero and print the
requested digest's build time. This catches an artifact for the wrong
architecture or one that cannot start, before the working client is
replaced. It is the only protection against a bad build, since there is
no automatic rollback.

### 5. Install through the mechanism the client was installed with

- **deb:** `dpkg -i <file>`; **rpm:** `rpm -U --oldpackage <file>`. The
  package database stays truthful, postinst regenerates the environment
  file and restarts the service as it does today. The install method is
  detected by asking `dpkg-query -S` / `rpm -qf` who owns the running
  executable.
- **The installer must outlive the service.** postinst restarts the unit,
  and systemd kills everything in the unit's cgroup, including a `dpkg`
  the client spawned. The client therefore launches the install with
  `systemd-run --unit=node-agent-client-upgrade --collect --quiet`, which
  runs it in its own transient unit, and replies "started" as soon as the
  unit is accepted. Without systemd the install runs in a new session
  (`setsid`) and the client exits afterwards.
- **macOS:** write the new binary beside the installed one, `chmod`,
  rename over it (atomic; the running process keeps its old inode), reply,
  then exit. launchd's `KeepAlive` starts the new binary.
- **Windows:** a running `.exe` cannot be overwritten but can be renamed.
  Rename the current binary to `node-agent-client.exe.old`, move the new
  one into place, reply, then stop the service process with a non-zero
  exit so the Service Control Manager's recovery action restarts it. The
  install script gains explicit `sc.exe failure` restart actions, and the
  client removes a leftover `.old` on start.
- **unknown / script-managed Linux:** refuse with "re-run the install
  script".

*Alternative on Linux:* `apt-get install --only-upgrade` against the
console repository. Rejected as the upgrade path because it depends on
the node's apt state and lock, on network access to other configured
repositories during `apt-get update`, and on version ordering. Ordering
is fixed anyway (decision 7) for the manual path.

### 6. A coordinator on the console, driven by connects and a sweep

A new package holds the coordinator and its store. One row per node in a
new table (migration `000028`):

`certname` (PK), `digest`, `build_time`, `version`, `os`, `arch`,
`install_method`, `accepts_upgrades`, `reported_at`, `upgrade_state`
(`idle|requested|failed|exhausted`), `target_digest`, `attempts`,
`last_error`, `requested_at`, `next_attempt_at`.

- **On connect** (same `onConnect` fan-out as `initialrun`): dispatch
  `agent_status`, upsert the row. If `upgrade_state = requested` and the
  reported digest equals `target_digest`, mark `idle` and clear the
  error.
- **Sweep** (every minute): pick rows that are out of date, connected,
  upgrade-capable, and due (`next_attempt_at <= now()`), up to the
  concurrency limit; claim each with a conditional `UPDATE ... RETURNING`
  that sets `requested`, so two console instances never instruct the
  same node. Then dispatch `agent_upgrade`.
- **Outcomes:** busy reply - leave `idle`, due again in one minute, not
  counted as an attempt. Error reply - `failed`, `attempts+1`,
  `next_attempt_at = now() + backoff` (5 min doubling, capped at 6 h).
  No matching build within 10 minutes of `requested_at` - same as an
  error, reason "did not report the new build". After 5 attempts at one
  `target_digest` - `exhausted`. A different console build resets
  attempts.
- **Concurrency:** at most 20 nodes in `requested` across the cluster,
  counted in the claiming query.
- The row's existing claim semantics make a lease unnecessary; the worker
  is registered in `surface.go` for every mode that runs the node
  listener, beside `initial-run-trigger`.

Out-of-date is computed at read time from the row and the console's own
`builds.json`, not stored, so a console upgrade changes every node's
state without a write.

### 7. Monotonic package versions

`build-agent-packages` takes the build time as well as the version:

- deb: `<base>+<YYYYMMDDHHMMSS>.<sha>` where `<base>` is the tag, or
  `0.1.0` when there is none.
- rpm: version `<base>`, release `<YYYYMMDDHHMMSS>.<sha>`.

`0.1.0+2026…` is greater than every existing `0.0.0+<sha>` under dpkg's
comparison. For rpm a version alone cannot be made to win: the existing
rpm Version is the bare sha, and one that begins with a digit
(`4a4a7ef`) compares above any small version number under `rpmvercmp`.
New rpms therefore also carry **epoch 1**, which is compared before the
version. Both orderings are asserted in tests against the versions
already shipped, letter- and digit-leading. This makes `apt-get install`
/ `dnf install` in the install script upgrade an existing client, which
is the bridge for nodes whose client predates `agent_status`.

### 8. Controls

- `CONSOLE_AGENT_AUTO_UPGRADE` (default `true`) in `internal/runtime`.
  Off stops the sweep from claiming; status collection continues.
- `NODE_AGENT_ACCEPT_UPGRADES` (default `true`) on the node; `false`
  makes the client refuse and report `acceptsUpgrades: false`, which the
  console shows as "upgrades declined on this node" rather than a
  failure.
- `POST /api/v1/nodes/{name}/agent-upgrade`, `nodes:manage`: resets
  attempts and claims immediately, bypassing the auto-upgrade switch;
  409 with a reason when the node is disconnected, current, or cannot
  upgrade. Audited like the certificate actions.

### 9. Surfacing

`nodeStatus` in `internal/nodeconnectivity` gains an `agent` object
(`version`, `buildTime`, `state: current|outdated|unknown|unreported`,
`upgrade: {state, error, attempts, nextAttemptAt}`, `canUpgrade`). The
nodes page shows a badge on out-of-date rows; the node page shows the
build and the failure reason with an "Upgrade client" action; the
dashboard's Fleet status section shows counts and whether auto-upgrade
is on (there is no separate fleet-status tab: the System page holds
stack status only). All new strings
go through the existing gettext catalogs.

## Risks / Trade-offs

- [A bad client build is pushed to the whole fleet] → pre-install
  validation (decision 4), bounded concurrency, and confirmation by
  reconnect: a build that cannot connect stops the rollout at 20 nodes
  and shows as failed. Those nodes need the install script re-run after
  a fixed console build; there is no automatic rollback.
- [Windows and macOS cannot be exercised live in this environment] →
  the replace-and-restart logic is unit-tested behind an interface with
  per-OS implementations, and the scripts are statically checked, as
  with the existing installers. The first real Windows/macOS upgrade is
  unverified until someone has such a node; documented in operations.md.
- [Console rollback leaves nodes on a newer client] → accepted;
  newer clients must keep working against an older console's requests,
  which holds because requests are additive. Re-running the install
  script downgrades nothing either (versions are monotonic), so a
  deliberate downgrade means removing the package first.
- [`systemd-run` missing or refused] → fall back to a new session and
  report clearly; a failed launch is an ordinary upgrade failure with
  backoff.
- [`CONSOLE_BASE_URL` not reachable from nodes] → the download fails,
  the reason is shown per node, attempts back off and stop at five.
- [Existing clients cannot self-upgrade] → shown in Fleet Status as
  needing the install script once; decision 7 makes that script (or
  `ensure => latest`) actually upgrade them.

## Migration Plan

1. Land `add-task-execution` (or rebase this onto it) so the client
   handler changes are not interleaved.
2. Deploy the console. Migration `000028` is additive. Existing nodes
   reconnect, answer `unsupported action`, and appear as "unknown build -
   re-run the install script".
3. Re-run the install script once on each existing node (or apply
   `ensure => latest` through Puppet). From then on the node upgrades
   itself.
4. Rollback: set `CONSOLE_AGENT_AUTO_UPGRADE=false`, or deploy the
   previous console; the table is ignored by older code and the down
   migration drops it.

## Implementation Notes

Where the implementation settled details the decisions above left open,
or departed from them:

- **Decision 1.** The cross-compile and `builds.json` are produced by a
  new `cmd/build-agent-binaries`, shared by the Makefile and the
  Dockerfile. The console takes each digest from the embedded bytes and
  uses `builds.json` only for the build time and version, and only when
  its digest matches - a binary rebuilt without rewriting that file is
  treated as unstamped rather than advertised wrongly.
- **Decision 3.** For a binary install the verified artifact is copied
  next to the installed executable and its digest checked again as it
  is written, so what is installed is what was verified. On Windows the
  upgrade directory is created with an explicit Administrators/SYSTEM
  ACL, since a plain directory under `%ProgramData%` is writable by
  ordinary users.
- **Decision 4.** The client binary is unpacked from the `.deb`/`.rpm`
  in Go, so validation needs no `dpkg-deb`, `rpm2cpio` or `cpio` on the
  node. The packages pin gzip payloads to make that possible.
- **Decision 5.** After handing a package to the installer the client
  stays busy until it is restarted (up to three minutes), refusing
  other requests rather than starting work the restart would kill. The
  hold runs off the NATS delivery goroutine: NATS delivers to a
  subscription serially, so blocking there would queue requests instead
  of refusing them. `rpm -U` also passes `--replacepkgs`. The per-node
  opt-out lives in a new optional `/etc/node-agent-client/local.env`,
  because postinst regenerates `environment` on every install.
- **Decision 6.** The table is `node_agents`, with a `capable` column
  distinguishing "cannot report" from "reported". A node found out of
  date on connect is upgraded then, not at the next sweep. The claim
  takes a transaction-scoped advisory lock so the concurrency count
  cannot be read by two claimers at once. A client with no build time
  of its own (a development build) is treated as older than any stamped
  build.
- **Decision 8.** A refused manual upgrade also reports `busy` and
  `in-progress`, beyond the three reasons listed.
- **Found in live verification (tasks 1.5, 5.1-5.5), and fixed.**
  - *Package scripts.* `postrm.sh` tore the service down on every run,
    and both package managers run it during an upgrade. On rpm it runs
    after the new package's `%post`, so every rpm upgrade - by the
    install script or by the console - left the client stopped and
    disabled. It now acts only on a real removal, and the rpm runs
    `postinst.sh` as `%posttrans` rather than `%post`, which is what
    repairs an upgrade *from* a package that still carries the old
    script.
  - *Connect before subscribe.* A node's connection is announced
    before its client subscribes for requests, so the status request
    sent on connect could find nobody listening. The coordinator took
    that for the node having gone and never asked again, which left a
    just-restarted client's build unread - about one reconnect in
    three. It now retries briefly and, failing that, asks at the next
    sweep.
  - *Connects before the coordinator is wired.* Nodes reconnect within
    a second of a console restart, before the coordinator receives
    connect notifications. It now asks every connected node when it
    starts.
  - A failed build reading is now logged; it was silent.
  - *Busy was not busy.* Decision 2 relies on a busy client answering
    busy. It did not while a run or task was executing: nats.go
    delivers to a subscription serially and the client ran the work on
    that delivery goroutine, so a second request waited in the client's
    queue and ran afterwards. An upgrade still never interrupted work,
    but the console's request stayed outstanding for the length of the
    task, and a manual upgrade mid-task answered 500 after a 30 s
    timeout. The client now handles each request on its own goroutine,
    so the refusal is immediate and the refused request never runs.
  - *Status is answered while busy.* With real refusals, the status
    request the console sends on connect could cross a new node's first
    Puppet run and get one of them refused. A build-status request is
    read-only, so the client answers it whatever else is running and it
    does not make the client busy.
  - *Manual upgrade reports busy.* Since status is always answered, the
    only place a busy client says so is its reply to the upgrade itself,
    which the console sends in the background. `UpgradeNow` waits up to
    two seconds for that refusal, which arrives in milliseconds, and
    answers 409 `busy`; otherwise 202 as before.

## Open Questions

- Whether five attempts and a 20-node concurrency limit should be
  configurable. They are constants for now; nothing in the specs depends
  on the values.
