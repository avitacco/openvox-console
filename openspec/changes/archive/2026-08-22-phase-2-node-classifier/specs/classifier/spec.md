## Purpose

Manages node groups - what classes, parameters, and environment apply to
which nodes - and resolves, for any given node, the final merged
classification from every group that applies to it.

## ADDED Requirements

### Requirement: Node group management
The system SHALL allow creating, viewing, updating, and deleting node
groups. Each group has a name, classes (optionally parameterized),
top-scope parameters, an environment, a match rule and/or a list of
explicitly pinned nodes, and an explicit numeric priority.

#### Scenario: Creating a group
- **WHEN** a group is created with a name, one or more classes, and a
  priority
- **THEN** the group exists and is included in future classification runs

#### Scenario: Updating a group's classes
- **WHEN** an existing group's classes are changed
- **THEN** subsequent classification of nodes matching that group reflects
  the updated classes

#### Scenario: Deleting a group
- **WHEN** a group is deleted
- **THEN** it no longer affects classification of any node

### Requirement: Fact-based group matching
The system SHALL match a node to a group when the node's facts satisfy the
group's match rule.

#### Scenario: A node's facts satisfy a group's rule
- **WHEN** classification runs for a node whose facts satisfy a group's
  match rule
- **THEN** that group is included among the node's matching groups

#### Scenario: A node's facts do not satisfy a group's rule
- **WHEN** classification runs for a node whose facts do not satisfy a
  group's match rule, and the node is not pinned to that group
- **THEN** that group is not included among the node's matching groups

### Requirement: Explicit node pinning
The system SHALL match a node to a group when the node is explicitly
pinned to that group, regardless of whether the group's match rule is
satisfied.

#### Scenario: A pinned node matches without satisfying the rule
- **WHEN** classification runs for a node that is explicitly pinned to a
  group, and the node's facts do not satisfy that group's match rule
- **THEN** that group is still included among the node's matching groups

### Requirement: Classification merge by explicit priority
The system SHALL merge classes, parameters, and environment from every
group matching a node into a single classification result. When two
matching groups set conflicting values (the same class parameter, the same
top-scope parameter, or the environment), the value from the group with
the higher explicit priority SHALL win. Groups do not inherit from or
override each other implicitly by any hierarchy - only explicit priority
determines precedence.

#### Scenario: Higher-priority group wins a conflicting value
- **WHEN** two matching groups set different values for the same
  parameter, and the groups have different priorities
- **THEN** the resulting classification uses the value from the
  higher-priority group

#### Scenario: Non-conflicting classes are unioned
- **WHEN** two matching groups declare different classes
- **THEN** the resulting classification includes classes from both groups

#### Scenario: No matching groups
- **WHEN** classification runs for a node matched by no group
- **THEN** the system returns an empty classification, not an error

### Requirement: Console UI for group management
The system SHALL provide a UI to list, create, edit, and delete node
groups.

#### Scenario: Viewing the group list
- **WHEN** a user opens the group management view
- **THEN** the system displays every configured node group

#### Scenario: Creating a group via the UI
- **WHEN** a user submits the create-group form with a name and at least
  one class
- **THEN** the new group appears in the group list and is included in
  future classification runs
