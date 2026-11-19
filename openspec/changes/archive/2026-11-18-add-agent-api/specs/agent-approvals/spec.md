# agent-approvals Specification

## Purpose
Hold every change an agent asks for until a person with authority decides it, run exactly what was approved, and leave a complete record.

## ADDED Requirements

### Requirement: A request freezes what will run
A pending request SHALL record the kind (task, Puppet run, plan, patch run), the exact task or options, the parameters, the resolved targets and the requesting token. These SHALL NOT change after submission. Sensitive parameter values SHALL be stored sealed and never shown.

#### Scenario: What is approved is what runs
- **WHEN** a request is approved
- **THEN** the job starts with the stored task, parameters and targets, unchanged

#### Scenario: Sensitive values
- **WHEN** a task request includes a parameter the task marks sensitive
- **THEN** the request listing shows it masked and the value is stored sealed, and it is absent from logs and audit entries

#### Scenario: No secrets key
- **WHEN** a request carries a sensitive value and the console has no secrets key
- **THEN** the request is refused rather than stored in the clear

### Requirement: Approvers see the whole request
The Approvals page and API SHALL show, for each request: the agent token's name, the time, the kind and task, the parameters with sensitive values masked, the targets or the query and group they came from, and the time it expires.

#### Scenario: Reviewing a request
- **WHEN** an approver opens a pending request
- **THEN** they see every item above and can approve or reject it with an optional note

### Requirement: Who may decide
Deciding SHALL require `agent-requests:approve` and also `orchestrator:run` covering every target of the request. The owner of the requesting agent token SHALL NOT approve that token's requests, though they may reject them.

#### Scenario: An approver outside the targets' scope
- **WHEN** a person holding `agent-requests:approve` but with no `orchestrator:run` on one of the request's nodes tries to approve
- **THEN** the approval is refused and the request stays pending

#### Scenario: The token's owner
- **WHEN** the person who created an agent token tries to approve a request from it
- **THEN** the approval is refused, and they may still reject it

### Requirement: Approval re-checks and starts the job
Approving SHALL confirm that the token is still valid and still permitted, and that the task and nodes still exist, then start the job through the same path as a person's request. If a check fails, the request SHALL be marked failed with the reason and no job started.

#### Scenario: Token revoked while waiting
- **WHEN** the agent token is revoked and then an approver approves its request
- **THEN** no job is started and the request shows it failed because the token was revoked

#### Scenario: Approval starts a job
- **WHEN** a valid request is approved
- **THEN** a job starts, the request records its id, and the job records the request and the approver

### Requirement: Rejection and expiry
A rejected request SHALL never run and SHALL keep the approver's note. A pending request SHALL expire after a configurable time (default one hour) and then never run.

#### Scenario: Rejected
- **WHEN** an approver rejects with a note
- **THEN** the agent's status check returns rejected and the note

#### Scenario: Expired
- **WHEN** a request is not decided within its time
- **THEN** it is marked expired and an approval attempt is refused

### Requirement: A request is decided once
Concurrent or repeated decisions SHALL result in exactly one outcome.

#### Scenario: Two approvers
- **WHEN** two approvers approve the same request at once
- **THEN** exactly one job starts

### Requirement: Approvals page
The console SHALL provide an Approvals page listing pending and recent requests, with approve and reject actions for people allowed to decide, and a badge for the number pending. Page text SHALL be translated in all supported languages.

#### Scenario: Viewing without deciding rights
- **WHEN** a person with `agent-requests:read` only opens the page
- **THEN** they see requests but no approve or reject actions
