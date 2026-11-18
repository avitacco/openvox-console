# Spec Delta

## ADDED Requirements

### Requirement: Client build in node status
The system SHALL include, in the status it reports for each node, the
node's client version and build as last reported, whether that client is
current, out of date or unknown, and the state and reason of its most
recent upgrade attempt, and SHALL show the same on the nodes page and on
the node's own page.

#### Scenario: Listing nodes with a mix of client builds
- **WHEN** a user views the nodes page while some nodes run an
  out-of-date client
- **THEN** each out-of-date node is marked as such, and current nodes are
  not

#### Scenario: Viewing a node with a failed upgrade
- **WHEN** a user opens the page of a node whose last upgrade attempt
  failed
- **THEN** the page shows the client version it runs, that it is out of
  date, and the reason the upgrade failed

#### Scenario: Viewing a node that has never reported a build
- **WHEN** a user views a node that has not connected since build
  tracking was introduced
- **THEN** its client build is shown as not yet reported, rather than as
  out of date

#### Scenario: Retrying from the node page
- **WHEN** a user with the node-management permission views a connected,
  out-of-date, upgrade-capable node
- **THEN** the node page offers an action to upgrade the client now

#### Scenario: A user without node management views an out-of-date node
- **WHEN** a user without the node-management permission views an
  out-of-date node
- **THEN** the client build and its state are shown, and no upgrade
  action is offered
