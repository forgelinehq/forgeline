// Forgeline web console: renders parsed Markdown. Every node comes from h(), so text is always
// inserted as text. See markdown.js for the parser.
import { h, mount } from './dom.js';
import { parseBlocks, parseInline, splitSections } from './markdown.js';

const inline = (s) => parseInline(s).map(p =>
  p.t === 'code' ? h('code', { class: 'rounded bg-slate-100 px-1 py-0.5 font-mono text-[0.85em] dark:bg-slate-800' }, p.text)
    : p.t === 'b' ? h('strong', { class: 'font-semibold' }, p.text)
      : p.text);

const HEADINGS = {
  1: 'mt-1 text-lg font-semibold tracking-tight',
  2: 'mt-3 text-base font-semibold tracking-tight',
  3: 'mt-2 text-sm font-semibold',
};

const ALIGN = { center: 'text-center', right: 'text-right', '': 'text-left' };

/**
 * One parsed block as a node. `codeBlock(b)` may return a node to show a code block with its own
 * actions (the assistant adds Copy and "Open as a new pipeline"); otherwise it is a plain box.
 */
export function blockNode(b, { codeBlock } = {}) {
  switch (b.t) {
    case 'h': return h('h' + b.level, { class: HEADINGS[b.level] }, inline(b.text));
    case 'p': return h('p', { class: 'whitespace-pre-line' }, inline(b.text));
    case 'ul': case 'ol': {
      const task = b.items.some(i => i.task !== null);
      return h(b.t, { class: task ? 'space-y-1' : (b.t === 'ul' ? 'list-disc space-y-1 pl-5' : 'list-decimal space-y-1 pl-5') },
        b.items.map(i => h('li', { class: i.task === null ? '' : 'flex items-start gap-2' },
          i.task !== null && h('input', { type: 'checkbox', class: 'mt-1 accent-brand-600', checked: i.task, disabled: true, 'aria-label': i.task ? 'Done' : 'Not done' }),
          h('span', {}, inline(i.text)))));
    }
    case 'quote': return h('blockquote', { class: 'whitespace-pre-line rounded-r-lg border-l-4 border-brand-500 bg-slate-50 px-3 py-2 text-slate-600 dark:bg-slate-900 dark:text-slate-300' }, inline(b.text));
    case 'code': return (codeBlock && codeBlock(b)) || h('pre', { class: 'overflow-x-auto rounded-lg bg-slate-900 p-3 font-mono text-xs leading-relaxed text-slate-100' }, b.text);
    case 'table': return h('div', { class: 'overflow-x-auto rounded-lg border border-slate-200 dark:border-slate-700' },
      h('table', { class: 'w-full border-collapse text-sm' },
        h('thead', { class: 'bg-slate-50 dark:bg-slate-800/60' }, h('tr', {}, b.head.map((c, i) =>
          h('th', { scope: 'col', class: 'whitespace-nowrap px-3 py-2 text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400 ' + ALIGN[b.align[i] || ''] }, inline(c))))),
        h('tbody', { class: 'divide-y divide-slate-200 dark:divide-slate-700' }, b.rows.map(r =>
          h('tr', {}, r.map((c, i) => h('td', { class: 'px-3 py-2 align-top ' + ALIGN[b.align[i] || ''] + (i === 0 ? ' font-medium' : '') }, inline(c))))))));
    default: return h('hr', { class: 'border-slate-200 dark:border-slate-800' });
  }
}

export function renderMarkdown(text, opts) {
  const blocks = parseBlocks(text);
  if (!blocks.length) return h('p', { class: 'muted text-sm' }, 'Nothing written yet.');
  return h('div', { class: 'space-y-3 text-sm leading-relaxed' }, blocks.map(b => blockNode(b, opts)));
}

/**
 * An assistant answer as cards: the opening text, then one card per section heading. Text that
 * has no headings is one plain block. Everything is built from parsed blocks, so it is all text.
 */
export function renderReply(text, opts) {
  const blocks = parseBlocks(text);
  if (!blocks.length) return h('p', { class: 'muted text-sm' }, 'The assistant sent an empty reply.');
  const { intro, sections } = splitSections(blocks);
  return h('div', { class: 'space-y-3 text-sm leading-relaxed', 'data-reply': '' },
    intro.length ? h('div', { class: 'space-y-3' }, intro.map(b => blockNode(b, opts))) : null,
    sections.map(sec => h('section', { class: 'overflow-hidden rounded-xl border border-slate-200 bg-white dark:border-slate-700 dark:bg-slate-900', 'data-section': sec.title },
      h('h3', { class: 'border-b border-slate-200 bg-slate-50 px-3.5 py-2 text-sm font-semibold dark:border-slate-700 dark:bg-slate-800/60' }, inline(sec.title)),
      h('div', { class: 'space-y-3 p-3.5' }, sec.blocks.map(b => blockNode(b, opts))))));
}

/** Stored text shown formatted, with a switch to the exact characters that are sent to the model. */
export function markdownBox(text, id) {
  let raw = false;
  const body = h('div', { id });
  const btn = h('button', { class: 'btn-ghost btn-sm', type: 'button', onclick: () => { raw = !raw; draw(); } });
  function draw() {
    btn.textContent = raw ? 'Show formatted' : 'Show raw text';
    mount(body, raw ? h('pre', { class: 'whitespace-pre-wrap rounded-lg bg-slate-50 p-3 text-sm dark:bg-slate-900' }, text) : renderMarkdown(text));
  }
  draw();
  return h('div', {}, h('div', { class: 'mb-1 flex justify-end' }, btn), body);
}
