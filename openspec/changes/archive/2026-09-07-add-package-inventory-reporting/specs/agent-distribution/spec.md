## MODIFIED Requirements

### Requirement: Install script route
The system SHALL serve an install script over HTTP that, when run on a
supported node, ensures openvoxagent is installed (or upgraded to the
version currently available from its normal package source), installs
this console's own node-agent build for the node's platform, configured
to connect to this console's NATS-based node transport, and sets up
package-inventory reporting for that node's platform so its installed
packages are included in its next Puppet run.

#### Scenario: Fetching the install script
- **WHEN** a request is made to the install script route
- **THEN** the system returns a script that installs or updates
  openvoxagent, installs the node-agent binary and its service unit
  pointed at this console's node transport address, and sets up
  package-inventory reporting for that platform

#### Scenario: Running the script on an already-provisioned node
- **WHEN** the install script runs on a node that already has
  openvoxagent and an up-to-date node-agent installed
- **THEN** it completes without disrupting the existing installation
  (idempotent - safe to re-run)

#### Scenario: A node reports its installed packages on its next run
- **WHEN** a node that has had the install script run on it next
  executes a Puppet run
- **THEN** the packages it reports are visible via openvoxdb's package
  inventory data for that node, without any separate opt-in step beyond
  having run the install script
