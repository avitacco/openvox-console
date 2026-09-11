## ADDED Requirements

### Requirement: Endpoints require authentication, scoped by action
The system SHALL require a valid, unexpired, unrevoked access token on
every endpoint in this capability: read endpoints (listing or viewing a
group) require the `classifier:read` permission; write endpoints
(creating, updating, or deleting a group) require `classifier:write`.

#### Scenario: Unauthenticated request rejected
- **WHEN** any request to a group endpoint has no valid access token
- **THEN** the system rejects the request

#### Scenario: Read permission does not grant write access
- **WHEN** a request to create, update, or delete a group presents a
  valid access token carrying `classifier:read` but not
  `classifier:write`
- **THEN** the system rejects the request

#### Scenario: Authenticated request with the required permission succeeds
- **WHEN** a request presents a valid access token carrying the
  permission its action requires
- **THEN** the system processes the request as before
