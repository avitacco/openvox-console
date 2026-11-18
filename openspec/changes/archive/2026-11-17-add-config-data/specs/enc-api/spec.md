## ADDED Requirements

### Requirement: The ENC API serves a node's configuration data
The system SHALL answer `GET /api/v1/enc/{certname}/data` with the node's
merged configuration data as a JSON object, for a caller holding
`enc:read`. A node that matches no group, or whose groups have no data,
SHALL get `{}`. The node's matching groups SHALL be decided as for the
classification route, from the node's current facts.

#### Scenario: Data for a node
- **WHEN** a token holding `enc:read` asks for a node that matches a group
  with data
- **THEN** the answer is that group's data merged with the rest of the
  node's groups'

#### Scenario: No data
- **WHEN** the node matches no group with data
- **THEN** the answer is `{}`

#### Scenario: No permission
- **WHEN** the caller lacks `enc:read`
- **THEN** the answer is `403`
