package main

import (
	"fmt"

	"github.com/voxpupuli/enterprise-console/internal/demodata"
	"github.com/voxpupuli/enterprise-console/marketing/shots"
)

// This file holds everything that exists purely so two capture runs
// produce identical bytes. Each measure addresses a specific way a
// screenshot would otherwise differ between runs while the page itself
// had not changed at all.

// initScript is evaluated on every new document *before* the page's own
// scripts run. Everything here has to happen that early:
//
//   - The theme must be set before first paint, or the page renders the
//     other theme briefly and a screenshot can catch it.
//   - Date must be replaced before any script reads it, since the
//     console formats timestamps as the page renders.
func initScript(theme shots.Theme, tokens tokenPair) string {
	return fmt.Sprintf(`
(() => {
  // --- session -------------------------------------------------
  // The same two keys the console's own login writes on success, so
  // every page loads as an authenticated user without this tool having
  // to drive a login form whose fields live inside a shadow root.
  try {
    localStorage.setItem('console.accessToken', %q);
    localStorage.setItem('console.refreshToken', %q);
  } catch (e) {}

  // --- theme ---------------------------------------------------
  // The console reads this key before first paint and stamps the
  // attribute itself; setting both covers either order.
  try { localStorage.setItem('vox-theme', %q); } catch (e) {}
  document.documentElement.setAttribute('data-vox-theme', %q);

  // --- clock ---------------------------------------------------
  // Pinned to the same instant the demo fleet is dated to, so relative
  // times ("4 minutes ago") and absolute ones both render identically
  // on every run. Without this each capture bakes in its own wall
  // clock and every refresh rewrites every file.
  //
  // Date is replaced rather than the DevTools virtual-time policy
  // being paused: pausing virtual time also stops the timers the page
  // needs to finish loading, which turns a screenshot into a hang.
  const fixed = %d;
  const RealDate = Date;
  function FrozenDate(...args) {
    if (!new.target) return new RealDate(fixed).toString();
    return args.length === 0 ? new RealDate(fixed) : new RealDate(...args);
  }
  FrozenDate.prototype = RealDate.prototype;
  FrozenDate.now = () => fixed;
  FrozenDate.parse = RealDate.parse;
  FrozenDate.UTC = RealDate.UTC;
  Object.defineProperty(FrozenDate, 'name', { value: 'Date' });
  window.Date = FrozenDate;
})()
`, tokens.AccessToken, tokens.RefreshToken, theme, theme, demodata.InstantMillis())
}

// stillnessCSS removes every source of motion and of per-frame
// variation. A screenshot taken a few milliseconds into a transition
// differs from one taken a frame later, and nothing about that
// difference is meaningful.
const stillnessCSS = `
*, *::before, *::after {
  animation-duration: 0s !important;
  animation-delay: 0s !important;
  animation-iteration-count: 1 !important;
  transition-duration: 0s !important;
  transition-delay: 0s !important;
  caret-color: transparent !important;
  scroll-behavior: auto !important;
}
/* The console shows a loader while fleet-wide queries run. By the time
   a shot is taken its data has rendered, but a leftover spinner frame
   would still vary between runs. */
vox-loader { visibility: hidden !important; }

/* Scrollbars are furniture of the window the console happened to be
   rendered in, not part of the console. Chrome's --hide-scrollbars flag
   does not reliably suppress them here, so they are hidden in CSS. */
html { scrollbar-width: none !important; }
::-webkit-scrollbar { width: 0 !important; height: 0 !important; display: none !important; }
`

// stillnessScript injects stillnessCSS into the current page. It runs
// after load rather than in initScript because it needs document.head.
func stillnessScript() string {
	return fmt.Sprintf(`(() => {
  if (document.getElementById('capture-stillness')) return;
  const style = document.createElement('style');
  style.id = 'capture-stillness';
  style.textContent = %q;
  document.head.appendChild(style);
})()`, stillnessCSS)
}
