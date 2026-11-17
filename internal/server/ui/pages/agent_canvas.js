// Agent graphs on a canvas (design note 044, part 2), like n8n: nodes as cards, wires from the right port of one to
// the left of the next, a palette to add nodes, a panel to set the selected one. The same canvas shows a run: each
// node's status and visits, the wires taken, and a question waiting for an answer. The server checks every save.
import { h, icon, mount, pageHeader, spinner, errorBox, notice, toast, field, pill } from '../lib/dom.js';
import { get, post, atLeast } from '../lib/api.js';
import { navigate } from '../lib/router.js';
import { session } from '../app.js';
import { fit } from '../lib/canvas.js';
import { flowPath } from '../lib/flow.js';
import {
  GNODE_W, GNODE_H, NODE_KINDS, PALETTE, branches, branchLabels, emptyGraph, addNode, updateNode, moveNode, removeNode, tidy,
  addWire, removeWire, wireProblem, nextLabel, stateKeys, autoPlace, graphBounds, wirePath, labelPoint, nodeSummary,
  graphRunStatus, stagesToGraph,
} from '../lib/agraph.js';

const SCHEDULES = [[0, 'Only when started by hand'], [15, 'Every 15 minutes'], [60, 'Every hour'], [360, 'Every 6 hours'], [1440, 'Every day'], [10080, 'Every week']];
const TONE = {
  pending: 'stroke-slate-300 dark:stroke-slate-600', running: 'stroke-brand-500 motion-safe:animate-pulse', succeeded: 'stroke-emerald-500',
  failed: 'stroke-rose-500', waiting: 'stroke-amber-500', cancelled: 'stroke-slate-300 dark:stroke-slate-700',
};
const WORD = { pending: '', running: 'working', succeeded: 'done', failed: 'failed', waiting: 'waits for an answer', cancelled: 'cancelled' };
const KIND_TONE = { start: 'stroke-brand-400', ask: 'stroke-amber-400', decide: 'stroke-violet-400', end: 'stroke-slate-400' };
const OPS = [['equals', 'equals'], ['contains', 'contains'], ['gt', 'is more than'], ['lt', 'is less than'], ['empty', 'is empty']];
const NO_LOOKUP = new Set(['ask_agent', 'request_environment', 'prepare_action']);

/**
 * Draws a graph. opts: { selected, statuses (from graphRunStatus), editable, agentName, onSelect(id), onMove(id, x, y),
 * onWire(from, to) }. Returns the scrolling box with the SVG in it.
 */
export function graphCanvas(g, opts = {}) {
  const { selected = '', statuses = null, editable = false, agentName = (id) => id } = opts;
  const { w, h: ht } = graphBounds(g);
  const byId = Object.fromEntries(g.nodes.map(n => [n.id, n]));
  const svg = h('svg', { width: String(w), height: String(ht), viewBox: `0 0 ${w} ${ht}`, class: 'block select-none', id: 'graph-svg', role: 'group',
    'aria-label': 'The workflow as a graph of ' + g.nodes.length + ' nodes' });
  for (const e of g.edges) {
    const a = byId[e.from], b = byId[e.to];
    if (!a || !b) continue;
    const d = wirePath(a, b);
    const taken = statuses && statuses.taken.has(e.from + '>' + e.to + '>' + (e.when || ''));
    svg.append(h('path', { d, fill: 'none', 'stroke-width': taken ? '2.5' : '1.5', class: taken ? 'stroke-brand-500' : 'stroke-slate-300 dark:stroke-slate-600', 'data-wire': e.from + '>' + e.to + (e.when ? '>' + e.when : '') }));
    if (taken && statuses.nodes[e.to]?.status === 'running') svg.append(flowPath(d, 'stroke-brand-500'));
    if (e.when) {
      const p = labelPoint(a, b);
      svg.append(h('text', { x: String(p.x), y: String(p.y), class: 'fill-violet-600 text-[11px] font-medium dark:fill-violet-300', 'data-wire-label': e.when }, e.when));
    }
  }
  for (const n of g.nodes) {
    const k = NODE_KINDS[n.kind] || { label: n.kind };
    const st = statuses?.nodes[n.id];
    const tone = st ? TONE[st.status] || TONE.pending : n.id === selected ? 'stroke-brand-500' : KIND_TONE[n.kind] || 'stroke-slate-300 dark:stroke-slate-600';
    const grp = h('g', { transform: `translate(${n.x} ${n.y})`, tabindex: '0', role: 'button', 'aria-pressed': String(n.id === selected), 'data-node': n.id, 'data-status': st?.status || '',
      'aria-label': (n.name || k.label) + (st ? ', ' + (WORD[st.status] || st.status) : ''), class: (editable ? 'cursor-grab ' : 'cursor-pointer ') + 'focus:outline-none' },
      h('rect', { width: String(GNODE_W), height: String(GNODE_H), rx: '12', 'stroke-width': n.id === selected ? '2.5' : st && st.status !== 'pending' ? '2' : '1.5',
        'stroke-dasharray': st?.status === 'cancelled' ? '4 3' : '', class: 'fill-white dark:fill-slate-900 ' + tone }),
      h('text', { x: '12', y: '23', class: 'fill-slate-900 text-[13px] font-semibold dark:fill-slate-100' }, fit(n.name || (n.kind === 'agent' ? agentName(n.agent) : k.label), 20)),
      h('text', { x: String(GNODE_W - 10), y: '22', 'text-anchor': 'end', class: 'fill-slate-400 text-[10px] uppercase tracking-wide' }, k.label),
      h('text', { x: '12', y: '42', class: 'fill-slate-500 text-[11px] dark:fill-slate-400' }, fit(nodeSummary(n, agentName), 30)),
      st && st.status !== 'pending' ? h('text', { x: '12', y: '57', class: 'fill-slate-500 text-[10px] dark:fill-slate-400' }, (WORD[st.status] || st.status) + (st.visits > 1 ? ' · ran ' + st.visits + ' times' : '')) : null,
      n.kind !== 'start' ? h('circle', { cx: '0', cy: String(GNODE_H / 2), r: '4', class: 'fill-white stroke-slate-400 dark:fill-slate-900' }) : null,
      editable && n.kind !== 'end' ? h('circle', { cx: String(GNODE_W), cy: String(GNODE_H / 2), r: '7', 'data-port': n.id, class: 'cursor-crosshair fill-brand-500 stroke-white hover:fill-brand-600 dark:stroke-slate-900', 'aria-hidden': 'true' }) : null,
      h('title', {}, (n.name || k.label) + ': ' + nodeSummary(n, agentName)));
    grp.addEventListener('pointerdown', (ev) => {
      if (ev.button !== 0) return;
      const port = ev.target.closest?.('[data-port]');
      const scale = svg.getBoundingClientRect().width / w || 1;
      const origin = svg.getBoundingClientRect();
      if (port && editable && opts.onWire) {
        // Draw a wire: follow the pointer, and drop on another node.
        ev.preventDefault();
        const line = h('path', { fill: 'none', 'stroke-width': '2', 'stroke-dasharray': '5 4', class: 'stroke-brand-500 pointer-events-none' });
        svg.append(line);
        const move = (e2) => {
          const x = (e2.clientX - origin.left) / scale, y = (e2.clientY - origin.top) / scale;
          line.setAttribute('d', `M${n.x + GNODE_W} ${n.y + GNODE_H / 2} L${x} ${y}`);
        };
        const up = (e2) => {
          window.removeEventListener('pointermove', move);
          window.removeEventListener('pointerup', up);
          line.remove();
          const target = document.elementFromPoint(e2.clientX, e2.clientY)?.closest?.('[data-node]');
          if (target && target.dataset.node !== n.id) opts.onWire(n.id, target.dataset.node);
        };
        window.addEventListener('pointermove', move);
        window.addEventListener('pointerup', up);
        return;
      }
      const start = { x: ev.clientX, y: ev.clientY, nx: n.x, ny: n.y };
      let moved = false;
      grp.setPointerCapture(ev.pointerId);
      const move = (e2) => {
        if (!editable) return;
        const dx = (e2.clientX - start.x) / scale, dy = (e2.clientY - start.y) / scale;
        if (!moved && Math.hypot(dx, dy) < 4) return;
        moved = true;
        grp.setAttribute('transform', `translate(${Math.max(0, start.nx + dx)} ${Math.max(0, start.ny + dy)})`);
      };
      const up = (e2) => {
        grp.removeEventListener('pointermove', move);
        grp.removeEventListener('pointerup', up);
        if (moved) opts.onMove?.(n.id, start.nx + (e2.clientX - start.x) / scale, start.ny + (e2.clientY - start.y) / scale);
        else opts.onSelect?.(n.id === selected ? '' : n.id);
      };
      grp.addEventListener('pointermove', move);
      grp.addEventListener('pointerup', up);
    });
    grp.addEventListener('keydown', (e) => {
      const step = e.shiftKey ? 40 : 10, d = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] }[e.key];
      if (d && editable) { e.preventDefault(); opts.onMove?.(n.id, n.x + d[0], n.y + d[1]); }
      else if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); opts.onSelect?.(n.id === selected ? '' : n.id); }
    });
    svg.append(grp);
  }
  return h('div', { class: 'overflow-auto rounded-xl border border-slate-200 bg-slate-50 p-2 dark:border-slate-800 dark:bg-slate-950/40', id: 'graph-canvas' }, svg);
}

// ------------------------------------------------------------------ editor

/** Team › Workflows › a workflow › Edit on the canvas (or a new graph workflow when params.id is "new"). */
export async function graphEditorPage({ params }) {
  const admin = atLeast(session.role, 'admin');
  const el = h('div', {});
  mount(el, spinner());
  let wf = null, agents = [], tools = [];
  try {
    [agents, tools] = await Promise.all([get('/v1/ai/agents').then(r => r.agents), get('/v1/ai/tools').then(r => r.tools)]);
    if (params.id !== 'new') wf = await get('/v1/ai/workflows/' + encodeURIComponent(params.id));
  } catch (e) { mount(el, errorBox(e.message)); return el; }
  if (!admin) { mount(el, notice('info', 'Only administrators change workflows.')); return el; }
  const live = agents.filter(a => !a.disabled);
  const agentName = (id) => agents.find(a => a.id === id)?.name || id || 'pick an agent';
  const lookups = tools.filter(t => !t.needs && !NO_LOOKUP.has(t.name) && !t.name.startsWith('propose_')).map(t => t.name);
  const cur = wf && wf.versions.find(v => v.n === wf.current);
  let g = cur?.graph ? autoPlace(cur.graph) : cur ? stagesToGraph(cur) : emptyGraph();
  let selected = '', problem = '';
  const meta = { name: wf?.name || '', description: wf?.description || '', every: cur?.every_minutes || 0, runAs: cur?.run_as || 'developer', note: '', webhook: !!cur?.webhook, follow: (cur?.on_run_failed || []).join(', ') };
  const canvasBox = h('div', {}), panel = h('aside', { class: 'card card-pad space-y-3 self-start', id: 'graph-panel' }), bar = h('div', {});

  const change = (next, redrawPanel = true) => { g = next; problem = ''; draw(); if (redrawPanel) drawPanel(); };
  const select = (id) => { selected = id; draw(); drawPanel(); };
  const wireUp = (from, to) => {
    const label = nextLabel(g, from);
    const why = wireProblem(g, from, to, label);
    if (why) { toast(why, 'error'); return; }
    change(addWire(g, from, to, label));
  };

  function draw() {
    mount(canvasBox, graphCanvas(g, { selected, editable: true, agentName, onSelect: select, onMove: (id, x, y) => change(moveNode(g, id, x, y), false), onWire: wireUp }));
  }

  function drawBar() {
    const save = h('button', { class: 'btn-primary', id: 'graph-save', type: 'button', onclick: async () => {
      save.disabled = true;
      try {
        const body = { name: meta.name, description: meta.description, every_minutes: Number(meta.every), run_as: meta.runAs, note: meta.note, graph: g,
          webhook: meta.webhook, on_run_failed: meta.follow.split(',').map(x => x.trim()).filter(Boolean) };
        const out = await post(wf ? '/v1/ai/workflows/' + wf.id + '/versions' : '/v1/ai/workflows', body);
        toast(wf ? 'Saved v' + out.current : 'Workflow created', 'ok');
        navigate('/workflows/' + out.id);
      } catch (e) { problem = e.message; drawProblem(); save.disabled = false; }
    } }, icon('check', 'size-4'), wf ? 'Save as a new version' : 'Create workflow');
    const name = h('input', { id: 'graph-name', class: 'input', maxlength: '60', value: meta.name, placeholder: 'Incident triage', oninput: (e) => { meta.name = e.target.value; } });
    const desc = h('input', { id: 'graph-desc', class: 'input', maxlength: '300', value: meta.description, placeholder: 'What it is for', oninput: (e) => { meta.description = e.target.value; } });
    const every = h('select', { id: 'graph-every', class: 'input', onchange: (e) => { meta.every = e.target.value; } }, SCHEDULES.map(([v, l]) => h('option', { value: String(v), selected: Number(meta.every) === v }, l)));
    const runAs = h('select', { id: 'graph-runas', class: 'input', onchange: (e) => { meta.runAs = e.target.value; } }, [['developer', 'Developer: may file change proposals'], ['viewer', 'Viewer: reads only']].map(([v, l]) => h('option', { value: v, selected: meta.runAs === v }, l)));
    const hook = h('input', { type: 'checkbox', id: 'graph-webhook', checked: meta.webhook, onchange: (e) => { meta.webhook = e.target.checked; } });
    const follow = h('input', { id: 'graph-follow', class: 'input', value: meta.follow, placeholder: 'pipeline names, or * for any', oninput: (e) => { meta.follow = e.target.value; } });
    const note = h('input', { id: 'graph-note', class: 'input', maxlength: '200', placeholder: wf ? 'What changed and why' : 'First version', oninput: (e) => { meta.note = e.target.value; } });
    mount(bar, h('section', { class: 'card card-pad mb-4 space-y-3' },
      h('div', { class: 'grid gap-3 md:grid-cols-2 xl:grid-cols-4' }, field('Name', name, null, 'graph-name'), field('Description', desc, null, 'graph-desc'), field('Schedule', every, null, 'graph-every'), field('Runs as', runAs, null, 'graph-runas')),
      h('div', { class: 'grid gap-3 md:grid-cols-2' },
        h('label', { class: 'flex items-center gap-2 self-end pb-2 text-sm', for: 'graph-webhook' }, hook, 'Also starts from a signed webhook (the secret is made on the workflow\u2019s page)'),
        field('Also starts after failed runs of', follow, 'What failed reaches the graph as trigger, which agents can read as data.', 'graph-follow')),
      h('div', { class: 'flex flex-wrap items-end gap-3' }, h('div', { class: 'min-w-64 flex-1' }, field('Note', note, null, 'graph-note')), save),
      h('div', { id: 'graph-problem' })));
    drawProblem();
  }
  const drawProblem = () => { const box = bar.querySelector('#graph-problem'); if (box) mount(box, problem ? errorBox(problem) : null); };

  const input = (label, value, onChange, attrs = {}) => {
    const id = attrs.id || 'g-' + label.toLowerCase().replace(/[^a-z]+/g, '-');
    const x = h('input', { class: 'input', value: value ?? '', ...attrs, id });
    x.addEventListener('change', () => onChange(attrs.type === 'number' ? (x.value === '' ? '' : Number(x.value)) : x.value.trim()));
    return h('div', {}, h('label', { class: 'label', for: id }, label), x);
  };
  const select_ = (label, value, options, onChange, id) => {
    id = id || 'g-' + label.toLowerCase().replace(/[^a-z]+/g, '-');
    const x = h('select', { class: 'input', id }, options.map(([v, t]) => h('option', { value: String(v), selected: String(v) === String(value ?? '') }, t)));
    x.addEventListener('change', () => onChange(x.value));
    return h('div', {}, h('label', { class: 'label', for: id }, label), x);
  };
  const textarea = (label, value, onChange, id, rows = 3) => {
    const x = h('textarea', { class: 'input text-sm', rows: String(rows), id, maxlength: '2000' }, value || '');
    x.addEventListener('change', () => onChange(x.value.trim()));
    return h('div', {}, h('label', { class: 'label', for: id }, label), x);
  };
  const list = (s) => s.split(',').map(x => x.trim()).filter(Boolean);

  function agentFields(n, set) {
    return [
      select_('Agent', n.agent || '', [['', 'Pick an agent'], ...live.map(a => [a.id, a.name + ' (' + a.id + ')'])], v => set({ agent: v, agent_n: 0 }), 'g-agent'),
      n.agent_n ? h('p', { class: 'hint -mt-2' }, 'Pinned at v' + n.agent_n + '. Pick the agent again to move to its current version.') : h('p', { class: 'hint -mt-2' }, 'Pinned at its current version when saved.'),
      textarea(n.kind === 'decide' ? 'What to decide' : 'Task', n.task, v => set({ task: v }), 'g-task', 4),
      h('div', {}, h('div', { class: 'label' }, 'Reads (handed over as data)'), h('div', { class: 'flex flex-wrap gap-2', id: 'g-reads' }, stateKeys(g, n.id).map(k => h('label', { class: 'flex items-center gap-1.5 text-sm' },
        h('input', { type: 'checkbox', checked: (n.reads || []).includes(k), 'data-read': k, onchange: (e) => set({ reads: e.target.checked ? [...(n.reads || []), k] : (n.reads || []).filter(x => x !== k) }) }), h('code', { class: 'text-xs' }, k)))),
        !stateKeys(g, n.id).length ? h('p', { class: 'hint' }, 'Nothing to read yet.') : null),
    ];
  }

  // The palette: with a node selected, a new node goes to its right and is wired from it when that is possible.
  const palette = (after) => h('div', { class: 'grid grid-cols-2 gap-2', id: 'graph-palette' }, PALETTE.map(kind => h('button', { type: 'button', class: 'btn-secondary btn-sm justify-start', 'data-add': kind, title: NODE_KINDS[kind].note,
    onclick: () => {
      const r = addNode(g, kind, null, kind === 'end' ? { outcome: 'succeeded' } : kind === 'decide' ? { by: 'rule', rules: [] } : {}, after);
      let next = r.graph;
      const label = after ? nextLabel(next, after) : '';
      if (after && !wireProblem(next, after, r.id, label)) next = addWire(next, after, r.id, label);
      selected = r.id; change(next);
    } }, icon(NODE_KINDS[kind].icon, 'size-3.5'), NODE_KINDS[kind].label)));

  function drawPanel() {
    const n = g.nodes.find(x => x.id === selected);
    if (!n) {
      mount(panel, h('div', { class: 'flex items-center justify-between gap-2' }, h('h2', { class: 'h-section' }, 'Add a node'),
          h('button', { type: 'button', class: 'btn-ghost btn-sm', id: 'graph-tidy', title: 'Lay the nodes out again, left to right', onclick: () => change(tidy(g)) }, icon('grid', 'size-3.5'), 'Tidy up')),
        palette(''),
        h('p', { class: 'hint' }, 'Pick a node to set it. Drag a node to move it; drag from its blue port to another node to wire them. A decision’s wires carry the branch they are for.'),
        h('div', { class: 'muted space-y-1 text-xs' }, Object.values(NODE_KINDS).map(k => h('p', {}, h('strong', {}, k.label + ': '), k.note))));
      return;
    }
    const set = (fields) => change(updateNode(g, n.id, fields));
    const out = [h('div', { class: 'flex items-center justify-between gap-2' }, h('h2', { class: 'h-section' }, NODE_KINDS[n.kind].label + ' · ' + n.id),
      h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Close', onclick: () => select('') }, icon('x', 'size-4'))),
      input('Name', n.name, v => set({ name: v }), { maxlength: '60', placeholder: NODE_KINDS[n.kind].label, id: 'g-name' })];
    switch (n.kind) {
      case 'agent':
        out.push(...agentFields(n, set), input('Writes these fields too', (n.fields || []).join(', '), v => set({ fields: list(v) }), { id: 'g-fields', placeholder: 'severity, service' }),
          h('p', { class: 'hint -mt-2' }, 'The agent is asked for them at the end of its answer; a decision can test them.'));
        break;
      case 'lookup':
        out.push(select_('Lookup', n.tool || '', [['', 'Pick a lookup'], ...lookups.map(t => [t, t])], v => set({ tool: v }), 'g-tool'),
          textarea('Arguments (JSON)', n.args ? JSON.stringify(n.args) : '{}', v => { try { set({ args: v ? JSON.parse(v) : null }); } catch { toast('Not valid JSON', 'error'); } }, 'g-args', 2),
          n.tool ? h('p', { class: 'hint -mt-2', id: 'g-tool-help' }, 'It takes ' + (tools.find(t => t.name === n.tool)?.help || '{}') + '. Numbers without quotes.') : null,
          h('p', { class: 'hint' }, 'Reads one thing without a model. What it returns is saved under ' + n.id + '.'));
        break;
      case 'decide': {
        out.push(select_('Decide by', n.by || 'rule', [['rule', 'A rule on what earlier nodes wrote'], ['agent', 'An agent choosing a branch']], v => set({ by: v, ...(v === 'rule' ? { agent: null, task: null, reads: null } : { rules: null }) }), 'g-by'));
        if ((n.by || 'rule') === 'rule') {
          const rules = n.rules || [];
          const keys = stateKeys(g, n.id);
          out.push(h('div', { class: 'space-y-2', id: 'g-rules' }, rules.map((r, i) => {
            const upd = (f) => set({ rules: rules.map((x, j) => j === i ? { ...x, ...f } : x) });
            return h('div', { class: 'grid grid-cols-2 gap-1.5 rounded-lg border border-slate-200 p-2 dark:border-slate-800', 'data-rule': String(i) },
              h('select', { class: 'input', 'aria-label': 'Key', onchange: (e) => upd({ key: e.target.value }) }, [['', 'value of…'], ...keys.map(k => [k, k])].map(([v, t]) => h('option', { value: v, selected: v === (r.key || '') }, t))),
              h('select', { class: 'input', 'aria-label': 'Test', onchange: (e) => upd({ op: e.target.value }) }, OPS.map(([v, t]) => h('option', { value: v, selected: v === (r.op || 'equals') }, t))),
              h('input', { class: 'input', 'aria-label': 'Value', value: r.value || '', placeholder: 'high', onchange: (e) => upd({ value: e.target.value.trim() }) }),
              h('input', { class: 'input', 'aria-label': 'Branch', value: r.when || '', placeholder: 'branch, e.g. page', onchange: (e) => upd({ when: e.target.value.trim().toLowerCase() }) }),
              h('button', { class: 'btn-ghost btn-sm col-span-2 justify-self-end', type: 'button', onclick: () => set({ rules: rules.filter((_, j) => j !== i) }) }, icon('trash', 'size-3.5'), 'Remove'));
          }), h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'g-add-rule', onclick: () => set({ rules: [...rules, { key: keys[0] || '', op: 'equals', value: '', when: 'branch-' + (rules.length + 1) }] }) }, icon('plus', 'size-3.5'), 'Add a rule')),
          h('p', { class: 'hint' }, 'The first rule that matches picks its branch; otherwise the else wire.'));
        } else {
          out.push(...agentFields(n, set), h('p', { class: 'hint' }, 'The agent must answer one of the wires’ branches; an unclear answer is asked once more, then else is taken.'));
        }
        break;
      }
      case 'ask':
        out.push(textarea('Question', n.question, v => set({ question: v }), 'g-question', 2),
          input('Choices', (n.choices || []).join(', '), v => set({ choices: list(v).map(x => x.toLowerCase()) }), { id: 'g-choices', placeholder: 'approve, reject (empty: a free answer)' }),
          input('Who may answer', (n.answerers || []).join(', '), v => set({ answerers: list(v) }), { id: 'g-answerers', placeholder: 'empty: any administrator' }),
          h('p', { class: 'hint' }, 'The run waits here (up to 7 days). The answer is saved under ' + n.id + ' and, with choices, picks the wire.'));
        break;
      case 'end':
        out.push(select_('Ends the run as', n.outcome || 'succeeded', [['succeeded', 'Done'], ['failed', 'Failed']], v => set({ outcome: v }), 'g-outcome'),
          input('Summary', n.summary, v => set({ summary: v }), { id: 'g-summary', maxlength: '500' }));
        break;
    }
    if (n.kind !== 'start' && n.kind !== 'end') out.push(input('Most times it may run', n.max_visits || '', v => set({ max_visits: v || null }), { type: 'number', min: '1', max: '10', placeholder: '3', id: 'g-max' }));
    // Its wires, and one more.
    const mine = g.edges.filter(e => e.from === n.id);
    const others = g.nodes.filter(x => x.id !== n.id && x.kind !== 'start');
    const to = h('select', { class: 'input', id: 'g-wire-to', 'aria-label': 'Wire to' }, others.map(x => h('option', { value: x.id }, (x.name || NODE_KINDS[x.kind].label) + ' (' + x.id + ')')));
    const label = branches(n) ? h('select', { class: 'input w-auto', id: 'g-wire-when', 'aria-label': 'Branch' }, branchLabels(n).map(l => h('option', { value: l }, l))) : null;
    out.push(h('div', { class: 'border-t border-slate-200 pt-3 dark:border-slate-800' }, h('h3', { class: 'h-section mb-2' }, 'Wires out'),
      mine.length ? h('ul', { class: 'space-y-1 text-sm', id: 'g-wires' }, mine.map(e => h('li', { class: 'flex items-center justify-between gap-2' },
        h('span', {}, e.when ? h('span', { class: 'mr-1 font-medium text-violet-700 dark:text-violet-300' }, e.when + ' →') : '→ ', g.nodes.find(x => x.id === e.to)?.name || e.to),
        h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Remove the wire to ' + e.to, onclick: () => change(removeWire(g, e.from, e.to, e.when || '')) }, icon('x', 'size-3.5'))))) : h('p', { class: 'muted text-xs' }, 'None yet.'),
      n.kind !== 'end' && others.length ? h('div', { class: 'mt-2 flex flex-wrap gap-2' }, label, to, h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'g-wire-add', onclick: () => {
        const why = wireProblem(g, n.id, to.value, label ? label.value : '');
        if (why) return toast(why, 'error');
        change(addWire(g, n.id, to.value, label ? label.value : ''));
      } }, icon('link', 'size-3.5'), 'Wire')) : null));
    if (n.kind !== 'end') out.push(h('div', { class: 'border-t border-slate-200 pt-3 dark:border-slate-800' }, h('h3', { class: 'h-section mb-2' }, 'Add a node after this one'), palette(n.id)));
    if (n.kind !== 'start') out.push(h('div', { class: 'flex justify-end' }, h('button', { class: 'btn-ghost btn-sm text-rose-600 dark:text-rose-400', type: 'button', id: 'g-remove', onclick: () => { selected = ''; change(removeNode(g, n.id)); } }, icon('trash', 'size-3.5'), 'Remove node')));
    mount(panel, ...out);
  }

  mount(el, pageHeader(wf ? 'Edit ' + wf.name : 'New workflow', 'Nodes share one state: each writes what it found under its id. Decisions take one wire; loops are allowed, each node runs at most its limit, and a run at most 50 steps. Only an agent’s own tools may propose changes, and a different administrator approves them.',
    h('a', { class: 'btn-secondary btn-sm', href: wf ? '#/workflows/' + wf.id : '#/agents?tab=workflows' }, 'Back')),
    !live.length ? h('div', { class: 'mb-4' }, notice('info', 'Create an agent first: agent and decision nodes need one.')) : null,
    cur && !cur.graph ? h('div', { class: 'mb-4' }, notice('info', 'This workflow had stages. They are drawn here as a graph; saving makes a new version as a graph.')) : null,
    bar, h('div', { class: 'grid gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]' }, canvasBox, panel));
  drawBar(); draw(); drawPanel();
  return el;
}

// ------------------------------------------------------------------ a graph run

/** The run page's graph part: the canvas with statuses, a waiting question, and each visit of the selected node. */
export function graphRunView(r, g, agents, reload) {
  const agentName = (id) => agents.find(a => a.id === id)?.name || id;
  const box = h('div', { class: 'space-y-4', id: 'graph-run' });
  let selected = r.waiting || '';
  const statuses = graphRunStatus(g, r);
  function draw() {
    const waitingNode = r.waiting && g.nodes.find(n => n.id === r.waiting);
    const visits = (r.visits || []).filter(v => !selected || v.node === selected);
    mount(box,
      waitingNode ? askCard(r, waitingNode, reload) : null,
      graphCanvas(g, { statuses, selected, agentName, onSelect: (id) => { selected = id; draw(); } }),
      h('section', { class: 'card card-pad' },
        h('div', { class: 'mb-2 flex flex-wrap items-center justify-between gap-2' }, h('h2', { class: 'h-section' }, selected ? 'Node ' + selected : 'Every step, in order'),
          h('div', { class: 'flex items-center gap-3 text-xs muted' }, h('span', {}, (r.steps || 0) + ' of 50 steps'), selected ? h('button', { class: 'btn-ghost btn-sm', type: 'button', onclick: () => { selected = ''; draw(); } }, 'Show all') : null)),
        visits.length ? h('ol', { class: 'space-y-2', id: 'graph-visits' }, visits.map(v => h('li', { class: 'rounded-md bg-slate-50 p-2.5 text-sm dark:bg-slate-900', 'data-visit': v.node + '#' + v.visit, 'data-status': v.status },
          h('div', { class: 'mb-1 flex flex-wrap items-center justify-between gap-2' },
            h('span', { class: 'font-medium' }, (g.nodes.find(n => n.id === v.node)?.name || v.node) + (v.visit > 1 ? ' (time ' + v.visit + ')' : ''), h('span', { class: 'muted ml-2 text-xs' }, NODE_KINDS[v.kind]?.label || v.kind)),
            h('span', { class: 'flex items-center gap-2' }, v.branch ? h('span', { class: 'pill-idle' }, '→ ' + v.branch) : null, pill(v.status))),
          v.agent_label ? h('p', { class: 'muted text-xs' }, 'Agent ' + v.agent_label) : null,
          v.answered_by ? h('p', { class: 'muted text-xs' }, 'Answered by ' + v.answered_by) : null,
          v.error ? h('p', { class: 'text-rose-700 dark:text-rose-400' }, v.error) : null,
          v.input ? h('details', {}, h('summary', { class: 'cursor-pointer text-xs' }, 'What went in'), h('pre', { class: 'mt-1 max-h-64 overflow-auto whitespace-pre-wrap text-xs' }, v.input)) : null,
          v.output ? h('pre', { class: 'mt-1 max-h-72 overflow-auto whitespace-pre-wrap text-sm', 'data-output': '' }, v.output) : null,
          v.lookups?.length ? h('p', { class: 'muted mt-1 text-xs' }, 'Looked up: ' + v.lookups.join(', ')) : null)))
          : h('p', { class: 'muted text-sm' }, 'Nothing has run here yet.')),
      Object.keys(r.state || {}).length ? h('details', { class: 'card card-pad' }, h('summary', { class: 'cursor-pointer h-section' }, 'The state'),
        h('dl', { class: 'mt-2 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm', id: 'graph-state' }, Object.entries(r.state).flatMap(([k, v]) => [h('dt', {}, h('code', { class: 'text-xs' }, k)), h('dd', { class: 'whitespace-pre-wrap break-words' }, v.length > 300 ? v.slice(0, 300) + '…' : v)]))) : null);
  }
  draw();
  return box;
}

function askCard(r, n, reload) {
  const admin = atLeast(session.role, 'admin');
  const allowed = (n.answerers || []).length ? n.answerers.includes(session.name) : admin;
  const who = (n.answerers || []).length ? n.answerers.join(', ') : 'an administrator';
  const send = async (answer) => {
    try { await post('/v1/ai/workflow-runs/' + r.id + '/answer', { answer }); toast('Answered', 'ok'); reload(); } catch (e) { toast(e.message, 'error'); }
  };
  let body;
  if (!allowed) body = h('p', { class: 'text-sm' }, 'Waiting for ' + who + ' to answer.');
  else if ((n.choices || []).length) body = h('div', { class: 'flex flex-wrap gap-2', id: 'answer-choices' }, n.choices.map(c => h('button', { class: 'btn-primary btn-sm', type: 'button', 'data-answer': c, onclick: () => send(c) }, c)));
  else {
    const ta = h('textarea', { class: 'input text-sm', rows: '2', maxlength: '1000', id: 'answer-text' });
    body = h('div', { class: 'space-y-2' }, ta, h('button', { class: 'btn-primary btn-sm', type: 'button', id: 'answer-send', onclick: () => send(ta.value) }, 'Answer'));
  }
  return h('div', { id: 'graph-ask' }, notice('warn', h('div', { class: 'space-y-2' }, h('strong', {}, n.name || 'A question'), h('p', { class: 'whitespace-pre-wrap' }, n.question), body)));
}

