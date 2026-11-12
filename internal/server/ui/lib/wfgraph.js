// A workflow drawn as a graph, left to right like n8n: what starts it, then each stage as a column of the steps that
// run together, with an approval node before a stage that waits for an administrator. Pure functions (no DOM), so
// the layout is tested under Node. The page draws the nodes and edges this returns.

export const NODE_W = 196, NODE_H = 64;
const GAP_X = 64, GAP_Y = 16, PAD = 16;

/**
 * Lays out a workflow version: { stages: [{ name, gate, steps: [{ agent, agent_n, task }] }], every_minutes }.
 * agentName(id) gives a step's label. Returns { nodes, edges, w, h }; each node has id, kind (trigger, gate or step),
 * x, y, label, sub, and for steps stage and step indexes.
 */
export function layoutWorkflow(v, agentName = (id) => id) {
  const columns = [];
  const every = v.every_minutes || 0;
  columns.push([{ id: 'trigger', kind: 'trigger', label: every ? 'On a schedule' : 'Started by hand', sub: every ? scheduleWords(every) : 'by someone with access' }]);
  (v.stages || []).forEach((st, i) => {
    if (st.gate) columns.push([{ id: `gate-${i}`, kind: 'gate', stage: i, label: 'Approval', sub: 'an administrator releases ' + st.name }]);
    columns.push((st.steps || []).map((sp, j) => ({
      id: `step-${i}-${j}`, kind: 'step', stage: i, step: j, stageName: st.name,
      label: agentName(sp.agent), sub: sp.task || '', agent: sp.agent, version: sp.agent_n || 0,
    })));
  });
  const tallest = Math.max(...columns.map(c => c.length));
  const colH = (n) => n * NODE_H + (n - 1) * GAP_Y;
  const nodes = [];
  columns.forEach((col, ci) => {
    const top = PAD + (colH(tallest) - colH(col.length)) / 2;
    col.forEach((n, ri) => nodes.push({ ...n, col: ci, x: PAD + ci * (NODE_W + GAP_X), y: top + ri * (NODE_H + GAP_Y) }));
  });
  const edges = [];
  for (let ci = 0; ci + 1 < columns.length; ci++) {
    for (const a of nodes.filter(n => n.col === ci)) for (const b of nodes.filter(n => n.col === ci + 1)) edges.push({ from: a.id, to: b.id });
  }
  return { nodes, edges, w: PAD * 2 + columns.length * NODE_W + (columns.length - 1) * GAP_X, h: PAD * 2 + colH(tallest) };
}

/** The drawn path of an edge: right middle of one node to left middle of the next. */
export function edgePath(a, b) {
  const x1 = a.x + NODE_W, y1 = a.y + NODE_H / 2, x2 = b.x, y2 = b.y + NODE_H / 2, mid = (x1 + x2) / 2;
  return `M${x1} ${y1} C${mid} ${y1}, ${mid} ${y2}, ${x2} ${y2}`;
}

/**
 * Each node's status in a run: steps take their own; an approval node is waiting while its stage waits, done once
 * released, pending before; the trigger is done. Unknown stays pending.
 */
export function runStatuses(layout, run) {
  const out = {};
  for (const n of layout.nodes) {
    if (n.kind === 'trigger') { out[n.id] = 'succeeded'; continue; }
    const st = (run.stages || [])[n.stage];
    if (!st) { out[n.id] = 'pending'; continue; }
    if (n.kind === 'gate') {
      out[n.id] = st.status === 'waiting' ? 'waiting' : st.gate_by ? 'succeeded' : (st.status === 'skipped' ? 'skipped' : 'pending');
      continue;
    }
    out[n.id] = ((st.steps || [])[n.step] || {}).status || 'pending';
  }
  return out;
}

/** An edge is live when the node it leaves is done and the one it enters is running or waiting. */
export function edgeLive(statuses, e) {
  return statuses[e.from] === 'succeeded' && (statuses[e.to] === 'running' || statuses[e.to] === 'waiting');
}

function scheduleWords(m) {
  return ({ 15: 'every 15 minutes', 60: 'every hour', 360: 'every 6 hours', 1440: 'every day', 10080: 'every week' })[m] || `every ${m} minutes`;
}
