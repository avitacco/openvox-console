/*
 * Keeps screenshots in the same theme as the page.
 *
 * Each screenshot is a <picture> whose <source> carries a
 * prefers-color-scheme media query, so with no JavaScript at all a
 * visitor already gets the variant matching their system setting, with
 * no flash and no layout shift. That is the mechanism; this file is the
 * refinement.
 *
 * What it adds is the third state. The theme toggle writes an explicit
 * choice to localStorage, and an explicit choice can disagree with the
 * system preference - a visitor whose OS is dark asking for the light
 * site. The media query cannot see that, so the page would go light
 * around screenshots that stayed dark. Here we swap the <img> src to
 * match whichever theme the document is actually in.
 */

const LIGHT = 'data-theme-light';
const DARK = 'data-theme-dark';

/** Current theme, from the attribute the pre-paint script stamped. */
function currentTheme() {
  return document.documentElement.getAttribute('data-vox-theme') === 'dark' ? 'dark' : 'light';
}

/**
 * Points every screenshot at the variant for `theme`.
 *
 * The <source> element carries both URLs as data attributes (written by
 * the site generator), so this needs no knowledge of the naming scheme.
 */
function applyTheme(theme) {
  const figures = document.querySelectorAll('.shot picture');

  figures.forEach((picture) => {
    const source = picture.querySelector('source');
    const img = picture.querySelector('img');
    if (!source || !img) return;

    const wanted = theme === 'dark' ? source.getAttribute(DARK) : source.getAttribute(LIGHT);
    if (!wanted) return;

    // Neutralise the media query once an explicit choice is in play,
    // or the browser keeps honouring it over the img we just set.
    source.setAttribute('srcset', wanted);
    if (img.getAttribute('src') !== wanted) {
      img.setAttribute('src', wanted);
    }
  });
}

/**
 * Only takes over once the visitor has actually expressed a choice.
 * Until then the media query is left to do its job.
 */
function hasExplicitChoice() {
  try {
    return localStorage.getItem('vox-theme') !== null;
  } catch (e) {
    // Private windows and blocked site data both throw here. Falling
    // back to the media query is the correct behaviour, not an error.
    return false;
  }
}

function sync() {
  if (!hasExplicitChoice()) return;
  applyTheme(currentTheme());
}

sync();

// vox-theme-toggle flips the attribute on <html>; watching it means this
// works regardless of what the toggle's own event is called.
new MutationObserver(sync).observe(document.documentElement, {
  attributes: true,
  attributeFilter: ['data-vox-theme'],
});

document.addEventListener('DOMContentLoaded', sync);
