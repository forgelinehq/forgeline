// The AWS map: resources inside the CloudFormation stack that made them or the VPC they are in, the rest by
// category, and a strip of what needs a look. Layout and rules are in lib/awsmap.js (notes 023 and 034).
// Nothing here is guessed: a line is drawn only where AWS states the link, and no health that was not measured.
import { h, icon, emptyState } from '../lib/dom.js';
import { buildMap, stateKind, attention, linePairs } from '../lib/awsmap.js';

const STATE_DOT = { ok: 'bg-emerald-500', bad: 'bg-rose-500', wait: 'bg-amber-500', idle: 'bg-slate-400', none: 'bg-slate-300 dark:bg-slate-600' };
const CAT_DOT = { Compute: 'bg-sky-500', Containers: 'bg-violet-500', Storage: 'bg-emerald-500', Databases: 'bg-amber-500', Network: 'bg-cyan-500', Messaging: 'bg-pink-500', AI: 'bg-fuchsia-500', Management: 'bg-slate-500' };
const SHOWN = 40; // tiles per group before "more"
const ATTN_SHOWN = 8;

export const keyOf = (svc, r) => svc.id + '|' + r.id;

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
    const items = open ? g.items : g.items.slice(0, SHOWN);
    return h('div', { 'data-group': g.svc.id },
      h('div', { class: 'mb-1.5 flex items-center gap-2 text-xs' },
        h('span', { class: 'font-medium text-slate-700 dark:text-slate-300' }, g.svc.label), h('span', { class: 'muted tabular-nums' }, String(g.items.length))),
      h('div', { class: 'flex flex-wrap gap-1.5' }, items.map(r => tile(g.svc, r)),
        g.items.length > SHOWN && !open ? h('button', { type: 'button', class: 'btn-ghost btn-sm', onclick: () => onExpand(k) }, `+${g.items.length - SHOWN} more`) : null));
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
      v.groups.length ? h('div', { class: 'space-y-3' }, v.groups.map(g => group(v.id, g))) : h('p', { class: 'muted text-xs' }, 'Nothing in this VPC that Forgeline lists.'));
  }

  // A stack's box: what CloudFormation says it made. Its own state is in the header, so a rolled-back stack shows.
  function stackBlock(v) {
    const r = v.resource, kind = stateKind(r?.state), key = r ? keyOf(v.svc, r) : '';
    const why = r ? attention('cfn', r, alarms) : [];
    return h('section', { class: 'rounded-2xl border-2 border-dashed p-4 ' + (key && key === selected ? 'border-brand-500' : why.some(w => w.level === 'bad') ? 'border-rose-300 dark:border-rose-500/50' : 'border-slate-300 dark:border-slate-700'),
      'data-stack': v.id, 'aria-label': 'Stack ' + v.name },
      h('div', { class: 'mb-3 flex flex-wrap items-center gap-2' },
        icon('layers', 'size-4 text-slate-500'),
        r ? h('button', { type: 'button', class: 'font-medium hover:underline', 'data-key': key, 'aria-pressed': String(key === selected), onclick: () => onSelect(key === selected ? '' : key) }, v.name) : h('span', { class: 'font-medium' }, v.name),
        h('span', { class: 'muted text-xs' }, 'CloudFormation stack'),
        r?.state && h('span', { class: 'flex items-center gap-1.5 text-xs ' + (kind === 'bad' ? 'text-rose-700 dark:text-rose-300' : 'muted') }, h('span', { class: 'size-2 rounded-full ' + STATE_DOT[kind], 'aria-hidden': 'true' }), r.state),
        h('span', { class: 'muted ml-auto text-xs tabular-nums' }, `${v.count} resource${v.count === 1 ? '' : 's'} here`)),
      v.error ? h('p', { class: 'text-xs text-amber-700 dark:text-amber-400' }, 'What it made could not be read: ' + v.error) : null,
      v.groups.length ? h('div', { class: 'space-y-3' }, v.groups.map(g => group('s:' + v.id, g))) : !v.error ? h('p', { class: 'muted text-xs' }, 'Nothing it made is in the lists Forgeline reads.') : null,
      v.others > 0 || v.kept > 0 ? h('p', { class: 'muted mt-3 text-xs' }, [
        v.others > 0 ? `It also made ${v.others} part${v.others === 1 ? '' : 's'} not shown on the map (roles, policies and the like; see the stack).` : '',
        v.kept > 0 ? ` ${v.kept} ${v.kept === 1 ? 'was' : 'were'} kept when the stack rolled back or was deleted, so the stack no longer manages ${v.kept === 1 ? 'it' : 'them'}.` : '',
      ].join('').trim()) : null);
  }

  function attentionStrip() {
    if (!m.attention.length) return null;
    const rows = m.attention.slice(0, ATTN_SHOWN);
    return h('section', { class: 'rounded-xl border border-amber-200 bg-amber-50 p-3 dark:border-amber-500/30 dark:bg-amber-950/30', 'aria-label': 'Needs a look', id: 'map-attention' },
      h('div', { class: 'mb-2 flex items-center gap-2 text-sm font-medium text-amber-900 dark:text-amber-200' }, icon('alert', 'size-4'),
        `${m.attention.length} ${m.attention.length === 1 ? 'resource needs' : 'resources need'} a look`),
      h('ul', { class: 'space-y-1' }, rows.map(({ svc, r, why }) => h('li', {},
        h('button', { type: 'button', class: 'flex w-full flex-wrap items-baseline gap-x-2 rounded px-1 text-left text-sm hover:bg-amber-100 dark:hover:bg-amber-500/10', onclick: () => onSelect(keyOf(svc, r)) },
          h('span', { class: 'font-medium' }, r.name), h('span', { class: 'muted text-xs' }, svc.label),
          h('span', { class: 'text-xs text-amber-800 dark:text-amber-300' }, why.map(w => w.text).join(' · ')))))),
      m.attention.length > ATTN_SHOWN && h('p', { class: 'muted mt-1 text-xs' }, `and ${m.attention.length - ATTN_SHOWN} more, marked on the map`));
  }

  const legend = h('div', { class: 'muted flex flex-wrap items-center gap-x-4 gap-y-1 text-xs', 'aria-label': 'Legend' },
    [['ok', 'running or available'], ['wait', 'changing'], ['bad', 'failing'], ['idle', 'other state']].map(([k, label]) => h('span', { class: 'flex items-center gap-1.5' }, h('span', { class: 'size-2 rounded-full ' + STATE_DOT[k] }), label)),
    h('span', { class: 'flex items-center gap-1.5' }, icon('alert', 'size-3.5 text-amber-600 dark:text-amber-400'), 'needs a look'),
    h('span', {}, 'Dashed box: what one CloudFormation stack made, or one VPC. Lines: a function that reads a queue or a table’s stream, as Lambda states it. Nothing else is linked.'));

  const pairs = links ? linePairs(inv, links) : [];
  // Under the tiles (isolate + -z-10), so a line shows between boxes and never across a name.
  const svg = h('svg', { class: 'pointer-events-none absolute inset-0 -z-10 mt-0! overflow-visible text-slate-400 dark:text-slate-500', 'aria-hidden': 'true', width: '0', height: '0' });
  const wrap = h('div', { class: 'relative isolate space-y-4', id: 'aws-map' }, legend, attentionStrip(),
    m.stacks.map(stackBlock),
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
    svg.append(h('path', { d: `M${x1} ${y1} C${bow} ${y1}, ${bow} ${y2}, ${x2} ${y2}`, fill: 'none', class: cls, stroke: 'currentColor',
      'stroke-width': on ? '2.5' : '1.5', 'stroke-dasharray': p.state === 'Enabled' ? '' : '4 4', 'data-line': p.from + '>' + p.to }));
    svg.append(h('circle', { cx: String(x2), cy: String(y2), r: on ? '4' : '3', class: cls, fill: 'currentColor' }));
  }
}
