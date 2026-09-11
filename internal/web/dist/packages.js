import { fetchJSON, escapeHtml } from './app.js';

const nameEl = document.getElementById('name-search');
const versionEl = document.getElementById('version-search');
const searchButton = document.getElementById('search-button');
const searchError = document.getElementById('search-error');
const results = document.getElementById('results');

function nodeLink(certname) {
  return `<a href="/node.html?name=${encodeURIComponent(certname)}">${escapeHtml(certname)}</a>`;
}

async function search() {
  searchError.innerHTML = '';
  const name = nameEl.value.trim();
  if (!name) {
    searchError.innerHTML = `<vox-alert variant="danger">Enter a package name to search.</vox-alert>`;
    return;
  }

  const params = new URLSearchParams({ name });
  const version = versionEl.value.trim();
  if (version) params.set('version', version);

  try {
    const matches = await fetchJSON(`/api/v1/packages?${params.toString()}`);

    if (matches.length === 0) {
      results.innerHTML = `
        <vox-empty-state heading="No matching nodes">
          <vox-icon slot="icon" name="search" size="lg"></vox-icon>
          No node reports having this package installed.
        </vox-empty-state>`;
      return;
    }

    const rows = matches
      .map((m) => `
        <tr>
          <td>${nodeLink(m.certname)}</td>
          <td>${escapeHtml(m.version)}</td>
          <td>${escapeHtml(m.provider)}</td>
        </tr>`)
      .join('');
    results.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead><tr><th scope="col">Node</th><th scope="col">Version</th><th scope="col">Provider</th></tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
  } catch (err) {
    results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

searchButton.addEventListener('click', search);
for (const el of [nameEl, versionEl]) {
  el.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') search();
  });
}
