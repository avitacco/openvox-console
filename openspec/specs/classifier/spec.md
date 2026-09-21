# classifier Specification

## Purpose

Manages node groups - what classes, parameters, and environment apply to
which nodes - and resolves, for any given node, the final merged
classification from every group that applies to it.

## Requirements

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
groups. Editing a group's match rule SHALL present each condition as
separate fields (a fact path, an operator constrained to the set of
operators the system supports, and a value) rather than requiring the
user to hand-edit the rule's underlying JSON representation, while
still allowing the underlying JSON to be viewed and edited directly for
cases the structured fields don't cover.

#### Scenario: Viewing the group list
- **WHEN** a user opens the group management view
- **THEN** the system displays every configured node group

#### Scenario: Creating a group via the UI
- **WHEN** a user submits the create-group form with a name and at least
  one class
- **THEN** the new group appears in the group list and is included in
  future classification runs

#### Scenario: Adding a match-rule condition via structured fields
- **WHEN** a user adds a condition to a group's match rule and fills in
  a fact path, selects an operator, and enters a value
- **THEN** saving the group produces a match rule containing that
  condition, equivalent to entering the same condition as JSON

#### Scenario: Removing a match-rule condition via structured fields
- **WHEN** a user removes a condition from a group's match rule using
  the structured editor
- **THEN** saving the group produces a match rule without that
  condition

#### Scenario: Viewing or editing a match rule as JSON
- **WHEN** a user switches a group's match-rule editor to its JSON
  view
- **THEN** the system displays the rule's current conditions as JSON,
  and a rule entered or edited there is reflected back in the
  structured fields when switched back

#### Scenario: Saving a match-rule condition with no fact path is blocked
- **WHEN** a user attempts to save a group whose match rule contains a
  condition with no fact path entered
- **THEN** the system blocks the save and identifies the incomplete
  condition, rather than saving a rule that can never match

#### Scenario: Saving an invalid regex match-rule value is blocked
- **WHEN** a user attempts to save a group whose match rule contains a
  condition using the regex-match operator with a value that is not a
  valid regular expression
- **THEN** the system blocks the save and identifies the invalid
  condition, rather than saving a rule that can never match

### Requirement: Editing a group never saves over unloaded values
The system SHALL NOT let a user save an existing group whose current
values were never successfully loaded into the edit view. An edit view
that failed to load holds only empty defaults, so saving it would
replace the group's stored classes, parameters, match rule, and pinned
nodes with empty ones; the system SHALL reject that save and say why,
rather than silently discarding the stored configuration.

#### Scenario: Saving after the group failed to load is refused
- **WHEN** a user opens an existing group for editing, the group's
  current values fail to load, and the user attempts to save
- **THEN** the system refuses the save, explains that the group has not
  loaded, and leaves the stored group unchanged

#### Scenario: Saving after a successful load proceeds normally
- **WHEN** a user opens an existing group for editing, its values load
  successfully, and the user saves
- **THEN** the system saves the group as edited

### Requirement: Endpoints require authentication, scoped by action
The system SHALL require a valid, unexpired, unrevoked access token on
every endpoint in this capability: read endpoints (listing or viewing a
group) require the `classifier:read` permission; write endpoints
(creating, updating, or deleting a group) require `classifier:write`.

#### Scenario: Unauthenticated request rejected
- **WHEN** any request to a group endpoint has no valid access token
- **THEN** the system rejects the request

#### Scenario: Read permission does not grant write access
- **WHEN** a request to create, update, or delete a group presents a
  valid access token carrying `classifier:read` but not
  `classifier:write`
- **THEN** the system rejects the request

#### Scenario: Authenticated request with the required permission succeeds
- **WHEN** a request presents a valid access token carrying the
  permission its action requires
- **THEN** the system processes the request as before

### Requirement: Node group changes publish an activity event
The system SHALL publish an activity event whenever a node group is
created, updated, or deleted, since a group change can change how nodes
are classified.

#### Scenario: Creating a group publishes an event
- **WHEN** a node group is created
- **THEN** an activity event recording the creation, the group, and the
  acting user is published

#### Scenario: Updating a group publishes an event
- **WHEN** a node group is updated
- **THEN** an activity event recording the update, the group, and the
  acting user is published

#### Scenario: Deleting a group publishes an event
- **WHEN** a node group is deleted
- **THEN** an activity event recording the deletion, the group, and the
  acting user is published

### Requirement: Group listings report matching node counts
The system SHALL report, for each group in a listing, how many nodes
currently match it, so a group's reach is visible without opening it.

Counts SHALL be resolved against the current fleet rather than stored,
since a fact-matched group's membership changes as nodes report. Where
the counts cannot be determined - the inventory being unreachable, for
instance - the listing SHALL still render, with the counts reported as
unavailable rather than as zero.

#### Scenario: Viewing groups with their node counts
- **WHEN** a user with `classifier:read` opens the group list
- **THEN** each group is shown with the number of nodes currently
  matching it

#### Scenario: A group nothing matches
- **WHEN** a group's rule matches no node in the current fleet
- **THEN** it is listed with a count of zero rather than omitted

#### Scenario: Node data unavailable
- **WHEN** the node data needed to resolve counts cannot be retrieved
- **THEN** the group list is still displayed, with counts reported as
  unavailable rather than as zero
