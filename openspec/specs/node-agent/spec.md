# node-agent Specification

## Purpose

Runs on a managed node and is what actually executes an on-demand run,
task, or plan step the console requests, connecting to the console's
NATS-based node transport.

## Requirements

### Requirement: Connects using the node's existing Puppet certificate
The system SHALL authenticate to the console's node transport using the
certificate and private key already issued to the node by its normal
Puppet agent enrollment, without performing any separate certificate
request or enrollment step of its own.

#### Scenario: Starting on a node with an existing signed certificate
- **WHEN** the client starts on a node that has already completed
  normal Puppet agent certificate enrollment
- **THEN** it connects to the configured node transport using that
  certificate and key, with no additional enrollment step

#### Scenario: Starting on a node with no certificate yet
- **WHEN** the client starts on a node that has not yet completed
  Puppet agent enrollment (no certificate present)
- **THEN** it fails to start with an error identifying the missing
  certificate, rather than attempting to request one itself

### Requirement: Automatic reconnection
The system SHALL maintain a persistent connection to the configured
node transport, reconnecting automatically (with backoff) if the
connection is lost, so a transient network issue or console restart
does not require manual intervention on the node.

#### Scenario: Node transport connection is lost and recovers
- **WHEN** the client's connection to the node transport drops and it
  becomes reachable again
- **THEN** the client reconnects without requiring a restart or manual
  action on the node

### Requirement: Executes an on-demand run request
The system SHALL, on receiving a run request over its node transport
connection, execute a Puppet run on the node and report the outcome
(success/failure, exit code, and relevant output) back over that
connection.

#### Scenario: A run request completes successfully
- **WHEN** the client receives a run request
- **THEN** it executes a Puppet run on the node and reports success
  with the run's outcome once it finishes

#### Scenario: A run request fails
- **WHEN** a Puppet run triggered by a request fails
- **THEN** the client reports failure with enough detail (exit code,
  error output) to distinguish it from a successful run

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

### Requirement: Single in-flight request per client
The system SHALL execute at most one request at a time per client,
rejecting (rather than silently queuing or dropping) a new request that
arrives while one is already running. A build-identity request is the
exception: it changes nothing on the node, is answered whatever else is
running, and does not count as a request in progress.

#### Scenario: A second request arrives while one is running
- **WHEN** a run or task request arrives while the client is still
  executing a previous one
- **THEN** the client rejects the new request with a response
  indicating it is busy, rather than queuing it silently or dropping
  the in-progress one

#### Scenario: The rejection is immediate
- **WHEN** a request arrives while the client is executing a
  long-running task
- **THEN** the busy response is sent straight away, not when the task
  finishes, and the rejected request is never executed later

### Requirement: Executes a package-inventory toggle request
The system SHALL, on receiving a package-inventory toggle request, set
whether the node reports package inventory (by writing or removing its
`package_inventory` external fact accordingly) and, when a change was
made, immediately run a Puppet run so the effect is reflected without
waiting for the node's next scheduled run - and report the resulting
state back over the connection. The system SHALL also support a
status-only request that reports the current state without changing it
or triggering a run.

#### Scenario: Enabling reporting on a node that does not yet report it
- **WHEN** the client receives a toggle request to enable package
  inventory reporting, and the fact is not currently present
- **THEN** it writes the `package_inventory` external fact, runs a
  Puppet run, and reports that reporting is now enabled

#### Scenario: Disabling reporting on a node that currently reports it
- **WHEN** the client receives a toggle request to disable package
  inventory reporting, and the fact is currently present
- **THEN** it removes the `package_inventory` external fact, runs a
  Puppet run, and reports that reporting is now disabled

#### Scenario: Toggling to the state it is already in is a no-op run
- **WHEN** the client receives a toggle request for the state package
  inventory reporting is already in
- **THEN** it makes no filesystem change, does not trigger a Puppet
  run, and reports the current (unchanged) state

#### Scenario: A status request reports state without side effects
- **WHEN** the client receives a status-only request
- **THEN** it reports whether the `package_inventory` external fact is
  currently present, without writing, removing, or triggering a run

#### Scenario: A toggle request arrives while another request is running
- **WHEN** a package-inventory toggle or status request arrives while
  the client is already executing a run, task, or another
  package-inventory request
- **THEN** the client rejects the new request with a response
  indicating it is busy, per the existing single-in-flight-request
  constraint

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

### Requirement: Reports its build identity
The system SHALL answer a build-identity request with a digest of the
client's own executable, the client's build time and version, the
operating system and architecture it runs on, how it was installed
(a Linux package format, or a directly installed binary), and whether it
will accept upgrades.

#### Scenario: The console asks for the build identity
- **WHEN** the client receives a build-identity request
- **THEN** it replies with its executable digest, build time, version,
  operating system, architecture, install method and whether upgrades
  are accepted, without changing anything on the node

#### Scenario: The client is busy when asked
- **WHEN** the client receives a build-identity request while it is
  executing a run, a task, or another request
- **THEN** it replies with its build identity at once, the request in
  progress is unaffected, and answering does not cause a request that
  arrives meanwhile to be rejected as busy

#### Scenario: A client built without a build stamp
- **WHEN** a client built without a stamped build time receives a
  build-identity request
- **THEN** it reports its executable digest with no build time, so the
  console can tell it from a stamped build

### Requirement: Performs a verified upgrade on request
The system SHALL, on receiving an upgrade request naming an artifact, its
size and its digest, install that artifact only after confirming it
matches the request and is a working client of the requested build, and
SHALL reply before restarting, saying whether the upgrade was started or
why it was not. A failed check SHALL leave the installed client
unchanged.

#### Scenario: A valid upgrade request
- **WHEN** the client receives an upgrade request and the downloaded
  artifact matches the requested size and digest
- **THEN** it replies that the upgrade has started, installs the
  artifact, and the service comes back running the requested build

#### Scenario: The artifact does not match its digest
- **WHEN** the downloaded artifact's digest or size differs from the
  request
- **THEN** the client discards it, replies with a failure naming the
  mismatch, and keeps running its existing build

#### Scenario: The artifact cannot be downloaded
- **WHEN** the client cannot download the artifact
- **THEN** it replies with a failure describing the download error and
  keeps running its existing build

#### Scenario: The artifact is not a working client
- **WHEN** the downloaded artifact matches its digest but does not run,
  or does not identify itself as the requested build
- **THEN** the client does not install it and replies with a failure

#### Scenario: Upgrading a package-installed Linux client
- **WHEN** a client installed from a Linux package upgrades
- **THEN** the node's package database records the new package version,
  and the installation completes even though it restarts the client that
  started it

#### Scenario: An install method that cannot be upgraded in place
- **WHEN** the client cannot determine how it was installed, or was
  installed by a method it cannot upgrade
- **THEN** it replies with a failure saying the install script must be
  re-run, and changes nothing

#### Scenario: Upgrade artifacts are kept out of reach of other users
- **WHEN** the client downloads an upgrade artifact
- **THEN** it is stored where only an administrator can read or write it,
  and removed once the upgrade has succeeded or failed

### Requirement: A node can decline upgrades
The system SHALL let a node's own configuration turn off acceptance of
upgrade requests, in which case the client SHALL refuse every upgrade
request and report in its build identity that upgrades are not accepted.

#### Scenario: Upgrades are declined on a node
- **WHEN** a client configured to decline upgrades receives an upgrade
  request
- **THEN** it replies with a refusal, changes nothing, and its build
  identity reports that upgrades are not accepted

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
