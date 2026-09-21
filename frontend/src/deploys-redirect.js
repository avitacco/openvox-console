// /deploys.html moved into the Code page's Deploy history tab. Kept as
// a redirect rather than deleted so existing bookmarks, and the links
// in the setup runbooks, still land somewhere useful. Query parameters
// are carried through, so /deploys.html?source=team_a still works.
const params = new URLSearchParams(window.location.search);
params.set('tab', 'deploys');
window.location.replace(`/code.html?${params.toString()}`);
