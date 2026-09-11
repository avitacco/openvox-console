## ADDED Requirements

### Requirement: Endpoints require authentication
The system SHALL require a valid, unexpired, unrevoked access token
carrying the `nodes:read` permission on every endpoint in this capability.

#### Scenario: Unauthenticated request rejected
- **WHEN** a request to the node list or node detail endpoint has no valid
  access token
- **THEN** the system rejects the request

#### Scenario: Authenticated request with the required permission succeeds
- **WHEN** a request to the node list or node detail endpoint presents a
  valid access token carrying `nodes:read`
- **THEN** the system processes the request as before
