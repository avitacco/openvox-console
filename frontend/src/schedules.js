import { fetchJSON, escapeHtml, requirePermission, hasPermission, withLoading, t } from './app.js';
import { sentence, actionHTML, targetHTML, repeatText, stateBadge, outcomeBadge, formatWhen, timeZoneName } from './schedule-common.js';

if (requirePermission('orchestrator:read')) {
  const results = document.getElementById('results');
  if (hasPermission('orchestrator:run')) document.getElementById('new-schedule').style.display = '';

  // What happened last time: a skipped or missed firing is the thing a
  // reader of this list most needs to see, so its reason is spelled
  // out rather than left behind a badge.
  function lastHTML(schedule) {
    const last = schedule.lastFiring;
    if (!last) return `<span class="vox-color-text-2">${escapeHtml(t('Not yet'))}</span>`;
    const when = escapeHtml(formatWhen(last.dueAt));
    const job = last.jobId ? ` <a href="/job.html?id=${last.jobId}">${escapeHtml(t('Job #{id}', { id: last.jobId }))}</a>` : '';
    const detail = last.outcome !== 'started' && last.detail ? `<div class="vox-ts-sm vox-color-text-2">${escapeHtml(sentence(last.detail))}</div>` : '';
    return `${outcomeBadge(last)} <span class="vox-ts-sm">${when}</span>${job}${detail}`;
  }

  function render(schedules, groupNames) {
    if (schedules.length === 0) {
      results.innerHTML = `
        <vox-empty-state heading="${escapeHtml(t('No scheduled jobs'))}">
          <vox-icon slot="icon" name="orchestrate" size="lg"></vox-icon>
          ${escapeHtml(t('Schedule a Puppet run, a task or a plan to start later, once or repeatedly.'))}
        </vox-empty-state>`;
      return;
    }
    const rows = schedules
      .map(
        (s) => `
      <tr>
        <td><a href="/schedule.html?id=${s.id}">${escapeHtml(s.name)}</a></td>
        <td>${actionHTML(s)}</td>
        <td>${targetHTML(s, groupNames)}</td>
        <td>${escapeHtml(repeatText(s))}</td>
        <td>${s.state === 'active' && s.nextRunAt ? escapeHtml(formatWhen(s.nextRunAt)) : '—'}</td>
        <td>${stateBadge(s)}</td>
        <td>${lastHTML(s)}</td>
        <td>${escapeHtml(s.owner)}</td>
      </tr>`
      )
      .join('');
    const zone = timeZoneName();
    results.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">${t('Name')}</th><th scope="col">${t('Action')}</th><th scope="col">${t('Nodes')}</th><th scope="col">${t('Repeats')}</th><th scope="col">${t('Next start')}</th><th scope="col">${t('State')}</th><th scope="col">${t('Last time')}</th><th scope="col">${t('Runs as')}</th></tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>
      ${zone ? `<p class="vox-ts-sm vox-color-text-2 vox-m-top-md">${escapeHtml(t('Times are shown in your time zone, {zone}.', { zone }))}</p>` : ''}`;
  }

  async function load() {
    try {
      const [{ items }, groups] = await Promise.all([
        withLoading(results, () => fetchJSON('/api/v1/orchestrator/schedules')),
        // Group names are a nicety; listing groups needs a permission
        // not everyone here holds.
        fetchJSON('/api/v1/groups?all=true').catch(() => ({ items: [] })),
      ]);
      render(items, new Map(groups.items.map((g) => [g.id, g.name])));
    } catch (err) {
      results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  load();
}
