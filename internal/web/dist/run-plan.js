// The "Run plan" dialog: which nodes, and the sensitive values the
// plan's steps need (see specs/plans' "Console UI for running a plan").
//
// Running a saved plan is an ordinary plan request carrying the plan's
// steps: what is sent is exactly what the dialog shows.

import { sendJSON, escapeHtml, t } from './app.js';
import { createTargetPicker } from './target-picker.js';
import { createPlanSteps } from './plan-common.js';
import { scheduleInstead } from './schedule-draft.js';

/**
 * Opens the dialog for running a saved plan.
 *
 * @param {{name: string, steps: Array<object>}} plan a saved plan as
 *   read, with each step's needs.
 */
export function openRunPlanDialog(plan) {
  const dialog = document.createElement('vox-dialog');
  dialog.heading = t('Run plan: {name}', { name: plan.name });
  dialog.setAttribute('light-dismiss', '');
  dialog.innerHTML = `
    <div class="run-task-scroll">
      <div data-targets class="vox-m-bottom-lg"></div>
      <h3 class="form-section-heading">${escapeHtml(t('Steps'))}</h3>
      <p class="vox-ts-sm vox-color-text-2 vox-m-top-none">${escapeHtml(
        t('Each step runs on every node, in this order. If a step fails on any node, the plan stops and the steps after it are not run.'),
      )}</p>
      <div data-steps></div>
      <div data-error class="vox-m-top-md"></div>
    </div>
    <div slot="footer" class="dialog-footer-split">
      <vox-button variant="alt" data-action="schedule" class="dialog-footer-start">${escapeHtml(t('Schedule instead'))}</vox-button>
      <vox-button variant="alt" data-action="cancel">${escapeHtml(t('Cancel'))}</vox-button>
      <vox-button variant="brand" data-action="run">${escapeHtml(t('Run plan'))}</vox-button>
    </div>`;
  const errorEl = dialog.querySelector('[data-error]');
  const runButton = dialog.querySelector('[data-action="run"]');
  dialog.addEventListener('vox-close', () => dialog.remove());
  dialog.querySelector('[data-action="cancel"]').addEventListener('click', () => dialog.close());
  document.body.appendChild(dialog);
  dialog.show();

  const picker = createTargetPicker(dialog.querySelector('[data-targets]'));
  const steps = createPlanSteps(dialog.querySelector('[data-steps]'), plan.steps);
  if (steps.hasProblem) {
    runButton.disabled = true;
    // Editing the plan does not help someone whose roles do not allow
    // one of its tasks, so they are told that instead.
    const message = steps.hasNotPermitted
      ? t('You cannot run this plan: a step uses a task your roles do not allow.')
      : t('This plan has a step that can no longer run. Edit the plan before running it.');
    errorEl.innerHTML = `<vox-alert variant="warning">${escapeHtml(message)}</vox-alert>`;
  }

  function showError(err) {
    // The console words a refusal as the rest of a sentence.
    const message = err.message.charAt(0).toUpperCase() + err.message.slice(1);
    if (err.step && steps.showStepError(err.step, message)) return;
    errorEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
  }

  dialog.querySelector('[data-action="schedule"]').addEventListener('click', () => {
    // The plan's steps go along as they are, needs included, with any
    // value already typed: the schedule form asks for the rest.
    const typed = steps.getSteps();
    const carried = plan.steps.map((step, i) => ({ ...step, params: typed ? typed[i].params : step.params }));
    scheduleInstead({ kind: 'plan', name: plan.name, planName: plan.name, steps: carried, ...picker.getTarget() });
  });

  runButton.addEventListener('click', async () => {
    errorEl.innerHTML = '';
    steps.showStepError(0, '');
    const request = steps.getSteps();
    if (!request) return;
    if (!(await picker.confirm())) return;
    runButton.disabled = true;
    try {
      const job = await sendJSON('/api/v1/orchestrator/plans', 'POST', { name: plan.name, steps: request, ...picker.getTarget() });
      window.location.href = `/job.html?id=${job.id}`;
    } catch (err) {
      showError(err);
      runButton.disabled = false;
    }
  });
}
