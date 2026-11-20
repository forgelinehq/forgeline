// Forgeline web console: DOM helpers.
//
// Everything that reaches the page goes through h(), which only ever creates elements and text
// nodes. There is no innerHTML anywhere in the console: pipeline names, step names, log output,
// GitHub data and error messages are all untrusted, and this makes inserting them as markup
// impossible rather than merely careful.

import { ICON_SET } from './icons.js';

const SVG_NS = 'http://www.w3.org/2000/svg';

/** h('div', {class: 'x', onclick: fn}, 'text', [child, child]) */
export function h(tag, props, ...children) {
  const el = SVG_TAGS.has(tag) ? document.createElementNS(SVG_NS, tag) : document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (v === false || v == null) continue;
    if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2).toLowerCase(), v);
    else if (k === 'class') el.setAttribute('class', v);
    else if (k === 'value' && 'value' in el) el.value = v;
    else if (k === 'checked' || k === 'disabled' || k === 'selected' || k === 'readOnly') el[k] = !!v;
    else if (k === 'style') throw new Error('inline styles are blocked by the CSP; use a class or el.style from script');
    else el.setAttribute(k, v === true ? '' : String(v));
  }
  append(el, children);
  return el;
}

const SVG_TAGS = new Set(['svg', 'path', 'g', 'rect', 'circle', 'line', 'text', 'title', 'defs', 'marker', 'polygon', 'polyline', 'ellipse', 'tspan', 'pattern']);

export function append(el, children) {
  for (const c of children.flat(Infinity)) {
    if (c == null || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}

export function clear(el) { while (el.firstChild) el.removeChild(el.firstChild); return el; }

export function mount(el, ...children) { clear(el); append(el, children); return el; }

// --- icons (Lucide, generated into icons.js; nothing here comes from the server) ---

function iconChildren(name) {
  const parts = ICON_SET[name];
  if (!parts) throw new Error('unknown icon ' + name);
  return parts.map(([tag, attrs]) => h(tag, attrs));
}

/** Whether the console has an icon by this name. */
export const hasIcon = (name) => Object.prototype.hasOwnProperty.call(ICON_SET, name);

export function iconAt(name, x, y, size, cls = '') {
  return h('svg', { x: String(x), y: String(y), width: String(size), height: String(size), viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor',
    'stroke-width': '1.75', 'stroke-linecap': 'round', 'stroke-linejoin': 'round', class: cls, 'aria-hidden': 'true' }, iconChildren(name));
}

export function icon(name, cls = 'size-4') {
  return h('svg', { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '1.75', 'stroke-linecap': 'round', 'stroke-linejoin': 'round', class: cls + ' shrink-0', 'aria-hidden': 'true' }, iconChildren(name));
}

// --- formatting ---

export function relTime(iso) {
  if (!iso) return '';
  const s = Math.round((Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 5) return 'just now';
  if (s < 60) return s + 's ago';
  if (s < 3600) return Math.floor(s / 60) + 'm ago';
  if (s < 86400) return Math.floor(s / 3600) + 'h ago';
  if (s < 86400 * 30) return Math.floor(s / 86400) + 'd ago';
  return new Date(iso).toLocaleDateString();
}

export function absTime(iso) { return iso ? new Date(iso).toLocaleString() : ''; }

export function duration(startIso, endIso) {
  if (!startIso) return '';
  const end = endIso ? new Date(endIso).getTime() : Date.now();
  return fmtSeconds((end - new Date(startIso).getTime()) / 1000);
}

export function fmtSeconds(s) {
  if (s == null || isNaN(s)) return '';
  if (s < 1) return '<1s';
  if (s < 60) return Math.round(s) + 's';
  if (s < 3600) return Math.floor(s / 60) + 'm ' + Math.round(s % 60) + 's';
  return Math.floor(s / 3600) + 'h ' + Math.floor((s % 3600) / 60) + 'm';
}

export function bytes(n) {
  if (!n) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(u.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  return (n / Math.pow(1024, i)).toFixed(i ? 1 : 0) + ' ' + u[i];
}

export function pct(x) { return x == null ? '–' : Math.round(x * 100) + '%'; }

// --- status ---

const STATUS_PILL = {
  succeeded: ['pill-ok', 'Succeeded'], success: ['pill-ok', 'Success'], cached: ['pill-ok', 'Cached'],
  failed: ['pill-bad', 'Failed'], rejected: ['pill-bad', 'Rejected'],
  running: ['pill-run', 'Running'], executing: ['pill-run', 'Executing'], preparing: ['pill-run', 'Preparing'],
  queued: ['pill-wait', 'Queued'], pending: ['pill-wait', 'Pending'], proposed: ['pill-wait', 'Awaiting approval'],
  applying: ['pill-run', 'Applying'], applied: ['pill-ok', 'Applied'], cancelled: ['pill-idle', 'Withdrawn'],
  canceled: ['pill-idle', 'Canceled'], skipped: ['pill-idle', 'Skipped'], expired: ['pill-idle', 'Expired'],
  requested: ['pill-wait', 'Requested'], provisioning: ['pill-run', 'Creating'], active: ['pill-ok', 'Active'], destroying: ['pill-run', 'Deleting'], destroyed: ['pill-idle', 'Deleted'],
};

export function pill(status, label) {
  const [cls, text] = STATUS_PILL[status] || ['pill-idle', status];
  const live = status === 'applying' || status === 'running' || status === 'executing' || status === 'preparing';
  return h('span', { class: cls }, h('span', { class: 'size-1.5 rounded-full bg-current ' + (live ? 'animate-pulse-dot' : '') }), label || text);
}

export function dotClass(status) {
  return ({
    succeeded: 'bg-emerald-500', cached: 'bg-emerald-400', failed: 'bg-rose-500', running: 'bg-sky-500',
    queued: 'bg-amber-400', pending: 'bg-amber-300', canceled: 'bg-slate-400', skipped: 'bg-slate-300',
  })[status] || 'bg-slate-300';
}

export function sparkline(statuses) {
  const slots = Array.from({ length: 12 }, (_, i) => statuses[statuses.length - 12 + i]);
  return h('div', { class: 'flex items-end gap-0.5', role: 'img', 'aria-label': 'Last runs: ' + (statuses.join(', ') || 'none') },
    slots.map(s => h('span', { class: 'h-5 w-1.5 rounded-sm ' + (s ? dotClass(s) : 'bg-slate-200 dark:bg-slate-800'), title: s || '' })));
}

// --- toast and dialogs ---

let toastHost;
export function toast(message, kind = 'info') {
  if (!toastHost) {
    toastHost = h('div', { class: 'pointer-events-none fixed bottom-4 right-4 z-[60] flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-2', 'aria-live': 'polite' });
    document.body.append(toastHost);
  }
  const tone = kind === 'error' ? 'border-rose-300 bg-rose-50 text-rose-900 dark:border-rose-500/40 dark:bg-rose-950 dark:text-rose-100'
    : kind === 'ok' ? 'border-emerald-300 bg-emerald-50 text-emerald-900 dark:border-emerald-500/40 dark:bg-emerald-950 dark:text-emerald-100'
    : 'border-slate-200 bg-white text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-100';
  const el = h('div', { class: 'pointer-events-auto flex items-start gap-2 rounded-lg border px-3 py-2.5 text-sm shadow-lg ' + tone, role: kind === 'error' ? 'alert' : 'status' },
    icon(kind === 'error' ? 'alert' : kind === 'ok' ? 'check' : 'info', 'mt-0.5 size-4'), h('div', { class: 'min-w-0 break-words' }, message));
  toastHost.append(el);
  setTimeout(() => el.remove(), kind === 'error' ? 8000 : 4000);
}

/** Opens a modal. build(close) returns the dialog body; resolves when closed. */
export function modal(title, build, { wide = false, xl = false } = {}) {
  return new Promise(resolve => {
    const prev = document.activeElement;
    // Leaving the page closes it too: a dialog about the old page must not sit over the new one.
    const close = (value) => { overlay.remove(); document.removeEventListener('keydown', onKey); removeEventListener('hashchange', onLeave); prev && prev.focus && prev.focus(); resolve(value); };
    const onKey = (e) => { if (e.key === 'Escape') close(undefined); };
    const onLeave = () => close(undefined);
    const panel = h('div', { class: 'w-full ' + (xl ? 'max-w-5xl' : wide ? 'max-w-3xl' : 'max-w-lg') + ' max-h-[90vh] overflow-y-auto rounded-2xl border border-slate-200 bg-white shadow-2xl dark:border-slate-700 dark:bg-slate-900', role: 'dialog', 'aria-modal': 'true', 'aria-label': title },
      h('div', { class: 'sticky top-0 z-10 flex items-center justify-between border-b border-slate-200 bg-white/95 px-5 py-3.5 backdrop-blur dark:border-slate-800 dark:bg-slate-900/95' },
        h('h2', { class: 'font-display text-lg font-bold' }, title),
        h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Close', onclick: () => close(undefined) }, icon('x'))),
      // A form, so Enter submits (it presses the primary button) and password managers behave.
      h('form', { class: 'p-5', novalidate: true, onsubmit: (e) => { e.preventDefault(); const go = e.target.querySelector('button.btn-primary, button.btn-danger'); if (go && !go.disabled) go.click(); } }, build(close)));
    const overlay = h('div', { class: 'fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-slate-900/50 p-4 pt-[8vh] backdrop-blur-sm', onmousedown: (e) => { if (e.target === overlay) close(undefined); } }, panel);
    document.body.append(overlay);
    document.addEventListener('keydown', onKey);
    addEventListener('hashchange', onLeave);
    const first = panel.querySelector('input,textarea,select,button.btn-primary');
    first && first.focus();
  });
}

/** Opens a panel from the right side (design note 045): the pickers and editors that keep the page in view.
 *  build(close) returns the body; footer(close), when given, returns buttons that stay at the bottom. Resolves when closed. */
export function drawer(title, build, { subtitle = '', footer = null } = {}) {
  return new Promise(resolve => {
    const prev = document.activeElement;
    const close = (value) => { overlay.remove(); document.removeEventListener('keydown', onKey); removeEventListener('hashchange', onLeave); prev && prev.focus && prev.focus(); resolve(value); };
    const onKey = (e) => { if (e.key === 'Escape') close(undefined); };
    const onLeave = () => close(undefined);
    const panel = h('aside', { class: 'flex h-full w-full max-w-xl flex-col border-l border-slate-200 bg-white shadow-2xl dark:border-slate-700 dark:bg-slate-900', role: 'dialog', 'aria-modal': 'true', 'aria-label': title },
      h('div', { class: 'flex items-start justify-between gap-3 border-b border-slate-200 px-5 py-4 dark:border-slate-800' },
        h('div', { class: 'min-w-0' }, h('h2', { class: 'font-display text-lg font-bold' }, title), subtitle ? h('p', { class: 'muted mt-0.5 text-sm' }, subtitle) : null),
        h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Close', onclick: () => close(undefined) }, icon('x'))),
      h('div', { class: 'min-h-0 flex-1 overflow-y-auto p-5' }, build(close)),
      footer ? h('div', { class: 'flex flex-wrap items-center justify-end gap-2 border-t border-slate-200 bg-slate-50 px-5 py-3 dark:border-slate-800 dark:bg-slate-950' }, footer(close)) : null);
    const overlay = h('div', { class: 'fixed inset-0 z-50 flex justify-end bg-slate-900/40 backdrop-blur-[1px]', onmousedown: (e) => { if (e.target === overlay) close(undefined); } }, panel);
    document.body.append(overlay);
    document.addEventListener('keydown', onKey);
    addEventListener('hashchange', onLeave);
    const first = panel.querySelector('input,textarea,select');
    first && first.focus();
  });
}

/** A provider's or server's mark: its initials on its colours, until official logos are added where their rules allow. */
export function brandMark(kind, size = 'md') {
  const m = BRANDS[kind] || BRANDS.other;
  const sz = size === 'sm' ? 'size-7 rounded-md text-[10px]' : 'size-11 rounded-xl text-sm';
  return h('span', { class: 'inline-flex flex-none items-center justify-center font-bold ' + sz + ' ' + m[1], 'aria-hidden': 'true' }, m[0]);
}

const BRANDS = {
  anthropic: ['A', 'bg-orange-100 text-orange-900 dark:bg-orange-950 dark:text-orange-200'],
  openai: ['O', 'bg-slate-200 text-slate-900 dark:bg-slate-700 dark:text-slate-100'],
  bedrock: ['aws', 'bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200'],
  gemini: ['G', 'bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-200'],
  'azure-openai': ['Az', 'bg-sky-100 text-sky-800 dark:bg-sky-950 dark:text-sky-200'],
  'openai-compatible': ['{ }', 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-200'],
  'claude-token': ['CC', 'bg-orange-100 text-orange-900 dark:bg-orange-950 dark:text-orange-200'],
  cli: ['CC', 'bg-orange-100 text-orange-900 dark:bg-orange-950 dark:text-orange-200'],
  api: ['A', 'bg-orange-100 text-orange-900 dark:bg-orange-950 dark:text-orange-200'],
  copilot: ['Co', 'bg-slate-800 text-white dark:bg-slate-200 dark:text-slate-900'],
  mcp: ['MCP', 'bg-violet-100 text-violet-800 dark:bg-violet-950 dark:text-violet-200'],
  library: ['Lb', 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-200'],
  forgeline: ['F', 'bg-slate-900 text-white dark:bg-slate-200 dark:text-slate-900'],
  guard: ['✓', 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-200'],
  other: ['?', 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-200'],
};

/** Asks for confirmation. If typeToConfirm is set the person must type it. Resolves true or false. */
export function confirmDialog({ title, body, confirmLabel = 'Confirm', cancelLabel = 'Cancel', danger = false, typeToConfirm = null }) {
  return modal(title, (close) => {
    const input = typeToConfirm ? h('input', { class: 'input mt-3', autocomplete: 'off', 'aria-label': 'Type ' + typeToConfirm + ' to confirm' }) : null;
    const ok = h('button', { class: danger ? 'btn-danger' : 'btn-primary', type: 'button', disabled: !!typeToConfirm, onclick: () => close(true) }, confirmLabel);
    if (input) input.addEventListener('input', () => { ok.disabled = input.value !== typeToConfirm; });
    return h('div', {}, h('div', { class: 'text-sm text-slate-600 dark:text-slate-300' }, body),
      typeToConfirm && h('div', { class: 'mt-3 text-sm' }, 'Type ', h('code', { class: 'rounded bg-slate-100 px-1.5 py-0.5 font-mono text-xs dark:bg-slate-800' }, typeToConfirm), ' to confirm.'), input,
      h('div', { class: 'mt-5 flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close(false) }, cancelLabel), ok));
  }).then(v => v === true);
}

/** Asks what to do with unsaved changes before leaving a page. save() resolves true once saved. Resolves true to
 *  leave (saved or discarded), false to keep editing. */
export function leaveDialog({ what = 'this page', save }) {
  return modal('Save your changes?', (close) => {
    const err = h('div', {});
    const saveBtn = h('button', { class: 'btn-primary', type: 'button', id: 'leave-save', onclick: async () => {
      saveBtn.disabled = true;
      try { if (await save()) close(true); else saveBtn.disabled = false; } catch (e) { mount(err, errorBox(e.message)); saveBtn.disabled = false; }
    } }, 'Save and leave');
    return h('div', {}, h('p', { class: 'text-sm text-slate-600 dark:text-slate-300' }, 'You changed ' + what + ' and have not saved it. Leaving now loses the changes.'), err,
      h('div', { class: 'mt-5 flex flex-wrap justify-end gap-2' },
        h('button', { class: 'btn-secondary', type: 'button', id: 'leave-stay', onclick: () => close(false) }, 'Keep editing'),
        h('button', { class: 'btn-secondary text-rose-700 dark:text-rose-300', type: 'button', id: 'leave-discard', onclick: () => close(true) }, 'Discard changes'), saveBtn));
  }).then(v => v === true);
}

// --- small building blocks ---

export function pageHeader(title, subtitle, ...actions) {
  return h('div', { class: 'mb-6 flex flex-wrap items-start justify-between gap-3' },
    h('div', { class: 'min-w-0' }, h('h1', { class: 'h-page' }, title), subtitle && h('p', { class: 'muted mt-1 text-sm' }, subtitle)),
    h('div', { class: 'flex flex-wrap items-center gap-2' }, actions));
}

export function field(label, control, hint, id) {
  return h('div', {}, h('label', { class: 'label', for: id || control.id }, label), control, hint && h('p', { class: 'hint' }, hint));
}

export function emptyState(iconName, title, text, action) {
  return h('div', { class: 'flex flex-col items-center px-6 py-14 text-center' },
    h('div', { class: 'mb-3 rounded-full bg-slate-100 p-3 text-slate-500 dark:bg-slate-800' }, icon(iconName, 'size-6')),
    h('h3', { class: 'font-medium' }, title), text && h('p', { class: 'muted mt-1 max-w-sm text-sm' }, text), action && h('div', { class: 'mt-4' }, action));
}

export function errorBox(message) {
  return h('div', { class: 'flex items-start gap-2 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2.5 text-sm text-rose-800 dark:border-rose-500/30 dark:bg-rose-950/50 dark:text-rose-200', role: 'alert' }, icon('alert', 'mt-0.5 size-4'), h('div', { class: 'min-w-0 break-words' }, message));
}

export function notice(kind, ...children) {
  const tone = kind === 'warn' ? 'border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/30 dark:bg-amber-950/40 dark:text-amber-200'
    : kind === 'ok' ? 'border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-500/30 dark:bg-emerald-950/40 dark:text-emerald-200'
    : 'border-sky-200 bg-sky-50 text-sky-900 dark:border-sky-500/30 dark:bg-sky-950/40 dark:text-sky-200';
  return h('div', { class: 'flex items-start gap-2 rounded-lg border px-3 py-2.5 text-sm ' + tone }, icon(kind === 'warn' ? 'alert' : kind === 'ok' ? 'check' : 'info', 'mt-0.5 size-4'), h('div', { class: 'min-w-0 break-words' }, children));
}

export function spinner() { return h('div', { class: 'flex justify-center py-16', role: 'status', 'aria-label': 'Loading' }, h('div', { class: 'size-6 animate-spin rounded-full border-2 border-slate-300 border-t-brand-600' })); }

export function copyButton(text, label = 'Copy') {
  const b = h('button', { class: 'btn-secondary btn-sm', type: 'button', onclick: async () => {
    try { await navigator.clipboard.writeText(text); toast('Copied', 'ok'); } catch { toast('Copy is blocked in this browser; select the text instead', 'error'); }
  } }, icon('copy', 'size-3.5'), label);
  return b;
}

export function kpi(label, value, sub, tone) {
  const toneCls = tone === 'bad' ? 'text-rose-600 dark:text-rose-400' : tone === 'ok' ? 'text-emerald-600 dark:text-emerald-400' : tone === 'run' ? 'text-sky-600 dark:text-sky-400' : '';
  return h('div', { class: 'card card-pad' }, h('div', { class: 'h-section' }, label),
    h('div', { class: 'mt-2 font-display text-3xl font-bold tabular-nums ' + toneCls }, value), sub && h('div', { class: 'muted mt-1 text-xs' }, sub));
}
