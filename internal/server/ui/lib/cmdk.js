// Forgeline web console: the search box (⌘K or Ctrl+K, or the Search button). Type to find a page, an agent, a
// workflow, a team, a skill, a pipeline, a mission or a blueprint; the arrows move, Enter opens, Escape closes. What
// it finds comes from lib/palette.js; the lists are read when it opens and kept for a minute.
import { h, icon, mount } from './dom.js';
import { get } from './api.js';
import { navigate } from './router.js';
import { pageEntries, actionEntries, thingEntries, search } from './palette.js';

const KIND = { page: 'Page', action: 'Create', agent: 'Agent', workflow: 'Workflow', team: 'Team', skill: 'Skill', pipeline: 'Pipeline', mission: 'Mission', blueprint: 'Blueprint' };
let cache = null, cachedAt = 0, cachedRole = '';

async function things(role) {
  if (cache && cachedRole === role && Date.now() - cachedAt < 60000) return cache;
  const dev = role === 'developer' || role === 'admin';
  const read = (path, key) => get(path).then(r => r[key]).catch(() => null);
  const [agents, workflows, teams, skills, pipelines, missions, blueprints] = await Promise.all([
    dev ? read('/v1/ai/agents', 'agents') : null, dev ? read('/v1/ai/workflows', 'workflows') : null, dev ? read('/v1/ai/teams', 'teams') : null,
    dev ? read('/v1/ai/skills', 'skills') : null, read('/v1/pipelines', 'pipelines'), dev ? read('/v1/missions', 'missions') : null, dev ? read('/v1/blueprints', 'blueprints') : null]);
  cache = thingEntries({ agents, workflows, teams, skills, pipelines, missions, blueprints });
  cachedAt = Date.now(); cachedRole = role;
  return cache;
}

let open = false;

/** Opens the search box for a role. */
export async function openSearch(role) {
  if (open) return;
  open = true;
  const prev = document.activeElement;
  let entries = [...pageEntries(role), ...actionEntries(role)];
  let results = [], at = 0;
  const input = h('input', { id: 'cmdk-input', type: 'text', class: 'w-full bg-transparent py-3.5 text-base placeholder:text-slate-400 focus:outline-none', placeholder: 'Search pages, agents, workflows, pipelines…',
    autocomplete: 'off', spellcheck: 'false', role: 'combobox', 'aria-expanded': 'true', 'aria-controls': 'cmdk-list', 'aria-label': 'Search' });
  const list = h('ul', { id: 'cmdk-list', role: 'listbox', class: 'max-h-[55vh] overflow-y-auto p-2' });
  const status = h('span', { class: 'muted text-xs', id: 'cmdk-status' }, 'Loading your agents, workflows and pipelines…');
  const close = (go) => {
    open = false;
    overlay.remove();
    document.removeEventListener('keydown', onKey, true);
    if (go) navigate(go.path); else prev && prev.focus && prev.focus();
  };
  const draw = () => {
    results = search(entries, input.value, 12);
    at = Math.min(at, Math.max(results.length - 1, 0));
    mount(list, results.length ? results.map((e, i) => h('li', { role: 'option', 'aria-selected': String(i === at), id: 'cmdk-' + i, 'data-result': e.path,
      class: 'flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2 ' + (i === at ? 'bg-brand-50 text-brand-900 dark:bg-brand-500/15 dark:text-white' : 'hover:bg-slate-50 dark:hover:bg-slate-800/60'),
      onmousemove: () => { if (at !== i) { at = i; draw(); } }, onclick: () => close(e) },
      h('span', { class: 'flex size-8 flex-none items-center justify-center rounded-lg ' + (i === at ? 'bg-white text-brand-700 dark:bg-slate-900 dark:text-brand-200' : 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300') }, icon(e.icon || 'search', 'size-4')),
      h('span', { class: 'min-w-0 flex-1' }, h('span', { class: 'block truncate text-sm font-medium' }, e.label), h('span', { class: 'muted block truncate text-xs' }, e.where)),
      h('span', { class: 'muted flex-none text-[11px] uppercase tracking-wide' }, KIND[e.kind])))
      : h('li', { class: 'muted px-3 py-8 text-center text-sm', id: 'cmdk-empty' }, 'Nothing matches "' + input.value.trim() + '". Try a shorter word.'));
    input.setAttribute('aria-activedescendant', results.length ? 'cmdk-' + at : '');
    list.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' });
  };
  const onKey = (e) => {
    if (e.key === 'Escape') { e.preventDefault(); close(null); }
    else if (e.key === 'ArrowDown') { e.preventDefault(); at = Math.min(at + 1, results.length - 1); draw(); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); at = Math.max(at - 1, 0); draw(); }
    else if (e.key === 'Enter' && results[at]) { e.preventDefault(); close(results[at]); }
  };
  input.addEventListener('input', () => { at = 0; draw(); });
  const panel = h('div', { class: 'w-full max-w-xl overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl dark:border-slate-700 dark:bg-slate-900', role: 'dialog', 'aria-modal': 'true', 'aria-label': 'Search' },
    h('div', { class: 'flex items-center gap-3 border-b border-slate-200 px-4 dark:border-slate-800' }, icon('search', 'size-5 text-slate-400'), input,
      h('kbd', { class: 'rounded border border-slate-200 px-1.5 py-0.5 font-mono text-[10px] text-slate-500 dark:border-slate-700' }, 'Esc')),
    list,
    h('div', { class: 'flex items-center justify-between gap-3 border-t border-slate-200 px-4 py-2 dark:border-slate-800' }, status,
      h('span', { class: 'muted hidden text-xs sm:block' }, '↑ ↓ to move · Enter to open')));
  const overlay = h('div', { class: 'fixed inset-0 z-50 flex items-start justify-center bg-slate-900/40 px-4 pt-[12vh] backdrop-blur-[1px]', id: 'cmdk',
    onmousedown: (e) => { if (e.target === overlay) close(null); } }, panel);
  document.body.append(overlay);
  document.addEventListener('keydown', onKey, true);
  draw();
  input.focus();
  const more = await things(role);
  if (!open) return;
  entries = [...entries, ...more];
  status.textContent = more.length ? 'Searching pages and ' + more.length + (more.length === 1 ? ' thing' : ' things') + ' you can open' : 'Searching pages and shortcuts';
  draw();
}

/** Whether a key press asks for the search box: ⌘K, Ctrl+K, or / outside a text field. */
export function isSearchKey(e) {
  if ((e.metaKey || e.ctrlKey) && !e.altKey && e.key.toLowerCase() === 'k') return true;
  const t = e.target;
  const typing = t && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName));
  return e.key === '/' && !typing && !e.metaKey && !e.ctrlKey && !e.altKey;
}
