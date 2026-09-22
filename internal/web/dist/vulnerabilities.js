import { fetchJSON, escapeHtml, requirePermission, hasPermission, paginationHTML, bindPagination, severityBadge, withLoading, t } from './app.js';

if (requirePermission('vulnerabilities:read')) {
  const results = document.getElementById('results');
  const coverageEl = document.getElementById('coverage');
  const severityFilter = document.getElementById('severity-filter');
  const fixFilter = document.getElementById('fix-filter');
  const providerFilter = document.getElementById('provider-filter');
  const packageFilter = document.getElementById('package-filter');
  const sortOrder = document.getElementById('sort-order');
  const providersLink = document.getElementById('providers-link');
  const canManage = hasPermission('vulnerabilities:manage');

  let currentPage = 1;

  // Default to findings a patch would actually fix. Debian and Ubuntu
  // publish a record for every CVE ever triaged against a source package,
  // including ones they have decided not to fix, so an unfiltered list is
  // dominated by findings nobody can act on (thousands per node). The
  // select shows the active filter, and "All" is one click away.
  fixFilter.value = 'true';

  if (canManage) {
    providersLink.style.display = '';
    // Listing providers needs vulnerabilities:manage, so the provider
    // filter is only offered to users who have it.
    fetchJSON('/api/v1/vulnerability-providers')
      .then((providers) => {
        for (const p of providers) {
          const opt = document.createElement('option');
          opt.value = p.id;
          opt.textContent = p.name;
          providerFilter.appendChild(opt);
        }
        providerFilter.style.display = '';
      })
      .catch(() => {});
  }

  function renderCoverage(c) {
    if (c.providersEnabled === 0) {
      coverageEl.innerHTML = `<vox-alert variant="warning">No vulnerability provider is enabled, so no node has been assessed.${canManage ? ' <a href="/vulnerability-providers.html">Set up a provider</a>.' : ''}</vox-alert>`;
      return;
    }
    coverageEl.innerHTML = `
      <vox-grid min="10rem">
        <vox-stat value="${c.assessedNodes}" label="${t('Nodes assessed')}"></vox-stat>
        <vox-stat value="${c.notAssessedNodes}" label="${t('Nodes not assessed')}"></vox-stat>
        <vox-stat value="${c.providersEnabled}" label="${t('Providers enabled')}"></vox-stat>
      </vox-grid>`;
  }

  function render(page) {
    renderCoverage(page.coverage);
    if (page.items.length === 0) {
      if (page.coverage.providersEnabled === 0) {
        results.innerHTML = '';
        return;
      }
      const filtered = severityFilter.value || fixFilter.value || providerFilter.value || packageFilter.value.trim();
      const unassessed = page.coverage.notAssessedNodes > 0
        ? ` ${page.coverage.notAssessedNodes} node(s) weren't assessed, so aren't known to be clean.`
        : '';
      const onlyFixable = fixFilter.value === 'true' && !severityFilter.value && !providerFilter.value && !packageFilter.value.trim();
      let body;
      if (onlyFixable) {
        body = `No open finding on the ${page.coverage.assessedNodes} assessed nodes has a fix released. Set Fix to "All" to include findings with no fix available.${unassessed}`;
      } else if (filtered) {
        body = 'No open finding matches these filters.';
      } else {
        body = `None of the ${page.coverage.assessedNodes} assessed nodes has an open finding.${unassessed}`;
      }
      results.innerHTML = `
        <vox-empty-state heading="${filtered ? 'No matching vulnerabilities' : 'No open vulnerabilities'}">
          <vox-icon slot="icon" name="vulnerability" size="lg"></vox-icon>
          ${body}
        </vox-empty-state>`;
      return;
    }

    const rows = page.items
      .map((v) => `
        <tr>
          <th scope="row"><a href="/vulnerability.html?id=${encodeURIComponent(v.vulnId)}">${escapeHtml(v.vulnId)}</a></th>
          <td>${severityBadge(v.severity)}</td>
          <td><a href="/vulnerability.html?id=${encodeURIComponent(v.vulnId)}">${v.affectedNodes}</a></td>
          <td>${v.fixAvailable ? t('Available') : t('None released')}</td>
          <td>${escapeHtml(v.packages.join(', ')) || '—'}</td>
          <td>${escapeHtml(v.providers.map((p) => p.name).join(', '))}</td>
        </tr>`)
      .join('');
    results.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead><tr><th scope="col">${t('Vulnerability')}</th><th scope="col">${t('Severity')}</th><th scope="col">${t('Affected nodes')}</th><th scope="col">${t('Fix')}</th><th scope="col">${t('Packages')}</th><th scope="col">${t('Providers')}</th></tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>
      ${paginationHTML(page.page, page.pageSize, page.total)}`;
    bindPagination(results, (target) => {
      currentPage = target;
      load();
    });
  }

  function buildQuery() {
    const params = new URLSearchParams();
    if (severityFilter.value) params.set('severity', severityFilter.value);
    if (fixFilter.value) params.set('fixAvailable', fixFilter.value);
    if (providerFilter.value) params.set('provider', providerFilter.value);
    if (packageFilter.value.trim()) params.set('package', packageFilter.value.trim());
    params.set('sort', sortOrder.value || 'severity');
    params.set('page', currentPage);
    return `/api/v1/vulnerabilities?${params.toString()}`;
  }

  async function load() {
    try {
      const page = await withLoading(results, () => fetchJSON(buildQuery()));
      if (page.items.length === 0 && page.page > 1 && page.total > 0) {
        currentPage = 1;
        return load();
      }
      render(page);
    } catch (err) {
      results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  function reload() {
    currentPage = 1;
    load();
  }

  for (const el of [severityFilter, fixFilter, providerFilter, sortOrder]) {
    el.addEventListener('change', reload);
  }
  let debounceTimer;
  packageFilter.addEventListener('input', () => {
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(reload, 300);
  });

  load();
}
