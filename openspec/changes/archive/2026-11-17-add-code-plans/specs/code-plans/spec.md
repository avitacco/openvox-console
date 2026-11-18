# Spec Delta

## Purpose
Discovers plans in each environment's deployed code and runs YAML plans step by
step, passing results between steps, with every task step checked against node
scope and task permissions as it is reached.

## ADDED Requirements

### Requirement: Plans in environment code are listed
The system SHALL list the YAML plans in an environment's modules, naming each
as Puppet does (`module::plan`, or `module` for `init.yaml`), with its
description and parameters, to a user with `orchestrator:read`. A plan file
that cannot be used SHALL be listed as unusable with the reason.

#### Scenario: Listing an environment's plans
- **WHEN** a user lists the plans of `production`
- **THEN** each plan under a module's `plans` directory appears with its description and parameters

#### Scenario: A plan outside the supported subset
- **WHEN** a plan has a `command` step
- **THEN** it is listed as unusable, naming that step and saying commands are not supported

#### Scenario: No code on this instance
- **WHEN** the instance has no deployed code directory
- **THEN** listing says plans are not available here, not that there are none

### Requirement: Plan parameters are typed and checked
The system SHALL check the parameters a plan is started with against the types
and defaults the plan declares, refuse unknown or missing ones, and refuse
the request before anything starts when a parameter is wrong, naming it.

#### Scenario: A missing required parameter
- **WHEN** a plan declares a parameter without a default and it is not given
- **THEN** the request is refused naming the parameter

#### Scenario: A wrong type
- **WHEN** an Integer parameter is given text
- **THEN** the request is refused naming the parameter and the type

### Requirement: A plan's steps run in order with results passed on
The system SHALL run a plan's steps one after another, evaluating each step's
targets and parameters from the plan's parameters and the results of earlier
steps, running a task, another plan, or printing a message, and SHALL give the
plan's `return` value as its result.

#### Scenario: A result feeds the next step
- **WHEN** a first step runs a task and a second step names the first step's successful targets as its targets
- **THEN** the second step runs on only the nodes the first succeeded on

#### Scenario: A message
- **WHEN** a step prints a message that refers to a parameter
- **THEN** the message, with the value filled in, is in the run's trace

#### Scenario: A failed step ends the plan
- **WHEN** a task step fails on any target
- **THEN** the plan fails, the later steps are recorded as not run, and nothing more is sent

#### Scenario: A plan calling a plan
- **WHEN** a step runs another plan with parameters
- **THEN** it runs as part of this run with its own trace, and its return value is the step's result

### Requirement: Only the supported syntax is accepted
The system SHALL refuse a plan that uses a step type, key or expression outside
the supported subset, when it is loaded and before any run, naming the step,
and SHALL NOT run part of it.

#### Scenario: A Puppet-language expression
- **WHEN** a plan uses an `eval` step
- **THEN** it is unusable, with a reason naming the step

### Requirement: Every task step is checked as it is reached
The system SHALL check each task step, when it is reached, against the node
scope and task permissions of the user who started the plan, exactly as an
on-demand task is, and SHALL fail the plan at a step that would be refused,
naming it. A step may target agentless targets.

#### Scenario: A node outside the caller's groups
- **WHEN** a plan's step names a node the starting user may not act on
- **THEN** the plan fails at that step with the refusal and nothing is sent to that step's nodes

#### Scenario: A task the user may not run
- **WHEN** a plan's step runs a task the user's roles do not allow
- **THEN** the request is refused before the plan starts if the task is named in the plan, naming the step

### Requirement: Targets can be chosen like any job's
The system SHALL accept a plan's target parameters as a list of node names, a
node group or a query, resolve a group or query once when the plan starts,
within the starting user's node scope, and refuse names outside it.

#### Scenario: A node group
- **WHEN** a plan is started with a node group as its targets
- **THEN** the plan's steps use the group's nodes in the starting user's scope as they were at start

### Requirement: Sensitive values stay out of every record
The system SHALL keep the value of a `Sensitive` parameter, and anything made
from it, out of stored parameters, the trace, messages, the result, logs and
audit entries, and SHALL pass it to a task only as that task's parameter.

#### Scenario: A sensitive parameter in a message
- **WHEN** a message refers to a Sensitive parameter
- **THEN** the trace shows a redacted placeholder

### Requirement: Runs are recorded, audited and bounded
The system SHALL record a run's steps in order with their state, time, job and
output, audit when a plan is started and when it ends, and bound a run by
the number of steps it executes, the depth of plans calling plans and its
total time, failing the plan when a bound is reached.

#### Scenario: A plan that never ends
- **WHEN** plans call each other deeper than the limit
- **THEN** the plan fails naming the limit

#### Scenario: Auditing
- **WHEN** a user starts a plan
- **THEN** an audit entry records who, the plan, its environment and the names of the parameters given, never their values

### Requirement: Console UI for code plans
The console SHALL list code plans on the Plans page, show their parameters and
unusable reasons, let a user with `orchestrator:run` fill in parameters and run
one, and show a run's trace and returned value on its job page. Strings SHALL be
translated in every supported language.

#### Scenario: Running a plan
- **WHEN** a user picks a plan, gives its parameters and starts it
- **THEN** the job page opens and shows each step as it runs

#### Scenario: Without orchestrator:run
- **WHEN** a user lacks the permission
- **THEN** the page lists plans and offers no way to run one
