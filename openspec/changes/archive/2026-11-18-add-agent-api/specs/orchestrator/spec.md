# Spec Delta

## ADDED Requirements

### Requirement: Jobs record the agent request behind them
A job started by approving an agent request SHALL record that request, the agent token's name and the approving person, and the job SHALL show them in its details and in job listings and filters.

#### Scenario: Job from an approved request
- **WHEN** a request is approved and its job runs
- **THEN** the job's details name the request, the agent token and the approver, and the job's "triggered by" is the agent token

#### Scenario: Other jobs unchanged
- **WHEN** a person starts a job directly
- **THEN** it shows no agent origin
