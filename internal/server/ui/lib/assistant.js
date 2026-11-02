// The assistant panel: a read-only helper that answers questions about the page you are on.
// It cannot change anything; a pipeline it drafts is text until you save it yourself.
import { h, icon, mount, errorBox, notice, copyButton, toast } from './dom.js';
import { get, post, atLeast } from './api.js';
import { currentPath, navigate } from './router.js';
import { renderReply } from './mdview.js';
import { dockWidth, wideDockWidth, floatRect, readPrefs } from './panelgeom.js';

const DRAFT_KEY = 'forgeline-draft';

/** What the page is about, so the server can attach the right data to the question. */
function pageContext() {
  const p = currentPath().split('?')[0];
  let m = p.match(/^\/runs\/([^/]+)$/);
  if (m) return { kind: 'run', id: decodeURIComponent(m[1]), label: 'this run' };
  m = p.match(/^\/pipelines\/([^/]+)$/);
  if (m && m[1] !== 'new') return { kind: 'pipeline', id: decodeURIComponent(m[1]), label: 'this pipeline' };
  if (p === '/infrastructure') return { kind: 'infra', id: '', label: 'the infrastructure' };
  return { kind: 'none', id: '', label: 'the overview' };
}

const SUGGESTIONS = {
  run: ['Why did this run fail?', 'Summarise what happened in this run.'],
  pipeline: ['Review this pipeline for problems.', 'How could this run faster?'],
  infra: ['Summarise what is running in AWS.', 'Is anything here worth cleaning up?'],
  none: ['What needs attention right now?', 'Draft a pipeline that builds and tests a Go project.'],
};

const PREF_KEY = 'forgeline-assistant';
const loadPrefs = () => { try { return readPrefs(localStorage.getItem(PREF_KEY)); } catch { return readPrefs(''); } };
const savePrefs = (p) => { try { localStorage.setItem(PREF_KEY, JSON.stringify(p)); } catch { /* storage may be blocked */ } };

const DOCK_CLASS = 'fixed bottom-0 right-0 top-0 z-40 flex w-full flex-col border-l border-slate-200 bg-white shadow-xl lg:top-14 lg:w-[var(--assistant-w)] dark:border-slate-700 dark:bg-slate-900';
const FLOAT_CLASS = 'fixed z-40 flex resize flex-col overflow-hidden rounded-xl border border-slate-300 bg-white shadow-2xl dark:border-slate-600 dark:bg-slate-900';

export function assistantButton(role) {
  if (!atLeast(role, 'developer')) return null;
  let panel = null;
  const btn = h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'assistant-open', 'aria-haspopup': 'dialog', onclick: () => toggle() }, icon('sparkle'), h('span', { class: 'hidden sm:inline' }, 'Assistant'));
  const close = () => { if (!panel) return; panel.dispose(); panel.remove(); panel = null; btn.focus(); };

  function toggle() {
    if (panel) return close();
    panel = build(close, role);
    document.body.append(panel);
    panel.layout();
    panel.querySelector('textarea')?.focus();
  }
  return btn;
}

function build(close, role) {
  const history = []; // {role, content}
  const prefs = loadPrefs();
  const list = h('div', { class: 'min-h-0 flex-1 space-y-5 overflow-y-auto px-4 py-4', 'aria-live': 'polite', id: 'assistant-thread' });
  const ctxLabel = h('span', { class: 'font-medium' });
  const input = h('textarea', { id: 'assistant-input', class: 'input min-h-[2.75rem] flex-1 resize-none', rows: '2', maxlength: '4000', placeholder: 'Ask about this page…', 'aria-label': 'Your question' });
  const sendBtn = h('button', { class: 'btn-primary', type: 'submit', 'aria-label': 'Send' }, icon('send'));
  let busy = false, ready = null;

  const refreshCtx = () => { ctxLabel.textContent = pageContext().label; };
  refreshCtx();
  addEventListener('hashchange', refreshCtx);

  function userBubble(text) {
    return h('div', { class: 'flex justify-end' }, h('div', { class: 'max-w-[85%] whitespace-pre-wrap break-words rounded-xl bg-brand-600 px-3.5 py-2.5 text-sm text-white' }, text));
  }

  /** A code block with Copy, and for YAML a way to open it as a new pipeline. */
  function codeBlock(part) {
    const isYaml = part.lang === 'yaml' || part.lang === 'yml';
    return h('div', { class: 'overflow-hidden rounded-lg bg-slate-950' },
      h('pre', { class: 'max-h-80 overflow-auto p-3 font-mono text-xs text-slate-100' }, part.text),
      h('div', { class: 'flex flex-wrap gap-2 border-t border-slate-800 p-2' }, copyButton(part.text, 'Copy'),
        isYaml && h('button', { class: 'btn-secondary btn-sm', type: 'button', onclick: () => {
          try { sessionStorage.setItem(DRAFT_KEY, part.text); } catch { toast('Your browser blocked saving the draft. Copy it instead.', 'error'); return; }
          close(); navigate('/pipelines'); setTimeout(() => navigate('/pipelines/new'), 0);
        } }, icon('plus', 'size-3.5'), 'Open as a new pipeline')));
  }

  const amber = 'flex items-start gap-2 rounded-xl border border-amber-300 bg-amber-50 px-3 py-2.5 text-xs text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-200';

  /** One answer: what it looked at, the answer as cards, and anything it filed for review. */
  function answerTurn(r) {
    const lookups = r.lookups || [];
    return h('div', { class: 'space-y-3', 'data-turn': 'assistant' },
      lookups.length ? h('div', { class: 'flex flex-wrap items-center gap-1.5 text-xs', 'aria-label': 'What the assistant looked up' },
        h('span', { class: 'muted' }, 'Looked up'),
        lookups.map(n => h('span', { class: 'inline-flex items-center gap-1 rounded-md border border-slate-200 bg-slate-50 px-1.5 py-0.5 font-mono text-[11px] dark:border-slate-700 dark:bg-slate-800' }, icon('tool', 'size-3'), n))) : null,
      renderReply(r.reply, { codeBlock }),
      (r.proposals || []).length ? h('div', { class: amber, 'data-filed': 'proposal' }, icon('shield', 'mt-0.5 size-4 shrink-0'), h('div', {}, 'Filed for review. Nothing has run: ',
        ...r.proposals.map(id => h('a', { href: '#/proposals/' + id, class: 'font-semibold underline', onclick: close }, 'proposal ' + id)))) : null,
      (r.environments || []).length ? h('div', { class: amber, 'data-filed': 'environment' }, icon('server', 'mt-0.5 size-4 shrink-0'), h('div', {}, 'Environment requested. Nothing exists until an administrator approves: ',
        h('a', { href: '#/environments', class: 'font-semibold underline', onclick: close }, 'see Environments'))) : null,
      h('div', { class: 'muted text-[11px]' }, 'Based on: ' + r.context));
  }

  function empty() {
    const c = pageContext();
    mount(list, h('div', { class: 'space-y-3 text-sm' },
      h('p', { class: 'muted' }, 'I can explain failures, review pipelines and draft new ones. I can only read: I cannot change anything, and I never see secret values.'),
      h('div', { class: 'flex flex-col gap-2' }, (SUGGESTIONS[c.kind] || SUGGESTIONS.none).map(q =>
        h('button', { class: 'btn-secondary justify-start text-left', type: 'button', onclick: () => ask(q) }, q)))));
  }

  async function ask(question) {
    const q = (question ?? input.value).trim();
    if (!q || busy) return;
    if (!history.length) list.replaceChildren();
    busy = true; sendBtn.disabled = true; input.value = '';
    list.append(userBubble(q));
    const wait = h('div', { class: 'flex items-center gap-2 text-sm muted', role: 'status' }, h('span', { class: 'size-2 animate-pulse-dot rounded-full bg-brand-500' }), 'Thinking…');
    list.append(wait); list.scrollTop = list.scrollHeight;
    const c = pageContext();
    try {
      const r = await post('/v1/assistant/chat', { message: q, history, context: { kind: c.kind, id: c.id } });
      history.push({ role: 'user', content: q }, { role: 'assistant', content: r.reply });
      wait.replaceWith(answerTurn(r));
    } catch (e) {
      wait.replaceWith(h('div', {}, errorBox(e.message)));
    }
    busy = false; sendBtn.disabled = false; list.scrollTop = list.scrollHeight; input.focus();
  }

  const form = h('form', { class: 'flex items-end gap-2 border-t border-slate-200 p-3 dark:border-slate-800', onsubmit: (e) => { e.preventDefault(); ask(); } }, input, sendBtn);
  input.addEventListener('keydown', (e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); form.requestSubmit(); } });

  const root = document.documentElement;
  const modeBtn = h('button', { class: 'btn-ghost btn-sm', type: 'button', id: 'assistant-mode', onclick: () => { prefs.mode = prefs.mode === 'dock' ? 'float' : 'dock'; savePrefs(prefs); layout(); } });
  const wideBtn = h('button', { class: 'btn-ghost btn-sm', type: 'button', id: 'assistant-wide', onclick: () => {
    prefs.dockW = dockWidth(prefs.dockW, innerWidth) >= wideDockWidth(innerWidth) ? 520 : wideDockWidth(innerWidth);
    savePrefs(prefs); layout();
  } });
  const handle = h('div', { class: 'absolute inset-y-0 -left-1.5 z-10 hidden w-3 cursor-col-resize touch-none after:absolute after:inset-y-0 after:left-1 after:w-0.5 after:bg-transparent hover:after:bg-brand-500 focus-visible:after:bg-brand-500', id: 'assistant-resize',
    role: 'separator', 'aria-orientation': 'vertical', 'aria-label': 'Resize the assistant. Use the left and right arrow keys.', tabindex: '0' });
  const header = h('div', { class: 'flex shrink-0 items-center justify-between border-b border-slate-200 px-4 py-3 dark:border-slate-800', id: 'assistant-header' },
    h('div', {}, h('div', { class: 'flex items-center gap-2 text-sm font-semibold' }, icon('sparkle'), 'Assistant'), h('div', { class: 'muted text-xs' }, 'Looking at ', ctxLabel)),
    h('div', { class: 'flex gap-1' }, wideBtn, modeBtn,
      h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Clear the conversation', onclick: () => { history.length = 0; empty(); } }, icon('refresh')),
      h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Close the assistant', onclick: close }, icon('x'))));
  const panel = h('aside', { class: DOCK_CLASS, role: 'dialog', 'aria-label': 'Assistant', id: 'assistant-panel' }, handle, header, list, form);
  panel.addEventListener('keydown', (e) => { if (e.key === 'Escape') close(); });

  /** Applies the saved mode and size, kept inside the window. Docked: a width the page makes room for. Floating: a window. */
  function layout() {
    const vw = innerWidth, vh = innerHeight, docked = prefs.mode === 'dock';
    panel.className = docked ? DOCK_CLASS : FLOAT_CLASS;
    handle.classList.toggle('lg:block', docked);
    if (docked) {
      prefs.dockW = dockWidth(prefs.dockW, vw);
      root.style.setProperty('--assistant-w', prefs.dockW + 'px');
      panel.style.left = panel.style.top = panel.style.width = panel.style.height = '';
      handle.setAttribute('aria-valuenow', String(prefs.dockW));
    } else {
      root.style.removeProperty('--assistant-w');
      const r = floatRect(prefs.float, vw, vh);
      prefs.float = r;
      panel.style.left = r.x + 'px'; panel.style.top = r.y + 'px'; panel.style.width = r.w + 'px'; panel.style.height = r.h + 'px';
    }
    const wide = docked && prefs.dockW >= wideDockWidth(vw);
    wideBtn.classList.toggle('hidden', !docked);
    wideBtn.replaceChildren(icon(wide ? 'shrink' : 'expand'));
    wideBtn.setAttribute('aria-label', wide ? 'Make the assistant narrower' : 'Make the assistant wider');
    modeBtn.replaceChildren(icon(docked ? 'window' : 'dock'));
    modeBtn.setAttribute('aria-label', docked ? 'Float the assistant as a window' : 'Dock the assistant to the side');
    header.classList.toggle('cursor-move', !docked);
  }

  // Docked: drag the left edge, or use the arrow keys on it.
  handle.addEventListener('pointerdown', (e) => {
    handle.setPointerCapture(e.pointerId);
    const move = (ev) => { prefs.dockW = dockWidth(innerWidth - ev.clientX, innerWidth); layout(); };
    const up = () => { handle.removeEventListener('pointermove', move); handle.removeEventListener('pointerup', up); handle.removeEventListener('pointercancel', up); savePrefs(prefs); };
    handle.addEventListener('pointermove', move); handle.addEventListener('pointerup', up); handle.addEventListener('pointercancel', up);
  });
  handle.addEventListener('keydown', (e) => {
    const step = (e.shiftKey ? 96 : 24) * (e.key === 'ArrowLeft' ? 1 : e.key === 'ArrowRight' ? -1 : 0);
    if (!step) return;
    e.preventDefault(); e.stopPropagation();
    prefs.dockW = dockWidth(prefs.dockW + step, innerWidth); layout(); savePrefs(prefs);
  });

  // Floating: drag by the header; the browser's own corner handle resizes, and the size is remembered.
  header.addEventListener('pointerdown', (e) => {
    if (prefs.mode !== 'float' || e.target.closest('button')) return;
    header.setPointerCapture(e.pointerId);
    const start = { px: e.clientX, py: e.clientY, ...floatRect(prefs.float, innerWidth, innerHeight) };
    const move = (ev) => { prefs.float = { ...start, x: start.x + ev.clientX - start.px, y: start.y + ev.clientY - start.py }; layout(); };
    const up = () => { header.removeEventListener('pointermove', move); header.removeEventListener('pointerup', up); header.removeEventListener('pointercancel', up); savePrefs(prefs); };
    header.addEventListener('pointermove', move); header.addEventListener('pointerup', up); header.addEventListener('pointercancel', up);
  });
  const sizeWatch = typeof ResizeObserver === 'function' ? new ResizeObserver(() => {
    if (prefs.mode !== 'float') return;
    const b = panel.getBoundingClientRect();
    if (Math.abs(b.width - prefs.float.w) > 1 || Math.abs(b.height - prefs.float.h) > 1) { prefs.float = { ...prefs.float, w: b.width, h: b.height }; savePrefs(prefs); }
  }) : null;
  sizeWatch?.observe(panel);
  const onResize = () => layout();
  addEventListener('resize', onResize);
  panel.layout = layout;
  panel.dispose = () => { removeEventListener('resize', onResize); removeEventListener('hashchange', refreshCtx); sizeWatch?.disconnect(); root.style.removeProperty('--assistant-w'); };

  // Say so up front if it is not set up, instead of failing on the first question.
  get('/v1/assistant').then(cfg => {
    ready = cfg.ready;
    if (!cfg.ready) {
      form.classList.add('hidden');
      mount(list, notice('info', 'The assistant is not set up yet. ', atLeast(role, 'admin') ? h('a', { href: '#/integrations', class: 'underline', onclick: close }, 'Turn it on under Integrations.') : 'Ask an administrator to turn it on.'));
    } else empty();
  }).catch(e => mount(list, errorBox(e.message)));
  return panel;
}

/** A draft handed over by the assistant, once. */
export function takeDraft() {
  try { const d = sessionStorage.getItem(DRAFT_KEY); if (d) sessionStorage.removeItem(DRAFT_KEY); return d; } catch { return null; }
}
