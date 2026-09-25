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
| Node transport connections | `internal/nodetransport.Registry` | **Yes, when clustered.** A node's connection is still a single TCP connection terminated at one instance, but with the instances peered (`CONSOLE_CLUSTER_*`) a dispatch published by any routed instance - including one that terminates no node connections, like `web` - is routed to whichever one holds that connection, and the connection registry reflects connects and disconnects cluster-wide. Unclustered, this remains instance-local: only the instance a node connected to can reach it. |
| Orchestrator in-flight dispatch tracking | `internal/orchestrator.Dispatcher.pending` | **Instance-local, but no longer load-bearing.** A dispatch's response still returns to the instance that published it, so that instance tracks it to completion. What changed is that *any* instance can publish the dispatch, so triggering a run no longer depends on reaching a particular one. If the dispatching instance stops before a job reaches a terminal state, the stale-job reaper (`worker`/`all` mode, leased) records that job as failed rather than leaving it "running" forever. |

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

