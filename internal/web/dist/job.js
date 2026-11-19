import { fetchJSON, escapeHtml, requirePermission, statusVariant, statusLabel, kindLabel, qs, withLoading, t, formatDateTime, hasPermission } from './app.js';
import { runOptionBadges } from './run-puppet.js';
import { patchOptionBadges, patchProgressHTML, patchTargetsHTML } from './run-patch.js';
import { codePlanTraceHTML } from './code-plans.js';
import { targetSourceHTML } from './target-picker.js';
import { stepLabel, stepActionHTML, stepStateBadge } from './plan-common.js';

if (requirePermission('orchestrator:read')) {
  const jobId = qs('id');
  const heading = document.getElementById('job-heading');
  const summaryEl = document.getElementById('summary');
  const targetsEl = document.getElementById('targets');
  const targetsHeading = document.getElementById('targets-heading');

  // How often a job that is still running is read again.
  const REFRESH_MS = 3000;

  function jobName(job) {
    if (job.taskName) return ` (${job.taskName})`;
    if (job.planName) return ` (${job.planName})`;
    return '';
  }

  // What chose the job's nodes and what started it, when it was not
  // somebody naming the nodes by hand.
  function originHTML(job) {
    const parts = [];
    const source = targetSourceHTML(job.targetSource);
    if (source) {
      const label = job.targetSource.kind === 'group' ? t('Node group:') : t('Nodes matched by:');
      parts.push(`<div><strong>${escapeHtml(label)}</strong> ${source}</div>`);
    }
    if (job.schedule) {
      const name = escapeHtml(job.schedule.name);
      const link = job.schedule.id
        ? `<a href="/schedule.html?id=${job.schedule.id}">${name}</a>`
        : `${name} <span class="vox-color-text-2">(${escapeHtml(t('since deleted'))})</span>`;
      parts.push(`<div><strong>${escapeHtml(t('Started by schedule:'))}</strong> ${link}</div>`);
    }
    if (job.agentRequest) {
      const a = job.agentRequest;
      const request = hasPermission('agent-requests:read')
        ? `<a href="/approvals.html?id=${a.requestId}">${escapeHtml(t('Request #{id}', { id: a.requestId }))}</a>`
        : escapeHtml(t('Request #{id}', { id: a.requestId }));
      parts.push(`<div><strong>${escapeHtml(t('Requested by AI agent:'))}</strong> ${escapeHtml(a.token)} (${request})</div>`);
      parts.push(`<div><strong>${escapeHtml(t('Approved by:'))}</strong> ${escapeHtml(a.approvedBy)}</div>`);
    }
    return parts.length ? `<div class="vox-display-flex vox-gap-lg vox-flex-wrap vox-m-top-md">${parts.join('')}</div>` : '';
  }

  function renderSummary(job) {
    heading.textContent = `Job #${job.id}`;
    summaryEl.innerHTML = `
      ${planHTML(job)}
      <div class="vox-display-flex vox-gap-lg vox-flex-wrap">
        <div><strong>Kind:</strong> ${escapeHtml(job.kind === 'patch' || job.kind === 'codeplan' ? kindLabel(job.kind) : job.kind)}${escapeHtml(jobName(job))} ${runOptionBadges(job.runOptions)} ${patchOptionBadges(job.patchOptions)}</div>
        <div><strong>Status:</strong> <vox-badge variant="${statusVariant(job.status)}">${escapeHtml(job.status)}</vox-badge></div>
        <div><strong>Triggered by:</strong> ${escapeHtml(job.triggeredBy)}</div>
        <div><strong>Started:</strong> ${escapeHtml(formatDateTime(job.startedAt))}</div>
        <div><strong>Finished:</strong> ${job.finishedAt ? escapeHtml(formatDateTime(job.finishedAt)) : '—'}</div>
        ${job.environment && !job.runOptions ? `<div><strong>${t('Environment:')}</strong> ${escapeHtml(job.environment)}</div>` : ''}
      </div>
      ${originHTML(job)}
      ${patchProgressHTML(job)}
      ${job.targetsHidden ? `<vox-alert variant="info" class="vox-m-top-md">${t('Only the nodes in your node groups are shown. This job also ran on other nodes, and its status covers all of them.')}</vox-alert>` : ''}
      ${job.runOptions?.environment ? `<p class="vox-ts-sm vox-color-text-2 vox-m-top-md vox-m-bottom-none">${t('The environment is a request. A node whose node groups set an environment runs in that one instead, and is marked below once its report arrives.')}</p>` : ''}
      ${job.runOptions || job.patchOptions || job.kind === 'codeplan' ? '' : paramsHTML(job.params)}`;
  }

  // A step read by itself says which plan it is part of.
  function planHTML(job) {
    if (!job.plan) return '';
    const position = t('Step {number} of {count} in the plan', { number: (job.stepOrder ?? 0) + 1, count: job.plan.steps });
    const name = job.plan.name || t('Job #{id}', { id: job.plan.id });
    return `<p class="vox-m-top-none">${escapeHtml(position)} <a href="/job.html?id=${job.plan.id}">${escapeHtml(name)}</a></p>`;
  }

  // What the agent printed, collapsed under its row. A plain run prints
  // nothing; a no-op run lists what it would have changed, and a debug
  // run its whole log.
  function outputRow(target, columns) {
    if (!target.output || target.result) return '';
    return `
        <tr>
          <td colspan="${columns}">
            <vox-disclosure summary="${escapeHtml(t('Output from {node}', { node: target.certname }))}">
              <div class="job-output"><vox-code-block language="text" no-header>${escapeHtml(target.output)}</vox-code-block></div>
            </vox-disclosure>
          </td>
        </tr>`;
  }

  // Marks a node whose run used a different environment from the one
  // the job asked for - which happens whenever its node groups set one.
  function environmentMark(target, requested) {
    if (!requested || !target.reportEnvironment || target.reportEnvironment === requested) return '';
    const text = t('Ran in {used}, not {requested}', { used: target.reportEnvironment, requested });
    const title = t("This node's node groups set its environment, which takes precedence over the one a run asks for.");
    return ` <vox-badge variant="warning" title="${escapeHtml(title)}">${escapeHtml(text)}</vox-badge>`;
  }

  // A task's parameters as stored: sensitive values are already
  // redacted by the server, which never keeps them.
  function paramsHTML(params) {
    if (!params || Object.keys(params).length === 0) return '';
    return `
      <vox-disclosure class="vox-m-top-md" summary="${escapeHtml(t('Parameters'))}">
        <vox-code-block language="json" no-header>${escapeHtml(JSON.stringify(params, null, 2))}</vox-code-block>
      </vox-disclosure>`;
  }

  // A task target's structured result, collapsed under its row.
  function resultRow(target, columns) {
    if (!target.result) return '';
    return `
        <tr>
          <td colspan="${columns}">
            <vox-disclosure summary="${escapeHtml(t('Result from {node}', { node: target.certname }))}">
              <vox-code-block language="json" no-header>${escapeHtml(JSON.stringify(target.result, null, 2))}</vox-code-block>
            </vox-disclosure>
          </td>
        </tr>`;
  }

  function targetsHTML(targets, requestedEnvironment) {
    if (!targets || targets.length === 0) {
      return `<vox-empty-state heading="${t('No targets')}"></vox-empty-state>`;
    }

    const rows = targets
      .map((target) => {
        const report = target.reportHash
          ? `<a href="/report.html?id=${encodeURIComponent(target.reportHash)}&node=${encodeURIComponent(target.certname)}">${t('View report')}</a>`
          : '';
        return `
        <tr>
          <td><a href="/node.html?name=${encodeURIComponent(target.certname)}">${escapeHtml(target.certname)}</a></td>
          <td><vox-badge variant="${statusVariant(target.status)}">${escapeHtml(statusLabel(target.status))}</vox-badge></td>
          <td>${target.exitCode === undefined || target.exitCode === null ? '' : target.exitCode}</td>
          <td>${report}${environmentMark(target, requestedEnvironment)}</td>
          <td>${target.errorDetail ? escapeHtml(target.errorDetail) : ''}</td>
        </tr>${resultRow(target, 5)}${outputRow(target, 5)}`;
      })
      .join('');

    return `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">${t('Node')}</th><th scope="col">${t('Status')}</th><th scope="col">${t('Exit code')}</th><th scope="col">${t('Report')}</th><th scope="col">${t('Error')}</th></tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
  }

  // A step that has not been reached, or was closed because an earlier
  // one failed, has nothing to show for its nodes yet - or ever.
  function stepBodyHTML(step) {
    if (step.state === 'waiting') {
      return `<p class="vox-ts-sm vox-color-text-2 vox-m-y-none">${escapeHtml(t('Starts when the step before it has finished on every node.'))}</p>`;
    }
    if (step.state === 'not_run') {
      return `<p class="vox-ts-sm vox-color-text-2 vox-m-y-none">${escapeHtml(t('Not run, because an earlier step failed. Nothing was sent to any node for this step.'))}</p>`;
    }
    return targetsHTML(step.targets, step.runOptions?.environment);
  }

  function stepTimes(step) {
    if (!step.startedAt) return '';
    const started = t('Started {when}', { when: formatDateTime(step.startedAt) });
    const finished = step.finishedAt ? ` · ${t('Finished {when}', { when: formatDateTime(step.finishedAt) })}` : '';
    return `<span class="plan-step-times">${escapeHtml(started + finished)}</span>`;
  }

  // A plan's steps, in order, each with its state and its nodes'
  // results. A step links to its own job page.
  function stepsHTML(steps) {
    if (!steps || steps.length === 0) {
      return `<vox-empty-state heading="${t('No steps to show')}"></vox-empty-state>`;
    }
    return `<ol class="plan-step-list">${steps
      .map(
        (step) => `
      <li class="plan-step">
        <div class="plan-step-heading">
          <a class="plan-step-number" href="/job.html?id=${step.id}">${escapeHtml(stepLabel(step.stepOrder))}</a>
          ${stepActionHTML(step)} ${stepStateBadge(step.state)} ${stepTimes(step)}
        </div>
        ${step.kind === 'task' ? paramsHTML(step.params) : ''}
        ${stepBodyHTML(step)}
      </li>`
      )
      .join('')}</ol>`;
  }

  // What was last drawn, so that a refresh which found nothing new
  // leaves the page alone - an opened result or output stays open.
  let drawn = '';

  function render(job) {
    const snapshot = JSON.stringify(job);
    if (snapshot === drawn) return;
    drawn = snapshot;
    renderSummary(job);
    if (job.kind === 'codeplan') {
      targetsHeading.textContent = t('Steps');
      targetsEl.innerHTML = codePlanTraceHTML(job);
    } else if (job.kind === 'plan') {
      targetsHeading.textContent = t('Steps');
      targetsEl.innerHTML = stepsHTML(job.steps);
    } else {
      targetsEl.innerHTML = job.kind === 'patch' ? patchTargetsHTML(job) : targetsHTML(job.targets, job.runOptions?.environment);
    }
  }

  async function load() {
    try {
      const job = await withLoading(summaryEl, () => fetchJSON(`/api/v1/orchestrator/jobs/${encodeURIComponent(jobId)}`));
      render(job);
      if (job.status === 'running') setTimeout(refresh, REFRESH_MS);
    } catch (err) {
      summaryEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  // A running job is read again until it has finished, so a plan's
  // steps, and any job's nodes, move on without a reload. A failed read
  // is tried again rather than replacing what is on the page.
  async function refresh() {
    let running = true;
    try {
      const job = await fetchJSON(`/api/v1/orchestrator/jobs/${encodeURIComponent(jobId)}`);
      render(job);
      running = job.status === 'running';
    } catch {
      // Keep what is shown; the next read may succeed.
    }
    if (running) setTimeout(refresh, REFRESH_MS);
  }

  if (!jobId) {
    summaryEl.innerHTML = `<vox-alert variant="danger">${t('No job specified.')}</vox-alert>`;
  } else {
    load();
  }
}
