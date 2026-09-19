import { fetchJSON, escapeHtml, hasPermission, statusVariant, targetSummaryText, paginationHTML, bindPagination } from './app.js';

const results = document.getElementById('results');
const nameFilter = document.getElementById('name-filter');
const factNameFilter = document.getElementById('fact-name-filter');
const factValueFilter = document.getElementById('fact-value-filter');
const statusSummaryEl = document.getElementById('status-summary');
const recentActivityEl = document.getElementById('recent-activity');
const recentJobsCard = document.getElementById('recent-jobs-card');
const recentJobsEl = document.getElementById('recent-jobs');

let currentPage = 1;

// Fixed set and order for the four fleet-status categories the backend
// always returns (see internal/inventory's classifyForSummary) - unlike
// the old dynamic per-status list, this endpoint's shape is now fixed,
// so there's nothing left to order dynamically.
const STATUS_CARDS = [
  { key: 'failed', label: 'Failed' },
  { key: 'corrected', label: 'Corrected' },
  { key: 'intentional', label: 'Intentional changes' },
  { key: 'unchanged', label: 'Unchanged' },
];

// loadStatusSummary (report status, from openvoxdb) and
// loadConnectivitySummary (live node transport connectivity) fetch and
// error-handle independently - see design.md in
// add-dashboard-connectivity-status - but both render into the same
// #status-summary row, so their output is combined here rather than
// each overwriting statusSummaryEl.innerHTML directly, which would
// otherwise race (whichever fetch resolves last would wipe out the
// other's cards).
let reportSummaryState = { mode: 'loading', html: '' };
let connectivityCardsHTML = '';

function renderStatusSummary() {
  if (reportSummaryState.mode === 'cards') {
    // One flat flex row (report cards, a divider, connectivity cards).
    // Cards keep their natural content width (no flex-grow/basis) -
    // growing a card's own box always leaves the extra width trailing
    // *after* its left-aligned text, so whichever card happened to sit
    // right before the divider looked (mis)attached to it whenever
    // that card's label was short ("Unchanged"), while a short label
    // right *after* the divider ("Connected") showed no such gap,
    // since its own trailing space falls after its own text instead -
    // an artifact of each card's label length, not a real spacing bug,
    // but one that read as the divider crowding the connectivity
    // numbers regardless of which stretching mechanism produced it
    // (vox-grid's 1fr columns, then per-card flex-grow, both tried and
    // rejected here). `justify-content: space-between` sidesteps this
    // entirely: leftover width becomes extra *gap* between items,
    // evenly distributed, rather than extra width inside any one
    // item's box - so the space around the divider is guaranteed
    // identical to every other inter-card gap regardless of label
    // length, while the row still spans edge-to-edge like the
    // original single-row design.
    statusSummaryEl.innerHTML = `
      <div class="vox-display-flex vox-flex-wrap vox-justify-between vox-gap-lg">
        ${reportSummaryState.html}
        <div style="flex: 0 0 auto; align-self: stretch; width: 1px; background-color: var(--vox-color-divider)"></div>
        ${connectivityCardsHTML}
      </div>`;
  } else if (reportSummaryState.mode !== 'loading') {
    // 'empty' or 'error': no report cards row to attach connectivity
    // cards to.
    statusSummaryEl.innerHTML = reportSummaryState.html;
  }
}

async function loadStatusSummary() {
  try {
    const summary = await fetchJSON('/api/v1/nodes/summary');
    if (summary.total === 0) {
      reportSummaryState = {
        mode: 'empty',
        html: `
        <vox-empty-state heading="No nodes yet">
          <vox-icon slot="icon" name="node" size="lg"></vox-icon>
          Go to the <a href="/nodes.html">Nodes</a> page and choose
          <strong>Add node</strong> for the command to run on the machine
          you want to manage.
        </vox-empty-state>`,
      };
      renderStatusSummary();
      return;
    }

    const stats = STATUS_CARDS.map(
      ({ key, label }) => `<vox-stat class="vox-m-x-lg" value="${summary.byStatus[key] ?? 0}" label="${escapeHtml(label)}"></vox-stat>`
    ).join('');
    reportSummaryState = { mode: 'cards', html: stats };
  } catch (err) {
    reportSummaryState = { mode: 'error', html: `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>` };
  }
  renderStatusSummary();
}

async function loadConnectivitySummary() {
  try {
    const data = await fetchJSON('/api/v1/node-connectivity');
    // Exclude infrastructure certnames (console, openvoxserver,
    // openvoxdb, CA, etc.) - the connectivity endpoint deliberately
    // includes them for the Nodes page's own use, but they're not
    // managed nodes and most never hold a node-agent-client connection
    // at all, so counting them here inflates "Disconnected" and
    // misrepresents fleet connectivity. Matches nodes.js's own default
    // (its "Show infrastructure certs" toggle starts off).
    const managed = data.nodes.filter((n) => !n.isInfrastructure);
    const connected = managed.filter((n) => n.connected).length;
    const disconnected = managed.length - connected;
    connectivityCardsHTML = `<vox-stat class="vox-m-x-lg" value="${connected}" label="Connected"></vox-stat><vox-stat class="vox-m-x-lg" value="${disconnected}" label="Disconnected"></vox-stat>`;
  } catch (err) {
    // A connectivity fetch failure shouldn't blank out the report
    // status cards - just drop the connectivity cards for this cycle.
    connectivityCardsHTML = '';
  }
  renderStatusSummary();
}

async function loadRecentActivity() {
  if (!hasPermission('activity:read')) {
    recentActivityEl.innerHTML = `<p class="vox-ts-sm">You don't have permission to view activity.</p>`;
    return;
  }
  try {
    // See activity.js's own comment: "audit-log", not "activity" - some
    // ad/privacy blocklists filter fetch requests with tracking-adjacent
    // path words.
    const page = await fetchJSON('/api/v1/audit-log?page=1&pageSize=5');
    if (page.items.length === 0) {
      recentActivityEl.innerHTML = `
        <vox-empty-state heading="No activity yet">
          <vox-icon slot="icon" name="activity-log" size="lg"></vox-icon>
          No activity recorded yet.
        </vox-empty-state>`;
      return;
    }
    const items = page.items
      .map((e) => {
        const occurred = new Date(e.occurredAt);
        const time = occurred.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
        const fullDate = occurred.toLocaleString([], { dateStyle: 'full', timeStyle: 'medium' });
        // Who did it, matching the Recent runs list's use of vox-datum
        // above. Omitted rather than shown blank for an event with no
        // actor - a scheduled sync has no user behind it, and "User:"
        // with nothing after it reads as missing data.
        const actor = e.actor
          ? `<vox-datum name="User">${escapeHtml(e.actor)}</vox-datum>`
          : '';
        return `
      <vox-record-list-item size="sm" heading="${escapeHtml(time)}" title="${escapeHtml(fullDate)}">
        <vox-badge variant="neutral" title="${escapeHtml(e.category)}">${escapeHtml(e.category)}</vox-badge>
        ${actor}
        <span slot="end" title="${escapeHtml(e.summary)}">${escapeHtml(e.summary)}</span>
      </vox-record-list-item>`;
      })
      .join('');
    recentActivityEl.innerHTML = `<vox-record-list>${items}</vox-record-list>`;
  } catch (err) {
    recentActivityEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

async function loadRecentJobs() {
  if (!hasPermission('orchestrator:read')) return;
  recentJobsCard.style.display = '';

  try {
    const page = await fetchJSON('/api/v1/orchestrator/jobs?page=1&pageSize=5');
    if (page.items.length === 0) {
      recentJobsEl.innerHTML = `
        <vox-empty-state heading="No runs yet">
          <vox-icon slot="icon" name="orchestrate" size="lg"></vox-icon>
          No runs triggered yet.
        </vox-empty-state>`;
      return;
    }
    const items = page.items
      .map((j) => {
        const started = new Date(j.startedAt);
        const time = started.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
        const fullDate = started.toLocaleString([], { dateStyle: 'full', timeStyle: 'medium' });
        const node = targetSummaryText(j) || 'no targets';
        return `
      <vox-record-list-item size="sm" heading="${escapeHtml(time)}" href="/job.html?id=${j.id}" title="${escapeHtml(fullDate)}">
        <vox-datum name="Run">#${j.id}</vox-datum>
        <vox-datum name="Node">${escapeHtml(node)}</vox-datum>
        <span slot="end">
          <vox-badge variant="${statusVariant(j.status)}">${escapeHtml(j.status)}</vox-badge>
        </span>
      </vox-record-list-item>`;
      })
      .join('');
    recentJobsEl.innerHTML = `<vox-record-list>${items}</vox-record-list>`;
  } catch (err) {
    recentJobsEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

function buildQuery() {
  const params = new URLSearchParams();
  if (nameFilter.value) params.set('name', nameFilter.value);
  if (factNameFilter.value && factValueFilter.value) {
    params.set('fact', factNameFilter.value);
    params.set('value', factValueFilter.value);
  }
  params.set('page', currentPage);
  return `/api/v1/nodes?${params.toString()}`;
}

function renderNodes(page) {
  if (page.items.length === 0) {
    results.innerHTML = `
      <vox-empty-state heading="No nodes found">
        <vox-icon slot="icon" name="search" size="lg"></vox-icon>
        No nodes matched. Try a different search term or filter.
      </vox-empty-state>`;
    return;
  }

  const rows = page.items
    .map((n) => {
      const status = n.status
        ? `<vox-badge variant="${statusVariant(n.status)}">${escapeHtml(n.status)}</vox-badge>`
        : '';
      const checkIn = n.reportTimestamp ? new Date(n.reportTimestamp).toLocaleString() : 'never';
      return `
        <tr>
          <td><a href="/node.html?name=${encodeURIComponent(n.certname)}">${escapeHtml(n.certname)}</a></td>
          <td>${status}</td>
          <td>${escapeHtml(checkIn)}</td>
        </tr>`;
    })
    .join('');

  results.innerHTML = `
    <div class="vox-table-wrap">
      <table class="vox-table vox-table--striped">
        <thead>
          <tr><th scope="col">Node</th><th scope="col">Status</th><th scope="col">Last check-in</th></tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
    ${paginationHTML(page.page, page.pageSize, page.total)}`;

  bindPagination(results, (targetPage) => {
    currentPage = targetPage;
    load();
  });
}

async function load() {
  try {
    const page = await fetchJSON(buildQuery());
    if (page.items.length === 0 && page.page > 1 && page.total > 0) {
      currentPage = 1;
      return load();
    }
    renderNodes(page);
  } catch (err) {
    results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

let debounceTimer;
function debouncedLoad() {
  clearTimeout(debounceTimer);
  debounceTimer = setTimeout(() => {
    currentPage = 1;
    load();
  }, 200);
}

for (const el of [nameFilter, factNameFilter, factValueFilter]) {
  el.addEventListener('input', debouncedLoad);
}

loadStatusSummary();
loadConnectivitySummary();
loadRecentActivity();
loadRecentJobs();
load();

// Keep the dashboard's independently-loaded sections current without a
// manual reload. Refreshed together, all in the same tick, rather than
// staggered - these sections describe the same fleet state from
// different angles (e.g. a failed run in Recent runs vs. its count in
// Fleet status, or a newly-added node in the inventory table vs. Fleet
// status's total), and refreshing them out of phase makes them visibly
// disagree with each other for part of each cycle.
setInterval(() => {
  loadStatusSummary();
  loadConnectivitySummary();
  loadRecentActivity();
  loadRecentJobs();
  load();
}, 30_000);
