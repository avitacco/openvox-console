## MODIFIED Requirements

### Requirement: Console UI for user management
The system SHALL provide a UI, restricted to users with `rbac:admin`, to
list users, create a user (username and password), assign or unassign
roles on a user, and delete a user. The list SHALL mark an account that
is locked and when the lock ends, and SHALL let the administrator unlock
it. The create-user and set-password forms SHALL show the password
rules in force.

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

#### Scenario: A locked account in the list
- **WHEN** an administrator opens the user management page while an
  account is locked
- **THEN** that user is marked as locked with the time the lock ends

#### Scenario: Unlocking via the UI
- **WHEN** the administrator unlocks that user from the page
- **THEN** the mark is gone and the user can log in
