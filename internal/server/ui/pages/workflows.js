import { h, icon, mount, clear, pageHeader, pill, relTime, absTime, emptyState, spinner, errorBox, notice, toast, modal, confirmDialog, field, duration } from '../lib/dom.js';
import { get, post, atLeast } from '../lib/api.js';
import { navigate } from '../lib/router.js';
import { session } from '../app.js';
import { layoutWorkflow, edgePath, runStatuses, edgeLive, NODE_W, NODE_H } from '../lib/wfgraph.js';
import { fit } from '../lib/canvas.js';
import { flowPath } from '../lib/flow.js';

// Workflows: stages of agents, run in order. Steps in a stage run together; a gated stage waits for
// an administrator. Each save is a new version, like skills and agents.

const SCHEDULES = [[0, 'Only when started by hand'], [15, 'Every 15 minutes'], [60, 'Every hour'], [360, 'Every 6 hours'], [1440, 'Every day'], [10080, 'Every week']];
const scheduleLabel = (m) => (SCHEDULES.find(s => s[0] === m) || [m, `Every ${m} minutes`])[1];
const shortHash = (x) => h('code', { class: 'text-xs', title: x }, x.slice(0, 12));
// The workflow as a graph, left to right (what starts it, an approval before a gated stage, the steps of each stage that
// run together). With statuses (a run) each node shows how far it got and the edge into what is working moves.
const NODE_TONE = {
  pending: 'stroke-slate-300 dark:stroke-slate-600', running: 'stroke-brand-500 motion-safe:animate-pulse', succeeded: 'stroke-emerald-500',
  failed: 'stroke-rose-500', waiting: 'stroke-amber-500', skipped: 'stroke-slate-300 dark:stroke-slate-700', cancelled: 'stroke-slate-300 dark:stroke-slate-700',
};
const NODE_WORD = { pending: 'waiting to start', running: 'working', succeeded: 'done', failed: 'failed', waiting: 'waits for approval', skipped: 'skipped', cancelled: 'cancelled' };
const KIND_ICON = { trigger: '▶', gate: '✋', step: '●' };
function workflowGraph(layout, statuses = null) {
  const byId = Object.fromEntries(layout.nodes.map(n => [n.id, n]));
  const svg = h('svg', { width: String(layout.w), height: String(layout.h), viewBox: `0 0 ${layout.w} ${layout.h}`, class: 'block h-auto max-w-full min-w-[32rem] select-none', role: 'img',
    'aria-label': 'The workflow as a graph: ' + layout.nodes.map(n => n.label + (statuses ? ' (' + NODE_WORD[statuses[n.id]] + ')' : '')).join(', then ') });
  for (const e of layout.edges) {
    const d = edgePath(byId[e.from], byId[e.to]);
    const live = statuses && edgeLive(statuses, e);
    svg.append(h('path', { d, fill: 'none', 'stroke-width': '1.5', class: live ? 'stroke-brand-500' : 'stroke-slate-300 dark:stroke-slate-600', 'data-edge': e.from + '>' + e.to }));
    if (live) svg.append(flowPath(d, 'stroke-brand-500'));
  }
  for (const n of layout.nodes) {
    const st = statuses ? statuses[n.id] : 'pending';
    const tone = statuses ? NODE_TONE[st] || NODE_TONE.pending : n.kind === 'gate' ? 'stroke-amber-400' : n.kind === 'trigger' ? 'stroke-brand-400' : 'stroke-slate-300 dark:stroke-slate-600';
    const g = h('g', { 'data-node': n.id, 'data-status': statuses ? st : '' },
      h('rect', { x: String(n.x), y: String(n.y), width: String(NODE_W), height: String(NODE_H), rx: '12', 'stroke-width': statuses && st !== 'pending' ? '2' : '1.5',
        'stroke-dasharray': st === 'skipped' || st === 'cancelled' ? '4 3' : '', class: 'fill-white dark:fill-slate-900 ' + tone }),
      h('text', { x: String(n.x + 12), y: String(n.y + 24), class: 'fill-slate-900 text-[13px] font-semibold dark:fill-slate-100' }, KIND_ICON[n.kind] + ' ' + fit(n.label, 22)),
      h('text', { x: String(n.x + 12), y: String(n.y + 42), class: 'fill-slate-500 text-[11px] dark:fill-slate-400' }, fit(n.kind === 'step' ? n.stageName + (n.sub ? ' · ' + n.sub : '') : n.sub, 30)),
      statuses ? h('text', { x: String(n.x + NODE_W - 12), y: String(n.y + 56), 'text-anchor': 'end', class: 'fill-slate-500 text-[10px] dark:fill-slate-400' }, NODE_WORD[st] || st) : null,
      h('title', {}, n.label + (n.sub ? ': ' + n.sub : '') + (statuses ? ' (' + (NODE_WORD[st] || st) + ')' : '')));
    svg.append(g);
  }
  return h('div', { class: 'overflow-x-auto rounded-xl border border-slate-200 bg-slate-50 p-2 dark:border-slate-800 dark:bg-slate-950/40', id: 'workflow-graph' }, svg);
}

const statusPill = (s) => pill(s, s === 'waiting' ? 'Waiting for approval' : s === 'cancelled' ? 'Cancelled' : undefined);

// ------------------------------------------------------------------ list (a tab on the Agents page)

export function workflowList(workflows, admin) {
  if (!workflows.length) return emptyState('flow', 'No workflows yet', admin ? 'A workflow runs agents in stages, optionally on a schedule, and can pause for your approval before a stage.' : 'An administrator can create workflows.');
  return h('div', { class: 'card overflow-x-auto' }, h('table', { class: 'tbl' }, h('thead', {}, h('tr', {}, ['Workflow', 'Stages', 'Runs', 'Run as', 'Version'].map(x => h('th', {}, x)))),
    h('tbody', {}, workflows.map(w => h('tr', { class: 'row-link', tabindex: '0', 'data-workflow': w.id, onclick: () => navigate('/workflows/' + w.id), onkeydown: (e) => { if (e.key === 'Enter') navigate('/workflows/' + w.id); } },
      h('td', {}, h('div', { class: 'font-medium' }, w.name, w.disabled && h('span', { class: 'pill-idle ml-2' }, 'off')), h('div', { class: 'muted max-w-md truncate text-xs' }, w.description)),
      h('td', { class: 'text-sm' }, `${w.stages} stages, ${w.steps} steps`), h('td', { class: 'text-sm' }, scheduleLabel(w.every_minutes)),
      h('td', { class: 'text-sm' }, w.run_as), h('td', { class: 'text-sm' }, 'v' + w.current))))));
}

// ------------------------------------------------------------------ workflow page

export async function workflowPage({ params }) {
  const admin = atLeast(session.role, 'admin'), dev = atLeast(session.role, 'developer');
  const el = h('div', {});
  let wf, runs, agents, selected = 0;
  async function load(keep) {
    try { [wf, runs, agents] = await Promise.all([get('/v1/ai/workflows/' + encodeURIComponent(params.id)), get('/v1/ai/workflow-runs?workflow=' + encodeURIComponent(params.id)).then(r => r.runs), get('/v1/ai/agents').then(r => r.agents)]); } catch (e) { return mount(el, errorBox(e.message)); }
    if (!keep || !wf.versions.some(v => v.n === selected)) selected = wf.current;
    draw();
  }
  const agentName = (id) => agents.find(a => a.id === id)?.name || id;
  function draw() {
    const v = wf.versions.find(x => x.n === selected);
    const active = runs.find(r => r.status === 'running' || r.status === 'waiting');
    const start = h('button', { class: 'btn-primary btn-sm', id: 'run-workflow', type: 'button', disabled: !dev || wf.disabled || !!active, title: active ? 'A run is already in progress' : '', onclick: async () => {
      try { const r = await post('/v1/ai/workflows/' + wf.id + '/run', { version: v.n }); navigate('/workflow-runs/' + r.id); } catch (e) { toast(e.message, 'error'); }
    } }, icon('play', 'size-3.5'), 'Run v' + v.n);
    mount(el, pageHeader(wf.name, wf.description, h('a', { class: 'btn-secondary btn-sm', href: '#/agents?tab=workflows' }, 'All workflows'), start,
      admin && h('button', { class: 'btn-secondary btn-sm', id: 'new-version', type: 'button', onclick: () => workflowForm(wf, agents, () => load(false), v) }, icon('plus', 'size-3.5'), 'New version'),
      admin && h('button', { class: 'btn-secondary btn-sm', type: 'button', onclick: async () => { try { await post('/v1/ai/workflows/' + wf.id + (wf.disabled ? '/enable' : '/disable')); load(true); } catch (e) { toast(e.message, 'error'); } } }, wf.disabled ? 'Turn on' : 'Turn off')),
      wf.disabled && h('div', { class: 'mb-4' }, notice('warn', 'This workflow is off. It cannot run and its schedule is paused.')),
      h('div', { class: 'space-y-4' },
        h('section', { class: 'card card-pad' },
          h('div', { class: 'mb-3 flex flex-wrap items-center justify-between gap-2' }, h('h2', { class: 'h-section' }, 'Version ' + v.n + (v.n === wf.current ? ' (current)' : '')),
            h('div', { class: 'flex flex-wrap items-center gap-2 text-sm' }, h('span', { class: 'pill-idle' }, scheduleLabel(v.every_minutes || 0)), h('span', { class: 'pill-idle', title: 'The most a run of this workflow may do. Never an administrator.' }, 'runs as ' + v.run_as), shortHash(v.hash))),
          workflowGraph(layoutWorkflow(v, agentName)),
          h('h3', { class: 'h-section mb-2 mt-4' }, 'Stages in detail'),
          h('ol', { class: 'space-y-3', id: 'workflow-stages' }, v.stages.map((st, i) => h('li', { class: 'rounded-lg border border-slate-200 p-3 dark:border-slate-800' },
            h('div', { class: 'mb-2 flex items-center gap-2 text-sm font-medium' }, h('span', { class: 'muted' }, (i + 1) + '.'), st.name, st.gate && h('span', { class: 'pill-wait', title: 'An administrator releases this stage' }, icon('lock', 'size-3'), 'waits for approval')),
            h('div', { class: 'grid gap-2 ' + (st.steps.length > 1 ? 'sm:grid-cols-2' : '') }, st.steps.map(sp => h('div', { class: 'rounded-md bg-slate-50 p-2.5 text-sm dark:bg-slate-900' },
              h('div', { class: 'mb-1 flex items-center justify-between gap-2' }, h('a', { class: 'font-medium text-brand-700 hover:underline dark:text-brand-200', href: '#/agents/' + sp.agent }, agentName(sp.agent)), h('span', { class: 'muted text-xs' }, 'v' + sp.agent_n)),
              h('p', { class: 'whitespace-pre-wrap' }, sp.task)))),
            st.steps.length > 1 && h('p', { class: 'muted mt-1.5 text-xs' }, 'These steps run at the same time.'))))),
        h('section', { class: 'card' }, h('div', { class: 'border-b border-slate-200 px-5 py-3 dark:border-slate-800' }, h('h2', { class: 'h-section' }, 'Runs')),
          runs.length ? h('div', { class: 'overflow-x-auto' }, h('table', { class: 'tbl', id: 'run-table' }, h('thead', {}, h('tr', {}, ['Started', 'Version', 'By', 'How', 'Status', 'Proposals'].map(x => h('th', {}, x)))),
            h('tbody', {}, runs.slice(0, 15).map(r => h('tr', { class: 'row-link', tabindex: '0', onclick: () => navigate('/workflow-runs/' + r.id), onkeydown: (e) => { if (e.key === 'Enter') navigate('/workflow-runs/' + r.id); } },
              h('td', { class: 'muted whitespace-nowrap text-sm', title: absTime(r.started_at) }, relTime(r.started_at)), h('td', { class: 'text-sm' }, 'v' + r.version), h('td', { class: 'text-sm' }, r.by),
              h('td', { class: 'text-sm' }, r.trigger === 'schedule' ? 'schedule' : 'by hand'), h('td', {}, statusPill(r.status)), h('td', { class: 'text-sm' }, ((r.proposals || []).length + (r.environments || []).length) || h('span', { class: 'muted' }, '–')))))))
            : h('p', { class: 'muted p-5 text-sm' }, 'No runs yet.')),
        h('section', { class: 'card' }, h('div', { class: 'border-b border-slate-200 px-5 py-3 dark:border-slate-800' }, h('h2', { class: 'h-section' }, 'Versions')),
          h('div', { class: 'overflow-x-auto' }, h('table', { class: 'tbl', id: 'version-table' }, h('thead', {}, h('tr', {}, ['Version', 'Saved by', 'When', 'Note', 'Fingerprint', ''].map(x => h('th', {}, x)))),
            h('tbody', {}, [...wf.versions].reverse().map(x => h('tr', { class: 'row-link' + (x.n === selected ? ' bg-brand-50/60 dark:bg-brand-950/20' : ''), tabindex: '0', 'data-version': String(x.n), onclick: () => { selected = x.n; draw(); } },
              h('td', { class: 'whitespace-nowrap font-medium' }, 'v' + x.n, x.n === wf.current && h('span', { class: 'pill-ok ml-2' }, 'current')), h('td', { class: 'text-sm' }, x.by),
              h('td', { class: 'muted whitespace-nowrap text-sm', title: absTime(x.at) }, relTime(x.at)), h('td', { class: 'max-w-xs truncate text-sm' }, x.note || h('span', { class: 'muted' }, '–')), h('td', {}, shortHash(x.hash)),
              h('td', { class: 'text-right' }, admin && x.n !== wf.current && h('button', { class: 'btn-ghost btn-sm', type: 'button', 'data-rollback': String(x.n), onclick: async (e) => {
                e.stopPropagation();
                if (!await confirmDialog({ title: `Make v${x.n} current again?`, body: 'It is saved as a new version with the same stages and schedule. Nothing is erased.', confirmLabel: 'Roll back' })) return;
                try { await post('/v1/ai/workflows/' + wf.id + '/rollback', { to: x.n }); toast('Rolled back', 'ok'); load(false); } catch (err) { toast(err.message, 'error'); }
              } }, icon('history', 'size-3.5'), 'Roll back to this'))))))))));
  }
  mount(el, spinner());
  await load(false);
  return el;
}

// ------------------------------------------------------------------ form

export function workflowForm(wf, agents, after, base) {
  modal(wf ? 'New version of ' + wf.name : 'New workflow', (close) => {
    const cur = base || (wf && wf.versions.find(v => v.n === wf.current));
    const live = agents.filter(a => !a.disabled);
    if (!live.length) return h('div', { class: 'space-y-3' }, notice('info', 'Create an agent first: every step is an agent doing one task.'), h('div', { class: 'flex justify-end' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Close')));
    const stages = cur ? cur.stages.map(s => ({ name: s.name, gate: !!s.gate, steps: s.steps.map(p => ({ agent: p.agent, agent_n: p.agent_n, task: p.task })) })) : [{ name: 'Stage 1', gate: false, steps: [{ agent: live[0].id, agent_n: 0, task: '' }] }];
    const name = h('input', { id: 'wf-name', class: 'input', maxlength: '60', value: wf ? wf.name : '', placeholder: 'Weekly cost review' });
    const desc = h('input', { id: 'wf-desc', class: 'input', maxlength: '300', value: wf ? wf.description : '', placeholder: 'What it is for' });
    const every = h('select', { id: 'wf-every', class: 'input' }, SCHEDULES.map(([v, l]) => h('option', { value: String(v), selected: (cur?.every_minutes || 0) === v }, l)));
    const runAs = h('select', { id: 'wf-runas', class: 'input' }, [['developer', 'Developer: may file change proposals'], ['viewer', 'Viewer: reads only']].map(([v, l]) => h('option', { value: v, selected: (cur?.run_as || 'developer') === v }, l)));
    const note = h('input', { id: 'wf-note', class: 'input', maxlength: '200', placeholder: wf ? 'What changed and why' : 'First version' });
    const err = h('div', { class: 'hidden' });
    const box = h('div', { class: 'space-y-3', id: 'wf-stages' });
    function drawStages() {
      clear(box);
      stages.forEach((st, i) => {
        const sname = h('input', { class: 'input w-48', value: st.name, maxlength: '60', 'aria-label': 'Stage name', oninput: (e) => { st.name = e.target.value; } });
        const gate = h('label', { class: 'flex items-center gap-1.5 text-sm' }, h('input', { type: 'checkbox', checked: st.gate, 'data-gate': String(i), onchange: (e) => { st.gate = e.target.checked; } }), 'Wait for an administrator first');
        box.append(h('div', { class: 'rounded-lg border border-slate-200 p-3 dark:border-slate-800', 'data-stage': String(i) },
          h('div', { class: 'mb-2 flex flex-wrap items-center gap-3' }, h('span', { class: 'muted text-sm' }, (i + 1) + '.'), sname, gate, h('div', { class: 'flex-1' }),
            stages.length > 1 && h('button', { class: 'btn-ghost btn-sm text-rose-600', type: 'button', onclick: () => { stages.splice(i, 1); drawStages(); } }, icon('trash', 'size-3.5'), 'Remove stage')),
          h('div', { class: 'space-y-2' }, st.steps.map((sp, j) => {
            const sel = h('select', { class: 'input w-48', 'aria-label': 'Agent', 'data-step-agent': i + '-' + j, onchange: (e) => { sp.agent = e.target.value; sp.agent_n = 0; } }, live.map(a => h('option', { value: a.id, selected: a.id === sp.agent }, a.name)));
            const task = h('textarea', { class: 'input h-16 flex-1 text-sm', maxlength: '1000', placeholder: 'What this agent should do in this step', 'data-step-task': i + '-' + j, oninput: (e) => { sp.task = e.target.value; } }, sp.task);
            return h('div', { class: 'flex items-start gap-2' }, sel, task, st.steps.length > 1 && h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Remove step', onclick: () => { st.steps.splice(j, 1); drawStages(); } }, icon('x', 'size-3.5')));
          })),
          st.steps.length < 4 && h('button', { class: 'btn-ghost btn-sm mt-2', type: 'button', 'data-add-step': String(i), onclick: () => { st.steps.push({ agent: live[0].id, agent_n: 0, task: '' }); drawStages(); } }, icon('plus', 'size-3.5'), 'Add a step that runs at the same time')));
      });
      if (stages.length < 6) box.append(h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'add-stage', onclick: () => { stages.push({ name: 'Stage ' + (stages.length + 1), gate: false, steps: [{ agent: live[0].id, agent_n: 0, task: '' }] }); drawStages(); } }, icon('plus', 'size-3.5'), 'Add a stage'));
    }
    drawStages();
    const go = h('button', { class: 'btn-primary', type: 'button', id: 'wf-save', onclick: async () => {
      err.classList.add('hidden'); go.disabled = true;
      try {
        const out = await post(wf ? '/v1/ai/workflows/' + wf.id + '/versions' : '/v1/ai/workflows', { name: name.value, description: desc.value, every_minutes: Number(every.value), run_as: runAs.value, note: note.value, stages });
        toast(wf ? 'Saved v' + out.current : 'Workflow created', 'ok'); close();
        if (wf) after(); else navigate('/workflows/' + out.id);
      } catch (e) { mount(err, errorBox(e.message)); err.classList.remove('hidden'); go.disabled = false; }
    } }, wf ? 'Save as a new version' : 'Create workflow');
    return h('div', { class: 'space-y-4' }, h('div', { class: 'grid gap-4 sm:grid-cols-2' }, field('Name', name, null, 'wf-name'), field('Description', desc, null, 'wf-desc')),
      h('div', {}, h('div', { class: 'h-section mb-1' }, 'Stages'), box, h('p', { class: 'muted mt-1 text-xs' }, 'Stages run in order and each gets what the earlier ones found. Steps inside one stage run together. A step\'s agent is pinned at its current version when you save.')),
      h('div', { class: 'grid gap-4 sm:grid-cols-2' }, field('Schedule', every, null, 'wf-every'), field('Runs as', runAs, 'Nobody watches an unattended run, so it never runs as an administrator. Anything it proposes still needs a person to approve.', 'wf-runas')),
      field('Note', note, null, 'wf-note'), err, h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'), go));
  }, { wide: true });
}

// ------------------------------------------------------------------ run page

export async function workflowRunPage({ params }) {
  const admin = atLeast(session.role, 'admin');
  const el = h('div', {});
  let timer;
  async function load() {
    let r;
    try { r = await get('/v1/ai/workflow-runs/' + encodeURIComponent(params.id)); } catch (e) { return mount(el, errorBox(e.message)); }
    const live = r.status === 'running' || r.status === 'waiting';
    mount(el, pageHeader(r.workflow_name, `Run of version ${r.version}, ${r.trigger === 'schedule' ? 'by its schedule' : 'started by ' + r.by}, ${relTime(r.started_at)}`,
      h('a', { class: 'btn-secondary btn-sm', href: '#/workflows/' + r.workflow }, 'The workflow'),
      live && h('button', { class: 'btn-secondary btn-sm', id: 'cancel-run', type: 'button', onclick: async () => {
        if (!await confirmDialog({ title: 'Cancel this run?', body: 'Steps already finished keep their results. Nothing further starts.', confirmLabel: 'Cancel the run', cancelLabel: 'Keep running', danger: true })) return;
        try { await post('/v1/ai/workflow-runs/' + r.id + '/cancel'); toast('Cancelled', 'ok'); load(); } catch (e) { toast(e.message, 'error'); }
      } }, 'Cancel run')),
      h('div', { class: 'mb-4 flex flex-wrap items-center gap-3 text-sm' }, statusPill(r.status), h('span', { class: 'muted' }, 'runs as ' + r.run_as), shortHash(r.hash),
        r.finished_at && h('span', { class: 'muted' }, 'took ' + duration(r.started_at, r.finished_at))),
      r.error && h('div', { class: 'mb-4' }, r.status === 'failed' ? errorBox(r.error) : notice('info', r.error)),
      r.environments?.length ? h('div', { class: 'mb-4' }, notice('info', 'Environments requested: ', r.environments.length, '. They wait for an administrator. ', h('a', { class: 'underline', href: '#/environments' }, 'See Environments'))) : null,
      r.proposals?.length ? h('div', { class: 'mb-4' }, notice('info', 'Filed for approval: ', r.proposals.map(id => h('a', { class: 'mr-2 underline', href: '#/proposals/' + id }, id)), ' Nothing changes until a different administrator approves.')) : null,
      runGraph(r),
      h('ol', { class: 'space-y-3', id: 'run-stages' }, r.stages.map((st, i) => h('li', { class: 'card card-pad', 'data-stage-status': st.status },
        h('div', { class: 'mb-2 flex flex-wrap items-center justify-between gap-2' }, h('div', { class: 'flex items-center gap-2 font-medium' }, h('span', { class: 'muted' }, (i + 1) + '.'), st.name, st.gate && h('span', { class: 'pill-idle' }, icon('lock', 'size-3'), st.gate_by ? 'released by ' + st.gate_by : 'gated')), statusPill(st.status)),
        st.status === 'waiting' && h('div', { class: 'mb-3' }, notice('warn', 'This stage waits for an administrator. ', admin ? h('button', { class: 'btn-primary btn-sm ml-2', id: 'release-gate', type: 'button', onclick: async () => {
          try { await post('/v1/ai/workflow-runs/' + r.id + '/release'); toast('Released', 'ok'); load(); } catch (e) { toast(e.message, 'error'); }
        } }, 'Release this stage') : 'Ask an administrator to release it.')),
        h('div', { class: 'space-y-2' }, st.steps.map(sp => h('div', { class: 'rounded-md bg-slate-50 p-2.5 text-sm dark:bg-slate-900', 'data-step-status': sp.status },
          h('div', { class: 'mb-1 flex flex-wrap items-center justify-between gap-2' }, h('span', { class: 'font-medium' }, sp.agent_label || sp.agent), statusPill(sp.status)),
          sp.error && h('p', { class: 'text-rose-700 dark:text-rose-400' }, sp.error),
          sp.output && h('pre', { class: 'whitespace-pre-wrap text-sm', 'data-output': '' }, sp.output),
          sp.lookups?.length ? h('p', { class: 'muted mt-1 text-xs' }, 'Looked up: ' + sp.lookups.join(', ')) : null)))))));
    clearTimeout(timer);
    if (live) timer = setTimeout(() => { if (document.body.contains(el)) load(); }, 3000);
  }
  mount(el, spinner());
  await load();
  el.cleanup = () => clearTimeout(timer);
  return el;
}

// A run's graph: the stages as the run recorded them (the version it ran), each node with its status.
function runGraph(r) {
  const v = { every_minutes: r.trigger === 'schedule' ? 1 : 0, stages: (r.stages || []).map(st => ({ name: st.name, gate: !!st.gate, steps: (st.steps || []).map(sp => ({ agent: sp.agent, task: '' })) })) };
  const layout = layoutWorkflow(v, (id) => id);
  for (const n of layout.nodes) if (n.kind === 'trigger') { n.label = r.trigger === 'schedule' ? 'By its schedule' : 'Started by ' + r.by; n.sub = relTime(r.started_at); }
  for (const n of layout.nodes) if (n.kind === 'step') { const sp = r.stages[n.stage].steps[n.step]; n.label = sp.agent_label || sp.agent; n.sub = (sp.output || sp.error || '').split('\n').find(l => l.trim()) || (sp.lookups?.length ? sp.lookups.length + ' lookup' + (sp.lookups.length === 1 ? '' : 's') : ''); }
  return h('div', { class: 'mb-4' }, workflowGraph(layout, runStatuses(layout, r)));
}
