## Context

See proposal.md - Why, for motivation, and
`specs/package-inventory/spec.md` for the required behaviour.

Two embedded shell scripts produce the package-inventory external fact,
selected by the node's package manager. The apt script matters most: a
single `dpkg-query` invocation feeds one awk pass that emits *both*
outputs - `_puppet_inventory_1.packages`, which becomes openvoxdb's
package inventory and is what the vulnerability engine matches against,
and `console_package_inventory.sources`, the binary-to-source map. Any
filtering therefore has to happen once, before that aggregation, or the
two outputs disagree.

Constraints that shape the approach:

- The fact is a self-contained script embedded in the client binary and
  written to the node's external facts directory. It runs on every Puppet
  run, on every managed apt or rpm node, and may not acquire new
  dependencies or call back to the console.
- It reaches existing nodes only through the agent's existing stale-fact
  refresh on client upgrade. Nothing about that mechanism changes here.

## Goals / Non-Goals

**Goals:**

- Report only genuinely installed packages, on both package managers.
- Keep the fact's output shape and format version identical; only rows
  that should never have been present disappear.
- Preserve reporting of installed packages whose *selection* state is not
  "install" - a held package must still be scanned.

**Non-Goals:**

- Retroactively deleting stale findings from the database. Convergence on
  the node's next report handles that; see Migration Plan.
- Any change to how findings are ranked, filtered, or presented. The
  fix-available default and severity precedence are a separate concern in
  the vulnerability-tracking capability.
- Deprioritising findings against installed-but-not-booted kernels. On rpm
  systems old kernels are genuinely installed (`installonly_limit`), so
  that is a real-data ranking question, not a collection defect, and it is
  deliberately out of scope here.

## Decisions

### Filter apt on install status, not on selection state

Chosen: select `${db:Status-Status}` in the `dpkg-query` format and keep
only rows equal to `installed`.

Alternative considered - `dpkg --get-selections` filtered to `install`:
**rejected**, and worth recording why, because it looks like the obvious
tool for the job. It reports what the administrator *wants* done with a
package, not whether the package is installed. Measured behaviour:

| package | status | selection |
|---|---|---|
| `bash` after `apt-mark hold` | `installed` | `hold` |
| `cron` after `dpkg -r` | `config-files` | `deinstall` |

Filtering selections to `install` correctly drops `cron` but *also* drops
`bash`, which is installed. That trades an over-report for an under-report
- packages vanish from vulnerability scanning - and held packages are
disproportionately old, hence disproportionately vulnerable. For a
security feature the under-report is the far worse failure. It also
carries no version field, so it would need a second query and a join to
produce the same data.

Alternative considered - `${Status}` matched against `install ok
installed`: viable and marginally more portable, since `${Status}`
predates the `db:` namespace. Not chosen because `${db:Status-Status}` is
clearer and is available everywhere this project runs - verified on dpkg
1.19.7 (Ubuntu 20.04) and on Debian 12. `${Status}` remains the documented
fallback if a genuinely older dpkg ever has to be supported.

### Exclude rpm signing keys by package name

`rpm -qa` lists `gpg-pubkey-<keyid>-<timestamp>` entries once repository
signing keys have been imported, which is the normal state of any real
system. These are keys, not software, and the current script emits them as
packages.

Chosen: exclude entries whose name is `gpg-pubkey`.

Alternative considered - treat `%{ARCH}` or `%{SOURCERPM}` of `(none)` as
the discriminator. Both do distinguish the pseudo-entries (a real package
reports `x86_64` and `bash-5.1.8-6.el9_1.src.rpm`), but they are
heuristics about metadata absence that could match some legitimate edge
case. The name is what rpm actually defines the pseudo-package as, so
matching it is the narrower and more predictable filter.

### No install-state filter for rpm

RPM has no analogue of dpkg's `config-files` state: erasing a package
removes its rpmdb entry outright, verified by installing and erasing a
package and confirming the row count returns to zero. Adding a status
filter there would be dead code.

### Filter once, before aggregation

The status field is selected as the first column and filtered before the
awk aggregation, so both emitted structures are built from the same
already-filtered rows. Consequence: every existing awk field index shifts
by one, including the multi-arch dedupe key, which is the most likely
place for this change to go subtly wrong.

## Risks / Trade-offs

- **The field-index shift introduces an off-by-one** → cover the script
  with a test over sample `dpkg-query` output containing a `config-files`
  row, a held row, a multi-arch duplicate pair, and a row whose source
  package differs in name and version, asserting the emitted JSON.
- **Someone later "simplifies" the filter to `--get-selections`** → the
  spec pins a held-but-installed package as reportable, so the regression
  fails a scenario rather than silently shipping; this document records
  the measured reason.
- **`${db:Status-Status}` unavailable on some very old dpkg** → verified
  present on dpkg 1.19.7; Debian 10 and Ubuntu 18.04 both ship 1.19 or
  newer, and the `${Status}` fallback is documented above.
- **Existing false findings persist after the code change** → expected;
  they clear on convergence rather than instantly. Operators reading the
  vulnerability list in the interim still see them.

## Migration Plan

No data migration and no schema change.

1. The corrected scripts ship inside the client binary. On upgrade, the
   agent's existing stale-fact refresh rewrites the fact in place for
   nodes that already report package inventory - without enabling
   reporting where it is disabled, and without triggering a Puppet run.
2. The node's next scheduled Puppet run publishes corrected inventory.
3. The vulnerability engine re-evaluates and closes findings whose
   packages are no longer reported.

Operators who also want the stale entries gone from the node itself can
purge them with `apt purge '~c'`. `apt autoremove` does not clear them,
because the packages are already removed - only their configuration files
remain, which is exactly why they were invisible to the operator while
still being reported.

Rollback: revert the scripts. Nodes return to reporting the previous,
over-inclusive data on their next run; no state needs unwinding.
