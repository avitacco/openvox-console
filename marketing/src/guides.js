// Platform tabs on guide pages.
//
// A guide offers some steps per platform - Linux, macOS, Windows - as a
// vox-tabs group per step. A reader on Windows is on Windows for the
// whole guide, so choosing a tab in one group chooses the same tab in
// every other group on the page, and the choice is remembered for the
// next guide.
//
// Tabs are matched by data-tab-key, which the renderer derives from the
// English label: it is the same in every locale, so a choice made on the
// German site holds on the English one too.
//
// Selection goes through each tab's own select(), exactly as a click
// does, so vox-tabs keeps doing its own bookkeeping - panels, focus
// order, ARIA state - and this script never touches any of it.

const KEY = 'vox-guide-tab';

function stored() {
  // Private windows and blocked site data both throw here; with no
  // storage the page simply starts on each group's first tab.
  try {
    return localStorage.getItem(KEY);
  } catch (e) {
    return null;
  }
}

function remember(key) {
  try {
    localStorage.setItem(KEY, key);
  } catch (e) {
    // Nothing to do: the choice still applies to this page.
  }
}

// Set while this script is selecting tabs, so the change events those
// selections fire are not taken for the reader choosing again.
let syncing = false;

function selectEverywhere(key) {
  syncing = true;
  try {
    for (const tab of document.querySelectorAll('vox-tab[data-tab-key]')) {
      if (tab.getAttribute('data-tab-key') === key && !tab.selected) {
        tab.select();
      }
    }
  } finally {
    syncing = false;
  }
}

document.addEventListener('vox-tab-change', (event) => {
  if (syncing) return;
  const group = event.target;
  const tab = group.querySelector(`vox-tab[panel="${CSS.escape(event.detail.panel)}"]`);
  const key = tab && tab.getAttribute('data-tab-key');
  if (!key) return;
  remember(key);
  selectEverywhere(key);
});

customElements.whenDefined('vox-tab').then(() => {
  const key = stored();
  if (key) selectEverywhere(key);
});
