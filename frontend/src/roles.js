import { fetchJSON, sendJSON, escapeHtml, requirePermission, ALL_PERMISSIONS, withLoading } from './app.js';

if (requirePermission('rbac:admin')) {
  const results = document.getElementById('results');
  const createError = document.getElementById('create-error');
  const newRoleName = document.getElementById('new-role-name');
  const newRolePermissions = document.getElementById('new-role-permissions');

  newRolePermissions.innerHTML = ALL_PERMISSIONS.map(
    (p) => `<vox-checkbox value="${p}">${escapeHtml(p)}</vox-checkbox>`
  ).join('');

  function permissionCheckboxes(role) {
    return ALL_PERMISSIONS.map((p) => {
      const checked = role.permissions.includes(p) ? 'checked' : '';
      return `<vox-checkbox data-role-id="${role.id}" data-permission="${p}" ${checked}>${escapeHtml(p)}</vox-checkbox>`;
    }).join('');
  }

  function renderRoles(roles) {
    if (roles.length === 0) {
      results.innerHTML = `
        <vox-empty-state heading="No roles yet">
          <vox-icon slot="icon" name="shield" size="lg"></vox-icon>
          Create a role above to start assigning permissions.
        </vox-empty-state>`;
      return;
    }

    const rows = roles
      .map(
        (r) => `
      <tr>
        <td>${escapeHtml(r.name)}</td>
        <td><div class="vox-display-flex vox-flex-wrap vox-gap-sm">${permissionCheckboxes(r)}</div></td>
        <td><vox-button data-delete-id="${r.id}" variant="danger" size="sm">Delete</vox-button></td>
      </tr>`
      )
      .join('');

    results.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">Name</th><th scope="col">Permissions</th><th scope="col"></th></tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;

    results.querySelectorAll('vox-checkbox[data-role-id]').forEach((el) => {
      el.addEventListener('change', async () => {
        const roleId = el.dataset.roleId;
        const row = el.closest('tr');
        const permissions = Array.from(row.querySelectorAll('vox-checkbox[data-permission]'))
          .filter((cb) => cb.checked)
          .map((cb) => cb.dataset.permission);
        const roleName = row.querySelector('td').textContent.trim();
        try {
          await sendJSON(`/api/v1/roles/${roleId}`, 'PUT', { name: roleName, permissions });
        } catch (err) {
          el.checked = !el.checked;
          results.insertAdjacentHTML(
            'afterbegin',
            `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`
          );
        }
      });
    });

    results.querySelectorAll('vox-button[data-delete-id]').forEach((el) => {
      el.addEventListener('click', async () => {
        try {
          await sendJSON(`/api/v1/roles/${el.dataset.deleteId}`, 'DELETE');
          await load();
        } catch (err) {
          results.insertAdjacentHTML(
            'afterbegin',
            `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`
          );
        }
      });
    });
  }

  async function load() {
    try {
      const roles = await withLoading(results, () => fetchJSON('/api/v1/roles'));
      renderRoles(roles);
    } catch (err) {
      results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
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
    try {
      await sendJSON('/api/v1/roles', 'POST', { name, permissions });
      newRoleName.value = '';
      newRolePermissions.querySelectorAll('vox-checkbox').forEach((cb) => (cb.checked = false));
      await load();
    } catch (err) {
      createError.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  });

  load();
}
