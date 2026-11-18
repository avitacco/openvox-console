## ADDED Requirements

### Requirement: Targets chosen by a query
The system SHALL accept, in place of a list of nodes, a PQL query as the
target of a Puppet run or a task. It SHALL run the query against
openvoxdb when the request is made and start the job on the distinct
`certname` values the query returns. Every other part of the request
SHALL be checked exactly as it is for a list of nodes.

#### Scenario: A run on the nodes a query matches
- **WHEN** an operator triggers a run with the query
  `inventory[certname] { facts.os.family = "Debian" }` and three nodes
  match
- **THEN** one job is created with those three nodes as its targets

#### Scenario: A task on the nodes a query matches
- **WHEN** an operator triggers a task with a query as its target
- **THEN** the task's parameters are validated as usual, and the job
  runs on the nodes the query returns

#### Scenario: A node returned more than once
- **WHEN** a query returns the same `certname` in several rows
- **THEN** the node is a target once

#### Scenario: More than one kind of target
- **WHEN** a request gives both a list of nodes and a query, or both a
  query and a node group
- **THEN** the request is refused as invalid, and no job is created

### Requirement: A query that cannot be used is refused
The system SHALL refuse a query target, creating no job, when the query
is empty, is longer than 4,000 characters, is not in PQL string form,
is rejected by openvoxdb, or returns a row without a `certname`. The
refusal SHALL say which, and SHALL carry openvoxdb's own message when
openvoxdb rejected the query.

#### Scenario: A query openvoxdb rejects
- **WHEN** a run is requested with a query that openvoxdb answers as a
  syntax error
- **THEN** the request is refused as invalid with openvoxdb's message,
  and no job is created

#### Scenario: A query that returns no certname
- **WHEN** a run is requested with `nodes[count()] { }`
- **THEN** the request is refused as invalid, saying the query must
  return `certname`

#### Scenario: A query in AST form
- **WHEN** a run is requested with a query written as a JSON array
- **THEN** the request is refused as invalid, saying a PQL string is
  required

#### Scenario: openvoxdb cannot be reached
- **WHEN** a run is requested with a query while openvoxdb is
  unreachable
- **THEN** the request fails as a gateway error, and no job is created

### Requirement: Targets chosen by a node group
The system SHALL accept a node group as the target of a Puppet run or a
task, and SHALL start the job on the nodes that are in that group when
the request is made.

#### Scenario: A run on a group
- **WHEN** an operator triggers a run naming the group "Web Servers",
  which four nodes are in
- **THEN** one job is created with those four nodes as its targets

#### Scenario: A group that does not exist
- **WHEN** a run is requested naming a node group id that does not
  exist
- **THEN** the request is refused as invalid, and no job is created

### Requirement: A target that resolves to no nodes starts nothing
The system SHALL refuse a run or a task whose query or node group
resolves to no nodes, creating no job, with a reason saying that
nothing matched.

#### Scenario: A query matching nothing
- **WHEN** a run is requested with a query no node matches
- **THEN** the request is refused as invalid, saying no nodes matched,
  and no job is created

#### Scenario: An empty group
- **WHEN** a task is requested on a node group with no nodes in it
- **THEN** the request is refused the same way

### Requirement: Target preview
The system SHALL let a caller with `orchestrator:run` ask which nodes a
list, a query or a node group resolves to, without starting anything.
The answer SHALL be the nodes a job started by that caller at that
moment would run on, and a refusal SHALL be the refusal that job would
have received.

#### Scenario: Previewing a query
- **WHEN** an operator previews a query that five nodes match
- **THEN** the five node names are returned, and no job is created

#### Scenario: Previewing an unusable query
- **WHEN** an operator previews a query openvoxdb rejects
- **THEN** the preview is refused with the same message a run would get

#### Scenario: A caller without the run permission
- **WHEN** a caller holding `orchestrator:read` only asks for a preview
- **THEN** the request is rejected

### Requirement: A job records where its targets came from
The system SHALL record, on a job whose targets came from a query or a
node group, the query text or the group, and on a job started by a
schedule, that schedule. Both SHALL be returned with the job in the job
list and the job detail. A job started from a list of nodes SHALL
record neither.

#### Scenario: A job started by a query
- **WHEN** a job started by a query is read
- **THEN** it carries the query text alongside the nodes it ran on

#### Scenario: A job started by a group that was later deleted
- **WHEN** a job started on a node group is read after the group has
  been deleted
- **THEN** it still carries the group's name as it was when the job
  started

#### Scenario: A job started by a schedule
- **WHEN** a job a schedule started is read
- **THEN** it names the schedule, and its "triggered by" is the
  schedule's owner

#### Scenario: A job started from a list of nodes
- **WHEN** a job started from a list of nodes is read
- **THEN** it carries no target source

### Requirement: Console UI for choosing targets
The console UI SHALL let a user holding `orchestrator:run` start a
Puppet run or a task from the Jobs page, choosing the targets as a list
of nodes, a query, or a node group, and SHALL show the nodes the choice
resolves to before the job is started. The Jobs page and a job's page
SHALL show a job's query or group and the schedule that started it.

#### Scenario: Starting a run from the Jobs page with a query
- **WHEN** a user opens Run Puppet on the Jobs page, chooses "Query",
  types a query and asks for the preview
- **THEN** the matching nodes and their count are shown, and confirming
  starts a job on them

#### Scenario: A query that is refused
- **WHEN** the query typed is refused
- **THEN** the reason is shown beside the query, and the job cannot be
  started until a preview succeeds

#### Scenario: The existing entry points
- **WHEN** a user starts a run from a node's page, a selection on the
  Nodes page, or a group's page
- **THEN** the dialog names those nodes as it does today, without the
  target chooser

#### Scenario: A user without the run permission
- **WHEN** a user holding `orchestrator:read` only opens the Jobs page
- **THEN** the buttons for starting a job are not offered

#### Scenario: A job's page
- **WHEN** a user opens a job that a schedule started with a query
- **THEN** the page shows the query and links to the schedule
