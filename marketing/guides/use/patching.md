---
title: Find and apply missing updates
summary: Scan Linux nodes for pending package updates, then patch them in batches with a dry run, a stop threshold and an optional reboot.
order: 8
---

The **Patching** page shows which updates each node is missing, as the
node's own package manager reports them. A scan only looks. A
[patch run](#patch-nodes) installs them.

This covers Linux nodes that use **apt**, or **dnf** or **yum**. Windows,
macOS and zypper (SUSE) are not scanned: a node on one of them is listed
as unable to report, never as fully patched.

Seeing results needs `nodes:read`, limited to the node groups your role
names. Scanning or patching a node or a group needs `orchestrator:run`,
again for the nodes your role covers. Changing how often the console scans
needs `nodes:manage`. No new permission is involved.

## What a scan does

A scan asks the node's agent to run its package manager in a mode that
changes nothing: it refreshes the package lists, then reports what an
upgrade would replace. For each pending update you see the package, the
installed version, the available version, and whether the package manager
marks it a **security** update. A node also reports whether it already
needs a reboot.

Refreshing the lists means a scan reaches the node's mirrors. A node that
cannot reach them records that the scan failed, and keeps showing the
updates from the last scan that worked, marked as older than the last
attempt.

Two things to read carefully:

- **Security** is what the package manager says. A system whose
  repositories carry no security information shows none, which is not the
  same as having none. Ubuntu and Debian mark them in the repository names;
  on RHEL-family systems the console asks `dnf updateinfo` for the
  security set.
- **Never scanned** is not **nothing pending**. A node that has not been
  scanned is listed as not scanned, without counts.

The node's agent must be built from a version that includes scanning. An
older agent answers that it does not know the request, and the node is
shown as one that cannot report until the agent is upgraded: see
[Keep node agents current](agent-upgrades.html).

## Read the fleet

Open **Patching**. The figures at the top count the nodes you can see:
those with pending updates, those with security updates, those needing a
reboot, those not scanned, and those whose last scan is out of date.

A scan is **out of date** when it is older than twice the scan interval,
so a node that quietly stopped being scanned stands out. With automatic
scanning off, it is judged against a day.

Filter by:

- **Operating system**, to see one family at a time.
- **Group**, if your role can read node groups.
- **Pending package**, which switches the list to the nodes that have an
  update waiting for a package matching that name, with the installed and
  available versions. Searching `openssl` is how you find who still needs
  the fix.

Select a node to open its page. The **Patches** tab lists its updates,
security first, with when it was last scanned.

## Scan now

On a node's **Patches** tab, **Scan now** scans that node. On the Patching
page, choose a group and **Scan this group now** scans every node of the
group you may act on. The answer says, node by node, which were scanned,
which failed, which were offline, and how many were left out because they
are outside your node groups.

A node that is offline is not scanned and keeps what it had; it is
scanned on the next pass once it reconnects. A group scan runs a few nodes
at a time, so a large group takes a while.

Each scan you request is recorded in the audit log with who asked and the
node or group.

## Scan automatically

Unless you switch it off, the console scans every connected node once a
day. Under **Automatic scanning** on the Patching page (for `nodes:manage`)
set the interval anywhere from 15 minutes to 7 days, or turn it off.

- Only one console instance runs the automatic scan at a time, however
  many you run, and a node scanned within the interval is not scanned
  again.
- A node whose scan failed is tried again after the next interval, not on
  every pass.
- Automatic scans write no audit entry per node. A change to the interval
  is audited.
- With [run modes](run-modes.html), the scan runs in `all` and `worker`
  instances.
- Results for a node nothing has tried for 90 days are removed.

## Patch nodes

A **patch run** installs pending updates on Linux nodes (apt, dnf or yum).
Start one from the Patching page with **Patch this group…**, or from a
node's **Patches** tab with **Patch now…**. It needs `orchestrator:run`, and
like any run it is refused whole, naming the nodes, if any of them is
outside your node groups.

Choose:

- **Updates to apply**: **security updates only**, **all pending updates**,
  or **named packages**. A named package that has no update pending is
  reported as nothing to apply and nothing else is touched. On dnf and yum,
  security-only needs the repository's security information; without it the
  node fails the run rather than installing everything.
- **Dry run**, which reports what would be upgraded and changes, restarts
  and reboots nothing.
- **Reboot a node that needs it**, which reboots a node after a successful
  run only if it reports that it needs one. A dry run or a failed node is
  never rebooted. The node delivers its result first and reboots about half
  a minute later.
- **Nodes at a time** (1 to 100, default 10) and **Stop after failed nodes**
  (default 3, **0** never stops).

### Batches and stopping

Nodes are patched a batch at a time, in node-name order. A batch is patched
at once and the next starts only when it has finished. When the number of
failed nodes reaches the threshold, no further batch starts: the nodes not
reached are recorded as **not started**, and nothing is sent to them. Use a
small first batch on something you can afford to lose, then let the rest
follow.

A node that is already being patched by another run is not started again:
the request is refused, naming it.

### Watch a run

The run opens as a job. It lists each node with its batch, status, exit
status and the packages it upgraded (**from** and **to** versions), and
under **Details** the package manager's own output. The page refreshes while
the run is going. The node's **Patches** tab shows the last run on that
node, and the Patching page lists recent runs.

A run can take hours. The console asks each node how it is getting on every
15 seconds, and every answer counts as proof the run is alive. A node that
says nothing for ten minutes is failed, as is one still working after four
hours, and the run carries on with the others. If the console instance
following the run stops, the job is failed ten minutes after it was last
heard from, with the reason recorded. The patch itself carries on on the
node, so scan it again to see where it stands.

### Afterwards

When a node finishes, the packages it still has pending are what the
Patching page shows: the node's own scan is taken at the end of its run. A
node that rebooted is scanned again once it reconnects, for up to ten
minutes. If it does not come back in that time its snapshot says the scan
failed, and the run itself is not failed for it.

Each run is audited when it starts, with who started it, the nodes' source
and every option chosen, and again when it ends. Both appear in the
activity log.

A node whose agent is too old to patch is failed with a reason saying so and
nothing is installed on it: see [Keep node agents
current](agent-upgrades.html).

## Not covered

This version does not schedule patch windows or blackouts, group nodes for
patching, roll a patch back, patch Windows or macOS, or relate pending
updates to known vulnerabilities: see
[Track vulnerabilities](vulnerabilities.html) for what is installed
against known advisories.
