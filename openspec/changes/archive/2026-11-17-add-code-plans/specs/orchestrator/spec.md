# Spec Delta

## ADDED Requirements

### Requirement: A code plan is a job whose steps are jobs
The system SHALL record the run of a plan from environment code as a job of
its own kind, whose task steps are child jobs listed with it in the order they
ran, together with a trace of every step and the plan's returned value or the
reason it failed, readable under the same node-scope rules as any job.

#### Scenario: A plan run in the job list
- **WHEN** a code plan has run two task steps
- **THEN** the job list shows one code plan job, and reading it shows both steps' jobs and its trace

#### Scenario: A reader limited to some nodes
- **WHEN** a plan ran a task step on nodes the reader may not see
- **THEN** that step's targets are left out and the job says some are hidden

## MODIFIED Requirements

### Requirement: A job that reports progress is judged by its silence
The system SHALL fail a job that reports progress when it has reported none for
ten minutes, together with its unfinished targets, and SHALL NOT fail it for
its age alone. A job that does not report progress keeps the rule that fails it
two hours after it started.

#### Scenario: A long run that keeps reporting is not reaped
- **WHEN** a patch job or code plan job has run for five hours and its last progress report was a minute ago
- **THEN** it is left running

#### Scenario: A run that stopped reporting is failed
- **WHEN** a patch job or code plan job's last progress report was eleven minutes ago
- **THEN** the job and its unfinished targets are failed with a reason that says progress stopped being reported

#### Scenario: Other kinds are unchanged
- **WHEN** a Puppet run, task or saved plan has been running for two hours
- **THEN** it is failed as before, however recently anything was reported
