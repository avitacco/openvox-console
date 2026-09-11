## ADDED Requirements

### Requirement: RBAC changes publish an activity event
The system SHALL publish an activity event whenever a user, role, or
service token is created, updated, or deleted, whenever a role is
assigned to or unassigned from a user, and whenever OIDC login creates a
user or changes a user's OIDC-derived role assignments.

#### Scenario: Creating or deleting a user publishes an event
- **WHEN** an administrator creates or deletes a user
- **THEN** an activity event recording the action, the affected user, and
  the acting administrator is published

#### Scenario: Creating, updating, or deleting a role publishes an event
- **WHEN** an administrator creates, updates, or deletes a role
- **THEN** an activity event recording the action, the affected role, and
  the acting administrator is published

#### Scenario: Assigning or unassigning a role publishes an event
- **WHEN** an administrator assigns or unassigns a role on a user
- **THEN** an activity event recording the grant or revocation, the user,
  the role, and the acting administrator is published

#### Scenario: Creating or revoking a service token publishes an event
- **WHEN** an administrator creates or revokes a service token
- **THEN** an activity event recording the action, the token's name, and
  the acting administrator is published

#### Scenario: OIDC login provisioning or role reconciliation publishes an event
- **WHEN** an OIDC login creates a new local user, or changes a user's
  OIDC-derived role assignments
- **THEN** an activity event recording what changed and the affected user
  is published, with the acting "user" recorded as the system rather than
  an administrator
