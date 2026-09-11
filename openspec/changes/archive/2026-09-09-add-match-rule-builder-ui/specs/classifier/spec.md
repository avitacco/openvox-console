## MODIFIED Requirements

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

## ADDED Requirements

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
