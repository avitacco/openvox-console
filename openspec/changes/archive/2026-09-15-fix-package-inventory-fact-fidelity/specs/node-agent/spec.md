## ADDED Requirements

### Requirement: Keeps an enabled package-inventory fact current
The system SHALL, when it starts, compare the node's `package_inventory`
external fact (if present) against the fact content built into the
running client, and rewrite it in place when they differ, so upgrading
the client delivers package-inventory fact fixes to nodes that already
report package inventory. The refresh SHALL NOT enable reporting on a
node where the fact is absent, and SHALL NOT trigger a Puppet run.

#### Scenario: Starting with a stale fact present
- **WHEN** the client starts on a node whose `package_inventory` external
  fact is present but differs from the content built into the client
- **THEN** it rewrites the fact with the built-in content, without
  triggering a Puppet run, and the node's next Puppet run reports data
  produced by the current fact

#### Scenario: Starting with a current fact present
- **WHEN** the client starts on a node whose `package_inventory` external
  fact already matches the content built into the client
- **THEN** it makes no filesystem change

#### Scenario: Starting with reporting disabled
- **WHEN** the client starts on a node with no `package_inventory`
  external fact
- **THEN** it does not create one, and package-inventory reporting stays
  disabled

#### Scenario: The refresh fails
- **WHEN** the client cannot rewrite a stale fact (for example because
  the facts directory is not writable)
- **THEN** it logs the failure and continues starting and connecting to
  the node transport normally, rather than failing to start
