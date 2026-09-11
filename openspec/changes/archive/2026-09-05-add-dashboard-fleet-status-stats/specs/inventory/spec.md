## ADDED Requirements

### Requirement: Fleet status summary
The system SHALL report, across every known node, counts of exactly
four categories based on each node's latest report: `failed` (the
latest report failed), `corrected` (the latest report changed and
Puppet corrected at least one unexpectedly-drifted resource),
`intentional` (the latest report changed with no corrective drift
correction involved), and `unchanged` (the latest report applied
cleanly with no changes). A node whose latest report is `noop`, or that
has no report at all, SHALL be excluded from all four categories rather
than forced into one. The system SHALL also report the total count of
every known node, independent of and not reduced by the four
categories or any exclusion.

#### Scenario: A failed node is counted as failed
- **WHEN** the fleet status summary is requested and a node's latest
  report status is `failed`
- **THEN** that node is counted under `failed`

#### Scenario: A node with a corrective change is counted as corrected
- **WHEN** the fleet status summary is requested and a node's latest
  report changed and included at least one corrective (unexpected
  drift) change
- **THEN** that node is counted under `corrected`

#### Scenario: A node with only intentional changes is counted as intentional
- **WHEN** the fleet status summary is requested and a node's latest
  report changed with no corrective change involved (including when
  corrective-change tracking is not enabled for that node, so no
  corrective change can be confirmed)
- **THEN** that node is counted under `intentional`

#### Scenario: An unchanged node is counted as unchanged
- **WHEN** the fleet status summary is requested and a node's latest
  report applied with no changes
- **THEN** that node is counted under `unchanged`

#### Scenario: A noop or unreported node is excluded from the four categories
- **WHEN** the fleet status summary is requested and a node's latest
  report status is `noop`, or the node has no report at all
- **THEN** that node is not counted under `failed`, `corrected`,
  `intentional`, or `unchanged`

#### Scenario: The total count is unaffected by exclusions
- **WHEN** the fleet status summary is requested and at least one node
  is excluded from the four categories
- **THEN** the reported total still counts every known node, including
  the excluded ones
