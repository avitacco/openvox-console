# enc-api Specification

## Purpose

Exposes the console's classification decisions to openvox-server as an
External Node Classifier (ENC), so a node's catalog compilation uses the
classes and parameters this console assigns it - the one external
compatibility surface this phase adds, kept separate from the internal
`classifier` capability so it can be audited and versioned on its own.

## Requirements

### Requirement: ENC classification endpoint
The system SHALL expose an HTTP endpoint that returns a node's
classification (classes, parameters, and environment) as JSON, computed
using the same merge/precedence resolution as the classifier capability.

#### Scenario: Classifying a node with matching groups
- **WHEN** a client requests classification for a node matched by one or
  more groups
- **THEN** the endpoint returns that node's merged classes, parameters,
  and environment as JSON

#### Scenario: Classifying a node with no matching groups
- **WHEN** a client requests classification for a node matched by no group
- **THEN** the endpoint returns an empty classification, not an error

### Requirement: Exec-terminus-compatible bridge
The system SHALL provide a bridge script that, given a node name, calls
the ENC endpoint and writes to standard output the YAML document format
Puppet's exec-based node classifier terminus requires: classes as a hash
(so per-class parameters are supported), a `parameters` key for top-scope
variables, and an optional `environment` key. It SHALL exit 0 on success.

#### Scenario: openvox-server classifies a real node through the bridge
- **WHEN** openvox-server's `node_terminus = exec` invokes the bridge
  script for a node's certname
- **THEN** the script exits 0 and writes a YAML document containing that
  node's classes (hash form), parameters, and environment, matching the
  classification the ENC endpoint returned for that node

#### Scenario: A classified node's classes reach the compiled catalog
- **WHEN** openvox-server compiles a catalog for a node classified with at
  least one class by this console
- **THEN** the compiled catalog includes resources from that class

### Requirement: ENC endpoint requires a scoped service token
The system SHALL require a valid, unexpired, unrevoked access token
carrying the `enc:read` permission on the ENC classification endpoint.
This is expected to be a service token (see the `rbac` capability) issued
to the exec-terminus bridge, not a user login.

#### Scenario: Unauthenticated request rejected
- **WHEN** a request to the ENC endpoint has no valid access token
- **THEN** the system rejects the request

#### Scenario: A service token with the required permission succeeds
- **WHEN** the exec-terminus bridge presents a valid service token
  carrying `enc:read`
- **THEN** the system processes the classification request as before
