## 1. Storage and effective roles

- [x] 1.1 Migration 000035: `user_groups`, `user_group_members`, `user_group_roles`, case-insensitive unique name, and the `user_effective_roles` view; down migration. Verify with a migration test (up, down, up) and that the view returns exactly `user_roles` when no group exists
- [x] 1.2 Group store in `internal/rbac`: create, rename, delete, list, get with members and roles, add and remove member, grant and revoke role; name rules and `409` conflicts. Verify with store tests for each scenario under "A user group is a named set of users"
- [x] 1.3 Read effective roles in `ListUserRoles` (with `via`), `UserPermissions`, `UserNodeScopes` and `UserTaskGrants`. Verify with store tests for every scenario under "Effective roles combine direct and group roles", including the pairing example and a role held twice
- [x] 1.4 Cascades and the last-administrator guard: deleting a user or role cleans up memberships and grants; deleting a group, revoking its role or removing a member is refused when it would leave no `rbac:admin` holder. Verify with tests for each, and that direct paths are unchanged
- [x] 1.5 Guard test: no non-test Go file outside role assignment and OIDC reconciliation reads `user_roles`. Verify by adding a read locally and seeing it fail

## 2. API and audit

- [x] 2.1 Handlers and routes under `/api/v1/user-groups` gated by `rbac:admin`, `confirm=true` for a group with members, `via` on `GET /users/{id}/roles`. Verify with handler tests for each scenario, including `403` for a caller without `rbac:admin`
- [x] 2.2 Audit entries for every group change, member count on delete, no secrets. Verify with tests on the recorded entries
- [x] 2.3 End to end: a member's token carries the group's role's permissions, scopes and task grants, and a schedule owned by a member is skipped after they leave the group. Verify with the cluster test harness used for week 7

## 3. Console

- [x] 3.1 Groups page: list, create, rename, delete with the confirmation naming the member count, members, roles; route, navigation entry and permission gating. Verify in the browser
- [x] 3.2 Users page: groups and direct versus via-group roles, the latter read-only with the group's name. Roles page: the groups holding each role. Verify in the browser
- [x] 3.3 Every new string in all 15 locales (`go run ./i18n extract|merge|status`); unit tests for the new frontend modules; i18n lint passes. Verify with the locale status at 100% and `node --test`

## 4. Docs and verification

- [x] 4.1 `marketing/guides/use/access.md` (groups, how roles combine, deleting a group, the last-administrator rule) and `operations.md`; `make marketing-build` passes
- [x] 4.2 Live verification on the dev stack: a user with no direct roles who gains `patcher` by group membership and loses it on removal; the pairing example across a direct role and a group role; a schedule after leaving the group; the Groups, Users and Roles pages with screenshots in `evidence/`; test data removed afterwards
- [x] 4.3 Full suite, vet, gofmt, frontend tests and `openspec validate add-user-groups --strict` pass
