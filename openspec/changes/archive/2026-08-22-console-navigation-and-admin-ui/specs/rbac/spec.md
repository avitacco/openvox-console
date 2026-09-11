## ADDED Requirements

### Requirement: Console UI for user management
The system SHALL provide a UI, restricted to users with `rbac:admin`, to
list users, create a user (username and password), assign or unassign
roles on a user, and delete a user.

#### Scenario: Viewing the user list
- **WHEN** an administrator opens the user management page
- **THEN** the system displays every configured user with their assigned
  roles

#### Scenario: Creating a user via the UI
- **WHEN** an administrator submits the create-user form with a username
  and password
- **THEN** the new user appears in the user list and can log in with those
  credentials

#### Scenario: Assigning a role via the UI
- **WHEN** an administrator assigns a role to a user from the user
  management page
- **THEN** that user's next login's access token includes the role's
  permissions

#### Scenario: Deleting a user via the UI
- **WHEN** an administrator deletes a user from the user management page
- **THEN** that user no longer appears in the user list and can no longer
  log in

### Requirement: Console UI for role management
The system SHALL provide a UI, restricted to users with `rbac:admin`, to
list roles, create a role (name and permission set), edit an existing
role's permissions, and delete a role.

#### Scenario: Viewing the role list
- **WHEN** an administrator opens the role management page
- **THEN** the system displays every configured role with its permission
  set

#### Scenario: Creating a role via the UI
- **WHEN** an administrator submits the create-role form with a name and
  one or more permissions
- **THEN** the new role appears in the role list and can be assigned to a
  user

#### Scenario: Editing a role's permissions via the UI
- **WHEN** an administrator changes an existing role's permission set
- **THEN** any user assigned that role has the updated permissions on
  their next login

#### Scenario: Deleting a role via the UI
- **WHEN** an administrator deletes a role from the role management page
- **THEN** that role no longer appears in the role list or in any user's
  assigned roles

### Requirement: Console UI for service token management
The system SHALL provide a UI, restricted to users with `rbac:admin`, to
list service tokens (name, permissions, creation date - never the token
value after creation), create a service token (name and permission set),
and delete (revoke) a service token.

#### Scenario: Viewing the service token list
- **WHEN** an administrator opens the service token management page
- **THEN** the system displays every service token's name, permissions,
  and creation date, without its token value

#### Scenario: Creating a service token via the UI
- **WHEN** an administrator submits the create-service-token form with a
  name and one or more permissions
- **THEN** the new token's value is displayed once, and the token
  authenticates requests carrying those permissions

#### Scenario: Deleting a service token via the UI
- **WHEN** an administrator deletes a service token from the service token
  management page
- **THEN** that token no longer appears in the list and is rejected on any
  subsequent request
