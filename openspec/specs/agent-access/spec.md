# agent-access Specification

## Purpose
Let AI agents use the console through a standard tool protocol (MCP) and a published API description, with the same authentication, scope and audit as every other caller, and with every change they want held for a person's approval.

## Requirements

### Requirement: The MCP endpoint is off unless enabled
The console SHALL serve an MCP endpoint at `/mcp` only when `CONSOLE_MCP_ENABLED` is set. When it is not, the path SHALL NOT be routed and no agent request SHALL be accepted.

#### Scenario: Disabled by default
- **WHEN** the console starts without `CONSOLE_MCP_ENABLED`
- **THEN** a request to `/mcp` receives the same response as any unknown path

#### Scenario: Enabled
- **WHEN** the console starts with `CONSOLE_MCP_ENABLED=true`
- **THEN** an MCP client holding a valid agent token can initialize a session and list the tools it may use

### Requirement: Only agent tokens may use the endpoint
The endpoint SHALL accept a bearer agent token and refuse every other credential, including user access tokens and service tokens, with an authentication error.

#### Scenario: A user token is refused
- **WHEN** a client presents a user's access token to `/mcp`
- **THEN** the request is refused as unauthenticated

#### Scenario: An expired or revoked agent token is refused
- **WHEN** a client presents an agent token that has expired or been revoked
- **THEN** the request is refused as unauthenticated

### Requirement: Tools are limited to what the token allows
The tool list SHALL contain only the tools the agent token's permissions allow, and every tool call SHALL be authorized again with the token's permissions, node scope and task allowlist, exactly as the equivalent console request would be.

#### Scenario: A tool the token cannot use is absent
- **WHEN** an agent token without `orchestrator:run` lists tools
- **THEN** the tools that request tasks, runs or patches are not listed

#### Scenario: Node scope applies to reads
- **WHEN** a token scoped to one node group lists nodes
- **THEN** only nodes in that group and its descendants are returned

#### Scenario: A call outside scope is refused
- **WHEN** an agent requests a task on a node outside its token's scope
- **THEN** the call fails with the same refusal a scoped person would get, and no request is stored

### Requirement: Read tools return results immediately
The endpoint SHALL provide tools that read nodes, node facts and reports, groups, the task catalog and a task's parameters, jobs and job results, patch inventory, and the nodes a target would resolve to. They SHALL start no job and change nothing.

#### Scenario: Reading a job result
- **WHEN** an agent asks for a job it may see
- **THEN** it receives the job's status, targets and per-target results with sensitive values masked

#### Scenario: Resolving targets
- **WHEN** an agent resolves a PQL query or group to nodes
- **THEN** it receives the nodes within its scope and no job is created

### Requirement: Change tools only submit requests
Tools that would run a task, run Puppet, run a plan or run a patch SHALL validate the request exactly as the console does and then store it as a pending approval request. They SHALL NOT start a job, and SHALL return the request id and a status of awaiting approval.

#### Scenario: A task request
- **WHEN** an agent calls the run-task tool with a valid task, parameters and targets
- **THEN** a pending request is stored, no job exists, and the reply names the request id and says it awaits approval

#### Scenario: An invalid request stores nothing
- **WHEN** an agent submits parameters the task does not accept
- **THEN** the call fails with the console's validation message and nothing is stored

#### Scenario: Reading an outcome
- **WHEN** an agent asks for the status of one of its requests
- **THEN** it learns whether the request is pending, approved with a job id, rejected with the approver's note, or expired

### Requirement: Agents see only their own requests
An agent token SHALL be able to read the requests that token submitted and no others.

#### Scenario: Another token's request
- **WHEN** an agent asks for a request submitted by a different token
- **THEN** it is told the request does not exist

### Requirement: The endpoint is rate limited and bounded
The endpoint SHALL limit calls per agent token (configurable, with a default), refuse request bodies over a fixed size, and bound how many pending requests one token may hold.

#### Scenario: Too many calls
- **WHEN** a token exceeds its call rate
- **THEN** further calls fail with a rate limit error naming when to retry, and other tokens are unaffected

#### Scenario: Too many pending requests
- **WHEN** a token already holds the maximum number of pending requests
- **THEN** a new change request is refused until one is decided or expires

### Requirement: The API is described by an OpenAPI document
The console SHALL serve an OpenAPI document describing its `/api/v1` endpoints, and a short index of the same for agents, to any authenticated caller.

#### Scenario: Fetching the document
- **WHEN** an authenticated caller requests the OpenAPI document
- **THEN** it receives a valid OpenAPI description of the console's public API
