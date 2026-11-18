# Spec Delta

## ADDED Requirements

### Requirement: A task job may include agentless targets
The system SHALL let a task job, including a task step of a plan or a
scheduled task, name agentless targets together with nodes that have an agent,
checked against node scope and task permissions as any target is, and SHALL
track each target to its own result.

#### Scenario: A mixed job
- **WHEN** a task is run on one node with an agent and one agentless target
- **THEN** each gets its own result and the job completes when both have

### Requirement: Runs that need the agent are refused for agentless targets
The system SHALL refuse, before creating anything, a Puppet run, a patch run,
or a plan with a run step whose targets include an agentless target.

#### Scenario: A plan with a run step
- **WHEN** a plan with a run step is started on a group containing an agentless target
- **THEN** it is refused whole, naming the step and the target
