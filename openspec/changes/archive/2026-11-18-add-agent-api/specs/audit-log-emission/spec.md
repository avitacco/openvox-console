# Spec Delta

## ADDED Requirements

### Requirement: Agent calls and approvals are audited
The console SHALL audit each agent tool call at the same levels as the equivalent console request, naming the agent token, and SHALL always audit request submission, approval, rejection and expiry with the request id, the kind, the token and (for a decision) the person. Sensitive values SHALL never appear.

#### Scenario: Submission and approval
- **WHEN** an agent submits a request and a person approves it
- **THEN** the audit log holds a submission naming the token and a decision naming the approver and the request id

#### Scenario: No secrets
- **WHEN** a request includes a sensitive parameter
- **THEN** no audit entry contains its value
