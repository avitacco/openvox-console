# plans Specification

## Purpose

Saved plans: named, ordered lists of Puppet runs and tasks that operators build, keep, run on a chosen set of nodes and watch step by step in the console. Covers how a saved plan is stored and checked, that sensitive task values are never kept in one, and the console pages for building, running and watching plans.

## Requirements

### Requirement: Saved plans
The system SHALL keep saved plans. A saved plan has a unique name, an
optional description and an ordered list of one to twenty steps. A step
is a Puppet run with its run options, or a task with its environment,
parameters and timeout. Reading saved plans SHALL require
`orchestrator:read`; creating, changing and deleting one SHALL require
`orchestrator:run`.

#### Scenario: Saving a plan
- **WHEN** an operator saves a plan named "Patch web tier" with a task
  step followed by a run step
- **THEN** the plan is stored with those steps in that order and appears
  in the list of saved plans

#### Scenario: A name already in use
- **WHEN** an operator saves a plan under the name of another saved plan
- **THEN** the request is refused, naming the name as the cause

#### Scenario: No steps, or too many
- **WHEN** an operator saves a plan with no steps, or with twenty-one
- **THEN** the request is refused

#### Scenario: A caller with read only
- **WHEN** a caller holding `orchestrator:read` but not
  `orchestrator:run` tries to save, change or delete a plan
- **THEN** the request is rejected, and the same caller can list and
  read saved plans

#### Scenario: Deleting a plan
- **WHEN** an operator deletes a saved plan
- **THEN** it no longer appears in the list, and jobs it started earlier
  are unchanged

### Requirement: A saved plan's steps are checked when it is saved
The system SHALL refuse to save a plan containing a step that would be
refused as an on-demand run or task for a reason other than its target
or a missing sensitive parameter, and SHALL say which step and why.

#### Scenario: An unknown task
- **WHEN** a plan is saved whose second step names a task that does not
  exist in its environment
- **THEN** the request is refused, naming step 2 and the task

#### Scenario: An invalid parameter
- **WHEN** a plan is saved whose first step gives a task parameter a
  value of the wrong type
- **THEN** the request is refused, naming step 1 and the parameter

#### Scenario: An unknown environment on a run step
- **WHEN** a plan is saved with a run step asking for an environment
  that does not exist
- **THEN** the request is refused, naming the step

### Requirement: Sensitive values are not kept in a saved plan
The system SHALL NOT store the value of a sensitive task parameter in a
saved plan. A value given for one when saving SHALL be discarded, and a
step whose task requires one SHALL be saved without it.

#### Scenario: Saving a step with a sensitive parameter
- **WHEN** a plan is saved with a task step whose `password` parameter
  is sensitive and has a value
- **THEN** the plan is saved, and reading it shows no value for
  `password` and marks the parameter as needed when the plan is run

#### Scenario: Running without the value
- **WHEN** that plan is started without a value for `password`, and the
  task requires it
- **THEN** the request is refused, naming the step and the parameter,
  and nothing starts

### Requirement: Saved plan changes are recorded
The system SHALL record creating, changing and deleting a saved plan in
the activity log and the audit log, naming the plan and the actor.

#### Scenario: A plan is changed
- **WHEN** an operator changes a saved plan's steps
- **THEN** an activity entry and an audit entry name the plan and that
  operator

### Requirement: Console UI for saved plans
The system SHALL provide a Plans page in the Jobs area listing saved
plans. A user with `orchestrator:run` SHALL be able to create, change
and delete a plan there in an editor that adds run and task steps, sets
their options, reorders them and removes them. A user without
`orchestrator:run` SHALL see the plans and their steps with no controls
that change them.

#### Scenario: Where the page is
- **WHEN** a user opens the Jobs area
- **THEN** Plans is offered beside Jobs and Scheduled jobs, and lists
  each saved plan with its description and number of steps

#### Scenario: Step controls
- **WHEN** an operator adds a run step or a task step in the editor
- **THEN** its options are set with the same controls the Run Puppet
  and Run task dialogs use

#### Scenario: Building a plan
- **WHEN** an operator adds a task step, adds a run step, moves the run
  step above the task step and saves
- **THEN** the saved plan has the run step first

#### Scenario: A refused save keeps what was entered
- **WHEN** the save is refused because of a step
- **THEN** the editor stays open with everything entered, and the reason
  is shown on that step

#### Scenario: A reader's view
- **WHEN** a user with `orchestrator:read` only opens the Plans page
- **THEN** the plans are listed and can be opened, with no Create, Save,
  Delete or Run controls

### Requirement: Console UI for running a plan
The system SHALL let a user with `orchestrator:run` run a saved plan
from the Plans page: choosing the target with the same chooser and
preview a run uses, entering each sensitive value the plan's steps need,
and starting it. Starting SHALL open the plan's job page. The dialog
SHALL offer to schedule the plan instead.

#### Scenario: Running a saved plan on a group
- **WHEN** an operator opens Run on a saved plan, chooses a node group
  and starts it
- **THEN** the plan starts on that group's nodes and its job page opens

#### Scenario: A sensitive value is asked for
- **WHEN** the plan has a step needing a sensitive value
- **THEN** the dialog asks for it, labelled with the step, and does not
  start without it when the task requires it

#### Scenario: Schedule instead
- **WHEN** the operator chooses to schedule the plan instead
- **THEN** the schedule form opens with the plan's steps and the chosen
  target filled in

### Requirement: Console UI for watching a plan
The system SHALL show, on a plan's job page, its steps in order, each
with what it does, its state (waiting, running, succeeded, failed, not
run), when it started and finished, and its per-node results. The page
SHALL update while the plan is running, without a reload.

#### Scenario: A plan in progress
- **WHEN** an operator opens a running plan whose first step has
  finished
- **THEN** the first step shows as succeeded with its node results, the
  second as running and the rest as waiting, and the page moves on as
  the steps do

#### Scenario: A plan that failed
- **WHEN** an operator opens a plan whose second of three steps failed
- **THEN** the second step shows as failed with the nodes that failed
  and why, and the third shows as not run

#### Scenario: Opening a step
- **WHEN** an operator opens a step from the plan's page
- **THEN** that step's own job page opens, with a link back to the plan

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
