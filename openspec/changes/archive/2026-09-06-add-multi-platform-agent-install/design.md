## Context

`internal/agentdist` today serves exactly one install script
(`GET /packages/install.sh`, bash) that only handles Debian/Ubuntu
(`apt-get`) and only registers `node-agent-client` under systemd - see
`install_script.go`. `supportedPlatforms` in `agentdist.go` only lists
`linux/amd64` and `linux/arm64`; `Makefile`'s `agent-binaries` target
only cross-compiles those two. `cmd/node-agent-client/main.go` is a
plain `func main()` that loads config, builds a client, and runs until
`os.Interrupt`/`SIGTERM` - no OS-service-manager integration beyond
what systemd needs (just a normal foreground process it can supervise).

Confirmed live (see proposal.md) what OpenVox itself actually ships:
Debian-family via `apt.voxpupuli.org`, RedHat-family via
`yum.voxpupuli.org` (release package pattern
`openvox8-release-el-<version>.noarch.rpm`, confirmed for EL), Windows
via `downloads.voxpupuli.org/windows/openvox8/` (x64-only MSI,
`openvox-agent-<version>-x64.msi` - no ARM64 build exists), and macOS
via `downloads.voxpupuli.org/mac/openvox8/` (both `arm64` and
`x86_64` `.dmg` builds, organized by macOS major version).

This project's dev/test environment (`docker-compose.yml`, `make
openvox-test`) is Linux-container-only - there is no Windows or macOS
host or CI target anywhere in this repo. This shapes what "verified"
can mean for two of the three new operating systems - see Risks.

## Goals / Non-Goals

**Goals:**
- One install path per OS that gets both `openvoxagent` and a matching
  `node-agent-client` build onto a node, registered under that OS's
  native service manager.
- `node-agent-client` behaves as a properly supervised service on every
  platform (restart-on-crash, start-on-boot), not just Linux.

**Non-Goals:**
- Code-signing or notarizing the Windows/macOS `node-agent-client`
  binaries - both platforms will very likely warn on or block an
  unsigned binary fetched over the network (Windows SmartScreen, macOS
  Gatekeeper). Fixing this needs a paid code-signing identity this
  project doesn't have; out of scope here. Document the caveat instead.
- Windows ARM64 support - no upstream `openvoxagent` package exists for
  it (confirmed live), so there's nothing for a `windows/arm64`
  node-agent-client build to pair with. Revisit only if OpenVox
  publishes one.
- Automated end-to-end live verification on real Windows/macOS hosts -
  this repo's dev environment is Linux-container-only. See Risks for
  what verification actually means for those two platforms in this
  change.
- Any change to the package-inventory data-collection idea from the
  prior conversation - that's a separate, later change this one is a
  prerequisite for, not part of this one.

## Decisions

**Separate install routes per OS, not one route with OS sniffing.**
`GET /packages/install.sh` (existing route, Linux only - now branches
Debian/RedHat internally by reading `/etc/os-release`'s `ID`/`ID_LIKE`),
`GET /packages/install.ps1` (new, Windows, PowerShell), `GET
/packages/install-macos.sh` (new, macOS - POSIX shell, but a distinct
script from Linux's since the package format, service manager, and
install commands are all different). Matches how OpenVox's own docs
present distinct per-OS install commands rather than one script that
detects everything - an HTTP GET has no reliable way to know the
client's OS beyond an easily-wrong `User-Agent` sniff, so the operator
picks the right route the same way they'd pick the right `curl`/`iwr`
command from documentation.

**Linux: detect Debian vs. RedHat family from `/etc/os-release`,
mirroring the existing Debian-only logic's shape.** RedHat-family
branch downloads `openvox8-release-el-<major-version>.noarch.rpm` from
`yum.voxpupuli.org` (confirmed pattern) for `ID`/`ID_LIKE` values
`rhel`/`centos`/`rocky`/`almalinux` (mapped to the `el` family); `yum
install -y openvoxagent` (or `dnf` if present) after. Exact family-name
mapping (`el` vs. `fedora` vs. `amzn`) needs a final check against
`yum.voxpupuli.org`'s real index during implementation - the `el`
family covers this project's own dev/test needs (RHEL-alikes), the
others are extensions of the same pattern.

**Windows: `golang.org/x/sys/windows/svc` for real SCM integration,
install script uses `New-Service` (built into PowerShell, no extra
tooling) to register it.** `cmd/node-agent-client/main.go`'s bootstrap
logic (config load, client construction, run-until-cancelled) moves
into a shared `run(ctx context.Context) error`; a new
`service_windows.go` (`//go:build windows`) checks
`svc.IsWindowsService()` and, if true, implements a minimal
`svc.Handler` that calls `run` with a context cancelled on the SCM's
Stop/Shutdown request; a `service_other.go` (`//go:build !windows`)
stub keeps non-Windows builds free of the new dependency. Windows'
`install.ps1` installs the MSI silently (`msiexec /i ... /quiet`), then
`New-Service` pointing at the installed `node-agent-client.exe` with
`-StartupType Automatic`, then starts it. Alternative considered: a
third-party service wrapper (e.g. `kardianos/service`) instead of the
lower-level `x/sys/windows/svc` - rejected, since this project already
favors minimal dependencies wherever the stdlib-adjacent `x/...`
packages suffice (e.g. `golang.org/x/sys` itself may already be an
indirect dependency via other tooling), and the actual integration
surface here is small (start/stop only, no pause/continue needed).

**macOS: no `cmd/node-agent-client` code changes at all.** launchd
supervises any executable via a plist the same way systemd supervises
any executable via a unit file - no SCM-style handshake needed.
`install-macos.sh` mounts the `.dmg` (`hdiutil attach`), runs the
bundled `.pkg` (`installer -pkg .../*.pkg -target /`), unmounts
(`hdiutil detach`), writes a
`/Library/LaunchDaemons/org.voxpupuli.node-agent-client.plist` with
`RunAtLoad`/`KeepAlive` set and the transport/cert env vars under
`EnvironmentVariables`, then `launchctl bootstrap system <plist>` +
`launchctl enable system/org.voxpupuli.node-agent-client` (the modern
launchd command pair; `launchctl load -w` is the older equivalent still
common in examples but deprecated).

**Binary size: accept the growth, still embed all five builds at
console build time.** Five small Go binaries (no heavy dependencies
beyond the NATS client, `x/sys/windows/svc` on the Windows build only)
adds tens of MB to the console binary - a real but bounded, one-time
cost, consistent with the "revisit if the list grows significantly"
note left in the original two-platform design. Fetching binaries
on-demand from an external source instead was considered and rejected
for the same reason it was rejected originally: "binary reflects the
running console build" (existing requirement) is only true if there's
no separately-versioned artifact to drift out of sync.

## Risks / Trade-offs

- **This repo's dev environment cannot run Windows or macOS at all** -
  no host, no CI target, nothing. → Mitigation: verification for those
  two platforms in this change means static review, `shellcheck`
  against `install-macos.sh` (available in this environment,
  confirmed), and as-careful-as-possible manual reasoning against real
  OpenVox/Microsoft/Apple documentation for `install.ps1` - not a real
  end-to-end run. `pwsh` isn't available in this dev container either
  (confirmed) - if it can be installed cheaply during implementation
  (e.g. via Microsoft's apt repo) it should be, purely for syntax
  linting; if not, say so plainly in tasks.md rather than implying a
  verification that didn't happen. This is a materially weaker
  verification bar than every other capability in this project, which
  has all been live-verified against a real running stack - worth
  flagging to the user again before archiving this change.
- **Unsigned binaries will likely trigger OS security warnings**
  (SmartScreen, Gatekeeper) that a real operator has to click through -
  see Non-Goals. → Mitigation: document this plainly in the install
  scripts' own output/comments and in `operations.md`, so it reads as
  an expected step, not a broken installer.
- **The RedHat family-name mapping (`el`/`fedora`/`amzn`) is inferred
  from one confirmed example (`el`), not exhaustively verified against
  every family `yum.voxpupuli.org` hosts.** → Mitigation: verify the
  real index during implementation before hardcoding the mapping table;
  narrow scope to `el` alone if the others can't be confirmed live,
  documenting that narrowing rather than guessing at unverified
  filenames.
- **`golang.org/x/sys/windows/svc`'s `Execute` callback runs on every
  SCM control request for the life of the service** - a bug here could
  leave the service unresponsive to Stop requests (stuck until forcibly
  killed), a failure mode systemd's simpler process model doesn't have.
  → Mitigation: keep the handler minimal (translate Stop/Shutdown into
  context cancellation, nothing else) and verify manually against the
  package's own documented usage pattern; this can't be live-tested in
  this environment (see above), so extra care in review substitutes for
  the verification this project would normally do by actually running
  it.
