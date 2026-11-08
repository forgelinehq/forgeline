import { h, icon, mount, clear, pageHeader, relTime, absTime, emptyState, spinner, errorBox, notice, toast, modal, confirmDialog, field } from '../lib/dom.js';
import { get, post, del, atLeast } from '../lib/api.js';
import { navigate } from '../lib/router.js';
import { session } from '../app.js';
import { workflowList, workflowForm } from './workflows.js';
import { mdEditor } from '../lib/mdeditor.js';
import { markdownBox } from '../lib/mdview.js';
import { checkInstructions } from '../lib/markdown.js';
import { groupTools, toolInfo, plainHelp, TIERS, tierTitle } from '../lib/tools.js';

// Agents and skills. Every save is a new version; old versions are never edited, and a rollback is
// a copy of an old version saved as the newest one.

const tierPill = (t) => h('span', { class: t === 'T1' ? 'pill-wait' : 'pill-idle', title: (TIERS.find(x => x.id === t) || TIERS[0]).text }, tierTitle(t));

// An agent's face: its initials on a colour picked from its id, so it is the same everywhere.
const AVATAR = ['bg-brand-600', 'bg-teal-700', 'bg-violet-600', 'bg-sky-700', 'bg-rose-600', 'bg-amber-700'];
export function avatar(name, id, size = 'size-10 text-sm') {
  let n = 0;
  for (const c of String(id || name)) n = (n * 31 + c.charCodeAt(0)) >>> 0;
  const initials = String(name || '?').split(/\s+/).filter(Boolean).slice(0, 2).map(w => w[0].toUpperCase()).join('') || '?';
  return h('span', { class: 'flex shrink-0 items-center justify-center rounded-xl font-display font-bold text-white ' + size + ' ' + AVATAR[n % AVATAR.length], 'aria-hidden': 'true' }, initials);
}

/** An agent's tools, grouped and in plain words; the technical name on hover. */
function toolChips(names) {
  const groups = groupTools(names.map(n => ({ name: n })));
  return h('div', { class: 'space-y-2', id: 'agent-tools' }, groups.map(g => h('div', {},
    h('div', { class: 'muted mb-1 text-xs font-medium' }, g.title),
    h('div', { class: 'flex flex-wrap gap-1.5' }, g.tools.map(t => h('span', { class: 'inline-flex items-center gap-1 rounded-lg border border-slate-200 bg-slate-50 px-2 py-1 text-xs dark:border-slate-700 dark:bg-slate-800', title: t.name, 'data-tool': t.name },
      t.asks ? icon('shield', 'size-3 text-amber-600 dark:text-amber-400') : null, t.title))))));
}
const shortHash = (x) => h('code', { class: 'text-xs', title: x }, x.slice(0, 12));

/** A line diff: lines only in `a` are removed, only in `b` are added. Small inputs, so a plain LCS. */
function lineDiff(a, b) {
  const x = (a || '').split('\n'), y = (b || '').split('\n');
  const dp = Array.from({ length: x.length + 1 }, () => new Array(y.length + 1).fill(0));
  for (let i = x.length - 1; i >= 0; i--) for (let j = y.length - 1; j >= 0; j--) dp[i][j] = x[i] === y[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
  const out = [];
  let i = 0, j = 0;
  while (i < x.length && j < y.length) {
    if (x[i] === y[j]) { out.push([' ', x[i]]); i++; j++; }
    else if (dp[i + 1][j] >= dp[i][j + 1]) out.push(['-', x[i++]]);
    else out.push(['+', y[j++]]);
  }
  while (i < x.length) out.push(['-', x[i++]]);
  while (j < y.length) out.push(['+', y[j++]]);
  return out;
}

function diffView(a, b) {
  const rows = lineDiff(a, b);
  if (!rows.some(r => r[0] !== ' ')) return h('p', { class: 'muted text-sm' }, 'No change in this part.');
  return h('pre', { class: 'overflow-x-auto rounded-lg border border-slate-200 p-3 text-xs leading-5 dark:border-slate-800', 'data-diff': '' }, rows.map(([k, line]) =>
    h('div', { class: k === '+' ? 'bg-emerald-50 text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300' : k === '-' ? 'bg-rose-50 text-rose-800 dark:bg-rose-950/40 dark:text-rose-300' : 'muted' }, k + ' ' + line)));
}

function versionTable(versions, current, selected, onPick, onRollback) {
  return h('div', { class: 'overflow-x-auto' }, h('table', { class: 'tbl', id: 'version-table' },
    h('thead', {}, h('tr', {}, ['Version', 'Saved by', 'When', 'Note', 'Fingerprint', ''].map(x => h('th', {}, x)))),
    h('tbody', {}, [...versions].reverse().map(v => h('tr', { class: 'row-link' + (v.n === selected ? ' bg-brand-50/60 dark:bg-brand-950/20' : ''), tabindex: '0', 'data-version': String(v.n),
      onclick: () => onPick(v.n), onkeydown: (e) => { if (e.key === 'Enter') onPick(v.n); } },
      h('td', { class: 'whitespace-nowrap font-medium' }, 'v' + v.n, v.n === current && h('span', { class: 'pill-ok ml-2' }, 'current')),
      h('td', { class: 'text-sm' }, v.by), h('td', { class: 'muted whitespace-nowrap text-sm', title: absTime(v.at) }, relTime(v.at)),
      h('td', { class: 'max-w-xs truncate text-sm' }, v.note || h('span', { class: 'muted' }, '–')), h('td', {}, shortHash(v.hash)),
      h('td', { class: 'text-right' }, onRollback && v.n !== current && h('button', { class: 'btn-ghost btn-sm', type: 'button', 'data-rollback': String(v.n), onclick: (e) => { e.stopPropagation(); onRollback(v.n); } }, icon('history', 'size-3.5'), 'Roll back to this')))))));
}

// ------------------------------------------------------------------ list

export async function agentsPage({ query }) {
  const tab = query.tab === 'skills' ? 'skills' : query.tab === 'workflows' ? 'workflows' : 'agents';
  const admin = atLeast(session.role, 'admin');
  const el = h('div', {});
  let agents, skills, tools, workflows;
  try { [agents, skills, tools, workflows] = await Promise.all([get('/v1/ai/agents'), get('/v1/ai/skills'), get('/v1/ai/tools'), get('/v1/ai/workflows')]); } catch (e) { return mount(el, errorBox(e.message)); }

  const tabBtn = (id, label) => h('a', { href: '#/agents' + (id === 'agents' ? '' : '?tab=' + id), role: 'tab', 'aria-selected': String(tab === id), id: 'tab-' + id,
    class: 'rounded-lg px-3 py-1.5 text-sm font-medium ' + (tab === id ? 'bg-brand-50 text-brand-700 dark:bg-brand-950/40 dark:text-brand-200' : 'muted hover:bg-slate-100 dark:hover:bg-slate-800') }, label);

  const agentCards = agents.agents.length ? h('div', { class: 'grid gap-4 sm:grid-cols-2 lg:grid-cols-3' }, agents.agents.map(a =>
    h('a', { href: '#/agents/' + a.id, class: 'card card-pad group flex flex-col gap-3 transition hover:border-brand-300 dark:hover:border-brand-500/50', 'data-agent': a.id },
      h('div', { class: 'flex items-start gap-3' }, avatar(a.name, a.id),
        h('div', { class: 'min-w-0 flex-1' }, h('h3', { class: 'truncate font-display text-base font-bold group-hover:text-brand-700 dark:group-hover:text-brand-200' }, a.name),
          h('div', { class: 'mt-0.5 flex items-center gap-1.5 text-xs' }, a.disabled ? h('span', { class: 'pill-idle' }, 'Off') : h('span', { class: 'pill-ok' }, 'On'), tierPill(a.tier)))),
      h('p', { class: 'muted line-clamp-2 text-sm' }, a.description || 'No description.'),
      h('div', { class: 'muted mt-auto flex flex-wrap gap-x-4 border-t border-slate-100 pt-3 text-xs dark:border-slate-800' }, h('span', {}, a.tools.length + ' tool' + (a.tools.length === 1 ? '' : 's')), h('span', {}, a.skills.length + ' skill' + (a.skills.length === 1 ? '' : 's')), h('span', {}, 'version ' + a.current)))))
    : emptyState('sparkle', 'No agents yet', admin ? 'An agent is instructions, skills and a short list of tools. Create one, then talk to it here or let the assistant hand it work.' : 'An administrator can create agents.');

  const skillRows = skills.skills.length ? h('div', { class: 'card overflow-x-auto' }, h('table', { class: 'tbl' }, h('thead', {}, h('tr', {}, ['Skill', 'Description', 'Version'].map(x => h('th', {}, x)))),
    h('tbody', {}, skills.skills.map(s => h('tr', { class: 'row-link', tabindex: '0', 'data-skill': s.id, onclick: () => navigate('/skills/' + s.id), onkeydown: (e) => { if (e.key === 'Enter') navigate('/skills/' + s.id); } },
      h('td', { class: 'font-medium' }, s.name), h('td', { class: 'muted max-w-lg truncate text-sm' }, s.description), h('td', { class: 'text-sm' }, 'v' + s.current))))))
    : emptyState('list', 'No skills yet', admin ? 'A skill is a procedure, such as how to right-size an ECS service, that agents follow.' : 'An administrator can create skills.');

  mount(el, pageHeader('Agents', 'Your AI team. Each agent has instructions, skills it follows and a short list of tools, and works as the person who asks it. Every save is a version you can roll back.',
    admin && (tab === 'agents' ? h('button', { class: 'btn-primary', id: 'new-agent', type: 'button', onclick: () => agentForm(null, skills.skills, tools.tools) }, icon('plus', 'size-4'), 'New agent')
      : tab === 'skills' ? h('button', { class: 'btn-primary', id: 'new-skill', type: 'button', onclick: () => skillForm(null) }, icon('plus', 'size-4'), 'New skill')
      : h('button', { class: 'btn-primary', id: 'new-workflow', type: 'button', onclick: () => workflowForm(null, agents.agents) }, icon('plus', 'size-4'), 'New workflow'))),
    h('div', { class: 'mb-4 flex gap-1', role: 'tablist' }, tabBtn('agents', 'Agents (' + agents.agents.length + ')'), tabBtn('skills', 'Skills (' + skills.skills.length + ')'), tabBtn('workflows', 'Workflows (' + workflows.workflows.length + ')')),
    tab === 'agents' ? agentCards : tab === 'skills' ? skillRows : workflowList(workflows.workflows, admin));
  return el;
}

// ------------------------------------------------------------------ skill detail

export async function skillPage({ params }) {
  const admin = atLeast(session.role, 'admin');
  const el = h('div', {});
  let sk, selected = 0;
  async function load(keep) {
    try { sk = await get('/v1/ai/skills/' + encodeURIComponent(params.id)); } catch (e) { return mount(el, errorBox(e.message)); }
    if (!keep || !sk.versions.some(v => v.n === selected)) selected = sk.current;
    draw();
  }
  function draw() {
    const v = sk.versions.find(x => x.n === selected), prev = sk.versions.find(x => x.n === selected - 1);
    mount(el, pageHeader(sk.name, sk.description, h('a', { class: 'btn-secondary btn-sm', href: '#/agents?tab=skills' }, 'All skills'),
      admin && h('button', { class: 'btn-primary btn-sm', id: 'new-version', type: 'button', onclick: () => skillForm(sk, load) }, icon('edit', 'size-3.5'), 'Edit'),
      admin && h('button', { class: 'btn-ghost btn-sm text-rose-600 dark:text-rose-400', type: 'button', onclick: async () => {
        if (!await confirmDialog({ title: 'Delete ' + sk.name + '?', body: 'Its history is deleted too. Skills an agent version uses cannot be deleted.', confirmLabel: 'Delete', danger: true })) return;
        try { await del('/v1/ai/skills/' + sk.id); toast('Deleted', 'ok'); navigate('/agents?tab=skills'); } catch (e) { toast(e.message, 'error'); }
      } }, icon('trash', 'size-3.5'), 'Delete')),
      h('div', { class: 'space-y-4' },
        h('section', { class: 'card card-pad' }, h('div', { class: 'mb-2 flex items-center justify-between' }, h('h2', { class: 'h-section' }, 'Version ' + v.n), shortHash(v.hash)),
          markdownBox(v.body, 'skill-body')),
        prev && h('section', { class: 'card card-pad' }, h('h2', { class: 'h-section mb-2' }, `Changes from v${prev.n}`), diffView(prev.body, v.body)),
        h('section', { class: 'card' }, h('div', { class: 'border-b border-slate-200 px-5 py-3 dark:border-slate-800' }, h('h2', { class: 'h-section' }, 'History')),
          versionTable(sk.versions, sk.current, selected, (n) => { selected = n; draw(); }, admin && rollback))));
  }
  async function rollback(n) {
    if (!await confirmDialog({ title: `Make v${n} current again?`, body: `It is saved as a new version with the same text as v${n}. Nothing is erased.`, confirmLabel: 'Roll back' })) return;
    try { await post('/v1/ai/skills/' + sk.id + '/rollback', { to: n }); toast('Rolled back', 'ok'); load(false); } catch (e) { toast(e.message, 'error'); }
  }
  mount(el, spinner());
  await load(false);
  return el;
}

function skillForm(sk, after) {
  modal(sk ? 'Edit ' + sk.name : 'New skill', (close) => {
    const cur = sk && sk.versions.find(v => v.n === sk.current);
    const name = h('input', { id: 'skill-name', class: 'input', maxlength: '60', value: sk ? sk.name : '', placeholder: 'ECS right-sizing' });
    const desc = h('input', { id: 'skill-desc', class: 'input', maxlength: '300', value: sk ? sk.description : '', placeholder: 'One line: when to use it' });
    const body = mdEditor({ id: 'skill-body-input', value: cur ? cur.body : '', maxlength: 8192, placeholder: 'Numbered steps the agent should follow. Say what to look at, what to compare, and when to stop and ask.', check: (t) => checkInstructions(t, { max: 8192 }) });
    const note = h('input', { id: 'skill-note', class: 'input', maxlength: '200', placeholder: sk ? 'What changed and why' : 'First version' });
    const err = h('div', { class: 'hidden' });
    const go = h('button', { class: 'btn-primary', type: 'button', id: 'skill-save', onclick: async () => {
      err.classList.add('hidden'); go.disabled = true;
      try {
        const out = await post(sk ? '/v1/ai/skills/' + sk.id + '/versions' : '/v1/ai/skills', { name: name.value, description: desc.value, body: body.input.value, note: note.value });
        toast(sk ? 'Saved v' + out.current : 'Skill created', 'ok'); close();
        if (sk) after(false); else navigate('/skills/' + out.id);
      } catch (e) { mount(err, errorBox(e.message)); err.classList.remove('hidden'); go.disabled = false; }
    } }, sk ? 'Save as a new version' : 'Create skill');
    const step = (title, text, ...kids) => h('section', { class: 'space-y-3 border-t border-slate-200 pt-5 first:border-t-0 first:pt-0 dark:border-slate-800' },
      h('div', {}, h('h3', { class: 'font-display text-base font-bold' }, title), text ? h('p', { class: 'muted text-xs' }, text) : null), ...kids);
    return h('div', { class: 'space-y-6' },
      step('What it is', null, h('div', { class: 'grid gap-4 sm:grid-cols-2' }, field('Name', name, null, 'skill-name'), field('When to use it', desc, null, 'skill-desc'))),
      step('Steps', 'The procedure an agent follows, in Markdown. Skills are trusted text, like the assistant\'s own instructions: write only what you would be happy for an agent to follow.', body.el),
      step('Save', null, field('Note for the history', note, null, 'skill-note'), err,
        h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'), go)));
  }, { xl: true });
}

// ------------------------------------------------------------------ agent detail

export async function agentPage({ params }) {
  const admin = atLeast(session.role, 'admin');
  const el = h('div', {});
  let ag, selected = 0, skills = [], tools = [];
  async function load(keep) {
    try { [ag, skills, tools] = await Promise.all([get('/v1/ai/agents/' + encodeURIComponent(params.id)), get('/v1/ai/skills').then(r => r.skills), get('/v1/ai/tools').then(r => r.tools)]); } catch (e) { return mount(el, errorBox(e.message)); }
    if (!keep || !ag.versions.some(v => v.n === selected)) selected = ag.current;
    draw();
  }
  const skillName = (id) => skills.find(s => s.id === id)?.name || id;
  function draw() {
    const v = ag.versions.find(x => x.n === selected), prev = ag.versions.find(x => x.n === selected - 1);
    const newer = (pin) => { const s = skills.find(x => x.id === pin.id); return s && s.current > pin.n ? s.current : 0; };
    const setDiff = (a, b) => { const A = new Set(a), B = new Set(b); return [...B].filter(x => !A.has(x)).map(x => '+ ' + x).concat([...A].filter(x => !B.has(x)).map(x => '− ' + x)); };
    const pinLabel = (p) => `${skillName(p.id)} v${p.n}`;
    const section = (title, ...kids) => h('section', { class: 'card card-pad' }, h('h2', { class: 'h-section mb-3' }, title), ...kids);
    mount(el,
      h('a', { class: 'muted mb-4 inline-flex items-center gap-1 text-sm hover:text-slate-900 dark:hover:text-white', href: '#/agents' }, icon('chevron', 'size-3 rotate-180'), 'All agents'),
      h('div', { class: 'mb-6 flex flex-wrap items-start gap-4' }, avatar(ag.name, ag.id, 'size-14 text-lg'),
        h('div', { class: 'min-w-0 flex-1' },
          h('h1', { class: 'h-page' }, ag.name),
          h('p', { class: 'muted mt-1 text-sm' }, ag.description || 'No description.'),
          h('div', { class: 'mt-2 flex flex-wrap items-center gap-2 text-xs' }, ag.disabled ? h('span', { class: 'pill-idle' }, 'Off') : h('span', { class: 'pill-ok' }, 'On'), tierPill(v.tier),
            h('span', { class: 'muted' }, 'Version ' + v.n + (v.n === ag.current ? ' (current)' : ' (older)')), shortHash(v.hash))),
        admin && h('div', { class: 'flex w-full flex-wrap gap-2 sm:w-auto' },
          h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'toggle-agent', onclick: async () => {
            try { await post('/v1/ai/agents/' + ag.id + (ag.disabled ? '/enable' : '/disable')); load(true); } catch (e) { toast(e.message, 'error'); }
          } }, ag.disabled ? 'Turn on' : 'Turn off'),
          h('button', { class: 'btn-primary btn-sm', id: 'new-version', type: 'button', onclick: () => agentForm(ag, skills, tools, () => load(false), v) }, icon('edit', 'size-3.5'), 'Edit'))),
      ag.disabled && h('div', { class: 'mb-4' }, notice('warn', 'This agent is off. It cannot run and the assistant cannot hand it work.')),
      h('div', { class: 'grid gap-4 lg:grid-cols-5' },
        h('div', { class: 'min-w-0 space-y-4 lg:col-span-3' },
          section('Instructions', markdownBox(v.instructions, 'agent-instructions')),
          section('Skills it follows',
            v.skills.length ? h('ul', { class: 'grid gap-2 sm:grid-cols-2' }, v.skills.map(p => h('li', {}, h('a', { class: 'flex items-center gap-2 rounded-lg border border-slate-200 px-3 py-2 text-sm hover:border-brand-300 dark:border-slate-700 dark:hover:border-brand-500/50', href: '#/skills/' + p.id },
              icon('list', 'size-4 text-slate-400'), h('span', { class: 'min-w-0 flex-1 truncate font-medium' }, skillName(p.id)), h('span', { class: 'muted text-xs' }, 'v' + p.n),
              newer(p) ? h('span', { class: 'pill-wait', title: 'A newer version exists. Edit the agent to use it.' }, 'v' + newer(p) + ' available') : null)))) : h('p', { class: 'muted text-sm' }, 'None.')),
          section('What it can use',
            v.tools.length ? toolChips(v.tools) : h('p', { class: 'muted text-sm' }, 'Nothing: it can only answer from what you tell it.'),
            h('p', { class: 'muted mt-3 flex items-start gap-1.5 text-xs' }, icon('shield', 'mt-0.5 size-3.5'), 'Every tool runs as the person who asks, with their role, so the agent can never do more than they could. A shield marks a tool that asks a person to approve.')),
          prev && section(`What changed from version ${prev.n}`,
            h('div', { class: 'h-section mb-1 mt-1' }, 'Instructions'), diffView(prev.instructions, v.instructions),
            (() => { const ch = [...setDiff(prev.skills.map(pinLabel), v.skills.map(pinLabel)).map(x => 'Skill ' + x), ...setDiff(prev.tools, v.tools).map(x => 'Tool ' + x), ...(prev.tier !== v.tier ? [`Tier ${tierTitle(prev.tier)} → ${tierTitle(v.tier)}`] : [])];
              return ch.length ? h('ul', { class: 'mt-3 space-y-0.5 text-sm', id: 'agent-changes' }, ch.map(c => h('li', {}, c))) : null; })()),
          h('section', { class: 'card' }, h('div', { class: 'border-b border-slate-200 px-5 py-3 dark:border-slate-800' }, h('h2', { class: 'h-section' }, 'Versions')),
            versionTable(ag.versions, ag.current, selected, (n) => { selected = n; draw(); }, admin && rollback))),
        h('div', { class: 'min-w-0 lg:col-span-2' }, chatPanel(ag, v))));
  }
  async function rollback(n) {
    if (!await confirmDialog({ title: `Make v${n} current again?`, body: `It is saved as a new version with the same instructions, skills, tools and tier as v${n}. Nothing is erased.`, confirmLabel: 'Roll back' })) return;
    try { await post('/v1/ai/agents/' + ag.id + '/rollback', { to: n }); toast('Rolled back', 'ok'); load(false); } catch (e) { toast(e.message, 'error'); }
  }
  mount(el, spinner());
  await load(false);
  return el;
}

function chatPanel(ag, v) {
  const history = [];
  const list = h('div', { class: 'max-h-[28rem] min-h-40 space-y-3 overflow-y-auto text-sm', id: 'agent-chat-log', 'aria-live': 'polite' },
    h('p', { class: 'muted' }, `Ask ${ag.name} something. It answers with version ${v.n}, using only its own tools, as you.`));
  const input = h('textarea', { id: 'agent-input', class: 'block max-h-40 min-h-[2.5rem] flex-1 resize-none bg-transparent py-2 text-sm placeholder:text-slate-400 focus:outline-none', rows: '1', maxlength: '4000', placeholder: 'Ask ' + ag.name + '…', 'aria-label': 'Message to the agent',
    onkeydown: (e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); e.target.form.requestSubmit(); } } });
  const send = h('button', { class: 'btn-primary size-9 shrink-0 rounded-xl p-0', type: 'submit', id: 'agent-send', 'aria-label': 'Send', title: 'Send' }, icon('send'));
  const bubble = (who, ...kids) => who === 'you'
    ? h('div', { class: 'flex justify-end' }, h('div', { class: 'max-w-[85%] rounded-2xl rounded-br-md bg-slate-900 px-3 py-2 text-white dark:bg-slate-700' }, ...kids))
    : h('div', { class: 'flex gap-2' }, avatar(ag.name, ag.id, 'size-7 text-[10px]'), h('div', { class: 'min-w-0 flex-1' }, ...kids));
  const form = h('form', { class: 'mt-3 flex items-end gap-2 rounded-2xl border border-slate-300 bg-white p-1.5 pl-3 shadow-sm focus-within:border-brand-500 focus-within:ring-2 focus-within:ring-brand-500/25 dark:border-slate-700 dark:bg-slate-900', onsubmit: async (e) => {
    e.preventDefault();
    const msg = input.value.trim();
    if (!msg || send.disabled) return;
    input.value = ''; send.disabled = true;
    if (list.querySelector('p.muted')) clear(list);
    list.append(bubble('you', h('div', { class: 'whitespace-pre-wrap' }, msg)));
    const wait = bubble('agent', h('span', { class: 'muted' }, 'Thinking…'));
    list.append(wait); list.scrollTop = list.scrollHeight;
    try {
      const r = await post('/v1/ai/agents/' + ag.id + '/chat', { message: msg, history, version: v.n });
      history.push({ role: 'user', content: msg }, { role: 'assistant', content: r.reply });
      wait.replaceWith(bubble('agent', h('div', { class: 'whitespace-pre-wrap', 'data-reply': '' }, r.reply),
        r.lookups?.length ? h('div', { class: 'muted mt-2 flex flex-wrap gap-1 text-xs' }, icon('search', 'size-3.5'), 'Looked at: ' + r.lookups.map(n => toolInfo(n).title.toLowerCase()).join(', ')) : null,
        r.proposals?.length ? h('div', { class: 'mt-2 text-xs' }, 'Filed: ', r.proposals.map(id => h('a', { class: 'underline', href: '#/proposals/' + id }, id))) : null,
        r.environments?.length ? h('div', { class: 'mt-2 text-xs' }, 'Environment requested: ', h('a', { class: 'underline', href: '#/environments' }, 'see Environments')) : null));
    } catch (err) { wait.replaceWith(bubble('agent', errorBox(err.message))); }
    send.disabled = false; list.scrollTop = list.scrollHeight;
  } }, input, send);
  return h('aside', { class: 'card card-pad h-fit lg:sticky lg:top-20' }, h('h2', { class: 'h-section mb-3' }, 'Talk to ' + ag.name), list, form);
}

function agentForm(ag, skills, tools, after, base) {
  modal(ag ? 'Edit ' + ag.name : 'New agent', (close) => {
    const cur = base || (ag && ag.versions.find(v => v.n === ag.current));
    const name = h('input', { id: 'agent-name', class: 'input', maxlength: '60', value: ag ? ag.name : '', placeholder: 'Cost watcher' });
    const desc = h('input', { id: 'agent-desc', class: 'input', maxlength: '300', value: ag ? ag.description : '', placeholder: 'Finds idle or oversized resources and suggests the smallest safe change' });
    // The tier is a hidden select so the rest of the code (and its checks) read it as before; people pick a card.
    const tier = h('select', { id: 'agent-tier', class: 'sr-only', tabindex: '-1', 'aria-hidden': 'true', onchange: () => { ins.refresh(); drawTiers(); } }, TIERS.map(t => h('option', { value: t.id, selected: (cur?.tier || 'T0') === t.id }, t.title)));
    const tierBox = h('div', { class: 'grid gap-3 sm:grid-cols-2', role: 'radiogroup', 'aria-label': 'How much it may do' });
    function drawTiers() {
      mount(tierBox, TIERS.map(t => {
        const on = tier.value === t.id;
        return h('button', { type: 'button', role: 'radio', 'aria-checked': String(on), 'data-tier': t.id,
          class: 'rounded-xl border p-3.5 text-left transition-colors ' + (on ? 'border-brand-500 bg-brand-50 ring-1 ring-brand-500 dark:bg-brand-500/10' : 'border-slate-200 hover:border-slate-300 dark:border-slate-700 dark:hover:border-slate-600'),
          onclick: () => { tier.value = t.id; tier.dispatchEvent(new Event('change')); } },
          h('div', { class: 'flex items-center gap-2 font-medium' }, h('span', { class: 'flex size-4 items-center justify-center rounded-full border ' + (on ? 'border-brand-600 bg-brand-600' : 'border-slate-400') }, on ? h('span', { class: 'size-1.5 rounded-full bg-white' }) : null), t.title),
          h('p', { class: 'muted mt-1 text-xs' }, t.text));
      }));
    }
    const ins = mdEditor({ id: 'agent-ins', value: cur ? cur.instructions : '', maxlength: 4096, placeholder: '## Goal\nFind idle or oversized resources and propose the smallest safe change.\n\n## Rules\n- Look before you suggest anything.\n- Ask before assuming a resource is unused.', check: (t) => checkInstructions(t, { tier: tier.value, max: 4096 }) });
    const picked = new Set((cur?.skills || []).map(p => p.id));
    const pins = Object.fromEntries((cur?.skills || []).map(p => [p.id, p.n]));
    const checkCard = (attrs, checked, title, sub, extra) => h('label', { class: 'flex cursor-pointer items-start gap-2.5 rounded-lg border border-slate-200 p-2.5 text-sm transition-colors hover:border-slate-300 has-[:checked]:border-brand-400 has-[:checked]:bg-brand-50/60 dark:border-slate-700 dark:has-[:checked]:border-brand-500/60 dark:has-[:checked]:bg-brand-500/10' },
      h('input', { type: 'checkbox', class: 'mt-0.5 size-4 accent-brand-600', checked, ...attrs }),
      h('span', { class: 'min-w-0 flex-1' }, h('span', { class: 'block font-medium' }, title), sub ? h('span', { class: 'muted block text-xs' }, sub) : null, extra || null));
    const skillBoxes = skills.length ? h('div', { class: 'grid gap-2 sm:grid-cols-2' }, skills.map(s => checkCard({ 'data-skill': s.id, onchange: (e) => { if (!e.target.checked) delete pins[s.id]; } }, picked.has(s.id), s.name,
      s.description || '', h('span', { class: 'muted block text-[11px]' }, picked.has(s.id) ? `Pinned at v${pins[s.id]} · latest v${s.current}` : `Latest v${s.current}`))))
      : h('p', { class: 'muted text-sm' }, 'No skills yet. Create one under Agents › Skills, then add it here.');
    const toolSet = new Set(cur?.tools || []);
    const groups = groupTools(tools);
    const toolBoxes = h('div', { class: 'space-y-4' }, groups.map(g => {
      const boxes = g.tools.map(t => checkCard({ 'data-tool': t.name }, toolSet.has(t.name), t.title, plainHelp(t.help),
        h('span', { class: 'mt-1 flex flex-wrap gap-1' }, h('code', { class: 'rounded bg-slate-100 px-1 text-[10px] text-slate-500 dark:bg-slate-800' }, t.name),
          t.asks ? h('span', { class: 'pill-wait px-1.5 py-0 text-[10px]' }, 'a person approves') : null,
          t.needs === 'mcp' ? h('span', { class: 'pill-idle px-1.5 py-0 text-[10px]' }, 'needs the AWS MCP Server') : null,
          t.min && t.min !== 'viewer' ? h('span', { class: 'pill-idle px-1.5 py-0 text-[10px]' }, t.min + ' and up') : null)));
      return h('fieldset', {}, h('legend', { class: 'mb-1.5 flex w-full items-baseline justify-between gap-2' }, h('span', { class: 'text-sm font-semibold' }, g.title), h('span', { class: 'muted text-xs' }, g.note)),
        h('div', { class: 'grid gap-2 sm:grid-cols-2' }, boxes));
    }));
    const note = h('input', { id: 'agent-note', class: 'input', maxlength: '200', placeholder: ag ? 'What changed and why' : 'First version' });
    const err = h('div', { class: 'hidden' });
    const form = h('div', {});
    const go = h('button', { class: 'btn-primary', type: 'button', id: 'agent-save', onclick: async () => {
      err.classList.add('hidden'); go.disabled = true;
      const body = { name: name.value, description: desc.value, instructions: ins.input.value, tier: tier.value, note: note.value,
        // A skill already pinned stays at its pinned version; a newly ticked one takes the latest.
        skills: [...form.querySelectorAll('[data-skill]:checked')].map(b => ({ id: b.dataset.skill, n: pins[b.dataset.skill] || 0 })),
        tools: [...form.querySelectorAll('[data-tool]:checked')].map(b => b.dataset.tool) };
      try {
        const out = await post(ag ? '/v1/ai/agents/' + ag.id + '/versions' : '/v1/ai/agents', body);
        toast(ag ? 'Saved version ' + out.current : 'Agent created', 'ok'); close();
        if (ag) after(); else navigate('/agents/' + out.id);
      } catch (e) { mount(err, errorBox(e.message)); err.classList.remove('hidden'); go.disabled = false; }
    } }, ag ? 'Save as a new version' : 'Create agent');
    const step = (title, text, ...kids) => h('section', { class: 'space-y-3 border-t border-slate-200 pt-5 first:border-t-0 first:pt-0 dark:border-slate-800' },
      h('div', {}, h('h3', { class: 'font-display text-base font-bold' }, title), text ? h('p', { class: 'muted text-xs' }, text) : null), ...kids);
    drawTiers();
    form.append(h('div', { class: 'space-y-6' },
      step('Who it is', null, h('div', { class: 'grid gap-4 sm:grid-cols-2' }, field('Name', name, null, 'agent-name'), field('What it is for', desc, null, 'agent-desc'))),
      step('How much it may do', 'Either way it works as the person who asks, and can never do more than they could.', tierBox, tier),
      step('Instructions', 'What it should do and how. Write in Markdown; the model receives exactly this text.', ins.el),
      step('Skills it follows', ag ? 'Skills stay on the version they were pinned to. Untick and tick one to take its latest version.' : 'Step-by-step procedures it can follow.', skillBoxes),
      step('What it can use', 'Pick the fewest it needs. Administrators can turn services off for every agent in Settings › Connections.', toolBoxes),
      step('Save', null, field('Note for the history', note, null, 'agent-note'), err,
        h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'), go))));
    return form;
  }, { xl: true });
}
