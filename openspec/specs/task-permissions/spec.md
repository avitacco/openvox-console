# task-permissions Specification

## Purpose

Limiting which tasks a role or service token may run: an allowlist of task names and module wildcards paired with the role's own node groups, checked wherever a task is started, saved in a plan or fired by a schedule, and reflected in the task catalogue, audit trail and console.

## Requirements

### Requirement: A role carries a task allowlist
The system SHALL let a role carry a list of task patterns. A pattern is
a task name (`package::install`), a module name followed by `::*`
(`patch::*`), or `*`. An empty list SHALL allow every task. The list
SHALL be read and written with the role, with at most 20 patterns of at
most 64 characters each, and any other form of pattern refused naming
the field.

#### Scenario: A role limited to one module
- **WHEN** an administrator saves a role with the pattern `patch::*`
- **THEN** reading the role returns that pattern, and the role is
  treated as allowing `patch::apply`, `patch::reboot` and the module's
  own task `patch`, and no other task

#### Scenario: An exact name
- **WHEN** a role's only pattern is `package::install`
- **THEN** it allows `package::install` and not `package::upgrade`

#### Scenario: Every task
- **WHEN** a role's only pattern is `*`, or its list is empty
- **THEN** it allows every task

#### Scenario: A pattern that is not allowed
- **WHEN** a role is saved with the pattern `pa*ch`, `::*`, or an empty
  pattern
- **THEN** the request is refused, naming the allowlist as the cause,
  and the role is unchanged

#### Scenario: Names are matched as written
- **WHEN** a role's only pattern is `patch::*`
- **THEN** it does not allow `patching::apply`, `patch2::apply` or
  `Patch::apply`

### Requirement: Existing roles and tokens keep every task
The system SHALL treat a role or service token saved before this
capability existed as having an empty allowlist. Access tokens issued
before the upgrade SHALL carry no task limit and SHALL be treated as
unlimited until they expire.

#### Scenario: After an upgrade
- **WHEN** the console is upgraded and a user holding an existing
  `orchestrator:run` role starts any task on a node in their node groups
- **THEN** the task starts as it did before

### Requirement: An allowlist applies on the role's own node groups
The system SHALL allow a caller to run a task on a node only when some
role the caller holds both carries `orchestrator:run` for a node group
containing that node and allows that task. An allowlist SHALL NOT widen
or narrow what a different role of the same user allows.

#### Scenario: Two roles with different lists
- **WHEN** a user holds a role allowing `patch::*` on the web tier and a
  role allowing every task on the database tier
- **THEN** they can run `patch::apply` on web nodes and any task on
  database nodes, and cannot run any other task on a web node

#### Scenario: A user with no run permission
- **WHEN** a user holds no role carrying `orchestrator:run`
- **THEN** no allowlist gives them the ability to start a task

#### Scenario: A role with no node groups
- **WHEN** a role allowing every task is granted on no node group
- **THEN** it allows a task on no node

### Requirement: A task is started only if the caller may run it
The system SHALL refuse to start a task run when no role of the caller
allows that task on any node group they hold `orchestrator:run` for. The
refusal SHALL be `403`, SHALL name the task, and SHALL start nothing. It
SHALL not depend on whether the task exists.

#### Scenario: A task outside the allowlist
- **WHEN** a caller whose only role allows `patch::*` asks to run
  `package::install`
- **THEN** the request is refused, naming `package::install`, and no job
  is created

#### Scenario: Existence is not revealed
- **WHEN** the same caller asks to run `package::install` and then a
  task that does not exist
- **THEN** both refusals are identical in status and shape

#### Scenario: Nodes the task is not allowed on
- **WHEN** a caller allowed `patch::apply` on the web tier only names a
  database node in the request
- **THEN** the request is refused as naming a node outside their scope,
  and no job is created

#### Scenario: A query or group is narrowed
- **WHEN** a caller allowed `patch::apply` on the web tier only starts
  it on a node group or a query that also matches database nodes
- **THEN** it runs on the web nodes only, as a run narrows a query to a
  caller's node groups

### Requirement: Every task step of a plan is checked
The system SHALL check each task step of a plan against the caller's
allowlist before anything starts. A plan with a step whose task the
caller may not run SHALL be refused whole, naming the step and the task.
Run steps SHALL NOT be affected.

#### Scenario: A plan with a task the caller may not run
- **WHEN** a caller allowed `patch::*` starts a plan whose second step is
  the task `package::install`
- **THEN** the request is refused, naming step 2 and `package::install`,
  and no job is created

#### Scenario: A plan that only runs Puppet
- **WHEN** a caller allowed `patch::*` starts a plan of two Puppet runs
- **THEN** it starts, as the allowlist limits tasks only

#### Scenario: Nodes outside a step's task scope
- **WHEN** a caller starts a plan on a node list naming a node that the
  caller may not run one of the plan's tasks on
- **THEN** the request is refused, naming that node, and no job is
  created

#### Scenario: A query is narrowed for every step
- **WHEN** a plan has two task steps, the caller may run the first on
  the web and database tiers and the second on the web tier only, and
  the plan's target is a node group covering both tiers
- **THEN** the plan runs on the web nodes only, for every step

### Requirement: A service token may carry a task allowlist
The system SHALL let a service token carry a task allowlist under the
same rules as a role's. A token with an empty list SHALL be allowed
every task. A token's allowlist SHALL be set when it is created and
SHALL be shown with it.

#### Scenario: A token limited to deploy tasks
- **WHEN** a service token is created with the pattern `deploy::*` and
  used to start the task `package::install`
- **THEN** the request is refused, naming the task

#### Scenario: A token without a list
- **WHEN** a service token with no allowlist starts any task
- **THEN** the task starts, as before

### Requirement: The task catalogue offers only tasks the caller may run
The system SHALL list, and answer a request for the details of, only the
tasks a caller who holds `orchestrator:run` may run on at least one
node. A task the caller may not run SHALL be answered as an unknown task.
A caller who holds `orchestrator:read` but not `orchestrator:run` SHALL
see every task.

#### Scenario: The picker
- **WHEN** a caller allowed `patch::*` opens the Run task dialog
- **THEN** only tasks in the `patch` module are offered

#### Scenario: A task not offered
- **WHEN** that caller requests the details of `package::install`
- **THEN** the answer is the one given for a task that does not exist

#### Scenario: A read-only caller
- **WHEN** a caller with `orchestrator:read` only opens the task list
- **THEN** every task is listed

### Requirement: Console UI for task permissions
The system SHALL show and edit a role's and a service token's task
allowlist in the console, one pattern per line, with a note that an
empty list allows every task. The Roles page and the service tokens page
SHALL show each one's allowlist, and show an empty one as allowing every
task. A refused pattern SHALL be shown on the field with what was
entered kept.

#### Scenario: Editing a role
- **WHEN** an administrator adds `patch::*` to a role and saves
- **THEN** the Roles page shows that pattern for the role

#### Scenario: A refused pattern
- **WHEN** the administrator saves the pattern `pa*ch`
- **THEN** the form stays open with the entry kept and the reason shown
  on the field

#### Scenario: A role with no list
- **WHEN** a role has no patterns
- **THEN** the Roles page shows that it allows every task

### Requirement: Refused task starts are audited
The system SHALL record in the audit log each task start refused because
of the task allowlist, naming the actor, the task and, for a plan, the
step. The record SHALL NOT contain task parameters.

#### Scenario: A refused plan
- **WHEN** a plan is refused because step 2's task is not allowed
- **THEN** an audit entry names the actor, step 2 and the task, and
  holds no parameter values

### Requirement: Allowlist changes are recorded and take effect
The system SHALL include a role's allowlist, before and after, in the
audit entry for a change to the role, and a service token's allowlist in
the entry for its creation. A change to a role's allowlist SHALL apply to
a user's next access token and to a schedule at its next firing.

#### Scenario: A role is narrowed
- **WHEN** an administrator changes a role's allowlist from empty to
  `patch::*`
- **THEN** the audit entry shows both values and names the administrator,
  and a user holding the role is refused other tasks once their access
  token is next refreshed
