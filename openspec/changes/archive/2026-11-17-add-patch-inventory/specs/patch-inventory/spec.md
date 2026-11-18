# Spec Delta

## Purpose
Shows which updates each managed Linux node is missing, from a scan the node
agent runs read-only, so operators can see patch exposure before any patching
exists.

## ADDED Requirements

### Requirement: A node reports its pending updates
The system SHALL scan a connected node for pending updates using the node's
own package manager (apt, or dnf/yum), and SHALL store the result as that
node's latest patch snapshot, replacing the previous one whole. A scan SHALL
NOT install, upgrade or remove anything.

#### Scenario: A scan stores the pending updates
- **WHEN** a connected node with 3 pending updates, 1 of them security, is scanned
- **THEN** its snapshot lists those 3 updates with versions, marks 1 as security, and records the scan time

#### Scenario: A newer scan replaces the older one
- **WHEN** a node is scanned again after one update was applied outside the console
- **THEN** its snapshot lists only the remaining updates

#### Scenario: A node with nothing pending
- **WHEN** a node with no pending updates is scanned
- **THEN** its snapshot is recorded with zero updates and a scan time, which is different from never having been scanned

#### Scenario: A failed scan keeps the last good snapshot
- **WHEN** a scan fails, for example the package manager cannot reach its mirrors
- **THEN** the error and its time are recorded, and the previous updates stay shown, marked as older than the last attempt

#### Scenario: An unsupported system
- **WHEN** a node uses a package manager the scan does not support, or runs an agent without the scan action
- **THEN** the node is reported as unable to report patches, not as fully patched

### Requirement: A snapshot's content
Each pending update SHALL carry the package name, installed version (when
known), available version, and whether the package manager marks it a
security update. The snapshot SHALL also carry the time of the scan, the
package manager used, whether a reboot is already required, and the scan's
own error when it failed.

#### Scenario: Reboot already required
- **WHEN** a node already needs a reboot and is scanned
- **THEN** its snapshot says a reboot is required

#### Scenario: Security flag where the manager has none
- **WHEN** the node's package manager exposes no security metadata
- **THEN** no update is marked security and the snapshot does not claim otherwise

### Requirement: Fleet patch view
The system SHALL list the fleet's nodes with their patch state: pending count,
security count, reboot required, scan time, and whether the snapshot is stale
(older than twice the scan interval). It SHALL be filterable by node group, by
operating system, and by a package name, and nodes never scanned SHALL be
listed as such. A package search SHALL list which nodes have a given package
pending, with the installed and available versions.

#### Scenario: Filtering by package
- **WHEN** a user searches the fleet for pending `openssl`
- **THEN** the answer is each node with an `openssl` update pending, with installed and available versions

#### Scenario: A stale snapshot is marked
- **WHEN** a node was last scanned more than twice the scan interval ago
- **THEN** the view marks it stale

#### Scenario: A node never scanned
- **WHEN** a node has never been scanned
- **THEN** it is listed as not scanned, without counts

### Requirement: Reading patch data is scoped
Reading patch data SHALL require `nodes:read`, and SHALL only include nodes in
the caller's node scope. A node outside the caller's scope SHALL be answered
exactly as an unknown node, and SHALL NOT appear in lists, counts or package
searches.

#### Scenario: A scoped user sees only their nodes
- **WHEN** a user scoped to group A lists patch state
- **THEN** only nodes in group A appear, and the totals count only those

#### Scenario: An out-of-scope node reads as unknown
- **WHEN** that user asks for the patches of a node in another group
- **THEN** the answer is the same as for a node that does not exist

### Requirement: Scanning on demand
A user with `orchestrator:run` SHALL be able to scan one node, or every node
of a node group, and SHALL be told per node whether the scan succeeded,
failed, or the node was offline. Only nodes in the caller's node scope SHALL
be scanned; a group request covers the group's nodes in scope and says how many
were skipped as out of scope. A scan of a group SHALL bound how many nodes it
scans at once.

#### Scenario: Scanning one node
- **WHEN** a permitted user scans a connected node
- **THEN** its snapshot is updated and the answer shows the new counts

#### Scenario: An offline node
- **WHEN** a scan is requested for a node that is not connected
- **THEN** that node is reported offline and its snapshot is unchanged

#### Scenario: A group scan reports each node
- **WHEN** a user scans a group with one offline node
- **THEN** the other nodes are scanned and the answer lists the offline one

#### Scenario: Out of scope
- **WHEN** a user scans a node outside their scope
- **THEN** the answer is `403` naming the node, as for any other action on a node outside scope, and nothing is dispatched

#### Scenario: Missing permission
- **WHEN** a user without `orchestrator:run` asks for a scan
- **THEN** the answer is `403`

### Requirement: Scheduled scanning
The system SHALL scan every connected node on an interval, default 24 hours,
that a user with `nodes:manage` can change (15 minutes to 7 days) or switch
off. At most one console instance SHALL run the periodic scan at a time, and a
node scanned within the last interval SHALL NOT be scanned again by it.

#### Scenario: Interval elapses
- **WHEN** a connected node's last scan is older than the interval
- **THEN** it is scanned on the next periodic pass

#### Scenario: Switched off
- **WHEN** periodic scanning is off
- **THEN** no node is scanned unless a user asks

#### Scenario: Interval out of range
- **WHEN** a user sets an interval of 5 minutes
- **THEN** the answer is `400` naming the allowed range, and the setting is unchanged

#### Scenario: Setting needs permission
- **WHEN** a user without `nodes:manage` changes the interval
- **THEN** the answer is `403`

### Requirement: Scans are audited
A scan a user requests and a change to the scan interval SHALL be audited with
the actor and the nodes or group involved. Periodic scans SHALL NOT write one
audit entry per node.

#### Scenario: A requested scan is recorded
- **WHEN** a user scans a group
- **THEN** one audit entry names the user, the group, and how many nodes were scanned, failed or skipped

#### Scenario: Periodic scans are quiet
- **WHEN** the periodic scan covers 500 nodes
- **THEN** no per-node audit entries are written

### Requirement: Console UI for patching
The console SHALL show a Patching page with the fleet view, filters, the scan
control and (for `nodes:manage`) the interval setting, and a patch panel on the
node page with its updates, reboot flag and scan time. Every string SHALL be
translated in all supported languages.

#### Scenario: The node page shows pending updates
- **WHEN** a user opens a scanned node
- **THEN** the page lists its updates, marks the security ones, and shows when it was scanned

#### Scenario: A scan from the page
- **WHEN** a permitted user presses Scan on a node page
- **THEN** the panel shows progress and then the new result, or the node's error
