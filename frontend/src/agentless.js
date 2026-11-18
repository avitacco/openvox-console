// Agentless SSH targets (add-agentless-ssh-targets): hosts the console
// reaches over SSH instead of a node agent. Logins are write-only: this
// page can set or replace a key or password, never read one back.

import { fetchJSON, sendJSON, escapeHtml, hasPermission, confirmDialog, withLoading, t, formatDateTime } from './app.js';

const targetsEl = document.getElementById('targets');
const pageError = document.getElementById('page-error');
const checkResult = document.getElementById('check-result');
const addTarget = document.getElementById('add-target');
const credentialsSection = document.getElementById('credentials-section');
const credentialsEl = document.getElementById('credentials');
const addCredential = document.getElementById('add-credential');

const canManage = hasPermission('agentless:manage');
const canCheck = hasPermission('orchestrator:run');

let targets = [];
let credentials = [];
let credentialsReadable = false;

function showError(message) {
  pageError.innerHTML = `<vox-alert variant="danger" class="vox-m-bottom-md">${escapeHtml(message)}</vox-alert>`;
}

function fingerprint(fp) {
  return `<code class="vox-ts-sm">${escapeHtml(fp)}</code>`;
}

function loginCell(x) {
  const source = x.credentialName
    ? `<div class="vox-ts-sm vox-color-text-2">${escapeHtml(t('Shared credential: {name}', { name: x.credentialName }))}</div>`
    : '';
  const kind = x.hasKey ? t('key') : x.hasPassword ? t('password') : '';
  const runAs = x.runAs ? `<div class="vox-ts-sm vox-color-text-2">${escapeHtml(t('runs as {user}', { user: x.runAs }))}</div>` : '';
  return `${escapeHtml(x.username)}${kind ? ` <span class="vox-color-text-2">(${escapeHtml(kind)})</span>` : ''}${source}${runAs}`;
}

function hostKeyCell(x) {
  const k = x.hostKey;
  if (k.changed) {
    return `<vox-badge variant="danger">${escapeHtml(t('Host key changed'))}</vox-badge><div class="vox-ts-sm">${fingerprint(k.presented)}</div>`;
  }
  if (k.pinned) {
    return `<vox-badge variant="tip">${escapeHtml(t('Trusted'))}</vox-badge><div class="vox-ts-sm">${fingerprint(k.pinned)}</div>`;
  }
  if (k.proposed) {
    return `<vox-badge variant="warning">${escapeHtml(t('Waiting to be trusted'))}</vox-badge><div class="vox-ts-sm">${fingerprint(k.proposed)}</div>`;
  }
  return `<vox-badge variant="neutral">${escapeHtml(t('Not seen yet'))}</vox-badge>`;
}

function checkCell(x) {
  const c = x.check;
  if (!c.at) return `<vox-badge variant="neutral">${escapeHtml(t('Not checked'))}</vox-badge>`;
  const when = `<div class="vox-ts-sm vox-color-text-2">${escapeHtml(formatDateTime(c.at))}</div>`;
  if (c.ok) return `<vox-badge variant="tip">${escapeHtml(t('Reachable'))}</vox-badge>${when}`;
  return `<vox-badge variant="danger">${escapeHtml(t('Check failed'))}</vox-badge><div class="vox-ts-sm">${escapeHtml(c.error)}</div>${when}`;
}

function actionsCell(x) {
  const buttons = [];
  if (canCheck) buttons.push(`<vox-button size="sm" variant="alt" data-check="${x.id}">${escapeHtml(t('Check'))}</vox-button>`);
  if (canManage) {
    if (x.hostKey.changed) buttons.push(`<vox-button size="sm" variant="danger" data-key="${x.id}">${escapeHtml(t('Review host key'))}</vox-button>`);
    else if (x.hostKey.proposed && !x.hostKey.pinned) buttons.push(`<vox-button size="sm" variant="brand" data-key="${x.id}">${escapeHtml(t('Review host key'))}</vox-button>`);
    buttons.push(`<vox-button size="sm" variant="alt" data-edit="${x.id}">${escapeHtml(t('Edit'))}</vox-button>`);
    buttons.push(`<vox-button size="sm" variant="alt" data-delete="${x.id}">${escapeHtml(t('Delete'))}</vox-button>`);
  }
  return `<div class="vox-display-flex vox-gap-sm vox-flex-wrap">${buttons.join('')}</div>`;
}

function renderTargets() {
  if (targets.length === 0) {
    targetsEl.innerHTML = `
      <vox-empty-state heading="${escapeHtml(t('No agentless targets'))}">
        <vox-icon slot="icon" name="plug" size="lg"></vox-icon>
        ${escapeHtml(t('Add a host to run tasks on it over SSH.'))}
      </vox-empty-state>`;
    return;
  }
  const rows = targets.map((x) => `
    <tr>
      <th scope="row">${escapeHtml(x.name)}</th>
      <td>${escapeHtml(x.address)}:${x.port}</td>
      <td>${loginCell(x)}</td>
      <td>${hostKeyCell(x)}</td>
      <td>${checkCell(x)}</td>
      <td>${actionsCell(x)}</td>
    </tr>`).join('');
  targetsEl.innerHTML = `
    <div class="vox-table-wrap">
      <table class="vox-table vox-table--striped">
        <thead><tr>
          <th scope="col">${t('Name')}</th><th scope="col">${t('Address')}</th><th scope="col">${t('Login')}</th>
          <th scope="col">${t('Host key')}</th><th scope="col">${t('Last check')}</th><th scope="col"></th>
        </tr></thead>
        <tbody>${rows}</tbody>
      </table>
    </div>`;
  const byId = (id) => targets.find((x) => String(x.id) === String(id));
  targetsEl.querySelectorAll('[data-check]').forEach((b) => b.addEventListener('click', () => runCheck(byId(b.dataset.check), b)));
  targetsEl.querySelectorAll('[data-key]').forEach((b) => b.addEventListener('click', () => reviewKey(byId(b.dataset.key))));
  targetsEl.querySelectorAll('[data-edit]').forEach((b) => b.addEventListener('click', () => openTargetDialog(byId(b.dataset.edit))));
  targetsEl.querySelectorAll('[data-delete]').forEach((b) => b.addEventListener('click', () => removeTarget(byId(b.dataset.delete))));
}

function renderCredentials() {
  if (!credentialsReadable) {
    credentialsSection.style.display = 'none';
    return;
  }
  credentialsSection.style.display = '';
  if (credentials.length === 0) {
    credentialsEl.innerHTML = `<p class="vox-ts-sm vox-color-text-2">${escapeHtml(t('No shared credentials.'))}</p>`;
    return;
  }
  const rows = credentials.map((c) => `
    <tr>
      <th scope="row">${escapeHtml(c.name)}</th>
      <td>${escapeHtml(c.username)}${c.runAs ? ` <span class="vox-color-text-2">${escapeHtml(t('runs as {user}', { user: c.runAs }))}</span>` : ''}</td>
      <td>${c.hasKey ? escapeHtml(t('key')) + (c.keyFingerprint ? `<div class="vox-ts-sm">${fingerprint(c.keyFingerprint)}</div>` : '') : escapeHtml(t('password'))}</td>
      <td><div class="vox-display-flex vox-gap-sm">
        <vox-button size="sm" variant="alt" data-edit-credential="${c.id}">${escapeHtml(t('Edit'))}</vox-button>
        <vox-button size="sm" variant="alt" data-delete-credential="${c.id}">${escapeHtml(t('Delete'))}</vox-button>
      </div></td>
    </tr>`).join('');
  credentialsEl.innerHTML = `
    <div class="vox-table-wrap">
      <table class="vox-table vox-table--striped">
        <thead><tr><th scope="col">${t('Name')}</th><th scope="col">${t('Login')}</th><th scope="col">${t('Secret')}</th><th scope="col"></th></tr></thead>
        <tbody>${rows}</tbody>
      </table>
    </div>`;
  const byId = (id) => credentials.find((x) => String(x.id) === String(id));
  credentialsEl.querySelectorAll('[data-edit-credential]').forEach((b) => b.addEventListener('click', () => openCredentialDialog(byId(b.dataset.editCredential))));
  credentialsEl.querySelectorAll('[data-delete-credential]').forEach((b) => b.addEventListener('click', () => removeCredential(byId(b.dataset.deleteCredential))));
}

async function load() {
  pageError.innerHTML = '';
  try {
    targets = (await withLoading(targetsEl, () => fetchJSON('/api/v1/agentless/targets'))).items;
    renderTargets();
  } catch (err) {
    targetsEl.innerHTML = '';
    showError(err.message);
  }
  if (canManage) {
    try {
      credentials = (await fetchJSON('/api/v1/agentless/credentials')).items;
      credentialsReadable = true;
    } catch {
      // A caller limited to some node groups cannot manage shared credentials.
      credentialsReadable = false;
    }
    renderCredentials();
  }
}

// --- check, and the host key decision ---

async function runCheck(x, button) {
  checkResult.innerHTML = '';
  button.disabled = true;
  checkResult.innerHTML = `<vox-alert variant="info" class="vox-m-bottom-md">${escapeHtml(t('Checking {name}…', { name: x.name }))}</vox-alert>`;
  try {
    const r = await sendJSON(`/api/v1/agentless/targets/${x.id}/check`, 'POST');
    if (r.ok) {
      const as = r.runAs ? ` ${t('and as {user}', { user: r.runAs })}` : '';
      checkResult.innerHTML = `<vox-alert variant="success" class="vox-m-bottom-md">${escapeHtml(t('{name} is reachable. Logged in as {user}{as}.', { name: x.name, user: r.loggedInAs, as }))}</vox-alert>`;
    } else {
      checkResult.innerHTML = `<vox-alert variant="danger" class="vox-m-bottom-md">${escapeHtml(t('{name} failed at {stage}: {reason}', { name: x.name, stage: r.stage, reason: r.reason }))}</vox-alert>`;
    }
    await load();
    if (!r.ok && r.stage === 'host key' && canManage) reviewKey(targets.find((y) => y.id === x.id));
  } catch (err) {
    checkResult.innerHTML = `<vox-alert variant="danger" class="vox-m-bottom-md">${escapeHtml(err.message)}</vox-alert>`;
  } finally {
    button.disabled = false;
  }
}

async function reviewKey(x) {
  if (!x) return;
  const k = x.hostKey;
  const changed = k.changed;
  const shown = changed ? k.presented : k.proposed;
  if (!shown) return;
  const body = changed
    ? `<vox-alert variant="danger">${escapeHtml(t('{name} is presenting a different host key from the one pinned. Nothing was sent to it. Replace the pin only if you know the host was rebuilt or its key rotated.', { name: x.name }))}</vox-alert>
       <p>${escapeHtml(t('Pinned key'))}: ${fingerprint(k.pinned)}</p><p>${escapeHtml(t('Key it presents now'))}: ${fingerprint(shown)}</p>`
    : `<p>${escapeHtml(t('{name} presented this host key. Compare it with the host\'s own (ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub) before trusting it.', { name: x.name }))}</p>
       <p>${fingerprint(shown)}</p>`;
  const ok = await confirmDialog({
    heading: changed ? t('Replace the pinned host key?') : t('Trust this host key?'),
    body, confirmLabel: changed ? t('Replace pinned key') : t('Trust this key'), danger: changed,
  });
  if (!ok) return;
  try {
    await sendJSON(`/api/v1/agentless/targets/${x.id}/host-key/${changed ? 'replace' : 'accept'}`, 'POST', { fingerprint: shown });
    await load();
  } catch (err) {
    showError(err.message);
  }
}

async function removeTarget(x) {
  const ok = await confirmDialog({
    heading: t('Delete this target?'),
    body: `<p>${escapeHtml(t('{name} will no longer be reachable from the console. Its stored login is deleted.', { name: x.name }))}</p>`,
    confirmLabel: t('Delete'), danger: true,
  });
  if (!ok) return;
  try {
    await sendJSON(`/api/v1/agentless/targets/${x.id}`, 'DELETE');
    await load();
  } catch (err) {
    showError(err.message);
  }
}

async function removeCredential(c) {
  const ok = await confirmDialog({
    heading: t('Delete this shared credential?'),
    body: `<p>${escapeHtml(t('{name} will be deleted. A credential still used by a target cannot be deleted.', { name: c.name }))}</p>`,
    confirmLabel: t('Delete'), danger: true,
  });
  if (!ok) return;
  try {
    await sendJSON(`/api/v1/agentless/credentials/${c.id}`, 'DELETE');
    await load();
  } catch (err) {
    showError(err.message);
  }
}

// --- forms ---

// secretsHTML is the login's secret fields. On edit they are blank, and
// blank means unchanged: a stored secret is never shown.
function secretsHTML(editing) {
  const keep = editing ? ` ${t('Leave blank to keep the stored one.')}` : '';
  return `
    <vox-textarea data-field="privateKey" rows="5" label="${escapeHtml(t('Private key'))}" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
      note="${escapeHtml(t('Paste a PEM or OpenSSH private key.') + keep)}"></vox-textarea>
    <div data-error-for="privateKey"></div>
    <vox-input data-field="passphrase" type="password" autocomplete="new-password" label="${escapeHtml(t('Key passphrase (if it has one)'))}"></vox-input>
    <div data-error-for="passphrase"></div>
    <vox-input data-field="password" type="password" autocomplete="new-password" label="${escapeHtml(t('Password'))}"
      note="${escapeHtml(t('Used when there is no key, or as well.') + keep)}"></vox-input>
    <div data-error-for="password"></div>`;
}

function readSecrets(dialog) {
  const body = {};
  for (const name of ['privateKey', 'passphrase', 'password']) {
    const value = dialog.querySelector(`[data-field="${name}"]`).value;
    if (value) body[name] = value;
  }
  return body;
}

function placeError(dialog, err) {
  const place = (err.field && dialog.querySelector(`[data-error-for="${CSS.escape(err.field)}"]`)) || dialog.querySelector('[data-error]');
  const message = err.message.charAt(0).toUpperCase() + err.message.slice(1);
  place.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
}

function openTargetDialog(existing) {
  const editing = Boolean(existing);
  const shared = credentialsReadable && credentials.length > 0;
  const dialog = document.createElement('vox-dialog');
  dialog.heading = editing ? t('Edit {name}', { name: existing.name }) : t('Add an agentless target');
  dialog.setAttribute('light-dismiss', '');
  dialog.innerHTML = `
    <div class="run-task-scroll">
      <vox-input data-field="name" label="${escapeHtml(t('Name'))}" ${editing ? 'disabled' : ''} value="${escapeHtml(existing?.name || '')}"
        note="${escapeHtml(t('How the target is named in jobs and node groups. It cannot be the name of a node that exists.'))}"></vox-input>
      <div data-error-for="name"></div>
      <div class="vox-display-flex vox-gap-md vox-flex-wrap">
        <div><vox-input data-field="address" label="${escapeHtml(t('Address'))}" value="${escapeHtml(existing?.address || '')}"></vox-input><div data-error-for="address"></div></div>
        <div><vox-input data-field="port" class="field-narrow" type="number" min="1" max="65535" label="${escapeHtml(t('Port'))}" value="${existing?.port || 22}"></vox-input><div data-error-for="port"></div></div>
      </div>
      ${shared ? `
      <vox-select data-field="source" label="${escapeHtml(t('Login'))}">
        <option value="own">${escapeHtml(t('A login of its own'))}</option>
        <option value="shared">${escapeHtml(t('A shared credential'))}</option>
      </vox-select>
      <div data-pane="shared" style="display: none">
        <vox-select data-field="credentialId" label="${escapeHtml(t('Shared credential'))}">
          ${credentials.map((c) => `<option value="${c.id}">${escapeHtml(c.name)}</option>`).join('')}
        </vox-select>
        <div data-error-for="credentialId"></div>
      </div>` : ''}
      <div data-pane="own">
        <vox-input data-field="username" label="${escapeHtml(t('User'))}" value="${escapeHtml(existing && !existing.credentialName ? existing.username : '')}"></vox-input>
        <div data-error-for="username"></div>
        <vox-input data-field="runAs" label="${escapeHtml(t('Run tasks as (optional)'))}" value="${escapeHtml(existing && !existing.credentialName ? existing.runAs || '' : '')}"
          note="${escapeHtml(t('Tasks run through sudo as this user. sudo must not ask for a password.'))}"></vox-input>
        <div data-error-for="runAs"></div>
        ${secretsHTML(editing)}
      </div>
      <div data-error class="vox-m-top-md"></div>
    </div>
    <div slot="footer" class="dialog-footer-split">
      <vox-button variant="alt" data-action="cancel">${escapeHtml(t('Cancel'))}</vox-button>
      <vox-button variant="brand" data-action="save">${escapeHtml(editing ? t('Save') : t('Add target'))}</vox-button>
    </div>`;
  dialog.addEventListener('vox-close', () => dialog.remove());
  dialog.querySelector('[data-action="cancel"]').addEventListener('click', () => dialog.close());
  document.body.appendChild(dialog);
  dialog.show();

  const field = (n) => dialog.querySelector(`[data-field="${n}"]`);
  const sourceEl = field('source');
  const syncPane = () => {
    const useShared = sourceEl && sourceEl.value === 'shared';
    dialog.querySelector('[data-pane="own"]').style.display = useShared ? 'none' : '';
    const sharedPane = dialog.querySelector('[data-pane="shared"]');
    if (sharedPane) sharedPane.style.display = useShared ? '' : 'none';
  };
  if (sourceEl) {
    if (existing?.credentialId) {
      sourceEl.value = 'shared';
      field('credentialId').value = String(existing.credentialId);
    }
    sourceEl.addEventListener('change', syncPane);
    syncPane();
  }

  const saveButton = dialog.querySelector('[data-action="save"]');
  saveButton.addEventListener('click', async () => {
    dialog.querySelector('[data-error]').innerHTML = '';
    dialog.querySelectorAll('[data-error-for]').forEach((el) => { el.innerHTML = ''; });
    const body = { address: field('address').value.trim(), port: Number(field('port').value) || 22 };
    if (!editing) body.name = field('name').value.trim();
    if (sourceEl && sourceEl.value === 'shared') {
      body.credentialId = Number(field('credentialId').value);
    } else {
      body.username = field('username').value.trim();
      body.runAs = field('runAs').value.trim();
      Object.assign(body, readSecrets(dialog));
    }
    saveButton.disabled = true;
    try {
      const saved = editing
        ? await sendJSON(`/api/v1/agentless/targets/${existing.id}`, 'PUT', body)
        : await sendJSON('/api/v1/agentless/targets', 'POST', body);
      dialog.close();
      await load();
      checkResult.innerHTML = `<vox-alert variant="info" class="vox-m-bottom-md">${escapeHtml(t('Saved {name}. Check it to see its host key and confirm the login works.', { name: saved.name }))}</vox-alert>`;
    } catch (err) {
      placeError(dialog, err);
      saveButton.disabled = false;
    }
  });
}

function openCredentialDialog(existing) {
  const editing = Boolean(existing);
  const dialog = document.createElement('vox-dialog');
  dialog.heading = editing ? t('Edit {name}', { name: existing.name }) : t('Add a shared credential');
  dialog.setAttribute('light-dismiss', '');
  dialog.innerHTML = `
    <div class="run-task-scroll">
      <vox-input data-field="name" label="${escapeHtml(t('Name'))}" ${editing ? 'disabled' : ''} value="${escapeHtml(existing?.name || '')}"></vox-input>
      <div data-error-for="name"></div>
      <vox-input data-field="username" label="${escapeHtml(t('User'))}" value="${escapeHtml(existing?.username || '')}"></vox-input>
      <div data-error-for="username"></div>
      <vox-input data-field="runAs" label="${escapeHtml(t('Run tasks as (optional)'))}" value="${escapeHtml(existing?.runAs || '')}"></vox-input>
      <div data-error-for="runAs"></div>
      ${secretsHTML(editing)}
      <div data-error class="vox-m-top-md"></div>
    </div>
    <div slot="footer" class="dialog-footer-split">
      <vox-button variant="alt" data-action="cancel">${escapeHtml(t('Cancel'))}</vox-button>
      <vox-button variant="brand" data-action="save">${escapeHtml(editing ? t('Save') : t('Add shared credential'))}</vox-button>
    </div>`;
  dialog.addEventListener('vox-close', () => dialog.remove());
  dialog.querySelector('[data-action="cancel"]').addEventListener('click', () => dialog.close());
  document.body.appendChild(dialog);
  dialog.show();
  const saveButton = dialog.querySelector('[data-action="save"]');
  saveButton.addEventListener('click', async () => {
    dialog.querySelector('[data-error]').innerHTML = '';
    dialog.querySelectorAll('[data-error-for]').forEach((el) => { el.innerHTML = ''; });
    const get = (n) => dialog.querySelector(`[data-field="${n}"]`).value.trim();
    const body = { username: get('username'), runAs: get('runAs'), ...readSecrets(dialog) };
    if (!editing) body.name = get('name');
    saveButton.disabled = true;
    try {
      await (editing
        ? sendJSON(`/api/v1/agentless/credentials/${existing.id}`, 'PUT', body)
        : sendJSON('/api/v1/agentless/credentials', 'POST', body));
      dialog.close();
      await load();
    } catch (err) {
      placeError(dialog, err);
      saveButton.disabled = false;
    }
  });
}

if (canManage) {
  addTarget.style.display = '';
  addTarget.addEventListener('click', () => openTargetDialog(null));
  addCredential.addEventListener('click', () => openCredentialDialog(null));
}
load();
