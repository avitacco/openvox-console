# Design

## Context

Package inventory already comes from openvoxdb facts, and the console already
dispatches small actions to a node over the node transport
(`package_inventory_status`/`_set` in `internal/packageinventory`, handled in
`internal/nodeagent/handler.go`). Those handlers call the transport directly
rather than creating orchestrator jobs, and apply `orchestrator:run` plus node
scope. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:** accurate, read-only pending-update visibility for Linux nodes that
week 11 can build on; scope and permission rules identical to neighbouring
features.

**Non-Goals:** applying anything; history of past scans (latest only);
Windows/macOS/zypper; correlating with CVEs.

## Decisions

1. **A node-agent action, not a fact module.** `patch_scan` runs the package
   manager on demand. Facts only refresh with a Puppet run, cannot be asked
   for "now", and would need a module deployed to every environment.
   Alternative: a pe_patch-style fact - rejected for staleness and the module
   dependency.
2. **Commands (all read-only).**
   - apt: `apt-get update` (refreshing lists only) then `apt-get -s upgrade`,
     parsing `Inst` lines. A line whose origin contains `-security` is a
     security update. `/var/run/reboot-required` marks reboot required.
   - dnf (yum when dnf is absent): `check-update --refresh` (exit 100 means
     updates), with `updateinfo list security` for the security set, and
     `needs-restarting -r` for reboot.
   The agent parses; the console stores what it is given. A `refresh` param
   (default true) lets a caller skip the list refresh.
   Alternative: skip the refresh - rejected, the list would report only what
   the last refresh knew.
3. **Not orchestrator jobs.** A scan is a short dispatch whose result is a
   snapshot, not a job history. It follows the package-inventory precedent:
   dispatch directly, `orchestrator:run` for the permission, node scope applied
   before dispatch. Week 11's patch runs, which are long and need history,
   will be jobs.
4. **Permissions: no new one.** Reads use `nodes:read` like package inventory.
   Scanning uses `orchestrator:run`. Task patterns do not apply (a scan is not a
   task). The interval setting uses `nodes:manage`.
5. **Storage.** `node_patch_scans` (certname PK, scanned_at, attempted_at,
   manager, reboot_required, error, update_count, security_count) and
   `node_patch_updates` (certname, name, installed, available, security; PK
   certname+name), replaced in one transaction per scan, so a reader never sees
   a half-written snapshot. A singleton `patch_scan_settings` row holds the
   interval (minutes) and enabled flag. Snapshots of nodes unseen for 90 days
   are purged by the periodic pass.
6. **Periodic scan.** A leased loop (as the vulnerability scheduler) ticks every
   minute, picks connected nodes whose last attempt is older than the interval,
   and scans them with a small worker pool (default 8). Group scans use the
   same pool. A failed attempt updates `attempted_at` so a broken node is not
   retried every tick.
7. **Staleness** is derived at read time: `scanned_at` older than twice the
   interval. With scanning off it is judged against 24 hours.
8. **Fleet view reads Postgres, not openvoxdb**, and takes node group and OS
   filters by joining the console's existing classification and the facts it
   already uses for the package catalogue. The node scope filter is applied
   as the sweep test expects (listed routes must scope or be exempt).

## Risks / Trade-offs

- `apt-get update` / `dnf --refresh` touch the network and take time on a
  slow node → the dispatch timeout is generous (5 min) and the failure is
  recorded; the old snapshot stays.
- A scan holds the package manager lock → a node mid-install returns an error;
  it is retried next pass.
- Old agents → unsupported-action is a distinct result, shown as such.
- Security flags depend on what the manager exposes (a distro without
  security metadata shows none) → the UI says "security flagged by the
  package manager", not "no security updates".

## Migration Plan

Migration 000037 is additive. Rollback drops the three tables. Nodes begin
to appear as agents upgrade and the first periodic pass runs.

## Implementation Notes

Where the build differs from, or settles, what is written above.

- **`dist-upgrade`, not `upgrade`.** The apt scan simulates `dist-upgrade`
  (`apt-get -s`), because plain `upgrade` leaves out updates that need a new
  dependency and would report a kernel update as nothing. Only lines with an
  installed version are updates; packages that would be newly installed are
  not listed.
- **dnf/yum installed version.** `check-update` does not print it, so it is
  left empty for those nodes rather than costing an `rpm` query per package.
  The UI shows a dash. The security set comes from `updateinfo list security`
  and is matched by package name.
- **Out-of-scope scan is a `403`.** Decision 4 left this open; the
  existing convention for actions on a node (orchestrator, package reporting)
  is a refusal naming the node, so a single-node scan does the same and the
  spec says so. A group scan instead skips nodes outside the caller's scope
  and reports how many, because the caller named a group, not those nodes.
  Reads of an out-of-scope node answer exactly as an unknown node (`404`
  with no name echoed).
- **Where the periodic scan runs.** A worker `patch-scanner` in modes `all`
  and `worker`: every instance can dispatch, so it sits with the other
  leased workers. A pass takes the `patch-scan` lease, so one instance
  scans at a time.
- **Offline nodes record no attempt**, so they are scanned on the first pass
  after they reconnect; a failed attempt does record one, so a broken node
  is retried once per interval, not every minute.
- **Interval error shape.** A refused interval answers `400` with `field:
  "intervalMinutes"`, the shape the account policy page already reads.
- **No new route group.** The routes register with package inventory
  (`package-inventory`), the neighbouring node-data surface.
- **Task count.** The proposal said 13 tasks; there are 14.
- **Fleet totals** count every node in scope after filters, and treat an
  unsupported node separately from one never scanned.
