## ADDED Requirements

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
