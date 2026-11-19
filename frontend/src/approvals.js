import { fetchJSON, sendJSON, escapeHtml, requirePermission, hasPermission, withLoading, t, formatDateTime, qs } from './app.js';
import { kindLabel } from './i18n.js';

// A request's summary starts mid-sentence ("task x::y on node"): shown as
// a line of its own it starts with a capital.
const sentenceCase = (text) => text.charAt(0).toUpperCase() + text.slice(1);

if (requirePermission('agent-requests:read')) {
  const results = document.getElementById('results');
  const panel = document.getElementById('decision-panel');
  const statusFilter = document.getElementById('status-filter');
  const canDecide = hasPermission('agent-requests:approve');

  function statusBadge(request) {
    const variants = { pending: 'warning', approved: 'success', rejected: 'danger', failed: 'danger', expired: 'neutral' };
    const labels = {
      pending: t('Waiting'), approved: t('Approved'), rejected: t('Rejected'), failed: t('Failed'), expired: t('Expired'),
    };
    return `<vox-badge variant="${variants[request.status] || 'neutral'}">${escapeHtml(labels[request.status] || request.status)}</vox-badge>`;
  }

  function render(items) {
    if (items.length === 0) {
      results.innerHTML = `
        <vox-empty-state heading="${escapeHtml(t('No requests'))}">
          <vox-icon slot="icon" name="orchestrate" size="lg"></vox-icon>
          ${escapeHtml(t('When an AI agent asks to run something, its request appears here.'))}
        </vox-empty-state>`;
      return;
    }
    const rows = items
      .map(
        (r) => `
      <tr>
        <td>${r.id}</td>
        <td>${escapeHtml(r.token)}</td>
        <td>${escapeHtml(sentenceCase(r.summary))}</td>
        <td>${statusBadge(r)}</td>
        <td>${escapeHtml(formatDateTime(r.createdAt))}</td>
        <td>${r.status === 'pending' ? escapeHtml(formatDateTime(r.expiresAt)) : '—'}</td>
        <td><vox-button data-review-id="${r.id}" size="sm" variant="${r.status === 'pending' && canDecide ? 'brand' : 'alt'}">${escapeHtml(r.status === 'pending' && canDecide ? t('Review') : t('View'))}</vox-button></td>
      </tr>`
      )
      .join('');
    results.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">#</th><th scope="col">${t('Agent')}</th><th scope="col">${t('Request')}</th><th scope="col">${t('Status')}</th><th scope="col">${t('Asked')}</th><th scope="col">${t('Expires')}</th><th scope="col"></th></tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
    results.querySelectorAll('vox-button[data-review-id]').forEach((el) => {
      el.addEventListener('click', () => open(items.find((r) => String(r.id) === el.dataset.reviewId)));
    });
  }

  // The request spelled out as what will run: its parameters are already
  // masked by the server, and are shown as they were stored.
  function detailRows(r) {
    const body = r.body || {};
    const lines = [];
    const add = (label, value) => lines.push(`<div><strong>${escapeHtml(label)}</strong> ${value}</div>`);
    add(t('Agent:'), escapeHtml(r.token));
    add(t('Kind:'), escapeHtml(kindLabel(r.kind)));
    if (body.task) add(t('Task:'), `<code>${escapeHtml(body.task)}</code>`);
    if (body.plan) add(t('Plan:'), `<code>${escapeHtml(body.plan)}</code>`);
    if (body.name && r.kind === 'plan') add(t('Plan:'), escapeHtml(body.name));
    if (body.environment) add(t('Environment:'), escapeHtml(body.environment));
    add(t('Asked:'), escapeHtml(formatDateTime(r.createdAt)));
    if (r.status === 'pending') add(t('Expires:'), escapeHtml(formatDateTime(r.expiresAt)));
    return `<div class="vox-display-flex vox-gap-lg vox-flex-wrap">${lines.join('')}</div>`;
  }

  function jsonBlock(label, value) {
    if (value === undefined || value === null) return '';
    if (typeof value === 'object' && Object.keys(value).length === 0) return '';
    return `<div class="vox-m-top-md"><strong>${escapeHtml(label)}</strong><pre class="value">${escapeHtml(JSON.stringify(value, null, 2))}</pre></div>`;
  }

  function open(request) {
    const r = request;
    const body = r.body || {};
    const nodes = r.targets && r.targets.length
      ? `<div class="vox-m-top-md"><strong>${escapeHtml(t('Nodes ({count}):', { count: r.targets.length }))}</strong><div class="vox-display-flex vox-flex-wrap vox-gap-sm vox-m-top-sm">${r.targets.map((n) => `<vox-badge variant="neutral">${escapeHtml(n)}</vox-badge>`).join('')}</div></div>`
      : `<p class="vox-m-top-md vox-color-text-2">${escapeHtml(t('This plan chooses its own nodes, among those its agent may reach.'))}</p>`;
    const outcome = [];
    if (r.decidedBy) outcome.push(`<div><strong>${escapeHtml(t('Decided by:'))}</strong> ${escapeHtml(r.decidedBy)}</div>`);
    if (r.note) outcome.push(`<div><strong>${escapeHtml(t('Note:'))}</strong> ${escapeHtml(r.note)}</div>`);
    if (r.failure) outcome.push(`<vox-alert variant="danger">${escapeHtml(r.failure)}</vox-alert>`);
    if (r.jobId) outcome.push(`<div><strong>${escapeHtml(t('Job:'))}</strong> <a href="/job.html?id=${r.jobId}">${escapeHtml(t('Job #{id}', { id: r.jobId }))}</a></div>`);

    const deciding = canDecide && r.status === 'pending'
      ? `<div class="vox-m-top-lg">
           <vox-textarea id="decision-note" rows="2" class="vox-width-full" label="${escapeHtml(t('Note (optional)'))}"></vox-textarea>
           <div class="vox-display-flex vox-gap-sm vox-m-top-md">
             <vox-button id="approve" variant="brand">${escapeHtml(t('Approve and run'))}</vox-button>
             <vox-button id="reject" variant="danger">${escapeHtml(t('Reject'))}</vox-button>
             <vox-button id="close-panel" variant="alt">${escapeHtml(t('Close'))}</vox-button>
           </div>
         </div>`
      : `<div class="vox-m-top-lg"><vox-button id="close-panel" variant="alt">${escapeHtml(t('Close'))}</vox-button></div>`;

    panel.innerHTML = `
      <vox-card class="vox-m-bottom-lg">
        <h2 class="vox-m-top-none">${escapeHtml(t('Request #{id}', { id: r.id }))} ${statusBadge(r)}</h2>
        <p>${escapeHtml(r.summary)}</p>
        <div id="decision-error"></div>
        ${detailRows(r)}
        ${nodes}
        ${jsonBlock(t('Parameters'), body.params ?? body.parameters)}
        ${jsonBlock(t('Options'), body.options)}
        ${jsonBlock(t('Steps'), body.steps)}
        ${outcome.length ? `<div class="vox-m-top-md vox-display-flex vox-flex-col vox-gap-sm">${outcome.join('')}</div>` : ''}
        <p class="vox-ts-sm vox-color-text-2 vox-m-top-md">${escapeHtml(t('Sensitive values are never shown. Approving runs exactly this, as the agent\'s token, on the nodes above.'))}</p>
        ${deciding}
      </vox-card>`;
    panel.scrollIntoView({ behavior: 'smooth', block: 'start' });
    document.getElementById('close-panel')?.addEventListener('click', () => { panel.innerHTML = ''; });

    const decide = async (action) => {
      const errorEl = document.getElementById('decision-error');
      errorEl.innerHTML = '';
      const note = document.getElementById('decision-note')?.value.trim() || '';
      try {
        const decided = await sendJSON(`/api/v1/agent-requests/${r.id}/${action}`, 'POST', { note });
        await load();
        open(decided);
      } catch (err) {
        errorEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
        await load();
      }
    };
    document.getElementById('approve')?.addEventListener('click', () => decide('approve'));
    document.getElementById('reject')?.addEventListener('click', () => decide('reject'));
  }

  async function load() {
    try {
      const status = statusFilter.value;
      const { items } = await withLoading(results, () => fetchJSON(`/api/v1/agent-requests${status ? `?status=${status}` : ''}`));
      render(items);
    } catch (err) {
      results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  statusFilter.addEventListener('change', load);
  load();
  // A link from a job opens the request it came from.
  const wanted = qs('id');
  if (wanted) {
    statusFilter.value = '';
    fetchJSON(`/api/v1/agent-requests/${encodeURIComponent(wanted)}`).then(open).then(load).catch(() => {});
  }
}
