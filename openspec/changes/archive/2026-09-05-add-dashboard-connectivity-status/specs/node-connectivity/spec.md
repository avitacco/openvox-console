## ADDED Requirements

### Requirement: Dashboard connectivity summary
The system SHALL display, on the Dashboard, a count of nodes currently
holding a live node transport connection ("Connected") and a count of
known nodes currently without one ("Disconnected"), alongside the
Dashboard's existing fleet report-status summary.

#### Scenario: Viewing the Dashboard with connected and disconnected nodes
- **WHEN** a user opens the Dashboard and the console has connectivity
  data for one or more nodes
- **THEN** the system displays a "Connected" count of nodes currently
  holding a live node transport connection and a "Disconnected" count
  of known nodes currently without one

#### Scenario: Viewing the Dashboard with node transport disabled
- **WHEN** a user opens the Dashboard and node transport connectivity
  reporting is not configured
- **THEN** the system displays a "Connected" count of zero and a
  "Disconnected" count of zero, rather than omitting the summary or
  showing an error
