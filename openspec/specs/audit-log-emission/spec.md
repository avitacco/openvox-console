# audit-log-emission Specification

## Purpose

Reliably emits a complete, structured stream of audit-relevant events - authentication, administrative changes, and (where enabled) read access - to a log sink the operator controls, so an organization can route it into whatever compliance/SIEM infrastructure they already run. Storage, retention, and export of that stream are explicitly the operator's responsibility, not this capability's.

## Requirements

### Requirement: Independently configurable audit level per category
The system SHALL support an audit level of `off`, `writes`, or `full`, configured independently for each capability category (nodes/inventory, classifier/groups, rbac/users, auth, code deploys, orchestrator jobs, vulnerabilities). `off` emits nothing for that category. `writes` emits mutating actions and authentication events for that category. `full` emits everything `writes` does, plus read/view access to that category's data.

#### Scenario: A category left at the default emits writes and auth events only
- **WHEN** a category's audit level is `writes`
- **THEN** a mutating action or authentication event in that category is emitted, and a read-only view of that category's data is not

#### Scenario: A category disabled entirely emits nothing
- **WHEN** a category's audit level is `off`
- **THEN** no action in that category, mutating or not, produces an audit event

#### Scenario: A category raised to full also emits read access
- **WHEN** a category's audit level is `full`
- **THEN** a read-only view of that category's data produces an audit event in addition to its mutating actions

#### Scenario: Categories are configured independently
- **WHEN** one category's audit level is set to `full` and another's is set to `off`
- **THEN** each category's emission behavior reflects only its own configured level, unaffected by the other

#### Scenario: Vulnerability provider changes are audited at the default level
- **WHEN** the vulnerabilities category's audit level is `writes` and an administrator creates, updates, enables, disables, or deletes a vulnerability provider, or starts a manual sync
- **THEN** an audit event records the action, the acting identity, and the affected provider, without including any credential value

### Requirement: Structured audit event emission
The system SHALL emit each audit event as a single structured (JSON) record containing at minimum a stable event/action identifier, the acting identity, the category, a timestamp, and structured identifiers for the affected resource. A change to existing data SHALL include the before and after values where the underlying store makes them available.

#### Scenario: An emitted event is machine-parseable
- **WHEN** any audit event is emitted
- **THEN** it is a single structured record carrying an action identifier, actor, category, timestamp, and resource identifiers, not a free-text-only message

#### Scenario: A permission change records what changed
- **WHEN** a role's permissions are modified
- **THEN** the emitted event includes the permission set before and after the change

### Requirement: Configurable emission destination
The system SHALL emit audit events to its standard log output by default, and SHALL support configuring a separate destination so audit events can be physically separated from ordinary operational log output at the source.

#### Scenario: Default destination
- **WHEN** no separate audit output is configured
- **THEN** audit events are emitted through the same log output as the rest of the application

#### Scenario: Configured separate destination
- **WHEN** a separate audit output destination is configured
- **THEN** audit events are emitted there instead of the standard log output

### Requirement: Authentication events are audited
The system SHALL emit an audit event for a successful login, a failed login attempt, and a logout, subject to the auth category's configured level.

#### Scenario: A successful login is audited
- **WHEN** a user successfully logs in and the auth category's level is `writes` or `full`
- **THEN** an audit event records the successful login and the identity that logged in

#### Scenario: A failed login attempt is audited
- **WHEN** a login attempt fails and the auth category's level is `writes` or `full`
- **THEN** an audit event records the failed attempt

#### Scenario: A logout is audited
- **WHEN** a user logs out and the auth category's level is `writes` or `full`
- **THEN** an audit event records the logout

### Requirement: Self-service and administrative profile changes are audited
The system SHALL emit an audit event when a user changes their own profile or password, and when an administrator changes another user's account, subject to the rbac/users category's configured level.

#### Scenario: A self-service profile change is audited
- **WHEN** a user updates their own profile or password and the rbac/users category's level is `writes` or `full`
- **THEN** an audit event records the change and the acting identity, without recording the password itself

#### Scenario: An administrative edit to an existing user is audited
- **WHEN** an administrator changes another user's account and the rbac/users category's level is `writes` or `full`
- **THEN** an audit event records the change, the acting administrator, and the affected user

### Requirement: Read access is audited only at the full level
The system SHALL emit an audit event for a read/view of a category's data only when that category's configured level is `full`; at `writes` or `off`, read access SHALL NOT produce an audit event.

#### Scenario: Viewing data at the writes level produces no event
- **WHEN** a user views data in a category whose level is `writes`
- **THEN** no audit event is emitted for that view

#### Scenario: Viewing data at the full level produces an event
- **WHEN** a user views data in a category whose level is `full`
- **THEN** an audit event records the view, the identity, and the resource viewed
