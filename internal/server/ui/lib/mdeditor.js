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
  const input = h('textarea', { id, class: 'input h-64 resize-y font-mono text-xs leading-relaxed', maxlength: maxlength ? String(maxlength) : null, placeholder, spellcheck: 'false' }, value);
  const preview = h('div', { class: 'h-64 overflow-auto rounded-lg border border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900', 'aria-label': 'Formatted preview' });
  const panes = h('div', { class: 'grid gap-3' }, input, preview);
  const checks = h('ul', { class: 'space-y-1 text-xs', 'aria-live': 'polite' });
  const count = h('span', { class: 'muted flex-none whitespace-nowrap text-xs tabular-nums' });

  const MODES = [['write', 'Write'], ['split', 'Split'], ['read', 'Preview']];
  const seg = h('div', { class: 'inline-flex overflow-hidden rounded-lg border border-slate-300 text-xs dark:border-slate-700', role: 'group', 'aria-label': 'Editor view' },
    MODES.map(([k, label]) => h('button', { type: 'button', 'data-mode': k, onclick: () => setMode(k) }, label)));
  function setMode(m) {
    input.classList.toggle('hidden', m === 'read');
    preview.classList.toggle('hidden', m === 'write');
    panes.setAttribute('class', m === 'split' ? 'grid gap-3 sm:grid-cols-2' : 'grid gap-3');
    for (const b of seg.children) {
      const on = b.dataset.mode === m;
      b.setAttribute('aria-pressed', String(on));
      b.setAttribute('class', 'px-2.5 py-1 ' + (on ? 'bg-slate-900 text-white dark:bg-slate-100 dark:text-slate-900' : 'text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800'));
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
  const tool = (label, title, fn) => h('button', { type: 'button', class: 'min-w-8 rounded px-2 py-1 font-mono text-xs text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800', title, 'aria-label': title, onclick: fn }, label);
  const bar = h('div', { class: 'mb-2 flex flex-wrap items-center gap-1', role: 'toolbar', 'aria-label': 'Formatting' },
    tool('H', 'Heading', () => prefix('## ')), tool('B', 'Bold', () => wrap('**', '**', 'bold')), tool('</>', 'Code', () => wrap('`', '`', 'code')),
    tool('•', 'List', () => prefix('- ')), tool('1.', 'Numbered list', () => prefix('1. ')), tool('[ ]', 'Checklist', () => prefix('- [ ] ')), tool('>', 'Rule or callout', () => prefix('> ')),
    h('span', { class: 'flex-1' }), seg);

  input.addEventListener('input', refresh);
  setMode('split');
  refresh();
  const el = h('div', {}, bar, panes, h('div', { class: 'mt-2 flex items-start justify-between gap-3' }, checks, count));
  return { el, input, refresh };
}
