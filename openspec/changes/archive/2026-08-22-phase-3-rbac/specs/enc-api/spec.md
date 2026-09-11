## ADDED Requirements

### Requirement: ENC endpoint requires a scoped service token
The system SHALL require a valid, unexpired, unrevoked access token
carrying the `enc:read` permission on the ENC classification endpoint.
This is expected to be a service token (see the `rbac` capability) issued
to the exec-terminus bridge, not a user login.

#### Scenario: Unauthenticated request rejected
- **WHEN** a request to the ENC endpoint has no valid access token
- **THEN** the system rejects the request

#### Scenario: A service token with the required permission succeeds
- **WHEN** the exec-terminus bridge presents a valid service token
  carrying `enc:read`
- **THEN** the system processes the classification request as before
