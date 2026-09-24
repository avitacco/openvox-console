## Purpose

Shows an operator the live state of the running stack - every console
instance with its role, location, health and background workers, plus the
external services the console depends on - so that a multi-instance
deployment can be understood from the console rather than by inspecting
deployment configuration and probing each instance by hand.

## ADDED Requirements

### Requirement: Every instance describes itself
Every console instance SHALL be able to report its own status, in every
run mode, comprising at minimum: its run mode, the address it serves HTTP
on, its health, how long it has been running, its software version,
which background workers it is running, and its own view of the cluster -
how it is attached (standalone, routed peer, or leaf) and how many
routed peers and leaf instances it can see.

The cluster view is reported because no single instance can see the whole
cluster: a leaf instance is visible only to the peer it is attached to.
Each instance's view is therefore a piece of the picture, and only the
combination of them is the picture.

An instance SHALL report this regardless of its mode, because an instance
that could not describe itself would be absent from the stack's status
while still running - the most misleading answer available.

#### Scenario: An instance reports its own status
- **WHEN** a console instance is asked for its status
- **THEN** it reports its run mode, serving address, health, uptime,
  version, and the background workers it runs
- **AND** it reports how it is attached to the cluster and how many
  routed peers and leaf instances it can see

#### Scenario: Every mode can describe itself
- **WHEN** a console instance running in any supported mode is asked for
  its status
- **THEN** it answers
- **AND** the answer names the mode it is running in

#### Scenario: An unhealthy instance still reports
- **WHEN** an instance whose own dependency check is failing is asked for
  its status
- **THEN** it answers, reporting itself unhealthy and naming the failing
  dependency
- **AND** it is not omitted from the result

### Requirement: Instance status is gathered live across the cluster
The system SHALL gather the status of every running instance by asking
them over the internal event bus and collecting the replies within a
bounded window, rather than reading a stored record of what was last
reported.

The collection window SHALL be bounded, so that a request for the stack's
status completes in a predictable time regardless of how many instances
are running or whether any of them is unresponsive.

#### Scenario: Several instances are reported
- **WHEN** several instances are running and peered, and one of them is
  asked for the stack's status
- **THEN** the result includes an entry for every instance that replied
- **AND** each entry carries that instance's own reported status

#### Scenario: A single unclustered instance
- **WHEN** an instance running alone, with no peers configured, is asked
  for the stack's status
- **THEN** the result includes exactly that one instance
- **AND** it is not reported as an error or an incomplete result

#### Scenario: Collection completes within a bounded time
- **WHEN** the stack's status is requested
- **THEN** the response is returned within the bounded collection window,
  whether or not every instance replied

### Requirement: The expected set is established by combining every reply
The system SHALL determine how many instances were expected to answer by
combining the cluster views reported by every instance that did answer,
rather than by trusting the view of the instance serving the request.

An instance that is visible to any replying instance SHALL therefore be
accounted for, even when it is not visible to the instance serving the
request. This matters concretely: `enc` instances attach as leaves, and a
leaf is visible only to the peer it attached to, so a status request
served by a different peer would otherwise have no idea it exists.

When the serving instance cannot establish an expected set at all -
no instance replied with a usable cluster view - the system SHALL treat
the picture as incomplete rather than assuming the replies it holds are
everything.

#### Scenario: A leaf attached to another peer is accounted for
- **WHEN** a leaf instance is attached to one peer, and the stack's
  status is requested from a different peer
- **AND** every instance replies
- **THEN** the leaf appears in the result
- **AND** the result is not marked incomplete

#### Scenario: A silent leaf attached to another peer is detected
- **WHEN** a leaf instance is attached to one peer and does not reply
- **AND** the peer it is attached to does reply, reporting that leaf in
  its cluster view
- **AND** the stack's status is requested from a different peer
- **THEN** the result is marked incomplete

#### Scenario: No usable cluster view
- **WHEN** no instance replies with a usable cluster view
- **THEN** the result is marked incomplete

### Requirement: An incomplete picture is reported as incomplete
When the system cannot establish the full set of running instances -
because an instance did not reply within the collection window, or
because the bus could not be reached - it SHALL say so in the result,
rather than presenting the instances it did hear from as though they were
all of them.

This is the requirement that makes the page trustworthy: silently
omitting an instance that is running but wedged would show a healthy
stack at exactly the moment something is wrong.

#### Scenario: An instance does not reply in time
- **WHEN** an instance is running but does not reply within the
  collection window
- **THEN** the result indicates that the picture may be incomplete
- **AND** the instances that did reply are still reported

#### Scenario: The event bus cannot be reached
- **WHEN** the serving instance cannot reach the event bus to ask the
  others
- **THEN** the result reports the serving instance's own status
- **AND** it indicates that the status of other instances could not be
  determined

#### Scenario: A complete picture is not flagged
- **WHEN** every instance replies within the collection window
- **THEN** the result is not marked incomplete

### Requirement: External dependencies are reported alongside instances
The system SHALL report the state of the external services the console
depends on - at minimum its Postgres database, openvoxdb, and the
openvoxserver CA - each with the target it is configured to reach and
whether that target is currently reachable.

A dependency that is not configured SHALL be reported as not configured,
distinctly from one that is configured but unreachable. These mean
different things to an operator: the first is a deployment choice, the
second is a fault.

#### Scenario: A reachable dependency
- **WHEN** the stack's status is requested and a configured dependency is
  reachable
- **THEN** it is reported as healthy, with the target it was reached at

#### Scenario: An unreachable dependency
- **WHEN** the stack's status is requested and a configured dependency
  cannot be reached
- **THEN** it is reported as unreachable, with the target that was tried

#### Scenario: An unconfigured optional dependency
- **WHEN** the stack's status is requested and an optional dependency
  (for example the CA client) is not configured
- **THEN** it is reported as not configured
- **AND** it is not reported as unhealthy

### Requirement: Stack status API
The system SHALL expose the aggregated stack status over HTTP as JSON,
restricted to callers whose access token carries the `status:read`
permission.

#### Scenario: An authorized caller reads the status
- **WHEN** a caller with `status:read` requests the stack status endpoint
- **THEN** the system responds with the instances and dependencies as
  JSON

#### Scenario: An unauthorized caller is refused
- **WHEN** a caller without `status:read` requests the stack status
  endpoint
- **THEN** the system refuses the request

### Requirement: Stack status page
The system SHALL provide a console page, restricted to users with
`status:read`, presenting the running instances grouped by run mode with
a count per mode, and the external dependencies, each showing its health.

The page SHALL make an incomplete picture visible to the reader, rather
than only recording it in the underlying data.

#### Scenario: Viewing the stack status
- **WHEN** a user with `status:read` opens the status page
- **THEN** it lists the running console instances, grouped by run mode
  and showing how many are running in each
- **AND** it lists the external dependencies with their health

#### Scenario: An instance's detail is visible
- **WHEN** a user views a reported instance on the status page
- **THEN** its run mode, address, health, uptime, version and background
  workers are shown

#### Scenario: Incompleteness is visible on the page
- **WHEN** the underlying status could not be fully determined
- **THEN** the page tells the reader the picture may be incomplete

#### Scenario: A user without permission
- **WHEN** a user without `status:read` navigates to the status page
- **THEN** the page does not show the stack's status

### Requirement: The status permission exists and is granted at bootstrap
The system SHALL define a `status:read` permission, and SHALL include it
in the permission set granted to the administrator role created when a
deployment is first bootstrapped, so that a new installation can view the
status page without a manual grant.

On an existing deployment, an administrator SHALL be able to grant
`status:read` to a role through the existing role management, like any
other permission.

#### Scenario: A freshly bootstrapped administrator can view the status
- **WHEN** a deployment is bootstrapped and its administrator logs in
- **THEN** that administrator holds `status:read`

#### Scenario: Granting the permission to an existing role
- **WHEN** an administrator adds `status:read` to a role
- **THEN** users holding that role can view the status page on their next
  token issuance
