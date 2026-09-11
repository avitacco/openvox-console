## MODIFIED Requirements

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

## ADDED Requirements

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
