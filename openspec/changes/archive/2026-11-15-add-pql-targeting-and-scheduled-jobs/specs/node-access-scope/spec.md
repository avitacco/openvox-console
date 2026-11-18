## ADDED Requirements

### Requirement: Targets chosen by a query or a group are narrowed to scope
The system SHALL, when an action's targets are chosen by a query or a
node group, act only on those resolved nodes that are in the caller's
scope for the action's permission, and SHALL NOT say whether, or how
many, other nodes matched. Nodes given by name SHALL remain all or
nothing.

#### Scenario: A query that also matches other teams' nodes
- **WHEN** a user whose `orchestrator:run` scope is "Web Servers" runs
  Puppet with a query that matches two web servers and three database
  servers
- **THEN** one job is created on the two web servers, and nothing in
  the response refers to the other three

#### Scenario: A query that matches only other teams' nodes
- **WHEN** that user runs Puppet with a query that matches database
  servers only
- **THEN** the request is refused as matching no nodes, exactly as a
  query that matches nothing at all is

#### Scenario: A group partly outside scope
- **WHEN** that user runs a task on a node group holding one web server
  and one database server
- **THEN** the job runs on the web server only

#### Scenario: Previewing a query
- **WHEN** that user previews a query
- **THEN** only nodes in their scope are listed

#### Scenario: A fleet-wide caller
- **WHEN** a caller whose `orchestrator:run` scope includes the root
  group runs Puppet with a query
- **THEN** the job runs on every node the query returns

#### Scenario: A named node outside scope
- **WHEN** a scoped user gives a list of nodes one of which is outside
  their scope
- **THEN** the request is refused whole and names that node, as before

### Requirement: A scoped caller's query cannot read across nodes
The system SHALL refuse a query containing a subquery when the caller's
scope for the action's permission does not cover every node, so that a
query cannot be used to test facts about nodes outside that scope. A
caller whose scope covers every node SHALL be able to use subqueries.

#### Scenario: A subquery from a scoped caller
- **WHEN** a user whose `orchestrator:run` scope is "Web Servers" runs
  or previews
  `nodes[certname] { certname in facts[certname] { name = "role" } }`
- **THEN** the request is refused as invalid, saying that subqueries
  need access to every node, and the query is not sent to openvoxdb

#### Scenario: A single-entity query from a scoped caller
- **WHEN** that user runs
  `inventory[certname] { facts.os.family = "Debian" }`
- **THEN** the query is run and its answer is narrowed to their scope

#### Scenario: A subquery from a fleet-wide caller
- **WHEN** a caller whose scope includes the root group runs the
  subquery above
- **THEN** the query is run

### Requirement: A schedule fires with its owner's current scope
The system SHALL, each time a schedule fires, apply its owner's
permissions and node scope as they are at that moment, treating the
firing as a request the owner made then: named nodes all or nothing,
a query or a group narrowed, and nothing started if the owner no longer
holds `orchestrator:run`.

#### Scenario: The owner's scope has narrowed
- **WHEN** a schedule targeting a query fires after its owner's
  `orchestrator:run` scope was reduced from every node to "Web Servers"
- **THEN** the job runs on the matching web servers only

#### Scenario: A named node has left the owner's scope
- **WHEN** a schedule naming three nodes fires after one of them has
  left the owner's node groups
- **THEN** nothing is started, and the firing is recorded as skipped
  with the reason

#### Scenario: The owner has lost the permission
- **WHEN** a schedule fires after its owner's roles no longer grant
  `orchestrator:run`, or the owner's account or service token has been
  deleted
- **THEN** nothing is started, and the firing is recorded as skipped
  with the reason

#### Scenario: A subquery after the owner became scoped
- **WHEN** a schedule whose query contains a subquery fires after its
  owner's scope stopped covering every node
- **THEN** nothing is started, and the firing is recorded as skipped
  with the reason

### Requirement: Schedules are limited to their owner and fleet-wide callers
The system SHALL show a schedule to its owner and to callers whose
`orchestrator:read` scope covers every node, and SHALL let it be
changed, paused, resumed or deleted by its owner and by callers whose
`orchestrator:run` scope covers every node. To any other caller the
schedule SHALL be indistinguishable from one that does not exist.

#### Scenario: A scoped user lists schedules
- **WHEN** a user whose `orchestrator:read` scope is "Web Servers"
  lists schedules
- **THEN** only the schedules they own are listed

#### Scenario: A scoped user reads another user's schedule
- **WHEN** that user requests a schedule another user owns
- **THEN** the answer is the same not-found answer as for an id that
  does not exist

#### Scenario: A fleet-wide user changes another user's schedule
- **WHEN** a user whose `orchestrator:run` scope includes the root
  group saves a change to a schedule another user owns
- **THEN** the change is saved

#### Scenario: A scoped user deletes another user's schedule
- **WHEN** a scoped user sends a delete for a schedule another user
  owns
- **THEN** the answer is not-found and the schedule is unchanged

### Requirement: A job's query is shown only with all of its targets
The system SHALL leave a job's query text out of the job as read by a
caller who is not shown every node the job ran on.

#### Scenario: A job partly outside the reader's scope
- **WHEN** a user whose `orchestrator:read` scope is "Web Servers"
  reads a job that a query started on web and database servers
- **THEN** the job shows their web servers, says that some targets are
  hidden, and does not carry the query text

#### Scenario: A job wholly inside the reader's scope
- **WHEN** that user reads a job that a query started on web servers
  only
- **THEN** the job carries the query text
