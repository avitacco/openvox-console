## ADDED Requirements

### Requirement: A user group is a named set of users
The system SHALL let an administrator (`rbac:admin`) create, rename and
delete user groups, and add and remove members. A group name SHALL be
unique, case-insensitively, between 1 and 64 characters after trimming.
A user MAY belong to any number of groups, and a group MAY have no
members.

#### Scenario: Creating a group
- **WHEN** an administrator creates a group named `web-ops`
- **THEN** it exists with no members and no roles, and the creation is
  audited

#### Scenario: A name already in use
- **WHEN** an administrator creates `Web-Ops` while `web-ops` exists
- **THEN** the request is refused with `409` and nothing changes

#### Scenario: Adding a member twice
- **WHEN** a user already in a group is added to it again
- **THEN** the request succeeds and the membership is unchanged

#### Scenario: Only administrators manage groups
- **WHEN** a caller without `rbac:admin` calls any group route
- **THEN** it is refused with `403`

### Requirement: Roles can be granted to a group
The system SHALL let an administrator grant roles to a group and revoke
them. Every member of the group SHALL hold each granted role.

#### Scenario: A member gains the group's role
- **WHEN** a role `patcher` is granted to `web-ops` and a member next
  refreshes their token
- **THEN** their token carries `patcher`'s permissions, node scopes and
  task grants

#### Scenario: Revoking a role
- **WHEN** `patcher` is revoked from `web-ops`
- **THEN** members hold it no more from their next token refresh, and a
  member who also holds it directly or through another group still does

### Requirement: Effective roles combine direct and group roles
The system SHALL treat a user's roles as the roles assigned to them
directly together with the roles granted to every group they belong to,
each counted once. Everything that reads a user's roles SHALL read these:
their permissions, their node group scopes, their task grants and, for a
schedule they own, the authority it fires with. Each role SHALL keep its
own node groups and task allowlist whichever way the user holds it.

#### Scenario: The same role twice
- **WHEN** a user holds `patcher` directly and through a group
- **THEN** it counts once, and removing the direct assignment leaves the
  group's

#### Scenario: Pairing survives a group
- **WHEN** a user holds a role limited to `patch::*` on the web tier
  directly, and a group they belong to grants a role allowing every task
  on the database tier
- **THEN** they may run patch tasks on the web tier and any task on the
  database tier, and may not run another task on the web tier

#### Scenario: A schedule follows the owner's groups
- **WHEN** a schedule's owner is removed from the group that gave them
  the right to run its task, and the schedule comes due
- **THEN** the firing is skipped, naming the task, as it would be had a
  direct role been removed

#### Scenario: No groups
- **WHEN** no group exists
- **THEN** every user's permissions, scopes and grants are exactly what
  their direct roles give

### Requirement: Deleting groups, users and roles keeps access consistent
The system SHALL remove a deleted user from their groups, remove a
deleted role from every group, and, when a group is deleted, remove its
membership and its role grants. Deleting a group that has members SHALL be refused with `409` and the
member count unless the request carries `confirm=true`.

#### Scenario: Deleting a group with members
- **WHEN** an administrator deletes a group with 3 members
- **THEN** without `confirm=true` the answer is `409` naming 3 members
  and nothing is deleted; with it the group is deleted, and the audit
  entry records the member count. The console asks "3 people will lose
  the roles this group gives them" before sending the confirmation

#### Scenario: Deleting a role
- **WHEN** a role granted to a group is deleted
- **THEN** the group no longer lists it, and members no longer hold it

### Requirement: Groups appear where roles are managed
The Groups page SHALL list groups with their member counts and roles and
let an administrator create, rename, delete, change members and change
roles. The Users page SHALL show each user's groups and, for each role,
whether it is direct or comes from a named group. The Roles page SHALL
show the groups holding each role. `GET /api/v1/users/{id}/roles` SHALL
return each role with `via`: `direct` or the group's name.

#### Scenario: Seeing where a role comes from
- **WHEN** an administrator opens a user who holds `patcher` through
  `web-ops`
- **THEN** the role is listed with "via web-ops" and cannot be removed
  from the user, only from the group

#### Scenario: Everything is translated
- **WHEN** the console is shown in any of its 15 languages
- **THEN** every string on these pages is in that language

### Requirement: Group changes are audited
The system SHALL record, with the actor, each group creation, rename and
deletion (with the member count), each member added or removed, and each
role granted or revoked. No entry SHALL carry a password or token.

#### Scenario: A member is added
- **WHEN** an administrator adds a user to a group
- **THEN** the audit trail shows who added whom to which group

### Requirement: Groups cannot remove the last administrator
The system SHALL refuse, with `409`, to delete a group, revoke a role from
a group or remove a member from a group when the result would leave no
user holding `rbac:admin`.

#### Scenario: The group is the only way
- **WHEN** the only users holding `rbac:admin` hold it through one group,
  and an administrator deletes that group
- **THEN** the request is refused and the group remains
