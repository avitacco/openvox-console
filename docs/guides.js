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

// "On this page" scrollspy.
//
// vox-toc draws the outline and styles whichever item carries `current`;
// deciding which that is belongs to the page. The section being read is
// the last one whose heading has scrolled up past a line near the top of
// the viewport - so a section stays current for as long as any of it is
// what the reader is looking at, and following a link to a heading
// (which lands it just below the top) makes that heading current.
//
// Scroll position rather than an IntersectionObserver: an observer
// reports headings entering and leaving a band, which says nothing about
// which section fills the screen while no heading is in view - the usual
// case in a long section.

// How far below the top of the viewport a heading counts as passed.
// Clear of scroll-margin-top on guide headings (site.css), so a heading
// jumped to from the outline is already past it.
const READ_LINE = 96;

function initScrollspy() {
  const items = [...document.querySelectorAll('.guide-toc-rail vox-toc-item')];
  const sections = items
    .map((item) => {
      const id = decodeURIComponent((item.getAttribute('href') || '').replace(/^#/, ''));
      return { item, heading: document.getElementById(id) };
    })
    .filter((s) => s.heading);
  if (sections.length === 0) return;

  // Document order, which is also outline order; kept explicit so a
  // reordered outline cannot quietly break the "last one passed" rule.
  sections.sort((a, b) =>
    a.heading.compareDocumentPosition(b.heading) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1,
  );

  let active = null;
  let frame = 0;

  function update() {
    frame = 0;
    let current = sections[0];
    for (const s of sections) {
      if (s.heading.getBoundingClientRect().top > READ_LINE) break;
      current = s;
    }
    // At the very bottom, the last section is the one being read even if
    // it is too short for its heading to reach the line.
    const doc = document.documentElement;
    if (window.innerHeight + window.scrollY >= doc.scrollHeight - 2) {
      current = sections[sections.length - 1];
    }
    if (current === active) return;
    if (active) active.item.current = false;
    current.item.current = true;
    active = current;
  }

  const schedule = () => {
    if (!frame) frame = requestAnimationFrame(update);
  };
  window.addEventListener('scroll', schedule, { passive: true });
  window.addEventListener('resize', schedule);
  update();
}

customElements.whenDefined('vox-toc-item').then(initScrollspy);
