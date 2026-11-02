// The Settings landing page: every setup page in one place, grouped by what it is for. Daily work stays in
// the sidebar; this is where connections, people, security and developer tools live (design note 025).
import { h, icon, pageHeader } from '../lib/dom.js';
import { session } from '../app.js';
import { visibleGroups } from '../lib/settingsnav.js';

/** A page that exists: a link with what it is for. */
function pageLink(i) {
  return h('a', { href: '#' + i.path, class: 'block rounded-lg px-3 py-2 hover:bg-slate-50 dark:hover:bg-slate-800', 'data-settings': i.path },
    h('div', { class: 'flex items-center gap-2 text-sm font-medium' }, i.label, icon('chevron', 'size-3.5 text-slate-400')),
    h('div', { class: 'muted text-xs' }, i.note));
}

/** A page that is planned: shown dashed, with no link, so nothing looks clickable that is not. */
function plannedRow(i) {
  return h('div', { class: 'rounded-lg border border-dashed border-slate-300 px-3 py-2 dark:border-slate-700' },
    h('div', { class: 'flex items-center justify-between gap-2 text-sm font-medium text-slate-500' }, i.label, h('span', { class: 'pill-idle' }, 'Coming soon')),
    h('div', { class: 'muted text-xs' }, i.note));
}

function groupCard(g) {
  const rows = g.items.map(i => h('li', {}, i.planned ? plannedRow(i) : pageLink(i)));
  return h('section', { class: 'card card-pad', 'aria-label': g.title }, h('h2', { class: 'h-section mb-3' }, g.title), h('ul', { class: 'space-y-3' }, rows));
}

export async function settingsPage() {
  return h('div', {},
    pageHeader('Settings', 'Set up what Forgeline connects to, who can use it and what is allowed. Day-to-day work is in the sidebar.'),
    h('div', { class: 'grid gap-4 md:grid-cols-2 xl:grid-cols-3', id: 'settings-groups' }, visibleGroups(session.role).map(groupCard)));
}
