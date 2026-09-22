import { fetchJSON, sendJSON, escapeHtml, requirePermission, applyAvatar, withLoading, t } from './app.js';

if (requirePermission('rbac:admin')) {
  const results = document.getElementById('results');
  const createError = document.getElementById('create-error');
  const newUsername = document.getElementById('new-username');
  const newPassword = document.getElementById('new-password');
  const newFirstName = document.getElementById('new-first-name');
  const newLastName = document.getElementById('new-last-name');
  const newEmail = document.getElementById('new-email');

  let allRoles = [];

  function showCreateError(message) {
    createError.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
  }

  function roleCheckboxes(user) {
    return allRoles
      .map((role) => {
        const checked = user.roleIds.has(role.id) ? 'checked' : '';
        return `
          <vox-checkbox data-user-id="${user.id}" data-role-id="${role.id}" ${checked}>
            ${escapeHtml(role.name)}
          </vox-checkbox>`;
      })
      .join('');
  }

  function renderUsers(users) {
    if (users.length === 0) {
      results.innerHTML = `
        <vox-empty-state heading="${t('No users yet')}">
          <vox-icon slot="icon" name="person" size="lg"></vox-icon>
          Create the first user above.
        </vox-empty-state>`;
      return;
    }

    const fullName = (u) => [u.firstName, u.lastName].filter(Boolean).join(' ');

    const rows = users
      .map(
        (u) => `
      <tr>
        <td><vox-avatar data-avatar-id="${u.id}" size="sm"></vox-avatar></td>
        <td>${escapeHtml(u.username)}</td>
        <td>${escapeHtml(fullName(u))}</td>
        <td>${escapeHtml(u.email || '')}</td>
        <td><div class="vox-display-flex vox-flex-wrap vox-gap-sm">${roleCheckboxes(u)}</div></td>
        <td><vox-button data-delete-id="${u.id}" variant="danger" size="sm">${t('Delete')}</vox-button></td>
      </tr>`
      )
      .join('');

    results.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col"></th><th scope="col">${t('Username')}</th><th scope="col">${t('Name')}</th><th scope="col">${t('Email')}</th><th scope="col">${t('Roles')}</th><th scope="col"></th></tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;

    results.querySelectorAll('vox-avatar[data-avatar-id]').forEach((el) => {
      const u = users.find((user) => String(user.id) === el.dataset.avatarId);
      if (u) applyAvatar(el, { firstName: u.firstName, lastName: u.lastName, email: u.email, username: u.username });
    });

    results.querySelectorAll('vox-checkbox[data-role-id]').forEach((el) => {
      el.addEventListener('change', async () => {
        const userId = el.dataset.userId;
        const roleId = el.dataset.roleId;
        try {
          if (el.checked) {
            await sendJSON(`/api/v1/users/${userId}/roles/${roleId}`, 'POST');
          } else {
            await sendJSON(`/api/v1/users/${userId}/roles/${roleId}`, 'DELETE');
          }
        } catch (err) {
          el.checked = !el.checked;
          showCreateError(err.message);
        }
      });
    });

    results.querySelectorAll('vox-button[data-delete-id]').forEach((el) => {
      el.addEventListener('click', async () => {
        try {
          await sendJSON(`/api/v1/users/${el.dataset.deleteId}`, 'DELETE');
          await load();
        } catch (err) {
          showCreateError(err.message);
        }
      });
    });
  }

  async function load() {
    try {
      const [users, roles] = await withLoading(results, () =>
        Promise.all([fetchJSON('/api/v1/users'), fetchJSON('/api/v1/roles')]));
      allRoles = roles;
      const withRoles = await Promise.all(
        users.map(async (u) => {
          const assigned = await fetchJSON(`/api/v1/users/${u.id}/roles`);
          return { ...u, roleIds: new Set(assigned.map((r) => r.id)) };
        })
      );
      renderUsers(withRoles);
    } catch (err) {
      results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  document.getElementById('create-user').addEventListener('click', async () => {
    createError.innerHTML = '';
    const username = newUsername.value.trim();
    const password = newPassword.value;
    if (!username || !password) {
      showCreateError('Username and password are required.');
      return;
    }
    try {
      await sendJSON('/api/v1/users', 'POST', {
        username,
        password,
        firstName: newFirstName.value.trim(),
        lastName: newLastName.value.trim(),
        email: newEmail.value.trim(),
      });
      newUsername.value = '';
      newPassword.value = '';
      newFirstName.value = '';
      newLastName.value = '';
      newEmail.value = '';
      await load();
    } catch (err) {
      showCreateError(err.message);
    }
  });

  load();
}
