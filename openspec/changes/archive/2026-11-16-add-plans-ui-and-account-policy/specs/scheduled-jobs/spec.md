## MODIFIED Requirements

### Requirement: Saving a job to start later
The system SHALL let a caller with `orchestrator:run` save a schedule: a
name, an action (a Puppet run with its run options, a task with its
environment, parameters and timeout, or a plan as a name and its list of
steps), a target (a list of nodes, a query, or a node group), and when
to start. The caller SHALL become the schedule's owner.

#### Scenario: Scheduling a no-op run on a group
- **WHEN** an operator saves a schedule for a no-op run on the group
  "Web Servers" starting at 02:00 tomorrow
- **THEN** the schedule is stored with that operator as its owner, and
  its next start is 02:00 tomorrow

#### Scenario: Scheduling a task on a query
- **WHEN** an operator saves a schedule for the task `service` with its
  parameters, targeting a query
- **THEN** the schedule is stored with the task, the parameters and the
  query

#### Scenario: A caller without the run permission
- **WHEN** a caller holding `orchestrator:read` only tries to save a
  schedule
- **THEN** the request is rejected

#### Scenario: A plan
- **WHEN** an operator saves a schedule whose action is a plan of two
  steps
- **THEN** the schedule is stored with the plan's name and both steps,
  and when it comes due it starts a plan job with those steps

## ADDED Requirements

### Requirement: A scheduled plan keeps its own steps
The system SHALL store a scheduled plan's steps in the schedule itself.
Changing or deleting a saved plan SHALL NOT change what a schedule
already saved will run. Sensitive values for a scheduled plan's task
steps SHALL be kept and handled as a scheduled task's are.

#### Scenario: The saved plan is edited afterwards
- **WHEN** a schedule is created from the saved plan "Patch web tier",
  and another operator then adds a step to that saved plan
- **THEN** the schedule's next firing runs the steps it was saved with

#### Scenario: The saved plan is deleted
- **WHEN** the saved plan a schedule was created from is deleted
- **THEN** the schedule still fires with its steps

#### Scenario: A sensitive value in a scheduled plan
- **WHEN** a plan with a step that takes a sensitive value is scheduled
  and the console has a secrets key
- **THEN** the value is stored sealed, shown redacted wherever the
  schedule is read, and sent to the nodes when the step runs

#### Scenario: A step that can no longer run
- **WHEN** a scheduled plan comes due and one of its steps names a task
  that no longer exists
- **THEN** nothing starts and the firing is recorded as skipped, naming
  the step
