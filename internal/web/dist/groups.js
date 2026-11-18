import { fetchJSON, escapeHtml, withLoading, t, tn, treeOrder } from './app.js';

const results = document.getElementById('results');

function warningBadge(g) {
  const count = (g.warnings || []).length;
  if (count === 0) return '';
  const title = tn(
    'Overrides {count} value set by a parent with a higher priority',
    'Overrides {count} values set by a parent with a higher priority',
    count,
    { count },
  );
  return ` <vox-badge variant="warning" title="${escapeHtml(title)}">${t('Overrides priority')}</vox-badge>`;
}

// Marks a group one or more of whose classes, parameters or values do
// not match the code of its environment. The group page says which.
function classBadge(g) {
  const count = (g.classWarnings || []).length;
  if (count === 0) return '';
  const title = tn(
    '{count} class or parameter does not match the code in {environment}',
    '{count} classes or parameters do not match the code in {environment}',
    count,
    { count, environment: g.classEnvironment },
  );
  return ` <vox-badge variant="warning" title="${escapeHtml(title)}">${t('Class mismatch')}</vox-badge>`;
}

function renderGroups(groups, nodeCounts) {
  // The built-in All Nodes group always exists, so "no groups yet" means
  // nothing besides it.
  const prompt = groups.every((g) => g.isRoot)
    ? `<p class="vox-ts-sm vox-color-text-2">${t('Create a group to start classifying nodes.')}</p>`
    : '';

  const rows = treeOrder(groups)
    .map(({ group: g, depth }) => `
      <tr>
        <td>
          <span class="group-tree-name" style="padding-inline-start: ${depth * 1.5}rem">
            ${depth > 0 ? '<span class="group-tree-branch" aria-hidden="true">└</span>' : ''}
            <a href="/group.html?id=${g.id}">${escapeHtml(g.name)}</a>${warningBadge(g)}${classBadge(g)}
          </span>
        </td>
        <td>${g.isRoot ? '—' : g.priority}</td>
        <td>${escapeHtml(g.environment || '')}</td>
        <td>${g.classes.length}</td>
        <td>${nodeCounts ? (nodeCounts.get(g.id) ?? 0) : '—'}</td>
        <td>${g.pins.length}</td>
      </tr>`)
    .join('');

  results.innerHTML = `
    ${prompt}
    <div class="vox-table-wrap">
      <table class="vox-table vox-table--striped">
        <thead>
          <tr><th scope="col">${t('Name')}</th><th scope="col">${t('Priority')}</th><th scope="col">${t('Environment')}</th><th scope="col">${t('Classes')}</th><th scope="col">${t('Matching nodes')}</th><th scope="col">${t('Pinned nodes')}</th></tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    </div>`;
}

async function load() {
  try {
    // Every group in one response (?all=true), not a page of them: a
    // tree cannot be drawn from part of itself.
    // Counts come from their own endpoint (one fleet-wide fact snapshot
    // matched against every group) rather than per row, and a failure
    // there shows as "-" instead of failing the whole list - the group
    // list is still useful without it.
    const [page, counts] = await Promise.all([
      withLoading(results, () => fetchJSON('/api/v1/groups?all=true')),
      fetchJSON('/api/v1/groups/node-counts').catch(() => null),
    ]);
    const nodeCounts = counts ? new Map(counts.counts.map((c) => [c.groupId, c.nodes])) : null;
    renderGroups(page.items, nodeCounts);
  } catch (err) {
    results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

load();
