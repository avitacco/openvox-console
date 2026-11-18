# Spec Delta

## ADDED Requirements

### Requirement: Actions are limited to the caller's scope
The system SHALL refuse an action on a node - a Puppet run, a task, a
plan, a package reporting change, a client upgrade, a node removal, or
a certificate sign, revoke or clean - unless the node is in the
caller's scope for the permission that gates that action.

#### Scenario: Running Puppet inside scope
- **WHEN** a user whose `orchestrator:run` scope is "Web Servers" runs
  Puppet on a node in "Web Servers"
- **THEN** the run is started

#### Scenario: Running Puppet outside scope
- **WHEN** that user runs Puppet on a node that is only in
  "Database Servers"
- **THEN** the request is refused as forbidden and no job is created

#### Scenario: Removing a node outside scope
- **WHEN** a user whose `nodes:manage` scope is "Web Servers" removes a
  node that is only in "Database Servers"
- **THEN** the request is refused as forbidden and the node is unchanged

#### Scenario: Revoking a certificate outside scope
- **WHEN** a user whose `nodes:certs:manage` scope is "Web Servers"
  revokes the certificate of a node that is only in "Database Servers"
- **THEN** the request is refused as forbidden and the certificate is
  unchanged

#### Scenario: Fleet-wide caller
- **WHEN** a caller whose scope for the action's permission includes
  the root group performs the action on any node
- **THEN** the action proceeds as it did before this requirement existed

### Requirement: An action on several nodes is all or nothing
The system SHALL refuse a multi-node action whole when any named node
is outside the caller's scope, starting nothing for the nodes that are
in scope, and SHALL name the out-of-scope nodes in the refusal.

#### Scenario: A mixed target list
- **WHEN** a user whose `orchestrator:run` scope is "Web Servers" runs
  a task on two web servers and one database server
- **THEN** the request is refused as forbidden, the refusal names the
  database server, and no job is created for any of the three

#### Scenario: Every target in scope
- **WHEN** the same user runs the task on the two web servers only
- **THEN** one job is created covering both

### Requirement: A refused action does not reveal whether a node exists
The system SHALL decide scope before looking the node up, so that an
action naming a node outside the caller's scope and an action naming a
node that does not exist are refused identically.

#### Scenario: Unknown node and hidden node
- **WHEN** a scoped caller requests the same action once for a node
  outside their scope and once for a name no node has
- **THEN** the two refusals differ only in the name they echo

#### Scenario: Nothing is sent to an out-of-scope node
- **WHEN** an action is refused for a node outside the caller's scope
- **THEN** no request is dispatched to that node and no certificate
  authority call is made for it

### Requirement: Action scope is independent of read scope
The system SHALL evaluate an action against the caller's scope for that
action's permission alone, whatever the caller may read.

#### Scenario: Reads everything, acts on one group
- **WHEN** a user holds `nodes:read` on the root group from one role
  and `orchestrator:run` on "Web Servers" from another
- **THEN** they see every node, can run Puppet on web servers, and are
  refused a run on a database server

#### Scenario: A node the caller cannot read
- **WHEN** a user holds `orchestrator:run` on the root group and
  `nodes:read` on "Web Servers" only
- **THEN** a run on a database server they name is started

### Requirement: Jobs are limited to the caller's scope
The system SHALL list a job to a caller only when at least one of the
job's nodes is in the caller's `orchestrator:read` scope, and SHALL
show within a job only the results, counts and previews of nodes in
that scope.

#### Scenario: A job entirely inside scope
- **WHEN** a caller scoped to "Web Servers" lists jobs
- **THEN** a job that ran on web servers is listed with all its nodes

#### Scenario: A job entirely outside scope
- **WHEN** that caller lists jobs, or requests by id a job that ran
  only on database servers
- **THEN** the job is absent from the list, and the request by id gets
  the response a job that does not exist gets

#### Scenario: A job spanning both
- **WHEN** that caller opens a job that ran on one web server and one
  database server
- **THEN** the job shows one node, a node count of one, and nothing of
  the database server's result

#### Scenario: Plan steps
- **WHEN** that caller opens a plan whose steps ran on both groups
- **THEN** each step shows only the in-scope nodes, and a step with
  none is not shown

#### Scenario: Job list totals
- **WHEN** a scoped caller pages through the job list
- **THEN** the total counts only the jobs they can see

### Requirement: Nodes that have never reported can be acted on through a pin
The system SHALL treat a node with no facts as in a caller's scope when
it is pinned into a group in that scope and every ancestor of that
group also holds it, so that a scoped team can sign the certificate of
a node pinned into its group before the node has ever run.

#### Scenario: Signing a pinned new node
- **WHEN** a pending certificate request's certname is pinned into
  "Web Servers", which sits directly under the root, and a user whose
  `nodes:certs:manage` scope is "Web Servers" signs it
- **THEN** the certificate is signed

#### Scenario: Signing an unpinned new node
- **WHEN** the same user signs a pending request whose certname is
  pinned nowhere
- **THEN** the request is refused as forbidden

## MODIFIED Requirements

### Requirement: Surfaces that are not scoped
The system SHALL leave the activity log, node group definitions, the
ENC endpoint, code deploys, service tokens and the console's own
automatic client upgrades unaffected by node group scope.

#### Scenario: Running Puppet on a node outside read scope
- **WHEN** a user with `orchestrator:run` on the root group and a
  `nodes:read` scope of "Web Servers" triggers a run on a node in
  "Database Servers"
- **THEN** the run is accepted, because the action is judged by the
  `orchestrator:run` scope and not by what the user may read

#### Scenario: A service token
- **WHEN** a service token carrying `nodes:read` lists nodes, or one
  carrying `orchestrator:run` runs Puppet on any node
- **THEN** every node is returned, and the run is started

#### Scenario: Group definitions
- **WHEN** a scoped caller with `classifier:read` lists node groups
- **THEN** every group is listed

#### Scenario: The activity log
- **WHEN** a caller with `activity:read` and a narrow node scope views
  the activity log
- **THEN** every event is listed, including those naming nodes outside
  their scope

#### Scenario: Automatic client upgrades
- **WHEN** the console upgrades an out-of-date client by itself
- **THEN** the upgrade proceeds whatever any user's scope is
