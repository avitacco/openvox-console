import { fetchJSON, sendJSON, escapeHtml, requirePermission, ALL_PERMISSIONS, withLoading, t, formatDateTime } from './app.js';

if (requirePermission('rbac:admin')) {
  const results = document.getElementById('results');
  const createError = document.getElementById('create-error');
  const newTokenPanel = document.getElementById('new-token-panel');
  const newTokenName = document.getElementById('new-token-name');
  const newTokenPermissions = document.getElementById('new-token-permissions');

  newTokenPermissions.innerHTML = ALL_PERMISSIONS.map(
    (p) => `<vox-checkbox value="${p}">${escapeHtml(p)}</vox-checkbox>`
  ).join('');

  function renderTokens(tokens) {
    if (tokens.length === 0) {
      results.innerHTML = `
        <vox-empty-state heading="${t('No service tokens yet')}">
          <vox-icon slot="icon" name="key" size="lg"></vox-icon>
          Create one above for a machine client like enc-bridge.
        </vox-empty-state>`;
      return;
    }

    const rows = tokens
      .map(
        (t) => `
      <tr>
        <td>${escapeHtml(t.name)}</td>
        <td>${t.permissions.map((p) => `<vox-badge variant="neutral">${escapeHtml(p)}</vox-badge>`).join(' ')}</td>
        <td>${escapeHtml(t.createdAt ? formatDateTime(t.createdAt) : '')}</td>
        <td><vox-button data-delete-id="${t.id}" variant="danger" size="sm">${t('Delete')}</vox-button></td>
      </tr>`
      )
      .join('');

    results.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">${t('Name')}</th><th scope="col">${t('Permissions')}</th><th scope="col">${t('Created')}</th><th scope="col"></th></tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;

    results.querySelectorAll('vox-button[data-delete-id]').forEach((el) => {
      el.addEventListener('click', async () => {
        try {
          await sendJSON(`/api/v1/service-tokens/${el.dataset.deleteId}`, 'DELETE');
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
      const tokens = await withLoading(results, () => fetchJSON('/api/v1/service-tokens'));
      renderTokens(tokens);
    } catch (err) {
      results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  document.getElementById('create-token').addEventListener('click', async () => {
    createError.innerHTML = '';
    const name = newTokenName.value.trim();
    const permissions = Array.from(newTokenPermissions.querySelectorAll('vox-checkbox'))
      .filter((cb) => cb.checked)
      .map((cb) => cb.value);
    if (!name) {
      createError.innerHTML = `<vox-alert variant="danger">Name is required.</vox-alert>`;
      return;
    }
    if (permissions.length === 0) {
      createError.innerHTML = `<vox-alert variant="danger">Select at least one permission.</vox-alert>`;
      return;
    }
    try {
      const created = await sendJSON('/api/v1/service-tokens', 'POST', { name, permissions });
      newTokenPanel.innerHTML = `
        <vox-alert variant="success" heading="${t('Service token created - copy it now, it won\'t be shown again')}" dismissible>
          <pre class="value">${escapeHtml(created.token)}</pre>
        </vox-alert>`;
      newTokenPanel.querySelector('vox-alert').addEventListener('vox-dismiss', () => {
        newTokenPanel.innerHTML = '';
      });
      newTokenName.value = '';
      newTokenPermissions.querySelectorAll('vox-checkbox').forEach((cb) => (cb.checked = false));
      await load();
    } catch (err) {
      createError.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  });

  load();
}
