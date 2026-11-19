import { fetchJSON, sendJSON, escapeHtml, requirePermission, withLoading, t, formatDateTime, treeOrder } from './app.js';
import { parsePatterns, patternBadges, showPatternError } from './task-patterns.js';

// What an agent token may carry: reading, and asking to run. Deciding a
// request is a person's permission, and is never offered here.
const AGENT_PERMISSIONS = ['nodes:read', 'classifier:read', 'orchestrator:read', 'orchestrator:run', 'vulnerabilities:read'];
const DEFAULT_DAYS = 30;

if (requirePermission('rbac:admin')) {
  const results = document.getElementById('results');
  const createError = document.getElementById('create-error');
  const newTokenPanel = document.getElementById('new-token-panel');
  const newTokenName = document.getElementById('new-token-name');
  const newTokenPermissions = document.getElementById('new-token-permissions');
  const newTokenGroups = document.getElementById('new-token-groups');
  const newTokenTasks = document.getElementById('new-token-tasks');
  const newTokenTasksError = document.getElementById('new-token-tasks-error');
  const newTokenDays = document.getElementById('new-token-days');

  // The node group tree, or null when it cannot be loaded: an administrator
  // of tokens does not necessarily hold classifier:read.
  let groupTree = null;
  const groupNames = new Map();

  newTokenDays.value = String(DEFAULT_DAYS);
  newTokenPermissions.innerHTML = AGENT_PERMISSIONS.map(
    (p) => `<vox-checkbox value="${p}"${p.endsWith(':read') && p.startsWith('nodes') ? ' checked' : ''}>${escapeHtml(p)}</vox-checkbox>`
  ).join('');

  function groupCheckboxes() {
    if (groupTree === null) {
      return `<vox-alert variant="info">${escapeHtml(t('Node groups could not be loaded. Viewing node groups needs the classifier:read permission.'))}</vox-alert>`;
    }
    return groupTree
      .map(({ group: g, depth }) => `<vox-checkbox data-group-id="${g.id}" style="padding-inline-start: ${depth * 1.5}rem">${escapeHtml(g.name)}</vox-checkbox>`)
      .join('');
  }

  function groupsText(ids) {
    if (!ids || ids.length === 0) {
      return `<vox-badge variant="warning">${escapeHtml(t('No nodes'))}</vox-badge>`;
    }
    return ids.map((id) => `<vox-badge variant="neutral">${escapeHtml(groupNames.get(id) || `#${id}`)}</vox-badge>`).join(' ');
  }

  function renderTokens(tokens) {
    if (tokens.length === 0) {
      results.innerHTML = `
        <vox-empty-state heading="${escapeHtml(t('No agent tokens yet'))}">
          <vox-icon slot="icon" name="key" size="lg"></vox-icon>
          ${escapeHtml(t('Create one above to let an AI agent use the console.'))}
        </vox-empty-state>`;
      return;
    }
    const now = Date.now();
    // Named `token`, not `t`: `t` is the translation function.
    const rows = tokens
      .map((token) => {
        const expired = new Date(token.expiresAt).getTime() <= now;
        const expiry = `${escapeHtml(formatDateTime(token.expiresAt))}${expired ? ` <vox-badge variant="danger">${escapeHtml(t('Expired'))}</vox-badge>` : ''}`;
        return `
      <tr>
        <td>${escapeHtml(token.name)}</td>
        <td>${token.permissions.map((p) => `<vox-badge variant="neutral">${escapeHtml(p)}</vox-badge>`).join(' ')}</td>
        <td>${groupsText(token.nodeGroups)}</td>
        <td>${patternBadges(token.taskPatterns)}</td>
        <td>${escapeHtml(token.owner)}</td>
        <td>${expiry}</td>
        <td><vox-button data-revoke-id="${token.id}" variant="danger" size="sm">${escapeHtml(t('Revoke'))}</vox-button></td>
      </tr>`;
      })
      .join('');
    results.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">${t('Name')}</th><th scope="col">${t('Permissions')}</th><th scope="col">${t('Node groups')}</th><th scope="col">${t('Tasks')}</th><th scope="col">${t('Issued by')}</th><th scope="col">${t('Expires')}</th><th scope="col"></th></tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
    results.querySelectorAll('vox-button[data-revoke-id]').forEach((el) => {
      el.addEventListener('click', async () => {
        try {
          await sendJSON(`/api/v1/agent-tokens/${el.dataset.revokeId}`, 'DELETE');
          await load();
        } catch (err) {
          results.insertAdjacentHTML('afterbegin', `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`);
        }
      });
    });
  }

  async function load() {
    try {
      const tokens = await withLoading(results, () => fetchJSON('/api/v1/agent-tokens'));
      renderTokens(tokens);
    } catch (err) {
      results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  async function loadGroups() {
    try {
      const page = await fetchJSON('/api/v1/groups?all=true');
      groupTree = treeOrder(page.items);
      for (const g of page.items) groupNames.set(g.id, g.name);
    } catch {
      groupTree = null;
    }
    newTokenGroups.innerHTML = groupCheckboxes();
  }

  document.getElementById('create-token').addEventListener('click', async () => {
    createError.innerHTML = '';
    newTokenTasksError.innerHTML = '';
    const name = newTokenName.value.trim();
    const permissions = Array.from(newTokenPermissions.querySelectorAll('vox-checkbox'))
      .filter((cb) => cb.checked)
      .map((cb) => cb.value);
    const nodeGroups = Array.from(newTokenGroups.querySelectorAll('vox-checkbox[data-group-id]'))
      .filter((cb) => cb.checked)
      .map((cb) => Number(cb.dataset.groupId));
    const days = Number(newTokenDays.value);
    const fail = (message) => {
      createError.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
    };
    if (!name) return fail(t('Name is required.'));
    if (permissions.length === 0) return fail(t('Select at least one permission.'));
    if (!Number.isInteger(days) || days < 1) return fail(t('Give the number of days the token lasts.'));
    if (nodeGroups.length === 0) {
      return fail(t('Choose at least one node group. A token with none can reach no nodes.'));
    }
    try {
      const created = await sendJSON('/api/v1/agent-tokens', 'POST', {
        name, permissions, nodeGroups, taskPatterns: parsePatterns(newTokenTasks.value), expiresInDays: days,
      });
      newTokenPanel.innerHTML = `
        <vox-alert variant="success" heading="${escapeHtml(t('Agent token created - copy it now, it won\'t be shown again'))}" dismissible>
          <pre class="value">${escapeHtml(created.token)}</pre>
          <p class="vox-ts-sm vox-m-bottom-none">${escapeHtml(t('Give it to the agent as a bearer token for the MCP endpoint. It expires on {when}.', { when: formatDateTime(created.expiresAt) }))}</p>
        </vox-alert>`;
      newTokenPanel.querySelector('vox-alert').addEventListener('vox-dismiss', () => {
        newTokenPanel.innerHTML = '';
      });
      newTokenName.value = '';
      newTokenTasks.value = '';
      newTokenDays.value = String(DEFAULT_DAYS);
      newTokenGroups.querySelectorAll('vox-checkbox').forEach((cb) => (cb.checked = false));
      await load();
    } catch (err) {
      if (!showPatternError(newTokenTasksError, err)) fail(err.message);
    }
  });

  // The groups first, so the list can name them.
  loadGroups().then(load);
}
