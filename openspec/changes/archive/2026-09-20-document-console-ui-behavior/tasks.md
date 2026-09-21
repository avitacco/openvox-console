## 1. Verify each requirement against the shipped behavior

These are verification tasks, not implementation. Each checks that the
written requirement matches what the console already does; a mismatch
means the spec needs correcting, not the code.

- [x] 1.1 Node detail: confirm facts, packages, runs and vulnerabilities are each reachable as their own section, and that capacity facts render proportionally - check against a node reporting filesystems and memory
- [x] 1.2 Node detail: confirm every reported address is attributed to its interface with the primary identifiable, and that the raw presentation exposes facts the structured one omits
- [x] 1.3 Node list: confirm multi-select triggers a run per selected node, that the control is absent without permission, and that filtering or paging clears selection for nodes leaving the view
- [x] 1.4 Package catalogue: confirm it lists packages with no search term, narrows by name/version/provider/group in combination, paginates with a total, and rejects a group filter from a caller lacking permission to read groups
- [x] 1.5 Group listings: confirm counts are present per group, that a group matching nothing reads as zero, and that an unreachable inventory leaves counts unavailable rather than zero
- [x] 1.6 Activity: confirm entries name the acting user, and that a system- or schedule-triggered entry is shown without a user rather than with a placeholder

## 2. Confirm the change introduces nothing

- [x] 2.1 Confirm `git status` shows no source change attributable to this change - it is documentation only
- [x] 2.2 Run `openspec validate --changes document-console-ui-behavior --strict` and confirm it passes
