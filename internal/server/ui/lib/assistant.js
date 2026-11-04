// The assistant panel: a read-only helper that answers questions about the page you are on.
// It cannot change anything; a pipeline it drafts is text until you save it yourself.
import { h, icon, mount, errorBox, notice, copyButton, toast, pill, relTime } from './dom.js';
import { get, post, del, atLeast, streamPost } from './api.js';
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
  let conversation = ''; // the saved conversation this panel continues; the server starts one on the first question
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

  /**
   * A read the access policy wants a person to approve. It is the person's own session that answers, once; the
   * model cannot. Nothing runs until Approve, and no answer before the time is up counts as a denial.
   */
  function approvalCard(a) {
    const btn = (decision, label, cls) => h('button', { type: 'button', class: cls, 'data-approval': decision, onclick: async (e) => {
      const box = e.currentTarget.closest('[data-approval-id]');
      box.querySelectorAll('button').forEach(b => { b.disabled = true; });
      try { await post('/v1/assistant/approvals/' + encodeURIComponent(a.id), { decision }); } catch (err) { toast(err.message, 'error'); box.querySelectorAll('button').forEach(b => { b.disabled = false; }); }
    } }, label);
    return h('div', { class: amber + ' flex-col', 'data-approval-id': a.id, role: 'group', 'aria-label': 'Approve this lookup' },
      h('div', { class: 'flex items-center gap-2 font-semibold' }, icon('shield', 'size-4 shrink-0'), 'Approve this lookup?'),
      h('div', {}, 'The access policy for ', h('b', {}, a.integration || 'this connection'), ' asks you before ', h('code', { class: 'font-mono' }, a.tool), ' runs. It only reads. Nothing runs until you approve',
        a.seconds ? ` (it is a no after ${Math.round(a.seconds / 60) || 1} min)` : '', '.'),
      a.args ? h('pre', { class: 'max-h-24 w-full overflow-auto whitespace-pre-wrap break-words rounded-lg bg-white/70 p-2 font-mono text-[11px] dark:bg-slate-900/50' }, a.args) : null,
      h('div', { class: 'flex gap-2' }, btn('approve', 'Approve', 'btn-primary btn-sm'), btn('deny', 'Deny', 'btn-secondary btn-sm')));
  }

  /**
   * Something the assistant prepared inside Forgeline. Nothing has happened: the card lists the exact effects
   * (written by the server, not the model) and the file, and it only runs when you press Confirm.
   */
  function actionCard(a) {
    const box = h('div', { class: amber + ' flex-col', 'data-action-id': a.id, 'data-kind': a.kind });
    const done = (text, link) => { box.replaceChildren(h('div', { class: 'flex items-center gap-2' }, icon('check', 'size-4 shrink-0'), h('span', {}, text),
      link ? h('a', { href: '#' + link, class: 'font-semibold underline', onclick: close }, 'Open') : null)); };
    const busy = (on) => box.querySelectorAll('button').forEach(b => { b.disabled = on; });
    const send = async (verb) => {
      busy(true);
      try { const r = await post('/v1/assistant/actions/' + encodeURIComponent(a.id) + '/' + verb); verb === 'confirm' ? done(r.result || 'Done.', r.link) : done('Dismissed.'); }
      catch (err) { toast(err.message, 'error'); busy(false); }
    };
    box.append(
      h('div', { class: 'flex items-center gap-2 font-semibold' }, icon('bolt', 'size-4 shrink-0'), a.title || 'Waiting for you'),
      h('ul', { class: 'list-disc space-y-0.5 pl-5 text-xs' }, a.effects.map(e => h('li', {}, e))),
      a.detail ? h('details', {}, h('summary', { class: 'cursor-pointer text-xs underline' }, 'Show the file'),
        h('pre', { class: 'mt-1 max-h-48 w-full overflow-auto whitespace-pre-wrap break-words rounded-lg bg-white/70 p-2 font-mono text-[11px] dark:bg-slate-900/50' }, a.detail)) : null,
      h('div', { class: 'flex gap-2' },
        h('button', { type: 'button', class: 'btn-primary btn-sm', 'data-action': 'confirm', onclick: () => send('confirm') }, 'Confirm'),
        h('button', { type: 'button', class: 'btn-secondary btn-sm', 'data-action': 'dismiss', onclick: () => send('dismiss') }, 'Dismiss')));
    return box;
  }

  /**
   * A note the assistant tried to keep. One it saved at once can be undone here; one it only suggested (because it
   * read data you did not type) is kept only if you say so. Either way the Memory page lists it.
   */
  function memoryCard(n) {
    const pending = n.status === 'pending';
    const act = async (e, fn) => {
      const box = e.currentTarget.closest('[data-memory-id]');
      box.querySelectorAll('button').forEach(b => { b.disabled = true; });
      try { await fn(); box.replaceChildren(h('span', { class: 'muted' }, 'Done.')); } catch (err) { toast(err.message, 'error'); box.querySelectorAll('button').forEach(b => { b.disabled = false; }); }
    };
    return h('div', { class: amber + ' flex-wrap items-center', 'data-memory-id': n.id, 'data-status': n.status },
      icon('sparkle', 'size-4 shrink-0'),
      h('div', { class: 'min-w-0 flex-1' }, h('b', {}, pending ? 'Keep this note? ' : 'Remembered: '), n.text,
        n.about === 'system' ? ' (about this system; an administrator confirms it)' : ''),
      pending && n.about !== 'system' ? h('button', { type: 'button', class: 'btn-primary btn-sm', 'data-memory': 'keep', onclick: (e) => act(e, () => post('/v1/assistant/memory/' + encodeURIComponent(n.id) + '/approve')) }, 'Keep') : null,
      n.about === 'system' && pending ? null : h('button', { type: 'button', class: 'btn-secondary btn-sm', 'data-memory': 'undo', onclick: (e) => act(e, () => del('/v1/assistant/memory/' + encodeURIComponent(n.id))) }, pending ? 'Discard' : 'Undo'));
  }

  /** The run so far: its lookups as cards, what it is doing, the answer as cards, and anything it filed. */
  function runNode(run, opened, cards, stop) {
    const cardFor = (id) => { if (!cards.has(id)) cards.set(id, proposalCard(id)); return cards.get(id); };
    const label = progressLabel(run);
    return h('div', { class: 'space-y-3' },
      run.steps.length ? h('div', { class: 'space-y-1.5', 'aria-label': 'What the assistant looked up' }, run.steps.map(st => stepCard(st, opened))) : null,
      run.approvals.filter(a => a.state === 'waiting').map(approvalCard),
      label ? h('div', { class: 'flex items-center gap-2 text-sm muted', role: 'status' }, h('span', { class: 'size-2 animate-pulse-dot rounded-full bg-brand-500' }), label,
        h('button', { class: 'btn-ghost btn-sm ml-auto', type: 'button', id: 'assistant-stop', onclick: stop }, icon('stop', 'size-3'), 'Stop')) : null,
      run.status === 'stopped' ? h('p', { class: 'muted text-xs' }, 'Stopped. Nothing more was looked up.') : null,
      run.text ? renderReply(run.text, { codeBlock }) : null,
      run.error ? errorBox(run.error) : null,
      run.proposals.map(cardFor),
      run.actions.map(actionCard),
      run.memories.map(memoryCard),
      run.environments.length ? h('div', { class: amber, 'data-filed': 'environment' }, icon('server', 'mt-0.5 size-4 shrink-0'), h('div', {}, 'Environment requested. Nothing exists until an administrator approves: ',
        h('a', { href: '#/environments', class: 'font-semibold underline', onclick: close }, 'see Environments'))) : null,
      run.status === 'finished' && run.context ? h('div', { class: 'muted text-[11px]' }, 'Based on: ' + run.context) : null);
  }

  function empty() {
    const c = pageContext();
    mount(list, h('div', { class: 'space-y-3 text-sm' },
      h('p', { class: 'muted' }, 'I can explain failures, review pipelines, and set things up here: I prepare a pipeline or a run and you press Confirm. I cannot change your cloud myself (I file a proposal for another administrator), and I never see secret values.'),
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
      await streamPost('/v1/assistant/runs', { message: q, history, conversationId: conversation, context: { kind: c.kind, id: c.id } }, (ev) => { applyEvent(run, ev); draw(); }, ctl.signal);
      if (run.status === 'running') { run.status = 'error'; run.error = 'The connection closed before the assistant finished.'; }
    } catch (e) {
      if (e && e.name === 'AbortError') run.status = 'stopped';
      else { run.status = 'error'; run.error = e.message; }
    }
    draw();
    if (run.conversation) conversation = run.conversation;
    if (run.status === 'finished') history.push({ role: 'user', content: q }, { role: 'assistant', content: run.text });
    busy = false; sendBtn.disabled = false; input.focus();
  }

  /** Your saved conversations: open one to carry on from it, or delete it. They are private to you. */
  async function showHistory() {
    if (busy) return;
    let rows;
    try { rows = await get('/v1/assistant/conversations'); } catch (e) { return mount(list, errorBox(e.message)); }
    const back = h('button', { class: 'btn-ghost btn-sm', type: 'button', onclick: () => (history.length ? restore() : empty()) }, 'Back');
    mount(list, h('div', { class: 'space-y-2', id: 'assistant-history-list' },
      h('div', { class: 'flex items-center justify-between' }, h('h3', { class: 'text-sm font-semibold' }, 'Your conversations'), back),
      rows.length ? rows.slice(0, 30).map(c => h('div', { class: 'flex items-center gap-1 rounded-lg border border-slate-200 dark:border-slate-700', 'data-conversation': c.id },
        h('button', { class: 'min-w-0 flex-1 px-3 py-2 text-left text-sm', type: 'button', onclick: () => openConversation(c.id) },
          h('div', { class: 'truncate font-medium' }, c.title || 'Untitled'),
          h('div', { class: 'muted text-xs' }, relTime(c.updated) + (c.refs && c.refs.length ? ' · filed ' + c.refs.length : ''))),
        h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Delete this conversation', onclick: async () => {
          try { await del('/v1/assistant/conversations/' + encodeURIComponent(c.id)); if (conversation === c.id) { history.length = 0; conversation = ''; } showHistory(); } catch (e) { toast(e.message, 'error'); }
        } }, icon('trash', 'size-4')))) : h('p', { class: 'muted text-sm' }, 'Nothing yet. Conversations are saved as you have them.')));
  }

  /** Shows what is in `history` again, after the history list. */
  function restore() {
    list.replaceChildren();
    for (let i = 0; i < history.length; i++) {
      const t = history[i];
      list.append(t.role === 'user' ? userBubble(t.content) : h('div', { 'data-turn': 'assistant' }, renderReply(t.content, { codeBlock })));
    }
    list.scrollTop = list.scrollHeight;
  }

  async function openConversation(id) {
    let c;
    try { c = await get('/v1/assistant/conversations/' + encodeURIComponent(id)); } catch (e) { return toast(e.message, 'error'); }
    history.length = 0;
    for (const m of c.messages) history.push({ role: m.role, content: m.text });
    conversation = c.id;
    restore();
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
      h('button', { class: 'btn-ghost btn-sm', type: 'button', id: 'assistant-history', 'aria-label': 'Your past conversations', onclick: showHistory }, icon('history')),
      h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Clear the conversation', onclick: () => { history.length = 0; conversation = ''; empty(); } }, icon('refresh')),
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
      mount(list, notice('info', 'The assistant is not set up yet. ', atLeast(role, 'admin') ? h('a', { href: '#/integrations/claude/settings', class: 'underline', onclick: close }, 'Turn it on under Integrations.') : 'Ask an administrator to turn it on.'));
    } else empty();
  }).catch(e => mount(list, errorBox(e.message)));
  return panel;
}

/** A draft handed over by the assistant, once. */
export function takeDraft() {
  try { const d = sessionStorage.getItem(DRAFT_KEY); if (d) sessionStorage.removeItem(DRAFT_KEY); return d; } catch { return null; }
}
