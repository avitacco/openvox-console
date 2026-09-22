import { fetchJSON, escapeHtml, hasPermission, paginationHTML, bindPagination, withLoading, tn, t, formatNumber } from './app.js';

const nameFilter = document.getElementById('name-filter');
const versionFilter = document.getElementById('version-filter');
const providerFilter = document.getElementById('provider-filter');
const groupFilter = document.getElementById('group-filter');
const searchError = document.getElementById('search-error');
const coverage = document.getElementById('coverage');
const results = document.getElementById('results');

const PAGE_SIZE = 50;

let currentPage = 1;
let providersLoaded = false;

function nodeLink(certname) {
  return `<a href="/node.html?name=${encodeURIComponent(certname)}">${escapeHtml(certname)}</a>`;
}

// The provider choices come from the catalogue response, but the select
// is only filled once: rebuilding it on every keystroke would reset the
// user's own selection mid-filter.
function fillProviders(providers) {
  if (providersLoaded || providers.length === 0) return;
  providersLoaded = true;
  providerFilter.innerHTML = `<option value="">All</option>${providers
    .map((p) => `<option value="${escapeHtml(p)}">${escapeHtml(p)}</option>`)
    .join('')}`;
}

function renderCoverage(cov, shown, total) {
  const missing = Math.max(0, cov.nodesTotal - cov.nodesReporting);
  const range =
    total === 0
      ? t('No packages')
      : t('Showing {shown} of {total} packages', { shown, total: formatNumber(total) });
  const from = tn('from {reporting} of {count} node', 'from {reporting} of {count} nodes', cov.nodesTotal, { reporting: cov.nodesReporting });
  const nudge = missing > 0
    ? ` &mdash; ${missing} node${missing === 1 ? " isn't" : "s aren't"} reporting package inventory, so nothing they have is listed.`
    : '';
  coverage.innerHTML = `${range} ${from}.${nudge}`;
}

function renderCatalog(page) {
  fillProviders(page.providers || []);
  renderCoverage(page.coverage, page.items.length, page.total);

  if (page.items.length === 0) {
    results.innerHTML = `
      <vox-empty-state heading="${t('No packages match')}">
        <vox-icon slot="icon" name="search" size="lg"></vox-icon>
        ${page.coverage.nodesReporting === 0
          ? 'No node is reporting package inventory yet. Enable it from a node\'s Packages tab.'
          : 'Try a different name or provider.'}
      </vox-empty-state>`;
    return;
  }

  const rows = page.items
    .map((p) => {
      // A package at several versions is the interesting case, so the
      // count leads and the versions themselves are the detail.
      const versions = p.versions.length > 3
        ? `${p.versions.length} versions`
        : escapeHtml(p.versions.join(', ')) || '—';
      return `
        <tr>
          <th scope="row"><a href="#" data-package="${escapeHtml(p.packageName)}">${escapeHtml(p.packageName)}</a></th>
          <td>${escapeHtml(p.providers.join(', ')) || '—'}</td>
          <td title="${escapeHtml(p.versions.join(', '))}">${versions}</td>
          <td>${p.nodeCount}</td>
        </tr>`;
    })
    .join('');

  results.innerHTML = `
    <div class="vox-table-wrap">
      <table class="vox-table vox-table--striped">
        <thead><tr><th scope="col">${t('Package')}</th><th scope="col">${t('Providers')}</th><th scope="col">${t('Versions')}</th><th scope="col">${t('Nodes')}</th></tr></thead>
        <tbody>${rows}</tbody>
      </table>
    </div>
    ${paginationHTML(page.page, page.pageSize, page.total)}`;

  bindPagination(results, (target) => {
    currentPage = target;
    loadCatalog();
  });
  results.querySelectorAll('a[data-package]').forEach((a) => {
    a.addEventListener('click', (ev) => {
      ev.preventDefault();
      showPackageNodes(a.dataset.package);
    });
  });
}

async function loadCatalog() {
  searchError.innerHTML = '';
  const params = new URLSearchParams({ page: currentPage, pageSize: PAGE_SIZE });
  if (nameFilter.value.trim()) params.set('name', nameFilter.value.trim());
  if (versionFilter.value.trim()) params.set('version', versionFilter.value.trim());
  if (providerFilter.value) params.set('provider', providerFilter.value);
  if (groupFilter.value) params.set('group', groupFilter.value);

  try {
    const page = await withLoading(results, () => fetchJSON(`/api/v1/packages/catalog?${params.toString()}`));
    // Filtering down can leave the current page past the end.
    if (page.items.length === 0 && page.page > 1 && page.total > 0) {
      currentPage = 1;
      return loadCatalog();
    }
    renderCatalog(page);
  } catch (err) {
    coverage.innerHTML = '';
    results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

// Drilling into one package: which nodes have it, and at what version.
async function showPackageNodes(name) {
  coverage.innerHTML = '';
  results.innerHTML = '';
  try {
    const matches = await fetchJSON(`/api/v1/packages?name=${encodeURIComponent(name)}`);
    const back = `<p class="vox-m-bottom-md"><a href="#" id="back-to-packages">&larr; All packages</a></p>`;

    if (matches.length === 0) {
      results.innerHTML = `${back}
        <vox-empty-state heading="${t('No matching nodes')}">
          <vox-icon slot="icon" name="search" size="lg"></vox-icon>
          No node reports having ${escapeHtml(name)} installed.
        </vox-empty-state>`;
    } else {
      const rows = matches
        .map((m) => `
          <tr>
            <td>${nodeLink(m.certname)}</td>
            <td>${escapeHtml(m.version)}</td>
            <td>${escapeHtml(m.provider)}</td>
          </tr>`)
        .join('');
      results.innerHTML = `${back}
        <h2 class="vox-m-bottom-md">${escapeHtml(name)}</h2>
        <div class="vox-table-wrap">
          <table class="vox-table vox-table--striped">
            <thead><tr><th scope="col">${t('Node')}</th><th scope="col">${t('Version')}</th><th scope="col">${t('Provider')}</th></tr></thead>
            <tbody>${rows}</tbody>
          </table>
        </div>`;
    }
    results.querySelector('#back-to-packages')?.addEventListener('click', (ev) => {
      ev.preventDefault();
      loadCatalog();
    });
  } catch (err) {
    results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

// The group filter needs the classifier, which is a separate permission
// from the one this page requires - so it only appears for someone who
// can already read groups. The API enforces the same thing; this just
// avoids offering a control that would 403.
async function loadGroups() {
  if (!hasPermission('classifier:read')) return;
  try {
    const page = await fetchJSON('/api/v1/groups?pageSize=100');
    if (page.items.length === 0) return;
    groupFilter.innerHTML = `<option value="">All</option>${page.items
      .map((g) => `<option value="${g.id}">${escapeHtml(g.name)}</option>`)
      .join('')}`;
    groupFilter.style.display = '';
  } catch {
    // A failure here just leaves the filter hidden - the catalogue
    // itself is unaffected.
  }
}

// Typing filters as you go, but not on every keystroke - each one is a
// round trip to openvoxdb.
let debounce;
function debouncedReload() {
  clearTimeout(debounce);
  debounce = setTimeout(() => {
    currentPage = 1;
    loadCatalog();
  }, 300);
}
for (const el of [nameFilter, versionFilter]) {
  el.addEventListener('input', debouncedReload);
  el.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      clearTimeout(debounce);
      currentPage = 1;
      loadCatalog();
    }
  });
}
for (const el of [providerFilter, groupFilter]) {
  el.addEventListener('change', () => {
    currentPage = 1;
    loadCatalog();
  });
}

loadGroups();
loadCatalog();
