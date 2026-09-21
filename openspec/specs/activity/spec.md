# activity Specification

## Purpose

Records and exposes an audit trail of meaningful actions taken through the
console - who did what, and when - as a single persisted log fed by every
other capability's activity events, rather than each capability keeping
its own audit records.

## Requirements

### Requirement: Activity events are persisted centrally
The system SHALL persist every published activity event (category, action,
actor, a human-readable summary, and when it occurred) to a single activity
log, regardless of which capability published it.

#### Scenario: An event from any capability is recorded
- **WHEN** any capability publishes an activity event
- **THEN** the event appears in the activity log with its category, action,
  actor, summary, and timestamp

#### Scenario: The activity log survives a restart
- **WHEN** the console restarts after activity events have been recorded
- **THEN** previously recorded events are still present in the activity log

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
