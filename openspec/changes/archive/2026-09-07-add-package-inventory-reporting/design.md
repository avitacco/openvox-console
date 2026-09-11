## Context

Confirmed live (see proposal.md) that openvoxdb's facts terminus
(`puppet/lib/puppet/indirector/facts/puppetdb.rb`, part of the vendored
PDB/openvoxdb code this project's `openvoxserver` already runs) already
does everything needed server-side: it looks for `facts.values['_puppet_inventory_1']`,
and if present and shaped as a hash, extracts its `'packages'` key and
forwards it separately as `package_inventory` in the facts-upload
payload - independent of the `store_report` command-version gap that
blocks `corrective_change` (see `operations.md`), since this rides on
facts upload, not report submission. Nothing server-side needs to
change; this change is entirely about getting a real `_puppet_inventory_1`
fact onto each node.

Confirmed via research: Facter's **external facts** mechanism (an
executable script in a fixed per-OS directory, output parsed as
JSON/YAML) is untouched by Puppet's pluginsync, unlike a plain Ruby
custom fact placed in the pluginsync-managed lib directory (which
pluginsync purges on the node's next real Puppet run if it isn't
sourced from an actual module - confirmed via Puppet's own pluginsync
documentation). This makes external facts the only mechanism that
survives being dropped once by an install script and run outside of any
control-repo module.

External facts directories (confirmed via Puppet's own docs):
`/opt/puppetlabs/facter/facts.d/` (Linux and macOS, AIO path) and
`C:\ProgramData\PuppetLabs\facter\facts.d\` (Windows, AIO path).
Unix executable facts need only a shebang and the executable bit -
any filename works. Windows requires a recognized extension
(`.ps1`/`.bat`/`.cmd`/`.exe`/`.com`) - `.ps1` is a natural fit here
since `install.ps1` is already PowerShell.

## Goals / Non-Goals

**Goals:**
- Every node onboarded through this project's install scripts reports
  real installed-package data on its next Puppet run, no separate
  opt-in step.
- Match Puppet's own idiomatic package-provider categorization per
  platform where there's a clear existing convention to follow (e.g.
  macOS's `pkgutil`-based receipts are what Puppet's own macOS package
  provider already inspects).

**Non-Goals:**
- Any control-repo/module-based delivery mechanism (Puppet Enterprise's
  own approach) - this project delivers everything through its own
  install scripts, which already run as root/Administrator on the node;
  no module/Puppetfile involvement needed or wanted.
- Live-testing Windows or macOS end to end - no real host for either
  exists in this project's dev environment (already documented,
  unchanged by this proposal). Verified statically only, same bar as
  `add-multi-platform-agent-install`.
- Deduplicating or version-comparing packages across multi-arch
  installs (e.g. the same Debian package installed for two
  architectures) - out of scope; report exactly what the platform's
  own package-manager query returns.
- Any change to how the `packages`/`package_inventory` PQL entities
  work, or to `internal/openvoxdb`/`internal/packageinventory` - those
  already work correctly against whatever data reaches openvoxdb; this
  change only makes real data reach it.

## Decisions

**Per-platform enumeration, following each platform's most idiomatic
source given what's already installed and what a real Puppet package
provider would itself inspect:**

- **Debian/RedHat (`install.sh`)**: branch the same way the
  package-manager install step already does. Debian:
  `dpkg-query -W -f='${Package}\t${Version}\n'`, provider `"apt"`.
  RedHat: `rpm -qa --queryformat '%{NAME}\t%{VERSION}-%{RELEASE}\n'`,
  provider `"rpm"`. Both are the same tools already relied on
  elsewhere in this script (`dpkg`/`rpm` are always present wherever
  `apt-get`/`yum`/`dnf` are); no new dependency.
- **Windows (`install.ps1`)**: `Get-Package | Select-Object Name,
  Version, ProviderName`, converted to the `[name, version, provider]`
  tuple shape via `ConvertTo-Json`. `Get-Package` (from the built-in
  `PackageManagement` module) already reports each package's real
  installing provider (`msi`, `Programs`, etc.) rather than assuming
  one, unlike Debian/RedHat where the provider is fixed by which branch
  ran.
- **macOS (`install-macos.sh`)**: `pkgutil --pkgs` (installed package
  receipt IDs) + `pkgutil --pkg-info <id>` per ID for its version,
  provider `"pkgutil"`. Chosen over assuming Homebrew is installed
  (it frequently isn't, and this project makes no Homebrew assumption
  anywhere else) - `pkgutil` receipts are also what Puppet's own macOS
  package provider (`pkgdmg`) already inspects under the hood, so this
  matches the platform's existing Puppet-idiomatic package concept
  rather than inventing a new one.

**Each install script emits one executable `package_inventory.sh`/
`.ps1` file** (matching its own scripting language - bash for
install.sh/install-macos.sh, PowerShell for install.ps1) that computes
the platform's package list *at Facter-run time*, not once at install
time - so the reported inventory stays current on every subsequent
Puppet run, not just a one-time snapshot from when the node was
onboarded. Output format on all three platforms:
```json
{"_puppet_inventory_1": {"packages": [["name", "version", "provider"], ...]}}
```

**No marker-file/confine gating** (unlike Puppet Enterprise's own
`_puppet_inventory_1` fact, which checks for
`/opt/puppetlabs/puppet/cache/state/package_inventory_enabled` before
resolving - see the earlier research this proposal is built on): that
gate exists so a control-repo module can be rolled out to every node in
a group while individual nodes opt in separately. This project has no
such rollout path - the install script running on a given node already
*is* the per-node opt-in - so the dropped fact script just always
computes and emits the list, with no separate enable step to forget.

## Risks / Trade-offs

- **`Get-Package`'s exact output/performance depends on the Windows
  version and installed `PackageManagement` module version** - cannot
  be verified live (no Windows host in this dev environment). →
  Mitigation: real syntax verified via `pwsh` + `Get-Package`'s own
  documented parameter names (task list includes a syntax check, same
  as `install.ps1`'s own prior verification in
  `add-multi-platform-agent-install`); if a real Windows node ever hits
  a version-specific quirk, that's a fast, isolated fix to one file.
- **`pkgutil --pkg-info` run once per installed package can be slow on
  a node with many receipts** (a subprocess per package). → Accepted:
  this only runs once per Puppet run (typically every 30 minutes), and
  macOS package-receipt counts are typically in the tens to low
  hundreds, not thousands - not worth the complexity of a faster
  batched approach unless real usage shows otherwise.
- **A node's package inventory can go stale between Puppet runs** (a
  package installed/removed outside Puppet won't show until the next
  run recomputes the fact) - inherent to any fact-based approach, not
  specific to this design; consistent with how every other fact
  (including this project's already-shipped fleet-status/report data)
  already works.
