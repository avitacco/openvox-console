## Context

See `proposal.md` - Why for the motivating bug (curl `ETXTBSY`-class
failure when the install script overwrites a running node-agent-client).
This section covers the research the proposal explicitly asked for
before committing to a mechanism: what's actually realistic to build,
per platform, in this project's Go/Linux-only build environment (no
Windows or macOS build machine, no code-signing infrastructure of any
kind - see `operations.md`'s existing documented limitations).

**Linux packaging (`.deb`/`.rpm`)**: buildable entirely in Go via
`github.com/goreleaser/nfpm` - a pure-Go package builder (no `dpkg-deb`,
`rpmbuild`, or `fpm` system dependency) already widely used for exactly
this purpose (building Linux packages from a Go project's own CI without
installing the target distro's packaging toolchain). This removes the
main risk the proposal flagged - "no viable pure-Go builder, forcing a
system-tool dependency on the build machine" - for Linux specifically.

**Windows packaging (`.msi`)**: also buildable from Linux without a
Windows host, via `wixl` (part of the `msitools` package, a Linux-native
reimplementation of the WiX toolset's linker). This is real but far less
established than nfpm, and Windows can't be live-verified in this dev
environment regardless (no Windows host - `add-multi-platform-agent-install`
already hit this limitation for the current script and documented it).

**macOS packaging (`.pkg`)**: Apple's installer package format
(`pkgbuild`/`productbuild`) has no practical Linux-hosted equivalent,
and - more importantly - an unsigned, unnotarized `.pkg` triggers the
same Gatekeeper friction an unsigned raw binary does. Building one
properly requires a real Mac and an Apple Developer Program enrollment
for code signing and notarization, neither of which this project has.
The current `install-macos.sh` doesn't build a `.pkg` for openvoxagent
either - it mounts OpenVox's own pre-built, pre-signed `.dmg`.

## Goals / Non-Goals

**Goals:**
- Close the "install script re-implements what a package manager already
  does correctly" gap on the platform where it's realistic to do so.
- Keep the fix proportional to actual risk and this project's real build
  constraints, rather than uniformly applying one mechanism to all three
  OSes regardless of fit.

**Non-Goals:**
- Code-signing or notarizing anything for Windows or macOS - out of
  reach without infrastructure this project doesn't have (a Windows or
  macOS build/signing machine).
- Building a public, internet-facing package repository - the repo this
  proposal describes is served by the console itself, for its own
  fleet, the same trust boundary the install script already lives in.
- Replacing openvoxagent's own installation path - unaffected either
  way; it already uses real upstream package repos.

## Decisions

**Linux: adopt native `.deb`/`.rpm` packaging via `nfpm`.** This is
where the motivating bug actually lives (systemd + a script-driven
`curl -o` onto a running binary), where live verification is actually
possible (the project's existing Docker-based test containers), and
where the tooling risk the proposal worried about turns out not to
apply. `nfpm` takes a small declarative package spec (name, version,
files, systemd unit, facts.d script, maintainer scripts) and emits both
`.deb` and `.rpm` from the same input - one new build-time step, not
two.

**Linux repo metadata: hand-roll it in Go rather than shelling out to
`dpkg-scanpackages`/`createrepo`.** This repo only ever serves one
package (node-agent-client, one file per arch), so the metadata format
- an apt `Packages` file (name/version/architecture/filename/SHA256
stanzas) and a yum `repomd.xml`/`primary.xml` - is simple enough to
generate directly and deterministically at the point the console
serves it, without needing `dpkg-dev`/`createrepo` present anywhere.
This keeps the "no new system-tool dependency" property `nfpm` already
bought us for the packages themselves.

**Linux repo signing: skip it for now, mark the repo `trusted`/
`gpgcheck=0`.** This is a private repo the console serves to its own
enrolled fleet, not a public distribution channel - the same trust
boundary the install script itself already crosses (a node fetching
`install.sh` over HTTP from its own console has already trusted that
console completely). An unsigned repo with real per-file SHA256
checksums in its metadata is a strict improvement over today's `curl -o`
with no integrity checking at all. GPG signing can be added later
without a breaking change if a stronger threat model emerges.

**Windows and macOS: do not pursue native packaging now.** For Windows,
`wixl`/MSI is technically reachable from Linux but is a materially less
proven path than `nfpm`, and doesn't clearly beat the fix already
shipped for the running-service case (stop-service-before-download) -
Windows Installer's own service handling would be a nicer-to-have, not
a bug fix, at that point. For macOS, a legitimate `.pkg` requires
infrastructure (a Mac, an Apple Developer account, notarization) this
project has never had and this proposal doesn't create. Recommendation:
keep both platforms on the current binary + script approach (already
fixed for the running-service bug in the temp-file/rename and
stop-before-download changes made just before this proposal), and
revisit only if this project gains real Windows/macOS build or
signing infrastructure.

**Console-specific config still flows through the install script, not the
package.** `ConsoleBaseURL` and `TransportAddr` are runtime config (read
from `.env` at console startup - see `cmd/console/main.go`), not known
at `make agent-packages` build time, so they can't be baked into a
package built once and embedded. The (still per-request-rendered, still
console-specific) install script writes a one-line
`/etc/node-agent-client/console.env` with the transport address just
before invoking the package manager; the package's postinst script
reads that file and independently re-derives everything node-specific
(cert paths via `puppet config print`) itself, on every install *and*
every upgrade - which is also what makes upgrades self-healing if
`puppet config print`'s answers ever change, for free.

**Package binary path: `/usr/bin/node-agent-client`.** Settling one of
the two items design.md's own Open Questions section left open below in
favor of the conventional FHS path for a system-package-managed
service binary, rather than reusing the old script-era `/opt/openvox-
console/bin/` path (which stays meaningful only as "the thing the
migration step removes").

**Migrating existing script-installed nodes**: see Migration Plan below
- this needs explicit handling, not an assumption that `apt-get install`
will cleanly take over a path a shell script already wrote to.

## Risks / Trade-offs

- [`nfpm`/`wixl` becomes unmaintained or changes its API] → Both are
  narrow, single-purpose build-time dependencies (not runtime
  dependencies of the console or the node), so a breaking change only
  affects the release pipeline, not running installations; pinned like
  any other Go module dependency.
- [Splitting the approach by platform (native package for Linux, script
  for Windows/macOS) adds asymmetry future contributors have to
  remember] → Documented explicitly here and in `operations.md`, same
  as this project's existing precedent of documenting asymmetric
  platform support (e.g. Windows ARM64 not being supported at all).
- [An unsigned internal repo could be a weaker link if the console
  itself were ever compromised] → No weaker than the install script
  itself already is today (both assume the console is trusted); not a
  new attack surface, just a differently-shaped one.
- [Existing enrolled nodes have the old hand-written systemd unit and
  raw binary in place already] → See Migration Plan.

## Migration Plan

Nodes already enrolled via the current script have:
`/opt/openvox-console/bin/node-agent-client` (raw binary, not
package-tracked) and `/etc/systemd/system/node-agent-client.service`
(hand-written unit file, also not package-tracked). A `.deb`/`.rpm`
installing files at those same paths would either silently diverge from
what dpkg/rpm believe they own, or fail outright (dpkg refuses to
overwrite a file it doesn't already track unless the new package
declares it via `Conffiles`/replaces metadata).

Plan: the package installs its systemd unit at the conventional
package-owned path (`/usr/lib/systemd/system/node-agent-client.service`,
not `/etc/systemd/system/`) and its binary at a package-owned path
under `/opt/openvox-console/bin/` (or a versioned path, TBD in tasks).
The install script's Linux branch, on first run under this new scheme,
detects and removes the old hand-written `/etc/systemd/system/node-
agent-client.service` and stops the old unmanaged binary's process
*before* adding the repo and installing the package - a one-time cleanup
step, not a permanent script responsibility. After that one-time
migration, all future upgrades are pure `apt-get install --only-upgrade`/
`yum update`, no script-side file surgery at all.

Rollback: if the packaged install needs to be reverted, `apt-get remove
node-agent-client` / `yum remove node-agent-client` cleanly removes the
package-tracked files and stops the service - no different from
removing any other package.

## Open Questions

- Package naming (`node-agent-client` vs. a vendor-prefixed name
  matching `openvox-agent`'s own convention) - cosmetic, doesn't affect
  the approach or specs; settled as `node-agent-client` during
  implementation for consistency with the existing binary/route names.
- Whether the console should keep serving the raw binary at its current
  URL alongside the new package repo (for scripting/debugging
  convenience) or retire it entirely - doesn't change fleet-facing
  behavior either way; kept during implementation (harmless, and other
  tooling may still want a raw binary), retiring it is a non-breaking
  follow-up if it turns out to be dead weight.
