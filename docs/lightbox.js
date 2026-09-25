// Full-size screenshots.
//
// Every screenshot is a link to its full-size image (see gen's
// screenshot function), which already works with no script at all. This
// opens it in a viewer on the page instead, so a reader can look closely
// and carry on reading where they were.
//
// Only a plain click is taken over. A click with a modifier, or a middle
// click, still follows the link - "open in a new tab" keeps working -
// and before any click is handled the link is pointed at the image the
// reader is actually seeing, so a dark-theme reader gets the dark image
// whichever way they open it.

const dialog = document.querySelector('dialog.lightbox');
const image = dialog && dialog.querySelector('.lightbox-image');

function shown(link) {
  const img = link.querySelector('img');
  return img ? img.currentSrc || img.getAttribute('src') : link.href;
}

document.addEventListener('click', (event) => {
  const link = event.target.closest && event.target.closest('a.shot-link');
  if (!link) return;

  const img = link.querySelector('img');
  link.href = shown(link);

  const plain = event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
  if (!plain || !dialog || typeof dialog.showModal !== 'function') return;

  event.preventDefault();
  image.src = link.href;
  image.alt = img ? img.alt : '';
  // The image's own width in CSS pixels - the width it was captured at -
  // so "full size" means as large as the console itself was, scaled
  // down only where the screen is smaller.
  image.style.width = img && img.getAttribute('width') ? `${img.getAttribute('width')}px` : '';
  dialog.showModal();
  dialog.scrollTop = 0;
});

if (dialog) {
  dialog.querySelector('.lightbox-close').addEventListener('click', () => dialog.close());

  // A click on the backdrop lands on the dialog element itself, outside
  // its content box; a click on the image or the bar does not close it.
  dialog.addEventListener('click', (event) => {
    if (event.target === dialog) dialog.close();
  });

  // Nothing large left in memory, and nothing stale shown for an instant
  // the next time it opens.
  dialog.addEventListener('close', () => {
    image.removeAttribute('src');
  });
}
