## ADDED Requirements

### Requirement: Executes a run request with options
The system SHALL, on receiving a run-with-options request, execute a
Puppet run on the node with the requested options - no-op, environment
and debug - and report the outcome with the run's output. It SHALL pass
the environment to Puppet as a single argument and SHALL refuse a
request whose environment is not a valid environment name or that
carries an option it does not know, without running Puppet. A plain run
request SHALL be executed as before.

#### Scenario: A no-op request
- **WHEN** the client receives a run-with-options request with no-op set
- **THEN** it runs Puppet in no-op mode and reports the outcome with
  output listing what would have changed

#### Scenario: An environment request
- **WHEN** the request names an environment
- **THEN** the client runs Puppet asking for that environment

#### Scenario: A debug request
- **WHEN** the request sets debug
- **THEN** the client runs Puppet with debug output and returns it,
  truncated with a notice past the output limit

#### Scenario: An invalid environment name
- **WHEN** the request's environment contains anything but letters,
  digits and underscores
- **THEN** the client reports failure without running Puppet

#### Scenario: An unknown option
- **WHEN** the request carries an option the client does not know
- **THEN** the client reports failure naming the option, without
  running Puppet

#### Scenario: A plain run request
- **WHEN** the client receives a plain run request
- **THEN** it runs Puppet exactly as it did before run options existed
