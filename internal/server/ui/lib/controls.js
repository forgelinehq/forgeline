// Forgeline web console: controls used by panels and drawers (design note 048, step 3): tabs with icons, so a panel
// shows one part at a time instead of a long column, and chips for short lists (fields, choices, people) instead of
// comma-separated text.
import { h, icon, mount } from './dom.js';

/**
 * tabs({ items: [{ id, label, icon, badge?, render() }], active, onchange, id }) returns { el, show(id) }.
 * Only the open tab is built, so a panel stays light; `onchange(id)` lets the caller remember the tab.
 */
export function tabs({ items, active, onchange = () => {}, id = '', compact = false }) {
  // compact: equal widths with the icon above the label, for narrow panels.
  const bar = h('div', { class: (compact ? 'grid auto-cols-fr grid-flow-col' : 'flex flex-wrap gap-0.5') + ' border-b border-slate-200 dark:border-slate-800', role: 'tablist', id: id ? id + '-tabs' : null });
  const body = h('div', { class: 'space-y-3 pt-3', role: 'tabpanel', id: id ? id + '-body' : null });
  let cur = items.some(t => t.id === active) ? active : items[0]?.id;
  const show = (tid) => {
    cur = tid;
    mount(bar, items.map(t => h('button', { type: 'button', role: 'tab', 'aria-selected': String(t.id === cur), 'data-panel-tab': t.id,
      class: '-mb-px relative flex items-center whitespace-nowrap border-b-2 font-medium transition-colors ' + (compact ? 'flex-col gap-1 px-1 pb-2 pt-1.5 text-[11px] ' : 'gap-1.5 px-2.5 py-2 text-xs ') + (t.id === cur
        ? 'border-brand-600 text-brand-700 dark:border-brand-400 dark:text-brand-200'
        : 'border-transparent text-slate-500 hover:text-slate-900 dark:text-slate-400 dark:hover:text-white'),
      onclick: () => { show(t.id); onchange(t.id); } }, icon(t.icon, compact ? 'size-4' : 'size-3.5'), t.label,
      t.badge ? h('span', { class: (compact ? 'absolute right-1 top-0.5 ' : '') + 'rounded-full bg-brand-100 px-1.5 text-[10px] tabular-nums text-brand-800 dark:bg-brand-500/20 dark:text-brand-200' }, String(t.badge)) : null)));
    const t = items.find(x => x.id === cur);
    mount(body, t ? t.render() : null);
  };
  show(cur);
  return { el: h('div', {}, bar, body), show };
}

/**
 * chipInput({ id, label, values, placeholder, hint, lower, max, onchange }) returns an element: the values as chips with
 * a remove button each, and a box that adds one on Enter or comma. Backspace in the empty box removes the last chip.
 */
export function chipInput({ id, label, values = [], placeholder = '', hint = '', lower = false, max = 20, onchange }) {
  let vals = [...values];
  const wrap = h('div', { class: 'flex min-h-[2.5rem] flex-wrap items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-2 py-1.5 focus-within:border-brand-500 focus-within:ring-2 focus-within:ring-brand-500/20 dark:border-slate-700 dark:bg-slate-900', 'data-chips': id });
  const input = h('input', { id, class: 'min-w-24 flex-1 bg-transparent py-0.5 text-sm placeholder:text-slate-400 focus:outline-none', placeholder: vals.length ? '' : placeholder, autocomplete: 'off', spellcheck: 'false' });
  const commit = () => onchange([...vals]);
  const addFrom = (text) => {
    let added = false;
    for (let v of String(text).split(',')) {
      v = v.trim();
      if (lower) v = v.toLowerCase();
      if (v && !vals.includes(v) && vals.length < max) { vals.push(v); added = true; }
    }
    input.value = '';
    if (added) { commit(); draw(); }
  };
  const draw = () => {
    mount(wrap, vals.map((v, i) => h('span', { class: 'inline-flex items-center gap-1 rounded-full bg-slate-100 py-0.5 pl-2.5 pr-1 text-xs font-medium text-slate-700 dark:bg-slate-800 dark:text-slate-200', 'data-chip': v },
      v, h('button', { type: 'button', class: 'rounded-full p-0.5 hover:bg-slate-200 dark:hover:bg-slate-700', 'aria-label': 'Remove ' + v, onclick: () => { vals.splice(i, 1); commit(); draw(); input.focus(); } }, icon('x', 'size-3')))), input);
    input.placeholder = vals.length ? '' : placeholder;
  };
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ',') { e.preventDefault(); addFrom(input.value); }
    else if (e.key === 'Backspace' && input.value === '' && vals.length) { vals.pop(); commit(); draw(); }
  });
  input.addEventListener('blur', () => { if (input.value.trim()) addFrom(input.value); });
  wrap.addEventListener('click', (e) => { if (e.target === wrap) input.focus(); });
  draw();
  return h('div', {}, label ? h('label', { class: 'label', for: id }, label) : null, wrap, hint ? h('p', { class: 'hint' }, hint) : null);
}

/** A choice between a few options as cards with an icon each, instead of a select. */
export function choiceCards({ id, value, options, onchange }) {
  const box = h('div', { class: 'grid gap-2 sm:grid-cols-2', role: 'radiogroup', id });
  const draw = () => mount(box, options.map(o => h('button', { type: 'button', role: 'radio', 'aria-checked': String(o.value === value), 'data-choice': o.value,
    class: 'flex items-start gap-2.5 rounded-xl border p-3 text-left ' + (o.value === value ? 'border-brand-400 bg-brand-50/70 dark:border-brand-500/60 dark:bg-brand-500/10' : 'border-slate-200 hover:border-slate-300 dark:border-slate-700 dark:hover:border-slate-600'),
    onclick: () => { value = o.value; draw(); onchange(o.value); } },
    h('span', { class: 'mt-0.5 ' + (o.value === value ? 'text-brand-700 dark:text-brand-200' : 'text-slate-500') }, icon(o.icon, 'size-4')),
    h('span', { class: 'min-w-0' }, h('span', { class: 'block text-sm font-medium' }, o.label), o.note ? h('span', { class: 'muted block text-xs' }, o.note) : null))));
  draw();
  return box;
}
