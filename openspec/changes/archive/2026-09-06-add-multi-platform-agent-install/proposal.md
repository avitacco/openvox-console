## Why

`agent-distribution`'s install script and node-agent binary matrix
currently only support Debian/Ubuntu-family Linux (`apt-get`) - a
deliberate, explicitly-flagged v1 scope limit from when this capability
was first built, not a final state. OpenVox itself publishes `openvox-agent` packages for
Debian family (apt), RedHat family (yum/dnf), Windows, and macOS - a
console that can only onboard Debian/Ubuntu nodes can't manage a
realistic mixed fleet. This is also a prerequisite for a planned
follow-up change (package-inventory data collection via a script the
install process drops on the node), which needs an install path to
exist on every platform it should eventually cover.

## What Changes

- Widen `internal/agentdist`'s install script to support RedHat-family
  Linux (yum/dnf) in addition to Debian/Ubuntu (apt-get), each branch
  installing `openvoxagent` via that family's normal package source and
  a systemd unit for `node-agent-client`, as today.
- Add Windows and macOS support: a separate install script per OS
  (PowerShell for Windows; a POSIX shell script for macOS, distinct
  from the Linux one due to launchd instead of systemd), each serving
  the platform-appropriate `openvoxagent` package and a node-agent-client
  build for that OS/architecture.
- Cross-compile and embed `node-agent-client` for `windows/amd64` and
  `darwin/{amd64,arm64}`, alongside the existing `linux/{amd64,arm64}`
  - five supported platforms total. Windows is amd64-only: OpenVox
  publishes no Windows ARM64 `openvox-agent` package at all (confirmed
  live against `downloads.voxpupuli.org/windows/openvox8/` - only
  `-x64.msi` builds exist across every published version), so a
  `windows/arm64` node-agent-client build would have no matching
  openvoxagent to pair with.
- **BREAKING (internal, not user-facing)**: `cmd/node-agent-client`
  gains real Windows Service Control Manager integration (start/stop/
  shutdown handling via the SCM, not just OS signals) so the installed
  binary behaves as a properly supervised Windows service, matching
  systemd's auto-restart/start-on-boot behavior. macOS needs no
  equivalent binary change - launchd supervises any executable via a
  plist, the same way systemd does today, with no special code required
  in the binary itself.
- Each platform's install script registers/starts `node-agent-client`
  under that OS's native service manager: systemd (Linux, existing),
  Windows Service via the SCM (new), launchd via a plist (new, macOS).

## Capabilities

### New Capabilities
(none)

### Modified Capabilities
- `agent-distribution`: widens "Install script route" and "Platform-
  matched binary" to cover RedHat/Windows/macOS in addition to Debian/
  Ubuntu, and adds a requirement that each platform's service
  registration uses that OS's native service manager.

## Impact

- **Code**: `internal/agentdist` (per-OS install script templates and
  routing, widened `supportedPlatforms`), `cmd/node-agent-client`
  (Windows SCM integration, Linux/macOS unaffected), `Makefile`'s
  `agent-binaries` target (four new cross-compile targets).
- **New dependency**: a Windows service-control library for Go (e.g.
  `golang.org/x/sys/windows/svc`) - decided in design.md.
- **Binary size**: the console binary embeds five `node-agent-client`
  builds instead of two - see design.md's Risks for the size trade-off
  already anticipated (but deferred) when this capability was first
  scoped to two platforms.
- **Not addressed by this change**: code-signing/notarization for the
  Windows and macOS binaries - both platforms' OS-level security
  features (SmartScreen, Gatekeeper) will very likely warn on or block
  an unsigned binary fetched over the network. Flagged as a real risk
  in design.md, not solved here (it requires a paid code-signing
  identity this project doesn't have).
