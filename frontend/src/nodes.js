import { fetchJSON, sendJSON, escapeHtml, hasPermission, confirmDialog, withLoading, tn, t, statusLabel, formatDateTime } from './app.js';
import { openRunTaskDialog } from './run-task.js';
import { openRunPuppetDialog } from './run-puppet.js';

const results = document.getElementById('results');
const actionError = document.getElementById('action-error');
const nameFilterEl = document.getElementById('name-filter');
const connectionFilterEl = document.getElementById('connection-filter');
const certStatusFilterEl = document.getElementById('cert-status-filter');
const showInfrastructureEl = document.getElementById('show-infrastructure');
const addNodeButton = document.getElementById('add-node');
const selectionBar = document.getElementById('selection-bar');
const selectionCount = document.getElementById('selection-count');
const runSelectedButton = document.getElementById('run-selected');
const runTaskSelectedButton = document.getElementById('run-task-selected');
const clearSelectionButton = document.getElementById('clear-selection');

// inventoryCertnames: null until /api/v1/nodes (fetched in full - see
// fetchAllNodes) resolves; certname -> true for nodes openvoxdb knows about.
let inventoryCertnames = null;
// connectivityByCertname: null until /api/v1/node-connectivity resolves;
// certname -> {connected, lastConnected, lastDisconnected, certStatus}.
let connectivityByCertname = null;

// agentlessByName: certname -> target, for hosts reached over SSH rather
// than through a node agent (add-agentless-ssh-targets). They are not in
// openvoxdb or the CA registry, so they are added to the list here.
let agentlessByName = new Map();

let sortColumn = 'name';
let sortDirection = 'asc';

const CERT_STATUS_VARIANTS = {
  signed: 'tip',
  requested: 'warning',
  revoked: 'danger',
  unknown: 'neutral',
};

// CERT_STATUS_RANK orders certificate status sort by triage priority
// (healthy first, unknown last) rather than alphabetically.
const CERT_STATUS_RANK = { signed: 0, requested: 1, revoked: 2, unknown: 3 };

function connectivityBadge(certname) {
  if (connectivityByCertname === null) {
    return `<vox-badge variant="neutral">${t('checking…')}</vox-badge>`;
  }
  const entry = connectivityByCertname.get(certname);
  return entry?.connected
    ? `<vox-badge variant="tip">${t('Connected')}</vox-badge>`
    : `<vox-badge variant="neutral">${t('Not connected')}</vox-badge>`;
}

function lastConnectedCell(certname) {
  if (connectivityByCertname === null) return '…';
  const entry = connectivityByCertname.get(certname);
  return entry?.lastConnected ? escapeHtml(formatDateTime(entry.lastConnected)) : t('never');
}

function certStatusBadge(certname) {
  if (connectivityByCertname === null) {
    return `<vox-badge variant="neutral">${t('checking…')}</vox-badge>`;
  }
  const status = connectivityByCertname.get(certname)?.certStatus ?? 'unknown';
  const variant = CERT_STATUS_VARIANTS[status] ?? 'neutral';
  return `<vox-badge variant="${variant}">${escapeHtml(statusLabel(status))}</vox-badge>`;
}

// certActionsCell renders sign/revoke/clean/delete buttons for
// certname. Sign/revoke/clean are omitted entirely (not merely
// disabled) unless the viewer holds nodes:certs:manage - see design.md
// in add-node-certificate-management - and further narrowed by the
// node's current certificate status so the common case only offers the
// action that actually applies (signing a "requested" cert, revoking a
// "signed" one); the backend is the authoritative check either way, so
// a stale/racy status here just means a rejected action with a clear
// error, not an inconsistent state. Delete is gated on the separate
// nodes:manage permission instead, and shown regardless of cert status
// - deleting a node's openvoxdb inventory record is independent of its
// certificate lifecycle (see design.md in add-node-deletion).
function certActionsCell(certname) {
  const buttons = [];
  if (connectivityByCertname !== null && hasPermission('nodes:certs:manage')) {
    const status = connectivityByCertname.get(certname)?.certStatus ?? 'unknown';
    if (status === 'requested') {
      buttons.push(`<vox-button data-sign-certname="${escapeHtml(certname)}" variant="brand" size="sm">${t('Sign')}</vox-button>`);
    }
    if (status === 'signed') {
      buttons.push(`<vox-button data-revoke-certname="${escapeHtml(certname)}" variant="danger" size="sm">${t('Revoke')}</vox-button>`);
    }
    if (status === 'signed' || status === 'requested' || status === 'revoked') {
      buttons.push(`<vox-button data-clean-certname="${escapeHtml(certname)}" variant="danger" size="sm">${t('Clean')}</vox-button>`);
    }
  }
  if (hasPermission('nodes:manage')) {
    buttons.push(`<vox-button data-delete-certname="${escapeHtml(certname)}" variant="danger" size="sm">${t('Delete')}</vox-button>`);
  }
  if (buttons.length === 0) return '';
  return `<div class="vox-display-flex vox-gap-sm">${buttons.join('')}</div>`;
}

// infrastructureBadge marks a row as one of this console's own
// infrastructure identities rather than a managed node - see design.md
// in add-infrastructure-cert-detection for why this is shown at all
// once revealed (not just hidden by default): a toggle with no visual
// distinction once revealed would just recreate the original confusion.
function infrastructureBadge(certname) {
  if (!connectivityByCertname?.get(certname)?.isInfrastructure) return '';
  return ` <vox-badge variant="neutral">${t('Infrastructure')}</vox-badge>`;
}

// Selecting nodes to run against. Gated on orchestrator:run, the same
// permission the node detail page's own Run now button uses.
const canRun = hasPermission('orchestrator:run');
const selected = new Set();

function selectCell(certname) {
  if (!canRun) return '';
  const checked = selected.has(certname) ? ' checked' : '';
  return `<td><vox-checkbox data-select-certname="${escapeHtml(certname)}"${checked}></vox-checkbox></td>`;
}

// agentBadge marks a node whose node-agent-client is older than the
// build this console serves (or too old to say). A node that has simply
// not reported yet gets nothing: that is not the same as out of date.
// The node's own page says why, and offers the upgrade.
function agentBadge(certname) {
  const entry = connectivityByCertname?.get(certname);
  if (!entry?.agent?.outOfDate || entry.isInfrastructure) return '';
  return ` <vox-badge variant="warning">${t('Client out of date')}</vox-badge>`;
}

// An agentless target has no connection, certificate or last connected
// time; what the console knows is whether its last check worked.
function agentlessRow(x) {
  const check = x.check;
  const state = !check.at
    ? `<vox-badge variant="neutral">${t('Not checked')}</vox-badge>`
    : check.ok
      ? `<vox-badge variant="tip">${t('Reachable')}</vox-badge>`
      : `<vox-badge variant="danger">${t('Check failed')}</vox-badge>`;
  return `
        <tr>
          ${selectCell(x.name)}
          <td><a href="/agentless.html">${escapeHtml(x.name)}</a> <vox-badge variant="neutral">${t('Agentless')}</vox-badge></td>
          <td>${state}</td>
          <td>${check.at ? escapeHtml(formatDateTime(check.at)) : t('never')}</td>
          <td>—</td>
          <td></td>
        </tr>`;
}

function nodeRow(certname, nodeLink) {
  if (agentlessByName.has(certname)) return agentlessRow(agentlessByName.get(certname));
  return `
        <tr>
          ${selectCell(certname)}
          <td>${nodeLink}${infrastructureBadge(certname)}${agentBadge(certname)}</td>
          <td>${connectivityBadge(certname)}</td>
          <td>${lastConnectedCell(certname)}</td>
          <td>${certStatusBadge(certname)}</td>
          <td>${certActionsCell(certname)}</td>
        </tr>`;
}

// allCertnames returns the union of every certname openvoxdb's inventory
// knows about and every certname the CA/connectivity registry knows
// about, so a pending certificate request that's never had a real
// Puppet run (and so isn't in openvoxdb yet) still appears - see
// design.md in add-node-certificate-management for why this union
// matters. No node.html detail page exists for an inventory-less
// certname, so its name isn't rendered as a link.
function allCertnames() {
  const seen = new Set(inventoryCertnames ?? []);
  for (const name of agentlessByName.keys()) seen.add(name);
  if (connectivityByCertname) {
    for (const c of connectivityByCertname.keys()) seen.add(c);
  }
  return [...seen];
}

function connectionSortValue(certname) {
  const target = agentlessByName.get(certname);
  if (target) return target.check.ok ? 1 : 0;
  return connectivityByCertname?.get(certname)?.connected ? 1 : 0;
}

function lastConnectedSortValue(certname) {
  const raw = agentlessByName.get(certname)?.check.at ?? connectivityByCertname?.get(certname)?.lastConnected;
  return raw ? new Date(raw).getTime() : null;
}

function certStatusSortValue(certname) {
  const status = connectivityByCertname?.get(certname)?.certStatus ?? 'unknown';
  return CERT_STATUS_RANK[status] ?? CERT_STATUS_RANK.unknown;
}

const SORT_COMPARATORS = {
  name: (a, b) => a.localeCompare(b),
  connection: (a, b) => connectionSortValue(a) - connectionSortValue(b),
  lastConnected: (a, b) => {
    const va = lastConnectedSortValue(a);
    const vb = lastConnectedSortValue(b);
    // Nodes with no timestamp always sort last, regardless of direction -
    // handled here (before the direction flip applied by sortedCertnames)
    // by returning a magnitude that direction-flipping won't reorder.
    if (va === null && vb === null) return 0;
    if (va === null) return sortDirection === 'asc' ? 1 : -1;
    if (vb === null) return sortDirection === 'asc' ? -1 : 1;
    return va - vb;
  },
  certStatus: (a, b) => certStatusSortValue(a) - certStatusSortValue(b),
};

function filteredCertnames() {
  const nameFilter = (nameFilterEl.value ?? '').trim().toLowerCase();
  const connectionFilter = connectionFilterEl.value;
  const certStatusFilter = certStatusFilterEl.value;

  return allCertnames().filter((certname) => {
    if (!showInfrastructureEl.checked && connectivityByCertname?.get(certname)?.isInfrastructure) return false;
    if (nameFilter && !certname.toLowerCase().includes(nameFilter)) return false;
    if (connectionFilter) {
      const target = agentlessByName.get(certname);
      // An agentless target is "connected" when its last check worked.
      const connected = target ? Boolean(target.check.ok) : connectivityByCertname?.get(certname)?.connected ?? false;
      if (connectionFilter === 'connected' && !connected) return false;
      if (connectionFilter === 'not-connected' && connected) return false;
    }
    if (certStatusFilter) {
      const status = connectivityByCertname?.get(certname)?.certStatus ?? 'unknown';
      if (status !== certStatusFilter) return false;
    }
    return true;
  });
}

function sortedCertnames() {
  const comparator = SORT_COMPARATORS[sortColumn];
  const sorted = filteredCertnames().sort(comparator);
  return sortDirection === 'asc' ? sorted : sorted.reverse();
}

function sortIndicator(column) {
  if (column !== sortColumn) return '';
  return sortDirection === 'asc' ? ' ▲' : ' ▼';
}

function sortableHeader(column, label) {
  return `<th scope="col"><a href="#" data-sort-column="${column}">${escapeHtml(label)}${sortIndicator(column)}</a></th>`;
}

function render() {
  if (inventoryCertnames === null) return; // still loading the initial node list

  const certnames = sortedCertnames();
  pruneSelection(certnames);

  if (certnames.length === 0) {
    renderSelectionBar();
    const hasAnyNodes = allCertnames().length > 0;
    results.innerHTML = hasAnyNodes
      ? `
      <vox-empty-state heading="${t('No matching nodes')}">
        <vox-icon slot="icon" name="search" size="lg"></vox-icon>
        No nodes match the current filters.
      </vox-empty-state>`
      : `
      <vox-empty-state heading="${t('No nodes found')}">
        <vox-icon slot="icon" name="node" size="lg"></vox-icon>
        No nodes known yet. Choose <strong>Add node</strong> above for the
        command to run on the machine you want to manage.
      </vox-empty-state>`;
    return;
  }

  const inventorySet = new Set(inventoryCertnames);
  const rows = certnames
    .map((certname) => {
      const nodeLink = inventorySet.has(certname)
        ? `<a href="/node.html?name=${encodeURIComponent(certname)}">${escapeHtml(certname)}</a>`
        : escapeHtml(certname);
      return nodeRow(certname, nodeLink);
    })
    .join('');

  results.innerHTML = `
    <div class="vox-table-wrap">
      <table class="vox-table vox-table--striped">
        <thead>
          <tr>
            ${canRun ? '<th scope="col"><vox-checkbox id="select-all"></vox-checkbox></th>' : ''}
            ${sortableHeader('name', t('Node'))}
            ${sortableHeader('connection', t('Connection'))}
            ${sortableHeader('lastConnected', t('Last connected'))}
            ${sortableHeader('certStatus', t('Cert status'))}
            <th scope="col"></th>
          </tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </div>`;

  results.querySelectorAll('a[data-sort-column]').forEach((el) => {
    el.addEventListener('click', (event) => {
      event.preventDefault();
      const column = el.dataset.sortColumn;
      if (sortColumn === column) {
        sortDirection = sortDirection === 'asc' ? 'desc' : 'asc';
      } else {
        sortColumn = column;
        sortDirection = 'asc';
      }
      render();
    });
  });

  bindSelection(certnames);
  bindCertActions();
}

// Selection is pruned to what's currently visible on every render, so a
// filter change can't leave nodes silently selected off-screen and then
// run against them.
function pruneSelection(visible) {
  const shown = new Set(visible);
  for (const certname of selected) {
    if (!shown.has(certname)) selected.delete(certname);
  }
}

function selectionHasAgentless() {
  return [...selected].some((c) => agentlessByName.has(c));
}

function renderSelectionBar() {
  if (!canRun) return;
  const count = selected.size;
  selectionBar.classList.toggle('vox-hide', count === 0);
  selectionCount.textContent = tn('{count} node selected', '{count} nodes selected', count);
  // Puppet needs the agent; tasks do not. An agentless target in the
  // selection leaves only the task button.
  const agentless = selectionHasAgentless();
  runSelectedButton.disabled = agentless;
  runSelectedButton.title = agentless ? t('Puppet runs need the node agent, and a selected node is agentless. Run a task instead.') : '';
}

function bindSelection(visible) {
  if (!canRun) return;
  results.querySelectorAll('vox-checkbox[data-select-certname]').forEach((el) => {
    el.addEventListener('change', () => {
      const certname = el.dataset.selectCertname;
      if (el.checked) selected.add(certname);
      else selected.delete(certname);
      syncSelectAll(visible);
      renderSelectionBar();
    });
  });

  const selectAll = results.querySelector('#select-all');
  if (selectAll) {
    syncSelectAll(visible);
    selectAll.addEventListener('change', () => {
      for (const certname of visible) {
        if (selectAll.checked) selected.add(certname);
        else selected.delete(certname);
      }
      // Re-render rather than poking each checkbox, so the rows and the
      // bar can't disagree about what's selected.
      render();
    });
  }
  renderSelectionBar();
}

function syncSelectAll(visible) {
  const selectAll = results.querySelector('#select-all');
  if (!selectAll) return;
  selectAll.checked = visible.length > 0 && visible.every((c) => selected.has(c));
}

// A run applies configuration on every target, so the count is
// confirmed before anything is dispatched - selecting 200 nodes with a
// header checkbox is one click away. The dialog that confirms it is
// also where a no-op, debug or other-environment run is chosen.
function runSelected() {
  const targets = [...selected];
  if (targets.length === 0) return;
  actionError.innerHTML = '';
  openRunPuppetDialog({ targets });
}

function showActionError(message) {
  actionError.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
}

// runCertAction posts/deletes the given certificate action, then
// refreshes connectivity from the server (rather than guessing the new
// state locally) so the row reflects what the CA actually did.
async function runCertAction(method, url) {
  actionError.innerHTML = '';
  try {
    await sendJSON(url, method);
    await loadConnectivity();
  } catch (err) {
    showActionError(err.message);
  }
}

// runDeleteAction reloads the full node list afterward (not just
// connectivity, like runCertAction does) - a deleted node needs to
// disappear from inventoryCertnames itself, not just get a badge
// update. openvoxdb excludes a deactivated node from that list
// unconditionally (see design.md in add-node-deletion), so a plain
// reload is enough - no client-side row removal needs to be guessed at.
async function runDeleteAction(certname) {
  actionError.innerHTML = '';
  try {
    await sendJSON(`/api/v1/nodes/${encodeURIComponent(certname)}`, 'DELETE');
    await load();
  } catch (err) {
    showActionError(err.message);
  }
}

// infrastructureAwareConfirm returns genericBody unchanged for a managed
// node, or a body naming certname's specific infrastructure reason in
// place of it - so the real consequence of acting on this console's own
// service identities is stated up front, not discovered after the fact.
// See design.md in add-infrastructure-cert-detection. Callers must pass
// genericBody with any interpolated certname already escapeHtml()'d -
// this, like confirmDialog itself, treats body as trusted HTML.
function infrastructureAwareConfirm(certname, genericBody) {
  const entry = connectivityByCertname?.get(certname);
  if (!entry?.isInfrastructure) return genericBody;
  const safeCertname = escapeHtml(certname);
  const safeReason = escapeHtml(entry.infrastructureReason);
  return `<p>"${safeCertname}" is infrastructure, not a managed node: ${safeReason}. Acting on it may break real functionality that depends on it.</p>`;
}

// showAddNodeDialog lists every platform's install script as a full,
// copyable URL (resolved against this page's own origin). Built
// directly here rather than through confirmDialog, since this is a
// plain info dialog (a single Close action, no confirm/cancel
// semantics) - confirmDialog is specifically for yes/no confirmations.
const INSTALL_SCRIPT_PLATFORMS = [
  { label: 'Linux (Debian/Ubuntu or RedHat family)', path: '/packages/install.sh' },
  { label: 'Windows', path: '/packages/install.ps1' },
  { label: 'macOS', path: '/packages/install-macos.sh' },
];

function showAddNodeDialog() {
  const rows = INSTALL_SCRIPT_PLATFORMS.map(({ label, path }) => {
    const url = `${window.location.origin}${path}`;
    const safeUrl = escapeHtml(url);
    return `<p><strong>${escapeHtml(label)}</strong><br><a href="${safeUrl}">${safeUrl}</a></p>`;
  }).join('');

  const dialog = document.createElement('vox-dialog');
  dialog.heading = 'Add a node';
  dialog.setAttribute('light-dismiss', '');
  dialog.innerHTML = `
    <p>Run the install script below for the new node's platform, as that node's root/administrator user.</p>
    ${rows}
    <div slot="footer">
      <vox-button variant="brand" data-action="close">${t('Close')}</vox-button>
    </div>`;
  dialog.addEventListener('vox-close', () => dialog.remove());
  dialog.querySelector('[data-action="close"]').addEventListener('click', () => dialog.close());
  document.body.appendChild(dialog);
  dialog.show();
}

addNodeButton.addEventListener('click', showAddNodeDialog);

function bindCertActions() {
  results.querySelectorAll('vox-button[data-sign-certname]').forEach((el) => {
    el.addEventListener('click', () => {
      runCertAction('POST', `/api/v1/nodes/${encodeURIComponent(el.dataset.signCertname)}/cert/sign`);
    });
  });
  results.querySelectorAll('vox-button[data-revoke-certname]').forEach((el) => {
    el.addEventListener('click', async () => {
      const certname = el.dataset.revokeCertname;
      const safeCertname = escapeHtml(certname);
      const body = infrastructureAwareConfirm(
        certname,
        `<p>Revoke the certificate for "${safeCertname}"? It will immediately lose access on its next connection attempt.</p>`
      );
      const confirmed = await confirmDialog({ heading: 'Revoke certificate', body, confirmLabel: 'Revoke', danger: true });
      if (!confirmed) return;
      runCertAction('POST', `/api/v1/nodes/${encodeURIComponent(certname)}/cert/revoke`);
    });
  });
  results.querySelectorAll('vox-button[data-clean-certname]').forEach((el) => {
    el.addEventListener('click', async () => {
      const certname = el.dataset.cleanCertname;
      const safeCertname = escapeHtml(certname);
      const body = infrastructureAwareConfirm(
        certname,
        `<p>Clean the certificate for "${safeCertname}"? This removes its CA record entirely - "${safeCertname}" will have to submit a brand new certificate request to re-enroll.</p>`
      );
      const confirmed = await confirmDialog({ heading: 'Clean certificate', body, confirmLabel: 'Clean', danger: true });
      if (!confirmed) return;
      runCertAction('DELETE', `/api/v1/nodes/${encodeURIComponent(certname)}/cert`);
    });
  });
  results.querySelectorAll('vox-button[data-delete-certname]').forEach((el) => {
    el.addEventListener('click', async () => {
      const certname = el.dataset.deleteCertname;
      const safeCertname = escapeHtml(certname);
      // Explicit about there being no undo (see design.md in
      // add-node-deletion - openvoxdb has no supported way to list a
      // deactivated node back out once it's gone from this list), not
      // just a generic "are you sure?".
      const body = infrastructureAwareConfirm(
        certname,
        `<p>Delete "${safeCertname}"? It will disappear from this list immediately. Its historical data (facts, catalogs, reports) is not deleted right away - openvoxdb removes it later, on its own schedule. This cannot be undone from the console: once deleted, "${safeCertname}" cannot be viewed or restored here.</p>`
      );
      const confirmed = await confirmDialog({ heading: 'Delete node', body, confirmLabel: 'Delete', danger: true });
      if (!confirmed) return;
      runDeleteAction(certname);
    });
  });
}

// fetchAllNodes assembles the complete node list across every page of
// /api/v1/nodes, rather than rendering one server-paginated page at a
// time - sorting/filtering need to operate on the whole fleet, and the
// backend already fetches everything from openvoxdb internally per
// request regardless of page size (see design.md), so this adds no new
// backend cost.
async function fetchAllNodes() {
  const pageSize = 100;
  let page = 1;
  let items = [];
  let total = Infinity;
  while (items.length < total) {
    const res = await withLoading(results, () => fetchJSON(`/api/v1/nodes?page=${page}&pageSize=${pageSize}`));
    if (res.items.length === 0) break;
    items = items.concat(res.items);
    total = res.total;
    page += 1;
  }
  return items;
}

async function load() {
  try {
    const items = await fetchAllNodes();
    inventoryCertnames = items.map((n) => n.certname);
    try {
      const page = await fetchJSON('/api/v1/agentless/targets');
      agentlessByName = new Map(page.items.map((x) => [x.name, x]));
    } catch {
      // Without them the list is the agent nodes alone.
    }
    render();
  } catch (err) {
    results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

async function loadConnectivity() {
  try {
    const data = await fetchJSON('/api/v1/node-connectivity');
    connectivityByCertname = new Map(data.nodes.map((n) => [n.certname, n]));
  } catch {
    // Fail open to "not connected"/"unknown" for every node rather than
    // leaving every row stuck on "checking..." forever.
    connectivityByCertname = new Map();
  }
  render();
}

// A user whose roles are granted on particular node groups sees only
// those groups' nodes. Say so, or a short list looks like a broken one.
async function loadScopeNotice() {
  const notice = document.getElementById('scope-notice');
  try {
    const me = await fetchJSON('/api/v1/me');
    const scope = me.nodeScope;
    if (!scope || scope.all) return;
    if (scope.groups.length === 0) {
      notice.innerHTML = `<vox-alert variant="warning" class="vox-m-bottom-md">${t('Your roles are not granted on any node group, so no nodes are shown. Ask an administrator to add a node group to one of your roles.')}</vox-alert>`;
      return;
    }
    const names = scope.groups.map((g) => escapeHtml(g.name)).join(', ');
    notice.innerHTML = `<vox-alert variant="info" class="vox-m-bottom-md">${tn(
      'You are seeing the nodes in this node group and the groups beneath it: {groups}',
      'You are seeing the nodes in these node groups and the groups beneath them: {groups}',
      scope.groups.length,
      { groups: names }
    )}</vox-alert>`;
  } catch {
    // The notice is an explanation, not data: without it the page still
    // shows exactly the nodes the caller may see.
  }
}

nameFilterEl.addEventListener('input', render);
connectionFilterEl.addEventListener('change', render);
certStatusFilterEl.addEventListener('change', render);
showInfrastructureEl.addEventListener('change', render);

runSelectedButton?.addEventListener('click', runSelected);
runTaskSelectedButton?.addEventListener('click', () => {
  if (selected.size > 0) openRunTaskDialog({ targets: [...selected] });
});
clearSelectionButton?.addEventListener('click', () => {
  selected.clear();
  render();
});

load();
loadConnectivity();
loadScopeNotice();
