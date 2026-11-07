// Forgeline web console: a Markdown editor for agent instructions and skill steps.
// A textarea with a toolbar, a live preview and advisory checks. The value is the same plain
// text as before; the editor only helps write it.
import { h, icon, mount } from './dom.js';
import { renderMarkdown } from './mdview.js';

const TONE = {
  ok: 'text-emerald-700 dark:text-emerald-300',
  warn: 'text-amber-700 dark:text-amber-300',
  bad: 'text-rose-700 dark:text-rose-300',
};

/**
 * mdEditor({id, value, maxlength, placeholder, check}) returns {el, input, refresh}.
 * `input` is the textarea (read .value from it). `check(text)` returns [{level, text}]; call
 * `refresh()` when something the check depends on changes, such as the agent's tier.
 */
export function mdEditor({ id, value = '', maxlength, placeholder, check }) {
  const input = h('textarea', { id, class: 'block h-72 w-full resize-y bg-transparent p-3.5 font-mono text-[13px] leading-relaxed placeholder:text-slate-400 focus:outline-none', maxlength: maxlength ? String(maxlength) : null, placeholder, spellcheck: 'false' }, value);
  const preview = h('div', { class: 'h-72 overflow-auto bg-slate-50/70 p-4 text-sm dark:bg-slate-950/40', 'aria-label': 'Formatted preview' });
  const panes = h('div', { class: 'grid' }, input, preview);
  const checks = h('ul', { class: 'space-y-1 text-xs', 'aria-live': 'polite' });
  const count = h('span', { class: 'muted flex-none whitespace-nowrap text-xs tabular-nums' });

  const MODES = [['write', 'Write'], ['split', 'Split'], ['read', 'Preview']];
  const seg = h('div', { class: 'inline-flex rounded-lg bg-slate-200/70 p-0.5 text-xs dark:bg-slate-800', role: 'group', 'aria-label': 'Editor view' },
    MODES.map(([k, label]) => h('button', { type: 'button', 'data-mode': k, onclick: () => setMode(k) }, label)));
  function setMode(m) {
    input.classList.toggle('hidden', m === 'read');
    preview.classList.toggle('hidden', m === 'write');
    panes.setAttribute('class', m === 'split' ? 'grid sm:grid-cols-2 sm:divide-x sm:divide-slate-200 dark:sm:divide-slate-800' : 'grid');
    for (const b of seg.children) {
      const on = b.dataset.mode === m;
      b.setAttribute('aria-pressed', String(on));
      b.setAttribute('class', 'rounded-md px-2.5 py-1 font-medium ' + (on ? 'bg-white text-slate-900 shadow-sm dark:bg-slate-700 dark:text-white' : 'text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-white'));
    }
  }

  function refresh() {
    mount(preview, renderMarkdown(input.value));
    count.textContent = input.value.length + (maxlength ? ' / ' + maxlength : '');
    mount(checks, (check ? check(input.value) : []).map(r =>
      h('li', { class: 'flex items-start gap-1.5 ' + TONE[r.level] }, icon(r.level === 'ok' ? 'check' : 'alert', 'mt-0.5 size-3.5 flex-none'), h('span', {}, r.text))));
  }

  function edit(fn) { input.focus(); fn(); input.dispatchEvent(new Event('input', { bubbles: true })); }
  function wrap(before, after, fallback) {
    edit(() => {
      const s = input.selectionStart, e = input.selectionEnd;
      input.setRangeText(before + (input.value.slice(s, e) || fallback) + after, s, e, 'select');
    });
  }
  function prefix(p) {
    edit(() => {
      const s = input.selectionStart, ls = input.value.lastIndexOf('\n', s - 1) + 1;
      input.setRangeText(p, ls, ls, 'preserve');
      input.setSelectionRange(s + p.length, s + p.length);
    });
  }
  const tool = (label, title, fn) => h('button', { type: 'button', class: 'min-w-8 rounded-md px-2 py-1 font-mono text-xs font-medium text-slate-600 hover:bg-white hover:text-slate-900 hover:shadow-sm dark:text-slate-300 dark:hover:bg-slate-700 dark:hover:text-white', title, 'aria-label': title, onclick: fn }, label);
  const bar = h('div', { class: 'flex flex-wrap items-center gap-0.5 border-b border-slate-200 bg-slate-50 px-2 py-1.5 dark:border-slate-800 dark:bg-slate-900', role: 'toolbar', 'aria-label': 'Formatting' },
    tool('H', 'Heading', () => prefix('## ')), tool('B', 'Bold', () => wrap('**', '**', 'bold')), tool('</>', 'Code', () => wrap('`', '`', 'code')),
    tool('•', 'List', () => prefix('- ')), tool('1.', 'Numbered list', () => prefix('1. ')), tool('[ ]', 'Checklist', () => prefix('- [ ] ')), tool('>', 'Rule or callout', () => prefix('> ')),
    h('span', { class: 'flex-1' }), seg);

  input.addEventListener('input', refresh);
  setMode('split');
  refresh();
  const el = h('div', {},
    h('div', { class: 'overflow-hidden rounded-xl border border-slate-300 bg-white shadow-sm focus-within:border-brand-500 focus-within:ring-2 focus-within:ring-brand-500/25 dark:border-slate-700 dark:bg-slate-900' }, bar, panes),
    h('div', { class: 'mt-2 flex items-start justify-between gap-3' }, checks, count));
  return { el, input, refresh };
}
