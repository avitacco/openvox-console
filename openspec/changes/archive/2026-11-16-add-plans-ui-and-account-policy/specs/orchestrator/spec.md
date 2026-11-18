## MODIFIED Requirements

### Requirement: Task and plan execution
The system SHALL support running a Puppet task against target nodes and
running a plan (a sequence of tasks and Puppet runs with defined
ordering), tracking each as a job the same way an on-demand run is
tracked. A plan's steps SHALL accept what an on-demand run or task
accepts. A plan SHALL take its target as a run does, resolved once and
used for every step. A plan with a step that would be refused SHALL be
refused whole, naming the step, before anything is created or sent.

#### Scenario: Triggering a task run
- **WHEN** an operator triggers a task with its required parameters
  against a target node
- **THEN** the system dispatches the corresponding task request and
  tracks its completion as a job

#### Scenario: A plan runs its steps in the declared order
- **WHEN** an operator triggers a plan composed of multiple steps
- **THEN** the system executes those steps in the plan's declared order
  and the plan's job is not reported complete until every step has
  finished

#### Scenario: A run step with options
- **WHEN** a plan has a run step asking for a no-op run
- **THEN** that step's run is sent to the nodes as a no-op run

#### Scenario: A task step in an environment
- **WHEN** a plan has a task step naming the environment `staging`
- **THEN** the task is taken from `staging` and its parameters are
  checked against that environment's task

#### Scenario: A plan targeted by a node group
- **WHEN** a plan is started on a node group
- **THEN** every step runs on the nodes that were in the group when the
  plan started

#### Scenario: A bad step refuses the whole plan
- **WHEN** a plan's third step names a task that does not exist
- **THEN** the request is refused naming step 3, and no step starts

## ADDED Requirements

### Requirement: A failed step ends the plan
The system SHALL treat a step as failed when it fails on any of its
nodes, SHALL NOT start any later step, and SHALL close each later step
as not run, recording which step failed as the reason. The plan's job
SHALL then be failed.

#### Scenario: The second of three steps fails on one node
- **WHEN** a plan's second step fails on one of its four nodes
- **THEN** the plan is failed, the third step is recorded as not run
  because step 2 failed, and nothing is sent to any node for it

#### Scenario: No step is left open
- **WHEN** a plan has ended, by success or by failure
- **THEN** none of its steps is still reported as running

### Requirement: A plan's job is read with its steps
The system SHALL return, when a plan's job is read, its steps in order,
each with its kind, task, options, state, start and finish time and
per-node results. The job list SHALL show a plan as one entry and SHALL
NOT list its steps as entries of their own. A step read by itself SHALL
name its plan.

#### Scenario: Reading a plan
- **WHEN** a caller with `orchestrator:read` reads a plan's job
- **THEN** the response carries the steps in their declared order with
  each step's state

#### Scenario: The job list
- **WHEN** a plan of three steps has run
- **THEN** the job list shows one entry for it, not four

#### Scenario: Reading a step
- **WHEN** a caller reads one step's job
- **THEN** the response names the plan it belongs to and its position
