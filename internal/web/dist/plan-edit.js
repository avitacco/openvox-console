// The plan editor: a name, a description and an ordered list of steps,
// each a Puppet run or a task (see specs/plans' "Console UI for saved
// plans").
//
// A step's options are set with the same controls the Run Puppet and
// Run task dialogs use. Saving is checked by the console, step by step,
// as running each one would be; a refusal is shown on the step it
// concerns and nothing typed is lost.

import { fetchJSON, sendJSON, escapeHtml, requirePermission, qs, t } from './app.js';
import { createRunOptions } from './run-puppet.js';
import { createTaskForm } from './task-form.js';
import { stepLabel } from './plan-common.js';

const MAX_STEPS = 20;

if (requirePermission('orchestrator:run')) {
  const id = qs('id');
  const formError = document.getElementById('form-error');
  const nameEl = document.getElementById('name');
  const descriptionEl = document.getElementById('description');
  const stepsEl = document.getElementById('steps');
  const saveButton = document.getElementById('save');
  const addRun = document.getElementById('add-run');
  const addTask = document.getElementById('add-task');

  // The steps in order: { kind, el, runOptions | taskForm }.
  const steps = [];

  // Renumbers the cards and sets which of the move buttons can be used.
  function refresh() {
    steps.forEach((step, i) => {
      const title = step.kind === 'task' ? t('Run a task') : t('Run Puppet');
      step.el.querySelector('[data-step-title]').textContent = `${stepLabel(i)}: ${title}`;
      step.el.querySelector('[data-move="up"]').disabled = i === 0;
      step.el.querySelector('[data-move="down"]').disabled = i === steps.length - 1;
    });
    document.getElementById('no-steps').style.display = steps.length === 0 ? '' : 'none';
    const full = steps.length >= MAX_STEPS;
    addRun.disabled = full;
    addTask.disabled = full;
    document.getElementById('steps-full').style.display = full ? '' : 'none';
  }

  // Moves a step by delta places. The card itself is moved, not
  // rebuilt, so what has been typed in it stays.
  function move(step, delta) {
    const from = steps.indexOf(step);
    const to = from + delta;
    if (to < 0 || to >= steps.length) return;
    steps.splice(from, 1);
    steps.splice(to, 0, step);
    const next = steps[to + 1];
    stepsEl.insertBefore(step.el, next ? next.el : null);
    refresh();
    step.el.querySelector(`[data-move="${delta < 0 ? 'up' : 'down'}"]`).focus();
  }

  function addStep(kind, initial = null) {
    const el = document.createElement('li');
    el.className = 'plan-step plan-step--editing';
    el.innerHTML = `
      <div class="plan-step-bar">
        <h3 class="plan-step-title" data-step-title></h3>
        <div class="plan-step-buttons">
          <vox-button size="sm" variant="alt" data-move="up">${escapeHtml(t('Move up'))}</vox-button>
          <vox-button size="sm" variant="alt" data-move="down">${escapeHtml(t('Move down'))}</vox-button>
          <vox-button size="sm" variant="danger" data-remove>${escapeHtml(t('Remove'))}</vox-button>
        </div>
      </div>
      <div data-step-body></div>
      <div data-step-error class="vox-m-top-md"></div>`;
    stepsEl.appendChild(el);
    const body = el.querySelector('[data-step-body]');
    const errorEl = el.querySelector('[data-step-error]');
    const step = { kind, el, errorEl };
    if (kind === 'task') {
      step.taskForm = createTaskForm(body, errorEl, {
        withoutSensitive: true,
        initial: initial ? { task: initial.taskName, environment: initial.environment, params: initial.params, timeoutSeconds: initial.timeoutSeconds } : null,
      });
      step.taskForm.load();
    } else {
      step.runOptions = createRunOptions(body, (initial && initial.options) || {});
    }
    el.querySelector('[data-move="up"]').addEventListener('click', () => move(step, -1));
    el.querySelector('[data-move="down"]').addEventListener('click', () => move(step, 1));
    el.querySelector('[data-remove]').addEventListener('click', () => {
      steps.splice(steps.indexOf(step), 1);
      el.remove();
      refresh();
    });
    steps.push(step);
    refresh();
    return step;
  }

  function clearErrors() {
    formError.innerHTML = '';
    document.querySelectorAll('[data-error-for]').forEach((el) => {
      el.innerHTML = '';
    });
    for (const step of steps) step.errorEl.innerHTML = '';
  }

  // Shows a refusal where it belongs: on the step it names, beside the
  // field it names, or above the form.
  function showRefusal(err) {
    // The console words a refusal as the rest of a sentence.
    const message = err.message.charAt(0).toUpperCase() + err.message.slice(1);
    const alert = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
    const step = err.step ? steps[err.step - 1] : null;
    let place;
    if (step) {
      if (err.fields && step.taskForm) step.taskForm.showServerProblems(err.fields);
      else step.errorEl.innerHTML = alert;
      place = step.el;
    } else {
      place = (err.field && document.querySelector(`[data-error-for="${CSS.escape(err.field)}"]`)) || formError;
      place.innerHTML = alert;
    }
    place.scrollIntoView({ block: 'center', behavior: 'smooth' });
  }

  function collect() {
    clearErrors();
    const body = { name: nameEl.value.trim(), description: descriptionEl.value.trim(), steps: [] };
    for (const step of steps) {
      if (step.kind === 'task') {
        const task = step.taskForm.collect();
        if (!task) {
          step.el.scrollIntoView({ block: 'center', behavior: 'smooth' });
          return null;
        }
        const out = { kind: 'task', taskName: task.task, environment: task.environment, params: task.params };
        if (task.timeoutSeconds) out.timeoutSeconds = task.timeoutSeconds;
        body.steps.push(out);
      } else {
        const options = step.runOptions.getOptions();
        body.steps.push(Object.keys(options).length > 0 ? { kind: 'run', options } : { kind: 'run' });
      }
    }
    if (!body.name) {
      showRefusal({ field: 'name', message: t('Give the plan a name.') });
      return null;
    }
    if (body.steps.length === 0) {
      showRefusal({ field: 'steps', message: t('Add at least one step.') });
      return null;
    }
    return body;
  }

  addRun.addEventListener('click', () => addStep('run').el.scrollIntoView({ block: 'center', behavior: 'smooth' }));
  addTask.addEventListener('click', () => addStep('task').el.scrollIntoView({ block: 'center', behavior: 'smooth' }));

  saveButton.addEventListener('click', async () => {
    const body = collect();
    if (!body) return;
    saveButton.disabled = true;
    try {
      const saved = id
        ? await sendJSON(`/api/v1/orchestrator/saved-plans/${encodeURIComponent(id)}`, 'PUT', body)
        : await sendJSON('/api/v1/orchestrator/saved-plans', 'POST', body);
      window.location.href = `/plan.html?id=${saved.id}`;
    } catch (err) {
      showRefusal(err);
      saveButton.disabled = false;
    }
  });

  async function load() {
    refresh();
    if (!id) return;
    document.getElementById('heading').textContent = t('Edit plan');
    document.getElementById('breadcrumb-current').textContent = t('Edit plan');
    document.getElementById('cancel').setAttribute('href', `/plan.html?id=${encodeURIComponent(id)}`);
    try {
      const plan = await fetchJSON(`/api/v1/orchestrator/saved-plans/${encodeURIComponent(id)}`);
      nameEl.value = plan.name;
      descriptionEl.value = plan.description || '';
      for (const step of plan.steps) addStep(step.kind, step);
    } catch (err) {
      document.getElementById('plan-form').innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  load();
}
