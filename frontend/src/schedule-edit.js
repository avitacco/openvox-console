// The schedule form: a Puppet run or a task, the nodes to run it on,
// and when (see specs/scheduled-jobs' "Console UI for schedules").
//
// Saving is checked by the console as the same request made now would
// be, so the form shows the console's refusal beside the part of the
// schedule it concerns rather than second-guessing it.

import { fetchJSON, sendJSON, escapeHtml, requirePermission, getClaims, qs, t } from './app.js';
import { createRunOptions } from './run-puppet.js';
import { createTaskForm } from './task-form.js';
import { createTargetPicker, setSelectValue } from './target-picker.js';
import { takeDraft } from './schedule-draft.js';
import { timeZoneName } from './schedule-common.js';
import { createPlanSteps } from './plan-common.js';

if (requirePermission('orchestrator:run')) {
  const id = qs('id');
  const formError = document.getElementById('form-error');
  const nameEl = document.getElementById('name');
  const kindEl = document.getElementById('kind');
  const startEl = document.getElementById('start-at');
  const repeatEl = document.getElementById('repeat');
  const repeatEveryEl = document.getElementById('repeat-every');
  const repeatUnitEl = document.getElementById('repeat-unit');
  const saveButton = document.getElementById('save');

  let runOptions;
  let taskForm;
  let picker;
  // The plan the schedule will run: its name, its steps, and the part
  // of the form that shows them and asks for their sensitive values.
  let plan = null;
  const planSelect = document.getElementById('plan-select');
  const planStepsEl = document.getElementById('plan-steps');

  // A Date as the value a datetime-local input takes: local wall-clock
  // time, to the minute.
  function toLocalInput(date) {
    const pad = (n) => String(n).padStart(2, '0');
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
  }

  function showPanes() {
    const kind = kindEl.value || 'run';
    for (const name of ['run', 'task', 'plan']) {
      document.getElementById(`${name}-pane`).style.display = kind === name ? '' : 'none';
    }
  }

  function setPlan(name, steps) {
    plan = { name, steps, form: createPlanSteps(planStepsEl, steps) };
    document.getElementById('plan-copy-note').style.display = '';
  }

  // The saved plans to choose from. A schedule that already has steps
  // keeps them unless another plan is chosen.
  async function loadPlans(hasSteps) {
    const first = hasSteps ? t('Keep the steps shown below') : t('Choose a plan');
    let options = `<option value="">${escapeHtml(first)}</option>`;
    try {
      const { items } = await fetchJSON('/api/v1/orchestrator/saved-plans');
      options += items.map((p) => `<option value="${p.id}">${escapeHtml(p.name)}</option>`).join('');
      if (items.length === 0 && !hasSteps) {
        planSelect.setAttribute('note', t('There are no saved plans yet. Create one on the Plans page first.'));
      }
    } catch (err) {
      planSelect.setAttribute('note', err.message);
    }
    planSelect.innerHTML = options;
  }

  planSelect.addEventListener('change', async () => {
    document.querySelector('[data-error-for="steps"]').innerHTML = '';
    if (!planSelect.value) return;
    try {
      const chosen = await fetchJSON(`/api/v1/orchestrator/saved-plans/${encodeURIComponent(planSelect.value)}`);
      setPlan(chosen.name, chosen.steps);
    } catch (err) {
      document.querySelector('[data-error-for="steps"]').innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  });

  function showRepeat() {
    const on = repeatEl.checked;
    document.getElementById('repeat-fields').style.display = on ? '' : 'none';
    document.getElementById('repeat-note').style.display = on ? '' : 'none';
  }

  function clearErrors() {
    formError.innerHTML = '';
    document.querySelectorAll('[data-error-for]').forEach((el) => {
      el.innerHTML = '';
    });
    picker.showError('');
  }

  // Shows a refusal beside the part of the schedule it concerns, and
  // brings that part into view.
  function showRefusal(err) {
    // The console words a refusal as the rest of a sentence; shown by
    // itself it starts one.
    const message = err.message.charAt(0).toUpperCase() + err.message.slice(1);
    const alert = `<vox-alert variant="danger">${escapeHtml(message)}</vox-alert>`;
    let place;
    if (err.step && plan && plan.form.showStepError(err.step, message)) {
      return;
    }
    if (err.field === 'target') {
      picker.showError(message);
      place = document.getElementById('target');
    } else if (err.field === 'task') {
      if (err.fields) taskForm.showServerProblems(err.fields);
      else taskForm.showError(message);
      place = document.getElementById('task-pane');
    } else {
      place = err.field ? document.querySelector(`[data-error-for="${CSS.escape(err.field)}"]`) : null;
      (place || formError).innerHTML = alert;
      place = place || formError;
    }
    place.scrollIntoView({ block: 'center', behavior: 'smooth' });
  }

  function build(schedule) {
    setSelectValue(kindEl, ['task', 'plan'].includes(schedule.kind) ? schedule.kind : 'run');
    const hasSteps = schedule.kind === 'plan' && Array.isArray(schedule.steps) && schedule.steps.length > 0;
    if (hasSteps) setPlan(schedule.planName || schedule.name || '', schedule.steps);
    loadPlans(hasSteps);
    nameEl.value = schedule.name || '';
    runOptions = createRunOptions(document.getElementById('run-options'), schedule.options || {});
    taskForm = createTaskForm(document.getElementById('task-body'), document.getElementById('task-error'), {
      initial: schedule.kind === 'task' ? schedule : null,
    });
    taskForm.load();
    picker = createTargetPicker(document.getElementById('target'), { initial: schedule });

    // Default start: the next whole hour, which is at least a few
    // minutes away and reads as a deliberate time.
    const start = schedule.startAt ? new Date(schedule.startAt) : new Date(Math.ceil((Date.now() + 5 * 60000) / 3600000) * 3600000);
    startEl.value = toLocalInput(start);
    const zone = timeZoneName();
    document.getElementById('start-note').textContent = zone
      ? t('In your time zone, {zone}.', { zone })
      : t('In your local time.');

    if (schedule.repeat) {
      repeatEl.checked = true;
      repeatEveryEl.value = String(schedule.repeat.every);
      setSelectValue(repeatUnitEl, schedule.repeat.unit);
    }
    showPanes();
    showRepeat();
  }

  function collect() {
    clearErrors();
    const body = { name: nameEl.value.trim(), kind: kindEl.value, ...picker.getTarget() };

    if (body.kind === 'task') {
      const task = taskForm.collect();
      if (!task) {
        document.getElementById('task-pane').scrollIntoView({ block: 'center', behavior: 'smooth' });
        return null;
      }
      Object.assign(body, task);
    } else if (body.kind === 'plan') {
      if (!plan) {
        showRefusal({ field: 'steps', message: t('Choose the plan to run.') });
        return null;
      }
      plan.form.showStepError(0, '');
      const steps = plan.form.getSteps();
      if (!steps) return null;
      body.planName = plan.name;
      body.steps = steps;
    } else {
      const options = runOptions.getOptions();
      if (Object.keys(options).length > 0) body.options = options;
    }

    const start = new Date(startEl.value);
    if (!startEl.value || Number.isNaN(start.getTime())) {
      showRefusal({ field: 'startAt', message: t('Choose when it starts.') });
      return null;
    }
    body.startAt = start.toISOString();
    if (repeatEl.checked) {
      body.repeat = { every: Number(repeatEveryEl.value), unit: repeatUnitEl.value };
    }
    if (picker.isEmpty()) {
      showRefusal({ field: 'target', message: picker.emptyMessage() });
      return null;
    }
    return body;
  }

  kindEl.addEventListener('change', showPanes);
  repeatEl.addEventListener('change', showRepeat);

  saveButton.addEventListener('click', async () => {
    const body = collect();
    if (!body) return;
    saveButton.disabled = true;
    try {
      const saved = id
        ? await sendJSON(`/api/v1/orchestrator/schedules/${encodeURIComponent(id)}`, 'PUT', body)
        : await sendJSON('/api/v1/orchestrator/schedules', 'POST', body);
      window.location.href = `/schedule.html?id=${saved.id}`;
    } catch (err) {
      showRefusal(err);
      saveButton.disabled = false;
    }
  });

  async function load() {
    if (!id) {
      build((qs('draft') && takeDraft()) || {});
      return;
    }
    document.getElementById('heading').textContent = t('Edit schedule');
    document.getElementById('breadcrumb-current').textContent = t('Edit schedule');
    document.getElementById('cancel').setAttribute('href', `/schedule.html?id=${encodeURIComponent(id)}`);
    try {
      const schedule = await fetchJSON(`/api/v1/orchestrator/schedules/${encodeURIComponent(id)}`);
      build(schedule);
      // Whoever saves a schedule owns it from then on: say so before
      // someone takes over another person's without meaning to.
      const me = getClaims()?.sub;
      if (me && schedule.owner !== me) {
        document.getElementById('owner-notice').innerHTML = `<vox-alert variant="info" class="vox-m-bottom-lg">${escapeHtml(
          t('This schedule runs as {owner}. If you save it, it will run as you from then on, with your access. Any sensitive task values must be entered again.', { owner: schedule.owner }),
        )}</vox-alert>`;
      }
    } catch (err) {
      document.getElementById('schedule-form').innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  load();
}
