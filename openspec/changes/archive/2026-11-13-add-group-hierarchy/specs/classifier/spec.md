# Spec Delta

## RENAMED Requirements

- FROM: `### Requirement: Classification merge by explicit priority`
- TO: `### Requirement: Classification merge by hierarchy and priority`

## MODIFIED Requirements

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

## ADDED Requirements

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
