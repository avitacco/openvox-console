# classifier Specification

## Purpose

Manages node groups - what classes, parameters, and environment apply to
which nodes - arranged as a single tree in which a group contains only
nodes its parent does, and resolves, for any given node, the final merged
classification from every group that applies to it.

## Requirements

### Requirement: Node group management
The system SHALL allow creating, viewing, updating, and deleting node
groups. Each group has a name, a parent group, classes (optionally
parameterized), top-scope parameters, an environment, a match rule with
a match mode and/or a list of explicitly pinned nodes, and an explicit
numeric priority.

#### Scenario: Creating a group
- **WHEN** a group is created with a name, one or more classes, and a
  priority
- **THEN** the group exists and is included in future classification runs

#### Scenario: Creating a group without naming a parent
- **WHEN** a group is created and no parent is given
- **THEN** the group is created as a child of the All Nodes group

#### Scenario: Updating a group's classes
- **WHEN** an existing group's classes are changed
- **THEN** subsequent classification of nodes matching that group reflects
  the updated classes

#### Scenario: Deleting a group
- **WHEN** a group with no child groups is deleted
- **THEN** it no longer affects classification of any node

#### Scenario: Deleting a group that has children is refused
- **WHEN** a request is made to delete a group that still has child
  groups
- **THEN** the system refuses, says the children must be moved or
  deleted first, and leaves the group and its children unchanged

### Requirement: Fact-based group matching
The system SHALL match a node to a group when the node's facts satisfy the
group's match rule and the node also matches the group's parent.

#### Scenario: A node's facts satisfy a group's rule
- **WHEN** classification runs for a node whose facts satisfy a group's
  match rule, and the node matches the group's parent
- **THEN** that group is included among the node's matching groups

#### Scenario: A node's facts do not satisfy a group's rule
- **WHEN** classification runs for a node whose facts do not satisfy a
  group's match rule, and the node is not pinned to that group
- **THEN** that group is not included among the node's matching groups

#### Scenario: A node satisfies the rule but not the parent's
- **WHEN** classification runs for a node whose facts satisfy a group's
  match rule, and the node does not match the group's parent
- **THEN** that group is not included among the node's matching groups

#### Scenario: A group with no rule and no pins
- **WHEN** classification runs for any node against a group, other than
  All Nodes, that has no rule conditions and no pinned nodes
- **THEN** that group is not included among the node's matching groups

### Requirement: Explicit node pinning
The system SHALL match a node to a group when the node is explicitly
pinned to that group and the node also matches the group's parent,
regardless of whether the group's own match rule is satisfied. A pin
overrides the group's own rule; it does not override the group's
ancestors.

#### Scenario: A pinned node matches without satisfying the rule
- **WHEN** classification runs for a node that is explicitly pinned to a
  group, the node's facts do not satisfy that group's match rule, and
  the node matches the group's parent
- **THEN** that group is still included among the node's matching groups

#### Scenario: A pinned node outside the parent does not match
- **WHEN** classification runs for a node that is explicitly pinned to a
  group, and the node does not match the group's parent
- **THEN** that group is not included among the node's matching groups

### Requirement: Classification merge by hierarchy and priority
The system SHALL merge classes, parameters, and environment from every
group matching a node into a single classification result. When matching
groups set conflicting values (the same class parameter, top-scope
parameter, or environment), a descendant SHALL always win over its
ancestors, and explicit priority SHALL decide between groups that are not
ancestor and descendant, with a group ranking no lower than its parent
and All Nodes ranking below every other group.

#### Scenario: Equal rank at different depths
- **WHEN** two matching groups that are not ancestor and descendant have
  both been lifted to the same parent's rank, one sits deeper in the
  tree than the other, and they set different values for the same
  parameter
- **THEN** the resulting classification uses the value from the deeper
  group

#### Scenario: Two lifted siblings
- **WHEN** a parent with priority 10 has two matching children with
  priorities 1 and 2 that set different values for the same parameter
- **THEN** the resulting classification uses the value from the child
  with priority 2

#### Scenario: Higher-priority group wins a conflicting value
- **WHEN** two matching groups that are not ancestor and descendant set
  different values for the same parameter, and neither has an ancestor
  with a higher priority than its own
- **THEN** the resulting classification uses the value from the
  higher-priority group

#### Scenario: A child wins over a lower-priority parent
- **WHEN** a matching group and its matching parent set different values
  for the same parameter, and the child has the higher priority
- **THEN** the resulting classification uses the child's value

#### Scenario: A child wins over a higher-priority parent
- **WHEN** a matching group and its matching parent set different values
  for the same parameter, and the parent has the higher priority
- **THEN** the resulting classification uses the child's value

#### Scenario: A descendant wins over a distant ancestor
- **WHEN** a matching group and a matching ancestor several levels above
  it set different environments
- **THEN** the resulting classification uses the descendant's
  environment

#### Scenario: A child carries its parent's rank against unrelated groups
- **WHEN** a parent with priority 10 has a child with priority 1, an
  unrelated group has priority 5, all three match a node, and the child
  and the unrelated group set different values for the same parameter
- **THEN** the resulting classification uses the child's value, since
  the child ranks no lower than its parent

#### Scenario: Non-conflicting classes are unioned
- **WHEN** two matching groups declare different classes
- **THEN** the resulting classification includes classes from both groups

#### Scenario: A child inherits what it does not set
- **WHEN** a matching parent declares a class and its matching child
  does not mention that class
- **THEN** the resulting classification includes the parent's class

#### Scenario: No matching groups
- **WHEN** classification runs for a node matched by no group other
  than an All Nodes group that sets nothing
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

#### Scenario: A child's count never exceeds its parent's
- **WHEN** a child group's rule is satisfied by nodes its parent does
  not match
- **THEN** those nodes are not counted for the child, and the child's
  count is no greater than its parent's

#### Scenario: All Nodes counts the fleet
- **WHEN** the group list is shown
- **THEN** the All Nodes group's count is the number of nodes in the
  current fleet

#### Scenario: Node data unavailable
- **WHEN** the node data needed to resolve counts cannot be retrieved
- **THEN** the group list is still displayed, with counts reported as
  unavailable rather than as zero

### Requirement: Built-in All Nodes root group
The system SHALL always contain exactly one root group, All Nodes, which
has no parent and matches every node. It SHALL NOT be deletable,
renamable, or movable, and SHALL NOT accept a match rule or pinned
nodes. Its classes, top-scope parameters and environment remain
editable, and apply to every node.

#### Scenario: Every node matches All Nodes
- **WHEN** classification runs for any node, including one whose facts
  are unknown
- **THEN** the All Nodes group is among the node's matching groups

#### Scenario: Deleting All Nodes is refused
- **WHEN** a request is made to delete the All Nodes group
- **THEN** the system refuses and the group is unchanged

#### Scenario: Giving All Nodes a parent, rule or pins is refused
- **WHEN** a request is made to give the All Nodes group a parent, a
  different name, a match rule, or pinned nodes
- **THEN** the system refuses and the group is unchanged

#### Scenario: A class on All Nodes reaches every node
- **WHEN** a class is added to the All Nodes group
- **THEN** every node's classification includes that class

#### Scenario: Existing groups after upgrade
- **WHEN** a deployment with existing groups is upgraded to a version
  with the hierarchy
- **THEN** every existing group is a child of All Nodes, and every
  node's classification is the same as before the upgrade

### Requirement: Groups form a single tree
The system SHALL give every group other than All Nodes exactly one
parent group, and SHALL allow a group's parent to be changed. A change
that would make a group its own ancestor SHALL be refused, as SHALL a
parent that does not exist.

#### Scenario: Moving a group under another
- **WHEN** a group's parent is changed to another existing group that is
  not the group itself or one of its descendants
- **THEN** the group and all of its descendants move with it, and
  subsequent classification reflects the new ancestry

#### Scenario: Moving a group under itself is refused
- **WHEN** a request is made to set a group's parent to the group itself
- **THEN** the system refuses and the group is unchanged

#### Scenario: Moving a group under its own descendant is refused
- **WHEN** a request is made to set a group's parent to one of the
  group's own descendants
- **THEN** the system refuses, says the move would create a loop, and
  the hierarchy is unchanged

#### Scenario: A parent that does not exist is refused
- **WHEN** a group is created or updated naming a parent that does not
  exist
- **THEN** the system refuses the request

### Requirement: Rule match mode
The system SHALL let a group's match rule be satisfied either when all
of its conditions hold or when any one of them holds, as chosen per
group. A group whose mode has never been chosen SHALL use "all".

#### Scenario: All-of rule with one failing condition
- **WHEN** a group's rule uses the "all" mode and a node's facts satisfy
  every condition but one
- **THEN** the rule is not satisfied for that node

#### Scenario: Any-of rule with one passing condition
- **WHEN** a group's rule uses the "any" mode and a node's facts satisfy
  exactly one of its conditions
- **THEN** the rule is satisfied for that node

#### Scenario: Any-of rule with no passing condition
- **WHEN** a group's rule uses the "any" mode and a node's facts satisfy
  none of its conditions
- **THEN** the rule is not satisfied for that node

#### Scenario: Groups that predate the mode
- **WHEN** a deployment with existing groups is upgraded
- **THEN** every existing group's rule uses the "all" mode and matches
  the same nodes as before

#### Scenario: An unknown mode is refused
- **WHEN** a group is saved with a match mode other than "all" or "any"
- **THEN** the system refuses the save

### Requirement: Warning when hierarchy overrides priority
The system SHALL warn when a group sets a value that conflicts with one
set by an ancestor whose priority is higher than the group's own, since
the group's value is used although priority alone would have chosen the
ancestor's. The warning SHALL name the ancestor and the value
concerned. It SHALL NOT prevent the group being saved or any node being
classified.

#### Scenario: Saving a group that overrides a higher-priority ancestor
- **WHEN** a group is saved setting a class parameter that an ancestor
  with a higher priority sets to a different value
- **THEN** the save succeeds, and its response carries a warning naming
  the ancestor and the parameter

#### Scenario: The warning persists while the conflict does
- **WHEN** a group that overrides a higher-priority ancestor is viewed
  later, or listed
- **THEN** the same warning is reported for it

#### Scenario: A change to the ancestor raises the warning
- **WHEN** an ancestor is edited so that it now sets a value one of its
  lower-priority descendants already sets differently
- **THEN** the descendant is reported with the warning, and the
  ancestor's save response says which descendants now override it

#### Scenario: No warning when the child has the higher priority
- **WHEN** a group sets a value that conflicts with an ancestor's, and
  the group's priority is higher than that ancestor's
- **THEN** no warning is reported

#### Scenario: No warning without a conflicting value
- **WHEN** a group has a lower priority than an ancestor but sets no
  value the ancestor also sets, or sets the same value
- **THEN** no warning is reported

#### Scenario: No warning against All Nodes
- **WHEN** a group sets a value that conflicts with one set on the All
  Nodes group
- **THEN** no warning is reported, whatever the priorities

#### Scenario: The warning is recorded
- **WHEN** a group is saved and the save leaves a group overriding a
  higher-priority ancestor
- **THEN** the activity event for that save mentions the override, and
  the server log records it

#### Scenario: Classification is unaffected by the warning
- **WHEN** openvoxserver requests the classification of a node matched
  by a group that carries the warning
- **THEN** the response is a normal classification using the
  descendant's value, with no warning in it

### Requirement: Console UI for the group hierarchy
The system SHALL show node groups as a tree, with each group beneath its
parent, and SHALL let a user choose a group's parent and its rule match
mode when creating or editing it. Parents that would create a loop
SHALL NOT be offered.

#### Scenario: Viewing the hierarchy
- **WHEN** a user opens the group management view
- **THEN** All Nodes is shown at the top, and every other group is shown
  beneath its parent, with its priority and matching node count

#### Scenario: Choosing a parent
- **WHEN** a user creates or edits a group
- **THEN** the form offers the existing groups as the parent, defaulting
  to All Nodes for a new group, and does not offer the group itself or
  its descendants

#### Scenario: Choosing a match mode
- **WHEN** a user edits a group's match rule
- **THEN** the form lets them choose whether a node must satisfy all of
  the conditions or any of them, and shows the group's current choice

#### Scenario: Editing All Nodes
- **WHEN** a user opens the All Nodes group for editing
- **THEN** its classes, parameters and environment can be edited, and
  its name, parent, rule and pinned nodes cannot

#### Scenario: Override warning shown on the group
- **WHEN** a user opens, or has just saved, a group that overrides a
  higher-priority ancestor
- **THEN** the page shows a warning naming the ancestor and the values
  concerned

#### Scenario: Override warning shown in the list
- **WHEN** the group list contains a group that overrides a
  higher-priority ancestor
- **THEN** that group is marked as having a warning

#### Scenario: Deleting a group with children
- **WHEN** a user tries to delete a group that has child groups
- **THEN** the console explains that the children must be moved or
  deleted first, and nothing is deleted

### Requirement: Pinned nodes excluded by an ancestor are indicated
The system SHALL indicate, when a group is viewed, which of its pinned
nodes do not currently match it because they fall outside one of its
ancestors, so that a pin which has no effect is visible rather than
silent.

#### Scenario: A pin outside the parent
- **WHEN** a user views a group that has a pinned node its parent does
  not match
- **THEN** that pinned node is marked as not currently in the group,
  with the reason

#### Scenario: Pins inside the parent
- **WHEN** a user views a group whose pinned nodes all match its parent
- **THEN** no pinned node is marked

#### Scenario: Node data unavailable
- **WHEN** the node data needed to tell cannot be retrieved
- **THEN** the group is still shown, with its pins unmarked and a note
  that their status is unavailable

### Requirement: A group granted to a role cannot be deleted
The system SHALL refuse to delete a node group that any role is granted
on, and SHALL say in the refusal that roles still use the group.

#### Scenario: Deleting a granted group
- **WHEN** a user deletes a node group that a role is granted on
- **THEN** the deletion is refused with a conflict response saying the
  group is granted to one or more roles, and the group is unchanged

#### Scenario: Deleting after the grant is removed
- **WHEN** the group is removed from every role and then deleted
- **THEN** the deletion succeeds

### Requirement: Classes are chosen from the group's environment
The system SHALL let a user edit a group's classes by choosing classes
and their declared parameters from the code of the group's environment,
with value inputs suited to each parameter's type, while keeping the
underlying JSON viewable and editable. The environment used SHALL be
the group's own, else its nearest ancestor's, else `production`.

#### Scenario: Adding a class from the picker
- **WHEN** a user adds a class by choosing it from the list for the
  group's environment and saves
- **THEN** the group's classes include that class, exactly as if it had
  been entered as JSON

#### Scenario: Adding a declared parameter
- **WHEN** a user adds a parameter the class declares and enters a value
- **THEN** saving stores that parameter and value on the class, with a
  Boolean, Integer or Float parameter stored as that JSON type rather
  than as text

#### Scenario: A parameter left unset
- **WHEN** a user adds a class and sets none of its parameters
- **THEN** the class is saved with no parameters, so the code's own
  defaults and Hiera apply

#### Scenario: A group that inherits its environment
- **WHEN** a group sets no environment and its parent sets `staging`
- **THEN** the picker offers the classes of `staging`

#### Scenario: Switching to the JSON view
- **WHEN** a user switches the classes editor to its JSON view
- **THEN** the current classes are shown as JSON, and an edit made there
  is reflected in the structured editor when switched back

#### Scenario: A class or parameter not in the list
- **WHEN** a user enters a class name or parameter name the environment
  does not define
- **THEN** the editor accepts it and flags it, rather than refusing it

#### Scenario: Discovery is unavailable
- **WHEN** the group page is opened while class discovery is unavailable
- **THEN** classes and parameters are entered by name, the page says
  that the list of classes is unavailable and why, and saving works

#### Scenario: Choosing the group's environment
- **WHEN** a user edits the group's environment field
- **THEN** the server's environments are offered, and a name not among
  them is still accepted

#### Scenario: Refreshing the list
- **WHEN** a user with `classifier:write` asks the editor to refresh
  classes
- **THEN** the list is read again from the server and the page shows
  when it was read

### Requirement: Groups flag classes that do not match their environment's code
The system SHALL report, on the group page and as a mark in the groups
list, each class, parameter and value of a group that does not match
the code of its environment. These reports SHALL be advisory: they
SHALL NOT block a save or alter what is stored, and SHALL NOT be made
when discovery is unavailable or stale.

#### Scenario: A class removed from the code
- **WHEN** a group names a class and a later code deploy removes it from
  the group's environment
- **THEN** the group page flags the class as not defined in that
  environment, the groups list marks the group, and the group's stored
  classes are unchanged

#### Scenario: An undeclared parameter
- **WHEN** a group sets a parameter its class does not declare
- **THEN** the group page flags that parameter

#### Scenario: A value of the wrong type
- **WHEN** a group sets a text value on a parameter declared `Integer`
- **THEN** the group page flags the value as not fitting the type

#### Scenario: A type the system cannot check
- **WHEN** a parameter is declared with a type the system does not
  evaluate
- **THEN** no value for it is flagged

#### Scenario: Saving with flags present
- **WHEN** a user saves a group that has flagged classes or parameters
- **THEN** the save succeeds

#### Scenario: Discovery is unavailable
- **WHEN** discovery is unavailable or stale for the group's environment
- **THEN** no class or parameter of the group is flagged
