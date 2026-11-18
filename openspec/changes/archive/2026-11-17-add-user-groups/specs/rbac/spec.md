## ADDED Requirements

### Requirement: Permissions come from effective roles
The system SHALL compute a user's permissions, node group scopes and task
grants from their effective roles (their direct roles and the roles of
their user groups), so that a role held through a group behaves exactly
like one assigned directly.

#### Scenario: A permission through a group
- **WHEN** a user's only role comes from a group
- **THEN** their access token carries that role's permissions and node
  scopes

#### Scenario: A role revoked from the user directly
- **WHEN** a direct assignment of a role is removed while a group still
  grants it
- **THEN** the user keeps the role, and the Users page shows it as coming
  from the group
