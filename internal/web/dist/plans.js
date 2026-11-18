import { fetchJSON, escapeHtml, requirePermission, hasPermission, withLoading, t, formatDateTime } from './app.js';
import { stepCountText } from './plan-common.js';
import { openRunPlanDialog } from './run-plan.js';
import { renderCodePlans } from './code-plans.js';

if (requirePermission('orchestrator:read')) {
  const results = document.getElementById('results');
  const canRun = hasPermission('orchestrator:run');
  if (canRun) document.getElementById('new-plan').style.display = '';

  function render(plans) {
    if (plans.length === 0) {
      results.innerHTML = `
        <vox-empty-state heading="${escapeHtml(t('No plans yet'))}">
          <vox-icon slot="icon" name="orchestrate" size="lg"></vox-icon>
          ${escapeHtml(t('A plan is a list of Puppet runs and tasks to carry out in order on the same nodes.'))}
        </vox-empty-state>`;
      return;
    }
    const rows = plans
      .map(
        (p) => `
      <tr>
        <td><a href="/plan.html?id=${p.id}">${escapeHtml(p.name)}</a></td>
        <td>${escapeHtml(p.description || '')}</td>
        <td>${escapeHtml(stepCountText(p.stepCount))}</td>
        <td>${escapeHtml(p.updatedBy)} <span class="vox-ts-sm vox-color-text-2">${escapeHtml(formatDateTime(p.updatedAt))}</span></td>
        ${canRun ? `<td><vox-button size="sm" variant="brand" data-run-id="${p.id}">${escapeHtml(t('Run'))}</vox-button></td>` : ''}
      </tr>`
      )
      .join('');
    results.innerHTML = `
      <div class="vox-table-wrap">
        <table class="vox-table vox-table--striped">
          <thead>
            <tr><th scope="col">${t('Name')}</th><th scope="col">${t('Description')}</th><th scope="col">${t('Steps')}</th><th scope="col">${t('Last changed by')}</th>${canRun ? '<th scope="col"></th>' : ''}</tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`;
    results.querySelectorAll('[data-run-id]').forEach((button) => {
      button.addEventListener('click', async () => {
        button.disabled = true;
        try {
          // The list does not carry steps; the dialog needs them, with
          // what each one asks for.
          openRunPlanDialog(await fetchJSON(`/api/v1/orchestrator/saved-plans/${encodeURIComponent(button.dataset.runId)}`));
        } catch (err) {
          results.insertAdjacentHTML('afterbegin', `<vox-alert variant="danger" class="vox-m-bottom-md">${escapeHtml(err.message)}</vox-alert>`);
        } finally {
          button.disabled = false;
        }
      });
    });
  }

  async function load() {
    try {
      const { items } = await withLoading(results, () => fetchJSON('/api/v1/orchestrator/saved-plans'));
      render(items);
    } catch (err) {
      results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
    }
  }

  load();

  const codeEnvironment = document.getElementById('code-environment');
  const loadCode = () => renderCodePlans(document.getElementById('code-plans'), codeEnvironment.value.trim() || 'production');
  codeEnvironment.addEventListener('change', loadCode);
  loadCode();
}
