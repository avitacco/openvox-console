// One saved plan: its steps, read-only, with Run, Edit and Delete for
// someone who may run jobs.

import { fetchJSON, escapeHtml, requirePermission, hasPermission, confirmDialog, qs, withLoading, t, formatDateTime } from './app.js';
import { stepLabel, stepActionHTML, stepDetailHTML, stepProblemHTML } from './plan-common.js';
import { openRunPlanDialog } from './run-plan.js';

if (requirePermission('orchestrator:read')) {
  const id = qs('id');
  const heading = document.getElementById('heading');
  const summaryEl = document.getElementById('summary');
  const stepsEl = document.getElementById('steps');
  const actionError = document.getElementById('action-error');
  const path = `/api/v1/orchestrator/saved-plans/${encodeURIComponent(id)}`;
  let plan = null;

  function stepHTML(step, i) {
    const needs = (step.needs || [])
      .map((n) => `<code>${escapeHtml(n.param)}</code>${n.required ? '' : ` <span class="vox-color-text-2">(${escapeHtml(t('optional'))})</span>`}`)
      .join(', ');
    const detail = stepDetailHTML(step);
    return `
      <li class="plan-step">
        <div class="plan-step-heading"><span class="plan-step-number">${escapeHtml(stepLabel(i))}</span> ${stepActionHTML(step)}</div>
        ${stepProblemHTML(step)}
        ${detail}
        ${needs ? `<p class="vox-ts-sm vox-m-y-none">${escapeHtml(t('Asked for when the plan is run:'))} ${needs}</p>` : ''}
      </li>`;
  }

  function render() {
    heading.textContent = plan.name;
    document.getElementById('breadcrumb-current').textContent = plan.name;
    summaryEl.innerHTML = `
      ${plan.description ? `<p>${escapeHtml(plan.description)}</p>` : ''}
      <p class="vox-ts-sm vox-color-text-2 vox-m-y-none">${escapeHtml(
        t('Last changed by {user} on {when}.', { user: plan.updatedBy, when: formatDateTime(plan.updatedAt) }),
      )} ${escapeHtml(t('Each step runs on every node, in this order. If a step fails on any node, the plan stops and the steps after it are not run.'))}</p>`;
    stepsEl.innerHTML = `<ol class="plan-step-list">${plan.steps.map(stepHTML).join('')}</ol>`;
    if (hasPermission('orchestrator:run')) document.getElementById('controls').style.display = '';
  }

  async function load() {
    try {
      plan = await withLoading(summaryEl, () => fetchJSON(path));
      render();
    } catch (err) {
      heading.textContent = t('Plan');
      summaryEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  document.getElementById('run').addEventListener('click', () => plan && openRunPlanDialog(plan));
  document.getElementById('edit').addEventListener('click', () => {
    window.location.href = `/plan-edit.html?id=${encodeURIComponent(id)}`;
  });
  document.getElementById('delete').addEventListener('click', async () => {
    const confirmed = await confirmDialog({
      heading: t('Delete this plan?'),
      body: `<p>${escapeHtml(t('Jobs it has started stay in the job history, and schedules made from it keep their own copy of its steps.'))}</p>`,
      confirmLabel: t('Delete'),
      danger: true,
    });
    if (!confirmed) return;
    actionError.innerHTML = '';
    try {
      await fetchJSON(path, { method: 'DELETE' });
      window.location.href = '/plans.html';
    } catch (err) {
      actionError.innerHTML = `<vox-alert variant="danger" class="vox-m-bottom-md">${escapeHtml(err.message)}</vox-alert>`;
    }
  });

  if (!id) {
    summaryEl.innerHTML = `<vox-alert variant="danger">${t('No plan specified.')}</vox-alert>`;
  } else {
    load();
  }
}
