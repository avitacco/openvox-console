## Why

The install scripts built in `add-multi-platform-agent-install` and
`add-package-inventory-reporting` have grown into a hand-rolled package
manager written in bash/PowerShell - and we just hit a real bug that a
real package manager would never have had: overwriting a currently-running
`node-agent-client` binary in place fails with `curl: (23) client returned
ERROR on write` (Linux `ETXTBSY`) or a locked-file error (Windows), because
`curl -o`/`Invoke-WebRequest` don't know how to safely swap a running
service's executable. The fix required a manual temp-file-plus-rename
dance and a Windows stop-before-download reordering - workarounds for a
problem `dpkg`/`rpm`/`msiexec` solve correctly by default. This project
already trusts real package managers for `openvoxagent` itself
(`apt.voxpupuli.org`, `yum.voxpupuli.org`); `node-agent-client` only got
the script treatment because the console builds it itself and no
packaging pipeline existed yet for it.

## What Changes

- Explore replacing the current curl-and-shell-script delivery of
  `node-agent-client` with native OS packages that carry their own
  install/upgrade logic, so problems like the ETXTBSY bug are handled by
  the packaging format itself rather than hand-written script logic.
- Research (not assume) what's realistic to build in this project's
  toolchain: a Go-native or shelled-out `.deb`/`.rpm` builder, an `.msi`
  builder, and a macOS `.pkg` builder, invoked at console build time
  (alongside the existing `agent-binaries` Makefile target).
- Research whether a self-hosted apt/yum repository needs GPG signing to
  be usable, and how lightweight such a repo's metadata (`Packages.gz`,
  `repodata`) can realistically be for a single internal package.
- If native packaging proves realistic, replace the `install.sh`/
  `install.ps1`/`install-macos.sh` templates' node-agent-client delivery
  step with: Linux nodes adding the console's package repo and running
  `apt-get install`/`yum install`; Windows/macOS nodes downloading and
  running an `.msi`/`.pkg` the same way they already do for openvoxagent's
  own installer.
- If research instead shows this is materially more complexity than this
  project's actual scale justifies (e.g. no viable pure-Go `.deb`/`.rpm`
  builder, forcing a dependency on `dpkg-deb`/`rpmbuild` being present on
  the console's own build machine), **document that finding plainly in
  design.md and stop there** - this proposal is exploratory, not a
  committed direction. A lighter alternative (keeping the binary+script
  delivery but moving the systemd-unit/facts.d/service-registration logic
  into `node-agent-client` itself via a `-install` flag, so the shell
  script shrinks to "download binary, run `node-agent-client -install`")
  should be recorded as the fallback if full native packaging isn't
  justified.
- **BREAKING** (contingent on the research above panning out): the
  node-agent-client artifact served by the console changes from a raw
  binary to a package format per OS, and the install scripts' node-agent-
  client step changes shape accordingly. openvoxagent's own installation
  path is unaffected.

## Capabilities

### New Capabilities

(none - this reshapes how an existing capability is delivered rather than
introducing new observable behavior)

### Modified Capabilities

- `agent-distribution`: the node-agent-client artifact the system serves
  changes from a platform-matched raw binary to a platform-matched native
  package (`.deb`/`.rpm`/`.msi`/`.pkg`), and installing/upgrading it must
  succeed even when the currently-installed node-agent-client is actively
  running as a service - closing the gap that caused the ETXTBSY-class
  bug.

## Impact

- `internal/agentdist/` (`agentdist.go`, `handlers.go`,
  `handlers_test.go`, all three `install_script*.go` templates) - the
  node-agent-client delivery step in each script and the binary-serving
  handler both change shape.
- Build tooling: a new build-time step (likely a new Makefile target
  alongside `agent-binaries`) that produces the packages, plus whatever
  new dependency that requires (a Go library, or `dpkg-deb`/`rpmbuild`/
  `pkgbuild`/an MSI-building tool as a build-machine prerequisite - to be
  determined by design.md's research).
  - Self-hosted repository serving (Linux only) if that's the direction
    research supports: new routes and repo metadata generation in
    `internal/agentdist`.
- `operations.md`: the "Multi-platform agent install" and "Real-node
  install bugs found via an actual VM" sections need updating once a
  direction is chosen, since they currently describe the script-based
  approach this proposal may replace.
- No change to `package-inventory`'s requirements - the `_puppet_inventory_1`
  external fact mechanism is unaffected regardless of how
  node-agent-client itself gets delivered; only the file's delivery
  packaging would move from a script-written heredoc to a package-managed
  file.
