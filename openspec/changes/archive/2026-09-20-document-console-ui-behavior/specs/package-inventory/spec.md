## ADDED Requirements

### Requirement: Package catalogue browsing
The system SHALL let a user browse the packages installed across the
fleet as a paginated catalogue, without first knowing a package's name.

The catalogue SHALL be narrowable by package name, by version, by
provider, and by node group, in any combination. Filtering by node group
SHALL require the permission needed to read groups: the catalogue is
otherwise readable by anyone who may read nodes, and group filtering
would otherwise disclose which nodes belong to a group to someone not
permitted to know.

#### Scenario: Browsing without a search term
- **WHEN** a user with `nodes:read` opens the package catalogue
- **THEN** the system returns a page of packages installed across the
  fleet, rather than requiring a package name first

#### Scenario: Narrowing the catalogue
- **WHEN** a user applies any combination of name, version, provider and
  group filters
- **THEN** only packages matching every applied filter are returned

#### Scenario: Paging through the catalogue
- **WHEN** more packages match than fit on one page
- **THEN** the results are paginated, reporting the total number of
  matches

#### Scenario: Group filtering without permission to read groups
- **WHEN** a catalogue request filters by node group but does not carry
  the permission to read groups
- **THEN** the system rejects the request rather than returning results

#### Scenario: Nothing matches the filters
- **WHEN** no package matches the applied filters
- **THEN** the system returns an empty result rather than an error
