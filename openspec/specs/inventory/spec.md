# inventory Specification

## Purpose

Lets console users browse the inventory of OpenVox-managed nodes - their
facts, current status, and how recently they last checked in - and find
specific nodes through search and filtering.

## Requirements

### Requirement: Node list view
The system SHALL display a list of nodes known to openvoxdb, showing each
node's name, status, and last check-in time.

#### Scenario: Viewing the node list
- **WHEN** a user opens the node inventory view
- **THEN** the system displays every known node with its status and last
  check-in time, sourced from openvoxdb

#### Scenario: No nodes known yet
- **WHEN** a user opens the node inventory view and openvoxdb reports no
  nodes
- **THEN** the system displays an empty state rather than an error

### Requirement: Node detail view
The system SHALL display a single node's full fact set on request.

#### Scenario: Viewing node facts
- **WHEN** a user selects a node from the inventory
- **THEN** the system displays that node's facts as reported by openvoxdb

### Requirement: Node search and filtering
The system SHALL let a user filter the node list by node name and by fact
value.

#### Scenario: Filtering by name
- **WHEN** a user enters a search term matching part of a node's name
- **THEN** the displayed node list narrows to nodes whose name matches the
  term

#### Scenario: Filtering by fact value
- **WHEN** a user filters by a specific fact name and value
- **THEN** the displayed node list narrows to nodes reporting that fact
  value

### Requirement: Endpoints require authentication
The system SHALL require a valid, unexpired, unrevoked access token
carrying the `nodes:read` permission on the node list, node detail, and
fleet status summary endpoints in this capability, and the
`nodes:manage` permission on the node deletion endpoint.

#### Scenario: Unauthenticated request rejected
- **WHEN** a request to the node list, node detail, or node deletion
  endpoint has no valid access token
- **THEN** the system rejects the request

#### Scenario: Authenticated request with the required permission succeeds
- **WHEN** a request to the node list or node detail endpoint presents a
  valid access token carrying `nodes:read`, or a request to the node
  deletion endpoint presents one carrying `nodes:manage`
- **THEN** the system processes the request as before

#### Scenario: Read permission is not sufficient to delete a node
- **WHEN** a request to the node deletion endpoint presents a valid
  access token carrying `nodes:read` but not `nodes:manage`
- **THEN** the system rejects the request

### Requirement: Fleet status summary
The system SHALL report, across every known node, counts of exactly
four categories based on each node's latest report: `failed` (the
latest report failed), `corrected` (the latest report changed and
Puppet corrected at least one unexpectedly-drifted resource),
`intentional` (the latest report changed with no corrective drift
correction involved), and `unchanged` (the latest report applied
cleanly with no changes). A node whose latest report is `noop`, or that
has no report at all, SHALL be excluded from all four categories rather
than forced into one. The system SHALL also report the total count of
every known node, independent of and not reduced by the four
categories or any exclusion.

#### Scenario: A failed node is counted as failed
- **WHEN** the fleet status summary is requested and a node's latest
  report status is `failed`
- **THEN** that node is counted under `failed`

#### Scenario: A node with a corrective change is counted as corrected
- **WHEN** the fleet status summary is requested and a node's latest
  report changed and included at least one corrective (unexpected
  drift) change
- **THEN** that node is counted under `corrected`

#### Scenario: A node with only intentional changes is counted as intentional
- **WHEN** the fleet status summary is requested and a node's latest
  report changed with no corrective change involved (including when
  corrective-change tracking is not enabled for that node, so no
  corrective change can be confirmed)
- **THEN** that node is counted under `intentional`

#### Scenario: An unchanged node is counted as unchanged
- **WHEN** the fleet status summary is requested and a node's latest
  report applied with no changes
- **THEN** that node is counted under `unchanged`

#### Scenario: A noop or unreported node is excluded from the four categories
- **WHEN** the fleet status summary is requested and a node's latest
  report status is `noop`, or the node has no report at all
- **THEN** that node is not counted under `failed`, `corrected`,
  `intentional`, or `unchanged`

#### Scenario: The total count is unaffected by exclusions
- **WHEN** the fleet status summary is requested and at least one node
  is excluded from the four categories
- **THEN** the reported total still counts every known node, including
  the excluded ones

### Requirement: Node deletion
The system SHALL let an authorized user delete a node, which
deactivates it in openvoxdb and cleans its CA certificate record,
without forcing immediate removal of its historical data (facts,
catalogs, reports); openvoxdb's own background garbage collection is
responsible for eventually erasing a deactivated node's data, on its
own schedule. Both the deactivation and the certificate cleanup SHALL
occur, since the node list reflects both openvoxdb's inventory and the
separate certificate/connectivity registry - deactivating alone leaves
a node with a lingering certificate record still visible.

#### Scenario: Deleting a node removes it from the node list
- **WHEN** an authorized user deletes a node
- **THEN** the system deactivates that node in openvoxdb, cleans its CA
  certificate record, and it no longer appears in the node list

#### Scenario: Deleting an already-deactivated node
- **WHEN** an authorized user deletes a node that is already
  deactivated in openvoxdb
- **THEN** the system completes the request without error

#### Scenario: Deleting a node with no certificate record
- **WHEN** an authorized user deletes a node that openvoxdb knows about
  but that has no CA certificate record
- **THEN** the system completes the request without error, having
  nothing to clean on the certificate side

#### Scenario: Deleting an unknown node
- **WHEN** an authorized user requests deletion of a certname openvoxdb
  has no record of
- **THEN** the system responds with an error identifying the node as
  unknown, rather than a success

#### Scenario: No CA client configured
- **WHEN** an authorized user deletes a node and no CA client is
  configured for this console
- **THEN** the system still deactivates the node in openvoxdb and
  completes the request without error, even though no certificate
  cleanup can occur

### Requirement: Deletion requires confirmation
The system SHALL require the user to confirm a node deletion, through
an explicit dialog identifying the node and stating that its data will
be deleted through openvoxdb's own garbage collection rather than
immediately, before the system deactivates it - and SHALL NOT deactivate
a node before the user confirms.

#### Scenario: Confirming a deletion proceeds
- **WHEN** a user clicks the delete action for a node and confirms in
  the resulting dialog
- **THEN** the system proceeds with deactivating that node

#### Scenario: Canceling a deletion has no effect
- **WHEN** a user clicks the delete action for a node and cancels or
  dismisses the resulting dialog without confirming
- **THEN** the system does not deactivate that node
