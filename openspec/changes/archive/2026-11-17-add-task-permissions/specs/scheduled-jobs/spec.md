## ADDED Requirements

### Requirement: A schedule runs only tasks its owner may run
The system SHALL save a schedule for a task, or for a plan with task
steps, only when the saving caller may run each of those tasks, and
SHALL check them again each time the schedule fires against its owner's
roles or token as they are then. A schedule whose owner may no longer
run one of its tasks SHALL start nothing and record a skipped firing
naming the task and, for a plan, the step.

#### Scenario: Saving a schedule for a task the caller may not run
- **WHEN** a caller allowed `patch::*` saves a schedule for the task
  `package::install`
- **THEN** the request is refused, naming the task, and no schedule is
  saved

#### Scenario: The owner loses the task
- **WHEN** a schedule for `patch::apply` comes due after its owner's
  role stopped allowing `patch::*`
- **THEN** nothing is started and the firing is recorded as skipped,
  naming `patch::apply`

#### Scenario: A plan schedule
- **WHEN** a scheduled plan's second step is a task its owner may no
  longer run
- **THEN** nothing is started and the skipped firing names step 2 and
  the task

#### Scenario: Another caller replaces the schedule
- **WHEN** a caller who may not run the schedule's task replaces it,
  which would make them its owner
- **THEN** the request is refused, naming the task, and the schedule is
  unchanged
