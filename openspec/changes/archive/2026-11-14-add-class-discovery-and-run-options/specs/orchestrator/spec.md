## ADDED Requirements

### Requirement: Puppet run options
The system SHALL accept, with a request to run Puppet, the options
`noop` (report what would change and change nothing), `environment`
(ask the node to run against the named environment) and `debug`
(debug-level output), in any combination. A request with no options
SHALL behave exactly as a run did before options existed. Options SHALL
NOT alter the permission a run needs, the node scope check, or its
all-or-nothing treatment of targets.

#### Scenario: A no-op run
- **WHEN** an operator triggers a run with `noop` against a connected
  node
- **THEN** the node performs a no-op Puppet run and nothing on it is
  changed

#### Scenario: A run against a named environment
- **WHEN** an operator triggers a run with an `environment`
- **THEN** the node is asked to run against that environment

#### Scenario: A run with no options
- **WHEN** an operator triggers a run with no options
- **THEN** the node receives the same request as before options existed,
  whatever version its client is

#### Scenario: An invalid environment name
- **WHEN** a run is requested with an `environment` that is not a valid
  environment name
- **THEN** the request is refused as invalid, and no job is created

#### Scenario: An environment the server does not have
- **WHEN** a run is requested with an `environment` the server is known
  not to have
- **THEN** the request is refused as invalid, naming the environment,
  and no job is created

#### Scenario: A target outside the caller's node groups
- **WHEN** a run with options names a node outside the caller's groups
  for `orchestrator:run`
- **THEN** the request is refused exactly as a run without options is

### Requirement: A run with options is never carried out as a plain run
The system SHALL send a run with options as a request that a node agent
client predating run options rejects. A target whose client rejects it
SHALL be recorded as failed with a reason saying its client must be
upgraded, and SHALL NOT have run Puppet.

#### Scenario: A no-op run sent to an older client
- **WHEN** a no-op run targets a node whose client predates run options
- **THEN** that target fails with a reason naming the client upgrade,
  and no Puppet run takes place on the node

#### Scenario: Mixed client versions
- **WHEN** a run with options targets one node with a current client
  and one with an older client
- **THEN** the first runs with the options and the second fails as
  above

### Requirement: Run options are recorded and shown
The system SHALL record a run's options on its job and return them with
the job. The jobs list and the job page SHALL show them, and the
activity log entry for the run SHALL name them. A run with options
SHALL record the agent's output for each target on the job.

#### Scenario: Viewing a no-op job
- **WHEN** a user views a job that was a no-op run
- **THEN** the job is shown as a no-op run, in the list and on its page

#### Scenario: Reading what a no-op run would change
- **WHEN** a no-op run finishes on a node with pending changes
- **THEN** the job page shows, for that node, output listing what would
  have changed

#### Scenario: Reading a debug run
- **WHEN** a run with `debug` finishes
- **THEN** the job page shows the debug output for each node, truncated
  with a notice when it exceeds the stored limit

#### Scenario: The activity log
- **WHEN** a no-op run against environment `feature_x` is triggered
- **THEN** its activity entry says it was a no-op run against
  `feature_x`

### Requirement: A requested environment that was not used is marked
The system SHALL, for a run that requested an environment, mark each
target whose resulting report records a different environment, naming
both. The run dialog SHALL state that a node whose classification sets
an environment runs in that one.

#### Scenario: Classification sets another environment
- **WHEN** a run requests `feature_x` on a node whose classification
  sets `production`, and the node's report records `production`
- **THEN** the job page marks that node as having run in `production`
  rather than `feature_x`

#### Scenario: The requested environment was used
- **WHEN** a run requests `feature_x` and the node's report records
  `feature_x`
- **THEN** no mark is shown for that node

#### Scenario: No report yet
- **WHEN** a target of such a run has no correlated report
- **THEN** no mark is shown for that node

### Requirement: Console UI for run options
The system SHALL offer the run options wherever the console starts a
Puppet run on chosen nodes - the nodes page, a node's page and a group's
page - in a dialog that names the nodes or their count, and SHALL start
a plain run when none is chosen.

#### Scenario: Running with no options chosen
- **WHEN** a user opens the run dialog and confirms without choosing an
  option
- **THEN** a plain run is started

#### Scenario: Choosing no-op
- **WHEN** a user ticks no-op and confirms
- **THEN** a no-op run is started and the user is taken to, or linked
  to, its job

#### Scenario: Choosing an environment
- **WHEN** a user opens the environment choice
- **THEN** the server's environments are offered, with a note that a
  node's classified environment takes precedence
