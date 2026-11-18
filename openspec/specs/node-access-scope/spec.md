# node-access-scope Specification

## Purpose

Limits the node data a caller sees to the nodes in the node groups their
roles grant, so several teams can share one console without seeing each
other's servers.

## Requirements

### Requirement: Scope is inherited down the group tree
The system SHALL treat a grant on a node group as covering every node in
that group and in each of its descendant groups, and a grant on the root
group as covering every node.

#### Scenario: A node in the granted group
- **WHEN** a caller's scope is the group "Web Servers" and a node is in
  "Web Servers"
- **THEN** the node is in the caller's scope

#### Scenario: A node in a descendant group
- **WHEN** a caller's scope is "Linux Servers" and a node is in its
  child group "Debian Family"
- **THEN** the node is in the caller's scope

#### Scenario: A node in a sibling group only
- **WHEN** a caller's scope is "Web Servers" and a node is only in the
  sibling group "Database Servers"
- **THEN** the node is not in the caller's scope

#### Scenario: A grant on the root group
- **WHEN** a caller's scope includes the root group
- **THEN** every node is in the caller's scope, including nodes that
  have never reported facts

### Requirement: Scope is the union across roles, per permission
The system SHALL compute a caller's scope for a permission as the union
of the node groups of every role that gives the caller that permission,
ignoring the groups of roles that do not.

#### Scenario: Two roles with the same permission
- **WHEN** a user holds one role granting `nodes:read` on "Web Servers"
  and another granting `nodes:read` on "Database Servers"
- **THEN** the user's `nodes:read` scope covers the nodes of both groups

#### Scenario: A role without the permission adds nothing
- **WHEN** a user holds `nodes:read` on "Web Servers" from one role and
  `vulnerabilities:read` on the root group from another
- **THEN** the user's `nodes:read` scope is "Web Servers" only, and
  their `vulnerabilities:read` scope is every node

#### Scenario: A permission with no groups
- **WHEN** the only role giving a user `nodes:read` has no node groups
- **THEN** no node is in the user's `nodes:read` scope

### Requirement: Scope follows current group membership
The system SHALL decide whether a node is in scope from the node groups
as they are when the request is served, not as they were when the
caller logged in.

#### Scenario: A node joins a granted group
- **WHEN** a node starts matching a group in a logged-in caller's scope
- **THEN** the caller sees the node without logging in again, within
  one minute

#### Scenario: A node leaves a granted group
- **WHEN** a node stops matching every group in a logged-in caller's
  scope
- **THEN** the caller no longer sees the node, within one minute

#### Scenario: A node that has never reported facts
- **WHEN** a node has no facts and is not pinned to a group in the
  caller's scope
- **THEN** the node is in scope only for callers whose scope includes
  the root group

### Requirement: Node listings are limited to the caller's scope
The system SHALL return, from every endpoint that lists nodes or
per-node records, only those belonging to nodes in the caller's scope
for the permission that gates that endpoint. This covers the node list,
node connectivity, package search and catalogue, vulnerability listings
and their affected nodes, a group's node listing, and the fleet upgrade
list.

#### Scenario: Node list
- **WHEN** a caller scoped to "Web Servers" lists nodes
- **THEN** the response contains only nodes in "Web Servers" and its
  descendants, and its total counts only those

#### Scenario: Connectivity list
- **WHEN** a caller scoped to "Web Servers" lists node connectivity
- **THEN** connected nodes and pending certificate requests outside
  "Web Servers" are absent

#### Scenario: Package search
- **WHEN** a caller scoped to "Web Servers" searches for a package
- **THEN** only installations on in-scope nodes are returned

#### Scenario: A group's nodes
- **WHEN** a caller scoped to "Web Servers" lists the nodes of
  "Debian Family"
- **THEN** only the nodes that are in both are returned

#### Scenario: Fleet-wide caller
- **WHEN** a caller whose scope includes the root group lists nodes
- **THEN** the response is the same as before this capability existed

### Requirement: Out-of-scope nodes are indistinguishable from missing ones
The system SHALL answer a request for one node's data - its detail,
facts, reports, a report's events, packages, package reporting status
or vulnerabilities - with the same not-found response whether the node
is outside the caller's scope or does not exist.

#### Scenario: Node detail outside scope
- **WHEN** a caller scoped to "Web Servers" requests a node that is only
  in "Database Servers"
- **THEN** the response is the not-found response a nonexistent node
  gets

#### Scenario: Report events outside scope
- **WHEN** a caller requests the events of a report that belongs to a
  node outside their scope
- **THEN** the response is the not-found response a nonexistent report
  gets

#### Scenario: Node detail inside scope
- **WHEN** the caller requests a node inside their scope
- **THEN** the node's data is returned in full

### Requirement: Counts and summaries cover only in-scope nodes
The system SHALL compute every count or summary derived from nodes -
the node status summary, per-group node counts, package and provider
counts, vulnerability affected-node counts, code repository usage
counts and the fleet upgrade totals - over the nodes in the caller's
scope only.

#### Scenario: Dashboard summary
- **WHEN** a caller whose scope holds 12 of the fleet's 200 nodes
  requests the node status summary
- **THEN** the figures add up to 12

#### Scenario: Per-group counts
- **WHEN** that caller requests node counts for all groups
- **THEN** each group's count is the number of in-scope nodes in it,
  and the root group's count is 12

#### Scenario: A vulnerability affecting only out-of-scope nodes
- **WHEN** a vulnerability affects no node in the caller's scope
- **THEN** it is absent from the caller's vulnerability list, and
  requesting it directly gives the not-found response

### Requirement: A filter by group cannot widen the scope
The system SHALL apply the caller's scope in addition to any group or
node filter the request asks for.

#### Scenario: Filtering by a group outside scope
- **WHEN** a caller scoped to "Web Servers" filters the package
  catalogue by "Database Servers"
- **THEN** the result is empty rather than that group's packages

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

### Requirement: A plan's target is scoped like a run's
The system SHALL apply to a plan's target the rules that apply to a
run's: a list of nodes naming any node outside the caller's
`orchestrator:run` scope refuses the plan; a query or node group is
narrowed to the nodes in that scope; a caller whose scope is not the
whole fleet cannot use a query that reads across nodes.

#### Scenario: A plan on a named node outside scope
- **WHEN** a caller whose role covers "Web Servers" starts a plan
  naming a database server
- **THEN** the request is refused and no step starts

#### Scenario: A plan on a query
- **WHEN** that caller starts a plan on a query matching web and
  database servers
- **THEN** every step runs on the web servers only

### Requirement: Saved plans are not limited by node groups
The system SHALL make every saved plan readable by every caller with
`orchestrator:read` and changeable by every caller with
`orchestrator:run`, whatever node groups their roles cover, since a
saved plan names no nodes.

#### Scenario: A scoped operator
- **WHEN** a caller whose role covers one node group lists saved plans
- **THEN** every saved plan is listed, and running one is limited to
  that caller's nodes
