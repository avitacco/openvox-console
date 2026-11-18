// The "Run task" dialog, shared by the Jobs page, the node page, the
// Nodes page's selection and a group's matching nodes (see
// specs/orchestrator's "Console UI for running a task"). The form
// itself - environment, task, parameters, timeout - is task-form.js.

import { sendJSON, escapeHtml, t, tn } from './app.js';
import { createTaskForm } from './task-form.js';
import { createTargetPicker } from './target-picker.js';
import { scheduleInstead } from './schedule-draft.js';

/**
 * Opens the dialog for running a task.
 *
 * Given targets, the dialog says how many. Given none - opened from the
 * Jobs page - it offers the target chooser: a list of nodes, a query,
 * or a node group.
 *
 * @param {Object} [options]
 * @param {string[]} [options.targets] certnames to run against.
 * @param {number} [options.group] the node group the targets are the
 *   matching nodes of, when there is one: scheduling from here then
 *   targets the group rather than the nodes it holds today.
 */
export function openRunTaskDialog({ targets, group } = {}) {
  const dialog = document.createElement('vox-dialog');
  dialog.heading = t('Run a task');
  dialog.setAttribute('light-dismiss', '');
  let intro = '<div data-targets class="vox-m-bottom-lg"></div>';
  if (targets) {
    const targetText = targets.length === 1
      ? t('On {node}.', { node: targets[0] })
      : tn('On {count} node.', 'On {count} nodes.', targets.length);
    intro = `<p class="vox-ts-sm">${escapeHtml(targetText)}</p>`;
  }
  dialog.innerHTML = `
    <div class="run-task-scroll">
      ${intro}
      <div data-body></div>
      <div data-error></div>
    </div>
    <div slot="footer" class="dialog-footer-split">
      <vox-button variant="alt" data-action="schedule" class="dialog-footer-start">${escapeHtml(t('Schedule instead'))}</vox-button>
      <vox-button variant="alt" data-action="cancel">${escapeHtml(t('Cancel'))}</vox-button>
      <vox-button variant="brand" data-action="run" disabled>${escapeHtml(t('Run task'))}</vox-button>
    </div>`;
  const errorEl = dialog.querySelector('[data-error]');
  const runButton = dialog.querySelector('[data-action="run"]');
  const scheduleButton = dialog.querySelector('[data-action="schedule"]');
  dialog.addEventListener('vox-close', () => dialog.remove());
  dialog.querySelector('[data-action="cancel"]').addEventListener('click', () => dialog.close());
  document.body.appendChild(dialog);
  dialog.show();

  const picker = targets ? null : createTargetPicker(dialog.querySelector('[data-targets]'));
  const form = createTaskForm(dialog.querySelector('[data-body]'), errorEl, {
    onReady: (ready) => {
      runButton.disabled = !ready;
    },
    onUnavailable: () => {
      runButton.remove();
      scheduleButton.remove();
    },
  });

  function target() {
    return picker ? picker.getTarget() : { targets };
  }

  scheduleButton.addEventListener('click', () => {
    // Whatever has been filled in so far goes along; the schedule form
    // checks it again.
    const task = form.isReady() ? form.collect() : {};
    if (task === null) return;
    scheduleInstead({ kind: 'task', ...task, ...(group !== undefined ? { group } : target()) });
  });

  runButton.addEventListener('click', async () => {
    const task = form.collect();
    if (!task) return;
    if (picker && !(await picker.confirm())) return;

    runButton.disabled = true;
    try {
      const job = await sendJSON('/api/v1/orchestrator/tasks', 'POST', { ...target(), ...task });
      window.location.href = `/job.html?id=${job.id}`;
    } catch (err) {
      if (err.fields) {
        form.showServerProblems(err.fields);
      } else {
        form.showError(err.message);
      }
      runButton.disabled = false;
    }
  });

  form.load();
}
