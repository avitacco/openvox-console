import { fetchJSON, sendJSON, escapeHtml, requirePermission, hasPermission, confirmDialog, qs, withLoading, t } from './app.js';
import { sentence, actionHTML, targetHTML, repeatText, stateBadge, outcomeBadge, formatWhen, timeZoneName } from './schedule-common.js';
import { stepLabel, stepActionHTML, stepDetailHTML } from './plan-common.js';

if (requirePermission('orchestrator:read')) {
  const id = qs('id');
  const heading = document.getElementById('heading');
  const summaryEl = document.getElementById('summary');
  const firingsEl = document.getElementById('firings');
  const actionError = document.getElementById('action-error');
  const controls = document.getElementById('controls');
  const pauseButton = document.getElementById('pause');
  const resumeButton = document.getElementById('resume');
  const path = `/api/v1/orchestrator/schedules/${encodeURIComponent(id)}`;

  function showActionError(message) {
    actionError.innerHTML = message ? `<vox-alert variant="danger" class="vox-m-bottom-md">${escapeHtml(sentence(message))}</vox-alert>` : '';
  }

  function paramsHTML(params) {
    if (!params || Object.keys(params).length === 0) return '';
    return `
      <vox-disclosure class="vox-m-top-md" summary="${escapeHtml(t('Parameters'))}">
        <vox-code-block language="json" no-header>${escapeHtml(JSON.stringify(params, null, 2))}</vox-code-block>
      </vox-disclosure>`;
  }

  // A scheduled plan's steps: the schedule's own copy, which is what
  // runs whatever has since become of the saved plan it was made from.
  function stepsHTML(schedule) {
    if (schedule.kind !== 'plan') return '';
    const items = (schedule.steps || [])
      .map(
        (step, i) => `
        <li class="plan-step">
          <div class="plan-step-heading"><span class="plan-step-number">${escapeHtml(stepLabel(i))}</span> ${stepActionHTML(step)}</div>
          ${stepDetailHTML(step)}
        </li>`
      )
      .join('');
    return `
      <h2 class="vox-m-top-lg">${escapeHtml(t('Steps'))}</h2>
      <p class="vox-ts-sm vox-color-text-2 vox-m-top-none">${escapeHtml(
        t('This schedule keeps its own copy of the steps, taken when it was saved. Changing or deleting the saved plan does not change what the schedule runs.'),
      )}</p>
      <ol class="plan-step-list">${items}</ol>`;
  }

  function nextText(schedule) {
    if (schedule.state === 'paused') return t('Paused: it will not start until it is resumed.');
    if (schedule.state === 'finished') return t('Nothing further: it does not repeat, and its time has passed.');
    return formatWhen(schedule.nextRunAt);
  }

  function render(schedule, groupNames) {
    heading.textContent = schedule.name;
    document.getElementById('breadcrumb-current').textContent = schedule.name;
    const zone = timeZoneName();
    const nodes = schedule.targets && schedule.targets.length > 3
      ? `<vox-disclosure summary="${escapeHtml(t('All {count} nodes', { count: schedule.targets.length }))}"><ul class="target-preview-list">${schedule.targets.map((n) => `<li>${escapeHtml(n)}</li>`).join('')}</ul></vox-disclosure>`
      : '';
    summaryEl.innerHTML = `
      <dl class="schedule-summary">
        <dt>${escapeHtml(t('State'))}</dt><dd>${stateBadge(schedule)}</dd>
        <dt>${escapeHtml(t('Action'))}</dt><dd>${actionHTML(schedule)}${schedule.kind === 'task' && schedule.environment ? ` <span class="vox-color-text-2">(${escapeHtml(schedule.environment)})</span>` : ''}</dd>
        <dt>${escapeHtml(t('Nodes'))}</dt><dd>${targetHTML(schedule, groupNames)}${nodes}</dd>
        <dt>${escapeHtml(t('Start'))}</dt><dd>${escapeHtml(formatWhen(schedule.startAt))}</dd>
        <dt>${escapeHtml(t('Repeats'))}</dt><dd>${escapeHtml(repeatText(schedule))}</dd>
        <dt>${escapeHtml(t('Next start'))}</dt><dd>${escapeHtml(nextText(schedule))}</dd>
        <dt>${escapeHtml(t('Runs as'))}</dt><dd>${escapeHtml(schedule.owner)}${schedule.ownerKind === 'service_token' ? ` <span class="vox-color-text-2">(${escapeHtml(t('service token'))})</span>` : ''}</dd>
      </dl>
      ${paramsHTML(schedule.params)}
      ${stepsHTML(schedule)}
      <p class="vox-ts-sm vox-color-text-2 vox-m-top-md vox-m-bottom-none">${escapeHtml(
        t("Each time it starts, it runs on the nodes its owner's role can act on at that moment. If the owner can no longer run jobs, nothing is started."),
      )}${zone ? ` ${escapeHtml(t('Times are shown in your time zone, {zone}.', { zone }))}` : ''}</p>`;

    if (hasPermission('orchestrator:run')) {
      controls.style.display = '';
      // A finished schedule has nothing left to pause.
      pauseButton.style.display = schedule.state === 'active' ? '' : 'none';
      resumeButton.style.display = schedule.state === 'paused' ? '' : 'none';
    }
    renderFirings(schedule.firings || []);
  }

  function renderFirings(firings) {
    if (firings.length === 0) {
      firingsEl.innerHTML = `<vox-empty-state heading="${escapeHtml(t('It has not come due yet'))}"></vox-empty-state>`;
      return;
    }
    const rows = firings
      .map(
        (f) => `
      <tr>
        <td>${escapeHtml(formatWhen(f.dueAt))}</td>
        <td>${outcomeBadge(f)}</td>
        <td>${f.jobId ? `<a href="/job.html?id=${f.jobId}">${escapeHtml(t('Job #{id}', { id: f.jobId }))}</a>` : ''}</td>
        <td>${escapeHtml(sentence(f.detail))}</td>
      </tr>`
      )
      .join('');
    firingsEl.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">${t('Due')}</th><th scope="col">${t('Outcome')}</th><th scope="col">${t('Job')}</th><th scope="col">${t('Reason')}</th></tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
  }

  let groupNames = new Map();

  async function load() {
    try {
      const [schedule, groups] = await Promise.all([
        withLoading(summaryEl, () => fetchJSON(path)),
        fetchJSON('/api/v1/groups?all=true').catch(() => ({ items: [] })),
      ]);
      groupNames = new Map(groups.items.map((g) => [g.id, g.name]));
      render(schedule, groupNames);
    } catch (err) {
      heading.textContent = t('Schedule');
      summaryEl.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  async function change(action) {
    showActionError('');
    try {
      render(await sendJSON(`${path}/${action}`, 'POST', {}), groupNames);
    } catch (err) {
      showActionError(err.message);
    }
  }

  pauseButton.addEventListener('click', () => change('pause'));
  resumeButton.addEventListener('click', () => change('resume'));
  document.getElementById('edit').addEventListener('click', () => {
    window.location.href = `/schedule-edit.html?id=${encodeURIComponent(id)}`;
  });
  document.getElementById('delete').addEventListener('click', async () => {
    const confirmed = await confirmDialog({
      heading: t('Delete this schedule?'),
      body: `<p>${escapeHtml(t('It will not start again. The jobs it has already started stay in the job history.'))}</p>`,
      confirmLabel: t('Delete'),
      danger: true,
    });
    if (!confirmed) return;
    showActionError('');
    try {
      await fetchJSON(path, { method: 'DELETE' });
      window.location.href = '/schedules.html';
    } catch (err) {
      showActionError(err.message);
    }
  });

  if (!id) {
    summaryEl.innerHTML = `<vox-alert variant="danger">${t('No schedule specified.')}</vox-alert>`;
  } else {
    load();
  }
}
