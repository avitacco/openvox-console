# Spec Delta

## ADDED Requirements

### Requirement: Roles are granted on node groups
The system SHALL let each role name zero or more node groups to which
its permissions apply, and SHALL reject a role that names a node group
that does not exist.

#### Scenario: Creating a role on a group
- **WHEN** an administrator creates a role with `nodes:read` and the
  node group "Web Servers"
- **THEN** the role is stored with that group and returned with it

#### Scenario: Creating a role without naming groups
- **WHEN** a role is created through the API with no node groups field
- **THEN** the role is granted on the root group

#### Scenario: A role with an explicitly empty group list
- **WHEN** an administrator saves a role with an empty node group list
- **THEN** the role is stored with no groups and its permissions cover
  no nodes

#### Scenario: Naming a group that does not exist
- **WHEN** a role is saved naming a node group id that does not exist
- **THEN** the request is rejected and the role is unchanged

#### Scenario: Changing a role's groups
- **WHEN** an administrator changes a role's node groups
- **THEN** each user holding the role gets the new scope at their next
  login or token refresh

### Requirement: Existing roles keep fleet-wide access on upgrade
The system SHALL grant the root node group to every role that exists
when this capability is first installed.

#### Scenario: Upgrading a console with roles
- **WHEN** a console with existing roles is upgraded
- **THEN** every existing role is granted on the root group, and each
  user sees the same nodes as before

### Requirement: Access tokens carry node group scope
The system SHALL embed in each user access token, for every permission
it carries, the node groups that permission applies to, derived from
the user's roles when the token is issued or refreshed.

#### Scenario: Login
- **WHEN** a user holding a role with `nodes:read` on "Web Servers"
  logs in
- **THEN** the access token carries `nodes:read` with the scope
  "Web Servers"

#### Scenario: A token issued before the upgrade
- **WHEN** an access token issued before this capability existed is
  presented
- **THEN** it is treated as fleet-wide until it expires

#### Scenario: A scoped group is later deleted
- **WHEN** a token names a node group that no longer exists
- **THEN** that group contributes no nodes to the caller's scope

### Requirement: Callers can read their own node scope
The system SHALL report, from the current-user endpoint, the node
groups of the caller's `nodes:read` scope and whether it is fleet-wide.

#### Scenario: A scoped user
- **WHEN** a user scoped to "Web Servers" requests their own profile
- **THEN** the response names "Web Servers" as their node scope and
  marks it as not fleet-wide

### Requirement: Console UI for role node groups
The system SHALL let an administrator choose a role's node groups in
the role editor from the group tree, defaulting a new role to the root
group, and SHALL show each role's groups in the roles list.

#### Scenario: Creating a role in the console
- **WHEN** an administrator opens the new role form
- **THEN** the root group is preselected and can be replaced with other
  groups

#### Scenario: Roles list
- **WHEN** an administrator views the roles list
- **THEN** each role shows its node groups, or "All Nodes" for a
  fleet-wide role, or a warning for a role with none

#### Scenario: Scoped user on the nodes page
- **WHEN** a user whose node scope is not fleet-wide opens the nodes
  page
- **THEN** the page states which node groups they are seeing
