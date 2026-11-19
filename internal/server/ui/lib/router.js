// Forgeline web console: a small hash router.
//
// Routes look like '/runs/:id'. A page function receives {params, query} and returns an element,
// optionally with a `cleanup` property that is called when the person navigates away. That is
// where timers and log streams are stopped.
//
// A page that takes a moment is replaced by a loading line at once, so the page before it never stays on screen
// looking current. pageSignal() is aborted when the person leaves; slow reads pass it to get() so they stop
// holding one of the browser's few connections to the server.
//
// A page with unsaved work sets `leaveGuard` on its element: a function returning { dirty, ask }. Before the router
// leaves it (a link, the back button), it puts the address back and awaits ask(), which resolves true to go on; the
// browser's own prompt covers closing or reloading the tab.

const routes = [];
let current = null;
let host = null;
let renderToken = 0;
let pageAbort = new AbortController();
const LOADING_AFTER = 120; // ms: quicker pages swap without a flash
let shownPath = null; // the address of the page on screen
let leaving = false;  // set once a guarded page agreed to be left

function unsaved() {
  try { return current && current.leaveGuard ? current.leaveGuard() : null; } catch (e) { console.error(e); return null; }
}

addEventListener('beforeunload', (e) => { const g = unsaved(); if (g && g.dirty) { e.preventDefault(); e.returnValue = ''; } });

/** Aborted when the person navigates away from the page that is showing (or loading). */
export function pageSignal() { return pageAbort.signal; }

function loadingLine() {
  const el = document.createElement('div');
  el.className = 'flex items-center gap-2 p-6 text-sm text-slate-500 dark:text-slate-400';
  el.id = 'page-loading';
  el.setAttribute('role', 'status');
  const dot = document.createElement('span');
  dot.className = 'size-3 animate-spin rounded-full border-2 border-slate-300 border-t-brand-500';
  el.append(dot, document.createTextNode('Loading…'));
  return el;
}

export function route(pattern, page) {
  const keys = [];
  const re = new RegExp('^' + pattern.replace(/:([a-z]+)/g, (_, k) => { keys.push(k); return '([^/]+)'; }) + '/?$');
  routes.push({ re, keys, page });
}

export function navigate(path) { location.hash = '#' + path; }

export function currentPath() { return location.hash.slice(1) || '/'; }

export function start(outlet, notFound) {
  host = outlet;
  const run = () => show(notFound);
  addEventListener('hashchange', run);
  run();
}

export function reload() { return show(); }

/** Runs the current page's cleanup (timers, streams) and forgets it. Call before leaving the app shell. */
export function stop() {
  shownPath = null;
  renderToken++; // a page still loading must not appear afterwards
  pageAbort.abort();
  pageAbort = new AbortController();
  if (current && current.cleanup) { try { current.cleanup(); } catch (e) { console.error(e); } }
  current = null;
}

async function show(notFound) {
  const target = currentPath();
  if (!leaving && shownPath !== null && target !== shownPath) {
    const g = unsaved();
    if (g && g.dirty) {
      history.replaceState(null, '', '#' + shownPath); // stay put (no hashchange) while the person decides
      if (!(await g.ask())) return;
      leaving = true;
      location.hash = '#' + target; // shows it through the hashchange
      return;
    }
  }
  leaving = false;
  shownPath = target;
  const token = ++renderToken;
  pageAbort.abort();
  pageAbort = new AbortController();
  if (current && current.cleanup) { try { current.cleanup(); } catch (e) { console.error(e); } }
  current = null;
  const slow = setTimeout(() => { if (token === renderToken && host) host.replaceChildren(loadingLine()); }, LOADING_AFTER);
  const [path, qs] = currentPath().split('?');
  const query = Object.fromEntries(new URLSearchParams(qs || ''));
  for (const r of routes) {
    const m = r.re.exec(path);
    if (!m) continue;
    const params = {};
    r.keys.forEach((k, i) => { params[k] = decodeURIComponent(m[i + 1]); });
    let el;
    try { el = await r.page({ params, query }); } catch (e) { console.error(e); el = notFound ? notFound(e) : document.createTextNode(String(e)); }
    clearTimeout(slow);
    if (token !== renderToken) { if (el && el.cleanup) el.cleanup(); return; } // navigated away while loading
    current = el;
    host.replaceChildren(el);
    window.scrollTo(0, 0);
    host.focus({ preventScroll: true });
    return;
  }
  clearTimeout(slow);
  host.replaceChildren(notFound ? notFound() : document.createTextNode('Not found'));
}
