## Context

See proposal.md - Why. The Linux facts are two short shell scripts
embedded in `internal/nodeagent/factassets/` and written to
`facts.d/package_inventory.sh` by `handlePackageInventorySet` only when
reporting is toggled on. openvoxdb's facts terminus strips the
`_puppet_inventory_1` fact out and stores it as `package_inventory`
rows of `[name, version, provider]` - the tuple has no room for extra
fields, and any other top-level key the script emits is stored as an
ordinary fact.

## Goals / Non-Goals

**Goals:**
- Versions and names that distribution security data can be matched
  against, without changing `_puppet_inventory_1`'s shape.
- A way for consumers to tell corrected data from old data per node.
- Nodes already reporting pick up the fix on agent upgrade alone.

**Non-Goals:**
- Architecture. Multi-arch installs of the same package at the same
  version already collapse into one tuple; that is unchanged and does
  not affect distribution advisory matching, which is not arch-specific.
- Windows and macOS facts.
- Reporting the RPM source package. RHEL-family security data (Red Hat,
  AlmaLinux, Rocky OSV records) is keyed by binary package name -
  confirmed live - so it isn't needed.

## Decisions

**RPM: epoch folded into the version string.** Query format becomes
`%{NAME}\t%|EPOCH?{%{EPOCH}:}|%{VERSION}-%{RELEASE}\n`. `epoch:version-release`
is RPM's own canonical EVR form and what distribution advisories use,
so no new field is needed. Emitting `0:` for epoch-less packages was
rejected: it would change every RPM version string in the fleet (and
break every existing exact-version search) for no matching benefit,
since a missing epoch and epoch 0 compare equal.

**apt: source package in a companion fact, not in the tuple.** The
tuple keeps binary name and binary version, so the fleet-wide package
search still finds `libssl3` by the name administrators see in `dpkg -l`.
The companion fact is:

```json
{"console_package_inventory": {"format": 2,
  "sources": {"libssl3": ["openssl", "3.0.11-1~deb12u1"]}}}
```

`sources` lists only binaries whose source name or version differs
from their own (from `dpkg-query -W -f='${Package}\t${Version}\t${source:Package}\t${source:Version}\n'`,
supported since dpkg 1.16.2); a binary absent from `sources` is its own
source. Alternatives rejected:
- *Replace the tuple name with the source name* - breaks package search
  by the name users know, and collapses distinct binaries.
- *Encode source in the version string* - pollutes a field other tools
  read and makes exact-version search unusable.
- *List every binary in `sources`* - roughly doubles fact size for no
  information gain.

**One companion fact for both families, carrying `format`.** RPM's
companion fact is `{"format": 2}` alone. Format `1` is implicit (fact
absent). Consumers - `add-vulnerability-tracking` in particular - use
it to report a node as needing an agent upgrade rather than silently
matching lossy data. A single fact name rather than per-family names
keeps the consumer's lookup one query.

**JSON built with awk, no escaping added.** Debian package names are
restricted to `[a-z0-9+.-]`, RPM names and EVRs contain no `"` or `\`,
and Debian versions to `[A-Za-z0-9.+~:-]`, so string values cannot break
the JSON - the same assumption the current scripts already make. Unit
tests assert the output parses as JSON for representative inputs.

**Refresh at client start, not on toggle or on a timer.** A new
`nodeagent` function compares the on-disk script with the embedded one
(byte equality) and rewrites it if different, called from
`cmd/node-agent-client` before connecting. Start is the one moment a
new client version is guaranteed to run; a package upgrade restarts the
service. Refreshing inside the existing toggle handler was rejected
because a node nobody toggles again would never be fixed. No Puppet run
is triggered - the next scheduled run picks up the new fact, and an
unrequested run on every agent upgrade across a fleet would be a
surprise load spike.

**Console reads the companion fact separately.** `internal/openvoxdb`
gains a single-fact lookup (`facts { certname = ... and name =
"console_package_inventory" }`); `internal/packageinventory` merges
`sources` into the node package list response as optional
`sourcePackage`/`sourceVersion` fields (present on every apt entry when
the fact exists: either the mapped source or the binary's own
name/version; absent otherwise).

## Risks / Trade-offs

- [Epoch-bearing RPM exact-version searches stop matching the old
  string] → Called out as BREAKING in proposal.md; searches by name are
  unaffected, and the old string was not the real version.
- [Nodes whose agent is not upgraded keep format-1 data] → The `format`
  marker makes that visible to consumers instead of silent.
- [The companion fact is stored per node in openvoxdb] → Only changes
  when installed packages change; typically a few kilobytes on Debian
  (source-differs entries are a minority).

## Migration Plan

Ships in the node-agent-client packages; nodes pick it up on their
normal package upgrade, which restarts the service and triggers the
refresh. No console-side migration. Rollback: downgrading the client
rewrites the old script on its next start, since the refresh compares
for inequality, not newer-ness.
