## Why

The node agent reports packages that are not installed on the node, so the
console matches vulnerability advisories against software that is not
present. On a production fleet of three Ubuntu 24.04 nodes this produced
roughly 3,192 false "fix available" findings - 100% of that fleet's fixable
findings, 128 pages of the vulnerability UI - every one of them against
kernel packages that had been removed months earlier and were not on disk.
An operator acting on that list has no way to tell the false findings from
the real ones, which makes the vulnerability feature untrustworthy rather
than merely noisy.

## What Changes

- The apt package-inventory fact reports only packages that are actually
  installed. `dpkg-query -W` lists packages in dpkg's `config-files` state
  (shown as `rc` by `dpkg -l`: removed, but configuration files retained)
  alongside installed ones, and the fact currently emits them as though
  they were installed.
- The same filtering applies to both outputs the apt fact produces, since
  one pass feeds both: `_puppet_inventory_1.packages` (which becomes
  openvoxdb's package inventory, and is what the vulnerability engine
  actually matches against) and `console_package_inventory.sources`.
- The rpm package-inventory fact excludes RPM's `gpg-pubkey-*`
  pseudo-package entries, which `rpm -qa` reports once repository signing
  keys have been imported. These are signing keys, not software. They do
  not produce false vulnerability findings, because no advisory names such
  a package, but they are incorrect inventory data and appear in the node
  package list.
- No change to RPM install-state handling. RPM has no analogue of dpkg's
  `config-files` state; erasing a package removes its rpmdb entry outright,
  so the apt defect does not exist there.

This is not a breaking change. The fact format version is unchanged and the
shape of the reported data is identical; only rows that never should have
been present are removed.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `package-inventory`: gains a new requirement defining what qualifies as
  installed at collection time. The existing "Node package list"
  requirement already promises to report "every package openvoxdb knows to
  be installed", which is precisely the claim this defect violates, but it
  never says what counts as installed. None of its existing scenarios
  change, so the definition arrives as a new requirement rather than a
  rewrite of that one. It covers the apt removed-but-not-purged case, the
  RPM signing-key case, and the guarantee that a held package is still
  reported and still assessed.

## Impact

Affected code:

- `internal/nodeagent/factassets/package_inventory_apt.sh` - filter on
  install status; the existing awk field indices shift accordingly,
  including the multi-arch dedupe key.
- `internal/nodeagent/factassets/package_inventory_rpm.sh` - exclude
  pseudo-package entries.

Affected downstream behaviour:

- openvoxdb's `package_inventory`, read fleet-wide by the openvoxdb
  client's `FleetPackages`, is the vulnerability engine's matching input;
  it becomes correspondingly smaller and accurate.
- The node package list shows only real packages.
- Vulnerability findings raised against packages that were never installed
  stop being produced.

Convergence, not an instant fix: existing false findings persist until each
node re-reports the corrected fact and the engine re-evaluates. The agent's
existing "Keeps an enabled package-inventory fact current" behaviour
delivers the corrected script to nodes that already report package
inventory when the client is upgraded, without enabling reporting anywhere
it is currently disabled and without triggering a Puppet run; the node's
next scheduled Puppet run then publishes the corrected data. No node-agent
requirement changes as a result.

Operator note: on an affected node the stale dpkg entries themselves are
cleared with `apt purge '~c'`. `apt autoremove` does not clear them,
because they are already removed - only their configuration files remain.
