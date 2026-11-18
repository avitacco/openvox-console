# scheduled-jobs Specification

## Purpose

Saves a Puppet run or task to start later, once or on a repeating
interval, as its owner and with the owner's access at the moment it
starts, and keeps a record of every time a schedule came due and what
happened.

## Requirements

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

### Requirement: A schedule is checked when it is saved
The system SHALL refuse to save a schedule that, as an on-demand request
made by the same caller at that moment, would be refused for its action
or its target, giving the same reason. A target that resolves to no
nodes SHALL NOT prevent saving.

#### Scenario: A task parameter of the wrong type
- **WHEN** a schedule is saved for a task with a parameter value of the
  wrong type
- **THEN** it is refused, naming the parameter, as the on-demand task
  request is

#### Scenario: A named node outside the caller's scope
- **WHEN** a scoped caller saves a schedule naming a node outside their
  node groups
- **THEN** it is refused as forbidden, naming the node

#### Scenario: A query openvoxdb rejects
- **WHEN** a schedule is saved with a query openvoxdb rejects
- **THEN** it is refused with openvoxdb's message

#### Scenario: A query that matches nothing yet
- **WHEN** a schedule is saved with a valid query that no node matches
  at present
- **THEN** it is saved

### Requirement: Start times and repetition
The system SHALL accept a start time and, optionally, a repetition of
every N minutes, hours, days or weeks. A schedule without repetition
SHALL start once, at its start time, which must be in the future when
saved. A repeating schedule SHALL come due at its start time and at
every whole interval after it, and its interval SHALL be at least five
minutes.

#### Scenario: A one-off schedule
- **WHEN** a schedule with no repetition reaches its start time and
  fires
- **THEN** it has no next start, and is shown as finished

#### Scenario: A repeating schedule
- **WHEN** a schedule starting Sunday 03:00 UTC and repeating every
  week fires
- **THEN** its next start is the following Sunday at 03:00 UTC

#### Scenario: A one-off start time in the past
- **WHEN** a schedule with no repetition is saved with a start time
  that has passed
- **THEN** it is refused as invalid

#### Scenario: A repeating schedule with a start time in the past
- **WHEN** a schedule repeating every day is saved with a start time
  three days ago
- **THEN** it is saved, and its next start is the next whole day after
  that start time that is still in the future

#### Scenario: Too short an interval
- **WHEN** a schedule is saved repeating every two minutes
- **THEN** it is refused as invalid, naming the five-minute minimum

### Requirement: A schedule fires once
The system SHALL start at most one job each time a schedule comes due,
however many console instances are running, and SHALL prefer starting
none to starting two.

#### Scenario: Several instances
- **WHEN** a schedule comes due while three console instances are
  running the scheduler
- **THEN** exactly one job is started

#### Scenario: An instance stops while firing
- **WHEN** the instance firing a schedule stops after taking the firing
  and before starting the job
- **THEN** no other instance starts it, the firing is shown as
  interrupted, and a repeating schedule comes due again at its next
  time

### Requirement: A firing is a request made at that moment
The system SHALL, when a schedule fires, resolve its target and check
its action as the equivalent on-demand request made then would be, and
start the same job that request would. The job SHALL name the schedule,
and its "triggered by" SHALL be the schedule's owner.

#### Scenario: A node joined the group since the schedule was saved
- **WHEN** a schedule targeting a node group fires after a new node has
  joined the group
- **THEN** the job includes the new node

#### Scenario: Run options
- **WHEN** a schedule for a no-op run in the environment `staging`
  fires
- **THEN** the job is a run with those options, sent to nodes as an
  on-demand run with those options is

#### Scenario: A node that is not connected
- **WHEN** a schedule fires and one of its targets is not connected
- **THEN** a job is started, and that target fails as "agent not
  connected", as in an on-demand job

### Requirement: A firing that cannot start is recorded, not retried
The system SHALL record a firing that started nothing as skipped, with
the reason, and SHALL leave the schedule in place to come due at its
next time. It SHALL skip a firing when the target resolves to no nodes,
when the action or target would now be refused, and when the job the
schedule's previous firing started is still running.

#### Scenario: The task no longer exists
- **WHEN** a schedule for a task fires after the task was removed from
  the deployed code
- **THEN** no job is started, and the firing is recorded as skipped,
  naming the task

#### Scenario: Nothing matches
- **WHEN** a schedule fires and its query matches no nodes
- **THEN** no job is started, and the firing is recorded as skipped
  because no nodes matched

#### Scenario: The node group was deleted
- **WHEN** a schedule fires after its node group has been deleted
- **THEN** no job is started, and the firing is recorded as skipped,
  saying the group no longer exists

#### Scenario: The previous job is still running
- **WHEN** a repeating schedule comes due while the job from its
  previous firing has not finished
- **THEN** no second job is started, and the firing is recorded as
  skipped for that reason

#### Scenario: openvoxdb cannot be reached
- **WHEN** a schedule targeting a query fires while openvoxdb is
  unreachable
- **THEN** no job is started, and the firing is recorded as skipped
  with that reason

### Requirement: A late firing is missed
The system SHALL NOT start a firing found more than fifteen minutes
after it came due. It SHALL record that firing as missed, and a
repeating schedule SHALL next come due at its first time still in the
future, recording one missed firing however many times were passed.

#### Scenario: The console was down over a one-off schedule
- **WHEN** a schedule due at 02:00 is first seen by a running console
  at 09:00
- **THEN** no job is started, and the schedule shows that firing as
  missed

#### Scenario: The console was down over several repeats
- **WHEN** an hourly schedule is first seen four and a half hours after
  it last fired
- **THEN** no job is started for the hours passed, one missed firing is
  recorded, and the schedule next fires at the next whole hour

#### Scenario: A short delay
- **WHEN** a schedule due at 02:00 is first seen at 02:05
- **THEN** it fires

### Requirement: Pausing and resuming
The system SHALL let a schedule be paused, in which state it does not
come due, and resumed. Resuming SHALL NOT start anything for times that
passed while it was paused.

#### Scenario: A paused schedule
- **WHEN** a paused schedule's start time passes
- **THEN** nothing is started and no firing is recorded

#### Scenario: Resuming a repeating schedule
- **WHEN** a daily schedule is resumed after being paused for a week
- **THEN** its next start is its next daily time still in the future

#### Scenario: Resuming a one-off schedule whose time has passed
- **WHEN** a one-off schedule is resumed after its start time
- **THEN** the request is refused, saying a new start time is needed

### Requirement: Changing a schedule
The system SHALL let a schedule be replaced with new contents, checked
as a new schedule is, and deleted. The caller who saves a change SHALL
become its owner. Pausing and resuming SHALL NOT change the owner.
Deleting a schedule SHALL leave the jobs it started in place.

#### Scenario: Another user saves a change
- **WHEN** an administrator saves a change to a schedule a departed
  user owned
- **THEN** the administrator is its owner, and it fires with the
  administrator's permissions from then on

#### Scenario: Deleting a schedule
- **WHEN** a schedule that has started jobs is deleted
- **THEN** it no longer fires, and its jobs remain in the job history,
  still naming it

### Requirement: Sensitive task parameters in a schedule
The system SHALL store a scheduled task's sensitive parameter values
encrypted, and SHALL return them redacted wherever the schedule or its
jobs are read. When the console has no secrets key, a schedule carrying
a sensitive value SHALL be refused, saying so. On a change, a sensitive
value left out SHALL keep its stored value only when the caller is the
owner and the task is unchanged.

#### Scenario: A schedule with a sensitive parameter
- **WHEN** a schedule for a task with a sensitive parameter is saved
  and later read
- **THEN** the parameter reads as redacted, and the firing sends the
  real value to the nodes

#### Scenario: No secrets key
- **WHEN** such a schedule is saved on a console with no secrets key
- **THEN** it is refused, naming the secrets key, and nothing is stored

#### Scenario: The owner changes the start time
- **WHEN** the owner saves a new start time without re-entering the
  sensitive value
- **THEN** the stored value is kept

#### Scenario: Another user saves a change
- **WHEN** a different user saves a change without the sensitive value
- **THEN** the request is refused, naming the parameter as required

### Requirement: Schedules and their firings can be read
The system SHALL return, for each schedule a caller may see, its
contents, owner, state (active, paused or finished), next start, and
its most recent firings, each with when it was due, its outcome
(started, skipped, missed or interrupted), the reason, and the job it
started.

#### Scenario: Reading a schedule
- **WHEN** a schedule that fired twice and was skipped once is read
- **THEN** all three firings are listed, newest first, the two started
  ones linking to their jobs and the skipped one giving its reason

#### Scenario: Listing schedules
- **WHEN** schedules are listed
- **THEN** each shows its name, action, target, next start, state and
  the outcome of its last firing

### Requirement: Schedule changes and firings are recorded
The system SHALL record in the activity log and the audit log the
creation, change, pausing, resuming and deletion of a schedule, with
the acting caller, and every skipped or missed firing, with the
schedule's owner and the reason. A firing that starts a job SHALL be
recorded as that job's start, naming the schedule.

#### Scenario: A schedule is created
- **WHEN** an operator saves a new schedule
- **THEN** the activity log shows the operator creating it, by name

#### Scenario: A firing is skipped
- **WHEN** a firing is skipped because its owner lost the permission
- **THEN** the activity log shows the skipped firing and the reason

#### Scenario: A firing starts a job
- **WHEN** a schedule fires and starts a run
- **THEN** the activity log's entry for the run names the schedule

### Requirement: The scheduler runs where it is enabled
The system SHALL fire schedules only on console instances whose run
mode includes the scheduler, and SHALL serve the schedule API on any
instance serving the orchestrator API.

#### Scenario: An instance without the scheduler
- **WHEN** a schedule comes due and the only instances running are in
  a run mode without the scheduler
- **THEN** nothing is started until an instance with the scheduler
  runs, and the late-firing rule then applies

### Requirement: Console UI for schedules
The console UI SHALL offer, to users holding `orchestrator:read`, a
Scheduled jobs page listing the schedules they may see and a page for
each showing its firings; and, to users holding `orchestrator:run`, a
form for creating and changing a schedule and controls to pause, resume
and delete one. Times SHALL be entered and shown in the browser's time
zone, with the zone named.

#### Scenario: Creating a schedule
- **WHEN** a user opens the form, picks Run Puppet with No-op, chooses
  a node group, previews it, sets a start time and a weekly repetition,
  and saves
- **THEN** the schedule appears on the Scheduled jobs page with its
  next start

#### Scenario: Scheduling from an existing entry point
- **WHEN** a user chooses to schedule from the Run Puppet dialog opened
  on a node group's page
- **THEN** the schedule form opens with that group as the target

#### Scenario: A refused schedule
- **WHEN** saving is refused
- **THEN** the reason is shown beside the field it concerns, and the
  form keeps what was typed

#### Scenario: A skipped firing
- **WHEN** a schedule's last firing was skipped or missed
- **THEN** the Scheduled jobs page marks the schedule and shows the
  reason

#### Scenario: A user without the run permission
- **WHEN** a user holding `orchestrator:read` only opens a schedule
- **THEN** it is shown without the controls to change it

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
