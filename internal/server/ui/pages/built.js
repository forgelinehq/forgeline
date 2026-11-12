// Build › Built: everything Forgeline created and keeps in your cloud accounts. Each built blueprint is drawn as an
// interactive diagram (its blocks where they sit on the canvas, with what flows between them moving), each block with
// the AWS resources it made; pick one to list them. Stacks and environments follow. "Check against AWS" looks each
// resource up in the account's inventory (a read) and marks it as still there or missing.
import { h, icon, mount, pageHeader, relTime, spinner, errorBox, notice, emptyState, copyButton } from '../lib/dom.js';
import { get } from '../lib/api.js';
import { KINDS, blockFacts, linkWords } from '../lib/blueprint.js';
import { BLOCK_W, BLOCK_H, linkPath, linkPorts, bounds, fit, flowOf } from '../lib/canvas.js';
import { flowPath } from '../lib/flow.js';
import { typeWords, byBlock, checkState, totals, resourceLabel } from '../lib/built.js';

const STATE_DOT = { present: 'fill-emerald-500', missing: 'fill-rose-500', unchecked: 'fill-slate-300 dark:fill-slate-600' };
const STATE_WORDS = { present: 'all there', missing: 'something is missing', unchecked: 'not checked' };

export async function builtPage() {
  const el = h('div', {});
  let checked = false;
  const header = (busy) => pageHeader('Built by Forgeline', 'Everything Forgeline created and keeps in your cloud accounts, each through a change a different administrator approved.',
    h('button', { class: 'btn-secondary', type: 'button', id: 'built-check', disabled: !!busy, onclick: () => load(true) }, icon('refresh', 'size-4'), busy ? 'Checking…' : 'Check against AWS'));

  async function load(check) {
    mount(el, header(check), spinner());
    let b;
    try { b = await get('/v1/built' + (check ? '?check=1' : '')); } catch (e) { mount(el, header(), errorBox(e.message)); return; }
    checked = b.checked;
    draw(b);
  }

  function draw(b) {
    const t = totals(b);
    if (!t.blueprints && !t.stacks && !t.environments) {
      mount(el, header(), h('div', { class: 'card' }, emptyState('layers', 'Nothing built yet', 'When a blueprint, a stack or an environment is approved and built, it is listed here with everything it made.',
        h('a', { class: 'btn-primary', href: '#/design' }, 'Open the design canvas'))));
      return;
    }
    const tile = (n, label, sub) => h('div', { class: 'card card-pad' }, h('div', { class: 'text-2xl font-semibold tabular-nums' }, String(n)), h('div', { class: 'text-sm font-medium' }, label), sub && h('div', { class: 'muted text-xs' }, sub));
    mount(el, header(),
      checked && t.missing ? h('div', { class: 'mb-4' }, notice('warn', `${t.missing} ${t.missing === 1 ? 'resource Forgeline built is' : 'resources Forgeline built are'} not in the account any more: someone may have deleted ${t.missing === 1 ? 'it' : 'them'} outside Forgeline.`)) : null,
      checked && !t.missing && t.checked ? h('div', { class: 'mb-4' }, notice('info', `Checked against AWS just now: all ${t.checked} resources the inventory can list are still there. Kinds it cannot list (roles, rules, listeners) are marked not checked.`)) : null,
      checked && !t.checked ? h('div', { class: 'mb-4' }, notice('info', 'Checked against AWS just now, but none of these could be matched: the inventory does not list their kinds, or their saved state names no AWS id to look for.')) : null,
      h('div', { class: 'mb-6 grid grid-cols-2 gap-3 lg:grid-cols-4', id: 'built-totals' },
        tile(t.blueprints, t.blueprints === 1 ? 'blueprint built' : 'blueprints built', 'with OpenTofu'),
        tile(t.resources, 'resources they made', checked ? `${t.checked} checked` + (t.missing ? `, ${t.missing} missing` : '') : 'not checked yet'),
        tile(t.stacks, t.stacks === 1 ? 'stack' : 'stacks', 'from approved changes'),
        tile(t.environments, t.environments === 1 ? 'environment up' : 'environments up', 'temporary')),
      ...b.blueprints.map(blueprintCard),
      b.stacks.length ? stacksCard(b.stacks) : null,
      b.environments.length ? envsCard(b.environments) : null);
  }

  // One built blueprint: the diagram on the left, what the picked block made on the right.
  function blueprintCard(bp) {
    const groups = byBlock(bp);
    let picked = bp.blocks.find(x => groups[x.id])?.id ?? '';
    const side = h('aside', { class: 'min-w-0', 'aria-label': 'Resources' });
    const drawing = h('div', { class: 'overflow-auto' });

    function drawSide() {
      const block = bp.blocks.find(x => x.id === picked);
      const list = groups[picked] || [];
      mount(side, h('h3', { class: 'h-section mb-1' }, block ? block.name : 'Shared by the blueprint'),
        h('p', { class: 'muted mb-3 text-xs' }, block ? (KINDS[block.kind]?.label + ' · ' + blockFacts(block)) : 'The cluster, roles and lookups more than one block uses.'),
        list.length ? h('ul', { class: 'space-y-2 text-sm', 'data-resources': picked || 'shared' }, list.map(r => {
          const st = r.present === undefined || r.present === null ? 'unchecked' : r.present ? 'present' : 'missing';
          return h('li', { class: 'rounded-lg border border-slate-200 p-2.5 dark:border-slate-800', 'data-address': r.address },
            h('div', { class: 'flex items-center justify-between gap-2' }, h('b', { class: 'min-w-0 truncate' }, typeWords(r.type)),
              h('span', { class: st === 'present' ? 'pill-ok' : st === 'missing' ? 'pill-bad' : 'pill-idle' }, st === 'present' ? 'there' : st === 'missing' ? 'missing' : 'not checked')),
            h('div', { class: 'muted break-all text-xs' }, resourceLabel(r)),
            r.arn && h('div', { class: 'mt-1 flex items-center gap-1' }, h('code', { class: 'min-w-0 truncate text-[11px]' }, r.arn), copyButton(r.arn, 'Copy')),
            h('div', { class: 'muted text-[11px]' }, r.address));
        })) : h('p', { class: 'muted text-sm' }, 'This block made nothing in AWS (it may use something that already existed).'));
    }

    function drawDiagram() {
      const { w, h: ht } = bounds(bp);
      const byId = Object.fromEntries(bp.blocks.map(x => [x.id, x]));
      const ports = linkPorts(bp);
      const svg = h('svg', { width: String(w), height: String(ht), viewBox: `0 0 ${w} ${ht}`, class: 'block h-auto max-w-full min-w-[36rem]', role: 'group', 'aria-label': bp.name + ' as built' });
      for (const l of bp.links) {
        const a = byId[l.from], c = byId[l.to];
        if (!a || !c) continue;
        const d = linkPath(a, c, ports.get(l.from + '>' + l.to)), traffic = a.kind === 'frontdoor', way = flowOf(bp, l);
        svg.append(h('path', { d, fill: 'none', 'stroke-width': '1.75', 'stroke-dasharray': c.kind === 'logs' ? '5 4' : '', class: traffic ? 'stroke-brand-500' : 'stroke-slate-300 dark:stroke-slate-600' }, h('title', {}, linkWords(bp, l))));
        const dots = traffic ? 'stroke-brand-500' : 'stroke-slate-500 dark:stroke-slate-300';
        if (way === 'forward' || way === 'both') svg.append(flowPath(d, dots));
        if (way === 'back' || way === 'both') svg.append(flowPath(d, dots, { back: true }));
      }
      for (const x of bp.blocks.filter(x => x.kind === 'frontdoor')) {
        const y = x.y + BLOCK_H / 2, x0 = Math.max(0, x.x - 56), d = `M${x0} ${y} L${x.x} ${y}`;
        svg.append(h('path', { d, fill: 'none', 'stroke-width': '1.75', class: 'stroke-brand-500' }), flowPath(d, 'stroke-brand-500'),
          h('text', { x: String(x0), y: String(y - 8), class: 'fill-slate-400 text-[10px]' }, 'internet'));
      }
      for (const x of bp.blocks) {
        const rs = groups[x.id] || [], st = checkState(rs), on = x.id === picked, k = KINDS[x.kind] || { label: x.kind };
        const g = h('g', { transform: `translate(${x.x} ${x.y})`, tabindex: '0', role: 'button', 'aria-pressed': String(on), class: 'cursor-pointer focus:outline-none', 'data-block': x.id,
          'aria-label': `${x.name}: ${rs.length} resources, ${STATE_WORDS[st]}` },
          h('rect', { width: String(BLOCK_W), height: String(BLOCK_H), rx: '12', 'stroke-width': on ? '2.5' : '1.25', 'stroke-dasharray': x.kind === 'existing' ? '5 4' : '',
            class: 'fill-white dark:fill-slate-900 ' + (on ? 'stroke-brand-500' : st === 'missing' ? 'stroke-rose-400' : 'stroke-slate-300 dark:stroke-slate-700') }),
          h('circle', { cx: String(BLOCK_W - 14), cy: '16', r: '4', class: STATE_DOT[st] }, h('title', {}, STATE_WORDS[st])),
          h('text', { x: '14', y: '24', class: 'fill-slate-900 text-[13px] font-semibold dark:fill-slate-100' }, fit(x.name, 20)),
          h('text', { x: '14', y: '42', class: 'fill-slate-500 text-[11px] dark:fill-slate-400' }, k.label + (x.kind === 'existing' ? ' · used, not made' : '')),
          h('text', { x: '14', y: '57', class: 'fill-slate-700 text-[11px] font-medium dark:fill-slate-300' }, rs.length ? `${rs.length} ${rs.length === 1 ? 'resource' : 'resources'} in AWS` : 'nothing made'));
        const pick = () => { picked = x.id; drawDiagram(); drawSide(); };
        g.addEventListener('click', pick);
        g.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); pick(); } });
        svg.append(g);
      }
      mount(drawing, svg);
    }

    drawDiagram(); drawSide();
    const shared = groups[''] || [];
    return h('section', { class: 'card mb-6 overflow-hidden', 'data-built': bp.id },
      h('div', { class: 'flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-slate-200 px-5 py-3 dark:border-slate-800' },
        h('h2', { class: 'font-display text-lg font-semibold' }, bp.name),
        h('span', { class: 'muted text-xs' }, `${bp.account} · ${bp.region} · AWS account ${bp.aws_account} · ${bp.resources.length} resources · built ${bp.applies} ${bp.applies === 1 ? 'time' : 'times'}`),
        bp.change && h('span', { class: 'muted text-xs' }, `last approved by ${bp.change.decided_by} ${relTime(bp.change.finished_at)}`),
        h('span', { class: 'flex-1' }),
        h('a', { class: 'btn-ghost btn-sm', href: '#/design/' + bp.id }, 'Open on the canvas')),
      bp.error ? h('div', { class: 'px-5 pt-3' }, errorBox(bp.error)) : null,
      h('div', { class: 'grid gap-4 p-4 lg:grid-cols-[minmax(0,1fr)_20rem]' },
        h('div', { class: 'min-w-0 space-y-3' }, drawing,
          shared.length ? h('button', { type: 'button', class: 'btn-ghost btn-sm', 'data-shared': bp.id, onclick: () => { picked = ''; drawDiagram(); drawSide(); } }, `Shared by the blueprint · ${shared.length}`) : null,
          h('p', { class: 'hint' }, 'Pick a block to see the AWS resources it made. Moving dots show which way things flow, as built (not measured traffic). A green dot means checked and there, red means missing.')),
        side));
  }

  function stacksCard(stacks) {
    return h('section', { class: 'card mb-6 overflow-x-auto', id: 'built-stacks' }, h('h2', { class: 'h-section px-5 pt-4' }, 'Stacks'),
      h('table', { class: 'tbl' }, h('thead', {}, h('tr', {}, ['Stack', 'Account', 'Proposed by', 'Approved by', 'When'].map(x => h('th', {}, x)))),
        h('tbody', {}, stacks.map(s => h('tr', {}, h('td', { class: 'font-medium' }, s.stack), h('td', {}, (s.account || 'primary') + ' · ' + s.region),
          h('td', {}, s.change.proposed_by), h('td', {}, s.change.decided_by || '–'), h('td', { class: 'muted text-sm' }, relTime(s.change.finished_at)))))));
  }

  function envsCard(envs) {
    return h('section', { class: 'card mb-6 overflow-x-auto', id: 'built-envs' }, h('h2', { class: 'h-section px-5 pt-4' }, 'Environments'),
      h('table', { class: 'tbl' }, h('thead', {}, h('tr', {}, ['Environment', 'Template', 'Owner', 'Status', 'Ends'].map(x => h('th', {}, x)))),
        h('tbody', {}, envs.map(e => h('tr', {}, h('td', {}, h('a', { href: '#/environments', class: 'font-medium hover:underline' }, e.name), h('div', { class: 'muted text-xs' }, e.stack)),
          h('td', {}, e.template), h('td', {}, e.owner), h('td', {}, e.status), h('td', { class: 'muted text-sm' }, e.expires_at ? relTime(e.expires_at) : '–'))))));
  }

  await load(false);
  return el;
}
