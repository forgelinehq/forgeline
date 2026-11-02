// The assistant panel: a read-only helper that answers questions about the page you are on.
// It cannot change anything; a pipeline it drafts is text until you save it yourself.
import { h, icon, mount, errorBox, notice, copyButton, toast, pill } from './dom.js';
import { get, atLeast, streamPost } from './api.js';
import { currentPath, navigate } from './router.js';
import { renderReply } from './mdview.js';
import { newRun, applyEvent, progressLabel } from './runstate.js';
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

  const STEP_ICON = {
    running: () => h('span', { class: 'size-3.5 shrink-0 animate-spin rounded-full border-2 border-slate-300 border-t-brand-600', 'aria-hidden': 'true' }),
    done: () => icon('check', 'size-3.5 shrink-0 text-emerald-600 dark:text-emerald-400'),
    failed: () => icon('alert', 'size-3.5 shrink-0 text-rose-600 dark:text-rose-400'),
  };
  const STEP_WORD = { running: 'running', done: 'done', failed: 'failed' };

  /** One lookup: its name, what it was asked, its state, and the cut of the result on demand. */
  function stepCard(st, opened) {
    return h('details', { class: 'rounded-lg border border-slate-200 bg-slate-50 text-xs dark:border-slate-700 dark:bg-slate-800/60', 'data-step': st.name, 'data-status': st.status, open: opened.has(st.id),
      ontoggle: (e) => { if (e.target.open) opened.add(st.id); else opened.delete(st.id); } },
      h('summary', { class: 'flex cursor-pointer items-center gap-2 px-2.5 py-1.5' },
        STEP_ICON[st.status](), h('span', { class: 'sr-only' }, STEP_WORD[st.status]),
        icon('tool', 'size-3 shrink-0 text-slate-400'), h('span', { class: 'shrink-0 font-mono font-medium' }, st.name),
        st.args ? h('span', { class: 'muted min-w-0 truncate font-mono' }, st.args) : null),
      st.result ? h('pre', { class: 'max-h-60 overflow-auto whitespace-pre-wrap break-words border-t border-slate-200 p-2.5 font-mono text-[11px] leading-relaxed dark:border-slate-700' }, st.result) : null);
  }

  /**
   * A change the assistant filed, as a card that follows the proposal. It has no Approve button on
   * purpose: the assistant files a proposal as the person who asked, and the server refuses the
   * proposer as approver, so approval is always another administrator's, on the Proposals page.
   */
  function proposalCard(id) {
    const box = h('div', { class: 'overflow-hidden rounded-xl border border-amber-300 bg-amber-50/60 dark:border-amber-500/40 dark:bg-amber-500/5', 'data-proposal': id }, h('div', { class: 'p-3 text-xs muted' }, 'Loading the proposal…'));
    let timer = null;
    async function load() {
      let p;
      try { p = await get('/v1/ai/proposals/' + encodeURIComponent(id)); } catch (e) { return mount(box, h('div', { class: 'p-3' }, errorBox(e.message))); }
      const calls = p.calls || [];
      const open = p.status === 'proposed' || p.status === 'applying';
      const who = p.account_name || p.target || 'the connected account';
      mount(box,
        h('div', { class: 'flex flex-wrap items-center gap-2 border-b border-amber-200 px-3 py-2 dark:border-amber-500/30' },
          icon('shield', 'size-4 shrink-0 text-amber-700 dark:text-amber-300'), h('span', { class: 'min-w-0 flex-1 text-sm font-semibold' }, p.title),
          pill(p.status), h('span', { class: 'pill-idle' }, 'risk ' + p.risk)),
        h('div', { class: 'space-y-2 p-3 text-xs' },
          h('p', { class: 'whitespace-pre-wrap text-slate-700 dark:text-slate-300' }, p.reason),
          calls.length ? h('ul', { class: 'space-y-0.5 font-mono' }, calls.slice(0, 6).map(c => h('li', { class: 'truncate' }, h('span', { class: 'font-semibold' }, c.service + ' ' + c.operation), c.args ? h('span', { class: 'muted' }, ' ' + c.args) : null)),
            calls.length > 6 ? h('li', { class: 'muted' }, `and ${calls.length - 6} more`) : null) : null,
          h('p', { class: 'muted' }, p.status === 'proposed'
            ? `Drafted for ${who}. Nothing has run. A different administrator must approve it on the Proposals page.`
            : p.decided_by ? `${p.status === 'rejected' ? 'Rejected' : p.status === 'cancelled' ? 'Withdrawn' : 'Approved'} by ${p.decided_by}.` : `Status: ${p.status}.`),
          h('a', { href: '#/proposals/' + encodeURIComponent(id), class: 'inline-block font-semibold underline' }, 'Open the full proposal')));
      clearInterval(timer);
      if (open) timer = setInterval(() => { if (!box.isConnected) clearInterval(timer); else load(); }, 5000);
    }
    load();
    return box;
  }

  /** The run so far: its lookups as cards, what it is doing, the answer as cards, and anything it filed. */
  function runNode(run, opened, cards, stop) {
    const cardFor = (id) => { if (!cards.has(id)) cards.set(id, proposalCard(id)); return cards.get(id); };
    const label = progressLabel(run);
    return h('div', { class: 'space-y-3' },
      run.steps.length ? h('div', { class: 'space-y-1.5', 'aria-label': 'What the assistant looked up' }, run.steps.map(st => stepCard(st, opened))) : null,
      label ? h('div', { class: 'flex items-center gap-2 text-sm muted', role: 'status' }, h('span', { class: 'size-2 animate-pulse-dot rounded-full bg-brand-500' }), label,
        h('button', { class: 'btn-ghost btn-sm ml-auto', type: 'button', id: 'assistant-stop', onclick: stop }, icon('stop', 'size-3'), 'Stop')) : null,
      run.status === 'stopped' ? h('p', { class: 'muted text-xs' }, 'Stopped. Nothing more was looked up.') : null,
      run.text ? renderReply(run.text, { codeBlock }) : null,
      run.error ? errorBox(run.error) : null,
      run.proposals.map(cardFor),
      run.environments.length ? h('div', { class: amber, 'data-filed': 'environment' }, icon('server', 'mt-0.5 size-4 shrink-0'), h('div', {}, 'Environment requested. Nothing exists until an administrator approves: ',
        h('a', { href: '#/environments', class: 'font-semibold underline', onclick: close }, 'see Environments'))) : null,
      run.status === 'finished' && run.context ? h('div', { class: 'muted text-[11px]' }, 'Based on: ' + run.context) : null);
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
    const run = newRun(), opened = new Set(), cards = new Map(), ctl = new AbortController();
    const host = h('div', { 'data-turn': 'assistant' });
    list.append(userBubble(q), host);
    const draw = () => {
      const near = list.scrollHeight - list.scrollTop - list.clientHeight < 120;
      mount(host, runNode(run, opened, cards, () => ctl.abort()));
      if (near) list.scrollTop = list.scrollHeight;
    };
    draw(); list.scrollTop = list.scrollHeight;
    const c = pageContext();
    try {
      await streamPost('/v1/assistant/runs', { message: q, history, context: { kind: c.kind, id: c.id } }, (ev) => { applyEvent(run, ev); draw(); }, ctl.signal);
      if (run.status === 'running') { run.status = 'error'; run.error = 'The connection closed before the assistant finished.'; }
    } catch (e) {
      if (e && e.name === 'AbortError') run.status = 'stopped';
      else { run.status = 'error'; run.error = e.message; }
    }
    draw();
    if (run.status === 'finished') history.push({ role: 'user', content: q }, { role: 'assistant', content: run.text });
    busy = false; sendBtn.disabled = false; input.focus();
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
