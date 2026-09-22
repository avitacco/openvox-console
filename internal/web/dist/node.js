import { fetchJSON, sendJSON, escapeHtml, hasPermission, statusVariant, qs, severityBadge, coverageReasonText, closeReasonText, paginationHTML, bindPagination, withLoading, t, formatDateTime } from './app.js';

const certname = qs('name');
const factsEl = document.getElementById('facts');
const factsRawSwitch = document.getElementById('facts-raw-switch');
const packagesEl = document.getElementById('packages');
const vulnerabilitiesTab = document.getElementById('vulnerabilities-tab');
const vulnerabilitiesCoverageEl = document.getElementById('vulnerabilities-coverage');
const vulnerabilitiesEl = document.getElementById('vulnerabilities');
const vulnerabilitiesClosedSwitch = document.getElementById('vulnerabilities-closed-switch');
const reportsEl = document.getElementById('reports');
const statusFilter = document.getElementById('status-filter');
const runNowButton = document.getElementById('run-now');
const runNowError = document.getElementById('run-now-error');
const packageInventoryToggleEl = document.getElementById('package-inventory-toggle');
const packageInventorySwitch = document.getElementById('package-inventory-switch');
const packageInventoryStatusEl = document.getElementById('package-inventory-status');

// Neither node endpoint takes a limit or page parameter - both return
// every row for the node - so these lists cap what they show to the
// first PAGE_SIZE and let "View all" page over the array already
// fetched, rather than refetching or rendering thousands of rows at
// once. A single node can carry thousands of findings.
const PAGE_SIZE = 25;
const vulnState = { all: [], expanded: false, page: 1, assessed: false };
const reportState = { all: [], expanded: false, page: 1 };

// Header shared by both capped lists: says what's on screen out of the
// total, and toggles between the top-PAGE_SIZE view and the full
// paginated one. Absent entirely when everything already fits.
function listHeader(expanded, total, showing, nounPlural) {
  if (total <= PAGE_SIZE) return '';
  const label = expanded
    ? `Showing ${showing} of ${total} ${nounPlural}`
    : `Top ${showing} of ${total} ${nounPlural}`;
  const action = expanded ? `Show top ${PAGE_SIZE} only` : `View all ${total}`;
  return `
    <div class="vox-display-flex vox-justify-between vox-items-center vox-gap-md vox-m-bottom-md">
      <span class="vox-ts-sm">${label}</span>
      <a href="#" data-list-toggle>${escapeHtml(action)}</a>
    </div>`;
}

function bindListHeader(container, state, rerender) {
  const toggle = container.querySelector('[data-list-toggle]');
  if (!toggle) return;
  toggle.addEventListener('click', (ev) => {
    ev.preventDefault();
    state.expanded = !state.expanded;
    state.page = 1;
    rerender();
  });
}

document.getElementById('node-heading').textContent = certname;
document.getElementById('node-name').textContent = certname;

if (hasPermission('orchestrator:run')) {
  runNowButton.style.display = '';
}

runNowButton.addEventListener('click', async () => {
  runNowError.innerHTML = '';
  runNowButton.disabled = true;
  try {
    const job = await sendJSON('/api/v1/orchestrator/runs', 'POST', { targets: [certname] });
    window.location.href = `/job.html?id=${job.id}`;
  } catch (err) {
    runNowError.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  } finally {
    runNowButton.disabled = false;
  }
});

// Facts render two ways, chosen by the Raw JSON switch: a curated view
// of the handful an operator actually reads, and the complete fact set
// as JSON. A node reports 30-40 top-level facts (~60 KB on a real
// server), which is what Raw is for - the curated view deliberately
// shows a fraction of it.
const factsState = { all: null, showAllMounts: false, showAllInterfaces: false };

// Facter reports capacity as a preformatted string ("3.67%"). Parsed
// rather than recomputed from the *_bytes fields, since the string is
// what the node itself reported.
function capacityPercent(capacity) {
  const n = Number.parseFloat(String(capacity ?? '').replace('%', ''));
  return Number.isFinite(n) ? Math.min(Math.max(n, 0), 100) : null;
}

// voxblocks has no progress/meter component, so the bar is local markup
// (see shell.css). The percentage stays as text beside it rather than
// colour alone carrying the meaning.
function usageBar(capacity, detail, { muted = false } = {}) {
  const pct = capacityPercent(capacity);
  if (pct === null) return '—';
  const level = muted ? 'none' : pct >= 90 ? 'crit' : pct >= 75 ? 'warn' : 'ok';
  return `
    <div class="usage" data-level="${level}"${detail ? ` title="${escapeHtml(detail)}"` : ''}>
      <div class="usage-track"><span class="usage-fill" style="width: ${pct}%"></span></div>
      <span class="usage-pct">${escapeHtml(String(capacity))}</span>
    </div>`;
}

// A Nomad CSI mount path runs past 150 characters and what distinguishes
// one volume from another sits at the END, so trim from the middle
// rather than the right. Full path stays in the title.
function middleEllipsis(text, max = 44) {
  if (text.length <= max) return text;
  const head = Math.ceil((max - 1) / 2);
  const tail = Math.floor((max - 1) / 2);
  return `${text.slice(0, head)}…${text.slice(text.length - tail)}`;
}

// Docker bind-mounts exactly these three files into every container, and
// each reports the *host* filesystem's usage under what looks like a
// config-file path - never what an operator means by storage.
const BIND_MOUNT_FILES = new Set(['/etc/hosts', '/etc/hostname', '/etc/resolv.conf']);

// Of 47 mountpoints on a real server, 3 are worth showing. Keeping only
// mounts backed by a real block device drops tmpfs, nsfs, per-container
// overlay layer directories, and the zero-byte pseudo-filesystems that
// report a meaningless 100%. "/" is kept regardless because its device
// is `overlay` on a containerised node and would otherwise vanish. One
// device can appear at several paths (a CSI volume is mounted at both a
// per-alloc and a staging path), so the shortest path per device wins.
function interestingMounts(mountpoints) {
  const kept = new Map();
  for (const [path, m] of Object.entries(mountpoints || {})) {
    if (!m || (m.size_bytes ?? 0) <= 0) continue;
    if (BIND_MOUNT_FILES.has(path)) continue;
    const device = m.device || '';
    if (!device.startsWith('/dev/') && path !== '/') continue;
    const existing = kept.get(device);
    if (!existing || path.length < existing.path.length) kept.set(device, { path, m });
  }
  return [...kept.values()].sort((a, b) => a.path.localeCompare(b.path));
}

function factsSection(heading, body) {
  if (!body) return '';
  return `<section class="vox-m-bottom-lg"><h3 class="vox-m-bottom-md">${escapeHtml(heading)}</h3>${body}</section>`;
}

function factsTable(headers, rows) {
  return `
    <div class="vox-table-wrap">
      <table class="vox-table vox-table--striped">
        <thead><tr>${headers.map((h) => `<th scope="col">${escapeHtml(h)}</th>`).join('')}</tr></thead>
        <tbody>${rows}</tbody>
      </table>
    </div>`;
}

// Every pair is conditional: a container reports no dmi, a VM no serial,
// and an unreachable fact should leave no empty row behind.
function overviewHTML(f) {
  const pairs = [];
  const add = (label, value) => { if (value) pairs.push([label, String(value)]); };
  const os = f.os || {};
  add('Operating system', os.distro?.description || [os.name, os.release?.full].filter(Boolean).join(' '));
  add('Architecture', os.architecture);
  add('Kernel', [f.kernel, f.kernelrelease].filter(Boolean).join(' '));
  add('Uptime', f.system_uptime?.uptime);
  add('Virtualization', f.is_virtual === false ? 'physical' : f.virtual);
  const p = f.processors || {};
  if (p.count) {
    add('Processors', `${p.count} × ${p.models?.[0] || p.isa || 'CPU'}${p.speed ? ` @ ${p.speed}` : ''}`);
  }
  const n = f.networking || {};
  add('FQDN', n.fqdn);
  add('Domain', n.domain);
  add('Agent version', f.aio_agent_version || f.puppetversion);
  if (pairs.length === 0) return '';
  return `<dl class="fact-overview">${pairs
    .map(([k, v]) => `<div><dt>${escapeHtml(k)}</dt><dd>${escapeHtml(v)}</dd></div>`)
    .join('')}</dl>`;
}

// An interface's addresses, preferring the bindings arrays (an
// interface can hold several) and falling back to the singular ip/ip6
// a node reports when it has just one.
function interfaceAddresses(iface) {
  const v4 = (iface.bindings || []).map((b) => b.address).filter(Boolean);
  if (v4.length === 0 && iface.ip) v4.push(iface.ip);
  const v6 = (iface.bindings6 || []).map((b) => b.address).filter(Boolean);
  if (v6.length === 0 && iface.ip6) v6.push(iface.ip6);
  return { v4, v6 };
}

// A veth with only an fe80:: address is container plumbing, not network
// configuration - a Nomad host has eight of them against four real
// interfaces, so they'd bury the addresses worth reading.
function linkLocalOnly({ v4, v6 }) {
  return v4.length === 0 && v6.length > 0 && v6.every((a) => a.toLowerCase().startsWith('fe80:'));
}

function networkHTML(networking) {
  const all = Object.entries(networking?.interfaces || {})
    .map(([name, iface]) => ({ name, iface, addrs: interfaceAddresses(iface) }))
    .filter((i) => i.addrs.v4.length > 0 || i.addrs.v6.length > 0)
    .sort((a, b) => a.name.localeCompare(b.name));
  if (all.length === 0) return '';

  const interesting = all.filter((i) => !linkLocalOnly(i.addrs));
  const hidden = all.length - interesting.length;
  const showAll = factsState.showAllInterfaces;
  const visible = showAll ? all : interesting;

  const rows = visible
    .map(({ name, iface, addrs }) => {
      const isPrimary = name === networking.primary;
      // The primary is marked on the interface rather than the address:
      // networking.ip names one address, but it's the interface that is
      // the node's primary, and it can hold several.
      const label = `${escapeHtml(name)}${isPrimary ? ' <vox-badge variant="tip">primary</vox-badge>' : ''}`;
      // Joined on one line rather than broken with <br>: these cells
      // are nowrap/ellipsised (see shell.css), so a second line would
      // be clipped anyway. The full set stays on the title.
      const addr = (list) => (list.length
        ? `<span title="${escapeHtml(list.join(', '))}">${escapeHtml(list.join(', '))}</span>`
        : '—');
      return `
        <tr>
          <th scope="row">${label}</th>
          <td>${addr(addrs.v4)}</td>
          <td>${addr(addrs.v6)}</td>
          <td>${escapeHtml(iface.mac || '—')}</td>
        </tr>`;
    })
    .join('');

  const note = hidden > 0
    ? `<p class="vox-ts-sm vox-m-top-md">${showAll
        ? `Showing all ${all.length} interfaces. <a href="#" data-interfaces-toggle>Show only addressed interfaces</a>`
        : `Only showing interfaces with a routable address. <a href="#" data-interfaces-toggle>Show all ${all.length}</a>`}</p>`
    : '';

  return factsTable(['Interface', 'IPv4', 'IPv6', 'MAC'], rows) + note;
}

function memoryHTML(memory) {
  const rows = ['system', 'swap']
    .filter((k) => memory?.[k])
    .map((k) => {
      const m = memory[k];
      return `
        <tr>
          <th scope="row">${k === 'system' ? 'RAM' : 'Swap'}</th>
          <td class="usage-cell">${usageBar(m.capacity, `${m.used || '?'} used of ${m.total || '?'}, ${m.available || '?'} free`)}</td>
          <td>${escapeHtml(m.used || '—')} / ${escapeHtml(m.total || '—')}</td>
          <td>${escapeHtml(m.available || '—')}</td>
        </tr>`;
    })
    .join('');
  return rows ? factsTable(['', 'Used', 'Used / total', 'Available'], rows) : '';
}

// Disks carry no usage figure - that lives on mountpoints - so this is
// identity only, with model/serial/vendor on hover. A Ceph RBD device
// reports none of those three, hence the fallbacks.
function disksHTML(disks) {
  const entries = Object.entries(disks || {}).sort(([a], [b]) => a.localeCompare(b));
  if (entries.length === 0) return '';
  const rows = entries
    .map(([name, d]) => {
      // Model is its own column now, so the tooltip carries what's left.
      // A Ceph RBD device reports none of the three, and then there is
      // no tooltip rather than an empty one.
      const detail = [d.vendor, d.serial ? `serial ${d.serial}` : '']
        .filter(Boolean).join(' · ');
      return `
        <tr${detail ? ` title="${escapeHtml(detail)}"` : ''}>
          <th scope="row" title="${escapeHtml(name)}">${escapeHtml(name)}</th>
          <td title="${escapeHtml(d.model || '')}">${escapeHtml(d.model || '—')}</td>
          <td>${escapeHtml(d.size || '—')}</td>
          <td>${escapeHtml(d.type || '—')}</td>
        </tr>`;
    })
    .join('');
  return factsTable(['Device', 'Model', 'Size', 'Type'], rows);
}

function mountRow(path, m) {
  const detail = `${path}\n${m.filesystem || '?'} on ${m.device || '?'}\n${m.used || '?'} used of ${m.size || '?'}, ${m.available || '?'} free`;
  // A zero-byte pseudo-filesystem reports a meaningless "100%" (nsfs,
  // mqueue and devpts all do), and these only appear at all once
  // everything is expanded. They still get a bar, so the column reads
  // consistently, but a muted one: colouring it danger red would say a
  // disk is about to fill up, which is the opposite of the truth.
  const zeroCapacity = (m.size_bytes ?? 0) <= 0;
  const usage = usageBar(
    m.capacity,
    zeroCapacity ? `${detail}\n\nNo capacity to fill - the 100% is an artefact of a pseudo-filesystem, not a full disk.` : detail,
    { muted: zeroCapacity },
  );
  return `
    <tr>
      <th scope="row" title="${escapeHtml(path)}">${escapeHtml(middleEllipsis(path))}</th>
      <td class="usage-cell">${usage}</td>
      <td>${escapeHtml(m.used || '—')} / ${escapeHtml(m.size || '—')}</td>
      <td title="${escapeHtml(m.filesystem || '')}">${escapeHtml(m.filesystem || '—')}</td>
      <td title="${escapeHtml(m.device || '')}">${escapeHtml(m.device || '—')}</td>
    </tr>`;
}

function mountsHTML(mountpoints) {
  const all = Object.entries(mountpoints || {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([path, m]) => ({ path, m }));
  if (all.length === 0) return '';

  const interesting = interestingMounts(mountpoints);
  const hidden = all.length - interesting.length;
  const showAll = factsState.showAllMounts;
  const visible = showAll ? all : interesting;

  // The filter is stated rather than applied silently - on a Nomad
  // server it hides 44 of 47 mounts, too much to do invisibly - but the
  // count rides on the link instead of a long sentence listing every
  // filesystem type that didn't make the cut.
  const note = hidden > 0
    ? `<p class="vox-ts-sm vox-m-top-md">${showAll
        ? `Showing all ${all.length} mounts. <a href="#" data-mounts-toggle>Show only real filesystems</a>`
        : `Only showing real filesystems. <a href="#" data-mounts-toggle>Show all ${all.length}</a>`}</p>`
    : '';

  // A node can have mounts where none survive the filter (every one a
  // tmpfs). The note still has to render, or there is no way back.
  if (visible.length === 0) return note;

  return factsTable(['Mount', 'Used', 'Used / size', 'Type', 'Device'],
    visible.map(({ path, m }) => mountRow(path, m)).join('')) + note;
}

function renderFacts() {
  const f = factsState.all;
  if (!f) return;
  if (Object.keys(f).length === 0) {
    factsEl.innerHTML = `<vox-empty-state heading="${t('No facts reported')}"></vox-empty-state>`;
    return;
  }

  if (factsRawSwitch?.checked) {
    // Sorted by rebuilding the object, NOT via a replacer array: an
    // array replacer applies at every level, so nested fact structures
    // would lose any key that isn't also a top-level fact name.
    const sorted = Object.fromEntries(Object.keys(f).sort().map((k) => [k, f[k]]));
    const json = JSON.stringify(sorted, null, 2);
    factsEl.innerHTML = `<vox-code-block language="json" no-header no-border>${escapeHtml(json)}</vox-code-block>`;
    return;
  }

  const body = [
    overviewHTML(f),
    factsSection('Network', networkHTML(f.networking)),
    factsSection('Memory', memoryHTML(f.memory)),
    factsSection('Disks', disksHTML(f.disks)),
    factsSection('Filesystems', mountsHTML(f.mountpoints)),
  ].join('');

  // A node can report facts without reporting any of the hardware ones
  // this view curates - say a fact-collection failure, or a platform
  // Facter has less to say about. Saying so beats a blank panel.
  factsEl.innerHTML = body.trim()
    ? body
    : `<vox-empty-state heading="${t('Nothing to summarise')}">This node reported no hardware facts. Switch to Raw JSON for everything it did report.</vox-empty-state>`;

  // Rebound on every render - innerHTML above replaces the links.
  factsEl.querySelector('[data-mounts-toggle]')?.addEventListener('click', (ev) => {
    ev.preventDefault();
    factsState.showAllMounts = !factsState.showAllMounts;
    renderFacts();
  });
  factsEl.querySelector('[data-interfaces-toggle]')?.addEventListener('click', (ev) => {
    ev.preventDefault();
    factsState.showAllInterfaces = !factsState.showAllInterfaces;
    renderFacts();
  });
}

async function loadFacts() {
  try {
    const detail = await fetchJSON(`/api/v1/nodes/${encodeURIComponent(certname)}`);
    factsState.all = detail.facts || {};
    renderFacts();
  } catch (err) {
    factsEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

factsRawSwitch?.addEventListener('change', renderFacts);

async function loadPackages() {
  try {
    const packages = await withLoading(packagesEl, () => fetchJSON(`/api/v1/nodes/${encodeURIComponent(certname)}/packages`));

    if (packages.length === 0) {
      packagesEl.innerHTML = `<vox-empty-state heading="${t('No package data reported')}"></vox-empty-state>`;
      return;
    }

    // A source line only when the source package actually differs - an
    // apt package that is its own source would just repeat its row.
    const sourceLine = (p) => (p.sourcePackage && (p.sourcePackage !== p.packageName || p.sourceVersion !== p.version)
      ? `<div class="vox-ts-sm">from ${escapeHtml(p.sourcePackage)} ${escapeHtml(p.sourceVersion)}</div>`
      : '');
    const rows = packages
      .map((p) => `
        <tr>
          <th scope="row">${escapeHtml(p.packageName)}${sourceLine(p)}</th>
          <td>${escapeHtml(p.version)}</td>
          <td>${escapeHtml(p.provider)}</td>
        </tr>`)
      .join('');
    packagesEl.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead><tr><th scope="col">${t('Package')}</th><th scope="col">${t('Version')}</th><th scope="col">${t('Provider')}</th></tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
  } catch (err) {
    packagesEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

async function loadReports() {
  try {
    const params = new URLSearchParams();
    if (statusFilter.value) params.set('status', statusFilter.value);
    const qsStr = params.toString();
    const url = `/api/v1/nodes/${encodeURIComponent(certname)}/reports${qsStr ? `?${qsStr}` : ''}`;
    reportState.all = await withLoading(reportsEl, () => fetchJSON(url));
    reportState.expanded = false;
    reportState.page = 1;
    renderReports();
  } catch (err) {
    reportsEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

// openvoxdb returns a node's reports newest-first, so the uncapped view
// is simply the first PAGE_SIZE of them - no client-side sort needed.
function renderReports() {
  const all = reportState.all;
  if (all.length === 0) {
    reportsEl.innerHTML = `
      <vox-empty-state heading="${t('No reports yet')}">
        <vox-icon slot="icon" name="report" size="lg"></vox-icon>
        Reports appear here after this node's next Puppet run.
      </vox-empty-state>`;
    return;
  }

  const start = reportState.expanded ? (reportState.page - 1) * PAGE_SIZE : 0;
  const visible = all.slice(start, start + PAGE_SIZE);
  const rows = visible
    .map((r) => `
      <tr>
        <td><a href="/report.html?id=${encodeURIComponent(r.hash)}&node=${encodeURIComponent(certname)}">${formatDateTime(r.startTime)}</a></td>
        <td><vox-badge variant="${statusVariant(r.status)}">${escapeHtml(r.status)}</vox-badge></td>
        <td>${formatDateTime(r.endTime)}</td>
      </tr>`)
    .join('');
  reportsEl.innerHTML = `
    ${listHeader(reportState.expanded, all.length, visible.length, 'runs')}
    <div class="vox-table-wrap">
      <table class="vox-table vox-table--striped">
        <thead><tr><th scope="col">${t('Started')}</th><th scope="col">${t('Status')}</th><th scope="col">${t('Ended')}</th></tr></thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
    ${reportState.expanded ? paginationHTML(reportState.page, PAGE_SIZE, all.length) : ''}`;

  bindListHeader(reportsEl, reportState, renderReports);
  if (reportState.expanded) {
    bindPagination(reportsEl, (target) => {
      reportState.page = target;
      renderReports();
    });
  }
}

// loadPackageInventoryToggle reflects this node's current
// package-inventory reporting state and disables the control when the
// node isn't connected - a toggle requires a live dispatch (see
// design.md in add-package-inventory-toggle), so there's nothing to
// offer an operator for a disconnected node. Reuses the same
// connectivity data source the nodes list page already shows, just not
// previously fetched on this page.
async function loadPackageInventoryToggle() {
  if (!hasPermission('orchestrator:run')) return;
  packageInventoryToggleEl.style.display = '';

  let connected = false;
  try {
    const data = await fetchJSON('/api/v1/node-connectivity');
    connected = !!data.nodes.find((n) => n.certname === certname)?.connected;
  } catch {
    connected = false;
  }
  if (!connected) {
    packageInventorySwitch.disabled = true;
    packageInventoryStatusEl.innerHTML = `<vox-alert variant="info">Node is offline - package-inventory reporting can't be changed until it reconnects.</vox-alert>`;
    return;
  }

  try {
    const status = await fetchJSON(`/api/v1/nodes/${encodeURIComponent(certname)}/packages/reporting`);
    packageInventorySwitch.checked = status.enabled;
  } catch (err) {
    packageInventorySwitch.disabled = true;
    packageInventoryStatusEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

packageInventorySwitch.addEventListener('change', async () => {
  const desired = packageInventorySwitch.checked;
  packageInventoryStatusEl.innerHTML = '';
  packageInventorySwitch.disabled = true;
  try {
    const result = await sendJSON(`/api/v1/nodes/${encodeURIComponent(certname)}/packages/reporting`, 'PUT', { enabled: desired });
    packageInventorySwitch.checked = result.enabled;
    if (result.output) {
      // A real Puppet run happened (see design.md's Ran distinction) -
      // surface its outcome rather than just the resulting boolean,
      // and refresh the packages table since the run may have changed
      // what's reported.
      const succeeded = result.output.exitCode === 0 || result.output.exitCode === 2;
      packageInventoryStatusEl.innerHTML = `<vox-alert variant="${succeeded ? 'success' : 'danger'}">Puppet run completed (exit code ${result.output.exitCode}).</vox-alert>`;
      loadPackages();
    }
  } catch (err) {
    packageInventorySwitch.checked = !desired; // revert the optimistic toggle on failure
    packageInventoryStatusEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  } finally {
    packageInventorySwitch.disabled = false;
  }
});

statusFilter.addEventListener('change', loadReports);

// Vulnerabilities section (add-vulnerability-tracking): only shown with
// vulnerabilities:read, and never presents an unassessed node as clean.
async function loadVulnerabilities() {
  vulnerabilitiesTab.style.display = '';
  const includeClosed = vulnerabilitiesClosedSwitch.checked;
  try {
    const data = await fetchJSON(`/api/v1/nodes/${encodeURIComponent(certname)}/vulnerabilities${includeClosed ? '?includeClosed=true' : ''}`);
    const cov = data.coverage;
    const notAssessedBy = cov.providers.filter((p) => !p.assessed);
    const reasons = notAssessedBy
      .map((p) => `<li>${escapeHtml(p.provider.name)}: ${escapeHtml(coverageReasonText(p.reason))}</li>`)
      .join('');
    if (cov.noProvidersEnabled) {
      vulnerabilitiesCoverageEl.innerHTML = `<vox-alert variant="info">No vulnerability provider is enabled, so this node hasn't been assessed.</vox-alert>`;
    } else if (!cov.assessed) {
      vulnerabilitiesCoverageEl.innerHTML = `<vox-alert variant="warning">No provider has assessed this node, so it isn't known to be free of vulnerabilities.<ul>${reasons}</ul></vox-alert>`;
    } else {
      const assessedBy = cov.providers.filter((p) => p.assessed).map((p) => escapeHtml(p.provider.name)).join(', ');
      vulnerabilitiesCoverageEl.innerHTML = `<p class="vox-ts-sm">Assessed by ${assessedBy}.</p>${notAssessedBy.length ? `<p class="vox-ts-sm">Not assessed by:</p><ul class="vox-ts-sm">${reasons}</ul>` : ''}`;
    }

    vulnState.all = data.findings;
    vulnState.assessed = cov.assessed;
    vulnState.expanded = false;
    vulnState.page = 1;
    renderVulnerabilities();
  } catch (err) {
    vulnerabilitiesEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

// The API already returns a node's findings open-first, then by
// descending severity, so the capped view is the most severe open
// findings without this having to re-sort anything.
function renderVulnerabilities() {
  const all = vulnState.all;
  if (all.length === 0) {
    vulnerabilitiesEl.innerHTML = vulnState.assessed
      ? `<vox-empty-state heading="${t('No open vulnerabilities')}"><vox-icon slot="icon" name="shield" size="lg"></vox-icon>No enabled provider reports an open finding on this node.</vox-empty-state>`
      : '';
    return;
  }

  const start = vulnState.expanded ? (vulnState.page - 1) * PAGE_SIZE : 0;
  const visible = all.slice(start, start + PAGE_SIZE);
  const rows = visible
    .map((f) => `
      <tr>
        <th scope="row"><a href="/vulnerability.html?id=${encodeURIComponent(f.vulnId)}">${escapeHtml(f.vulnId)}</a></th>
        <td>${f.status === 'open' ? severityBadge(f.severity) : '—'}</td>
        <td>${f.status === 'open' ? 'Open' : `Closed (${escapeHtml(closeReasonText(f.closeReason))})`}</td>
        <td>${f.status !== 'open' ? '—' : f.fixAvailable ? 'Available' : 'None released'}</td>
        <td>${escapeHtml(f.packages.join(', ')) || '—'}</td>
        <td>${escapeHtml(formatDateTime(f.firstSeen))}</td>
        <td>${escapeHtml(f.providers.map((p) => p.name).join(', ')) || '—'}</td>
      </tr>`)
    .join('');
  vulnerabilitiesEl.innerHTML = `
    ${listHeader(vulnState.expanded, all.length, visible.length, 'findings')}
    <div class="vox-table-wrap">
      <table class="vox-table vox-table--striped">
        <thead><tr><th scope="col">${t('Vulnerability')}</th><th scope="col">${t('Severity')}</th><th scope="col">${t('Status')}</th><th scope="col">${t('Fix')}</th><th scope="col">${t('Packages')}</th><th scope="col">${t('First seen')}</th><th scope="col">${t('Providers')}</th></tr></thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
    ${vulnState.expanded ? paginationHTML(vulnState.page, PAGE_SIZE, all.length) : ''}`;

  bindListHeader(vulnerabilitiesEl, vulnState, renderVulnerabilities);
  if (vulnState.expanded) {
    bindPagination(vulnerabilitiesEl, (target) => {
      vulnState.page = target;
      renderVulnerabilities();
    });
  }
}

vulnerabilitiesClosedSwitch.addEventListener('change', loadVulnerabilities);

if (!certname) {
  factsEl.innerHTML = `<vox-alert variant="danger">${t('No node specified.')}</vox-alert>`;
} else {
  loadFacts();
  loadPackages();
  loadReports();
  loadPackageInventoryToggle();
  if (hasPermission('vulnerabilities:read')) {
    loadVulnerabilities();
  }
}
