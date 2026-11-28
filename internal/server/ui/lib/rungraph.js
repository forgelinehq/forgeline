// A workflow run drawn small enough for the assistant's panel: top to bottom, each node below everything that leads to
// it, nodes of one level side by side. Pure functions (no DOM), so the layout is tested under Node.

export const NODE_W = 128, NODE_H = 36;
const GAP_X = 12, GAP_Y = 26, PAD = 8, LOOP = 14, LOOP_LABEL = 56;

/**
 * Each node's level: the longest way to it from where the run begins, not counting wires that lead back (loops).
 */
function levels(nodes, edges) {
  const out = new Map(nodes.map(n => [n.id, []]));
  for (const e of edges) if (out.has(e.from) && out.has(e.to)) out.get(e.from).push(e.to);
  const seen = new Set(), open = new Set(), order = [], back = new Set();
  const visit = (id) => {
    seen.add(id); open.add(id);
    for (const to of out.get(id)) {
      if (open.has(to)) back.add(id + '>' + to);
      else if (!seen.has(to)) visit(to);
    }
    open.delete(id); order.unshift(id);
  };
  const first = nodes.find(n => n.kind === 'start');
  if (first) visit(first.id);
  for (const n of nodes) if (!seen.has(n.id)) visit(n.id);
  const level = new Map(nodes.map(n => [n.id, 0]));
  for (const id of order) for (const to of out.get(id)) {
    if (!back.has(id + '>' + to)) level.set(to, Math.max(level.get(to), level.get(id) + 1));
  }
  return level;
}

/** Places a run's graph: { nodes with x and y, edges with their path d, a label point and whether they lead back, w, h }. */
export function layoutRun(graph) {
  const level = levels(graph.nodes || [], graph.edges || []);
  const rows = [];
  for (const n of graph.nodes || []) (rows[level.get(n.id)] ||= []).push(n);
  const full = rows.filter(Boolean);
  const widest = Math.max(1, ...full.map(r => r.length));
  const rowW = (n) => n * NODE_W + (n - 1) * GAP_X;
  const nodes = [];
  full.forEach((row, ri) => {
    const left = PAD + (rowW(widest) - rowW(row.length)) / 2;
    row.forEach((n, i) => nodes.push({ ...n, x: left + i * (NODE_W + GAP_X), y: PAD + ri * (NODE_H + GAP_Y) }));
  });
  const byId = new Map(nodes.map(n => [n.id, n]));
  const w = PAD * 2 + rowW(widest);
  let loops = false;
  const edges = (graph.edges || []).filter(e => byId.has(e.from) && byId.has(e.to)).map(e => {
    const a = byId.get(e.from), b = byId.get(e.to);
    const x1 = a.x + NODE_W / 2, y1 = a.y + NODE_H, x2 = b.x + NODE_W / 2, y2 = b.y;
    if (y2 > y1) {
      const mid = (y1 + y2) / 2;
      return { ...e, back: false, d: `M${x1} ${y1} C${x1} ${mid}, ${x2} ${mid}, ${x2} ${y2}`, lx: (x1 + x2) / 2 + 4, ly: mid + 3 };
    }
    // Back or sideways (a loop): out to the right of the widest level, so it runs beside the nodes, and back in.
    loops = true;
    const out = w - PAD + LOOP, m1 = a.y + NODE_H * 0.7, m2 = b.y + NODE_H * 0.3;
    return { ...e, back: true, d: `M${a.x + NODE_W} ${m1} H${out} V${m2} H${b.x + NODE_W}`, lx: out + 3, ly: (m1 + m2) / 2 + 3 };
  });
  const h = PAD * 2 + Math.max(0, full.length * (NODE_H + GAP_Y) - GAP_Y);
  return { nodes, edges, w: loops ? w + LOOP + LOOP_LABEL : w, h };
}
