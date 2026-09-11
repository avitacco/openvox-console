import { fetchJSON, sendJSON, escapeHtml, requirePermission, hasPermission, statusVariant, paginationHTML, bindPagination, loadFilterOptions } from './app.js';

if (requirePermission('code:read')) {
  const results = document.getElementById('results');
  const triggerError = document.getElementById('trigger-error');
  const triggerButton = document.getElementById('trigger-deploy');
  const refFilter = document.getElementById('ref-filter');
  const triggeredByFilter = document.getElementById('triggered-by-filter');
  const statusFilter = document.getElementById('status-filter');
  const sortOrder = document.getElementById('sort-order');

  if (hasPermission('code:deploy')) {
    triggerButton.style.display = '';
  }

  let currentPage = 1;

  function renderDeploys(page) {
    if (page.items.length === 0) {
      results.innerHTML = `
        <vox-empty-state heading="No deploys yet">
          <vox-icon slot="icon" name="deploy" size="lg"></vox-icon>
          Trigger one above, or push to the control repo.
        </vox-empty-state>`;
      return;
    }

    const rows = page.items
      .map(
        (d) => `
      <tr>
        <td>${escapeHtml(d.ref)}</td>
        <td><vox-badge variant="${statusVariant(d.status)}">${escapeHtml(d.status)}</vox-badge></td>
        <td>${escapeHtml(d.triggeredBy)}</td>
        <td>${escapeHtml(new Date(d.startedAt).toLocaleString())}</td>
        <td>${d.finishedAt ? escapeHtml(new Date(d.finishedAt).toLocaleString()) : ''}</td>
        <td>${d.errorDetail ? escapeHtml(d.errorDetail) : ''}</td>
      </tr>`
      )
      .join('');

    results.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">Ref</th><th scope="col">Status</th><th scope="col">Triggered by</th><th scope="col">Started</th><th scope="col">Finished</th><th scope="col">Error</th></tr>
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

  function buildQuery() {
    const params = new URLSearchParams();
    if (refFilter.value) params.set('ref', refFilter.value);
    if (triggeredByFilter.value) params.set('triggeredBy', triggeredByFilter.value);
    if (statusFilter.value) params.set('status', statusFilter.value);
    if (sortOrder.value === 'asc') params.set('sort', 'asc');
    params.set('page', currentPage);
    return `/api/v1/code-deploys?${params.toString()}`;
  }

  async function load() {
    try {
      const page = await fetchJSON(buildQuery());
      if (page.items.length === 0 && page.page > 1 && page.total > 0) {
        currentPage = 1;
        return load();
      }
      renderDeploys(page);
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

  triggeredByFilter.addEventListener('input', debouncedLoad);
  for (const el of [refFilter, statusFilter, sortOrder]) {
    el.addEventListener('change', () => {
      currentPage = 1;
      load();
    });
  }

  loadFilterOptions(refFilter, '/api/v1/code-deploys/refs');

  triggerButton.addEventListener('click', async () => {
    triggerError.innerHTML = '';
    triggerButton.disabled = true;
    try {
      await sendJSON('/api/v1/code-deploys', 'POST', { environment: 'production' });
      await load();
    } catch (err) {
      triggerError.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    } finally {
      triggerButton.disabled = false;
    }
  });

  load();
}
