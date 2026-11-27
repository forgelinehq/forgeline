// Agent graphs on a canvas (design note 044, part 2), like n8n: nodes as cards, wires from the right port of one to
// the left of the next, a palette to add nodes, a panel to set the selected one. The same canvas shows a run: each
// node's status and visits, the wires taken, and a question waiting for an answer. The server checks every save.
import { h, icon, iconAt, mount, spinner, errorBox, notice, toast, field, pill, drawer, leaveDialog } from '../lib/dom.js';
import { renderMarkdown } from '../lib/mdview.js';
import { scheduleField } from '../lib/schedulefield.js';
import { get, post, atLeast } from '../lib/api.js';
import { navigate } from '../lib/router.js';
import { session } from '../app.js';
import { fit } from '../lib/canvas.js';
import { flowPath } from '../lib/flow.js';
import {
  GNODE_W, GNODE_H, NODE_KINDS, branches, branchLabels, emptyGraph, updateNode, moveNode, removeNode, tidy,
  addWire, removeWire, wireProblem, nextLabel, stateKeys, autoPlace, wirePath, labelPoint, nodeSummary,
  graphRunStatus, stagesToGraph, fitView, zoomAt, toWorld, pickerItems, addPicked, triggerSummary, readOptions,
} from '../lib/agraph.js';
import { avatar, chatPanel } from './agents.js';
import { tabs, chipInput, choiceCards } from '../lib/controls.js';
import { toolInfo, tierTitle } from '../lib/tools.js';

const TONE = {
  pending: 'stroke-slate-300 dark:stroke-slate-600', running: 'stroke-brand-500 motion-safe:animate-pulse', succeeded: 'stroke-emerald-500',
  failed: 'stroke-rose-500', waiting: 'stroke-amber-500', cancelled: 'stroke-slate-300 dark:stroke-slate-700',
};
const WORD = { pending: '', running: 'working', succeeded: 'done', failed: 'failed', waiting: 'waits for an answer', cancelled: 'cancelled' };
const KIND_TONE = { start: 'stroke-brand-400', ask: 'stroke-amber-400', decide: 'stroke-violet-400', end: 'stroke-slate-400' };
const OPS = [['equals', 'equals'], ['contains', 'contains'], ['gt', 'is more than'], ['lt', 'is less than'], ['empty', 'is empty']];
const NO_LOOKUP = new Set(['ask_agent', 'request_environment', 'prepare_action']);

// Each kind's tile on its card: a colour and the kind's icon, as n8n marks its nodes.
const TILE = {
  start: { fill: 'fill-brand-100 dark:fill-brand-950', bg: 'bg-brand-100 dark:bg-brand-950', text: 'text-brand-700 dark:text-brand-300' },
  agent: { fill: 'fill-orange-100 dark:fill-orange-950', bg: 'bg-orange-100 dark:bg-orange-950', text: 'text-orange-700 dark:text-orange-300' },
  lookup: { fill: 'fill-sky-100 dark:fill-sky-950', bg: 'bg-sky-100 dark:bg-sky-950', text: 'text-sky-700 dark:text-sky-300' },
  decide: { fill: 'fill-violet-100 dark:fill-violet-950', bg: 'bg-violet-100 dark:bg-violet-950', text: 'text-violet-700 dark:text-violet-300' },
  ask: { fill: 'fill-amber-100 dark:fill-amber-950', bg: 'bg-amber-100 dark:bg-amber-950', text: 'text-amber-700 dark:text-amber-300' },
  merge: { fill: 'fill-teal-100 dark:fill-teal-950', bg: 'bg-teal-100 dark:bg-teal-950', text: 'text-teal-700 dark:text-teal-300' },
  end: { fill: 'fill-slate-100 dark:fill-slate-800', bg: 'bg-slate-100 dark:bg-slate-800', text: 'text-slate-600 dark:text-slate-300' },
};
const tileBox = (kind, size) => { const t = TILE[kind] || TILE.end; return h('span', { class: 'inline-flex flex-none items-center justify-center rounded-lg ' + size + ' ' + t.bg + ' ' + t.text }, icon(NODE_KINDS[kind]?.icon || 'box', 'size-4')); };

/** A new view for graphCanvas: it fits the graph to the box the first time the box has a size. */
export const newView = () => ({ x: 0, y: 0, k: 1, fitted: false });

/**
 * Draws a graph on a canvas that pans and zooms. opts: { selected, statuses (from graphRunStatus), editable, agentName,
 * view (from newView, kept across redraws), height (classes), startSummary, onSelect(id), onMove(id, x, y),
 * onWire(from, to), onPick(after) (the + of a node, or '' for the Add node button), onDelete(id), onTidy() }.
 */
// The node a click just selected: the canvas is drawn again for it, and the new node takes the keyboard focus.
let refocus = null;

export function graphCanvas(g, opts = {}) {
  const { selected = '', statuses = null, editable = false, agentName = (id) => id, view = newView(), height = 'h-[28rem]' } = opts;
  const byId = Object.fromEntries(g.nodes.map(n => [n.id, n]));
  const world = h('g', {});
  const grid = h('pattern', { id: 'graph-dots', width: '20', height: '20', patternUnits: 'userSpaceOnUse' }, h('circle', { cx: '1', cy: '1', r: '1', class: 'fill-slate-300 dark:fill-slate-700' }));
  const svg = h('svg', { class: 'block size-full touch-none select-none', id: 'graph-svg', role: 'group', 'aria-label': 'The workflow as a graph of ' + g.nodes.length + ' nodes' },
    h('defs', {}, grid), h('rect', { width: '100%', height: '100%', fill: 'url(#graph-dots)', 'data-bg': '' }), world);
  const zoomLabel = h('span', { class: 'w-12 text-center text-xs tabular-nums', id: 'graph-zoom' }, '100%');
  const apply = () => {
    const t = `translate(${view.x} ${view.y}) scale(${view.k})`;
    world.setAttribute('transform', t);
    grid.setAttribute('patternTransform', t);
    zoomLabel.textContent = Math.round(view.k * 100) + '%';
  };
  const box = h('div', { class: 'relative overflow-hidden rounded-xl border border-slate-200 bg-slate-50 dark:border-slate-800 dark:bg-slate-950/60 ' + height, id: 'graph-canvas' }, svg);
  const local = (e) => { const r = svg.getBoundingClientRect(); return { x: e.clientX - r.left, y: e.clientY - r.top }; };
  const setView = (v) => { Object.assign(view, v); apply(); };
  const fitNow = () => { if (box.clientWidth) { setView({ ...fitView(g, box.clientWidth, box.clientHeight), fitted: true }); } };
  const zoomBy = (f) => setView(zoomAt(view, f, box.clientWidth / 2, box.clientHeight / 2));

  for (const e of g.edges) {
    const a = byId[e.from], b = byId[e.to];
    if (!a || !b) continue;
    const d = wirePath(a, b);
    const taken = statuses && statuses.taken.has(e.from + '>' + e.to + '>' + (e.when || ''));
    world.append(h('path', { d, fill: 'none', 'stroke-width': taken ? '2.5' : '1.5', class: taken ? 'stroke-brand-500' : 'stroke-slate-400 dark:stroke-slate-600', 'data-wire': e.from + '>' + e.to + (e.when ? '>' + e.when : '') }));
    if (taken && statuses.nodes[e.to]?.status === 'running') world.append(flowPath(d, 'stroke-brand-500'));
    if (e.when) {
      // Labels of one node's wires stack, so two branches to the same row do not print over each other.
      const p = labelPoint(a, b), nth = g.edges.filter(x => x.from === e.from && x.when).indexOf(e);
      world.append(h('text', { x: String(p.x + 8), y: String(p.y - 4 - nth * 13), class: 'fill-violet-600 stroke-slate-50 text-[11px] font-medium [paint-order:stroke] [stroke-width:4px] dark:fill-violet-300 dark:stroke-slate-950', 'data-wire-label': e.when }, e.when));
    }
  }
  for (const n of g.nodes) {
    const k = NODE_KINDS[n.kind] || { label: n.kind, icon: 'box' };
    const st = statuses?.nodes[n.id];
    const tone = st ? TONE[st.status] || TONE.pending : n.id === selected ? 'stroke-brand-500' : KIND_TONE[n.kind] || 'stroke-slate-300 dark:stroke-slate-600';
    const tile = TILE[n.kind] || TILE.end;
    const summary = n.kind === 'start' && opts.startSummary ? opts.startSummary : nodeSummary(n, agentName);
    const open = editable && n.kind !== 'end';
    const dangling = open && !g.edges.some(e => e.from === n.id);
    const grp = h('g', { transform: `translate(${n.x} ${n.y})`, tabindex: '0', role: 'button', 'aria-pressed': String(n.id === selected), 'data-node': n.id, 'data-status': st?.status || '',
      'aria-label': (n.name || k.label) + ', ' + k.label + (st ? ', ' + (WORD[st.status] || st.status) : ''), class: (editable ? 'cursor-grab ' : 'cursor-pointer ') + 'group focus:outline-none' },
      h('rect', { width: String(GNODE_W), height: String(GNODE_H), rx: '12', 'stroke-width': n.id === selected ? '2.5' : st && st.status !== 'pending' ? '2' : '1.5',
        'stroke-dasharray': st?.status === 'cancelled' ? '4 3' : '', class: 'fill-white shadow-sm group-focus-visible:stroke-brand-500 dark:fill-slate-900 ' + tone }),
      h('rect', { x: '10', y: '14', width: '36', height: '36', rx: '9', class: tile.fill }),
      iconAt(k.icon, 18, 22, 20, tile.text),
      h('text', { x: '56', y: '28', class: 'fill-slate-900 text-[13px] font-semibold dark:fill-slate-100' }, fit(n.name || (n.kind === 'agent' ? agentName(n.agent) : k.label), 18)),
      h('text', { x: '56', y: '45', class: 'fill-slate-500 text-[11px] dark:fill-slate-400' }, fit(st && st.status !== 'pending' ? (WORD[st.status] || st.status) + (st.visits > 1 ? ' · ran ' + st.visits + ' times' : '') : summary, 24)),
      n.kind !== 'start' ? h('circle', { cx: '0', cy: String(GNODE_H / 2), r: '4', class: 'fill-white stroke-slate-400 dark:fill-slate-900' }) : null,
      dangling ? h('path', { d: `M${GNODE_W + 7} ${GNODE_H / 2} H${GNODE_W + 44}`, 'stroke-dasharray': '3 3', class: 'stroke-slate-400 pointer-events-none' }) : null,
      dangling ? h('g', { 'data-plus': n.id, class: 'cursor-pointer', role: 'button', 'aria-label': 'Add a node after ' + (n.name || k.label) },
        h('rect', { x: String(GNODE_W + 44), y: String(GNODE_H / 2 - 12), width: '24', height: '24', rx: '6', class: 'fill-white stroke-slate-400 hover:stroke-brand-500 dark:fill-slate-900' }),
        h('path', { d: `M${GNODE_W + 56} ${GNODE_H / 2 - 6} v12 M${GNODE_W + 50} ${GNODE_H / 2} h12`, class: 'stroke-slate-500 pointer-events-none', 'stroke-width': '1.8' })) : null,
      open ? h('g', { 'data-port': n.id, class: 'cursor-crosshair' },
        h('circle', { cx: String(GNODE_W), cy: String(GNODE_H / 2), r: '8', class: 'fill-brand-500 stroke-white hover:fill-brand-600 dark:stroke-slate-900' }),
        h('path', { d: `M${GNODE_W - 4} ${GNODE_H / 2} h8 M${GNODE_W} ${GNODE_H / 2 - 4} v8`, class: 'stroke-white pointer-events-none', 'stroke-width': '1.8' })) : null,
      h('title', {}, (n.name || k.label) + ' (' + k.label + '): ' + summary));
    grp.addEventListener('pointerdown', (ev) => {
      if (ev.button !== 0) return;
      ev.stopPropagation();
      if (ev.target.closest?.('[data-plus]')) { ev.preventDefault(); opts.onPick?.(n.id); return; }
      const port = ev.target.closest?.('[data-port]');
      if (port && editable) {
        // Click the port to add a node after this one; drag from it to wire it to another node.
        ev.preventDefault();
        const from = { x: n.x + GNODE_W, y: n.y + GNODE_H / 2 }, s0 = { x: ev.clientX, y: ev.clientY };
        const line = h('path', { fill: 'none', 'stroke-width': '2', 'stroke-dasharray': '5 4', class: 'stroke-brand-500 pointer-events-none' });
        world.append(line);
        const move = (e2) => { const p = toWorld(view, local(e2).x, local(e2).y); line.setAttribute('d', `M${from.x} ${from.y} L${p.x} ${p.y}`); };
        const up = (e2) => {
          window.removeEventListener('pointermove', move);
          window.removeEventListener('pointerup', up);
          line.remove();
          if (Math.hypot(e2.clientX - s0.x, e2.clientY - s0.y) < 4) { opts.onPick?.(n.id); return; }
          const target = document.elementFromPoint(e2.clientX, e2.clientY)?.closest?.('[data-node]');
          if (target && target.dataset.node !== n.id) opts.onWire?.(n.id, target.dataset.node);
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
        const dx = (e2.clientX - start.x) / view.k, dy = (e2.clientY - start.y) / view.k;
        if (!moved && Math.hypot(dx, dy) < 4) return;
        moved = true;
        grp.setAttribute('transform', `translate(${Math.max(0, start.nx + dx)} ${Math.max(0, start.ny + dy)})`);
      };
      const up = (e2) => {
        grp.removeEventListener('pointermove', move);
        grp.removeEventListener('pointerup', up);
        if (moved) opts.onMove?.(n.id, start.nx + (e2.clientX - start.x) / view.k, start.ny + (e2.clientY - start.y) / view.k);
        else {
          // Selecting redraws the canvas, which dropped the node's keyboard focus, so Delete and the arrow keys did nothing
          // until Tab found the node again. The redrawn node takes the focus back (see refocus below).
          refocus = n.id;
          opts.onSelect?.(n.id === selected ? '' : n.id);
        }
      };
      grp.addEventListener('pointermove', move);
      grp.addEventListener('pointerup', up);
    });
    if (refocus === n.id) {
      refocus = null;
      requestAnimationFrame(() => { if (grp.isConnected) grp.focus(); });
    }
    grp.addEventListener('keydown', (e) => {
      const step = e.shiftKey ? 40 : 10, d = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] }[e.key];
      if (d && editable) { e.preventDefault(); opts.onMove?.(n.id, n.x + d[0], n.y + d[1]); }
      else if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); opts.onSelect?.(n.id === selected ? '' : n.id); }
      else if ((e.key === 'Delete' || e.key === 'Backspace') && editable && n.kind !== 'start') { e.preventDefault(); opts.onDelete?.(n.id); }
    });
    world.append(grp);
  }

  // Drag the background to move around; a click on it clears the selection. Scroll moves, Ctrl or ⌘ and scroll zooms.
  svg.addEventListener('pointerdown', (ev) => {
    if (ev.button !== 0 || ev.target.closest?.('[data-node]')) return;
    const s0 = { x: ev.clientX, y: ev.clientY, vx: view.x, vy: view.y };
    let moved = false;
    svg.setPointerCapture(ev.pointerId);
    svg.classList.add('cursor-grabbing');
    const move = (e2) => {
      if (!moved && Math.hypot(e2.clientX - s0.x, e2.clientY - s0.y) < 4) return;
      moved = true;
      setView({ x: s0.vx + e2.clientX - s0.x, y: s0.vy + e2.clientY - s0.y });
    };
    const up = () => {
      svg.removeEventListener('pointermove', move);
      svg.removeEventListener('pointerup', up);
      svg.classList.remove('cursor-grabbing');
      if (!moved && selected) opts.onSelect?.('');
    };
    svg.addEventListener('pointermove', move);
    svg.addEventListener('pointerup', up);
  });
  svg.addEventListener('wheel', (e) => {
    e.preventDefault();
    if (e.ctrlKey || e.metaKey) { const p = local(e); setView(zoomAt(view, Math.exp(-e.deltaY * 0.002), p.x, p.y)); }
    else setView({ x: view.x - e.deltaX, y: view.y - e.deltaY });
  }, { passive: false });

  const ctl = (id, label, ic, onclick) => h('button', { type: 'button', class: 'btn-ghost btn-sm', id, 'aria-label': label, title: label, onclick }, icon(ic, 'size-4'));
  box.append(h('div', { class: 'absolute bottom-3 left-3 flex items-center rounded-lg border border-slate-200 bg-white/95 shadow-sm dark:border-slate-700 dark:bg-slate-900/95', id: 'graph-controls' },
    ctl('graph-zoom-out', 'Zoom out', 'minus', () => zoomBy(1 / 1.2)), zoomLabel, ctl('graph-zoom-in', 'Zoom in', 'plus', () => zoomBy(1.2)),
    ctl('graph-fit', 'Fit to the screen', 'expand', fitNow),
    opts.onTidy ? ctl('graph-tidy', 'Lay the nodes out again, left to right', 'grid', () => { view.fitted = false; opts.onTidy(); }) : null));
  if (editable && opts.onPick) box.append(h('div', { class: 'absolute right-3 top-3 flex items-center gap-2' },
    h('button', { type: 'button', class: 'btn-primary btn-sm shadow-sm', id: 'graph-add', onclick: () => opts.onPick(selected) }, icon('plus', 'size-4'), 'Add node')));
  if (editable) box.append(h('p', { class: 'muted pointer-events-none absolute bottom-3 right-3 hidden text-[11px] md:block' }, 'Drag the background to move · Ctrl + scroll to zoom · + adds the next node'));
  apply();
  // Fit once the box has a size (it is not on the page yet).
  if (!view.fitted) requestAnimationFrame(() => { if (!view.fitted) fitNow(); });
  return box;
}

// ------------------------------------------------------------------ the node picker

/** A side panel to pick what to add: agents by name, logic, lookups. Resolves with an item of pickerItems, or nothing. */
function pickNode(agents, lookups, afterName) {
  return drawer('Add a node', (close) => {
    const list = h('div', { class: 'space-y-5', id: 'picker-list' });
    const search = h('input', { class: 'input', type: 'search', id: 'picker-search', placeholder: 'Search agents, logic and lookups', 'aria-label': 'Search', autocomplete: 'off' });
    const draw = () => {
      const groups = pickerItems(agents, lookups, search.value);
      mount(list, groups.length ? groups.map(gr => h('section', { 'data-group': gr.id },
        h('h3', { class: 'h-section mb-2' }, gr.label),
        h('div', { class: 'grid gap-1.5' }, gr.items.map(it => {
          return h('button', { type: 'button', class: 'flex w-full items-start gap-3 rounded-lg border border-transparent p-2 text-left hover:border-slate-200 hover:bg-slate-50 focus-visible:border-brand-500 dark:hover:border-slate-700 dark:hover:bg-slate-800/60', 'data-pick': it.key, onclick: () => close(it) },
            h('span', { class: 'mt-0.5' }, tileBox(it.kind, 'size-8')),
            h('span', { class: 'min-w-0' }, h('span', { class: 'block text-sm font-medium' }, it.label), h('span', { class: 'muted block text-xs' }, it.note)));
        }))))
        : h('p', { class: 'muted text-sm' }, 'Nothing matches.' + (agents.length ? '' : ' Create an agent first under Agents.')));
    };
    search.addEventListener('input', draw);
    search.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); list.querySelector('[data-pick]')?.click(); } });
    draw();
    return h('div', { class: 'space-y-4' }, search, list);
  }, { subtitle: afterName ? 'It goes after ' + afterName + ', wired from it.' : 'It goes on the canvas; wire it from its port.' });
}

// ------------------------------------------------------------------ editor

/** Workflows › a workflow › Edit on the canvas (or a new graph workflow when params.id is "new"). */
export async function graphEditorPage({ params }) {
  const writer = atLeast(session.role, 'developer');
  const el = h('div', {});
  mount(el, spinner());
  let wf = null, agents = [], tools = [], skills = [];
  try {
    [agents, tools, skills] = await Promise.all([get('/v1/ai/agents').then(r => r.agents), get('/v1/ai/tools').then(r => r.tools), get('/v1/ai/skills').then(r => r.skills).catch(() => [])]);
    if (params.id !== 'new') wf = await get('/v1/ai/workflows/' + encodeURIComponent(params.id));
  } catch (e) { mount(el, errorBox(e.message)); return el; }
  if (!writer) { mount(el, notice('info', 'Developers and administrators change workflows.')); return el; }
  const live = agents.filter(a => !a.disabled);
  const agentName = (id) => agents.find(a => a.id === id)?.name || id || 'pick an agent';
  const lookups = tools.filter(t => !t.needs && !NO_LOOKUP.has(t.name) && !t.name.startsWith('propose_')).map(t => t.name);
  const cur = wf && wf.versions.find(v => v.n === wf.current);
  let g = cur?.graph ? autoPlace(cur.graph) : cur ? stagesToGraph(cur) : emptyGraph();
  let selected = '', problem = '', dirty = !wf, edited = false; // edited: changed since it was opened, so leaving asks
  const view = newView();
  const meta = { name: wf?.name || '', description: wf?.description || '', every: cur?.every_minutes || 0, cron: cur?.cron || '', tz: cur?.tz || '', runAs: cur?.run_as || 'developer', note: '', webhook: !!cur?.webhook, follow: (cur?.on_run_failed || []).join(', '), alarms: (cur?.on_alarm || []).join(', ') };
  const canvasBox = h('div', { class: 'relative' }), bar = h('div', {}), state = h('span', { class: 'text-xs', id: 'graph-dirty' });

  const drawState = () => mount(state, dirty ? h('span', { class: 'pill-wait' }, wf ? 'Unsaved changes' : 'Not saved yet') : h('span', { class: 'muted' }, 'Saved as v' + wf.current));
  // A change redraws the canvas; the side panel is rebuilt only when its shape changes, so typing in one field and
  // clicking the next does not lose the click to a rebuilt panel.
  const change = (next, panel = true) => { g = next; problem = ''; dirty = edited = true; drawProblem(); drawState(); draw(panel); };
  // Keep a node that was just picked clear of the side panel (22rem and its margin) and of the left edge.
  const reveal = (id) => {
    const n = g.nodes.find(x => x.id === id), w = canvasBox.clientWidth;
    if (!n || !w) return;
    const left = n.x * view.k + view.x, right = left + GNODE_W * view.k, limit = w - 384;
    if (right > limit) view.x -= right - limit + 24;
    else if (left < 16) view.x += 16 - left;
    const top = n.y * view.k + view.y, bottom = top + GNODE_H * view.k, ht = canvasBox.clientHeight;
    if (bottom > ht - 64) view.y -= bottom - ht + 64;
    else if (top < 56) view.y += 56 - top;
  };
  const select = (id) => { selected = id; reveal(id); draw(); };
  const wireUp = (from, to) => {
    const label = nextLabel(g, from);
    const why = wireProblem(g, from, to, label);
    if (why) { toast(why, 'error'); return; }
    change(addWire(g, from, to, label));
  };
  const remove = (id) => { selected = ''; change(removeNode(g, id)); };
  const openPicker = async (after) => {
    const a = after && g.nodes.find(n => n.id === after);
    const item = await pickNode(live, lookups, a && a.kind !== 'end' ? a.name || NODE_KINDS[a.kind].label : '');
    if (!item) return;
    const r = addPicked(g, item, a && a.kind !== 'end' ? after : '');
    selected = r.id;
    g = r.graph;
    reveal(r.id);
    change(r.graph);
  };

  const canvasSlot = h('div', {}), panelSlot = h('div', {});
  mount(canvasBox, canvasSlot, panelSlot);
  function draw(panel = true) {
    const n = g.nodes.find(x => x.id === selected);
    mount(canvasSlot, graphCanvas(g, { selected, editable: true, agentName, view, height: 'h-[calc(100vh-13rem)] min-h-[30rem]', startSummary: triggerSummary(meta),
      onSelect: select, onMove: (id, x, y) => change(moveNode(g, id, x, y), false), onWire: wireUp, onPick: openPicker, onDelete: remove, onTidy: () => change(tidy(g)) }));
    if (panel) mount(panelSlot, n ? h('aside', { class: 'card card-pad absolute bottom-3 right-3 top-14 w-[24rem] max-w-[calc(100%-1.5rem)] space-y-3 overflow-y-auto shadow-lg', id: 'graph-panel' }, nodePanel(n)) : null);
  }

  async function save(btn, stay = false) {
    btn.disabled = true;
    try {
      const body = { name: meta.name, description: meta.description, every_minutes: Number(meta.every), cron: meta.cron, tz: meta.tz, run_as: meta.runAs, note: meta.note, graph: g,
        webhook: meta.webhook, on_run_failed: meta.follow.split(',').map(x => x.trim()).filter(Boolean), on_alarm: meta.alarms.split(',').map(x => x.trim()).filter(Boolean) };
      const out = await post(wf ? '/v1/ai/workflows/' + wf.id + '/versions' : '/v1/ai/workflows', body);
      toast(wf ? 'Saved v' + out.current : 'Workflow created', 'ok');
      edited = false;
      if (!stay) navigate('/workflows/' + out.id);
      return true;
    } catch (e) { problem = e.message; drawProblem(); btn.disabled = false; if (stay) throw e; return false; }
  }

  // The workflow's settings, in a side panel so the canvas keeps the page.
  // The workflow's settings, in a side panel so the canvas keeps the page: what it is, what starts it, what it may do.
  let settingsTab = 'trigger';
  const openSettings = () => drawer('Workflow settings', () => {
    const desc = h('textarea', { id: 'graph-desc', class: 'input', rows: '3', maxlength: '300', placeholder: 'What it is for, in a sentence', oninput: (e) => { meta.description = e.target.value; dirty = edited = true; } }, meta.description);
    const hook = h('input', { type: 'checkbox', id: 'graph-webhook', class: 'mt-1', checked: meta.webhook, onchange: (e) => { meta.webhook = e.target.checked; dirty = edited = true; } });
    const card = (ic, title, note, ...kids) => h('section', { class: 'space-y-2 rounded-xl border border-slate-200 p-3.5 dark:border-slate-800' },
      h('div', { class: 'flex items-start gap-2.5' }, h('span', { class: 'flex size-8 flex-none items-center justify-center rounded-lg bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300' }, icon(ic, 'size-4')),
        h('div', { class: 'min-w-0' }, h('h3', { class: 'text-sm font-semibold' }, title), h('p', { class: 'muted text-xs' }, note))), ...kids);
    const t = tabs({ id: 'graph-settings', active: settingsTab, onchange: (v) => { settingsTab = v; }, items: [
      { id: 'about', label: 'About', icon: 'info', render: () => [field('Description', desc, 'Shown on the workflow\'s card and to the assistant.', 'graph-desc')] },
      { id: 'trigger', label: 'Trigger', icon: 'trigger', render: () => [
        card('play', 'By hand', 'Anyone who may see it can start it from its page. Always on.'),
        card('schedule', 'On a schedule', 'Workflows › Schedules lists every schedule and can pause one.',
          scheduleField({ id: 'graph-every', spec: { every_minutes: Number(meta.every), cron: meta.cron, tz: meta.tz },
            onChange: (sp) => { meta.every = sp.every_minutes || 0; meta.cron = sp.cron || ''; meta.tz = sp.tz || ''; dirty = edited = true; drawState(); } })),
        card('webhook', 'From a webhook', 'Another system starts it with a signed request. The secret is made on the workflow\'s page.',
          h('label', { class: 'flex items-start gap-2 text-sm', for: 'graph-webhook' }, hook, h('span', {}, 'Also starts from a signed webhook'))),
        card('failed', 'After a failed pipeline run', 'What failed reaches the workflow as its trigger, which agents read as data.',
          chipInput({ id: 'graph-follow', values: meta.follow.split(',').map(x => x.trim()).filter(Boolean), placeholder: 'pipeline names, or * for any',
            onchange: (v) => { meta.follow = v.join(', '); dirty = edited = true; drawState(); } })),
        card('alarm', 'When a CloudWatch alarm goes off', 'Names of alarms, or a pattern such as disk-* . Needs the alarm intake (Settings › Alarm intake). The alarm reaches the workflow as its trigger, which agents read as data.',
          chipInput({ id: 'graph-alarms', values: meta.alarms.split(',').map(x => x.trim()).filter(Boolean), placeholder: 'alarm names, or disk-*',
            onchange: (v) => { meta.alarms = v.join(', '); dirty = edited = true; drawState(); } }))] },
      { id: 'access', label: 'Permissions', icon: 'runas', render: () => [
        h('p', { class: 'muted text-xs' }, 'Who the run acts as. Never an administrator: change proposals still need a different administrator to approve them.'),
        choiceCards({ id: 'graph-runas', value: meta.runAs, onchange: (v) => { meta.runAs = v; dirty = edited = true; }, options: [
          { value: 'developer', label: 'Developer', icon: 'edit', note: 'Reads, and may file change proposals' },
          { value: 'viewer', label: 'Viewer', icon: 'eye', note: 'Reads only' }] }),
        h('p', { class: 'hint' }, 'Nodes share one state: each writes what it found under its id. Decisions take one wire; loops are allowed, each node runs at most its limit, and a run at most 50 steps.')] },
    ] });
    return t.el;
  }, { subtitle: meta.name || 'A new workflow', footer: (close) => h('button', { type: 'button', class: 'btn-primary btn-sm', id: 'graph-settings-done', onclick: () => close() }, 'Done') })
    .then(() => { drawState(); draw(); });

  function drawBar() {
    const saveBtn = h('button', { class: 'btn-primary', id: 'graph-save', type: 'button', onclick: () => save(saveBtn) }, icon('check', 'size-4'), wf ? 'Save version' : 'Create workflow');
    const name = h('input', { id: 'graph-name', class: 'input w-64 max-w-full font-semibold', maxlength: '60', value: meta.name, placeholder: 'Name the workflow', 'aria-label': 'Name', oninput: (e) => { meta.name = e.target.value; dirty = edited = true; drawState(); } });
    const note = h('input', { id: 'graph-note', class: 'input w-56 max-w-full', maxlength: '200', placeholder: wf ? 'What changed and why' : 'First version', 'aria-label': 'Note for this version', oninput: (e) => { meta.note = e.target.value; } });
    mount(bar, h('div', { class: 'mb-3 flex flex-wrap items-center gap-2' },
      h('a', { class: 'btn-ghost btn-sm', href: wf ? '#/workflows/' + wf.id : '#/workflows', 'aria-label': 'Back' }, icon('chevron', 'size-4 rotate-180'), 'Workflows'),
      name, state, h('div', { class: 'flex-1' }), note,
      h('button', { type: 'button', class: 'btn-secondary', id: 'graph-settings', onclick: openSettings }, icon('gear', 'size-4'), 'Settings'), saveBtn),
      h('div', { id: 'graph-problem' }));
    drawProblem();
  }
  const drawProblem = () => { const box = bar.querySelector('#graph-problem'); if (box) mount(box, problem ? h('div', { class: 'mb-3' }, errorBox(problem)) : null); };

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

  const skillName = (id) => skills.find(x => x.id === id)?.name || id;

  // Try an agent with this node's task: its own chat, beside the canvas, answering with the version the node uses.
  async function tryAgent(n) {
    let ag;
    try { ag = await get('/v1/ai/agents/' + encodeURIComponent(n.agent)); } catch (e) { return toast(e.message, 'error'); }
    const v = { n: n.agent_n || ag.current };
    drawer('Try ' + ag.name, () => {
      const box = chatPanel(ag, v);
      const ta = box.querySelector('#agent-input');
      if (ta && n.task) ta.value = n.task;
      return h('div', { class: 'space-y-3' }, h('p', { class: 'muted text-xs' }, 'A chat with version ' + v.n + ', as you, with only its own tools. The node\'s task is filled in; send it as it is or change it. Nothing here changes the workflow.'), box);
    }, { subtitle: 'Playground' });
  }

  function agentCard(n, set) {
    const a = agents.find(x => x.id === n.agent);
    const pick = select_(a ? 'Use another agent' : 'Agent', n.agent || '', [['', 'Pick one of your agents'], ...live.map(x => [x.id, x.name])], v => set({ agent: v, agent_n: 0 }), 'g-agent');
    const create = h('a', { class: 'text-xs underline', href: '#/agents/new/build', target: '_blank', rel: 'noopener', id: 'g-new-agent' }, 'Build a new agent');
    if (!a) return [pick, h('p', { class: 'hint -mt-1' }, 'An agent brings its model, instructions, skills and tools. ', create, ' (opens beside this one), then pick it here.')];
    const ver = n.agent_n || a.current;
    const row = (label, ...kids) => h('div', { class: 'grid grid-cols-[4.5rem_1fr] gap-2 text-xs' }, h('span', { class: 'muted' }, label), h('span', { class: 'min-w-0' }, ...kids));
    return [h('div', { class: 'space-y-2 rounded-xl border border-slate-200 bg-slate-50/60 p-3 dark:border-slate-700 dark:bg-slate-800/40', id: 'g-agent-card' },
      h('div', { class: 'flex items-center gap-2.5' }, avatar(a.name, a.id, 'size-9 text-xs'),
        h('div', { class: 'min-w-0' }, h('div', { class: 'truncate font-semibold' }, a.name), h('div', { class: 'muted text-xs' }, 'version ' + ver + (n.agent_n ? ' (kept)' : ' (newest)') + ' · ' + tierTitle(a.tier)))),
      row('Model', a.model || 'the assistant\'s model'),
      row('Skills', a.skills?.length ? a.skills.map(sp => skillName(sp.id)).join(', ') : h('span', { class: 'muted' }, 'none')),
      row('Tools', a.tools?.length ? a.tools.map(t => toolInfo(t).title).join(', ') : h('span', { class: 'muted' }, 'none: it answers from what it is given')),
      n.agent_n && n.agent_n < a.current ? h('button', { type: 'button', class: 'btn-ghost btn-sm', id: 'g-agent-newest', onclick: () => set({ agent_n: 0 }) }, 'Use its newest version (v' + a.current + ')') : null,
      h('div', { class: 'flex flex-wrap gap-2 pt-1' },
        h('button', { type: 'button', class: 'btn-primary btn-sm', id: 'g-try', onclick: () => tryAgent(n) }, icon('send', 'size-3.5'), 'Try it'),
        h('a', { class: 'btn-secondary btn-sm', href: '#/agents/' + a.id + '/build', target: '_blank', rel: 'noopener' }, icon('edit', 'size-3.5'), 'Change the agent'))),
      pick];
  }

  // Earlier answers it is handed, as chips to switch on and off.
  function readsField(n, set, hint) {
    const opts = readOptions(g, n.id);
    const on = new Set(n.reads || []);
    return h('div', {}, h('div', { class: 'label' }, 'Also hand it'), h('p', { class: 'muted -mt-1 mb-1.5 text-xs' }, hint),
      opts.length ? h('div', { class: 'flex flex-wrap gap-1.5', id: 'g-reads' }, opts.map(o => h('button', { type: 'button', 'aria-pressed': String(on.has(o.key)), 'data-read': o.key, title: o.sub,
        class: 'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs ' + (on.has(o.key)
          ? 'border-brand-400 bg-brand-50 text-brand-800 dark:border-brand-500/60 dark:bg-brand-500/10 dark:text-brand-200'
          : 'border-slate-200 text-slate-600 hover:border-slate-300 dark:border-slate-700 dark:text-slate-300'),
        onclick: () => set({ reads: on.has(o.key) ? (n.reads || []).filter(x => x !== o.key) : [...(n.reads || []), o.key] }) },
        icon(on.has(o.key) ? 'check' : 'plus', 'size-3'), o.label)))
        : h('p', { class: 'muted text-xs' }, 'Nothing yet: add nodes before this one and their answers show up here.'));
  }

  function taskField(n, set) {
    const x = h('textarea', { class: 'input text-sm leading-relaxed', rows: '5', id: 'g-task', maxlength: '2000',
      placeholder: n.kind === 'decide' ? 'Is this an incident? Answer with one of the branches.' : 'Explain why the latest run failed, in plain words.' }, n.task || '');
    x.addEventListener('change', () => set({ task: x.value.trim() }));
    return h('div', {}, h('label', { class: 'label flex items-center gap-1.5', for: 'g-task' }, icon('task', 'size-3.5'), n.kind === 'decide' ? 'What to decide' : 'Task'), x,
      h('p', { class: 'hint' }, 'What this node asks its agent. The agent\'s own instructions still apply.'));
  }

  const savedAs = (n, what) => h('div', { class: 'flex items-start gap-2 rounded-lg bg-slate-50 px-3 py-2 text-sm dark:bg-slate-800/50' }, icon('output', 'mt-0.5 size-4 text-slate-500'),
    h('span', {}, what + ' is saved as ', h('span', { class: 'font-medium' }, n.name || NODE_KINDS[n.kind].label), h('code', { class: 'muted ml-1 text-xs' }, n.id), ', for the nodes after it.'));

  // The open tab for each kind of node, so rebuilding the panel after a change keeps it.
  const panelTab = {};

  // The panel: the node's name and kind, then one tab at a time (who does it, what goes in, what comes out, where it
  // goes next, limits) instead of one long column.
  function nodePanel(node) {
    let n = node;
    // What changes the panel's own fields (or the wires it lists) rebuilds it; text does not.
    const SHAPE = ['by', 'agent', 'outcome', 'choices', 'tool', 'reads'];
    const set = (fields) => {
      const reshapes = SHAPE.some(k => k in fields) || ('rules' in fields && ((fields.rules || []).length !== (n.rules || []).length
        || (fields.rules || []).some((r, i) => r.when !== n.rules[i]?.when || r.key !== n.rules[i]?.key)));
      change(updateNode(g, n.id, fields), reshapes);
      if (!reshapes) n = g.nodes.find(x => x.id === n.id) || n;
    };
    const name = h('input', { id: 'g-name', class: 'w-full rounded bg-transparent text-base font-semibold focus:outline-none focus:ring-2 focus:ring-brand-500/25', value: n.name || '', maxlength: '60',
      placeholder: NODE_KINDS[n.kind].label, 'aria-label': 'Name on the canvas' });
    name.addEventListener('change', () => set({ name: name.value.trim() }));
    const head = h('div', { class: 'flex items-start gap-2.5' }, tileBox(n.kind, 'size-9'),
      h('div', { class: 'min-w-0 flex-1' }, name, h('p', { class: 'muted text-xs', id: 'g-about' }, NODE_KINDS[n.kind].label + ' · ' + NODE_KINDS[n.kind].note + '.')),
      h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Close', onclick: () => select('') }, icon('x', 'size-4')));

    const T = [];
    const agentTab = { id: 'agent', label: 'Agent', icon: 'agents', render: () => agentCard(n, set) };
    const inputTab = { id: 'input', label: 'Input', icon: 'input', badge: (n.reads || []).length || null, render: () => [taskField(n, set),
      readsField(n, set, 'Earlier answers, passed as data: it reads them but does not take orders from them.')] };
    switch (n.kind) {
      case 'start':
        T.push({ id: 'trigger', label: 'Trigger', icon: 'trigger', render: () => [
          h('div', { class: 'flex items-start gap-2 rounded-lg bg-slate-50 px-3 py-2.5 text-sm dark:bg-slate-800/50' }, icon('trigger', 'mt-0.5 size-4 text-slate-500'), h('span', {}, 'Starts ' + triggerSummary(meta) + '.')),
          h('button', { type: 'button', class: 'btn-secondary btn-sm', onclick: openSettings }, icon('gear', 'size-3.5'), 'Change what starts it')] });
        break;
      case 'agent':
        T.push(agentTab, inputTab, { id: 'output', label: 'Output', icon: 'output', badge: (n.fields || []).length || null, render: () => [savedAs(n, 'Its answer'),
          chipInput({ id: 'g-fields', label: 'Pull these out of the answer too', values: n.fields || [], placeholder: 'severity, service', onchange: (v) => set({ fields: v }),
            hint: 'Short values such as severity: high. The agent is asked for them at the end of its answer, and a Decide node can test them. Enter or a comma adds one.' })] });
        break;
      case 'lookup':
        T.push({ id: 'lookup', label: 'Lookup', icon: 'lookup', render: () => [
          select_('Lookup', n.tool || '', [['', 'Pick a lookup'], ...lookups.map(t => [t, toolInfo(t).title + ' (' + t + ')'])], v => set({ tool: v }), 'g-tool'),
          textarea('Arguments (JSON)', n.args ? JSON.stringify(n.args) : '{}', v => { try { set({ args: v ? JSON.parse(v) : null }); } catch { toast('Not valid JSON', 'error'); } }, 'g-args', 3),
          n.tool ? h('p', { class: 'hint -mt-2', id: 'g-tool-help' }, 'It takes ' + (tools.find(t => t.name === n.tool)?.help || '{}') + '. Numbers without quotes.') : h('p', { class: 'hint -mt-2' }, 'It reads; no model is asked.')] },
        { id: 'output', label: 'Output', icon: 'output', render: () => [savedAs(n, 'What it returns')] });
        break;
      case 'decide': {
        const byRule = (n.by || 'rule') === 'rule';
        T.push({ id: 'decide', label: 'Choose', icon: 'decide', render: () => {
          const out = [h('p', { class: 'muted text-xs' }, 'Each wire out of a Decide node is a branch; it takes one.'),
            choiceCards({ id: 'g-by', value: n.by || 'rule', onchange: (v) => set({ by: v, ...(v === 'rule' ? { agent: null, task: null, reads: null } : { rules: null }) }), options: [
              { value: 'rule', label: 'By a rule', icon: 'rules', note: 'Test what earlier nodes wrote' },
              { value: 'agent', label: 'By an agent', icon: 'agents', note: 'An agent picks a branch' }] })];
          if (byRule) {
            const rules = n.rules || [];
            const keys = stateKeys(g, n.id);
            out.push(h('div', { class: 'space-y-2', id: 'g-rules' }, rules.map((r, i) => {
              const upd = (f) => set({ rules: (n.rules || []).map((x, j) => j === i ? { ...x, ...f } : x) });
              return h('div', { class: 'space-y-1.5 rounded-xl border border-slate-200 p-2.5 dark:border-slate-800', 'data-rule': String(i) },
                h('div', { class: 'flex items-center gap-1.5 text-xs font-medium text-slate-500' }, h('span', { class: 'rounded bg-slate-100 px-1.5 dark:bg-slate-800' }, 'If'), 'rule ' + (i + 1),
                  h('button', { class: 'btn-ghost ml-auto p-1', type: 'button', 'aria-label': 'Remove rule ' + (i + 1), onclick: () => set({ rules: (n.rules || []).filter((_, j) => j !== i) }) }, icon('trash', 'size-3.5'))),
                h('div', { class: 'grid grid-cols-2 gap-1.5' },
                  h('select', { class: 'input', 'aria-label': 'Key', onchange: (e) => upd({ key: e.target.value }) }, [['', 'value of…'], ...keys.map(k => [k, k])].map(([v, t]) => h('option', { value: v, selected: v === (r.key || '') }, t))),
                  h('select', { class: 'input', 'aria-label': 'Test', onchange: (e) => upd({ op: e.target.value }) }, OPS.map(([v, t]) => h('option', { value: v, selected: v === (r.op || 'equals') }, t))),
                  h('input', { class: 'input', 'aria-label': 'Value', value: r.value || '', placeholder: 'high', onchange: (e) => upd({ value: e.target.value.trim() }) }),
                  h('div', { class: 'flex items-center gap-1.5' }, icon('next', 'size-3.5 flex-none text-violet-600'),
                    h('input', { class: 'input', 'aria-label': 'Branch', value: r.when || '', placeholder: 'branch, e.g. page', onchange: (e) => upd({ when: e.target.value.trim().toLowerCase() }) }))));
            }), h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'g-add-rule', onclick: () => { const now = n.rules || []; set({ rules: [...now, { key: keys[0] || '', op: 'equals', value: '', when: 'branch-' + (now.length + 1) }] }); } }, icon('plus', 'size-3.5'), 'Add a rule')),
            h('p', { class: 'hint' }, 'The first rule that matches picks its branch; otherwise the else wire.'));
          } else out.push(h('p', { class: 'hint' }, 'The agent must answer one of the wires’ branches; an unclear answer is asked once more, then else is taken. Set it up under Agent and Input.'));
          return out;
        } });
        if (!byRule) T.push(agentTab, inputTab);
        break;
      }
      case 'ask':
        T.push({ id: 'question', label: 'Question', icon: 'ask', render: () => [textarea('Question', n.question, v => set({ question: v }), 'g-question', 3),
          chipInput({ id: 'g-choices', label: 'Choices', values: n.choices || [], lower: true, placeholder: 'approve, reject', onchange: (v) => set({ choices: v }), hint: 'With choices, the answer picks the wire. With none, any answer is taken.' }),
          chipInput({ id: 'g-answerers', label: 'Who may answer', values: n.answerers || [], placeholder: 'user names', onchange: (v) => set({ answerers: v }), hint: 'Empty: any administrator.' }),
          h('p', { class: 'hint' }, 'The run waits here, up to 7 days, until someone answers in the run view. The answer is saved under ' + n.id + '.')] });
        break;
      case 'end':
        T.push({ id: 'end', label: 'Ending', icon: 'end', render: () => [choiceCards({ id: 'g-outcome', value: n.outcome || 'succeeded', onchange: (v) => set({ outcome: v }), options: [
          { value: 'succeeded', label: 'Done', icon: 'succeeded', note: 'The run succeeded' }, { value: 'failed', label: 'Failed', icon: 'failed', note: 'The run failed here' }] }),
          input('Summary', n.summary, v => set({ summary: v }), { id: 'g-summary', maxlength: '500', placeholder: 'What happened, for the run list' })] });
        break;
    }

    // Its wires, and one more.
    const mine = g.edges.filter(e => e.from === n.id);
    if (n.kind !== 'end') T.push({ id: 'next', label: 'Next', icon: 'next', badge: mine.length || null, render: () => {
      const others = g.nodes.filter(x => x.id !== n.id && x.kind !== 'start');
      const to = h('select', { class: 'input min-w-0 flex-1', id: 'g-wire-to', 'aria-label': 'Wire to' }, others.map(x => h('option', { value: x.id }, x.name || NODE_KINDS[x.kind].label)));
      const label = branches(n) ? h('select', { class: 'input w-auto', id: 'g-wire-when', 'aria-label': 'Branch' }, branchLabels(n).map(l => h('option', { value: l }, l))) : null;
      return [h('p', { class: 'muted text-xs' }, 'Where the run goes after this node. Drag from its right edge on the canvas, or pick here.'),
        mine.length ? h('ul', { class: 'space-y-1.5', id: 'g-wires' }, mine.map(e => {
          const t = g.nodes.find(x => x.id === e.to);
          return h('li', { class: 'flex items-center gap-2.5 rounded-xl border border-slate-200 px-2.5 py-2 dark:border-slate-800' },
            e.when ? h('span', { class: 'rounded-full bg-violet-50 px-2 py-0.5 text-xs font-medium text-violet-700 dark:bg-violet-500/15 dark:text-violet-300' }, 'if ' + e.when) : icon('next', 'size-4 text-slate-400'),
            t ? tileBox(t.kind, 'size-6') : null,
            h('span', { class: 'min-w-0 flex-1 truncate text-sm font-medium' }, t ? t.name || NODE_KINDS[t.kind].label : e.to),
            h('button', { class: 'btn-ghost p-1', type: 'button', 'aria-label': 'Remove the wire to ' + e.to, onclick: () => change(removeWire(g, e.from, e.to, e.when || '')) }, icon('x', 'size-3.5')));
        })) : h('p', { class: 'rounded-lg border border-dashed border-slate-300 px-3 py-2.5 text-xs text-slate-500 dark:border-slate-700' }, 'No wire yet: the run would stop here.'),
        others.length ? h('div', { class: 'flex flex-wrap gap-2' }, label, to, h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'g-wire-add', onclick: () => {
          const why = wireProblem(g, n.id, to.value, label ? label.value : '');
          if (why) return toast(why, 'error');
          change(addWire(g, n.id, to.value, label ? label.value : ''));
        } }, icon('link', 'size-3.5'), 'Connect')) : null,
        h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'g-add-after', onclick: () => openPicker(n.id) }, icon('plus', 'size-3.5'), 'Add a node after this one')];
    } });
    if (n.kind !== 'start' && n.kind !== 'end') T.push({ id: 'limits', label: 'Limits', icon: 'limits', render: () => [
      input('Most times it may run in one run', n.max_visits || '', v => set({ max_visits: v || null }), { type: 'number', min: '1', max: '10', placeholder: '3', id: 'g-max' }),
      h('p', { class: 'hint' }, 'Only matters in a loop, when a wire leads back to it. Then the run stops it after this many times.')] });

    const t = tabs({ items: T, active: panelTab[n.kind], onchange: (id) => { panelTab[n.kind] = id; }, id: 'g-panel', compact: true });
    const out = [head, t.el];
    if (n.kind !== 'start') out.push(h('div', { class: 'flex justify-end border-t border-slate-200 pt-3 dark:border-slate-800' },
      h('button', { class: 'btn-ghost btn-sm text-rose-600 dark:text-rose-400', type: 'button', id: 'g-remove', onclick: () => remove(n.id) }, icon('trash', 'size-3.5'), 'Remove node')));
    return out;
  }

  mount(el, bar,
    !live.length ? h('div', { class: 'mb-3' }, notice('info', 'Create an agent first: agent and decision nodes need one.')) : null,
    cur && !cur.graph ? h('div', { class: 'mb-3' }, notice('info', 'This workflow had stages. They are drawn here as a graph; saving makes a new version as a graph.')) : null,
    canvasBox);
  drawBar(); drawState(); draw();
  el.leaveGuard = () => ({ dirty: edited, ask: () => leaveDialog({ what: 'this workflow', save: () => save({}, true) }) });
  return el;
}

// ------------------------------------------------------------------ a graph run

/** The run page's graph part: the canvas with statuses, a waiting question, and each visit of the selected node. */
export function graphRunView(r, g, agents, reload) {
  const agentName = (id) => agents.find(a => a.id === id)?.name || id;
  const box = h('div', { class: 'space-y-4', id: 'graph-run' });
  let selected = r.waiting || '';
  const statuses = graphRunStatus(g, r), view = newView();
  function draw() {
    const waitingNode = r.waiting && g.nodes.find(n => n.id === r.waiting);
    const visits = (r.visits || []).filter(v => !selected || v.node === selected);
    mount(box,
      waitingNode ? askCard(r, waitingNode, reload) : null,
      graphCanvas(g, { statuses, selected, agentName, view, onSelect: (id) => { selected = id; draw(); } }),
      h('section', { class: 'card card-pad' },
        h('div', { class: 'mb-2 flex flex-wrap items-center justify-between gap-2' }, h('h2', { class: 'h-section' }, selected ? (() => { const n = g.nodes.find(x => x.id === selected); return n ? (n.name || NODE_KINDS[n.kind].label) + ': what it did' : selected; })() : 'Every step, in order'),
          h('div', { class: 'flex items-center gap-3 text-xs muted' }, h('span', {}, (r.steps || 0) + ' of 50 steps'), selected ? h('button', { class: 'btn-ghost btn-sm', type: 'button', onclick: () => { selected = ''; draw(); } }, 'Show all') : null)),
        visits.length ? h('ol', { class: 'space-y-2', id: 'graph-visits' }, visits.map(v => h('li', { class: 'rounded-md bg-slate-50 p-2.5 text-sm dark:bg-slate-900', 'data-visit': v.node + '#' + v.visit, 'data-status': v.status },
          h('div', { class: 'mb-1 flex flex-wrap items-center justify-between gap-2' },
            h('span', { class: 'font-medium' }, (g.nodes.find(n => n.id === v.node)?.name || v.node) + (v.visit > 1 ? ' (time ' + v.visit + ')' : ''), h('span', { class: 'muted ml-2 text-xs' }, NODE_KINDS[v.kind]?.label || v.kind)),
            h('span', { class: 'flex items-center gap-2' }, v.branch ? h('span', { class: 'pill-idle' }, '→ ' + v.branch) : null, pill(v.status))),
          v.agent_label ? h('p', { class: 'muted text-xs' }, 'Agent ' + v.agent_label) : null,
          v.answered_by ? h('p', { class: 'muted text-xs' }, 'Answered by ' + v.answered_by) : null,
          v.error ? h('p', { class: 'text-rose-700 dark:text-rose-400' }, v.error) : null,
          v.input ? h('details', {}, h('summary', { class: 'cursor-pointer text-xs' }, 'What went in'), h('pre', { class: 'mt-1 max-h-64 overflow-auto whitespace-pre-wrap text-xs' }, v.input)) : null,
          v.output ? h('div', { class: 'mt-1 max-h-96 overflow-auto break-words', 'data-output': '' }, renderMarkdown(v.output)) : null,
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

