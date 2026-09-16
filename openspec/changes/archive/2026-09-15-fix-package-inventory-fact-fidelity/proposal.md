## Why

The Linux package-inventory facts discard information that any
version-accurate consumer of package data needs. Confirmed live against
the OSV API while researching vulnerability tracking:

- The RPM fact reports `%{VERSION}-%{RELEASE}` with no epoch. AlmaLinux 9
  `openssl` reported without its `1:` epoch matched 15 advisories; with
  the epoch, 10 - the 5 extra were false positives, because a missing
  epoch orders as older than every fixed version.
- The apt fact reports only the binary package name. Debian and Ubuntu
  security data is keyed by *source* package: `openssl` at a given
  version matched 48 advisories, `libssl3` (a binary built from it) at
  the same version matched 0 - every library package would be a false
  negative.

`add-vulnerability-tracking` depends on this data being right, and the
existing fleet-wide package search benefits on its own (an RPM version
without its epoch is not the version the package manager itself
reports). It is split out as its own change because it is small, ships
independently, and is useful even if vulnerability tracking never lands.

A second gap makes the fix not reach existing nodes by itself:
`node-agent-client` writes the fact script only when reporting is
toggled on, so a node that already reports package inventory would keep
running the old, lossy script after its agent is upgraded.

## What Changes

- The RPM package-inventory fact reports each package's version in
  `epoch:version-release` form when the package has an epoch, and
  `version-release` (unchanged) when it does not.
- Both Linux package-inventory facts additionally emit a companion fact
  carrying a package-inventory format version, so a consumer can tell
  data produced by a corrected script from data produced by an old one
  (the `_puppet_inventory_1` tuples alone cannot show that).
- For apt, that companion fact also maps each binary package to the
  source package (name and version) it was built from, for packages
  where either differs from the binary package's own name or version.
  The `_puppet_inventory_1` tuples themselves are unchanged for apt -
  binary name, binary version, `apt`.
- The node package list API includes, for apt packages, the source
  package name and version when the companion fact has them.
- `node-agent-client` keeps an already-enabled package-inventory fact
  current: on start, if the fact script is present but its content
  differs from the version embedded in the running agent, it rewrites
  it in place (without triggering a Puppet run, and without enabling
  reporting on a node where it is disabled).
- Windows (`Get-Package`) and macOS (`pkgutil`) facts are unchanged.
- **BREAKING** (minor, data-visible): fleet-wide package search by exact
  version for an RPM package that has an epoch now matches the
  `epoch:version-release` string, not the previous epoch-less one. The
  epoch-less string was never the package manager's real version, so a
  search for it was already subtly wrong.

## Capabilities

### New Capabilities
(none)

### Modified Capabilities
- `package-inventory`: "Node package list" gains source-package
  attribution for apt packages and states the version format for RPM
  packages with an epoch.
- `node-agent`: adds a requirement that the agent refreshes a stale,
  already-enabled package-inventory fact script on start.

## Impact

- **Code**: `internal/nodeagent/factassets/package_inventory_rpm.sh`,
  `package_inventory_apt.sh`, `internal/nodeagent` (start-up refresh),
  `cmd/node-agent-client` (invoke the refresh), `internal/openvoxdb`
  (read the companion fact alongside `package_inventory`),
  `internal/packageinventory` (response fields), `frontend/src/node.js`
  (show source package where present).
- **No openvoxdb or openvox-server change**: the companion fact is an
  ordinary fact; `_puppet_inventory_1`'s shape is untouched.
- **Fact volume**: the companion fact only lists packages whose source
  name or version differs from the binary's, and only changes when
  packages change, so it does not add per-run churn to openvoxdb.
- **Live verification**: Debian-family and RedHat-family containers,
  same pattern as `add-package-inventory-reporting`.
