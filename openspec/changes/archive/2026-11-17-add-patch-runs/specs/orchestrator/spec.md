# Spec Delta

## ADDED Requirements

### Requirement: A job that reports progress is judged by its silence
The system SHALL fail a job of a kind that reports progress when it has
reported none for ten minutes, together with its unfinished targets, and
SHALL NOT fail it for its age alone. Every other kind of job keeps the rule
that fails it two hours after it started.

#### Scenario: A long run that keeps reporting is not reaped
- **WHEN** a patch job has run for five hours and its last progress report
  was a minute ago
- **THEN** it is left running

#### Scenario: A run that stopped reporting is failed
- **WHEN** a patch job's last progress report was eleven minutes ago
- **THEN** the job and its unfinished targets are failed with a reason that
  says progress stopped being reported

#### Scenario: Other kinds are unchanged
- **WHEN** a Puppet run, task or plan has been running for two hours
- **THEN** it is failed as before, however recently anything was reported

### Requirement: A patch run is a job
The system SHALL record a patch run as a job of its own kind, with one target
per node, readable, listed and filtered like any other job and subject to the
same node-scope reading rules.

#### Scenario: A patch run appears in the job list
- **WHEN** a patch run has started
- **THEN** the job list shows it as a patch job with its targets and status
