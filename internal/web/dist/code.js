import { requirePermission } from './app.js';
import { initRepositories } from './code-repositories.js';
import { initDeploys } from './deploys.js';

if (requirePermission('code:read')) {
  const tabs = document.getElementById('code-tabs');

  // Each view is initialised the first time its tab is shown, not on
  // page load. The repositories endpoint resolves every node's
  // classification against the whole fleet's facts, which is not work
  // worth doing for someone who came here for the deploy log.
  const started = new Set();
  function start(panel) {
    if (started.has(panel)) return;
    started.add(panel);
    if (panel === 'repositories') initRepositories();
    if (panel === 'deploys') initDeploys();
  }

  // ?tab= selects the opening tab so a link can point at either view -
  // the repository records link to ?tab=deploys&source=<name>, and the
  // old /deploys.html redirects here the same way.
  const params = new URLSearchParams(window.location.search);
  const requested = params.get('tab') === 'deploys' ? 'deploys' : 'repositories';

  // Both halves, deliberately. vox-tabs reconciles panels to tabs only
  // in its own sync(), which runs on slotchange and on a tab click -
  // neither of which a programmatic selection triggers. Setting just
  // tab.selected moves the underline while every panel keeps whatever
  // active state slotchange computed (the first tab's), so arriving at
  // ?tab=deploys showed the Deploy history tab as current with the
  // Repositories panel still displayed beneath it. sync() is private,
  // so the panels are set here instead - both are public reflected
  // properties, and a later click still goes through sync() normally.
  function select(panel) {
    for (const tab of tabs.querySelectorAll('vox-tab')) {
      tab.selected = tab.panel === panel;
    }
    for (const el of tabs.querySelectorAll('vox-tab-panel')) {
      el.active = el.name === panel;
    }
  }

  select(requested);
  tabs.addEventListener('vox-tab-change', (event) => start(event.detail.panel));
  start(requested);
}
