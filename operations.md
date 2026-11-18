# Operations

Engineering notes on running this console: what was verified and how,
known limitations and why they exist, and the reasoning behind
operational behavior. See architecture-summary.md for the design
principles behind them.

**How to install, scale, use and operate the console is in the guides**
on the project site, and in this repository under
[`marketing/guides/`](marketing/guides/). Each section below whose
procedure moved there says so, and links to its guide.

## Statelessness boundary: what survives routing a request to a different instance

For running several instances, see the **Run more instances** guide ([`scale/cluster.md`](marketing/guides/scale/cluster.md)). This section records which in-memory state exists, and why each piece survives load balancing.

The console is designed to be stateless wherever possible - every piece
of durable state lives in Postgres, and cross-instance coordination (where
needed) goes through NATS - specifically so a load balancer can route a
request to any healthy instance without a scripted promotion/demotion
step. That holds for almost everything. Three pieces of in-memory state
are the exceptions worth knowing about:

| State | Where | Survives routing to a different instance? |
| --- | --- | --- |
| RBAC token revocation cache | `internal/rbac.Revoker` | **Yes, when clustered.** In-memory only for zero-latency checks, but Postgres is the durable source of truth and a revocation is published over NATS (`rbac.revoked` subject) to every other running instance's in-memory cache immediately, with no restart. A request rejected for a revoked token on instance A is rejected identically on instance B. Each instance also re-reads revocations from Postgres every 30 seconds, so one missed on the bus - a route down at the wrong moment - is picked up within that window rather than lost. Instances should still be peered (`CONSOLE_CLUSTER_*`): unpeered, the 30-second re-read is the *only* propagation. |
| Node transport connections | `internal/nodetransport.Registry` | **Yes, when clustered.** A node's connection is still a single TCP connection terminated at one instance, but with the instances peered (`CONSOLE_CLUSTER_*`) a dispatch published by any routed instance - including one that terminates no node connections, like `web` - is routed to whichever one holds that connection, and the connection registry reflects connects and disconnects cluster-wide. An instance with no node listener (`web`) runs the registry on its own, and every registry asks the cluster once at start (`$SYS.REQ.ACCOUNT.<account>.CONNZ`, repeated twice while routes come up) for the nodes already connected, since events only cover what happens after it starts. Unclustered, this remains instance-local: only the instance a node connected to can reach it. |
| Orchestrator in-flight dispatch tracking | `internal/orchestrator.Dispatcher.pending` | **Instance-local, but no longer load-bearing.** A dispatch's response still returns to the instance that published it, so that instance tracks it to completion. What changed is that *any* instance can publish the dispatch, so triggering a run no longer depends on reaching a particular one. If the dispatching instance stops before a job reaches a terminal state, the stale-job reaper (`worker`/`all` mode, leased) records that job as failed rather than leaving it "running" forever. A patch run is judged by its heartbeat, not its age: see "Patch runs". |

Everything else - job/deploy/report history, users/roles/permissions,
node classification, activity log (written directly by the instance
where the action happened), audit log emission - reads and writes
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

Moved to the **Run modes** guide ([`scale/run-modes.md`](marketing/guides/scale/run-modes.md)), published on the project site. It covers what each mode serves and runs, and what each mode requires.

## Stack status page

Moved to the **Run more instances** guide ([`scale/cluster.md`](marketing/guides/scale/cluster.md)), published on the project site. See its "Check the whole fleet" section for the System page's Fleet Status tab: what it shows, the incomplete-picture banner, and dependencies that differ by instance.

## Multi-instance topology

Moved to the **Run more instances** guide ([`scale/cluster.md`](marketing/guides/scale/cluster.md)), published on the project site. It covers clustering, peer TLS and the two secrets, splitting into modes, `enc` instances as leaves, monitoring the bus and the migration path. Node certificate revocation is in **Manage node certificates** ([`nodes/certificates.md`](marketing/guides/nodes/certificates.md)).

## Postgres failover runbook

The runbook moved to the **Fail over to a standby database** guide ([`operate/postgres-failover.md`](marketing/guides/operate/postgres-failover.md)).

**Engineering notes** kept here:

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

This procedure was verified live against the `postgres-replication`
fixture: a row written on the primary appeared on the standby via
streaming replication, the standby rejected a write attempt while still
a replica, `pg_promote()` flipped it to writable, and a write against
the newly-promoted instance succeeded immediately - all without
restarting either Postgres container.

## JWT signing key rotation

The rotation procedure moved to the **Rotate the token signing key** guide ([`operate/rotate-signing-key.md`](marketing/guides/operate/rotate-signing-key.md)), which also covers the order to follow across several instances. For local development, `make rbac-rotate-key KID=<kid>` generates the new key into `certs/rbac-verification-keys/`.

**Engineering notes** kept here:

This procedure was verified live end to end: logged in under the
original (`"default"`) key, promoted a newly-generated key as the active
signer while keeping the old one in the verification set, confirmed the
pre-rotation token still verified (`GET /api/v1/me` → 200) and a freshly
issued token's header carried the new `kid`, then removed the old key
from the verification set and confirmed the pre-rotation token was then
rejected (`401 invalid or expired token`) - all against a real running
console instance, not a unit test.

## Certificate status reporting (CA-client credential)

How to issue, wire up, use and retire the CA-client credential, and what signing, revoking and cleaning do, moved to the **Manage node certificates** guide ([`nodes/certificates.md`](marketing/guides/nodes/certificates.md)); issuing it is in both install guides.

**Engineering notes** kept here:

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

What these are and how the Nodes page shows them moved to **Manage node certificates** ([`nodes/certificates.md`](marketing/guides/nodes/certificates.md)).

**Engineering notes** kept here:

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

What deleting a node does, and what it keeps, moved to the **Remove a node** guide ([`nodes/remove.md`](marketing/guides/nodes/remove.md)).

**Engineering notes** kept here:

`add-node-deletion` added a "Delete" action on the Nodes page. What it
actually does, and why, based on real findings against this project's
own running openvoxdb and CA - not assumed from PuppetDB's general
reputation:

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

Turning package reporting on and off for a node moved to the **Add nodes** guide ([`nodes/add.md`](marketing/guides/nodes/add.md)).

**Engineering notes** kept here:

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

Using the match-rule editor moved to the **Classify nodes** guide ([`use/classify.md`](marketing/guides/use/classify.md)).

**Engineering notes** kept here:

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

Enrolling a node with the install script, and what each outcome means, moved to the **Add nodes** guide ([`nodes/add.md`](marketing/guides/nodes/add.md)).

**Engineering notes** kept here:

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

Note that an unreachable CA is reported only after the wait expires, not
immediately - the agent keeps retrying for the whole window, which is
the right behavior for a transient network or DNS blip but does mean a
genuinely wrong address takes the full 5 minutes to report.

**Autosign in this dev environment:** `docker-compose.yml` sets
`AUTOSIGN` on openvoxserver, defaulting to `true` - which is what makes
`make openvox-test` and an unattended install script run work without a
human. To exercise the *not*-autosigned path, recreate the service with
it off:

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

The upgrade, the error it avoids, `pg_trgm`, and staying on 17 moved to the **Upgrade the container databases to Postgres 18** guide ([`operate/upgrade-postgres.md`](marketing/guides/operate/upgrade-postgres.md)).

**Engineering notes** kept here:

openvoxdb 8.15.0 is verified against PostgreSQL 18.6: all 62 schema
migrations apply and the service reaches `status=running`.

## Vulnerability tracking: providers, credentials, mirrors, and limits

Setting up providers, the secrets key, mirrors, coverage and OSV's limits moved to the **Track vulnerabilities** guide ([`use/vulnerabilities.md`](marketing/guides/use/vulnerabilities.md)).

**Engineering notes** kept here:

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
  credentials absent from every API response and log line. Behavior against a
  real tenant (export timings, rate limiting, data quirks) is untested.
- Not live: the Ubuntu import (~700 MB download) was only exercised with small
  fixture archives, not against the real directory.


## Tasks: the Puppet server credential, and where tasks actually run

How to run a task is in the **Run jobs** guide
([`use/run-jobs.md`](marketing/guides/use/run-jobs.md)); turning tasks on is
in both install guides.

**Engineering notes** kept here:

- **The credential is not the CA-client one, on purpose.** The console reads
  `/puppet/v3/environments` and `/puppet/v3/tasks` from openvoxserver with
  `CONSOLE_PUPPET_SERVER_*`, which default to the openvoxdb client certificate.
  openvoxserver's default `auth.conf` allows any CA-issued certificate on
  `/puppet/v3/tasks` and `/puppet/v3/file_content` (rules "puppet tasks
  information" and "puppetlabs file content"), so an ordinary certificate
  serves. The CA-client credential would also work, but carries `pp_cli_auth`
  - full CA admin - and reading tasks has no use for it. Keeping it in
  `internal/certstatus` alone is the point of that package.
- **Tasks never worked before `add-task-execution`.** `node-agent-client`
  used to run `puppet task run`, which the agent does not have (`Error:
  Unknown subcommand 'task'` on OpenVox 8.29.0). In Puppet Enterprise that
  work was `pxp-agent`'s; this project ships its own agent, so the agent now
  does it: download, verify by SHA-256, lay out an install directory as Bolt
  does, run with the input method, enforce a timeout on the whole process
  tree (process group on Unix, job object on Windows).
- **The console resolves, the node verifies.** The console fetches a task's
  metadata and file list (with checksums) from openvoxserver and sends that;
  the node downloads the files from *its own* Puppet server - the one
  `puppet config print server_list server serverport --section agent` names,
  often a compiler - and runs nothing on a checksum mismatch. The console
  vouches for content, never transports it.
- **Sensitive parameter values exist only in memory and on the wire.** They
  are redacted before the job row is written, and the real values held by
  the dispatching instance's `Dispatcher` until the job ends. That relies on
  the existing rule that a job is followed only by the instance that started
  it; if that instance stops, the reaper fails the job anyway.
- **Output is capped.** A reply is one NATS message (1 MB by default) and
  carries stdout twice, raw and as the result, so the agent keeps the first
  256 KiB of stdout and 64 KiB of stderr and says so in the output.
- **Run directories** live in `task-runs` beside the cache
  (`/opt/puppetlabs/node-agent/task-cache` on Linux and macOS), not in the
  temp directory, which is often mounted `noexec`.

## node-agent-client upgrades itself

`add-agent-auto-upgrade` exists because a production node ran a client
from before the package-inventory fix, kept the old fact script,
reported removed kernel packages as installed and showed 6,817
vulnerabilities on a patched host - and nothing said the client was out
of date. Nothing upgraded clients, and nothing could have: the packages
were versioned `0.0.0+<git sha>`, which no package manager can order.

**Build identity.** A client is identified by the SHA-256 of its own
executable, plus a version and build time stamped in by
`cmd/build-agent-binaries` (which `make agent-binaries` and the
Dockerfile now run instead of bare `go build`s). The same tool writes
`internal/agentdist/bin/builds.json`, which is how the console knows
what it serves. `node-agent-client --build-info` prints the three
values. A plain `go build` produces an unstamped client; a console
embedding unstamped clients records builds but upgrades nobody, and
logs a warning saying so at start.

**Package versions.** deb is `<base>+<YYYYMMDDHHMMSS>.<revision>`, rpm
is version `<base>`, release `<YYYYMMDDHHMMSS>.<revision>`, **epoch 1**.
`<base>` is the release tag, or `0.1.0` while there is none. The build
time leads, so a later build is always an upgrade. The rpm epoch is not
decoration: the old rpm Version was the bare sha, and a sha beginning
with a digit (`4a4a7ef`) sorts above any small version number under
rpm's rules, so only an epoch puts new packages above every old one.
`cmd/build-agent-packages/versions_test.go` holds both package
managers' comparison rules and asserts the ordering against the
versions already shipped.

**The exchange.** Two dispatch actions on the existing node transport:
`agent_status` (digest, build time, version, os, arch, install method,
whether upgrades are accepted) and `agent_upgrade` (target digest and
build time, plus the artifact's URL, size, SHA-256 and format). A client
from before this change answers `unsupported action`, which the console
records as an unknown build: out of date, and only fixable by re-running
the install script once. The artifact is downloaded over plain HTTP(S)
from `CONSOLE_BASE_URL` with TLS verification off on purpose - the
package routes are public, and nothing downloaded is trusted until it
matches the digest that came over the mutually authenticated transport.

**On the node.** The artifact is staged in a directory recreated private
to root/Administrators for each upgrade
(`/opt/puppetlabs/node-agent/upgrade`,
`%ProgramData%\PuppetLabs\node-agent\upgrade`; override with
`NODE_AGENT_UPGRADE_DIR`) and removed afterwards. After the size and
digest check, the client binary is unpacked from the package (in Go -
no `dpkg-deb`/`rpm2cpio` needed, which is why the packages pin gzip
payloads) and run with `--build-info`; it must report the requested
build. Then:

- **deb/rpm:** `dpkg -i` / `rpm -U --oldpackage --replacepkgs`, launched
  with `systemd-run --unit=node-agent-client-upgrade --collect`. It has
  to be its own unit: the package's install script restarts the service, and
  systemd kills everything in the service's cgroup, including a package
  manager the client started itself. `journalctl -u
  node-agent-client-upgrade` is where a failed install explains itself.
  Without systemd the install runs in a new session and stops the client
  afterwards for its supervisor to restart.
- **macOS:** the new binary is renamed over the installed one and the
  client exits; launchd's `KeepAlive` starts it.
- **Windows:** a running `.exe` cannot be replaced but can be renamed,
  so it is moved to `node-agent-client.exe.old` (deleted at the next
  start), the new one moved into place, and the client exits non-zero so
  the Service Control Manager's recovery action restarts it. `install.ps1`
  now sets those actions (`sc.exe failure`); a Windows node installed
  before this needs the script re-run for that as well.
- **A Linux binary no package owns** (a pre-packaging install) is
  refused with "re-run the install script".

**Package scripts on upgrade.** Both package managers run the old
package's removal script while upgrading, and rpm runs it *after* the
new package's `%post`. `postrm.sh` used to stop and disable the service
and delete its environment file every time it ran, so an rpm upgrade
ended with no client running. It now does that only on a real removal
(`remove`/`purge` from dpkg, `0` from rpm). The rpm also runs
`postinst.sh` as `%posttrans`, the last thing in the transaction,
instead of `%post`: that is what brings the service back when upgrading
*from* a package that still has the old removal script, which is every
rpm built before this. Found by upgrading a live Rocky node; the deb
was unaffected only because dpkg happens to run the two in the other
order.

A node opts out with `NODE_AGENT_ACCEPT_UPGRADES=false`. On Linux put it
in `/etc/node-agent-client/local.env`, a file the unit now reads if
present and the package never writes - `environment` beside it is
regenerated on every install.

**Reading a node's build.** A node's connection is announced before
its client has subscribed for requests, so the first status request can
find nobody listening. The coordinator retries for a few seconds and
then leaves it to the next sweep, rather than treating the node as gone.
It also asks every connected node when it starts, because nodes
reconnect within a second of a console restart - before the coordinator
is receiving connect notifications. A build that cannot be read is
logged (`could not read a node's client build`).

**A busy node.** A client executing a run or task refuses any other
request as busy, immediately, and the refused request is never run
later. (It used to wait in the client's queue until the work ended:
NATS delivers to a subscription one message at a time, and the client
did the work where the message was delivered. Each request now gets its
own goroutine.) The one exception is the build-status request, which
changes nothing and is answered whatever the client is doing - so the
console knows a busy node's build, and the request it sends on connect
cannot get a new node's first Puppet run refused. An automatic upgrade
refused as busy is tried again a minute later and is not counted as a
failure. A manual upgrade from the node page is refused with "the node
is busy"; try again when the job has finished.

**On the console.** `internal/agentupgrade` holds one row per node in
`node_agents` (migration `000028`). Out of date is computed when read,
not stored: an unknown build, or a build that differs from the
console's for that platform **and** was built earlier. Newer is never
out of date, so a cluster part-way through a deployment does not flap
clients and a rollback does not downgrade them. A coordinator runs
wherever the node listener does (`all` and `orchestrator` modes): it
asks each node for its build on connect and sweeps every minute.
Claiming a node is a conditional `UPDATE` under an advisory lock, which
is what keeps two instances from instructing one node and bounds the
fleet to 20 upgrades at a time. Busy nodes are retried in a minute and
not counted. Failures back off 5 minutes doubling to 6 hours and stop
after 5 attempts at one build; a different console build, or an
operator, starts again. An upgrade is only complete when the node
reports the new build - one that accepts and never does is failed after
10 minutes.

`CONSOLE_AGENT_AUTO_UPGRADE=false` stops the automatic upgrades and
nothing else. `POST /api/v1/nodes/{name}/agent-upgrade` (`nodes:manage`,
audited as `node.agent.upgrade_requested`) upgrades one node regardless,
answering 409 with a reason code when it cannot: `not-connected`,
`busy`, `current`, `in-progress`, `install-script`, `declined`,
`no-artifact`.

**There is no automatic rollback.** The `--build-info` check is the only
guard against a build that starts but then cannot connect. Such a build
stalls at 20 nodes, which then need the install script re-run against a
fixed console.

**What has and has not been verified.** Covered by tests: version
ordering, the package unpacking against real nfpm-built `.deb`/`.rpm`
files, every refusal and failure path on the client, the binary
replacement for both the macOS and Windows rules, the coordinator's
retry/backoff/exhaustion/concurrency behaviour, and one test that runs
the real client and the real coordinator against each other over a real
node transport (`TestEndToEnd_ConsoleUpgradesAnOutOfDateClient`).
**Not yet run on a real node of any platform:** the `systemd-run`
install on Debian and Rocky, the Postgres store (its tests need
`CONSOLE_TEST_POSTGRES_DSN`), and, as with the installers themselves,
anything on Windows or macOS. Do those before relying on this in
production - `tasks.md` in the change lists them.

## Node group scope: who sees which nodes

A role's permissions apply to the node groups it names, and to every
group beneath them. The user-facing procedure is in the site's
"Users, roles and tokens" guide; this is what an operator needs beyond
it.

**Upgrading changes nothing.** Migration `000030` grants the built-in
All Nodes group to every existing role, so each user sees what they saw
before. Access tokens issued by the previous build carry no scope and
are treated as fleet-wide until they expire (15 minutes at most);
refreshed tokens carry the root scope. Nothing is narrowed until an
administrator edits a role.

**Rolling back widens access.** The previous build ignores the scope in
a token and has no notion of a role's node groups, and the down
migration drops `role_node_groups`. Any role that was narrowed becomes
fleet-wide again. If roles have been narrowed, treat a rollback as an
access change: tell the people who rely on the restriction, or remove
the permissions from those roles first.

**During a rolling upgrade** an instance still on the old build serves
node data unfiltered. No role can be narrowed until an upgraded
instance is serving the Roles page, so the exposure is limited to the
rollout itself - but do not narrow a role until every instance is on
the new build.

**How long a change takes.** A change to a role's node groups reaches a
signed-in user at their next token refresh (15 minutes at most); revoke
their sessions to force it. A node that moves between groups shows up
for, or disappears from, scoped users within 30 seconds: each instance
caches "which nodes are in these groups" for that long, per distinct
set of groups. The cache is derived from Postgres and openvoxdb and
holds nothing that is lost with the instance.

**Cost.** A fleet-wide caller costs nothing extra: no lookup happens.
A scoped caller costs one read of the node groups and one fleet-wide
fact query to openvoxdb per cache miss, however many requests a page
makes.

**If openvoxdb is unreachable** a scoped caller's node-data requests
fail with 502 rather than fall back to unfiltered data. Fleet-wide
callers are unaffected by the scope lookup (their requests may still
fail for needing openvoxdb themselves).

**Actions are scoped too.** Runs, tasks, plans, the package reporting
toggle, client upgrades, node removal and certificate actions are
refused with a 403 naming the nodes unless every node is in the
caller's groups for that action's permission. A multi-node action is
refused whole. The check runs before openvoxdb, the CA or the node is
contacted, so a refused action leaves no trace but the 403.

**Upgrading from the read-only release narrows what narrowed roles can
do.** If you limited a role to some node groups before actions were
scoped, its members could still act fleet-wide; after this upgrade
they cannot. Before upgrading, look at each narrowed role's
`orchestrator:run`, `nodes:manage` and `nodes:certs:manage` and decide
whether that is what you want. Roles on All Nodes, and service tokens,
are unaffected.

**Jobs** are listed to a scoped caller when any of their nodes is in
scope, and show only those nodes. A job's stored status is not
recomputed per viewer, so it can read "failed" for a failure on a node
the viewer is not shown; the job page says nodes are hidden. Scope is
by current group membership: a removed node's job history is visible
to fleet-wide roles only.

**What is not scoped**, and so still names any node to anyone holding
the permission: the Activity page and the audit log, node group
definitions (including pinned node names), the ENC endpoint, and
service tokens, which are always fleet-wide. The upgrades the console
starts by itself are not a user's action and ignore scope.

**Nodes with no facts** - never run, or waiting for a certificate - are
in no group but All Nodes unless pinned, so only fleet-wide roles see
them. A team signs its own new nodes' certificates when the certname is
pinned into the team's group first (a group directly under All Nodes);
otherwise a fleet-wide role signs.

**Deleting a group a role is granted on** is refused with a 409. The
foreign key from `role_node_groups` enforces it in the database as
well, so a group cannot be removed out from under a role by any path.

## Class discovery and Puppet run options

Added by `add-class-discovery-and-run-options`.

**Class discovery**

- Source: `GET /puppet/v3/environment_classes?environment=<env>` on
  openvoxserver, with the Puppet server credential (the openvoxdb client
  certificate unless `CONSOLE_PUPPET_SERVER_CERT_FILE` names another). No new
  console setting.
- **openvoxserver refuses this endpoint by default** (403; confirmed on the
  dev stack). It needs one `auth.conf` rule allowing the console's
  certificate. The server image adds it on every start
  (`openvoxserver-init/20-allow-class-discovery.sh`, certificate name from
  `CLASS_DISCOVERY_CERTNAME`, default `console`); a package install adds it
  by hand - see the install guides.
- Symptoms and causes, as `GET /api/v1/classes` reports them in `reason`:
  `not_configured` (no `CONSOLE_PUPPET_SERVER_URL`), `forbidden` (the rule is
  missing or names another certificate), `unreachable`. In every case groups
  save and nodes classify as normal; only the picker and the marks are lost.
- Cache: per instance, in memory, five minutes. Expired on every instance
  when a deploy publishes `codemanager.deployed`. `POST
  /api/v1/classes/refresh` re-reads on the instance that serves it; the
  others follow within five minutes. A restart simply re-reads.
- **The server has its own environment cache.** With
  `environment_timeout = unlimited` (the container image's setting) the
  server keeps answering with the classes it first loaded until
  `DELETE /puppet-admin-api/v1/environment-cache` is called. The console does
  not call it. This is the same flush a catalog needs after new code, and
  until it happens "Refresh classes" re-reads the server's stale answer.
- The server sends no `ETag` unless its `environment-class-cache-enabled` is
  on, so each read is a full parse of the environment. One read per
  environment per five minutes per instance.
- A manifest that fails to parse is listed with its error, and suppresses
  "class not defined" marks for that environment: the class may be in it.

**Run options**

- `POST /api/v1/orchestrator/runs` takes `options` (`noop`, `environment`,
  `debug`). They are stored in the job's existing `params` and `environment`
  columns; there is no migration.
- A run with options is sent as `run_with_options`. A node agent client from
  before this change answers `unsupported action`, which the console records
  as "client is too old" on that node - and Puppet is not run there. Plain
  runs are unchanged for every client version.
- The client runs `puppet agent --onetime --no-daemonize --detailed-exitcodes
  [--noop] [--environment <env>] (--debug | --verbose) --color=false`.
  `--verbose` is what makes a no-op run print its would-be changes;
  `--color=false` because Puppet writes ANSI colour codes even to a pipe.
  Output is cut at 256 KB stdout, 64 KB stderr.
- The environment is checked against the server's list when the list can be
  read, and taken on trust when it cannot.
- **Environment is not an override.** A node whose classification sets an
  environment runs in that one. The job page compares the requested
  environment with the one in each node's report (one openvoxdb query per
  job view, only for jobs that requested an environment) and marks
  differences.
- Not covered: options on plan steps, on a new node's initial run, and on the
  run that follows a package-reporting toggle.

## Targets by query or group, and scheduled jobs

Added by `add-pql-targeting-and-scheduled-jobs`.

**Targets**

- A run or a task names its nodes as `targets` (a list), `query` (PQL) or
  `group` (a node group id). One resolver (`orchestrator.TargetResolver`)
  serves the HTTP handlers, the preview (`POST
  /api/v1/orchestrator/targets/resolve`) and scheduled firings.
- **Names are all or nothing; a query or a group is narrowed** to the
  caller's `orchestrator:run` scope, and nothing says what was left out. A
  query matching only other teams' nodes answers "no nodes matched", the
  same as one matching nothing.
- **A caller who is not fleet-wide may not send a subquery.** The check is a
  count of `{` outside string literals (more than one is a subquery), made
  before the query is sent. It is not a PQL parser. If PQL gains another way
  to read across entities, this check needs extending.
- A scoped caller's query still runs against the whole fleet in openvoxdb;
  only the result is narrowed. It is bounded at 30 seconds and 4,000
  characters. AST (JSON) queries are refused.
- A job stores where its targets came from (`jobs.target_source`) and the
  schedule that started it (`jobs.schedule_id`, `jobs.schedule_name`). The
  query text is withheld from a reader who is not shown every node the job
  ran on.

**Scheduled jobs**

- Tables `scheduled_jobs` and `scheduled_job_firings` (migration 31). No new
  setting, no new permission (`orchestrator:run` / `orchestrator:read`).
- Worker `orchestrator-scheduler`, in the `all` and `orchestrator` modes: it
  runs beside the dispatcher because the instance that fires a schedule
  dispatches its job. Every such instance polls every 20 seconds.
- **Not leased.** An instance takes a due time with a compare-and-set on
  `next_run_at`, in the same transaction that records the firing. Whichever
  instance's update changes the row fires it. Due-ness and lateness are
  judged by the database clock.
- **At most once.** The claim commits before the job is created. An instance
  that stops in between leaves the firing with outcome `firing`, shown as
  "interrupted" after ten minutes, and nobody retries it.
- **Late firings are missed, not run**: more than 15 minutes after the due
  time (`scheduler.LateLimit`, a constant). A repeating schedule records one
  missed firing and moves to its next time still ahead.
- A firing is the on-demand request made at that moment by the schedule's
  owner: same `orchestrator.Starter`, same checks. The owner's permission and
  node scope are read from the database then (`internal/app`'s
  `scheduleAuthority`), not from any token. An owner is a name and a kind
  (user or service token), because the two share a namespace.
- Saving a schedule's contents makes the saver its owner. Pause and resume do
  not.
- A firing is skipped, with the reason recorded on the schedule and in the
  activity log, when the owner has lost `orchestrator:run`, the target
  resolves to nothing or is now refused, the task or environment is gone,
  openvoxdb or the Puppet server cannot be reached, or the schedule's
  previous job is still running.
- **Sensitive task parameters** are sealed with `CONSOLE_SECRETS_KEY`
  (`internal/sealer`), bound to the schedule's id. No key: such a schedule is
  refused. Changed key: its firings are skipped until it is saved again.
  An `orchestrator`-mode instance needs the key, and
  `CONSOLE_PUPPET_SERVER_URL`, to fire scheduled tasks.
- Repeats are fixed intervals from the start time (a day is 24 hours), so a
  daily schedule shifts by an hour on a local clock across a daylight-saving
  change.
- Deleting a node group does not touch schedules that target it; they are
  skipped until changed.
- A user or service token deleted and recreated under the same name becomes
  the owner of schedules the earlier one saved, since ownership is by name.

**Found while building this, not changed by it**

- A job started through the API of a `web`-mode instance never completed:
  its targets stayed `pending` and the job `running` until the reaper failed
  it. `web` served the orchestrator routes but did not run
  `orchestrator-dispatcher`, the only consumer of dispatch outcomes.
  **Fixed since:** `web` now runs `orchestrator-dispatcher`, so every mode
  that can start a job follows it. Covered by
  `TestWebInstanceFollowsTheJobsItStarts` in `internal/app`.
- A `web`-mode instance reported every node as not connected, because the
  connection registry existed only beside a node listener, and an instance
  joining a running cluster knew nothing of the nodes already connected.
  **Fixed since:** see "Node transport connections" in the table at the top.
  Covered by `TestRegistryWithoutAListenerLearnsExistingConnections` in
  `internal/nodetransport`.

## Plans in the console, and the account policy

Added by `add-plans-ui-and-account-policy`.

**Plans**

- A plan request (`POST /api/v1/orchestrator/plans`) now goes through
  `orchestrator.Starter.StartPlan`, like runs and tasks. It takes `targets`,
  `query` or `group`, resolved once and used for every step. A step is
  `{kind: run, options}` or `{kind: task, taskName, environment, params,
  timeoutSeconds}`; 1 to 20 steps. Every step is checked before anything is
  created, and a refused step is named (`step`, counted from 1).
- **A failed step closes the rest.** `Store.FailPlan` fails the plan and, in
  the same transaction, every later step and its targets, with the reason
  `not run: step N failed`. Nothing is dispatched for them. Before this
  change those steps stayed `running` until the reaper.
- A step's state as read (`waiting`, `running`, `succeeded`, `failed`,
  `not_run`) is derived in `orchestrator.stepViews`; the stored status is
  still one of `running`, `succeeded`, `failed`. A step's job row is created
  with the plan, so a step's start time is the previous step's finish time.
- `GET /api/v1/orchestrator/jobs/{id}` returns `steps` for a plan and `plan`
  for a step. `GET /api/v1/orchestrator/jobs` lists only top-level jobs;
  `?parent=<id>` lists a plan's steps. A listed plan shows its first step's
  nodes.
- **Saved plans**: table `plans` (migration 32), package `internal/plans`,
  routes under `/api/v1/orchestrator/saved-plans`. Read with
  `orchestrator:read`, change with `orchestrator:run`. They are not limited
  by node scope: they name no nodes.
- A saved plan is a template. **There is no run-by-id route**: a client reads
  the plan and posts its steps to `/api/v1/orchestrator/plans`.
- **A saved plan never holds a sensitive task value.** On save the value is
  dropped and a missing sensitive parameter is not an error
  (`Starter.CheckPlanTemplate`). On read each task step lists `needs`, from
  the task's metadata at that moment, or `problem` if the task is gone.
- **A scheduled plan is a copy** (`scheduled_jobs.plan_name`, `steps`). It
  does not reference the saved plan. This is a security boundary: a schedule
  fires with its owner's access, and anyone with `orchestrator:run` can edit
  a saved plan. Sensitive values are sealed in the schedule's one sealed
  blob, keyed by step index; they are kept on a replace only for the same
  owner and only for a step whose task and environment are unchanged at the
  same position.
- The reaper's staleness limit counts from a job's start, and a plan's steps
  are created when the plan starts. A plan that runs longer than the limit
  (two hours) has its remaining steps failed by the reaper. Not changed here.
- A plan is followed by the instance that started it, `web` included (see
  the fixed `web`-mode fault in the previous section). Its task steps'
  resolved payloads live in that instance's memory, so a plan does not
  survive that instance stopping.

**Account policy**

- One row, `account_policy` (migration 33), read from the database each time
  a password is set or a login fails. No row means the defaults in
  `rbac.DefaultAccountPolicy`: minimum length 12, no required character
  kinds, lock for 15 minutes after 10 failures. No cache, so a change through
  one instance applies on all of them at once.
- Routes: `GET /api/v1/account-policy` (any authenticated caller, because
  the change-password form shows the rules), `PUT /api/v1/account-policy`
  and `POST /api/v1/users/{id}/unlock` (`rbac:admin`).
- **Password rules** (`rbac.CheckPassword`) apply in the create-user,
  administrator set-password and change-own-password handlers, and to the
  first-start administrator. `Store.CreateUser` itself does not check, so
  code that calls the store directly (tests, the OIDC provisioning path with
  its random password) is unaffected. Minimum length is counted in
  characters; the 72 maximum in bytes, which is bcrypt's limit.
- **First start**: a `CONSOLE_BOOTSTRAP_ADMIN_PASSWORD` that fails the policy
  creates no administrator and logs an error naming the rule. It is examined
  only while the users table is empty.
- **Lockout state is on the user row** (`users.failed_logins`,
  `users.locked_until`). `Store.RecordFailedLogin` is one statement judged by
  the database clock: concurrent failures are each counted, a failure while
  locked changes nothing, and the first failure after a lock has passed
  starts the count at one.
- **A locked account answers exactly as a wrong password does** (401,
  `invalid credentials`), and the bcrypt comparison is made either way. This
  keeps the existing rule that login does not reveal whether a username
  exists. The lock is visible to administrators (`lockedUntil` on a user) and
  in the audit log (`user.locked`, `user.login.locked`).
- Lockout covers password login and the current-password check of
  `PUT /api/v1/me` only. Token refresh, existing access tokens, OIDC login
  and service tokens are unaffected. An OIDC login does not clear a lock.
- **Known limit:** anyone who knows a username can keep that account locked
  by failing logins. The lock is temporary for that reason, and the threshold
  can be set to 0. There is no lockout by client address and no command-line
  unlock.
- Audit actions: `user.locked`, `user.unlocked`, `user.login.locked`,
  `user.password.rejected` (with the rule codes, never the password),
  `account_policy.updated` (before and after). Activity: `user.locked`,
  `user.unlocked`, `account_policy.updated`.

## Task permissions

Added by `add-task-permissions`.

- **Storage.** `roles.task_patterns` and `service_tokens.task_patterns`
  (migration 34), `TEXT[]`, empty by default. Empty means every task, which is
  what every row existing before the migration has. Patterns are a task name,
  `module::*` (which also allows the module's own task `module`) or `*`; at
  most 20 of at most 64 characters; validated in `internal/taskscope`, in the
  store as well as the handlers.
- **Pairing.** A caller's task grants are one `{tasks, groups}` pair per role
  that carries `orchestrator:run` (a role with an empty list contributes
  `{["*"], groups}`), built by `rbac.Store.UserTaskGrants` and written to the
  access token as `taskGrants`. A task may run on the union of the groups of
  the grants that match it. A service token carries one fleet-wide grant, or
  none. **No claim means no limit**: it is what a user none of whose roles has
  a list, a service token without one, and a token issued before the upgrade
  all carry. The issuer refuses an empty non-nil list, which JSON would drop
  and so read as unlimited.
- **Where it is enforced.** `orchestrator.Starter`, for `StartTask`,
  `CheckTask`, each task step of `StartPlan`/`CheckPlan`, and
  `CheckPlanTemplate` (saved plans; by name only, there being no target). The
  name is matched before the catalogue is read. A node list that names a node
  outside the task's nodes is refused (`403`, `outOfScope`); a query or group
  is narrowed, and a plan to the intersection over its task steps. A
  `StartRequest` with no `Tasks` set allows none: a caller that forgets to say
  fails closed.
- **Handlers** take `SetTaskScope`; the default allows every task so existing
  tests keep working. `TestTaskPermissions_EveryTaskHandlerIsLimitedOnEveryInstance`
  fails for any of the orchestrator, schedule, saved-plan and task catalogue
  handler sets left without one. The scheduler asks `scheduleAuthority` for the
  owner's node scope **and** task scope from their roles at each firing.
- **The catalogue** (`GET /api/v1/tasks`, `/tasks/{module}/{task}`) omits tasks
  the caller may not run on any node, and answers a hidden task as unknown. A
  caller without `orchestrator:run` is not limited.
- **Audit.** `task.denied` (category `orchestrator`; actor, task, and `step`
  for a plan; never parameters), written by the instance that answered the
  request. A refused save is not audited, and a refused scheduled firing is
  its firing record. Role updates carry `taskPatterns` before and after.
- **Token size.** About 1.3 KB per restricted role at the limits. A user with
  many restricted roles can approach an 8 KB header limit on a reverse proxy.
- **Takes effect** at the next token refresh (15 minutes) for people, and at
  the next firing for schedules.

## User groups

Added by `add-user-groups`.

- **Storage.** `user_groups` (unique on `lower(name)`), `user_group_members`
  and `user_group_roles` (migration 35), all cascading, and the view
  `user_effective_roles`: `user_roles` (direct assignments) `UNION` the roles of
  each group a user belongs to, so a role held twice is one row.
- **One source of truth.** `UserPermissions`, `UserNodeScopes`,
  `UserTaskGrants` and the role listing read the view, never `user_roles`, so
  the token, the schedule owner's authority at each firing and the Users page
  cannot disagree. `TestNothingReadsUserRolesDirectly` fails for any other
  production query on `user_roles`; only role assignment and OIDC
  reconciliation (which writes its own direct rows) may name it.
- **Pairing is untouched.** Each role is still one row joined to its own node
  groups and `task_patterns`; a group only adds rows.
- **API** (`rbac:admin`): `/api/v1/user-groups` (list, create), `/{id}` (read,
  rename, delete), `/{id}/members/{userId}` and `/{id}/roles/{roleId}` (POST,
  DELETE), `GET /api/v1/users/{id}/groups`. `GET /api/v1/users/{id}/roles`
  returns each role with `via`: `direct` and/or each granting group's name.
  Deleting a group with members answers `409` with `memberCount` unless
  `?confirm=true`.
- **Last administrator.** Deleting a group, revoking a role from it or removing
  a member is refused (`409`, `reason: last-admin`) when it would leave no user
  with `rbac:admin`. The direct assignment and user/role deletion paths are
  unchanged.
- **Audit** (category `rbac`): `group.created`, `group.renamed`,
  `group.deleted` (with the member count), `group.member.added`/`removed` and
  `group.role.granted`/`revoked`; never a password or token.
- **Takes effect** at the next token refresh (15 minutes) for people, and at
  the next firing for schedules. Out of scope: LDAP, SAML or OIDC group
  mapping (OIDC still assigns roles directly), and nested groups.

## Configuration data

Added by `add-config-data`.

- **Storage.** `node_group_config_data (group_id, key, value jsonb)`
  (migration 36), cascading with the group. Keys, counts and sizes are
  validated in `internal/classifier` (`ValidateConfigData`): up to 1000
  keys of up to 255 characters from `[A-Za-z0-9_:.-]`, values up to 64 KiB
  encoded, and `lookup_options` refused.
- **Merge.** `Tree.Classify` folds each matching group's data in
  `mergeOrder`, as it does class parameters: a later (winning) group
  replaces a key **whole**, with no deep merge.
- **API.** `configData` on a group (read and write). An update that does not
  send it keeps what is stored (a nil map); `{}` clears it. A refusal is
  `400` with `fields.configData`. `GET /api/v1/enc/{certname}/data`
  (`enc:read`) returns the node's merged data, `{}` when there is none.
- **Audit.** The group audit entries (`group.created`, `.updated`,
  `.deleted`) never carry a configuration value: they record
  `configDataKeys` and, on an update, `configDataChange` (added, changed,
  removed key names). `auditedGroup` hides `ConfigData` from the log.
- **Delivery.** `openvoxserver-init/console_config_data.rb` is a Hiera
  `data_hash` function that runs `enc-bridge --data <certname>` (the program
  from puppet.conf's `external_nodes`; it prints the data route's answer as
  JSON) once per compile. It does not call the console itself, because
  openvoxserver does not give its environment (`ENC_BRIDGE_URL`, the token) to
  Ruby code, only to the programs it runs. A console that cannot answer
  fails the compile.
  `openvoxserver-init/30-configure-config-data.sh` runs on every start and
  - copies the function into
    `jruby-gems/gems/openvox-<version>/lib/puppet/functions` (found by
    glob). This is the **only** place the server loads a global-layer
    function from; `vendor_ruby`, the plugin cache lib and the server's var
    dir lib were tried and do not work. A recreate loses it, a restart does
    not, hence every start;
  - writes `/etc/puppetlabs/puppet/hiera.yaml` with the console entry when
    there is none, and rewrites it only while its first line is `# Managed
    by OpenVox Console`. A global `hiera.yaml` you wrote is never edited:
    the script logs the entry to add. Remove the marker line from ours to
    take it over.
- **Precedence.** The global layer is consulted first, so console data beats
  the environment's Hiera data; keys it does not set fall through.
- **Checking it.** `puppet lookup` on the server does not exercise this path
  (no puppetdb terminus, a different load path). Run a real agent against a
  manifest that calls `lookup()`.

## Patch inventory

Added by `add-patch-inventory`.

- **What it is.** A read-only picture of the updates each Linux node is
  missing. Nothing installs, upgrades or removes a package: `apt-get -s`
  is a simulation, and dnf/yum only list.
- **Collection.** A new node-agent action, `patch_scan`
  (`internal/nodeagent/patchscan.go`). apt: `apt-get update`, then
  `apt-get -s dist-upgrade`, reading the `Inst` lines that have an
  installed version; a line whose origin names `security` is a security
  update; `/var/run/reboot-required` is the reboot flag. dnf/yum:
  `check-update --refresh` (exit 100 means updates), `updateinfo list
  security` for the security set (best effort), `needs-restarting -r` for the
  reboot flag. `refresh=false` skips the list refresh. A node with none
  of apt, dnf or yum answers `unsupported`.
  An agent that predates the action answers `unsupported action
  "patch_scan"`, which the console records as an agent too old to scan.
- **Not orchestrator jobs.** A scan is a short dispatch whose result is a
  snapshot, so it follows what package-inventory reporting does: dispatched
  directly from the API (`internal/patchinventory`), `orchestrator:run`,
  node scope checked before anything is sent. Week-by-week history is not
  kept: only the latest scan per node.
- **Storage.** `node_patch_scans` and `node_patch_updates` (migration 37),
  replaced in one transaction per scan and read in one snapshot-isolated
  transaction, so a reader never sees a summary that disagrees with its
  list. `scanned_at` is the last success, `attempted_at` the last try; a
  failure sets `attempted_at` and `error` and keeps the last good updates.
  `patch_scan_settings` is one row: `enabled`, `interval_minutes` (15 to
  10080).
- **Permissions.** Reads `nodes:read`, scan `orchestrator:run`, the
  interval `nodes:manage`. Each is scoped by the permission that gates it
  (the node-scope sweep covers the reads and the single-node scan; a group
  scan skips out-of-scope nodes and counts them, tested in
  `internal/patchinventory`).
- **Periodic scan.** Worker `patch-scanner` (modes `all` and `worker`),
  leased under `patch-scan`, ticking each minute. It scans connected nodes
  whose last attempt is at least the interval old, 8 at a time. A node that
  is offline records no attempt, so it is scanned on the pass after it
  reconnects; a failed attempt does count, so a broken node is retried once
  per interval. A pass logs one summary line and writes no audit entries.
  Once a day it purges snapshots of nodes not attempted for 90 days.
- **Staleness** is worked out when read: a successful scan older than twice
  the interval (a day with scanning off).
- **Limits.** One scan runs up to five minutes. A node that is mid-install
  holds the package manager lock and fails that scan; the next pass tries
  again. The security flag is only as good as the repository metadata.

## Patch runs

Added by `add-patch-runs`.

- **A job kind.** `patch` in `jobs.kind` (migration 38), with one
  `job_targets` row per node, numbered by `batch`. Options are the job's
  `params`: `scope` (`security`, `all`, `packages`), `packages`, `reboot`,
  `dryRun`, `batchSize` (1 to 100, default 10) and `stopAfterFailures` (0 to
  1000, default 3, 0 never). Per-package results are the target's `result`.
  Started with `POST /api/v1/orchestrator/patches` (`orchestrator:run`,
  through the same target resolution and node scope as a run; a refused
  option is a 400 naming `field`, a node in another patch run a 409).
- **Why not one request.** A dispatch is one request and reply, capped at an
  hour by `dispatchTimeout` and two by the node's reply window. A patch can
  outlast both, so it is start-and-poll: the agent action `patch_apply`
  answers at once and the run carries on in the background with the agent
  busy; `patch_status`, answered even while busy, reports `running`, `done`,
  `failed` or `interrupted`. The agent keeps the last status in
  `patch-run.json` beside the task cache, so a run an agent restart cut short
  reads as interrupted.
- **The coordinator** (`internal/orchestrator.PatchCoordinator`) runs
  in the instance that started the run, with the dispatcher worker. It starts
  each batch, polls each node every 15 seconds (30 second timeout), and
  records each answer as a heartbeat on the target and the job. A node
  silent for 10 minutes, or still running after 4 hours, is failed. It is
  held in memory, like a task's payload: if the instance stops, nothing
  follows the run.
- **The reaper's second rule.** A `patch` job is failed when its
  `heartbeat_at` is 10 minutes old, together with its pending and running
  targets, reason "progress stopped being reported". Every other kind keeps
  the rule that fails a job two hours after its `started_at`. A run that was
  failed this way may have installed some packages: read the node's status
  with a scan.
- **What runs on the node.** apt: `DEBIAN_FRONTEND=noninteractive apt-get -y
  -q -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold`,
  with `dist-upgrade` for all, or `install --only-upgrade <names>` for
  security and named packages (the names come from the scan's own
  simulation). dnf/yum: `upgrade -y`, `--security`, or the names. A dry run
  runs only the scan. Package names are checked on both ends against a
  strict pattern so one can never be read as an option.
- **Inventory.** The agent returns its post-run scan in the final status and
  the console stores it. After a reboot (the agent waits 30 seconds, then
  `systemctl reboot`) the node is scanned again once it reconnects, for up to
  10 minutes; otherwise its snapshot records the failed scan.
- **Audit.** `patch.triggered` (options and node count in `After`) and
  `patch.succeeded` / `patch.failed`, plus activity entries.
- **Limits.** The agent must be built from this version; an older one fails
  the node as too old. One run per node at a time (checked under an advisory
  lock when the run is created, and by the agent being busy). No schedules,
  windows or rollback.

## Agentless SSH targets

Added by `add-agentless-ssh-targets`.

- **What it is.** A host with no node agent that the console reaches over SSH
  (`internal/agentless`) and runs tasks on. Not Puppet runs, patch runs or
  plan run steps: `orchestrator.Starter` refuses them naming the target
  (`AgentlessTargetsError`), through every entry (HTTP, schedules, plans).
- **Storage.** `agentless_targets` and `agentless_credentials` (migration 39).
  A target has its own login or a `credential_id`; its own wins. Secrets
  (`sealed_private_key`, `sealed_passphrase`, `sealed_password`) are
  `internal/sealer` envelopes with associated data
  `agentless:<table>:<id>:<field>`, so a ciphertext moved to another row will
  not open. Reads return `hasKey`, `hasPassword` and a public key fingerprint,
  never material. Without `CONSOLE_SECRETS_KEY_FILE` a credentialed target
  cannot be created, and one that exists cannot be used.
- **Routing.** The orchestrator's dispatcher sends through a
  `RoutedTransport`: a name in `agentless.Directory` (the target names, cached
  for 5 seconds and cleared by a change on that instance) goes to the SSH
  `Executor`, everything else to the node transport. The executor answers in
  the JSON an agent would, so results, plans and schedules need no special case.
- **Host keys.** `pinned_key` is trusted; `proposed_key` is what the last
  check saw. A target with no pin is never logged in to: the key is recorded
  as proposed and the connection dropped, and a task fails as "not trusted".
  A pinned target presenting another key is refused before authentication and
  flagged `key_changed`; only an explicit replace (audited with both
  fingerprints) clears it. Changing address or port clears the pin.
- **Running a task.** The console downloads the task's files from the Puppet
  server with its own client certificate (`taskcatalog.Client.File`, checksum
  and size verified, cached by SHA-256 for a short while) before touching the
  host. On the host: `mktemp -d`, copy each file with `cat >` over the
  session (no SFTP needed), run, `rm -rf`. Environment parameters are sent
  ahead of the task's own stdin as shell assignments, which a bootstrap reads
  with `dd bs=1 count=N` and `eval`s, so no value is on a command line.
  Run-as is `sudo -n -u <user> --` around the bootstrap. The shared pure logic
  (implementation choice, input method, output parsing and caps) is
  `internal/taskexec`, used by the node agent too.
- **Limits.** 15 s to connect, a task's own timeout, output capped as an
  agent's, one task per target (a second is "busy"), 20 concurrent sessions
  console-wide (the rest wait). A task that outlives its timeout is sent
  SIGKILL on its session; a process that ignores a closed session may outlive
  it, and its directory is removed regardless.
- **Permissions.** New: `agentless:manage` (create, change, delete, trust
  keys, shared credentials). Reads are `nodes:read`; check and tasks are
  `orchestrator:run`. All are node-scoped; a target outside the caller's
  scope reads as one that does not exist, a new name must be inside their
  scope, and shared credentials need a caller whose permission covers every
  node. The node-scope sweep covers the routes.
- **Not included.** Windows/WinRM, jump hosts, SSH certificates, Puppet runs,
  patch scan/apply over SSH, facts.

## Code plans

Added by `add-code-plans`.

- **What it is.** Plans in environment code, run by the console itself
  (`internal/codeplans`): a subset of Bolt's YAML plan format with a Go
  interpreter. Not the Puppet language.
- **Where the files are.** `Loader` reads `<codedir>/environments/<env>` -
  `CONSOLE_CODE_DIR_PATH`, the directory the code manager deploys to - using
  the `modulepath` from `environment.conf` (default `modules`; `$` variables and
  absolute entries are not followed). Plans are `<module>/plans/<name>.yaml`
  (`init.yaml` is the module's name). Every path is resolved through
  symlinks (an environment is a symlink to a deploy) and must stay inside the
  environment; files over 256 KiB are refused. An instance with no code
  directory answers "not available", not "no plans". The Puppet Server's plans
  API lists plans but does not return a YAML plan's steps, so it is not used.
  Only the instance holding the code can run plans.
- **The subset.** Top level `description`, `parameters`, `steps`, `return`.
  Steps: `task`, `plan` or `message`, with `name`, `description`, `targets`
  (task) and `parameters`. `command`, `script`, `upload`, `download`,
  `resources` and `eval` are refused at load, naming the step. Values are
  references (`$a.b[0]`, `${a.b}` in text, `$$`) to parameters and earlier
  steps - never code. Parameter types use `taskcatalog.ParamType`, the
  checker task parameters are validated with, plus `TargetSpec` and
  `Sensitive`. Numbers are `json.Number` throughout so `Integer` and `Float` stay
  distinct.
- **Running.** `Coordinator` (like the patch coordinator, on the instance that
  started the run, under the dispatcher worker) checks the request - plan and
  every plan it calls load, no cycle, depth at most 5, parameters bind, target
  parameters resolve within the caller's scope, each task step's task exists
  and is allowed by name - then records a `codeplan` job and follows it. Each
  task step is started through `Starter.StartTask` with the caller's captured
  node scope and task scope, as a child job (`parent_job_id`, `step_order`), and
  polled every 500 ms; so every step is checked when it is reached. Failure of
  any target fails the plan; remaining steps are `not_run`. Nested plans run in
  the same job and trace (`path` `1.2`, `depth` 1), their task steps children of
  the outer plan job. `Dispatcher.advancePlan` is skipped for a code plan's
  children.
- **The record.** `jobs.kind = codeplan` (migration 40): `plan_name`,
  `environment`, `params` (`{parameters, sha256}`, sensitive redacted),
  `plan_trace`, `result`, `error`. A reader limited to some nodes finds the
  job through the steps they may see and, when others are hidden, gets no
  message, result, error or parameters.
- **Heartbeat.** The reaper judges any job with a `heartbeat_at` by silence
  (10 minutes) and any without by age (2 hours): patch runs and code plans
  report; everything else does not. A plan that fails this way may have started
  tasks that carry on.
- **Sensitive.** A `Sensitive[T]` parameter is a wrapper that prints as
  `[redacted]` (also in JSON), is unwrapped only to build a task's parameters,
  and may be passed to another plan only into a `Sensitive` parameter. A task
  that prints its own sensitive parameter is recorded as it printed it.
- **Limits.** 200 steps, depth 5, 24 hours, 1000 targets a step, 256 KiB a file.
- **Audit.** `codeplan.triggered` (plan, environment, sha256, parameter
  *names*) and `codeplan.succeeded` / `codeplan.failed`, plus activity entries.
- **Not included.** Scheduling a code plan, Puppet-language plans, parallel
  steps, error catching, retries.
