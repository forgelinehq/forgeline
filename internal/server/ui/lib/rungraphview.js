import { h } from './dom.js';
import { fit } from './canvas.js';
import { flowPath } from './flow.js';
import { layoutRun, NODE_W, NODE_H } from './rungraph.js';

const TONE = {
  pending: 'stroke-slate-300 dark:stroke-slate-600', running: 'stroke-brand-500 motion-safe:animate-pulse', succeeded: 'stroke-emerald-500',
  failed: 'stroke-rose-500', waiting: 'stroke-amber-500', skipped: 'stroke-slate-300 dark:stroke-slate-700', cancelled: 'stroke-slate-300 dark:stroke-slate-700',
};
const WORD = { pending: 'not started', running: 'working', succeeded: 'done', failed: 'failed', waiting: 'waits for a person', skipped: 'skipped', cancelled: 'cancelled' };

/** The graph of a run, for the run card: each node with its status in words, the wire into what is working moving. */
export function runGraphView(graph) {
  const layout = layoutRun(graph);
  const status = Object.fromEntries(layout.nodes.map(n => [n.id, n.status]));
  const svg = h('svg', { width: String(layout.w), height: String(layout.h), viewBox: `0 0 ${layout.w} ${layout.h}`, class: 'mx-auto block select-none', role: 'img',
    'aria-label': 'The run as a graph: ' + layout.nodes.map(n => `${n.label} (${WORD[n.status] || n.status})`).join(', ') });
  for (const e of layout.edges) {
    svg.append(h('path', { d: e.d, fill: 'none', 'stroke-width': e.taken ? '2' : '1.5', class: e.taken ? 'stroke-emerald-500' : 'stroke-slate-300 dark:stroke-slate-600', 'data-edge': e.from + '>' + e.to }));
    if (e.taken && (status[e.to] === 'running' || status[e.to] === 'waiting')) svg.append(flowPath(e.d, 'stroke-brand-500'));
    if (e.when) svg.append(h('text', { x: String(e.lx), y: String(e.ly), class: 'fill-slate-500 text-[9px] dark:fill-slate-400' }, fit(e.when, 12)));
  }
  for (const n of layout.nodes) {
    const st = n.status;
    svg.append(h('g', { 'data-node': n.id, 'data-status': st },
      h('rect', { x: String(n.x), y: String(n.y), width: String(NODE_W), height: String(NODE_H), rx: '8', 'stroke-width': st === 'pending' ? '1.5' : '2',
        'stroke-dasharray': st === 'skipped' || st === 'cancelled' ? '4 3' : '', class: 'fill-white dark:fill-slate-900 ' + (TONE[st] || TONE.pending) }),
      h('text', { x: String(n.x + NODE_W / 2), y: String(n.y + 15), 'text-anchor': 'middle', class: 'fill-slate-900 text-[11px] font-semibold dark:fill-slate-100' }, fit(n.label, 19)),
      h('text', { x: String(n.x + NODE_W / 2), y: String(n.y + 28), 'text-anchor': 'middle', class: 'fill-slate-500 text-[9px] dark:fill-slate-400' }, WORD[st] || st),
      h('title', {}, `${n.label} (${WORD[st] || st})`)));
  }
  return h('div', { class: 'max-h-96 overflow-auto rounded-lg bg-slate-50 dark:bg-slate-950/40', 'data-run-graph': '' }, svg);
}
