// The AWS map: resources inside the CloudFormation stack that made them or the VPC they are in, the rest by
// category, and a strip of what needs a look. Layout and rules are in lib/awsmap.js (notes 023 and 034).
// Nothing here is guessed: a line is drawn only where AWS states the link, and no health that was not measured.
import { h, icon, emptyState } from '../lib/dom.js';
import { buildMap, stateKind, attention, linePairs, arrangeStacks, contentsLine } from '../lib/awsmap.js';
import { flowPath } from '../lib/flow.js';

const STATE_DOT = { ok: 'bg-emerald-500', bad: 'bg-rose-500', wait: 'bg-amber-500', idle: 'bg-slate-400', none: 'bg-slate-300 dark:bg-slate-600' };
const CAT_DOT = { Compute: 'bg-sky-500', Containers: 'bg-violet-500', Storage: 'bg-emerald-500', Databases: 'bg-amber-500', Network: 'bg-cyan-500', Messaging: 'bg-pink-500', AI: 'bg-fuchsia-500', Management: 'bg-slate-500' };
const SHOWN = 12; // tiles per group before "more": enough to recognise a group, short enough for large accounts
const ATTN_SHOWN = 8;

export const keyOf = (svc, r) => svc.id + '|' + r.id;
// Security groups are many and rarely what someone looks for, so they come after what runs in a network.
const lastSG = (groups) => [...groups].sort((a, b) => (a.svc.id === 'sg') - (b.svc.id === 'sg'));

/**
 * mapView(inv, {query, selected, expanded, onSelect, onExpand}) returns the map element.
 * `selected` is a keyOf() string; `expanded` is a Set of group keys that show every tile.
 */
export function mapView(inv, { query = '', alarms = null, links = null, selected = '', expanded = new Set(), onSelect, onExpand }) {
  const m = buildMap(inv, query, alarms, links);
  if (!m.stacks.length && !m.vpcs.length && !m.regional.length) {
    return emptyState('cloud', query ? 'Nothing matches that search' : 'Nothing found in this region', query ? 'Try a name, an id or a tag.' : 'Global services such as S3 are listed too. Check the region in Change connection.');
  }

  function tile(svc, r) {
    const key = keyOf(svc, r), on = key === selected, why = attention(svc.id, r, alarms), kind = stateKind(r.state);
    const worst = why.some(w => w.level === 'bad');
    return h('button', {
      type: 'button', 'data-resource': r.id, 'data-key': key, 'aria-pressed': String(on),
      title: r.name + (r.state ? ' · ' + r.state : '') + (why.length ? ' · ' + why.map(w => w.text).join('; ') : ''),
      class: 'flex max-w-full items-center gap-2 rounded-lg border px-2.5 py-1.5 text-left text-sm transition hover:border-brand-400 ' + (on
        ? 'border-brand-500 bg-brand-50 ring-2 ring-brand-500/30 dark:bg-brand-950/30'
        : 'border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900'),
      onclick: () => onSelect(on ? '' : key),
    },
      h('span', { class: 'size-2 shrink-0 rounded-full ' + STATE_DOT[kind], 'aria-hidden': 'true' }),
      h('span', { class: 'max-w-48 truncate' }, r.name),
      r.state && h('span', { class: 'sr-only' }, 'state ' + r.state),
      why.length ? h('span', { class: 'shrink-0 ' + (worst ? 'text-rose-600 dark:text-rose-400' : 'text-amber-600 dark:text-amber-400'), role: 'img', 'aria-label': 'Needs a look: ' + why.map(w => w.text).join('; ') }, icon('alert', 'size-3.5')) : null);
  }

  function group(prefix, g) {
    const k = prefix + '/' + g.svc.id, open = expanded.has(k);
    let items = open ? g.items : g.items.slice(0, SHOWN);
    const sel = !open && selected ? g.items.find(r => keyOf(g.svc, r) === selected) : null;
    if (sel && !items.includes(sel)) items = [...items, sel]; // the selected one is always in view
    return h('div', { 'data-group': g.svc.id },
      h('div', { class: 'mb-1.5 flex items-center gap-2 text-xs' },
        h('span', { class: 'font-medium text-slate-700 dark:text-slate-300' }, g.svc.label), h('span', { class: 'muted tabular-nums' }, String(g.items.length))),
      h('div', { class: 'flex flex-wrap gap-1.5' }, items.map(r => tile(g.svc, r)),
        g.items.length > SHOWN ? h('button', { type: 'button', class: 'btn-ghost btn-sm', 'aria-expanded': String(open), onclick: () => onExpand(k) }, open ? 'Show fewer' : `+${g.items.length - items.length} more`) : null));
  }

  function vpcBlock(v) {
    return h('section', { class: 'rounded-2xl border border-dashed border-slate-300 p-4 dark:border-slate-700', 'data-vpc': v.id, 'aria-label': 'VPC ' + v.name },
      h('div', { class: 'mb-3 flex flex-wrap items-center gap-2' },
        icon('cloud', 'size-4 text-slate-500'),
        v.resource ? h('button', { type: 'button', class: 'font-medium hover:underline', onclick: () => onSelect(keyOf(v.svc, v.resource) === selected ? '' : keyOf(v.svc, v.resource)) }, v.name) : h('span', { class: 'font-medium' }, v.name),
        v.name !== v.id && h('code', { class: 'muted text-xs' }, v.id),
        v.cidr && h('span', { class: 'pill-idle' }, v.cidr),
        v.isDefault && h('span', { class: 'pill-idle' }, 'default'),
        v.synthetic && h('span', { class: 'muted text-xs' }, 'not in the VPC list'),
        h('span', { class: 'muted ml-auto text-xs tabular-nums' }, `${v.count} resource${v.count === 1 ? '' : 's'}`)),
      v.groups.length ? h('div', { class: 'space-y-3' }, lastSG(v.groups).map(g => group(v.id, g))) : h('p', { class: 'muted text-xs' }, 'Nothing in this VPC that Forgeline lists.'));
  }

  // Whether a stack shows its tiles: asked for, a search is on, or the selected resource is in it.
  const holdsSelected = (v) => !!selected && v.groups.some(g => g.items.some(r => keyOf(g.svc, r) === selected));
  const isOpen = (v) => expanded.has('s:' + v.id) || !!query || holdsSelected(v);
  const afterParts = (v) => v.others > 0 || v.kept > 0 ? h('p', { class: 'muted mt-2 text-xs' }, [
    v.others > 0 ? `It also made ${v.others} part${v.others === 1 ? '' : 's'} not shown on the map (roles, policies and the like; see the stack).` : '',
    v.kept > 0 ? ` ${v.kept} ${v.kept === 1 ? 'was' : 'were'} kept when the stack rolled back or was deleted, so the stack no longer manages ${v.kept === 1 ? 'it' : 'them'}.` : '',
  ].join('').trim()) : null;
  const stateDot = (r) => { const kind = stateKind(r?.state); return h('span', { class: 'size-2 shrink-0 rounded-full ' + STATE_DOT[kind], title: r?.state || '', 'aria-hidden': 'true' }); };

  // A compact stack card for the grid: its name, state and contents in one line; the tiles open on request.
  function stackCard(v) {
    const r = v.resource, key = r ? keyOf(v.svc, r) : '', open = isOpen(v);
    const why = r ? attention('cfn', r, alarms) : [];
    return h('section', { class: 'min-w-0 rounded-xl border-2 border-dashed p-3 ' + (key && key === selected ? 'border-brand-500' : why.some(w => w.level === 'bad') ? 'border-rose-300 dark:border-rose-500/50' : 'border-slate-300 dark:border-slate-700'),
      'data-stack': v.id, 'aria-label': 'Stack ' + v.name },
      h('div', { class: 'flex min-w-0 items-center gap-2' },
        icon('layers', 'size-4 shrink-0 text-slate-500'),
        r ? h('button', { type: 'button', class: 'min-w-0 truncate font-medium hover:underline', title: v.name + (r.state ? ' · ' + r.state : ''), 'data-key': key, 'aria-pressed': String(key === selected), onclick: () => onSelect(key === selected ? '' : key) }, v.name) : h('span', { class: 'min-w-0 truncate font-medium', title: v.name }, v.name),
        stateDot(r),
        h('span', { class: 'muted ml-auto shrink-0 text-xs tabular-nums' }, String(v.count))),
      v.error ? h('p', { class: 'mt-1 text-xs text-amber-700 dark:text-amber-400' }, 'What it made could not be read: ' + v.error) : null,
      v.groups.length ? h('button', { type: 'button', class: 'muted mt-1 flex w-full min-w-0 items-center gap-1 text-left text-xs hover:text-slate-700 dark:hover:text-slate-200', 'aria-expanded': String(open), 'data-toggle': v.id, onclick: () => onExpand('s:' + v.id) },
        icon('chevron', 'size-3 shrink-0 ' + (open ? 'rotate-90' : '')), h('span', { class: 'truncate' }, contentsLine(v))) : null,
      open && v.groups.length ? h('div', { class: 'mt-2 space-y-3' }, v.groups.map(g => group('s:' + v.id, g)), afterParts(v)) : null);
  }

  // Stacks with similar names (datadog-forwarder-…) in one card: a row each, opened one at a time.
  function familyCard(f) {
    return h('section', { class: 'min-w-0 rounded-xl border-2 border-dashed border-slate-300 p-3 lg:col-span-2 2xl:col-span-1 dark:border-slate-700', 'data-family': f.key, 'aria-label': 'Stacks named ' + f.key + '-…' },
      h('div', { class: 'mb-2 flex items-center gap-2' }, icon('layers', 'size-4 text-slate-500'),
        h('span', { class: 'font-medium' }, f.key + '-…'), h('span', { class: 'muted text-xs' }, `${f.stacks.length} stacks`),
        h('span', { class: 'muted ml-auto text-xs tabular-nums' }, String(f.count))),
      h('ul', { class: 'divide-y divide-slate-200 dark:divide-slate-800' }, f.stacks.map(v => {
        const open = isOpen(v), r = v.resource, key = r ? keyOf(v.svc, r) : '';
        return h('li', { class: 'py-1.5', 'data-stack': v.id },
          h('div', { class: 'flex min-w-0 items-center gap-2 text-sm' },
            h('button', { type: 'button', class: 'shrink-0', 'aria-label': (open ? 'Hide' : 'Show') + ' what ' + v.name + ' made', 'aria-expanded': String(open), 'data-toggle': v.id, onclick: () => onExpand('s:' + v.id) }, icon('chevron', 'size-3 text-slate-500 ' + (open ? 'rotate-90' : ''))),
            stateDot(r),
            r ? h('button', { type: 'button', class: 'min-w-0 truncate hover:underline' + (key === selected ? ' font-semibold text-brand-700 dark:text-brand-300' : ''), title: v.name, 'data-key': key, onclick: () => onSelect(key === selected ? '' : key) }, v.name.slice(f.key.length + 1) || v.name) : h('span', { class: 'min-w-0 truncate' }, v.name),
            h('span', { class: 'muted ml-auto hidden shrink-0 truncate text-xs sm:inline' }, contentsLine(v)),
            h('span', { class: 'muted shrink-0 text-xs tabular-nums' }, String(v.count))),
          v.error ? h('p', { class: 'ml-5 text-xs text-amber-700 dark:text-amber-400' }, 'What it made could not be read: ' + v.error) : null,
          open && v.groups.length ? h('div', { class: 'ml-5 mt-2 space-y-3' }, v.groups.map(g => group('s:' + v.id, g)), afterParts(v)) : null);
      })));
  }

  // Stacks that made nothing the map shows, in one line that opens.
  function quietStacks(list) {
    if (!list.length) return null;
    return h('details', { class: 'rounded-xl border border-slate-200 p-3 text-sm dark:border-slate-800', id: 'map-quiet', open: !!query },
      h('summary', { class: 'muted cursor-pointer text-xs' }, `${list.length} more stack${list.length === 1 ? '' : 's'} made only parts the map does not show (roles, policies and the like)`),
      h('div', { class: 'mt-2 flex flex-wrap gap-1.5' }, list.map(v => {
        const key = v.resource ? keyOf(v.svc, v.resource) : '';
        return h('button', { type: 'button', class: 'flex max-w-full items-center gap-1.5 rounded-lg border border-slate-200 px-2 py-1 text-xs dark:border-slate-800', 'data-stack': v.id, title: v.name, onclick: () => key && onSelect(key === selected ? '' : key) },
          stateDot(v.resource), h('span', { class: 'max-w-64 truncate' }, v.name));
      })));
  }

  function attentionStrip() {
    if (!m.attention.length) return null;
    const rows = m.attention.slice(0, ATTN_SHOWN);
    return h('details', { class: 'rounded-xl border border-amber-200 bg-amber-50 p-3 dark:border-amber-500/30 dark:bg-amber-950/30', 'aria-label': 'Needs a look', id: 'map-attention', open: m.attention.length <= 3 || !!query },
      h('summary', { class: 'flex cursor-pointer items-center gap-2 text-sm font-medium text-amber-900 dark:text-amber-200' }, icon('alert', 'size-4'),
        `${m.attention.length} ${m.attention.length === 1 ? 'resource needs' : 'resources need'} a look`),
      h('ul', { class: 'mt-2 space-y-0.5' }, rows.map(({ svc, r, why }) => h('li', {},
        h('button', { type: 'button', class: 'flex w-full min-w-0 items-baseline gap-x-2 rounded px-1 text-left text-sm hover:bg-amber-100 dark:hover:bg-amber-500/10', title: r.name + ': ' + why.map(w => w.text).join(' · '), onclick: () => onSelect(keyOf(svc, r)) },
          h('span', { class: 'max-w-72 shrink-0 truncate font-medium' }, r.name), h('span', { class: 'muted shrink-0 text-xs' }, svc.label),
          h('span', { class: 'min-w-0 truncate text-xs text-amber-800 dark:text-amber-300' }, why.map(w => w.text).join(' · ')))))),
      m.attention.length > ATTN_SHOWN && h('p', { class: 'muted mt-1 text-xs' }, `and ${m.attention.length - ATTN_SHOWN} more, marked on the map`));
  }

  const legend = h('div', { class: 'muted flex flex-wrap items-center gap-x-4 gap-y-1 text-xs', 'aria-label': 'Legend' },
    [['ok', 'running or available'], ['wait', 'changing'], ['bad', 'failing'], ['idle', 'other state']].map(([k, label]) => h('span', { class: 'flex items-center gap-1.5' }, h('span', { class: 'size-2 rounded-full ' + STATE_DOT[k] }), label)),
    h('span', { class: 'flex items-center gap-1.5' }, icon('alert', 'size-3.5 text-amber-600 dark:text-amber-400'), 'needs a look'),
    h('span', {}, 'Dashed box: what one CloudFormation stack made (open it to see its resources; similar names share a box), or one VPC. Lines: a function that reads a queue or a table’s stream, as Lambda states it (on a large account, only for the resource you pick); moving dots mean the link is enabled and show which way data goes (traffic is not measured). Nothing else is linked.'));

  function stackArea() {
    if (!m.stacks.length) return null;
    const ar = arrangeStacks(m.stacks, alarms);
    return h('div', { class: 'space-y-3', id: 'map-stacks' },
      ar.families.length || ar.single.length ? h('div', { class: 'grid items-start gap-3 lg:grid-cols-2 2xl:grid-cols-3' }, ar.families.map(familyCard), ar.single.map(stackCard)) : null,
      quietStacks(ar.quiet));
  }

  // On a large account every line at once crosses boxes and names; there only the selected resource's lines are drawn.
  const LARGE = 150;
  const allPairs = links ? linePairs(inv, links) : [];
  const large = m.total > LARGE;
  const pairs = large ? allPairs.filter(p => selected && (p.from === selected || p.to === selected)) : allPairs;
  // Under the tiles (isolate + -z-10), so a line shows between boxes and never across a name.
  const svg = h('svg', { class: 'pointer-events-none absolute inset-0 -z-10 mt-0! overflow-visible text-slate-400 dark:text-slate-500', 'aria-hidden': 'true', width: '0', height: '0' });
  const wrap = h('div', { class: 'relative isolate space-y-4', id: 'aws-map' }, legend, attentionStrip(),
    stackArea(),
    m.vpcs.map(vpcBlock),
    m.regional.length && m.vpcs.length ? h('p', { class: 'muted text-xs' }, 'Not placed in a VPC: the inventory records no VPC for these.') : null,
    m.regional.map(c => h('section', { 'data-category': c.category, 'aria-label': c.category },
      h('div', { class: 'mb-2 flex items-center gap-2' }, h('span', { class: 'size-2 rounded-full ' + (CAT_DOT[c.category] || 'bg-slate-500') }), h('h3', { class: 'h-section' }, c.category)),
      h('div', { class: 'space-y-3 rounded-2xl border border-slate-200 p-4 dark:border-slate-800' }, c.groups.map(g => group('r:' + c.category, g))))),
    svg);
  if (pairs.length) {
    // Lines need the laid-out tiles, so they are drawn after the browser places them, and again on resize.
    const draw = () => drawLines(wrap, svg, pairs, selected);
    requestAnimationFrame(draw);
    if (typeof ResizeObserver === 'function') new ResizeObserver(draw).observe(wrap);
  }
  return wrap;
}

/** Draws a line for each stated link whose two tiles are on the map; the selected resource's lines stand out. */
function drawLines(wrap, svg, pairs, selected) {
  if (!wrap.isConnected) return;
  const box = wrap.getBoundingClientRect();
  svg.setAttribute('width', String(Math.round(box.width)));
  svg.setAttribute('height', String(Math.round(box.height)));
  svg.replaceChildren();
  const at = key => wrap.querySelector('[data-key="' + CSS.escape(key) + '"]');
  for (const p of pairs) {
    const a = at(p.from), b = at(p.to);
    if (!a || !b) continue; // hidden by a search or behind "more"
    const ra = a.getBoundingClientRect(), rb = b.getBoundingClientRect();
    const on = selected && (p.from === selected || p.to === selected);
    // A curve that leaves and enters each tile on its left edge and bows out to the left, so it runs beside the
    // tiles and their headings instead of through them. A dot marks the end that is used.
    const x1 = ra.left - box.left, y1 = ra.top + ra.height / 2 - box.top, x2 = rb.left - box.left, y2 = rb.top + rb.height / 2 - box.top;
    const bow = Math.min(x1, x2) - Math.max(10, Math.min(28, Math.min(x1, x2) - 2));
    const cls = on ? 'text-brand-600 dark:text-brand-400' : '';
    const d = `M${x1} ${y1} C${bow} ${y1}, ${bow} ${y2}, ${x2} ${y2}`;
    svg.append(h('path', { d, fill: 'none', class: cls, stroke: 'currentColor',
      'stroke-width': on ? '2.5' : '1.5', 'stroke-dasharray': p.state === 'Enabled' ? '' : '4 4', 'data-line': p.from + '>' + p.to }));
    // An enabled event source hands data from the queue or stream (the line's end) to the function: dots run back.
    if (p.state === 'Enabled') svg.append(flowPath(d, on ? 'stroke-brand-600 dark:stroke-brand-400' : 'stroke-slate-500 dark:stroke-slate-300', { back: true, width: on ? 6 : 5 }));
    svg.append(h('circle', { cx: String(x2), cy: String(y2), r: on ? '4' : '3', class: cls, fill: 'currentColor' }));
  }
}
