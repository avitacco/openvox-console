## Purpose

Lets an authorized operator sign, revoke, or clean a node's Puppet
certificate directly from the console, completing the certificate
lifecycle that certificate status reporting already surfaces read-only.

## ADDED Requirements

### Requirement: Signing a pending certificate request
The system SHALL allow an authorized user to sign a node's pending
(`requested`) certificate, using the configured CA-client credential.

#### Scenario: Signing a pending request
- **WHEN** an authorized user signs a node whose certificate is currently
  in the `requested` state
- **THEN** the CA signs that node's certificate and its status
  subsequently reports as `signed`

#### Scenario: Signing a node that has no pending request
- **WHEN** an authorized user attempts to sign a node whose certificate
  is not in the `requested` state (already `signed`, `revoked`, or
  unknown to the CA)
- **THEN** the system rejects the action with an error explaining the
  certificate is not pending, rather than silently doing nothing or
  fabricating success

### Requirement: Revoking a signed certificate
The system SHALL allow an authorized user to revoke a node's currently
`signed` certificate.

#### Scenario: Revoking a signed certificate
- **WHEN** an authorized user revokes a node whose certificate is
  currently `signed`
- **THEN** the CA revokes that node's certificate, its status
  subsequently reports as `revoked`, and any node transport connection
  that node currently holds SHALL be rejected on its next authentication
  attempt

#### Scenario: Revoking a certificate that is not signed
- **WHEN** an authorized user attempts to revoke a node whose certificate
  is not currently `signed`
- **THEN** the system rejects the action with an error explaining the
  certificate is not currently signed

### Requirement: Cleaning a certificate
The system SHALL allow an authorized user to clean a node's certificate
(revoking it if still signed, and removing it from the CA so the same
certname can request and be issued a new certificate).

#### Scenario: Cleaning a node's certificate
- **WHEN** an authorized user cleans a node
- **THEN** the CA removes that node's certificate record entirely, and a
  subsequent certificate request for the same certname is treated as a
  new, unsigned request rather than a conflicting duplicate

#### Scenario: Cleaning a node the CA has no record of
- **WHEN** an authorized user attempts to clean a certname the CA has no
  certificate record for
- **THEN** the system rejects the action with an error explaining there
  is nothing to clean, rather than reporting false success

### Requirement: Certificate actions require a dedicated permission
The system SHALL require a permission distinct from read-only certificate
status visibility before allowing any sign, revoke, or clean action -
holding read access to certificate status SHALL NOT by itself permit
performing any of these actions.

#### Scenario: An authorized user can act
- **WHEN** a user holding the certificate-management permission signs,
  revokes, or cleans a node's certificate
- **THEN** the system performs the action

#### Scenario: A read-only user cannot act
- **WHEN** a user who can view certificate status, but does not hold the
  certificate-management permission, attempts to sign, revoke, or clean a
  node's certificate
- **THEN** the system rejects the action as unauthorized, and no change
  is made to that node's certificate

### Requirement: Certificate actions require certificate management to be configured
The system SHALL reject every sign, revoke, and clean action with a clear
"not configured" error when the CA-client credential used for certificate
status reporting is not configured, rather than attempting the action or
silently no-oping.

#### Scenario: Acting when certificate status reporting is not configured
- **WHEN** an authorized user attempts to sign, revoke, or clean a
  node's certificate while certificate status reporting is not
  configured
- **THEN** the system rejects the action with an error explaining
  certificate management is not configured, rather than attempting the
  action

### Requirement: Certificate actions are audit logged
The system SHALL record an audit log entry for every sign, revoke, and
clean action, identifying the acting user, the target certname, and the
outcome.

#### Scenario: A successful action is audit logged
- **WHEN** an authorized user successfully signs, revokes, or cleans a
  node's certificate
- **THEN** the system records an audit log entry identifying the acting
  user, the action taken, and the target node

#### Scenario: A rejected action is not falsely logged as successful
- **WHEN** a certificate action is rejected (unauthorized, not
  configured, or the certificate is not in the required state)
- **THEN** the system does not record an audit log entry claiming that
  action succeeded
