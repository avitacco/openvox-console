## MODIFIED Requirements

### Requirement: Dedicated nodes page
The system SHALL provide a page, separate from the existing node
inventory page, listing every node known to openvoxdb alongside its
current node transport connection status, its most recent connection/
disconnection timestamp (if known), and its Puppet certificate status
(if known). The system SHALL let the user sort this list by any of
those columns, and filter it by node name (substring match), connection
status, and certificate status, applied across every known node rather
than only those currently visible.

#### Scenario: Viewing the nodes page
- **WHEN** a user opens the nodes page
- **THEN** the system displays every known node's name alongside whether
  it currently has a live node transport connection, its most recent
  connection/disconnection timestamp, and its certificate status

#### Scenario: A node's connection status updates without a page reload requirement
- **WHEN** a node's connection status changes while a user is viewing the
  nodes page
- **THEN** the displayed status becomes accurate on the user's next
  refresh of the page (a live-updating requirement is not required for
  this capability)

#### Scenario: Sorting by a column
- **WHEN** a user selects a column to sort by (node name, connection
  status, last connected timestamp, or certificate status)
- **THEN** the system reorders every known node's row by that column, and
  reversing the selection reverses the order

#### Scenario: Filtering by node name
- **WHEN** a user enters text into the node name filter
- **THEN** the system displays only nodes whose name contains that text,
  across the entire known fleet, not only nodes on a given page

#### Scenario: Filtering by connection or certificate status
- **WHEN** a user selects a specific connection status or certificate
  status to filter by
- **THEN** the system displays only nodes matching that status, across
  the entire known fleet

#### Scenario: Combining sort and filter
- **WHEN** a user has both a filter and a sort order active
- **THEN** the system displays only the filtered nodes, in the selected
  sort order
