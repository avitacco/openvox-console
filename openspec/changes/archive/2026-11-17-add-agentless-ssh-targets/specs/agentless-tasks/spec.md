# Spec Delta

## Purpose
Runs Puppet tasks on agentless targets over SSH, with results recorded and
limits applied as they are for a node with an agent.

## ADDED Requirements

### Requirement: A task runs on an agentless target
The system SHALL run a task on an agentless target by connecting over SSH,
copying the task's files, verified against the checksums the Puppet server
reports, to a private temporary directory on the host, running the chosen
implementation with the task's parameters, and recording its output, exit
status and structured result as it does for a node with an agent.

#### Scenario: A shell task
- **WHEN** a task with a shell implementation is run on an agentless target
- **THEN** its stdout, exit status and parsed result are recorded on the target

#### Scenario: A checksum mismatch
- **WHEN** a downloaded task file does not match the reported checksum
- **THEN** nothing is sent to the host and the target fails with that reason

### Requirement: Task input follows the task's metadata
The system SHALL pass parameters by the input method the task declares
(environment variables, standard input, or both), SHALL treat an agentless
target as providing a shell and not Puppet, and SHALL fail a target for which
no implementation fits, naming what the task offers.

#### Scenario: A task that needs the Puppet agent
- **WHEN** every implementation of a task requires `puppet-agent`
- **THEN** the target fails saying no implementation suits it

### Requirement: Parameters and credentials stay out of the record
The system SHALL keep sensitive task parameters off the host's disk and out
of command lines, and SHALL NOT record a credential in a job, its output or an
error.

#### Scenario: A sensitive parameter
- **WHEN** a task with a sensitive parameter runs on an agentless target
- **THEN** the value reaches the task only through its input and is not in any process argument

### Requirement: Runs are bounded and cleaned up
The system SHALL bound connecting, each task by its timeout, and the output
kept, SHALL run one task at a time per target and a bounded number across all
targets, and SHALL remove the temporary directory whether the task succeeds,
fails or times out.

#### Scenario: A task that times out
- **WHEN** a task exceeds its timeout
- **THEN** it is stopped, the target fails with a timeout reason, and the directory is removed

#### Scenario: A target already busy
- **WHEN** a task is started on a target that is running another
- **THEN** the second fails as busy and the first is unaffected

### Requirement: Only tasks run on agentless targets
The system SHALL refuse a Puppet run, a patch run, or a plan run step that
names an agentless target, whole and before anything is sent, naming the
target and saying tasks are all they support.

#### Scenario: A Puppet run on an agentless target
- **WHEN** a Puppet run names an agentless target
- **THEN** the request is refused naming it and no job is created

### Requirement: Failures say which stage failed
The system SHALL record a target's failure as one of: could not connect,
host key not trusted or changed, authentication failed, could not become the
run-as user, file transfer failed, or the task's own failure, with the reason.

#### Scenario: An untrusted host key
- **WHEN** a task is started on a target whose key was never accepted
- **THEN** the target fails as not trusted and nothing was run
