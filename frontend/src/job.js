import { fetchJSON, escapeHtml, requirePermission, statusVariant, qs, withLoading } from './app.js';

if (requirePermission('orchestrator:read')) {
  const jobId = qs('id');
  const heading = document.getElementById('job-heading');
  const summaryEl = document.getElementById('summary');
  const targetsEl = document.getElementById('targets');

  function jobName(job) {
    if (job.taskName) return ` (${job.taskName})`;
    if (job.planName) return ` (${job.planName})`;
    return '';
  }

  function renderSummary(job) {
    heading.textContent = `Job #${job.id}`;
    summaryEl.innerHTML = `
      <div class="vox-display-flex vox-gap-lg vox-flex-wrap">
        <div><strong>Kind:</strong> ${escapeHtml(job.kind)}${escapeHtml(jobName(job))}</div>
        <div><strong>Status:</strong> <vox-badge variant="${statusVariant(job.status)}">${escapeHtml(job.status)}</vox-badge></div>
        <div><strong>Triggered by:</strong> ${escapeHtml(job.triggeredBy)}</div>
        <div><strong>Started:</strong> ${escapeHtml(new Date(job.startedAt).toLocaleString())}</div>
        <div><strong>Finished:</strong> ${job.finishedAt ? escapeHtml(new Date(job.finishedAt).toLocaleString()) : '—'}</div>
      </div>`;
  }

  function renderTargets(targets) {
    if (!targets || targets.length === 0) {
      targetsEl.innerHTML = `<vox-empty-state heading="No targets"></vox-empty-state>`;
      return;
    }

    const rows = targets
      .map((t) => {
        const report = t.reportHash
          ? `<a href="/report.html?id=${encodeURIComponent(t.reportHash)}&node=${encodeURIComponent(t.certname)}">View report</a>`
          : '';
        return `
        <tr>
          <td><a href="/node.html?name=${encodeURIComponent(t.certname)}">${escapeHtml(t.certname)}</a></td>
          <td><vox-badge variant="${statusVariant(t.status)}">${escapeHtml(t.status)}</vox-badge></td>
          <td>${t.exitCode === undefined || t.exitCode === null ? '' : t.exitCode}</td>
          <td>${report}</td>
          <td>${t.errorDetail ? escapeHtml(t.errorDetail) : ''}</td>
        </tr>`;
      })
      .join('');

    targetsEl.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">Node</th><th scope="col">Status</th><th scope="col">Exit code</th><th scope="col">Report</th><th scope="col">Error</th></tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
  }

  async function load() {
    try {
      const job = await withLoading(summaryEl, () => fetchJSON(`/api/v1/orchestrator/jobs/${encodeURIComponent(jobId)}`));
      renderSummary(job);
      renderTargets(job.targets);
    } catch (err) {
      summaryEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  if (!jobId) {
    summaryEl.innerHTML = `<vox-alert variant="danger">No job specified.</vox-alert>`;
  } else {
    load();
  }
}
