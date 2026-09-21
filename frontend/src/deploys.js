import { fetchJSON, sendJSON, escapeHtml, hasPermission, statusVariant, paginationHTML, bindPagination, loadFilterOptions, withLoading } from './app.js';

// initDeploys wires the deploy history view. Exported rather than run
// on import so the Code page can defer it until its tab is first shown,
// and so both views can share one page without racing each other for
// element ids.
export function initDeploys() {
  const results = document.getElementById('deploy-results');
  if (!results) return;

  const triggerError = document.getElementById('trigger-error');
  const triggerButton = document.getElementById('trigger-deploy');
  const sourceFilter = document.getElementById('source-filter');
  const triggerSource = document.getElementById('trigger-source');
  const refFilter = document.getElementById('ref-filter');
  const triggeredByFilter = document.getElementById('triggered-by-filter');
  const statusFilter = document.getElementById('status-filter');
  const sortOrder = document.getElementById('sort-order');
  const activeFilters = document.getElementById('deploy-active-filters');

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
        <td>${escapeHtml(d.source)}</td>
        <td>${d.environment ? escapeHtml(d.environment) : ''}</td>
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
            <tr><th scope="col">Source</th><th scope="col">Environment</th><th scope="col">Ref</th><th scope="col">Status</th><th scope="col">Triggered by</th><th scope="col">Started</th><th scope="col">Finished</th><th scope="col">Error</th></tr>
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

  // Every filter in one list so the chips, the query and "clear all"
  // cannot drift apart: adding a filter here is all it takes for it to
  // appear as a chip and be clearable.
  const filters = [
    { label: 'Repository', el: () => sourceFilter, empty: '' },
    { label: 'Ref', el: () => refFilter, empty: '' },
    { label: 'Triggered by', el: () => triggeredByFilter, empty: '' },
    { label: 'Status', el: () => statusFilter, empty: '' },
  ];

  // Arriving from a repository record leaves ?source= in the URL.
  // Without stripping it, setting the filter back to "All" appears to
  // work and then silently comes back on the next reload - so clearing
  // a filter has to clear the URL that set it too.
  function syncURL() {
    const params = new URLSearchParams(window.location.search);
    if (sourceFilter.value) {
      params.set('source', sourceFilter.value);
    } else {
      params.delete('source');
    }
    const query = params.toString();
    window.history.replaceState(null, '', query ? `?${query}` : window.location.pathname);
  }

  function renderActiveFilters() {
    if (!activeFilters) return;

    const active = filters.filter((f) => f.el().value !== f.empty);
    if (active.length === 0) {
      activeFilters.innerHTML = '';
      return;
    }

    const chips = active
      .map(
        (f) => `
        <button type="button" class="filter-chip" data-clear="${escapeHtml(f.label)}"
                title="Clear this filter">
          <span class="filter-chip-label">${escapeHtml(f.label)}:</span>
          ${escapeHtml(f.el().value)}
          <span aria-hidden="true">&times;</span>
          <span class="vox-sr-only">Clear ${escapeHtml(f.label)} filter</span>
        </button>`
      )
      .join('');

    activeFilters.innerHTML = `
      <div class="filter-chips">
        <span class="filter-chips-lead">Filtered by</span>
        ${chips}
        ${active.length > 1 ? '<button type="button" class="filter-chip filter-chip--all" data-clear-all>Clear all</button>' : ''}
      </div>`;

    for (const button of activeFilters.querySelectorAll('[data-clear]')) {
      button.addEventListener('click', () => {
        const target = filters.find((f) => f.label === button.dataset.clear);
        if (target) target.el().value = target.empty;
        applyFilterChange();
      });
    }
    const clearAll = activeFilters.querySelector('[data-clear-all]');
    if (clearAll) {
      clearAll.addEventListener('click', () => {
        for (const f of filters) f.el().value = f.empty;
        applyFilterChange();
      });
    }
  }

  function applyFilterChange() {
    currentPage = 1;
    syncURL();
    load();
  }

  function buildQuery() {
    const params = new URLSearchParams();
    if (sourceFilter.value) params.set('source', sourceFilter.value);
    if (refFilter.value) params.set('ref', refFilter.value);
    if (triggeredByFilter.value) params.set('triggeredBy', triggeredByFilter.value);
    if (statusFilter.value) params.set('status', statusFilter.value);
    if (sortOrder.value === 'asc') params.set('sort', 'asc');
    params.set('page', currentPage);
    return `/api/v1/code-deploys?${params.toString()}`;
  }

  async function load() {
    try {
      const page = await withLoading(results, () => fetchJSON(buildQuery()));
      if (page.items.length === 0 && page.page > 1 && page.total > 0) {
        currentPage = 1;
        return load();
      }
      renderDeploys(page);
      renderActiveFilters();
    } catch (err) {
      results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  let debounceTimer;
  function debouncedLoad() {
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(applyFilterChange, 200);
  }

  triggeredByFilter.addEventListener('input', debouncedLoad);
  for (const el of [sourceFilter, refFilter, statusFilter, sortOrder]) {
    el.addEventListener('change', applyFilterChange);
  }

  loadFilterOptions(refFilter, '/api/v1/code-deploys/refs');

  // A repository record links here as ?source=<name>; preselect it so
  // the drill-down lands on that repository's history rather than the
  // whole fleet's.
  const requestedSource = new URLSearchParams(window.location.search).get('source');

  // The sources endpoint returns objects rather than the bare strings
  // loadFilterOptions expects, because the filter and the deploy picker
  // want different subsets of it: the filter lists every source that
  // appears in history (including one whose repo has since been removed
  // from the configuration), while only a configured source can
  // actually be deployed to.
  async function loadSources() {
    let sources;
    try {
      sources = await fetchJSON('/api/v1/code-deploys/sources');
    } catch {
      return; // leave the filter's "All" and the picker hidden
    }

    for (const source of sources) {
      const option = document.createElement('option');
      option.value = source.name;
      option.textContent = source.name;
      sourceFilter.appendChild(option);
    }

    if (requestedSource) {
      // Only if the source actually exists - a stale bookmark should
      // fall back to "All" rather than silently filtering to nothing.
      // querySelectorAll, not .options: vox-select is a custom element
      // wrapping a native select in its shadow root, so it exposes no
      // .options collection - reading one throws, and the throw used to
      // abort the rest of this function (preselect, chip, and the
      // multi-source deploy picker all silently stopped working).
      if ([...sourceFilter.querySelectorAll('option')].some((o) => o.value === requestedSource)) {
        sourceFilter.value = requestedSource;
        currentPage = 1;
        load();
      } else {
        // A stale link naming a source that no longer appears in
        // history: drop it from the URL too, so a reload does not keep
        // re-applying a filter the user cannot see or clear.
        syncURL();
      }
    }

    const deployable = sources.filter((s) => s.configured);
    // One source is the single-control-repo case: a picker with a
    // single choice is just noise, so it stays hidden and the trigger
    // sends no source, which the API reads as the default.
    if (deployable.length > 1) {
      for (const source of deployable) {
        const option = document.createElement('option');
        option.value = source.name;
        option.textContent = source.name;
        triggerSource.appendChild(option);
      }
      triggerSource.classList.remove('vox-hide');
    }
  }

  triggerButton.addEventListener('click', async () => {
    triggerError.innerHTML = '';
    triggerButton.disabled = true;
    try {
      const body = { environment: 'production' };
      if (triggerSource.value) body.source = triggerSource.value;
      await sendJSON('/api/v1/code-deploys', 'POST', body);
      await load();
    } catch (err) {
      triggerError.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    } finally {
      triggerButton.disabled = false;
    }
  });

  loadSources();
  load();
}
