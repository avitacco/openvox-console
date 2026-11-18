// The fleet Patching page: which nodes are missing updates, a search by
// pending package, a scan of a whole group, and (for nodes:manage) the
// automatic scan interval. Read-only with respect to the nodes: a scan
// asks the package manager what is pending and never changes anything.

import { fetchJSON, sendJSON, escapeHtml, hasPermission, paginationHTML, bindPagination, withLoading, t, tn, formatDateTime, formatNumber, statusVariant, statusLabel } from './app.js';
import { openPatchDialog, patchOptionBadges } from './run-patch.js';

const packageFilter = document.getElementById('package-filter');
const osFilter = document.getElementById('os-filter');
const groupFilter = document.getElementById('group-filter');
const scanGroupButton = document.getElementById('scan-group');
const patchGroupButton = document.getElementById('patch-group');
const scanHint = document.getElementById('scan-hint');
const scanResult = document.getElementById('scan-result');
const patchError = document.getElementById('patch-error');
const totalsEl = document.getElementById('patch-totals');
const results = document.getElementById('results');

const PAGE_SIZE = 50;
let currentPage = 1;
let osLoaded = false;

function nodeLink(certname) {
  return `<a href="/node.html?name=${encodeURIComponent(certname)}">${escapeHtml(certname)}</a>`;
}

function renderTotals(totals) {
  const stat = (value, label) => `<vox-stat value="${formatNumber(value)}" label="${escapeHtml(label)}"></vox-stat>`;
  totalsEl.innerHTML = [
    stat(totals.nodes, t('Nodes')),
    stat(totals.withUpdates, t('With pending updates')),
    stat(totals.withSecurity, t('With security updates')),
    stat(totals.rebootRequired, t('Need a reboot')),
    stat(totals.neverScanned + totals.unsupported, t('Not scanned')),
    stat(totals.stale, t('Out of date scans')),
  ].join('');
}

// The OS choices come from the response, but the select is only filled
// once: rebuilding it on every reload would reset the user's choice.
function fillOS(names) {
  if (osLoaded || names.length === 0) return;
  osLoaded = true;
  osFilter.innerHTML = `<option value="">${t('All')}</option>${names
    .map((n) => `<option value="${escapeHtml(n)}">${escapeHtml(n)}</option>`)
    .join('')}`;
}

// What to say in the "last scanned" column. A node never scanned is not
// the same as a node with nothing pending, and says so.
function scannedCell(n) {
  if (n.unsupported) return `<vox-badge variant="neutral">${t('Cannot report')}</vox-badge>`;
  if (!n.scanned) {
    return n.error
      ? `<vox-badge variant="danger" title="${escapeHtml(n.error)}">${t('Scan failed')}</vox-badge>`
      : `<vox-badge variant="neutral">${t('Not scanned')}</vox-badge>`;
  }
  const when = escapeHtml(formatDateTime(n.scannedAt));
  const flags = [
    n.stale ? `<vox-badge variant="warning">${t('Out of date')}</vox-badge>` : '',
    n.error ? `<vox-badge variant="danger" title="${escapeHtml(n.error)}">${t('Last scan failed')}</vox-badge>` : '',
  ].join(' ');
  return `${when} ${flags}`;
}

function renderFleet(data) {
  renderTotals(data.totals);
  fillOS(data.osNames || []);
  const page = data.nodes;
  if (page.items.length === 0) {
    results.innerHTML = `
      <vox-empty-state heading="${t('No nodes match')}">
        <vox-icon slot="icon" name="search" size="lg"></vox-icon>
        ${t('Try a different package, operating system or group.')}
      </vox-empty-state>`;
    return;
  }
  const rows = page.items.map((n) => `
    <tr>
      <th scope="row">${nodeLink(n.certname)}</th>
      <td>${escapeHtml(n.os) || '—'}</td>
      <td>${n.scanned ? n.updates : '—'}</td>
      <td>${n.scanned ? (n.security > 0 ? `<vox-badge variant="danger">${n.security}</vox-badge>` : '0') : '—'}</td>
      <td>${n.scanned && n.rebootRequired ? t('Yes') : n.scanned ? t('No') : '—'}</td>
      <td>${scannedCell(n)}</td>
    </tr>`).join('');
  results.innerHTML = `
    <div class="vox-table-wrap">
      <table class="vox-table vox-table--striped">
        <thead><tr>
          <th scope="col">${t('Node')}</th><th scope="col">${t('Operating system')}</th>
          <th scope="col">${t('Updates')}</th><th scope="col">${t('Security')}</th>
          <th scope="col">${t('Reboot needed')}</th><th scope="col">${t('Last scanned')}</th>
        </tr></thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
    <p class="vox-ts-sm vox-color-text-2">${t('Security is what the node\'s package manager marks as a security update. A system without that information shows none.')}</p>
    ${paginationHTML(page.page, page.pageSize, page.total)}`;
  bindPagination(results, (target) => {
    currentPage = target;
    load();
  });
}

function renderPackageMatches(page, name) {
  totalsEl.innerHTML = '';
  if (page.items.length === 0) {
    results.innerHTML = `
      <vox-empty-state heading="${t('No node has that update pending')}">
        <vox-icon slot="icon" name="search" size="lg"></vox-icon>
        ${escapeHtml(t('No scanned node has an update pending for a package matching "{name}".', { name }))}
      </vox-empty-state>`;
    return;
  }
  const rows = page.items.map((m) => `
    <tr>
      <th scope="row">${escapeHtml(m.name)}</th>
      <td>${nodeLink(m.certname)}</td>
      <td>${escapeHtml(m.installed) || '—'}</td>
      <td>${escapeHtml(m.available)}</td>
      <td>${m.security ? `<vox-badge variant="danger">${t('Security')}</vox-badge>` : '—'}</td>
    </tr>`).join('');
  results.innerHTML = `
    <div class="vox-table-wrap">
      <table class="vox-table vox-table--striped">
        <thead><tr>
          <th scope="col">${t('Package')}</th><th scope="col">${t('Node')}</th>
          <th scope="col">${t('Installed')}</th><th scope="col">${t('Available')}</th><th scope="col">${t('Security')}</th>
        </tr></thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
    ${paginationHTML(page.page, page.pageSize, page.total)}`;
  bindPagination(results, (target) => {
    currentPage = target;
    load();
  });
}

async function load() {
  patchError.innerHTML = '';
  const params = new URLSearchParams({ page: currentPage, pageSize: PAGE_SIZE });
  if (groupFilter.value) params.set('group', groupFilter.value);
  const name = packageFilter.value.trim();
  try {
    if (name) {
      params.set('name', name);
      const page = await withLoading(results, () => fetchJSON(`/api/v1/patches/packages?${params}`));
      renderPackageMatches(page, name);
    } else {
      if (osFilter.value) params.set('os', osFilter.value);
      const data = await withLoading(results, () => fetchJSON(`/api/v1/patches?${params}`));
      if (data.nodes.items.length === 0 && data.nodes.page > 1 && data.nodes.total > 0) {
        currentPage = 1;
        return load();
      }
      renderFleet(data);
      showSettings(data.settings);
    }
  } catch (err) {
    totalsEl.innerHTML = '';
    results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

// Scanning a group is an action, so it needs orchestrator:run, and the
// group list needs classifier:read; without both the control is not offered.
function updateScanControl() {
  const canScan = hasPermission('orchestrator:run') && hasPermission('classifier:read');
  scanGroupButton.style.display = canScan ? '' : 'none';
  scanHint.style.display = canScan && !groupFilter.value ? '' : 'none';
  scanGroupButton.disabled = !groupFilter.value;
  patchGroupButton.style.display = canScan ? '' : 'none';
  patchGroupButton.disabled = !groupFilter.value;
}

function scanResultHTML(r) {
  const parts = [
    tn('{count} node scanned', '{count} nodes scanned', r.scanned, { count: r.scanned }),
    r.offline ? tn('{count} offline', '{count} offline', r.offline, { count: r.offline }) : '',
    r.failed ? tn('{count} failed', '{count} failed', r.failed, { count: r.failed }) : '',
    r.skipped ? tn('{count} left out: not in your node groups', '{count} left out: not in your node groups', r.skipped, { count: r.skipped }) : '',
  ].filter(Boolean);
  const problems = (r.results || [])
    .filter((n) => n.outcome === 'failed' || n.outcome === 'unsupported')
    .map((n) => `<li>${escapeHtml(n.certname)}: ${escapeHtml(n.error)}</li>`)
    .join('');
  const variant = r.failed > 0 ? 'warning' : 'success';
  return `<vox-alert variant="${variant}">${escapeHtml(parts.join(', '))}.${problems ? `<ul>${problems}</ul>` : ''}</vox-alert>`;
}

scanGroupButton.addEventListener('click', async () => {
  scanResult.innerHTML = '';
  scanGroupButton.disabled = true;
  scanResult.innerHTML = `<vox-alert variant="info">${t('Scanning. Refreshing package lists can take a while on a slow mirror.')}</vox-alert>`;
  try {
    const r = await sendJSON('/api/v1/patches/scan', 'POST', { group: Number(groupFilter.value) });
    scanResult.innerHTML = scanResultHTML(r);
    await load();
  } catch (err) {
    scanResult.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  } finally {
    updateScanControl();
  }
});

patchGroupButton.addEventListener('click', () => {
  const option = groupFilter.querySelector(`option[value="${CSS.escape(groupFilter.value)}"]`);
  openPatchDialog({ group: Number(groupFilter.value), groupName: option ? option.textContent : '' });
});

// The last few patch runs, for whoever may read jobs. A run is read on its
// job page; this is only the way in.
const patchRunsSection = document.getElementById('patch-runs');
const patchRunsList = document.getElementById('patch-runs-list');

async function loadPatchRuns() {
  if (!hasPermission('orchestrator:read')) return;
  try {
    const page = await fetchJSON('/api/v1/orchestrator/jobs?kind=patch&pageSize=5');
    if (page.items.length === 0) return;
    patchRunsSection.style.display = '';
    const rows = page.items.map((j) => `
      <tr>
        <th scope="row"><a href="/job.html?id=${j.id}">${escapeHtml(t('Job #{id}', { id: j.id }))}</a></th>
        <td><vox-badge variant="${statusVariant(j.status)}">${escapeHtml(statusLabel(j.status))}</vox-badge> ${patchOptionBadges(j.patchOptions)}</td>
        <td>${tn('{count} node', '{count} nodes', j.targetCount || 0)}</td>
        <td>${escapeHtml(j.triggeredBy)}</td>
        <td>${escapeHtml(formatDateTime(j.startedAt))}</td>
      </tr>`).join('');
    patchRunsList.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead><tr><th scope="col">${t('Job')}</th><th scope="col">${t('Status')}</th><th scope="col">${t('Nodes')}</th><th scope="col">${t('Triggered by')}</th><th scope="col">${t('Started')}</th></tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
  } catch {
    // The section simply stays hidden; the fleet view is unaffected.
  }
}

// The interval setting is for nodes:manage; everyone else reads the
// fleet and never sees the control.
const settingsSection = document.getElementById('scan-settings');
const settingsEnabled = document.getElementById('settings-enabled');
const settingsInterval = document.getElementById('settings-interval');
const settingsSave = document.getElementById('settings-save');
const settingsError = document.getElementById('settings-error');
const settingsSaved = document.getElementById('settings-saved');
let settingsShown = false;

function showSettings(settings) {
  if (!hasPermission('nodes:manage') || settingsShown) return;
  settingsShown = true;
  settingsSection.style.display = '';
  settingsEnabled.checked = settings.enabled;
  settingsInterval.value = String(settings.intervalMinutes);
}

settingsSave.addEventListener('click', async () => {
  settingsError.innerHTML = '';
  settingsSaved.innerHTML = '';
  document.querySelectorAll('[data-error-for="intervalMinutes"]').forEach((el) => { el.innerHTML = ''; });
  settingsSave.disabled = true;
  try {
    const s = await sendJSON('/api/v1/patches/settings', 'PUT', {
      enabled: settingsEnabled.checked,
      intervalMinutes: Number(settingsInterval.value),
    });
    settingsInterval.value = String(s.intervalMinutes);
    settingsSaved.innerHTML = `<vox-alert variant="success">${t('Saved.')}</vox-alert>`;
    await load();
  } catch (err) {
    const place = (err.field && document.querySelector(`[data-error-for="${CSS.escape(err.field)}"]`)) || settingsError;
    const message = err.message.charAt(0).toUpperCase() + err.message.slice(1);
    place.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
  } finally {
    settingsSave.disabled = false;
  }
});

async function loadGroups() {
  if (!hasPermission('classifier:read')) return;
  try {
    const page = await fetchJSON('/api/v1/groups?pageSize=100');
    if (page.items.length === 0) return;
    groupFilter.innerHTML = `<option value="">${t('All')}</option>${page.items
      .map((g) => `<option value="${g.id}">${escapeHtml(g.name)}</option>`)
      .join('')}`;
    groupFilter.style.display = '';
  } catch {
    // The filter simply stays hidden; the fleet view is unaffected.
  }
}

let debounce;
packageFilter.addEventListener('input', () => {
  clearTimeout(debounce);
  debounce = setTimeout(() => {
    currentPage = 1;
    load();
  }, 300);
});
for (const el of [osFilter, groupFilter]) {
  el.addEventListener('change', () => {
    currentPage = 1;
    updateScanControl();
    load();
  });
}

updateScanControl();
loadGroups();
load();
loadPatchRuns();
