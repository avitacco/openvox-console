import { fetchJSON, sendJSON, escapeHtml, hasPermission, statusVariant, qs, severityBadge, coverageReasonText, closeReasonText } from './app.js';

const certname = qs('name');
const factsEl = document.getElementById('facts');
const packagesEl = document.getElementById('packages');
const vulnerabilitiesSection = document.getElementById('vulnerabilities-section');
const vulnerabilitiesCoverageEl = document.getElementById('vulnerabilities-coverage');
const vulnerabilitiesEl = document.getElementById('vulnerabilities');
const vulnerabilitiesClosedSwitch = document.getElementById('vulnerabilities-closed-switch');
const reportsEl = document.getElementById('reports');
const statusFilter = document.getElementById('status-filter');
const runNowButton = document.getElementById('run-now');
const runNowError = document.getElementById('run-now-error');
const packageInventoryToggleEl = document.getElementById('package-inventory-toggle');
const packageInventorySwitch = document.getElementById('package-inventory-switch');
const packageInventoryStatusEl = document.getElementById('package-inventory-status');

document.getElementById('node-heading').textContent = certname;
document.getElementById('node-name').textContent = certname;

if (hasPermission('orchestrator:run')) {
  runNowButton.style.display = '';
}

runNowButton.addEventListener('click', async () => {
  runNowError.innerHTML = '';
  runNowButton.disabled = true;
  try {
    const job = await sendJSON('/api/v1/orchestrator/runs', 'POST', { targets: [certname] });
    window.location.href = `/job.html?id=${job.id}`;
  } catch (err) {
    runNowError.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  } finally {
    runNowButton.disabled = false;
  }
});

// Structured fact values (hashes, arrays) render as a real JSON code
// block - matches voxblocks' own documented facts-table example
// (docs/components/code-block: no-header/no-border for a table-cell
// block). A plain string fact stays plain text - stringifying it as
// "JSON" would just wrap it in pointless quotes.
function renderFactValue(value) {
  if (value === undefined || value === null) return '';
  if (typeof value === 'string') return escapeHtml(value);
  const json = JSON.stringify(value, null, 2);
  return `<vox-code-block language="json" no-header no-border>${escapeHtml(json)}</vox-code-block>`;
}

async function loadFacts() {
  try {
    const detail = await fetchJSON(`/api/v1/nodes/${encodeURIComponent(certname)}`);
    const names = Object.keys(detail.facts).sort();
    if (names.length === 0) {
      factsEl.innerHTML = `<vox-empty-state heading="No facts reported"></vox-empty-state>`;
      return;
    }
    const rows = names
      .map((name) => `
        <tr>
          <th scope="row">${escapeHtml(name)}</th>
          <td>${renderFactValue(detail.facts[name])}</td>
        </tr>`)
      .join('');
    factsEl.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead><tr><th scope="col">Fact</th><th scope="col">Value</th></tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
  } catch (err) {
    factsEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

async function loadPackages() {
  try {
    const packages = await fetchJSON(`/api/v1/nodes/${encodeURIComponent(certname)}/packages`);

    if (packages.length === 0) {
      packagesEl.innerHTML = `<vox-empty-state heading="No package data reported"></vox-empty-state>`;
      return;
    }

    // A source line only when the source package actually differs - an
    // apt package that is its own source would just repeat its row.
    const sourceLine = (p) => (p.sourcePackage && (p.sourcePackage !== p.packageName || p.sourceVersion !== p.version)
      ? `<div class="vox-ts-sm">from ${escapeHtml(p.sourcePackage)} ${escapeHtml(p.sourceVersion)}</div>`
      : '');
    const rows = packages
      .map((p) => `
        <tr>
          <th scope="row">${escapeHtml(p.packageName)}${sourceLine(p)}</th>
          <td>${escapeHtml(p.version)}</td>
          <td>${escapeHtml(p.provider)}</td>
        </tr>`)
      .join('');
    packagesEl.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead><tr><th scope="col">Package</th><th scope="col">Version</th><th scope="col">Provider</th></tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
  } catch (err) {
    packagesEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

async function loadReports() {
  try {
    const params = new URLSearchParams();
    if (statusFilter.value) params.set('status', statusFilter.value);
    const qsStr = params.toString();
    const url = `/api/v1/nodes/${encodeURIComponent(certname)}/reports${qsStr ? `?${qsStr}` : ''}`;
    const reports = await fetchJSON(url);

    if (reports.length === 0) {
      reportsEl.innerHTML = `
        <vox-empty-state heading="No reports yet">
          <vox-icon slot="icon" name="report" size="lg"></vox-icon>
          Reports appear here after this node's next Puppet run.
        </vox-empty-state>`;
      return;
    }

    const rows = reports
      .map((r) => `
        <tr>
          <td><a href="/report.html?id=${encodeURIComponent(r.hash)}&node=${encodeURIComponent(certname)}">${new Date(r.startTime).toLocaleString()}</a></td>
          <td><vox-badge variant="${statusVariant(r.status)}">${escapeHtml(r.status)}</vox-badge></td>
          <td>${new Date(r.endTime).toLocaleString()}</td>
        </tr>`)
      .join('');
    reportsEl.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead><tr><th scope="col">Started</th><th scope="col">Status</th><th scope="col">Ended</th></tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
  } catch (err) {
    reportsEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

// loadPackageInventoryToggle reflects this node's current
// package-inventory reporting state and disables the control when the
// node isn't connected - a toggle requires a live dispatch (see
// design.md in add-package-inventory-toggle), so there's nothing to
// offer an operator for a disconnected node. Reuses the same
// connectivity data source the nodes list page already shows, just not
// previously fetched on this page.
async function loadPackageInventoryToggle() {
  if (!hasPermission('orchestrator:run')) return;
  packageInventoryToggleEl.style.display = '';

  let connected = false;
  try {
    const data = await fetchJSON('/api/v1/node-connectivity');
    connected = !!data.nodes.find((n) => n.certname === certname)?.connected;
  } catch {
    connected = false;
  }
  if (!connected) {
    packageInventorySwitch.disabled = true;
    packageInventoryStatusEl.innerHTML = `<vox-alert variant="neutral">Node is offline - package-inventory reporting can't be changed until it reconnects.</vox-alert>`;
    return;
  }

  try {
    const status = await fetchJSON(`/api/v1/nodes/${encodeURIComponent(certname)}/packages/reporting`);
    packageInventorySwitch.checked = status.enabled;
  } catch (err) {
    packageInventorySwitch.disabled = true;
    packageInventoryStatusEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

packageInventorySwitch.addEventListener('change', async () => {
  const desired = packageInventorySwitch.checked;
  packageInventoryStatusEl.innerHTML = '';
  packageInventorySwitch.disabled = true;
  try {
    const result = await sendJSON(`/api/v1/nodes/${encodeURIComponent(certname)}/packages/reporting`, 'PUT', { enabled: desired });
    packageInventorySwitch.checked = result.enabled;
    if (result.output) {
      // A real Puppet run happened (see design.md's Ran distinction) -
      // surface its outcome rather than just the resulting boolean,
      // and refresh the packages table since the run may have changed
      // what's reported.
      const succeeded = result.output.exitCode === 0 || result.output.exitCode === 2;
      packageInventoryStatusEl.innerHTML = `<vox-alert variant="${succeeded ? 'tip' : 'danger'}">Puppet run completed (exit code ${result.output.exitCode}).</vox-alert>`;
      loadPackages();
    }
  } catch (err) {
    packageInventorySwitch.checked = !desired; // revert the optimistic toggle on failure
    packageInventoryStatusEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  } finally {
    packageInventorySwitch.disabled = false;
  }
});

statusFilter.addEventListener('change', loadReports);

// Vulnerabilities section (add-vulnerability-tracking): only shown with
// vulnerabilities:read, and never presents an unassessed node as clean.
async function loadVulnerabilities() {
  vulnerabilitiesSection.style.display = '';
  const includeClosed = vulnerabilitiesClosedSwitch.checked;
  try {
    const data = await fetchJSON(`/api/v1/nodes/${encodeURIComponent(certname)}/vulnerabilities${includeClosed ? '?includeClosed=true' : ''}`);
    const cov = data.coverage;
    const notAssessedBy = cov.providers.filter((p) => !p.assessed);
    const reasons = notAssessedBy
      .map((p) => `<li>${escapeHtml(p.provider.name)}: ${escapeHtml(coverageReasonText(p.reason))}</li>`)
      .join('');
    if (cov.noProvidersEnabled) {
      vulnerabilitiesCoverageEl.innerHTML = `<vox-alert variant="neutral">No vulnerability provider is enabled, so this node hasn't been assessed.</vox-alert>`;
    } else if (!cov.assessed) {
      vulnerabilitiesCoverageEl.innerHTML = `<vox-alert variant="warning">No provider has assessed this node, so it isn't known to be free of vulnerabilities.<ul>${reasons}</ul></vox-alert>`;
    } else {
      const assessedBy = cov.providers.filter((p) => p.assessed).map((p) => escapeHtml(p.provider.name)).join(', ');
      vulnerabilitiesCoverageEl.innerHTML = `<p class="vox-ts-sm">Assessed by ${assessedBy}.</p>${notAssessedBy.length ? `<p class="vox-ts-sm">Not assessed by:</p><ul class="vox-ts-sm">${reasons}</ul>` : ''}`;
    }

    if (data.findings.length === 0) {
      vulnerabilitiesEl.innerHTML = cov.assessed
        ? `<vox-empty-state heading="No open vulnerabilities"><vox-icon slot="icon" name="shield" size="lg"></vox-icon>No enabled provider reports an open finding on this node.</vox-empty-state>`
        : '';
      return;
    }
    const rows = data.findings
      .map((f) => `
        <tr>
          <th scope="row"><a href="/vulnerability.html?id=${encodeURIComponent(f.vulnId)}">${escapeHtml(f.vulnId)}</a></th>
          <td>${f.status === 'open' ? severityBadge(f.severity) : '—'}</td>
          <td>${f.status === 'open' ? 'Open' : `Closed (${escapeHtml(closeReasonText(f.closeReason))})`}</td>
          <td>${f.status !== 'open' ? '—' : f.fixAvailable ? 'Available' : 'None released'}</td>
          <td>${escapeHtml(f.packages.join(', ')) || '—'}</td>
          <td>${escapeHtml(new Date(f.firstSeen).toLocaleString())}</td>
          <td>${escapeHtml(f.providers.map((p) => p.name).join(', ')) || '—'}</td>
        </tr>`)
      .join('');
    vulnerabilitiesEl.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead><tr><th scope="col">Vulnerability</th><th scope="col">Severity</th><th scope="col">Status</th><th scope="col">Fix</th><th scope="col">Packages</th><th scope="col">First seen</th><th scope="col">Providers</th></tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
  } catch (err) {
    vulnerabilitiesEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}

vulnerabilitiesClosedSwitch.addEventListener('change', loadVulnerabilities);

if (!certname) {
  factsEl.innerHTML = `<vox-alert variant="danger">No node specified.</vox-alert>`;
} else {
  loadFacts();
  loadPackages();
  loadReports();
  loadPackageInventoryToggle();
  if (hasPermission('vulnerabilities:read')) {
    loadVulnerabilities();
  }
}
