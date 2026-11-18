# Spec Delta

## Purpose
Applies the pending updates the patch inventory shows, across Linux nodes, in
batches with a stop threshold, tracking each node through to completion and
refreshing the inventory afterwards.

## ADDED Requirements

### Requirement: A patch run applies updates to the chosen nodes
The system SHALL let a user with `orchestrator:run` start a patch run against
nodes chosen as for a Puppet run (a list, a query or a node group), within
that user's node scope. The run SHALL apply security updates only, all pending
updates, or the named packages, using each node's own package manager.

#### Scenario: Security updates only
- **WHEN** a run for security updates is started on a node with three
  pending updates, one security
- **THEN** only the security update is installed

#### Scenario: Named packages
- **WHEN** a run names a package the node has no update for
- **THEN** that package is reported as having nothing to apply and nothing
  else is changed

#### Scenario: A node outside the caller's scope
- **WHEN** the nodes include one outside the caller's scope
- **THEN** the request is refused as any run would be, and nothing starts

### Requirement: A dry run installs nothing
The system SHALL support a dry run that reports, per node, which packages
would be upgraded and installs nothing, restarts nothing and reboots nothing.

#### Scenario: A dry run
- **WHEN** a dry run is started on a node with pending updates
- **THEN** the node reports its pending packages, none is installed and the
  node does not reboot

### Requirement: A run proceeds in batches and can stop
The system SHALL patch a batch of nodes at a time, as many as the batch size
allows concurrently, starting the next batch only when the previous one has
finished. When the number of failed nodes reaches the run's stop threshold,
the system SHALL start no further batch and SHALL record each node not yet
started as not started. A threshold of zero SHALL mean never stop.

#### Scenario: The threshold is reached
- **WHEN** a run of ten nodes has batches of two and a threshold of two, and
  the first batch fails on both nodes
- **THEN** the remaining six nodes are recorded as not started and nothing is
  sent to them

#### Scenario: Batches run in turn
- **WHEN** a run of five nodes has batches of two
- **THEN** no more than two nodes are being patched at any moment, and the
  third node is not started until the first two have finished

### Requirement: A node's progress is followed by heartbeat
The system SHALL send a node's patch request and have the node answer at once,
then follow its progress by asking for status at a regular interval, recording
each answer as that node's and the job's heartbeat. A run SHALL NOT be limited
to the length of one request, and SHALL fail a node that has not finished
after four hours.

#### Scenario: A long patch
- **WHEN** a node takes ninety minutes to finish patching
- **THEN** the run waits for it, and the job stays running throughout

#### Scenario: A node stops answering
- **WHEN** a node does not answer status requests for ten minutes while its
  run is unfinished
- **THEN** its target is failed with a reason that says it stopped answering

#### Scenario: The console instance that follows a run stops
- **WHEN** the instance following a run stops
- **THEN** the job and its unfinished targets are failed by the stale-job
  reaper once their heartbeat is ten minutes old, with the reason recorded

### Requirement: A node is patched by one run at a time
The system SHALL refuse to start a patch run that includes a node already
being patched by another run, naming the nodes, and a node agent SHALL refuse
a patch request while it is carrying out another.

#### Scenario: A node already being patched
- **WHEN** a run is started on a node that another patch run is patching
- **THEN** the request is refused, naming the node, and nothing starts

### Requirement: Results are recorded per node and per package
The system SHALL record, for each node, its outcome, the package manager's
exit status and output, the packages it upgraded (name, previous version,
new version) and those it did not, and whether a reboot is still required.
A node's outcome SHALL be failed when the package manager failed or any
requested package was not upgraded.

#### Scenario: A successful node
- **WHEN** a node's updates were all installed
- **THEN** its target is succeeded and lists each package with the version
  it moved from and to

#### Scenario: A failing package manager
- **WHEN** the package manager exits with an error on a node
- **THEN** its target is failed with the exit status and the output

### Requirement: The inventory is refreshed after a run
The system SHALL replace a node's pending-update snapshot with the scan the
node takes when its run finishes, and, for a node that rebooted, scan it again
once it reconnects, waiting up to ten minutes for it.

#### Scenario: The Patching page reflects the run
- **WHEN** a run finishes on a node
- **THEN** the node's pending updates and counts are those after the run

#### Scenario: A node that reboots
- **WHEN** a node rebooted at the end of its run
- **THEN** its reboot-required flag is cleared by a scan taken after it
  reconnects, and if it does not return within ten minutes its snapshot says
  the scan failed

### Requirement: A reboot is only taken when asked for and required
The system SHALL reboot a node after a successful run only when the run asked
for it and the node reports that a reboot is required, and SHALL NOT reboot
for a dry run or a failed node.

#### Scenario: Reboot requested and required
- **WHEN** a run asks for reboots and a node needs one after patching
- **THEN** the node reboots after its result has been delivered

#### Scenario: Reboot not requested
- **WHEN** a node needs a reboot but the run did not ask for one
- **THEN** it is not rebooted and its result says a reboot is required

### Requirement: An agent that cannot patch is not asked to guess
The system SHALL record a node whose agent does not know the patch requests
as failed with a reason saying the agent is too old, and a node with no
supported package manager as failed with a reason saying so; neither SHALL
have anything installed.

#### Scenario: An old agent
- **WHEN** a patch run reaches a node whose agent predates patching
- **THEN** its target is failed as too old, and nothing was installed

### Requirement: Patch runs are audited
The system SHALL write an audit entry when a patch run is started, naming the
actor, the target, and the options chosen, and when it ends, and SHALL record
both in the activity log.

#### Scenario: A started run is audited
- **WHEN** a user starts a patch run
- **THEN** an audit entry records who, the nodes' source, the update scope,
  batch size, stop threshold, dry run and reboot choice

### Requirement: Console UI for patch runs
The console SHALL let a user with `orchestrator:run` start a patch run from
the Patching page for a node group or chosen nodes, choosing the options and
seeing a preview of the nodes, and SHALL show a run's batches, per-node
status and per-package results on the job, with the last run on the node's
Patches tab. Strings SHALL be translated in every supported language.

#### Scenario: Starting a run
- **WHEN** a user chooses a group, security updates and batches of five
- **THEN** the console previews the nodes and starts the run when confirmed

#### Scenario: A user without orchestrator:run
- **WHEN** a user without the permission opens the Patching page
- **THEN** the page offers no way to start a run
