## MODIFIED Requirements

### Requirement: Task and plan execution
The system SHALL support running a Puppet task against target nodes and
running a Puppet plan (a sequence of tasks/runs with defined ordering),
tracking each as a job the same way an on-demand run is tracked.

A task request SHALL name the code environment to take the task from,
defaulting to `production` when none is given. Before creating a job,
the system SHALL resolve the task in that environment and validate its
parameters against the task's metadata (see the `task-catalog`
capability), and SHALL reject the request without creating a job when
the task cannot be resolved or its parameters are invalid. The system
SHALL dispatch the resolved task - its implementations, files with
checksums, and input method - rather than only its name. A plan's task
steps SHALL be resolved, validated and dispatched the same way, from
the `production` environment.

#### Scenario: Triggering a task run
- **WHEN** an operator triggers a task with its required parameters
  against a target node
- **THEN** the system dispatches the corresponding task request and
  tracks its completion as a job

#### Scenario: Triggering a task from a named environment
- **WHEN** an operator triggers a task naming the environment
  `staging`
- **THEN** the task is resolved from `staging`, and the job records
  `staging` as its environment

#### Scenario: Triggering an unknown task
- **WHEN** an operator triggers a task that does not exist in the named
  environment
- **THEN** the system rejects the request as invalid, naming the task,
  and creates no job

#### Scenario: Triggering a task with invalid parameters
- **WHEN** an operator triggers a task omitting a required parameter
- **THEN** the system rejects the request as invalid, naming the
  missing parameter, and creates no job

#### Scenario: Triggering a task with no task catalogue configured
- **WHEN** an operator triggers a task on a console with no task
  catalogue configured
- **THEN** the system rejects the request, stating that the task
  catalogue is not configured, and creates no job

#### Scenario: A plan runs its steps in the declared order
- **WHEN** an operator triggers a plan composed of multiple steps
- **THEN** the system executes those steps in the plan's declared order
  and the plan's job is not reported complete until every step has
  finished

#### Scenario: A plan with an invalid task step
- **WHEN** an operator triggers a plan one of whose task steps names a
  task that does not exist
- **THEN** the system rejects the whole plan as invalid and creates no
  job, rather than failing partway through

## ADDED Requirements

### Requirement: Sensitive task parameters are not retained
The system SHALL deliver a parameter marked sensitive in a task's
metadata to the node, and SHALL NOT store its value in the job record,
return it from any job endpoint, write it to the activity or audit log,
or log it. Wherever the job's parameters are shown, a sensitive value
SHALL appear as redacted.

#### Scenario: Running a task with a sensitive parameter
- **WHEN** an operator runs a task whose `password` parameter is marked
  sensitive
- **THEN** the node receives the value, and the job's stored and
  displayed parameters show `password` as redacted

### Requirement: Task timeout
The system SHALL bound how long a task may run on a node. A task request
MAY set a timeout; when it does not, the system SHALL apply a default.
The system SHALL reject a timeout longer than it is prepared to wait for
a node's response, so a node always reports a timeout before the
dispatch itself gives up.

#### Scenario: A task outlasts its timeout
- **WHEN** a task runs longer than its timeout on a node
- **THEN** that target is recorded as failed with a reason stating the
  task timed out

#### Scenario: A timeout longer than the dispatch limit
- **WHEN** an operator triggers a task with a timeout longer than the
  system's dispatch limit
- **THEN** the system rejects the request as invalid and creates no job

### Requirement: Task results recorded per target
The system SHALL record each target's task result as a JSON object,
alongside its exit code, and show it on the job's detail view. A target
whose task reports an `_error` object SHALL be recorded as failed, with
the error's message as the failure reason.

#### Scenario: A task returns JSON
- **WHEN** a task prints a JSON object and exits zero
- **THEN** the target succeeds and the job's detail view shows that
  object as the target's result

#### Scenario: A task returns plain text
- **WHEN** a task prints text that is not a JSON object and exits zero
- **THEN** the target succeeds and its result is that text under an
  `_output` key

#### Scenario: A task reports an error
- **WHEN** a task prints a JSON object with an `_error` key, or exits
  non-zero
- **THEN** the target is recorded as failed, with the `_error` message
  when there is one, and the job's detail view shows the full result

### Requirement: Console UI for running a task
The system SHALL let a user with `orchestrator:run` run a task from each
place a Puppet run can be started: a node's page, a selection on the
Nodes page, and a group's matching nodes. The user picks an environment
and a task, then fills a parameter form generated from the task's
metadata, which shows each parameter's description and default and
marks required ones. Starting the task opens its job.

#### Scenario: Running a task from a node's page
- **WHEN** a user with `orchestrator:run` chooses to run a task on a
  node's page, picks an environment and task, fills its parameters and
  starts it
- **THEN** a task job targeting that node is created and its job page
  opens

#### Scenario: Invalid parameters in the form
- **WHEN** the user starts a task leaving a required parameter empty
- **THEN** the form identifies the missing parameter and no job is
  created

#### Scenario: Sensitive parameters in the form
- **WHEN** the chosen task has a parameter marked sensitive
- **THEN** the form takes that value in a masked input

#### Scenario: Users without the run permission
- **WHEN** a user without `orchestrator:run` views a node's page
- **THEN** no way to run a task is offered

#### Scenario: Task catalogue not configured
- **WHEN** a user opens the run-task dialog on a console with no task
  catalogue configured
- **THEN** the dialog states that tasks are not configured, instead of
  showing an empty task list
