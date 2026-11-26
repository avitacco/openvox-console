# task-catalog Specification

## Purpose

Tells the console which Puppet tasks exist in each deployed code
environment and what parameters each accepts, read live from
openvoxserver, so that tasks can be offered, validated and resolved for
execution against the code that is actually deployed.

## Requirements

### Requirement: Listing environments that can run tasks
The system SHALL list the code environments openvoxserver currently
serves, so a task can be run from any deployed environment.

#### Scenario: Listing environments
- **WHEN** a user with `orchestrator:read` requests the list of task
  environments
- **THEN** the system returns every environment openvoxserver reports,
  by name

### Requirement: Listing the tasks in an environment
The system SHALL list the tasks available in a named code environment,
each with its full name (`module::task`, or `module` for a module's
`init` task) and its description when the task's metadata has one. The
list SHALL be read from openvoxserver when requested, not from a stored
copy, so it reflects the code currently deployed.

#### Scenario: Listing tasks in an environment with tasks
- **WHEN** a user with `orchestrator:read` lists tasks in an environment
  whose modules contain tasks
- **THEN** the system returns each of those tasks by full name, with its
  description where the metadata provides one

#### Scenario: A newly deployed task appears without a restart
- **WHEN** a code deploy adds a task to an environment, and a user then
  lists tasks in that environment
- **THEN** the new task is included

#### Scenario: Listing tasks in an unknown environment
- **WHEN** a user lists tasks in an environment openvoxserver does not
  serve
- **THEN** the system responds that the environment was not found,
  rather than returning an empty list

### Requirement: Reading a task's parameters
The system SHALL return a named task's metadata in a named environment:
its description, each parameter's name, type, description, default and
whether it is sensitive, and whether the task supports noop.

#### Scenario: Reading a task with parameters
- **WHEN** a user with `orchestrator:read` requests a task's details
- **THEN** the system returns each parameter with its type, its
  description, its default if one is declared, and whether it is marked
  sensitive

#### Scenario: Reading a task that does not exist
- **WHEN** a user requests a task that does not exist in the named
  environment
- **THEN** the system responds that the task was not found

### Requirement: Validating parameters against a task's metadata
The system SHALL check a set of parameter values against a task's
metadata, rejecting a missing required parameter, a parameter the task
does not declare, and a value that does not match a declared type. A
parameter is required when its type does not accept undef and it has no
default.

#### Scenario: A required parameter is missing
- **WHEN** parameters are validated for a task that declares a
  `String` parameter `name` with no default, and `name` is not given
- **THEN** validation fails, naming `name` as missing

#### Scenario: An undeclared parameter is given
- **WHEN** parameters are validated that include a name the task's
  metadata does not declare
- **THEN** validation fails, naming the undeclared parameter

#### Scenario: A value of the wrong type is given
- **WHEN** a parameter declared `Integer` is given the value `"three"`
- **THEN** validation fails, naming the parameter and its declared type

#### Scenario: An optional parameter is omitted
- **WHEN** a parameter declared `Optional[String]` is not given
- **THEN** validation passes

#### Scenario: A type the system cannot check
- **WHEN** a parameter's declared type is one the system does not
  interpret, such as a type alias defined in a module
- **THEN** any value is accepted for that parameter and left for the
  task to check, rather than the request being rejected

#### Scenario: A task with no declared parameters
- **WHEN** a task's metadata declares no `parameters` key at all
- **THEN** any parameters are accepted, matching how Puppet treats a
  task without parameter metadata

### Requirement: Resolving a task for execution
The system SHALL resolve a named task in a named environment into
everything a node needs to run it: each implementation with its
requirements and the files it uses, each file's download location,
size and SHA-256 checksum, and the input method.

#### Scenario: Resolving a task with one implementation
- **WHEN** a task whose module ships a single script is resolved
- **THEN** the result names that file with its download location,
  size and SHA-256, and the task's input method

#### Scenario: Resolving a task with several implementations
- **WHEN** a task whose metadata lists a shell and a PowerShell
  implementation is resolved
- **THEN** the result lists both, each with its requirements and files

### Requirement: Task catalogue requires configuration
The system SHALL treat the task catalogue as optional: when no
openvoxserver address is configured for it, the console SHALL start
normally and every catalogue request SHALL respond that the task
catalogue is not configured. An unreachable openvoxserver SHALL produce
a reported error, not a crash.

#### Scenario: Catalogue not configured
- **WHEN** a user lists tasks on a console with no openvoxserver address
  configured for the task catalogue
- **THEN** the system responds that the task catalogue is not configured

#### Scenario: openvoxserver unreachable
- **WHEN** a user lists tasks and openvoxserver cannot be reached
- **THEN** the system responds with an error naming openvoxserver as
  unreachable, and keeps serving other requests

### Requirement: Task catalogue requires authentication
The system SHALL require `orchestrator:read` for every task catalogue
endpoint.

#### Scenario: Unauthenticated request
- **WHEN** a request to list tasks carries no valid access token
- **THEN** the system rejects it as unauthenticated

#### Scenario: Missing permission
- **WHEN** a request to list tasks carries a token without
  `orchestrator:read`
- **THEN** the system rejects it as forbidden
