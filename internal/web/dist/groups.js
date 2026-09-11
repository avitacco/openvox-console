import { fetchJSON, escapeHtml, paginationHTML, bindPagination } from './app.js';

const results = document.getElementById('results');

let currentPage = 1;

function renderGroups(page) {
  if (page.items.length === 0) {
    results.innerHTML = `
      <vox-empty-state heading="No node groups yet">
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
        <td>${g.pins.length}</td>
      </tr>`)
    .join('');

  results.innerHTML = `
    <div class="vox-table-wrap">
      <table class="vox-table vox-table--striped">
        <thead>
          <tr><th scope="col">Name</th><th scope="col">Priority</th><th scope="col">Environment</th><th scope="col">Classes</th><th scope="col">Pinned nodes</th></tr>
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
    const page = await fetchJSON(`/api/v1/groups?page=${currentPage}`);
    if (page.items.length === 0 && page.page > 1 && page.total > 0) {
      currentPage = 1;
      return load();
    }
    renderGroups(page);
  } catch (err) {
    results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

load();
