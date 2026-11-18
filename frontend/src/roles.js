import { fetchJSON, sendJSON, escapeHtml, requirePermission, ALL_PERMISSIONS, withLoading, t, treeOrder } from './app.js';
import { parsePatterns, formatPatterns, patternBadges, showPatternError } from './task-patterns.js';

if (requirePermission('rbac:admin')) {
  const results = document.getElementById('results');
  const createError = document.getElementById('create-error');
  const newRoleName = document.getElementById('new-role-name');
  const newRolePermissions = document.getElementById('new-role-permissions');
  const newRoleGroups = document.getElementById('new-role-groups');
  const newRoleTasks = document.getElementById('new-role-tasks');
  const newRoleTasksError = document.getElementById('new-role-tasks-error');

  // The node group tree, depth first, or null when it could not be
  // loaded - an administrator of users and roles does not necessarily
  // hold classifier:read. Without it a role's permissions can still be
  // edited; its node groups are left as they are, because every request
  // below leaves nodeGroups out unless the picker was shown.
  let groupTree = null;

  newRolePermissions.innerHTML = ALL_PERMISSIONS.map(
    (p) => `<vox-checkbox value="${p}">${escapeHtml(p)}</vox-checkbox>`
  ).join('');

  function permissionCheckboxes(role) {
    return ALL_PERMISSIONS.map((p) => {
      const checked = role.permissions.includes(p) ? 'checked' : '';
      return `<vox-checkbox data-role-id="${role.id}" data-permission="${p}" ${checked}>${escapeHtml(p)}</vox-checkbox>`;
    }).join('');
  }

  // The group tree as a column of checkboxes, indented by depth.
  // selected is the set of group ids to tick.
  function groupCheckboxes(selected) {
    return groupTree
      .map(({ group: g, depth }) => {
        const checked = selected.has(g.id) ? 'checked' : '';
        return `<vox-checkbox data-group-id="${g.id}" ${checked} style="padding-inline-start: ${depth * 1.5}rem">${escapeHtml(g.name)}</vox-checkbox>`;
      })
      .join('');
  }

  function checkedGroupIds(container) {
    return Array.from(container.querySelectorAll('vox-checkbox[data-group-id]'))
      .filter((cb) => cb.checked)
      .map((cb) => Number(cb.dataset.groupId));
  }

  function renderNewRoleGroups() {
    if (groupTree === null) {
      newRoleGroups.innerHTML = `<vox-alert variant="info">${t('Node groups could not be loaded, so the new role will cover all nodes. Viewing node groups needs the classifier:read permission.')}</vox-alert>`;
      return;
    }
    const root = groupTree.find(({ group }) => group.isRoot);
    newRoleGroups.innerHTML = groupCheckboxes(new Set(root ? [root.group.id] : []));
  }

  // What a role's node groups cell says: the groups by name, "All
  // Nodes" alone when the root is among them (it already covers the
  // rest), or a warning when there are none - such a role's permissions
  // apply to no node at all, which is rarely what was meant.
  function groupSummary(role) {
    const ids = role.nodeGroups || [];
    if (ids.length === 0) {
      return `<vox-badge variant="warning">${t('No node groups')}</vox-badge>`;
    }
    if (groupTree === null) {
      return `<vox-badge variant="neutral">${t('{count} node groups', { count: ids.length })}</vox-badge>`;
    }
    const byId = new Map(groupTree.map(({ group }) => [group.id, group]));
    const groups = ids.map((id) => byId.get(id)).filter(Boolean);
    const root = groups.find((g) => g.isRoot);
    const shown = root ? [root] : groups;
    return shown
      .map((g) => `<vox-badge variant="${g.isRoot ? 'tip' : 'neutral'}">${escapeHtml(g.name)}</vox-badge>`)
      .join(' ');
  }

  function showError(message) {
    results.insertAdjacentHTML('afterbegin', `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`);
  }

  // The user groups a role is granted to, under its name. Empty when none,
  // and when the groups could not be loaded.
  let userGroups = [];
  function heldByHTML(role) {
    const holders = userGroups.filter((g) => g.roles.some((gr) => gr.id === role.id));
    if (holders.length === 0) return '';
    const badges = holders.map((g) => `<vox-badge variant="neutral">${escapeHtml(g.name)}</vox-badge>`).join(' ');
    return `<div class="vox-display-flex vox-flex-wrap vox-items-center vox-gap-sm vox-m-top-sm"><span class="vox-ts-sm vox-color-text-2">${escapeHtml(t('Granted to groups:'))}</span> ${badges}</div>`;
  }

  function renderRoles(roles) {
    if (roles.length === 0) {
      results.innerHTML = `
        <vox-empty-state heading="${t('No roles yet')}">
          <vox-icon slot="icon" name="shield" size="lg"></vox-icon>
          Create a role above to start assigning permissions.
        </vox-empty-state>`;
      return;
    }

    const rows = roles
      .map(
        (r) => `
      <tr data-role-row="${r.id}">
        <td>${escapeHtml(r.name)}${heldByHTML(r)}</td>
        <td><div class="vox-display-flex vox-flex-wrap vox-gap-sm">${permissionCheckboxes(r)}</div></td>
        <td>
          <div class="vox-display-flex vox-flex-wrap vox-items-center vox-gap-sm" data-group-summary="${r.id}">
            ${groupSummary(r)}
            ${groupTree === null ? '' : `<vox-button data-edit-groups="${r.id}" variant="alt" size="sm">${t('Change')}</vox-button>`}
          </div>
          <div class="role-group-editor vox-hide" data-group-editor="${r.id}">
            <div class="role-group-tree vox-m-bottom-sm">${groupTree === null ? '' : groupCheckboxes(new Set(r.nodeGroups || []))}</div>
            <div class="vox-display-flex vox-gap-sm">
              <vox-button data-save-groups="${r.id}" variant="brand" size="sm">${t('Save node groups')}</vox-button>
              <vox-button data-cancel-groups="${r.id}" variant="alt" size="sm">${t('Cancel')}</vox-button>
            </div>
          </div>
        </td>
        <td>
          <div class="vox-display-flex vox-flex-wrap vox-items-center vox-gap-sm" data-task-summary="${r.id}">
            ${patternBadges(r.taskPatterns)}
            <vox-button data-edit-tasks="${r.id}" variant="alt" size="sm">${t('Change')}</vox-button>
          </div>
          <div class="role-group-editor vox-hide" data-task-editor="${r.id}">
            <vox-textarea data-task-field="${r.id}" rows="4" class="vox-width-full vox-m-bottom-sm" label="${escapeHtml(t('Tasks it may run'))}"
              note="${escapeHtml(t('One per line: a task name such as package::install, a module such as patch::*, or * for every task. Leave it empty to allow every task. This limits orchestrator:run; the role\'s node groups still decide which nodes.'))}"></vox-textarea>
            <div data-task-error="${r.id}"></div>
            <div class="vox-display-flex vox-gap-sm">
              <vox-button data-save-tasks="${r.id}" variant="brand" size="sm">${t('Save tasks')}</vox-button>
              <vox-button data-cancel-tasks="${r.id}" variant="alt" size="sm">${t('Cancel')}</vox-button>
            </div>
          </div>
        </td>
        <td><vox-button data-delete-id="${r.id}" variant="danger" size="sm">${t('Delete')}</vox-button></td>
      </tr>`
      )
      .join('');

    results.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">${t('Name')}</th><th scope="col">${t('Permissions')}</th><th scope="col">${t('Node groups')}</th><th scope="col">${t('Tasks')}</th><th scope="col"></th></tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;

    const rolesById = new Map(roles.map((r) => [String(r.id), r]));

    results.querySelectorAll('vox-checkbox[data-role-id]').forEach((el) => {
      el.addEventListener('change', async () => {
        const roleId = el.dataset.roleId;
        const row = el.closest('tr');
        const permissions = Array.from(row.querySelectorAll('vox-checkbox[data-permission]'))
          .filter((cb) => cb.checked)
          .map((cb) => cb.dataset.permission);
        try {
          // No nodeGroups: a permission change leaves the role's node
          // groups as they are.
          await sendJSON(`/api/v1/roles/${roleId}`, 'PUT', { name: rolesById.get(roleId).name, permissions });
          rolesById.get(roleId).permissions = permissions;
        } catch (err) {
          el.checked = !el.checked;
          showError(err.message);
        }
      });
    });

    const toggleEditor = (roleId, open) => {
      results.querySelector(`[data-group-editor="${roleId}"]`).classList.toggle('vox-hide', !open);
      results.querySelector(`[data-group-summary="${roleId}"]`).classList.toggle('vox-hide', open);
    };

    results.querySelectorAll('vox-button[data-edit-groups]').forEach((el) => {
      el.addEventListener('click', () => toggleEditor(el.dataset.editGroups, true));
    });

    results.querySelectorAll('vox-button[data-cancel-groups]').forEach((el) => {
      el.addEventListener('click', () => load());
    });

    results.querySelectorAll('vox-button[data-save-groups]').forEach((el) => {
      el.addEventListener('click', async () => {
        const role = rolesById.get(el.dataset.saveGroups);
        const editor = results.querySelector(`[data-group-editor="${role.id}"]`);
        try {
          await sendJSON(`/api/v1/roles/${role.id}`, 'PUT', {
            name: role.name,
            permissions: role.permissions,
            nodeGroups: checkedGroupIds(editor),
          });
          await load();
        } catch (err) {
          showError(err.message);
        }
      });
    });

    // A role's task allowlist, edited in place like its node groups. The
    // field's value is set after the row is built: a textarea's content is
    // not markup.
    results.querySelectorAll('vox-textarea[data-task-field]').forEach((el) => {
      el.value = formatPatterns(rolesById.get(el.dataset.taskField).taskPatterns);
    });

    const toggleTaskEditor = (roleId, open) => {
      results.querySelector(`[data-task-editor="${roleId}"]`).classList.toggle('vox-hide', !open);
      results.querySelector(`[data-task-summary="${roleId}"]`).classList.toggle('vox-hide', open);
    };

    results.querySelectorAll('vox-button[data-edit-tasks]').forEach((el) => {
      el.addEventListener('click', () => toggleTaskEditor(el.dataset.editTasks, true));
    });

    results.querySelectorAll('vox-button[data-cancel-tasks]').forEach((el) => {
      el.addEventListener('click', () => load());
    });

    results.querySelectorAll('vox-button[data-save-tasks]').forEach((el) => {
      el.addEventListener('click', async () => {
        const role = rolesById.get(el.dataset.saveTasks);
        const field = results.querySelector(`vox-textarea[data-task-field="${role.id}"]`);
        const place = results.querySelector(`[data-task-error="${role.id}"]`);
        place.innerHTML = '';
        try {
          // No nodeGroups: changing the allowlist leaves them as they are.
          await sendJSON(`/api/v1/roles/${role.id}`, 'PUT', {
            name: role.name,
            permissions: role.permissions,
            taskPatterns: parsePatterns(field.value),
          });
          await load();
        } catch (err) {
          // The editor stays open with what was entered, the reason on
          // the field.
          if (!showPatternError(place, err)) place.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
        }
      });
    });

    results.querySelectorAll('vox-button[data-delete-id]').forEach((el) => {
      el.addEventListener('click', async () => {
        try {
          await sendJSON(`/api/v1/roles/${el.dataset.deleteId}`, 'DELETE');
          await load();
        } catch (err) {
          showError(err.message);
        }
      });
    });
  }

  async function load() {
    try {
      const roles = await withLoading(results, () => fetchJSON('/api/v1/roles'));
      // Best effort: the roles still show if the groups cannot be read.
      userGroups = await fetchJSON('/api/v1/user-groups').catch(() => []);
      renderRoles(roles);
    } catch (err) {
      results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  async function loadGroups() {
    try {
      const page = await fetchJSON('/api/v1/groups?all=true');
      groupTree = treeOrder(page.items);
    } catch {
      groupTree = null;
    }
    renderNewRoleGroups();
  }

  document.getElementById('create-role').addEventListener('click', async () => {
    createError.innerHTML = '';
    const name = newRoleName.value.trim();
    const permissions = Array.from(newRolePermissions.querySelectorAll('vox-checkbox'))
      .filter((cb) => cb.checked)
      .map((cb) => cb.value);
    if (!name) {
      createError.innerHTML = `<vox-alert variant="danger">Name is required.</vox-alert>`;
      return;
    }
    newRoleTasksError.innerHTML = '';
    const body = { name, permissions, taskPatterns: parsePatterns(newRoleTasks.value) };
    // Left out when the picker could not be shown: the server then
    // grants the role on All Nodes, which is what the notice says.
    if (groupTree !== null) body.nodeGroups = checkedGroupIds(newRoleGroups);
    try {
      await sendJSON('/api/v1/roles', 'POST', body);
      newRoleName.value = '';
      newRolePermissions.querySelectorAll('vox-checkbox').forEach((cb) => (cb.checked = false));
      newRoleTasks.value = '';
      renderNewRoleGroups();
      await load();
    } catch (err) {
      // A refused pattern is shown on its field, with everything entered
      // kept; anything else where errors are shown.
      if (!showPatternError(newRoleTasksError, err)) {
        createError.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
      }
    }
  });

  // Groups first: the roles table names each role's groups.
  loadGroups().then(load);
}
