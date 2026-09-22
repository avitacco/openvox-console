import { fetchJSON, escapeHtml, statusVariant, formatValue, qs, withLoading, t } from './app.js';

const reportId = qs('id');
const node = qs('node');
const eventsEl = document.getElementById('events');
const statusFilter = document.getElementById('status-filter');
const nodeLink = document.getElementById('node-link');

if (node) {
  nodeLink.href = `/node.html?name=${encodeURIComponent(node)}`;
  nodeLink.textContent = node;
} else {
  nodeLink.remove();
}

async function loadEvents() {
  try {
    const params = new URLSearchParams();
    if (statusFilter.value) params.set('status', statusFilter.value);
    const qsStr = params.toString();
    const url = `/api/v1/reports/${encodeURIComponent(reportId)}/events${qsStr ? `?${qsStr}` : ''}`;
    const events = await withLoading(eventsEl, () => fetchJSON(url));

    if (events.length === 0) {
      eventsEl.innerHTML = `
        <vox-empty-state heading="${t('No events for this report')}">
          <vox-icon slot="icon" name="check-circle" size="lg"></vox-icon>
          This run made no changes (or none match the current filter).
        </vox-empty-state>`;
      return;
    }

    const rows = events
      .map((e) => `
        <tr>
          <td>${escapeHtml(e.resourceType)}[${escapeHtml(e.resourceTitle)}]</td>
          <td>${escapeHtml(e.property || '')}</td>
          <td><vox-badge variant="${statusVariant(e.status)}">${escapeHtml(e.status)}</vox-badge></td>
          <td><pre class="value">${escapeHtml(formatValue(e.oldValue))}</pre></td>
          <td><pre class="value">${escapeHtml(formatValue(e.newValue))}</pre></td>
          <td>${escapeHtml(e.message || '')}</td>
        </tr>`)
      .join('');
    eventsEl.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">${t('Resource')}</th><th scope="col">${t('Property')}</th><th scope="col">${t('Status')}</th><th scope="col">${t('Old')}</th><th scope="col">${t('New')}</th><th scope="col">${t('Message')}</th></tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
  } catch (err) {
    eventsEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

statusFilter.addEventListener('change', loadEvents);

if (!reportId) {
  eventsEl.innerHTML = `<vox-alert variant="danger">${t('No report specified.')}</vox-alert>`;
} else {
  loadEvents();
}
