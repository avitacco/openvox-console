# Spec Delta

## Purpose

Limits the node data a caller sees to the nodes in the node groups their
roles grant, so several teams can share one console without seeing each
other's servers.

## ADDED Requirements

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
The system SHALL leave node actions, orchestrator jobs, the activity
log, node group definitions, the ENC endpoint and service tokens
unaffected by node group scope.

#### Scenario: Running Puppet on a node outside read scope
- **WHEN** a user with `orchestrator:run` and a `nodes:read` scope of
  "Web Servers" triggers a run on a node in "Database Servers"
- **THEN** the run is accepted as it was before this capability existed

#### Scenario: A service token
- **WHEN** a service token carrying `nodes:read` lists nodes
- **THEN** every node is returned

#### Scenario: Group definitions
- **WHEN** a scoped caller with `classifier:read` lists node groups
- **THEN** every group is listed
