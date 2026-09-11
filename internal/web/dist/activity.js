import { fetchJSON, escapeHtml, requirePermission, paginationHTML, bindPagination, loadFilterOptions } from './app.js';

if (requirePermission('activity:read')) {
  const results = document.getElementById('results');
  const categoryFilter = document.getElementById('category-filter');
  const actionFilter = document.getElementById('action-filter');
  const actorFilter = document.getElementById('actor-filter');
  const sortOrder = document.getElementById('sort-order');

  let currentPage = 1;

  function renderEvents(page) {
    if (page.items.length === 0) {
      results.innerHTML = `
        <vox-empty-state heading="No activity recorded yet">
          <vox-icon slot="icon" name="activity-log" size="lg"></vox-icon>
          Actions taken through the console will show up here.
        </vox-empty-state>`;
      return;
    }

    const rows = page.items
      .map(
        (e) => `
      <tr>
        <td>${escapeHtml(new Date(e.occurredAt).toLocaleString())}</td>
        <td><vox-badge variant="neutral">${escapeHtml(e.category)}</vox-badge></td>
        <td>${escapeHtml(e.action)}</td>
        <td>${escapeHtml(e.actor)}</td>
        <td>${escapeHtml(e.summary)}</td>
      </tr>`
      )
      .join('');

    results.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">When</th><th scope="col">Category</th><th scope="col">Action</th><th scope="col">Actor</th><th scope="col">Summary</th></tr>
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
    if (categoryFilter.value) params.set('category', categoryFilter.value);
    if (actionFilter.value) params.set('action', actionFilter.value);
    if (actorFilter.value) params.set('actor', actorFilter.value);
    if (sortOrder.value === 'asc') params.set('sort', 'asc');
    params.set('page', currentPage);
    // Deliberately "audit-log", not "activity" - some ad/privacy
    // blocklists filter fetch requests whose path contains generic
    // tracking-adjacent words, which broke this exact call for real
    // users behind such an extension.
    return `/api/v1/audit-log?${params.toString()}`;
  }

  async function load() {
    try {
      const page = await fetchJSON(buildQuery());
      if (page.items.length === 0 && page.page > 1 && page.total > 0) {
        currentPage = 1;
        return load();
      }
      renderEvents(page);
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

  actorFilter.addEventListener('input', debouncedLoad);
  for (const el of [categoryFilter, actionFilter, sortOrder]) {
    el.addEventListener('change', () => {
      currentPage = 1;
      load();
    });
  }

  loadFilterOptions(categoryFilter, '/api/v1/audit-log/categories');
  loadFilterOptions(actionFilter, '/api/v1/audit-log/actions');
  load();
}
