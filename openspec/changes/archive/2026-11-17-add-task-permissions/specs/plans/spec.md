## ADDED Requirements

### Requirement: A saved plan's tasks are limited by what its editor may run
The system SHALL refuse to save a plan containing a task step whose task
the saving caller may not run, naming the step and the task. Reading a
saved plan SHALL show every step, and SHALL mark a task step the reading
caller may not run as one they cannot run, the same way a step that can
no longer run is marked.

#### Scenario: Saving a plan with a task the caller may not run
- **WHEN** a caller allowed `patch::*` saves a plan whose second step is
  the task `package::install`
- **THEN** the request is refused, naming step 2 and `package::install`,
  and the plan is not saved

#### Scenario: Reading a plan with a task outside the reader's list
- **WHEN** a caller allowed `patch::*` opens a plan that another user
  saved with a `package::install` step
- **THEN** every step is shown, and that step is marked as one the
  caller may not run

#### Scenario: Running that plan
- **WHEN** that caller runs the plan
- **THEN** the request is refused, naming the step and the task

#### Scenario: Editing that plan
- **WHEN** that caller saves the plan without removing the step
- **THEN** the request is refused, naming the step and the task
