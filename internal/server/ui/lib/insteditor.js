// Forgeline web console: the instructions editor (design note 048). The instructions are written in sections (who
// it is, goal, steps, rules, output, when to stop, and any you add), each with its own editor: a list of steps or
// rules, presets for the output, plain text for the rest. They are one Markdown text underneath, which the
// "What the model receives" view shows exactly, and Markdown mode edits directly. Nothing reaches the page as markup.
import { h, icon, mount, clear } from './dom.js';
import { mdEditor } from './mdeditor.js';
import { renderMarkdown } from './mdview.js';
import { SECTIONS, parse, compose, withStandard, addSection, addCustom, listItems, listBody, sectionInfo } from './instructions.js';

const TONE = { ok: 'text-emerald-700 dark:text-emerald-300', warn: 'text-amber-700 dark:text-amber-300', bad: 'text-rose-700 dark:text-rose-300' };

/** A textarea that grows with what is written in it. */
function growing(attrs, value, oninput) {
  const t = h('textarea', { rows: '2', class: 'block w-full resize-none rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm leading-relaxed placeholder:text-slate-400 focus:border-brand-400 focus:outline-none focus:ring-2 focus:ring-brand-500/20 dark:border-slate-700 dark:bg-slate-900', ...attrs }, value);
  const fit = () => { t.style.height = 'auto'; t.style.height = Math.min(t.scrollHeight + 2, 420) + 'px'; };
  t.addEventListener('input', () => { fit(); oninput(t.value); });
  requestAnimationFrame(fit);
  return t;
}

/** Steps or rules as items: a row each, with up, down and remove, and Enter to add the next. */
function itemList(sec, kind, items, onchange) {
  const box = h('div', { class: 'space-y-1.5', 'data-items': sec.id });
  let rows = items.length ? [...items] : [''];
  const commit = () => onchange(listBody(rows, kind));
  const draw = (focusAt = -1) => {
    mount(box,
      rows.map((v, i) => {
        const input = h('input', { class: 'input py-1.5 text-sm', value: v, placeholder: i === 0 ? (sectionInfo(sec.id)?.example || '') : '', 'aria-label': (sec.id === 'steps' ? 'Step ' : sec.id === 'escalate' ? 'Case ' : 'Rule ') + (i + 1),
          'data-item': String(i),
          oninput: (e) => { rows[i] = e.target.value; commit(); },
          onkeydown: (e) => {
            if (e.key === 'Enter') { e.preventDefault(); rows.splice(i + 1, 0, ''); commit(); draw(i + 1); }
            else if (e.key === 'Backspace' && e.target.value === '' && rows.length > 1) { e.preventDefault(); rows.splice(i, 1); commit(); draw(Math.max(0, i - 1)); }
          } });
        const move = (to) => { const [x] = rows.splice(i, 1); rows.splice(to, 0, x); commit(); draw(to); };
        return h('div', { class: 'group flex items-center gap-2' },
          h('span', { class: 'w-5 flex-none text-right text-xs font-semibold tabular-nums text-slate-400' }, kind === 'ordered' ? (i + 1) + '.' : '•'),
          input,
          h('div', { class: 'flex flex-none items-center opacity-60 group-focus-within:opacity-100 group-hover:opacity-100' },
            h('button', { type: 'button', class: 'btn-ghost p-1', title: 'Move up', 'aria-label': 'Move up', disabled: i === 0, onclick: () => move(i - 1) }, icon('up', 'size-3.5')),
            h('button', { type: 'button', class: 'btn-ghost p-1', title: 'Move down', 'aria-label': 'Move down', disabled: i === rows.length - 1, onclick: () => move(i + 1) }, icon('down', 'size-3.5')),
            h('button', { type: 'button', class: 'btn-ghost p-1 text-rose-600 dark:text-rose-400', title: 'Remove', 'aria-label': 'Remove', onclick: () => { rows.splice(i, 1); if (!rows.length) rows = ['']; commit(); draw(Math.min(i, rows.length - 1)); } }, icon('x', 'size-3.5'))));
      }),
      h('button', { type: 'button', class: 'btn-ghost btn-sm ml-7', 'data-add-item': sec.id, onclick: () => { rows.push(''); commit(); draw(rows.length - 1); } },
        icon('plus', 'size-3.5'), sec.id === 'steps' ? 'Add a step' : sec.id === 'escalate' ? 'Add a case' : 'Add a rule'));
    if (focusAt >= 0) box.querySelector('[data-item="' + focusAt + '"]')?.focus();
  };
  draw();
  return box;
}

/**
 * instructionsEditor({ value, maxlength, check, onchange }) returns { el, value(), refresh() }.
 * `check(text)` returns [{ level, text }] about the whole text; `onchange(text)` is called on every edit.
 */
export function instructionsEditor({ value = '', maxlength = 4096, check, onchange = () => {} }) {
  let text = value;
  let sections = withStandard(parse(text));
  let mode = 'sections';
  const body = h('div', { id: 'ins-body' });
  const checks = h('ul', { class: 'space-y-1 text-xs', 'aria-live': 'polite', id: 'ins-checks' });
  const count = h('span', { class: 'muted flex-none whitespace-nowrap text-xs tabular-nums', id: 'ins-count' });
  const preview = h('div', { class: 'max-h-96 overflow-auto rounded-lg border border-slate-200 bg-slate-50/70 p-4 dark:border-slate-800 dark:bg-slate-950/40', id: 'ins-preview' });
  const md = mdEditor({ id: 'agent-ins', value: text, maxlength, height: 'h-72', mode: 'split', check });

  const changed = () => {
    if (mode === 'sections') text = compose(sections);
    onchange(text);
    refresh();
  };
  function refresh() {
    count.textContent = text.length + ' / ' + maxlength;
    count.classList.toggle('text-rose-600', text.length > maxlength);
    mount(checks, (check ? check(text) : []).map(r => h('li', { class: 'flex items-start gap-1.5 ' + TONE[r.level] }, icon(r.level === 'ok' ? 'check' : 'alert', 'mt-0.5 size-3.5 flex-none'), h('span', {}, r.text))));
    if (!preview.closest('details')?.open) return;
    mount(preview, renderMarkdown(text));
  }

  function sectionBlock(s, i) {
    const info = sectionInfo(s.id);
    const setBody = (b) => { sections[i] = { ...sections[i], body: b }; changed(); };
    const items = info?.list ? listItems(s.body, info.list) : null;
    let editor;
    if (items) editor = itemList(s, info.list, items, setBody);
    else editor = growing({ 'data-section-text': s.key, placeholder: info?.example || 'Write this part in plain words.', 'aria-label': info?.label || s.title }, s.body, setBody);
    const title = s.id === 'custom'
      ? h('input', { class: 'w-full rounded bg-transparent font-semibold focus:outline-none focus:ring-2 focus:ring-brand-500/25', value: s.title, 'aria-label': 'Section title', 'data-section-title': s.key,
          oninput: (e) => { sections[i] = { ...sections[i], title: e.target.value }; changed(); } })
      : h('h3', { class: 'font-semibold' }, info.label, info.title && s.title && s.title !== info.title ? h('span', { class: 'muted ml-2 text-xs font-normal' }, '“' + s.title + '”') : null);
    const removable = s.id === 'custom' || info?.optional;
    return h('section', { class: 'flex gap-3 rounded-xl border border-slate-200 bg-white p-3.5 dark:border-slate-800 dark:bg-slate-900/60', 'data-section': s.key },
      h('span', { class: 'mt-0.5 flex size-8 flex-none items-center justify-center rounded-lg ' + (s.body.trim() ? 'bg-brand-50 text-brand-700 dark:bg-brand-500/15 dark:text-brand-200' : 'bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-400') },
        icon(info?.icon || 'note', 'size-4')),
      h('div', { class: 'min-w-0 flex-1 space-y-2' },
        h('div', { class: 'flex items-start justify-between gap-2' },
          h('div', { class: 'min-w-0 flex-1' }, title, h('p', { class: 'muted text-xs' }, info?.hint || 'A part of your own, written as its own heading.')),
          removable ? h('button', { type: 'button', class: 'btn-ghost p-1 text-slate-400 hover:text-rose-600', title: 'Remove this section', 'aria-label': 'Remove this section',
            onclick: () => { sections.splice(i, 1); changed(); draw(); } }, icon('trash', 'size-3.5')) : null),
        editor,
        info?.presets ? h('div', { class: 'flex flex-wrap items-center gap-1.5', 'data-presets': s.id }, h('span', { class: 'muted text-xs' }, 'Start from:'),
          info.presets.map(([label, txt]) => h('button', { type: 'button', class: 'rounded-full border border-slate-200 px-2.5 py-0.5 text-xs text-slate-600 hover:border-brand-400 hover:text-brand-700 dark:border-slate-700 dark:text-slate-300',
            onclick: () => { setBody(txt); draw(); } }, label))) : null,
        info?.list && !items && s.body.trim() ? h('p', { class: 'muted text-xs' }, 'Written as free text. Start each line with ' + (info.list === 'ordered' ? '"1. ", "2. "' : '"- "') + ' to edit it as a list.') : null));
  }

  function addRow() {
    const missing = SECTIONS.filter(x => x.optional && !sections.some(s => s.id === x.id));
    const own = h('input', { class: 'input w-48 py-1 text-sm', placeholder: 'A section of your own', 'aria-label': 'Title of a new section', id: 'ins-own',
      onkeydown: (e) => { if (e.key === 'Enter') { e.preventDefault(); add(); } } });
    const add = () => { if (!own.value.trim()) return; sections = addCustom(sections, own.value); changed(); draw(); body.querySelector('[data-section]:last-of-type textarea')?.focus(); };
    return h('div', { class: 'flex flex-wrap items-center gap-2 rounded-xl border border-dashed border-slate-300 px-3.5 py-2.5 dark:border-slate-700', id: 'ins-add' },
      h('span', { class: 'muted text-xs font-medium' }, 'Add a section'),
      missing.map(x => h('button', { type: 'button', class: 'inline-flex items-center gap-1.5 rounded-full border border-slate-200 px-2.5 py-1 text-xs hover:border-brand-400 dark:border-slate-700', 'data-add-section': x.id,
        onclick: () => { sections = addSection(sections, x.id); draw(); body.querySelector('[data-section="' + x.id + '"] textarea')?.focus(); } }, icon(x.icon, 'size-3.5'), x.label)),
      h('span', { class: 'flex items-center gap-1.5' }, own, h('button', { type: 'button', class: 'btn-secondary btn-sm', onclick: add }, icon('plus', 'size-3.5'), 'Add')));
  }

  const segBtn = (k, label, ic) => h('button', { type: 'button', 'data-ins-mode': k, 'aria-pressed': String(mode === k), onclick: () => setMode(k),
    class: 'inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium ' + (mode === k ? 'bg-white text-slate-900 shadow-sm dark:bg-slate-700 dark:text-white' : 'text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-white') }, icon(ic, 'size-3.5'), label);
  const seg = h('div', { class: 'inline-flex rounded-lg bg-slate-200/70 p-0.5 dark:bg-slate-800', role: 'group', 'aria-label': 'How to edit' });

  function setMode(m) {
    if (m === mode) return;
    if (m === 'markdown') { md.input.value = text; md.refresh(); }
    else { text = md.input.value; sections = withStandard(parse(text)); }
    mode = m;
    draw();
  }
  md.input.addEventListener('input', () => { text = md.input.value; onchange(text); refresh(); });

  function draw() {
    mount(seg, segBtn('sections', 'Sections', 'layers'), segBtn('markdown', 'Markdown', 'code'));
    if (mode === 'markdown') mount(body, md.el);
    else mount(body, h('div', { class: 'space-y-2.5' }, sections.map(sectionBlock), addRow()));
    refresh();
  }

  const details = h('details', { class: 'group', id: 'ins-what-model-gets', ontoggle: () => refresh() },
    h('summary', { class: 'flex cursor-pointer list-none items-center gap-1.5 text-xs font-medium text-slate-600 hover:text-slate-900 dark:text-slate-300' },
      icon('chevron', 'size-3.5 transition-transform group-open:rotate-90'), icon('eye', 'size-3.5'), 'What the model receives'),
    h('div', { class: 'mt-2' }, preview));

  const el = h('div', { class: 'space-y-3' },
    h('div', { class: 'flex flex-wrap items-center justify-between gap-2' }, h('p', { class: 'muted text-xs' }, 'Fill in the parts that matter; empty ones are left out.'), seg),
    body,
    details,
    h('div', { class: 'flex items-start justify-between gap-3' }, checks, count));
  draw();
  return { el, value: () => (mode === 'markdown' ? md.input.value : text), refresh };
}
