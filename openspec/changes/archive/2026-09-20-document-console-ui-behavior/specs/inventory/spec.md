## MODIFIED Requirements

### Requirement: Node list view
The system SHALL display a list of nodes known to openvoxdb, showing each
node's name, status, and last check-in time.

A user with permission to run Puppet SHALL be able to select several
nodes from that list and trigger a run against all of them in one
action, rather than visiting each node in turn. Selection SHALL NOT
silently outlive what the user can see: nodes that leave the list
through filtering or paging SHALL NOT remain selected, so a triggered
run can only ever target nodes the user had in view.

#### Scenario: Viewing the node list
- **WHEN** a user opens the node inventory view
- **THEN** the system displays every known node with its status and last
  check-in time, sourced from openvoxdb

#### Scenario: No nodes known yet
- **WHEN** a user opens the node inventory view and openvoxdb reports no
  nodes
- **THEN** the system displays an empty state rather than an error

#### Scenario: Running Puppet against several nodes at once
- **WHEN** a user with permission to run Puppet selects several nodes and
  triggers a run
- **THEN** a run is requested for each selected node

#### Scenario: Selection does not survive nodes leaving the view
- **WHEN** nodes are selected and the user then filters or pages so that
  some of them are no longer listed
- **THEN** those nodes are no longer selected, and a subsequent run
  targets only nodes still in view

#### Scenario: Selection is not offered without permission
- **WHEN** a user without permission to run Puppet opens the node list
- **THEN** no selection or run control is offered

### Requirement: Node detail view
The system SHALL display a single node's detail, organised so that its
facts, its installed packages, its recent runs and its vulnerabilities
are each reachable without scrolling past the others.

Facts SHALL be presented two ways. A structured presentation SHALL
summarise the facts an operator most often reads - storage and memory
capacity, mounted filesystems, and network addressing - showing
proportional usage for anything with a capacity, and identifying which
interface carries each address and which address is the node's primary.
A raw presentation SHALL expose the node's complete fact set, because
the structured one is deliberately a subset and an operator must still
be able to reach any fact the node reported.

#### Scenario: Viewing node facts
- **WHEN** a user selects a node from the inventory
- **THEN** the system displays that node's facts as reported by openvoxdb

#### Scenario: Moving between a node's sections
- **WHEN** a user opens a node's detail and selects its packages, runs or
  vulnerabilities section
- **THEN** that section is displayed in place, without leaving the node

#### Scenario: Capacity is shown proportionally
- **WHEN** a node reports a filesystem or memory total and the amount used
- **THEN** the usage is shown proportionally as well as numerically

#### Scenario: Every address is attributed to its interface
- **WHEN** a node reports addresses on several network interfaces
- **THEN** each address is shown against the interface carrying it, with
  the node's primary address identifiable among them

#### Scenario: Reaching a fact the structured view omits
- **WHEN** a user needs a fact the structured presentation does not
  include
- **THEN** the raw presentation exposes the node's complete fact set
