# Spec Delta

## Purpose

Keeps node-agent-client current across the fleet: the console knows which
client build each node runs, upgrades clients older than the build it
serves, and shows operators which nodes are behind and why.

## ADDED Requirements

### Requirement: Client build tracking
The system SHALL ask each node for its client's build identity when the
node connects to the node transport, record the answer per node, and keep
the most recent answer until the node reports again. A node whose client
cannot answer SHALL be recorded as running an unknown build.

#### Scenario: A node connects
- **WHEN** a node connects to the node transport
- **THEN** the console records that node's client build, version,
  platform and install method, and when it was reported

#### Scenario: A client that predates build reporting connects
- **WHEN** a node whose client does not support build reporting connects
- **THEN** the console records the node's client build as unknown, and
  the node keeps working for runs and every request it already supported

### Requirement: Out-of-date determination
The system SHALL treat a node's client as out of date when its build is
unknown, or when it differs from and was built earlier than the client
build this console serves for the node's platform. A client built later
than the one this console serves SHALL NOT be treated as out of date.

#### Scenario: Client matches the console's build
- **WHEN** a node reports the same client build the console serves for
  its platform
- **THEN** the node is current

#### Scenario: Client is older than the console's build
- **WHEN** a node reports a client build that was built earlier than the
  one the console serves for its platform
- **THEN** the node is out of date

#### Scenario: Client is newer than the console's build
- **WHEN** a node reports a client build that was built later than the
  one this console serves, as happens while a console cluster is part-way
  through a deployment or after a console rollback
- **THEN** the node is not out of date and is not offered an upgrade

#### Scenario: The console's own client build carries no build time
- **WHEN** the console was built without a stamped client build, as in a
  development build
- **THEN** no node is treated as out of date and no upgrade is offered

### Requirement: Automatic upgrade of out-of-date clients
The system SHALL, while automatic upgrades are enabled, instruct each
connected node whose client is out of date and supports upgrading to
upgrade to the client build the console serves for its platform, without
an operator acting on that node. The instruction SHALL carry the
artifact's digest over the mutually authenticated node transport.

#### Scenario: An out-of-date node connects
- **WHEN** automatic upgrades are enabled and a node with an out-of-date,
  upgrade-capable client connects
- **THEN** the console instructs it to upgrade, and after the client
  restarts the node reports the console's build and is current

#### Scenario: A console deployment brings a newer client build
- **WHEN** the console is replaced by a version serving a newer client
  build and its nodes reconnect
- **THEN** each connected upgrade-capable node is upgraded without anyone
  running the install script on it

#### Scenario: Many nodes are out of date at once
- **WHEN** a large number of nodes become out of date together
- **THEN** the console upgrades a bounded number at a time rather than
  instructing all of them at once

#### Scenario: Several console instances serve the same fleet
- **WHEN** more than one console instance is running
- **THEN** a node is instructed to upgrade by one instance at a time,
  never concurrently by two

### Requirement: Upgrades do not interrupt work
The system SHALL NOT upgrade a client while that client is executing a
run, a task, or any other request, and SHALL try again later instead.

#### Scenario: The node is busy when its upgrade is due
- **WHEN** an out-of-date node is executing a request when the console
  would instruct it to upgrade
- **THEN** the request in progress completes undisturbed and the upgrade
  is attempted again later

#### Scenario: The build of a busy node is still known
- **WHEN** a node is executing a request when the console asks for its
  client build
- **THEN** the console records the build and reports whether the node
  is out of date, without waiting for the request to finish

### Requirement: Upgrade confirmation and failure handling
The system SHALL consider an upgrade successful only when the node
subsequently reports the build it was instructed to install. When a
client refuses or fails an upgrade, or does not report the new build
within a bounded time, the system SHALL record the failure and its
reason for that node, retry with increasing delay, and stop retrying
automatically after a bounded number of attempts at the same build. A
failed upgrade SHALL leave the node's existing client running.

#### Scenario: The upgrade succeeds
- **WHEN** a node reports the instructed build after upgrading
- **THEN** the node is current and any earlier failure recorded for it is
  cleared

#### Scenario: The client reports that the upgrade failed
- **WHEN** a client answers an upgrade instruction with a failure, such
  as a digest mismatch or a failed download
- **THEN** the console records the reason against the node, the node
  keeps running its existing client, and the next attempt waits longer
  than the last

#### Scenario: The node never reports the new build
- **WHEN** a client accepted an upgrade but still reports its old build,
  or has not reconnected, when the confirmation time runs out
- **THEN** the console records the upgrade as failed

#### Scenario: Attempts are exhausted
- **WHEN** upgrading a node to the same build has failed the maximum
  number of times
- **THEN** the console stops attempting it automatically until the
  console serves a different build or an operator retries it

### Requirement: Operator control of upgrades
The system SHALL provide a console-wide setting, enabled by default, that
turns automatic upgrades on or off, and SHALL let a user holding the
node-management permission start or retry an upgrade of a single node
regardless of that setting. Build tracking and out-of-date reporting
SHALL continue while automatic upgrades are off.

#### Scenario: Automatic upgrades are turned off
- **WHEN** the console is configured with automatic upgrades off
- **THEN** no node is upgraded automatically, and out-of-date nodes are
  still reported as out of date

#### Scenario: An operator upgrades one node
- **WHEN** a user with the node-management permission requests an upgrade
  of a connected, out-of-date, upgrade-capable node
- **THEN** the console instructs that node to upgrade, including after
  its automatic attempts were exhausted

#### Scenario: An operator without permission requests an upgrade
- **WHEN** a user without the node-management permission requests an
  upgrade of a node
- **THEN** the request is refused and no instruction is sent

#### Scenario: The requested node cannot be upgraded
- **WHEN** an upgrade is requested for a node that is not connected, is
  already current, or whose client does not support upgrading
- **THEN** the request is refused with a message saying which of those
  applies

#### Scenario: The requested node is busy
- **WHEN** an upgrade is requested for a node that is executing a run,
  a task, or another request
- **THEN** the request is refused at once with a message saying the
  node is busy, the work in progress continues undisturbed, and nothing
  is recorded as a failed attempt

### Requirement: Fleet upgrade visibility
The system SHALL show, in the fleet status view, how many nodes run a
current client, how many are out of date, how many have a failed
upgrade, and whether automatic upgrades are enabled, and SHALL identify
nodes whose client can only be upgraded by re-running the install
script.

#### Scenario: Viewing fleet status with stale clients
- **WHEN** a user opens the fleet status view while some nodes are out of
  date
- **THEN** it shows the counts of current, out-of-date and failed nodes
  and whether automatic upgrades are enabled

#### Scenario: Nodes that need a manual upgrade
- **WHEN** some nodes run a client that does not support upgrading
- **THEN** the fleet status view says how many, and that re-running the
  install script on them is required once
