## Why

`add-package-inventory` built the console-side reporting (query methods,
API endpoints, node-detail and fleet-wide UI) for installed-package data,
but nothing populates it - confirmed live, `package_inventory {}` still
returns `[]` against a real managed node added through this project's
own install scripts. Traced the actual mechanism (confirmed by reading
openvoxdb's own facts terminus source,
`puppet/lib/puppet/indirector/facts/puppetdb.rb`): it looks for a fact
named exactly `_puppet_inventory_1`, shaped as `{"packages": [[name,
version, provider], ...]}`, strips it out of the regular fact set, and
forwards it to PDB as `package_inventory` via the facts-upload command -
a facts-side gap only, nothing server-side needs to change. This is a
planned follow-up to `add-multi-platform-agent-install`, which widened
the install scripts specifically so this had a delivery point on every
supported platform first.

## What Changes

- Each of the three install scripts (`install.sh`, `install.ps1`,
  `install-macos.sh`) additionally drops a Facter *external fact* -an
  executable script in the platform's `facts.d` directory, gathering
  that platform's installed packages and emitting the `_puppet_inventory_1`
  JSON shape on stdout - so every node onboarded through this project's
  own install scripts reports real package data on its very next Puppet
  run, with no separate opt-in step.
- Per-platform package enumeration (exact mechanism decided in
  design.md): Debian/RedHat via their native package-manager query
  tools, Windows via `Get-Package`, macOS via `pkgutil`.
- Deliberately **not** a plain Ruby custom fact dropped into Puppet's
  pluginsync-managed lib directory - confirmed via research that
  pluginsync purges anything there not sourced from a real module, so a
  one-time install-script file drop would be wiped on the node's very
  first real Puppet run. `facts.d` is untouched by pluginsync, which is
  exactly why it's used here instead.
- No marker-file/opt-in gating (unlike Puppet Enterprise's own
  equivalent, which needs one since it's rolled out via a control-repo
  module across a whole node group): the install script running on a
  given node *is* the per-node opt-in signal here, so the dropped script
  can unconditionally compute and emit the package list every time
  Facter runs.

## Capabilities

### New Capabilities
(none)

### Modified Capabilities
- `agent-distribution`: extends "Install script route" so each script
  also sets up package-inventory reporting for its platform, in addition
  to what it already installs.

## Impact

- **Code**: `internal/agentdist` (each install script template gains a
  facts.d file-drop step; no new routes, no new Go dependencies).
- **No server-side change**: openvoxdb already accepts and stores this
  data via its existing facts-upload path (confirmed working - this
  change only makes nodes actually send it).
- **Live verification**: Debian and RedHat can be verified for real
  against this project's existing Docker-based test containers (same
  pattern as `add-multi-platform-agent-install`) - confirming a real
  `package_inventory` row appears in openvoxdb, not just that the query
  entity exists. Windows and macOS remain static-verification-only, per
  this project's already-documented limitation (no real hosts for
  either in this dev environment).
