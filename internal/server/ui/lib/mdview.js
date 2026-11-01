// Forgeline web console: renders parsed Markdown. Every node comes from h(), so text is always
// inserted as text. See markdown.js for the parser.
import { h, mount } from './dom.js';
import { parseBlocks, parseInline } from './markdown.js';

const inline = (s) => parseInline(s).map(p =>
  p.t === 'code' ? h('code', { class: 'rounded bg-slate-100 px-1 py-0.5 font-mono text-[0.85em] dark:bg-slate-800' }, p.text)
    : p.t === 'b' ? h('strong', { class: 'font-semibold' }, p.text)
      : p.text);

const HEADINGS = {
  1: 'mt-1 text-lg font-semibold tracking-tight',
  2: 'mt-3 text-base font-semibold tracking-tight',
  3: 'mt-2 text-sm font-semibold',
};

export function renderMarkdown(text) {
  const blocks = parseBlocks(text);
  if (!blocks.length) return h('p', { class: 'muted text-sm' }, 'Nothing written yet.');
  return h('div', { class: 'space-y-3 text-sm leading-relaxed' }, blocks.map(b => {
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
      case 'code': return h('pre', { class: 'overflow-x-auto rounded-lg bg-slate-900 p-3 font-mono text-xs leading-relaxed text-slate-100' }, b.text);
      default: return h('hr', { class: 'border-slate-200 dark:border-slate-800' });
    }
  }));
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
