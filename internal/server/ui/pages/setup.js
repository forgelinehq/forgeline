// Setting up Forgeline (design note 038, step 1): the first-run steps, each done when what it sets up exists. Home
// shows them as a card until every step is done or skipped; Settings › Setup shows them any time. Each step links to
// the page that does it; onboarding itself changes nothing.
import { h, icon, mount, pageHeader, spinner, errorBox, toast } from '../lib/dom.js';
import { get, put, atLeast } from '../lib/api.js';
import { session } from '../app.js';

function stepRow(s, admin, redraw) {
  const state = s.done ? 'done' : s.skipped ? 'skipped' : 'todo';
  return h('li', { class: 'flex items-start gap-3 py-3', 'data-step': s.id, 'data-state': state },
    h('span', { class: 'mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full ' + (s.done ? 'bg-emerald-500 text-white' : s.skipped ? 'bg-slate-200 text-slate-500 dark:bg-slate-700' : 'border-2 border-brand-500 text-brand-600'), 'aria-hidden': 'true' },
      s.done ? icon('check', 'size-3.5') : s.skipped ? icon('x', 'size-3') : null),
    h('div', { class: 'min-w-0 flex-1' },
      h('b', { class: 'block text-sm' + (s.skipped ? ' text-slate-400 line-through' : '') }, s.title),
      h('span', { class: 'muted block text-xs' }, s.done ? s.detail : s.why)),
    !s.done && !s.skipped && h('a', { class: 'btn-primary btn-sm shrink-0', href: s.link }, s.action),
    !s.done && admin && h('button', { class: 'btn-ghost btn-sm shrink-0', type: 'button', 'data-skip': s.id, onclick: async () => {
      try { await put('/v1/onboarding/' + s.id, { skip: !s.skipped }); redraw(); } catch (e) { toast(e.message, 'error'); }
    } }, s.skipped ? 'Bring back' : 'Skip'));
}

/** The card on Home. Empty when there is nothing left to set up, or for people who cannot act on it. */
export function setupCard() {
  const box = h('div', { id: 'setup-card' });
  if (!atLeast(session.role, 'developer')) return box;
  const admin = atLeast(session.role, 'admin');
  let open = true;
  async function draw() {
    let o;
    try { o = await get('/v1/onboarding'); } catch { return mount(box, ''); }
    if (o.finished) return mount(box, '');
    const next = o.steps.find(s => !s.done && !s.skipped);
    const pct = Math.round(o.done / o.steps.length * 100);
    const bar = h('div', { class: 'h-full rounded-full bg-brand-500' });
    bar.style.width = Math.max(4, pct) + '%'; // the CSSOM, not a style attribute
    mount(box, h('section', { class: 'card overflow-hidden', 'aria-label': 'Set up Forgeline' },
      h('div', { class: 'flex flex-wrap items-center gap-3 px-5 py-4' },
        h('div', { class: 'min-w-0 flex-1' },
          h('h2', { class: 'font-display text-lg font-semibold' }, 'Set up Forgeline'),
          h('p', { class: 'muted text-sm' }, `${o.done} of ${o.steps.length} done.` + (next ? ' Next: ' + next.title + '.' : ''))),
        next && h('a', { class: 'btn-primary', href: next.link, id: 'setup-next' }, next.action),
        h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-expanded': String(open), onclick: () => { open = !open; draw(); } }, open ? 'Hide steps' : 'Show steps')),
      h('div', { class: 'mx-5 h-1.5 overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800', role: 'progressbar', 'aria-valuenow': String(pct), 'aria-valuemin': '0', 'aria-valuemax': '100' }, bar),
      open && h('ol', { class: 'divide-y divide-slate-100 px-5 pb-2 pt-1 dark:divide-slate-800' }, o.steps.map(s => stepRow(s, admin, draw)))));
  }
  draw();
  return box;
}

/** Settings › Setup: the same steps, any time. */
export async function setupPage() {
  const el = h('div', {});
  const admin = atLeast(session.role, 'admin');
  async function draw() {
    let o;
    try { o = await get('/v1/onboarding'); } catch (e) { return mount(el, pageHeader('Setup', ''), errorBox(e.message)); }
    mount(el, pageHeader('Setup', o.finished ? 'Everything is set up or skipped. You can come back to any step.' : `${o.done} of ${o.steps.length} done. Each step opens the page that does it.`),
      h('section', { class: 'card px-5' }, h('ol', { class: 'divide-y divide-slate-100 dark:divide-slate-800', id: 'setup-steps' }, o.steps.map(s => stepRow(s, admin, draw)))),
      h('p', { class: 'hint mt-3' }, 'The same list is in a terminal: forgeline onboard --server <address>.'));
  }
  mount(el, spinner());
  await draw();
  return el;
}
