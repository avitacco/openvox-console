## MODIFIED Requirements

### Requirement: Console UI and API for browsing activity history
The system SHALL provide an API and a UI, restricted to users with
`activity:read`, to view the activity log ordered by most recent first.

Each entry SHALL identify who performed the action, so that history
answers "who did this" and not only "what happened". An action with no
user behind it - one taken by a scheduled task or by the system itself -
SHALL be presented without attributing it to a user rather than
attributed to a placeholder.

#### Scenario: Viewing recent activity
- **WHEN** a user with `activity:read` opens the activity page
- **THEN** the system displays recorded events with the most recent first

#### Scenario: An entry records who acted
- **WHEN** a user performs an action that is recorded in the activity log
- **THEN** the recorded entry identifies that user, and the entry is
  displayed with them named

#### Scenario: An entry with no user behind it
- **WHEN** an event is recorded by a scheduled task or by the system
  itself
- **THEN** the entry is displayed without a user attributed to it

#### Scenario: Access without the required permission is rejected
- **WHEN** a request to the activity endpoint does not carry
  `activity:read`
- **THEN** the system rejects the request
