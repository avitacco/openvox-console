import { fetchJSON, sendJSON, escapeHtml, hasPermission } from './app.js';

// Compact rather than toLocaleString(): "9/20/2026, 6:15:37 PM" needs
// roughly 165px, which forces the whole data grid wider than the page
// can give it once the sidenav and status column are accounted for. To
// the minute is enough for "when did this last deploy", and the full
// timestamp stays in the tooltip.
function formatWhen(iso) {
  if (!iso) return null;
  const at = new Date(iso);
  const date = at.toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
  const time = at.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
  return `${date}, ${time}`;
}

function formatBytes(bytes) {
  if (bytes === null || bytes === undefined) return null;
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / 1024 ** exponent;
  return `${value >= 10 || exponent === 0 ? Math.round(value) : value.toFixed(1)} ${units[exponent]}`;
}

// Shortens a remote to the part that identifies the repository. The
// full value stays in the title attribute: an untruncated
// file:///home/user/very/long/path/control-repo.git fills the row and
// still gets clipped, so it names nothing useful at a glance.
function shortRemote(remote) {
  if (!remote) return '';
  const trimmed = remote.replace(/\.git$/, '');

  // SCP-style (git@host:org/repo) is not a URL, so handle it first.
  const scp = trimmed.match(/^[^/]+@([^:]+):(.+)$/);
  if (scp) return `${scp[1]}/${scp[2]}`;

  try {
    const url = new URL(trimmed);
    const path = url.pathname.replace(/^\/+/, '');
    // A local path has no meaningful host, so the last two segments
    // identify it better than the whole filesystem path.
    if (!url.host) return path.split('/').slice(-2).join('/');
    return `${url.host}/${path}`;
  } catch {
    return trimmed;
  }
}

// One datum: icon plus a self-describing value. vox-datum's `name` is
// deliberately screen-reader-only - the component's own docs say the
// icon carries the meaning for sighted users - so every value here
// reads on its own ("6 reporting", not a bare "6") and `name` supplies
// the spoken label.
// area names a fixed cell in .repo-data's grid. Placing every field
// explicitly is what keeps columns aligned down the list: the record
// item's own .meta is a wrapping flex in its shadow root, so datums
// otherwise flow by content width and no two records line up. An absent
// field leaves its cell empty rather than shifting everything after it.
function datum(name, icon, value, area, title = '') {
  if (value === null || value === undefined || value === '') return '';
  const attr = title ? ` title="${escapeHtml(title)}"` : '';
  return `
    <vox-datum name="${escapeHtml(name)}" style="grid-area: ${area}"${attr}>
      <vox-icon slot="icon" name="${icon}" size="sm"></vox-icon>${escapeHtml(value)}
    </vox-datum>`;
}

// Badge plus a short note. The full sentence lives in the badge's
// tooltip: the row has to be scannable, and three lines of prose per
// repository is not.
function usageState(repo, countsAvailable) {
  if (!repo.lastDeployedAt) {
    return { variant: 'neutral', label: 'Never deployed', note: '', title: 'No successful deploy has been recorded for this repository' };
  }
  if (!countsAvailable) {
    return { variant: 'neutral', label: 'Counts unknown', note: '', title: 'Node data could not be read, so usage counts are unavailable' };
  }

  const assigned = repo.assignedNodes ?? 0;
  const reporting = repo.reportingNodes ?? 0;

  if (assigned === 0 && reporting === 0) {
    return {
      variant: 'warning',
      label: 'Unused',
      note: 'No nodes assigned or reporting',
      title: 'Deployed, but no node is classified into or reporting from its environments',
    };
  }
  if (reporting === 0) {
    return {
      variant: 'warning',
      label: 'Not running yet',
      note: 'Assigned, none reporting',
      title: `${assigned} node(s) are classified into its environments, but none has reported from one yet`,
    };
  }
  if (assigned === 0) {
    return {
      variant: 'warning',
      label: 'Unclassified',
      note: 'Reporting, none assigned',
      title: `${reporting} node(s) report from its environments, but no group assigns any node there`,
    };
  }
  if (assigned !== reporting) {
    return {
      variant: 'tip',
      label: 'Converging',
      note: 'Not all nodes have run',
      title: `${assigned} assigned against ${reporting} reporting - some nodes have not run since the change`,
    };
  }
  return { variant: 'tip', label: 'In use', note: '', title: `${assigned} node(s) assigned and reporting` };
}

function deployButton(repo) {
  if (!hasPermission('code:deploy')) return '';
  return `<vox-button size="sm" data-deploy="${escapeHtml(repo.name)}">Deploy</vox-button>`;
}

function renderRepositories(results, data) {
  const repos = data.repositories ?? [];
  if (repos.length === 0) {
    results.innerHTML = `
      <vox-empty-state heading="No code repositories configured">
        <vox-icon slot="icon" name="deploy" size="lg"></vox-icon>
        Set CONSOLE_CONTROL_REPO_URL for a single control repo, or
        CONSOLE_CODE_SOURCES_PATH for several. See the README.
      </vox-empty-state>`;
    return;
  }

  const items = repos
    .map((repo) => {
      const state = usageState(repo, data.nodeCountsAvailable);
      const counts = data.nodeCountsAvailable;

      // Absent data is omitted rather than rendered as a dash. A row of
      // em-dashes reads as broken; a repository that has never deployed
      // simply has nothing to say about size or environments, and its
      // badge already says so.
      const data_ = [
        datum('Remote', 'repository', shortRemote(repo.remote), 'remote', repo.remote),
        datum('Environment prefix', 'tag', repo.prefix || null, 'prefix', 'Environment names from this repository are prefixed'),
        datum(
          'Environments',
          'fork',
          (repo.environments ?? []).length ? repo.environments.join(', ') : null,
          'envs',
          'Environments this repository has deployed'
        ),
        datum(
          'Last deployed',
          'clock',
          formatWhen(repo.lastDeployedAt),
          'deployed',
          repo.lastDeployedAt
            ? `Last successful deploy: ${new Date(repo.lastDeployedAt).toLocaleString()}`
            : ''
        ),
        counts ? datum('Nodes assigned', 'classifier', `${repo.assignedNodes ?? 0} assigned`, 'assigned', 'Nodes classification sends to this repository') : '',
        counts ? datum('Nodes reporting', 'pulse', `${repo.reportingNodes ?? 0} reporting`, 'reporting', 'Nodes whose last run came from this repository') : '',
        datum('Size', 'layers', formatBytes(repo.sizeBytes), 'size', 'Content size at the last successful deploy'),
      ].join('');

      return `
      <vox-record-list-item heading="${escapeHtml(repo.name)}"
                            href="?tab=deploys&source=${encodeURIComponent(repo.name)}"
                            link-label="View ${escapeHtml(repo.name)} deploy history">
        <div class="repo-data">${data_}</div>
        <div slot="end" class="repo-state">
          <div class="repo-state-row">
            <vox-badge variant="${state.variant}" title="${escapeHtml(state.title)}">${escapeHtml(state.label)}</vox-badge>
            ${deployButton(repo)}
          </div>
          ${state.note ? `<div class="repo-note">${escapeHtml(state.note)}</div>` : ''}
        </div>
      </vox-record-list-item>`;
    })
    .join('');

  results.innerHTML = `<vox-record-list>${items}</vox-record-list>`;
  bindDeployButtons(results);
}

// Deploys production from one repository, the same branch the shared
// "Deploy now" trigger uses, then reloads so the record's size, last
// deploy and status reflect what just happened.
function bindDeployButtons(results) {
  const errors = document.getElementById('repo-trigger-error');

  for (const button of results.querySelectorAll('[data-deploy]')) {
    button.addEventListener('click', async () => {
      const source = button.dataset.deploy;
      if (errors) errors.innerHTML = '';
      button.disabled = true;
      try {
        await sendJSON('/api/v1/code-deploys', 'POST', { source, environment: 'production' });
        await initRepositories();
      } catch (err) {
        if (errors) {
          errors.innerHTML = `<vox-alert variant="danger">Deploying ${escapeHtml(source)} failed: ${escapeHtml(err.message)}</vox-alert>`;
        }
      } finally {
        button.disabled = false;
      }
    });
  }
}

// Nodes the console cannot trace back to a configured repository are
// called out rather than hidden: they usually mean an environment left
// behind by a removed source, or a node with a hardcoded environment in
// its puppet.conf.
function renderUnattributed(unattributed, data) {
  if (!unattributed) return;

  if (!data.nodeCountsAvailable) {
    unattributed.innerHTML =
      '<vox-alert variant="warning">Node counts are unavailable - openvoxdb could not be reached. Everything else here is current.</vox-alert>';
    return;
  }

  const parts = [];
  const strays = Math.max(data.unattributedAssigned ?? 0, data.unattributedReporting ?? 0);
  if (strays > 0) {
    parts.push(`${strays} node${strays === 1 ? '' : 's'} using an environment no repository here has deployed.`);
  }
  if (data.nodesWithoutEnvironment) {
    parts.push(`${data.nodesWithoutEnvironment} with no environment recorded.`);
  }

  unattributed.innerHTML = parts.length
    ? `<vox-alert variant="info">${escapeHtml(parts.join(' '))}</vox-alert>`
    : '';
}

// initRepositories wires the repositories view. Exported rather than
// run on import so the Code page can defer it until its tab is first
// shown - the endpoint behind it resolves every node's classification,
// which is not work to do for someone who opened the Deploys tab.
export async function initRepositories() {
  const results = document.getElementById('repo-results');
  const unattributed = document.getElementById('repo-unattributed');
  if (!results) return;

  try {
    const data = await fetchJSON('/api/v1/code-repositories');
    renderUnattributed(unattributed, data);
    renderRepositories(results, data);
  } catch (err) {
    results.innerHTML = `<vox-alert variant="danger">${escapeHtml(err.message)}</vox-alert>`;
  }
}
