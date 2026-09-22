import { fetchJSON, escapeHtml, paginationHTML, bindPagination, withLoading, t } from './app.js';

const results = document.getElementById('results');

let currentPage = 1;

function renderGroups(page, nodeCounts) {
  if (page.items.length === 0) {
    results.innerHTML = `
      <vox-empty-state heading="${t('No node groups yet')}">
        <vox-icon slot="icon" name="node-group" size="lg"></vox-icon>
        Create a group to start classifying nodes.
      </vox-empty-state>`;
    return;
  }

  const rows = page.items
    .map((g) => `
      <tr>
        <td><a href="/group.html?id=${g.id}">${escapeHtml(g.name)}</a></td>
        <td>${g.priority}</td>
        <td>${escapeHtml(g.environment || '')}</td>
        <td>${g.classes.length}</td>
        <td>${nodeCounts ? (nodeCounts.get(g.id) ?? 0) : '—'}</td>
        <td>${g.pins.length}</td>
      </tr>`)
    .join('');

  results.innerHTML = `
    <div class="vox-table-wrap">
      <table class="vox-table vox-table--striped">
        <thead>
          <tr><th scope="col">${t('Name')}</th><th scope="col">${t('Priority')}</th><th scope="col">${t('Environment')}</th><th scope="col">${t('Classes')}</th><th scope="col">${t('Matching nodes')}</th><th scope="col">${t('Pinned nodes')}</th></tr>
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
    // node_groups is already ordered by priority server-side (see
    // internal/classifier.Store.ListGroups), so no client-side sort
    // needed - it'd only be able to sort the current page anyway.
    // Counts come from their own endpoint (one fleet-wide fact snapshot
    // matched against every group) rather than per row, and a failure
    // there shows as "-" instead of failing the whole list - the group
    // list is still useful without it.
    const [page, counts] = await Promise.all([
      withLoading(results, () => fetchJSON(`/api/v1/groups?page=${currentPage}`)),
      fetchJSON('/api/v1/groups/node-counts').catch(() => null),
    ]);
    if (page.items.length === 0 && page.page > 1 && page.total > 0) {
      currentPage = 1;
      return load();
    }
    const nodeCounts = counts ? new Map(counts.counts.map((c) => [c.groupId, c.nodes])) : null;
    renderGroups(page, nodeCounts);
  } catch (err) {
    results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

load();
