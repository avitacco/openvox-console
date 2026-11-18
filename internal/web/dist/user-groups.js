// The user groups page: groups of users that roles are granted to, for an
// administrator (see specs/user-groups).
import { fetchJSON, sendJSON, escapeHtml, requirePermission, withLoading, confirmDialog, t, tn } from './app.js';

if (requirePermission('rbac:admin')) {
  const results = document.getElementById('results');
  const createError = document.getElementById('create-error');
  const newGroupName = document.getElementById('new-group-name');

  let allRoles = [];
  let allUsers = [];

  function showError(message) {
    results.insertAdjacentHTML('afterbegin', `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`);
  }

  function memberSummary(group) {
    if (group.memberCount === 0) {
      return `<vox-badge variant="neutral">${escapeHtml(t('No members'))}</vox-badge>`;
    }
    return `<vox-badge variant="neutral">${escapeHtml(tn('{count} member', '{count} members', group.memberCount, { count: group.memberCount }))}</vox-badge>`;
  }

  function roleCheckboxes(group) {
    if (allRoles.length === 0) {
      return `<span class="vox-ts-sm vox-color-text-2">${escapeHtml(t('No roles yet'))}</span>`;
    }
    const held = new Set(group.roles.map((r) => r.id));
    return allRoles
      .map((role) => `<vox-checkbox data-group-id="${group.id}" data-role-id="${role.id}" ${held.has(role.id) ? 'checked' : ''}>${escapeHtml(role.name)}</vox-checkbox>`)
      .join('');
  }

  // The members dialog: one checkbox per user, each saved as it is
  // ticked, since adding and removing a member are separate requests.
  async function openMembersDialog(group) {
    let current;
    try {
      current = await fetchJSON(`/api/v1/user-groups/${group.id}`);
    } catch (err) {
      showError(err.message);
      return;
    }
    const members = new Set(current.members.map((m) => m.id));
    const dialog = document.createElement('vox-dialog');
    dialog.heading = t('Members of {group}', { group: group.name });
    dialog.setAttribute('light-dismiss', '');
    dialog.innerHTML = `
      <div data-error></div>
      <div class="vox-display-flex vox-flex-direction-column vox-gap-sm">
        ${allUsers.length === 0
          ? `<span class="vox-ts-sm vox-color-text-2">${escapeHtml(t('No users yet'))}</span>`
          : allUsers.map((u) => `<vox-checkbox data-user-id="${u.id}" ${members.has(u.id) ? 'checked' : ''}>${escapeHtml(u.username)}</vox-checkbox>`).join('')}
      </div>
      <div slot="footer">
        <vox-button variant="brand" data-action="done">${escapeHtml(t('Done'))}</vox-button>
      </div>`;
    const errorEl = dialog.querySelector('[data-error]');
    dialog.querySelectorAll('vox-checkbox[data-user-id]').forEach((el) => {
      el.addEventListener('change', async () => {
        errorEl.innerHTML = '';
        try {
          await sendJSON(`/api/v1/user-groups/${group.id}/members/${el.dataset.userId}`, el.checked ? 'POST' : 'DELETE');
        } catch (err) {
          el.checked = !el.checked;
          errorEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
        }
      });
    });
    dialog.querySelector('[data-action="done"]').addEventListener('click', () => dialog.close());
    dialog.addEventListener('vox-close', () => {
      dialog.remove();
      load();
    });
    document.body.appendChild(dialog);
    dialog.show();
  }

  function openRenameDialog(group) {
    const dialog = document.createElement('vox-dialog');
    dialog.heading = t('Rename {group}', { group: group.name });
    dialog.setAttribute('light-dismiss', '');
    dialog.innerHTML = `
      <vox-input data-name label="${escapeHtml(t('Name'))}"></vox-input>
      <div data-error></div>
      <div slot="footer">
        <vox-button variant="alt" data-action="cancel">${escapeHtml(t('Cancel'))}</vox-button>
        <vox-button variant="brand" data-action="save">${escapeHtml(t('Rename'))}</vox-button>
      </div>`;
    const nameEl = dialog.querySelector('[data-name]');
    nameEl.value = group.name;
    const errorEl = dialog.querySelector('[data-error]');
    dialog.addEventListener('vox-close', () => dialog.remove());
    dialog.querySelector('[data-action="cancel"]').addEventListener('click', () => dialog.close());
    dialog.querySelector('[data-action="save"]').addEventListener('click', async () => {
      errorEl.innerHTML = '';
      try {
        await sendJSON(`/api/v1/user-groups/${group.id}`, 'PUT', { name: nameEl.value });
        dialog.close();
        await load();
      } catch (err) {
        errorEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.fields?.name || err.message)}</vox-alert>`;
      }
    });
    document.body.appendChild(dialog);
    dialog.show();
  }

  async function deleteGroup(group) {
    if (group.memberCount > 0) {
      const ok = await confirmDialog({
        heading: t('Delete {group}?', { group: group.name }),
        body: `<p>${escapeHtml(tn(
          '{count} person will lose the roles this group gives them.',
          '{count} people will lose the roles this group gives them.',
          group.memberCount,
          { count: group.memberCount },
        ))}</p>`,
        confirmLabel: t('Delete'),
        danger: true,
      });
      if (!ok) return;
    }
    try {
      await fetchJSON(`/api/v1/user-groups/${group.id}${group.memberCount > 0 ? '?confirm=true' : ''}`, { method: 'DELETE' });
      await load();
    } catch (err) {
      showError(err.message);
    }
  }

  function renderGroups(groups) {
    if (groups.length === 0) {
      results.innerHTML = `
        <vox-empty-state heading="${escapeHtml(t('No groups yet'))}">
          <vox-icon slot="icon" name="person" size="lg"></vox-icon>
          ${escapeHtml(t('Create a group above, then add members and grant it roles.'))}
        </vox-empty-state>`;
      return;
    }
    const rows = groups
      .map(
        (g) => `
      <tr data-group-row="${g.id}">
        <td>${escapeHtml(g.name)}</td>
        <td>
          <div class="vox-display-flex vox-flex-wrap vox-items-center vox-gap-sm">
            ${memberSummary(g)}
            <vox-button data-members-id="${g.id}" variant="alt" size="sm">${escapeHtml(t('Change'))}</vox-button>
          </div>
        </td>
        <td><div class="vox-display-flex vox-flex-wrap vox-gap-sm">${roleCheckboxes(g)}</div></td>
        <td>
          <div class="vox-display-flex vox-gap-sm">
            <vox-button data-rename-id="${g.id}" variant="alt" size="sm">${escapeHtml(t('Rename'))}</vox-button>
            <vox-button data-delete-id="${g.id}" variant="danger" size="sm">${escapeHtml(t('Delete'))}</vox-button>
          </div>
        </td>
      </tr>`,
      )
      .join('');

    results.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">${escapeHtml(t('Name'))}</th><th scope="col">${escapeHtml(t('Members'))}</th><th scope="col">${escapeHtml(t('Roles'))}</th><th scope="col"></th></tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;

    const byId = new Map(groups.map((g) => [String(g.id), g]));
    results.querySelectorAll('vox-checkbox[data-role-id]').forEach((el) => {
      el.addEventListener('change', async () => {
        try {
          await sendJSON(`/api/v1/user-groups/${el.dataset.groupId}/roles/${el.dataset.roleId}`, el.checked ? 'POST' : 'DELETE');
        } catch (err) {
          el.checked = !el.checked;
          showError(err.message);
        }
      });
    });
    results.querySelectorAll('vox-button[data-members-id]').forEach((el) => {
      el.addEventListener('click', () => openMembersDialog(byId.get(el.dataset.membersId)));
    });
    results.querySelectorAll('vox-button[data-rename-id]').forEach((el) => {
      el.addEventListener('click', () => openRenameDialog(byId.get(el.dataset.renameId)));
    });
    results.querySelectorAll('vox-button[data-delete-id]').forEach((el) => {
      el.addEventListener('click', () => deleteGroup(byId.get(el.dataset.deleteId)));
    });
  }

  async function load() {
    try {
      const [groups, roles, users] = await withLoading(results, () =>
        Promise.all([fetchJSON('/api/v1/user-groups'), fetchJSON('/api/v1/roles'), fetchJSON('/api/v1/users')]));
      allRoles = roles;
      allUsers = users;
      renderGroups(groups);
    } catch (err) {
      results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  document.getElementById('create-group').addEventListener('click', async () => {
    createError.innerHTML = '';
    const name = newGroupName.value.trim();
    if (!name) {
      createError.innerHTML = `<vox-alert variant="danger">${escapeHtml(t('Give the group a name.'))}</vox-alert>`;
      return;
    }
    try {
      await sendJSON('/api/v1/user-groups', 'POST', { name });
      newGroupName.value = '';
      await load();
    } catch (err) {
      createError.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.fields?.name || err.message)}</vox-alert>`;
    }
  });

  load();
}
