// Forgeline web console: agent graphs (design note 044) on a canvas, like n8n. Pure functions, no DOM, so they run
// under Node in tests. Every edit returns a new graph and leaves the one it was given alone. The server checks every
// save (internal/server/console_ai_graph.go); these helpers keep the canvas honest while people edit.

import { describe } from './schedule.js';

export const GNODE_W = 200, GNODE_H = 64;
const COL_W = 260, ROW_H = 96, PAD = 24;

// What each node kind is, in the words and icon the canvas uses.
export const NODE_KINDS = {
  start: { label: 'Start', icon: 'play', note: 'By hand or on the workflow’s schedule' },
  agent: { label: 'Agent', icon: 'sparkle', note: 'An agent does a task and writes what it found' },
  lookup: { label: 'Lookup', icon: 'search', note: 'Reads one thing, no model' },
  decide: { label: 'Decide', icon: 'flow', note: 'Takes one wire by a rule or by an agent’s choice' },
  ask: { label: 'Ask a person', icon: 'users', note: 'Waits for an answer or an approval' },
  merge: { label: 'Merge', icon: 'layers', note: 'Waits for the branches that reach it' },
  end: { label: 'End', icon: 'check', note: 'Finishes the run' },
};

const clone = (x) => JSON.parse(JSON.stringify(x));

/** Does a node take a branch label on its outgoing wires? */
export const branches = (n) => !!n && (n.kind === 'decide' || (n.kind === 'ask' && (n.choices || []).length > 0));

/** The labels a node's wires may carry: a decision's rule branches and else, or a question's choices. */
export function branchLabels(n) {
  if (!n) return [];
  if (n.kind === 'ask') return [...(n.choices || [])];
  if (n.kind !== 'decide') return [];
  const out = n.by === 'rule' ? [...new Set((n.rules || []).map(r => r.when).filter(Boolean))] : [];
  if (!out.includes('else')) out.push('else');
  return out;
}

/** A new id for a node of a kind: agent, agent-2, ... */
export function newNodeID(g, kind) {
  const taken = new Set(g.nodes.map(n => n.id));
  if (!taken.has(kind)) return kind;
  for (let i = 2; ; i++) if (!taken.has(kind + '-' + i)) return kind + '-' + i;
}

/** An empty graph: a start and an end, wired. */
export function emptyGraph() {
  return { nodes: [{ id: 'start', kind: 'start', x: PAD, y: PAD + ROW_H }, { id: 'end', kind: 'end', x: PAD + COL_W * 2, y: PAD + ROW_H, outcome: 'succeeded' }], edges: [{ from: 'start', to: 'end' }] };
}

export function addNode(g, kind, at = null, fields = {}, after = '') {
  const out = clone(g);
  const id = newNodeID(out, kind);
  // Next to the node it follows: one column to the right, in the first free row.
  const prev = after && out.nodes.find(n => n.id === after);
  if (prev && !at) {
    let y = prev.y;
    while (out.nodes.some(n => Math.abs(n.x - (prev.x + COL_W)) < GNODE_W && Math.abs(n.y - y) < GNODE_H)) y += ROW_H;
    at = { x: prev.x + COL_W, y };
  }
  // In view: the second column, below what is there, so it can be wired without scrolling sideways.
  const below = out.nodes.reduce((m, n) => Math.max(m, (n.y || 0) + ROW_H), PAD);
  const pos = at || { x: PAD + COL_W, y: below };
  out.nodes.push({ id, kind, x: Math.max(0, Math.round(pos.x)), y: Math.max(0, Math.round(pos.y)), ...fields });
  return { graph: out, id };
}

export function updateNode(g, id, fields) {
  const out = clone(g);
  const n = out.nodes.find(x => x.id === id);
  if (!n) return out;
  for (const [k, v] of Object.entries(fields)) {
    if (v === '' || v === null || v === undefined || (Array.isArray(v) && !v.length)) delete n[k]; else n[k] = v;
  }
  // A node that stops branching keeps its wires, without labels; one that starts branching gets else on unlabelled ones.
  for (const e of out.edges.filter(e => e.from === id)) {
    if (!branches(n)) delete e.when;
    else if (!e.when) e.when = branchLabels(n).find(l => !out.edges.some(x => x.from === id && x.when === l)) || 'else';
  }
  return out;
}

export function moveNode(g, id, x, y) {
  return updateNode(g, id, { x: Math.max(0, Math.round(x)), y: Math.max(0, Math.round(y)) });
}

/** Removes a node and its wires. The start cannot be removed. */
export function removeNode(g, id) {
  const out = clone(g);
  if (out.nodes.find(n => n.id === id)?.kind === 'start') return out;
  out.nodes = out.nodes.filter(n => n.id !== id);
  out.edges = out.edges.filter(e => e.from !== id && e.to !== id);
  for (const n of out.nodes) {
    if (n.reads) n.reads = n.reads.filter(k => k !== id);
    if (n.reads && !n.reads.length) delete n.reads;
  }
  return out;
}

/** Why a wire cannot be drawn, or '' when it can. */
export function wireProblem(g, from, to, when = '') {
  const a = g.nodes.find(n => n.id === from), b = g.nodes.find(n => n.id === to);
  if (!a || !b) return 'pick two nodes';
  if (b.kind === 'start') return 'nothing wires into the start';
  if (a.kind === 'end') return 'nothing wires out of an end';
  if (branches(a) && !when) return 'say which branch this wire is for';
  if (!branches(a) && when) return 'only a decision or a question with choices has branches';
  if (g.edges.some(e => e.from === from && e.to === to && (e.when || '') === when)) return 'already wired';
  return '';
}

export function addWire(g, from, to, when = '') {
  if (wireProblem(g, from, to, when)) return g;
  const out = clone(g);
  out.edges.push(when ? { from, to, when } : { from, to });
  return out;
}

export function removeWire(g, from, to, when = '') {
  const out = clone(g);
  out.edges = out.edges.filter(e => !(e.from === from && e.to === to && (e.when || '') === when));
  return out;
}

/** The first label of a branching node that no wire takes yet, for the next wire drawn from it. */
export function nextLabel(g, id) {
  const n = g.nodes.find(x => x.id === id);
  return branchLabels(n).find(l => !g.edges.some(e => e.from === id && e.when === l)) || '';
}

/** State keys a node may read: other nodes' ids and the fields agents declare. */
export function stateKeys(g, except = '') {
  const keys = new Set();
  for (const n of g.nodes) {
    if (n.id === except) continue;
    if (['agent', 'lookup', 'decide', 'ask'].includes(n.kind)) keys.add(n.id);
    for (const f of n.fields || []) keys.add(f);
  }
  return [...keys].sort();
}

/** What a node can be handed from the others, labelled for people: a node's own answer by its name, an extra field
 *  with the node that writes it. Same keys as stateKeys. */
export function readOptions(g, except = '') {
  const name = (n) => n.name || NODE_KINDS[n.kind].label;
  const out = [];
  for (const k of stateKeys(g, except)) {
    const n = g.nodes.find(x => x.id === k);
    if (n) { out.push({ key: k, label: name(n), sub: 'its answer' }); continue; }
    const by = g.nodes.filter(x => x.id !== except && (x.fields || []).includes(k)).map(name);
    out.push({ key: k, label: k, sub: 'a field from ' + (by.join(', ') || 'an earlier node') });
  }
  return out;
}

/** Places nodes that have no position: columns by distance from the start, as n8n lays out an import. */
export function autoPlace(g) {
  if (g.nodes.every(n => n.x || n.y)) return clone(g);
  return tidy(g);
}

/** Lays every node out again: a column per distance from the start, nodes of a column one under the other. */
export function tidy(g) {
  const out = clone(g);
  // Breadth first from the start: a node's column is how many wires it is from the start.
  const start = out.nodes.find(n => n.kind === 'start');
  const depth = {};
  const queue = start ? [start.id] : [];
  if (start) depth[start.id] = 0;
  while (queue.length) {
    const id = queue.shift();
    for (const e of out.edges) if (e.from === id && depth[e.to] === undefined) { depth[e.to] = depth[id] + 1; queue.push(e.to); }
  }
  const rows = {};
  for (const n of out.nodes) {
    const c = depth[n.id] ?? 0;
    rows[c] = (rows[c] || 0) + 1;
    n.x = PAD + c * COL_W;
    n.y = PAD + (rows[c] - 1) * ROW_H;
  }
  return out;
}

/** A wire's path from the right port of one node to the left port of another, looping round when it goes back. */
export function wirePath(a, b) {
  const x1 = a.x + GNODE_W, y1 = a.y + GNODE_H / 2, x2 = b.x, y2 = b.y + GNODE_H / 2;
  if (x2 >= x1 + 20) {
    const mid = (x1 + x2) / 2;
    return `M${x1} ${y1} C${mid} ${y1}, ${mid} ${y2}, ${x2} ${y2}`;
  }
  // Back or sideways (a loop): out to the right, under both, in from the left.
  const low = Math.max(a.y, b.y) + GNODE_H + 28;
  return `M${x1} ${y1} C${x1 + 60} ${y1}, ${x1 + 60} ${low}, ${x1} ${low} L${x2} ${low} C${x2 - 60} ${low}, ${x2 - 60} ${y2}, ${x2} ${y2}`;
}

/** Where a wire's label goes: near its start, so labels of a decision fan out. */
export function labelPoint(a, b) {
  return { x: a.x + GNODE_W + 14, y: (a.y + GNODE_H / 2) * 0.6 + (b.y + GNODE_H / 2) * 0.4 - 4 };
}

/** A node's one line of what it does. */
export function nodeSummary(n, agentName = (id) => id) {
  switch (n.kind) {
    case 'start': return 'by hand or on schedule';
    case 'agent': return n.name && n.name === agentName(n.agent) ? n.task || 'say what to do' : agentName(n.agent) + (n.task ? ': ' + n.task : '');
    case 'lookup': return n.tool || 'pick a lookup';
    case 'decide': return n.by === 'agent' ? agentName(n.agent) + ' chooses' : (n.rules || []).map(r => `${r.key} ${r.op} ${r.value || ''}`.trim()).join('; ') || 'add a rule';
    case 'ask': return n.question || 'ask a question';
    case 'merge': return 'waits for its branches';
    case 'end': return n.outcome === 'failed' ? 'ends as failed' : 'ends as done';
  }
  return '';
}

/**
 * Each node's status in a run, from its visits: the last visit's status, how many times it ran, and the wires taken
 * (a wire is taken when its source finished and either has no branch or took this wire's branch).
 */
export function graphRunStatus(g, run) {
  const nodes = {};
  for (const n of g.nodes) nodes[n.id] = { status: 'pending', visits: 0 };
  for (const v of run.visits || []) {
    const s = nodes[v.node] || (nodes[v.node] = { status: 'pending', visits: 0 });
    s.status = v.status;
    if (v.status !== 'running' && v.status !== 'waiting') s.visits++;
    s.last = v;
  }
  if (run.status === 'cancelled') for (const s of Object.values(nodes)) if (s.status === 'running' || s.status === 'waiting') s.status = 'cancelled';
  const taken = new Set();
  for (const e of g.edges) {
    const s = nodes[e.from];
    if (!s || !s.last || s.last.status !== 'succeeded') continue;
    if (e.when ? s.last.branch === e.when : true) taken.add(e.from + '>' + e.to + '>' + (e.when || ''));
  }
  return { nodes, taken };
}

/** A stages workflow as a graph: a column per stage, a question before a gated stage, a merge after parallel steps. */
export function stagesToGraph(v) {
  const g = { nodes: [{ id: 'start', kind: 'start' }], edges: [] };
  let prev = ['start'];
  (v.stages || []).forEach((st, i) => {
    if (st.gate) {
      const gate = { id: `release-${i + 1}`, kind: 'ask', question: 'Release ' + (st.name || 'stage ' + (i + 1)) + '?', choices: ['release', 'stop'] };
      g.nodes.push(gate);
      for (const p of prev) g.edges.push({ from: p, to: gate.id });
      g.nodes.push({ id: `stopped-${i + 1}`, kind: 'end', outcome: 'failed', summary: 'Not released' });
      g.edges.push({ from: gate.id, to: `stopped-${i + 1}`, when: 'stop' });
      prev = [gate.id];
    }
    const ids = (st.steps || []).map((sp, j) => {
      const id = `s${i + 1}-${j + 1}`;
      const reads = i > 0 ? g.nodes.filter(n => n.kind === 'agent').map(n => n.id) : [];
      g.nodes.push({ id, kind: 'agent', agent: sp.agent, agent_n: sp.agent_n || 0, task: sp.task, ...(reads.length ? { reads } : {}) });
      for (const p of prev) g.edges.push(p.startsWith('release-') ? { from: p, to: id, when: 'release' } : { from: p, to: id });
      return id;
    });
    if (ids.length > 1) {
      const m = { id: `merge-${i + 1}`, kind: 'merge' };
      g.nodes.push(m);
      for (const id of ids) g.edges.push({ from: id, to: m.id });
      prev = [m.id];
    } else prev = ids;
  });
  g.nodes.push({ id: 'end', kind: 'end', outcome: 'succeeded' });
  for (const p of prev) g.edges.push(p.startsWith('release-') ? { from: p, to: 'end', when: 'release' } : { from: p, to: 'end' });
  return autoPlace(g);
}

// ------------------------------------------------------------------ viewport: pan and zoom, like n8n

export const MIN_ZOOM = 0.25, MAX_ZOOM = 2;
const clampZoom = (k) => Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, k));

/** A view that shows every node in a box of bw×bh, centred, never zoomed in past 100%. */
export function fitView(g, bw, bh, pad = 40) {
  if (!g.nodes.length || bw <= 0 || bh <= 0) return { x: 0, y: 0, k: 1 };
  const x0 = Math.min(...g.nodes.map(n => n.x || 0)), y0 = Math.min(...g.nodes.map(n => n.y || 0));
  const x1 = Math.max(...g.nodes.map(n => (n.x || 0) + GNODE_W)), y1 = Math.max(...g.nodes.map(n => (n.y || 0) + GNODE_H + 40));
  const k = clampZoom(Math.min(1, (bw - pad * 2) / (x1 - x0), (bh - pad * 2) / (y1 - y0)));
  return { x: Math.round((bw - (x1 - x0) * k) / 2 - x0 * k), y: Math.round((bh - (y1 - y0) * k) / 2 - y0 * k), k: Math.round(k * 1000) / 1000 };
}

/** Zoom by a factor and keep the point (px, py) of the box where it is on screen. */
export function zoomAt(view, factor, px, py) {
  const k = clampZoom(view.k * factor);
  if (k === view.k) return { ...view };
  const wx = (px - view.x) / view.k, wy = (py - view.y) / view.k;
  return { x: px - wx * k, y: py - wy * k, k };
}

/** A point of the box (from its top left corner) on the graph. */
export const toWorld = (view, sx, sy) => ({ x: (sx - view.x) / view.k, y: (sy - view.y) / view.k });

// ------------------------------------------------------------------ the node picker

/**
 * What the node picker offers, in groups, filtered by a query on the label and the note: each agent by name, the
 * logic nodes, and each lookup by name. Every item says which kind it adds and with which fields.
 */
export function pickerItems(agents, lookups, query = '') {
  const q = query.trim().toLowerCase();
  const groups = [
    { id: 'agents', label: 'Agents', items: agents.map(a => ({ key: 'agent:' + a.id, kind: 'agent', label: a.name, note: a.description || 'Does a task and writes what it found', fields: { agent: a.id, name: a.name } })) },
    { id: 'logic', label: 'Logic', items: [
      { key: 'decide:rule', kind: 'decide', label: 'Decide by a rule', note: 'Takes one wire by what earlier nodes wrote', fields: { by: 'rule', rules: [] } },
      { key: 'decide:agent', kind: 'decide', label: 'Decide by an agent', note: 'An agent picks one of the wires', fields: { by: 'agent' } },
      { key: 'merge', kind: 'merge', label: 'Merge', note: NODE_KINDS.merge.note, fields: {} },
      { key: 'ask', kind: 'ask', label: 'Ask a person', note: NODE_KINDS.ask.note, fields: {} },
      { key: 'end:succeeded', kind: 'end', label: 'End as done', note: 'Finishes the run', fields: { outcome: 'succeeded' } },
      { key: 'end:failed', kind: 'end', label: 'End as failed', note: 'Finishes the run as failed', fields: { outcome: 'failed' } },
    ] },
    { id: 'lookups', label: 'Lookups (no model)', items: lookups.map(t => ({ key: 'lookup:' + t, kind: 'lookup', label: t.replaceAll('_', ' '), note: 'Reads one thing', fields: { tool: t } })) },
  ];
  return groups.map(gr => ({ ...gr, items: gr.items.filter(i => !q || (i.label + ' ' + i.note + ' ' + i.kind).toLowerCase().includes(q)) })).filter(gr => gr.items.length);
}

/**
 * Adds what the picker chose after a node (or on its own when after is empty), wired from it when that is allowed.
 * When that node had one plain wire out, the new node goes in between, as n8n inserts: the nodes from there on move
 * one column right. An end goes beside instead, since nothing wires out of it. Returns { graph, id }.
 */
export function addPicked(g, item, after = '') {
  const prev = after && g.nodes.find(n => n.id === after);
  const outs = prev ? g.edges.filter(e => e.from === after) : [];
  const insert = prev && item.kind !== 'end' && !branches(prev) && outs.length === 1 && !outs[0].when;
  let base = g;
  if (insert) {
    base = clone(g);
    base.edges = base.edges.filter(e => e !== base.edges.find(x => x.from === after && x.to === outs[0].to && !x.when));
    for (const n of base.nodes) if (n.x > prev.x && Math.abs(n.y - prev.y) < ROW_H) n.x += COL_W;
  }
  const r = addNode(base, item.kind, insert ? { x: prev.x + COL_W, y: prev.y } : null, clone(item.fields), after);
  let next = r.graph;
  const label = after ? nextLabel(next, after) : '';
  if (after && !wireProblem(next, after, r.id, label)) next = addWire(next, after, r.id, label);
  if (insert) next = addWire(next, r.id, outs[0].to, nextLabel(next, r.id));
  return { graph: next, id: r.id };
}

/** The start node's line: what starts the workflow, from its settings. */
export function triggerSummary(meta) {
  const parts = ['by hand'];
  const every = Number(meta.every || 0);
  if (meta.cron) parts.push(describe({ cron: meta.cron, tz: meta.tz }));
  else if (every) parts.push(every < 60 ? 'every ' + every + ' min' : every < 1440 ? 'every ' + every / 60 + ' h' : every === 1440 ? 'daily' : 'weekly');
  if (meta.webhook) parts.push('webhook');
  if (String(meta.follow || '').trim()) parts.push('failed runs');
  if (String(meta.alarms || '').trim()) parts.push('alarms');
  return parts.join(' · ');
}
