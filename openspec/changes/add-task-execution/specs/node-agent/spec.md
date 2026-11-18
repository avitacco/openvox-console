## MODIFIED Requirements

### Requirement: Executes a task request
The system SHALL, on receiving a task request carrying a resolved task
and its parameters, execute that task on the node itself - without
relying on any Puppet subcommand to run tasks - and report its outcome
back the same way a run's outcome is reported, including the task's
structured result.

#### Scenario: A task request runs with its parameters
- **WHEN** the client receives a task request naming a task and a set
  of parameters
- **THEN** it executes that task with those parameters and reports the
  result

#### Scenario: A task runs on a node with only the standard agent
- **WHEN** the client receives a task request on a node with the
  OpenVox agent installed and nothing else task-specific
- **THEN** the task runs, with no further software required on the
  node

## ADDED Requirements

### Requirement: Task files are fetched from the node's Puppet server
The system SHALL download a task's files from the Puppet server the
node's own agent is configured to use, authenticating with the node's
existing certificate, and SHALL verify each file's SHA-256 against the
checksum in the request before running anything. Files already
downloaded with a matching checksum SHALL be reused rather than fetched
again.

#### Scenario: Files are downloaded and verified
- **WHEN** the client receives a task request whose files it does not
  already hold
- **THEN** it downloads them from its Puppet server and runs the task
  only after every file's SHA-256 matches the request

#### Scenario: A checksum does not match
- **WHEN** a downloaded file's SHA-256 differs from the request's
- **THEN** the client runs nothing and reports failure, naming the file
  whose checksum did not match

#### Scenario: Files are already held
- **WHEN** the client receives a task request whose files it previously
  downloaded and verified, with unchanged checksums
- **THEN** it runs the task without downloading them again

#### Scenario: The Puppet server cannot be reached
- **WHEN** the client cannot download a task's files
- **THEN** it reports failure with the download error, and runs nothing

### Requirement: Task implementation is chosen for the node
The system SHALL choose, from the task's implementations, the first
whose requirements the node meets, where a Windows node provides
`powershell` and every other node provides `shell`, and every node
provides `puppet-agent`. A task with no implementation the node meets
SHALL fail without running anything.

#### Scenario: Choosing between shell and PowerShell
- **WHEN** a task offers a `shell` and a `powershell` implementation,
  and the client runs on Windows
- **THEN** the PowerShell implementation runs

#### Scenario: No suitable implementation
- **WHEN** a task offers only a `powershell` implementation, and the
  client runs on Linux
- **THEN** the client reports failure stating that no implementation
  suits the node, and runs nothing

### Requirement: Task input is passed by the task's input method
The system SHALL pass a task's parameters by its declared input method:
`stdin` as a JSON object on standard input, `environment` as `PT_`
prefixed environment variables, `powershell` as named PowerShell
parameters, and `both` (or none declared) as stdin and environment
together. A task whose metadata declares supporting files SHALL also
receive an `_installdir` parameter naming where they were placed.

#### Scenario: Default input method
- **WHEN** a task with no declared input method runs with parameter
  `name` set to `web`
- **THEN** the task receives `{"name":"web"}` on standard input and
  `PT_name=web` in its environment

#### Scenario: PowerShell input method
- **WHEN** a PowerShell task with the `powershell` input method runs
  with parameter `name` set to `web`
- **THEN** the script receives `-name web` as a named parameter

### Requirement: Task execution is bounded by a timeout
The system SHALL stop a task that runs longer than the request's
timeout, terminating its process and any processes it started, and
report failure stating the task timed out.

#### Scenario: A task exceeds its timeout
- **WHEN** a task is still running when its timeout elapses
- **THEN** the client terminates it and reports a timeout failure,
  and is free to accept a new request

### Requirement: Task output is returned as a structured result
The system SHALL report a task's standard output as a JSON object when
it parses as one, and otherwise as the text under an `_output` key,
together with the exit code and standard error. A result containing an
`_error` key, or a non-zero exit code, SHALL be reported as a failure.

#### Scenario: JSON output
- **WHEN** a task prints `{"status":"ok"}` and exits zero
- **THEN** the client reports success with result `{"status":"ok"}`

#### Scenario: Plain-text output
- **WHEN** a task prints `done` and exits zero
- **THEN** the client reports success with result `{"_output":"done"}`

#### Scenario: Error output
- **WHEN** a task prints a JSON object with an `_error` key and exits
  non-zero
- **THEN** the client reports failure, with that object as the result
