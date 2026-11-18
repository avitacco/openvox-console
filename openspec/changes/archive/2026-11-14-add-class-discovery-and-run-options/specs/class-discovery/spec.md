## ADDED Requirements

### Requirement: Classes are read from each environment's code
The system SHALL obtain, for an environment, the classes its deployed
code defines from openvoxserver, with each class's parameters and, for
each parameter, its name, its declared Puppet type where one is
declared, and its default where one is declared. A parameter with no
default SHALL be reported as required.

#### Scenario: An environment with classes
- **WHEN** a caller with `classifier:read` requests the classes of an
  environment whose code defines classes
- **THEN** the system returns every class by name, sorted, each with its
  parameters, their types and their defaults

#### Scenario: A parameter with no default
- **WHEN** a class declares a parameter with no default
- **THEN** that parameter is reported as required

#### Scenario: A manifest that does not parse
- **WHEN** one manifest in the environment cannot be parsed
- **THEN** the classes of every other manifest are still returned, and
  the response names the file and its error

#### Scenario: An environment that does not exist
- **WHEN** a caller requests the classes of an environment the server
  does not have
- **THEN** the system answers not found, naming the environment

### Requirement: Environments are listed for classification
The system SHALL list the environments openvoxserver has to a caller
with `classifier:read`.

#### Scenario: Listing environments
- **WHEN** a caller with `classifier:read` requests the environments
- **THEN** the system returns their names, sorted

#### Scenario: A caller without the permission
- **WHEN** a caller without `classifier:read` requests the environments
  or an environment's classes
- **THEN** the request is refused as forbidden

### Requirement: Classes are kept current
The system SHALL reuse an environment's classes for up to five minutes
before reading them again, SHALL read them again after a code deploy
completes, and SHALL read them again at once when a caller with
`classifier:write` asks for a refresh. Every answer SHALL say when its
classes were read.

#### Scenario: A second request within five minutes
- **WHEN** an environment's classes are requested twice within five
  minutes with no deploy or refresh between
- **THEN** the server is asked once

#### Scenario: After a code deploy
- **WHEN** a code deploy completes
- **THEN** the next request for any environment's classes reads them
  from the server again

#### Scenario: A requested refresh
- **WHEN** a caller with `classifier:write` refreshes an environment
- **THEN** the classes are read from the server again and the new
  answer is returned

#### Scenario: Refresh without write permission
- **WHEN** a caller with only `classifier:read` requests a refresh
- **THEN** the request is refused as forbidden

### Requirement: Discovery failure does not hide the last good answer
The system SHALL, when the server cannot be reached and classes for the
environment were read before, return those classes marked stale with the
time they were read. When there is no earlier answer the system SHALL
report that discovery is unavailable and why, distinguishing a console
with no server connection configured, a server that refuses the
console's certificate, and a server that cannot be reached.

#### Scenario: The server goes away after a successful read
- **WHEN** an environment's classes were read and the server then
  becomes unreachable
- **THEN** a later request returns the earlier classes, marked stale

#### Scenario: No server connection is configured
- **WHEN** the console has no openvoxserver connection configured
- **THEN** a request for classes reports discovery as not configured

#### Scenario: The server refuses the console's certificate
- **WHEN** the server answers the class request as forbidden
- **THEN** the system reports that the server does not allow the console
  to read classes, distinctly from the server being unreachable

### Requirement: Discovery never affects classification
The system SHALL NOT change what the ENC answers for any node on account
of what discovery reports or whether it is available.

#### Scenario: A group names a class the environment does not define
- **WHEN** a node matches a group naming a class its environment's code
  does not define
- **THEN** the ENC answer for that node still includes the class

#### Scenario: Discovery is unavailable
- **WHEN** discovery is unavailable
- **THEN** ENC answers and group saves behave exactly as they do when it
  is available
