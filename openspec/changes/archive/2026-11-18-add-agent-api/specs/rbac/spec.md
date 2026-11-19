# Spec Delta

## ADDED Requirements

### Requirement: Agent tokens
An administrator with `rbac:admin` SHALL be able to issue an agent token with a name, an explicit permission list, node groups, an optional task allowlist, and an expiry. Agent tokens SHALL be a separate kind from service tokens, shown once at creation, listable and revocable, and valid only at the agent endpoint.

#### Scenario: Issuing
- **WHEN** an administrator creates an agent token with permissions, node groups and an expiry
- **THEN** the token is shown once and later listings show its name, scope, owner and expiry but not the token

#### Scenario: Expiry is mandatory
- **WHEN** an administrator creates an agent token without an expiry or with one beyond the maximum
- **THEN** creation is refused

#### Scenario: Scope is fixed
- **WHEN** an agent token is issued with node groups
- **THEN** its calls are limited to those groups and their descendants, and with no node groups it covers no nodes

#### Scenario: Revoking
- **WHEN** an administrator revokes an agent token
- **THEN** it is refused from then on, on every console instance

### Requirement: Agent request permissions
The console SHALL provide `agent-requests:read` (see requests) and `agent-requests:approve` (decide them), assignable to roles, and neither SHALL be grantable to an agent token.

#### Scenario: An agent token cannot approve
- **WHEN** an administrator tries to issue an agent token carrying `agent-requests:approve`
- **THEN** creation is refused

### Requirement: Console UI for agent tokens
The Admin page SHALL let an administrator create, list and revoke agent tokens, choosing permissions, node groups, task patterns and an expiry, and SHALL explain that changes the agent asks for need approval.

#### Scenario: Creating from the UI
- **WHEN** an administrator completes the form
- **THEN** the token is shown once with a note to store it safely
