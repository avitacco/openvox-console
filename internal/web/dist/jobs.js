import { fetchJSON, escapeHtml, requirePermission, statusVariant, targetSummaryHTML, paginationHTML, bindPagination, qs, withLoading, t, formatDateTime, statusLabel, kindLabel } from './app.js';

if (requirePermission('orchestrator:read')) {
  const results = document.getElementById('results');
  const targetFilter = document.getElementById('target-filter');
  const kindFilter = document.getElementById('kind-filter');
  const statusFilter = document.getElementById('status-filter');
  const triggeredByFilter = document.getElementById('triggered-by-filter');
  const sortOrder = document.getElementById('sort-order');

  // Lets a link like /jobs.html?target=web01.example.com (e.g. from a
  // node's own page) land pre-filtered, rather than requiring the
  // target to be re-typed by hand once here.
  targetFilter.value = qs('target') || '';

  let currentPage = 1;

  function jobName(j) {
    if (j.taskName) return j.taskName;
    if (j.planName) return j.planName;
    return '';
  }

  function renderJobs(page) {
    if (page.items.length === 0) {
      results.innerHTML = `
        <vox-empty-state heading="${t('No jobs yet')}">
          <vox-icon slot="icon" name="orchestrate" size="lg"></vox-icon>
          Trigger a run from a node's page to see it here.
        </vox-empty-state>`;
      return;
    }

    const rows = page.items
      .map(
        (j) => `
      <tr>
        <td><a href="/job.html?id=${j.id}">${j.id}</a></td>
        <td>${escapeHtml(kindLabel(j.kind))}</td>
        <td>${escapeHtml(jobName(j))}</td>
        <td>${targetSummaryHTML(j)}</td>
        <td><vox-badge variant="${statusVariant(j.status)}">${escapeHtml(statusLabel(j.status))}</vox-badge></td>
        <td>${escapeHtml(j.triggeredBy)}</td>
        <td>${escapeHtml(formatDateTime(j.startedAt))}</td>
        <td>${j.finishedAt ? escapeHtml(formatDateTime(j.finishedAt)) : ''}</td>
      </tr>`
      )
      .join('');

    results.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">${t('ID')}</th><th scope="col">${t('Kind')}</th><th scope="col">${t('Name')}</th><th scope="col">${t('Targets')}</th><th scope="col">${t('Status')}</th><th scope="col">${t('Triggered by')}</th><th scope="col">${t('Started')}</th><th scope="col">${t('Finished')}</th></tr>
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
    if (targetFilter.value) params.set('target', targetFilter.value);
    if (kindFilter.value) params.set('kind', kindFilter.value);
    if (statusFilter.value) params.set('status', statusFilter.value);
    if (triggeredByFilter.value) params.set('triggeredBy', triggeredByFilter.value);
    if (sortOrder.value === 'asc') params.set('sort', 'asc');
    params.set('page', currentPage);
    return `/api/v1/orchestrator/jobs?${params.toString()}`;
  }

  async function load() {
    try {
      const page = await withLoading(results, () => fetchJSON(buildQuery()));
      if (page.items.length === 0 && page.page > 1 && page.total > 0) {
        currentPage = 1;
        return load();
      }
      renderJobs(page);
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

  for (const el of [targetFilter, triggeredByFilter]) {
    el.addEventListener('input', debouncedLoad);
  }
  for (const el of [kindFilter, statusFilter, sortOrder]) {
    el.addEventListener('change', () => {
      currentPage = 1;
      load();
    });
  }

  load();
}
