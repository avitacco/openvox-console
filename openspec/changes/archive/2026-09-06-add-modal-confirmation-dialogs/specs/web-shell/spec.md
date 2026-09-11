## ADDED Requirements

### Requirement: Confirmation dialogs use modal components
The system SHALL confirm a consequential or destructive action using
the `voxblocks` modal dialog component, with explicit confirm and
cancel actions, rather than the browser's native `confirm()` popup.

#### Scenario: Confirming a destructive action
- **WHEN** a user triggers an action that requires confirmation (e.g.
  revoking or cleaning a certificate)
- **THEN** the system presents a modal dialog naming the action and its
  consequence, with distinct confirm and cancel controls, rather than a
  native browser popup

#### Scenario: Cancelling via the dialog does not perform the action
- **WHEN** a user dismisses the confirmation dialog (via its cancel
  control, its close control, or pressing Escape) without confirming
- **THEN** the system does not perform the action that required
  confirmation

#### Scenario: Confirming via the dialog performs the action
- **WHEN** a user selects the dialog's confirm control
- **THEN** the system proceeds with the action that required
  confirmation
