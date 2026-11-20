// Agents › Runs and traces (design note 049): every assistant chat, agent run and workflow run as a trace. The list
// says what started it, who, how long, what it cost and whether it failed; a trace opens as a waterfall of its spans
// (agent runs, model calls, tool calls, workflow nodes) on one time scale, with a span's details beside it.
import { h, icon, mount, pageHeader, relTime, absTime, emptyState, spinner, errorBox, pill } from '../lib/dom.js';
import { get } from '../lib/api.js';
import { navigate } from '../lib/router.js';
import { spanTree, spanLook, fmtMillis, fmtTokens, fmtCost, KIND_LABEL } from '../lib/traceview.js';

const FILTERS = [['', 'All'], ['agent', 'Agents'], ['workflow', 'Workflows'], ['team', 'Teams'], ['mission', 'Missions'], ['pipeline', 'Pipelines'], ['assistant', 'Assistant'], ['failed', 'Failed']];

export async function tracesPage({ params, query }) {
  if (params && params.id) return traceDetail(params.id);
  const el = h('div', {});
  let filter = (query && query.filter) || '';
  async function load() {
    const q = filter === 'failed' ? '?failed=1' : filter ? '?kind=' + filter : '';
    let r;
    try { r = await get('/v1/traces' + q); } catch (e) { return mount(el, errorBox(e.message)); }
    mount(el, pageHeader('Runs and traces', 'Every assistant chat, agent run, workflow, team run, mission and pipeline run, step by step: the model calls and tools it used, how long each took and what it cost. Prompts and answers are kept only if an administrator turned that on under Settings › OpenTelemetry.'),
      h('div', { class: 'mb-4 flex flex-wrap gap-1.5', id: 'trace-filters' }, FILTERS.map(([v, l]) => h('button', { type: 'button', 'aria-pressed': String(filter === v), 'data-filter': v,
        class: 'rounded-full border px-3 py-1 text-sm ' + (filter === v ? 'border-brand-400 bg-brand-50 text-brand-800 dark:border-brand-500/60 dark:bg-brand-500/10 dark:text-brand-200' : 'border-slate-200 text-slate-600 hover:border-slate-300 dark:border-slate-700 dark:text-slate-300'),
        onclick: () => { filter = v; load(); } }, l))),
      r.traces.length ? h('div', { class: 'card overflow-x-auto' }, h('table', { class: 'tbl', id: 'trace-list' },
        h('thead', {}, h('tr', {}, ['Run', 'Who', 'When', 'Took', 'Model calls', 'Tools', 'Tokens', 'Cost', ''].map(x => h('th', {}, x)))),
        h('tbody', {}, r.traces.map(t => h('tr', { class: 'cursor-pointer', 'data-trace': t.id, onclick: () => navigate('/traces/' + t.id) },
          h('td', {}, h('div', { class: 'flex items-center gap-2.5' }, h('span', { class: 'flex size-8 flex-none items-center justify-center rounded-lg ' + spanLook({ kind: t.kind }).tile }, icon(spanLook({ kind: t.kind }).icon, 'size-4')),
            h('div', { class: 'min-w-0' }, h('div', { class: 'truncate font-medium' }, t.name.replace(/^(invoke_agent|invoke_workflow|team|mission|pipeline) /, '')), h('div', { class: 'muted text-xs' }, KIND_LABEL[t.kind] || t.kind)))),
          h('td', { class: 'text-sm' }, t.who || h('span', { class: 'muted' }, t.kind === 'pipeline' ? 'pipeline' : 'schedule')),
          h('td', { class: 'muted whitespace-nowrap text-sm', title: absTime(t.started) }, relTime(t.started)),
          h('td', { class: 'whitespace-nowrap text-sm tabular-nums' }, fmtMillis(t.millis)),
          h('td', { class: 'text-sm tabular-nums' }, String(t.model_calls)),
          h('td', { class: 'text-sm tabular-nums' }, String(t.tool_calls)),
          h('td', { class: 'whitespace-nowrap text-sm tabular-nums' }, fmtTokens(t.input_tokens + t.output_tokens)),
          h('td', { class: 'whitespace-nowrap text-sm tabular-nums' }, fmtCost(t.cost_usd)),
          h('td', {}, t.failed ? pill('failed', 'Failed') : pill('succeeded', 'OK')))))))
        : emptyState('trace', filter ? 'Nothing here yet' : 'No traces yet', 'Talk to an agent, ask the assistant or run a workflow; each run shows up here with its model calls and tools.'));
  }
  mount(el, spinner());
  await load();
  return el;
}

async function traceDetail(id) {
  const el = h('div', {});
  let d;
  try { d = await get('/v1/traces/' + encodeURIComponent(id)); } catch (e) { return mount(el, errorBox(e.message)); }
  const rows = spanTree(d.spans);
  const t0 = new Date(d.started).getTime(), total = Math.max(1, new Date(d.ended).getTime() - t0);
  const side = h('aside', { class: 'card card-pad h-fit space-y-3 lg:sticky lg:top-20', id: 'span-detail' });
  let picked = rows[0]?.span.span_id;

  const showSpan = (sp) => {
    picked = sp.span_id;
    const look = spanLook(sp);
    const attrs = Object.entries(sp.attrs || {}).sort(([a], [b]) => a.localeCompare(b));
    mount(side,
      h('div', { class: 'flex items-start gap-2.5' }, h('span', { class: 'flex size-9 flex-none items-center justify-center rounded-lg ' + look.tile }, icon(look.icon, 'size-4')),
        h('div', { class: 'min-w-0' }, h('h2', { class: 'break-words font-semibold' }, sp.name), h('p', { class: 'muted text-xs' }, look.label + ' · ' + fmtMillis(new Date(sp.end) - new Date(sp.start))))),
      sp.error ? errorBox('Failed: ' + sp.error) : null,
      h('table', { class: 'w-full text-xs', id: 'span-attrs' }, h('tbody', {}, attrs.map(([k, v]) => h('tr', { class: 'border-t border-slate-100 dark:border-slate-800' },
        h('td', { class: 'muted py-1.5 pr-3 align-top font-mono' }, k), h('td', { class: 'break-all py-1.5 align-top font-mono' }, String(v)))))),
      h('p', { class: 'muted text-[11px]' }, 'Span ' + sp.span_id + (sp.parent_id ? ' · parent ' + sp.parent_id : '')));
    for (const r of el.querySelectorAll('[data-span]')) r.setAttribute('aria-selected', String(r.dataset.span === picked));
  };

  const waterfall = h('div', { class: 'card overflow-x-auto', id: 'waterfall' },
    h('div', { class: 'grid min-w-[40rem] grid-cols-[minmax(14rem,2fr)_3fr_4.5rem] gap-x-3 border-b border-slate-200 px-4 py-2 text-[11px] font-semibold uppercase tracking-wide text-slate-500 dark:border-slate-800' },
      h('span', {}, 'Step'), h('span', {}, 'When, on one scale'), h('span', { class: 'text-right' }, 'Took')),
    rows.map(({ span: sp, depth }) => {
      const look = spanLook(sp);
      const left = (new Date(sp.start).getTime() - t0) / total * 100, width = Math.max(0.6, (new Date(sp.end) - new Date(sp.start)) / total * 100);
      const bar = h('div', { class: 'absolute top-1/2 h-2.5 -translate-y-1/2 rounded-full ' + (sp.error ? 'bg-rose-500' : look.bar) });
      bar.style.left = Math.min(left, 99.4) + '%';
      bar.style.width = Math.min(width, 100 - Math.min(left, 99.4)) + '%';
      const indent = h('span', { class: 'flex-none' });
      indent.style.width = (depth * 14) + 'px';
      return h('button', { type: 'button', 'data-span': sp.span_id, onclick: () => showSpan(sp),
        class: 'grid w-full min-w-[40rem] grid-cols-[minmax(14rem,2fr)_3fr_4.5rem] items-center gap-x-3 border-b border-slate-100 px-4 py-1.5 text-left hover:bg-slate-50 aria-selected:bg-brand-50/70 dark:border-slate-800/60 dark:hover:bg-slate-800/40 dark:aria-selected:bg-brand-500/10' },
        h('span', { class: 'flex min-w-0 items-center gap-1.5' }, indent, h('span', { class: 'flex size-6 flex-none items-center justify-center rounded-md ' + look.tile }, icon(look.icon, 'size-3.5')),
          h('span', { class: 'truncate text-sm' + (sp.error ? ' text-rose-700 dark:text-rose-300' : '') }, sp.name)),
        h('span', { class: 'relative h-6' }, h('span', { class: 'absolute inset-x-0 top-1/2 h-px bg-slate-200 dark:bg-slate-800' }), bar),
        h('span', { class: 'text-right text-xs tabular-nums text-slate-500' }, fmtMillis(new Date(sp.end) - new Date(sp.start))));
    }));

  const stat = (label, value) => h('div', {}, h('div', { class: 'muted text-[11px] font-semibold uppercase tracking-wide' }, label), h('div', { class: 'text-sm font-medium tabular-nums' }, value));
  const look = spanLook({ kind: d.kind });
  mount(el,
    h('nav', { class: 'mb-2 text-sm', 'aria-label': 'Breadcrumb' }, h('a', { href: '#/traces', class: 'muted hover:underline' }, 'Runs and traces'), h('span', { class: 'muted' }, ' › '), h('span', { class: 'font-mono text-xs' }, d.id)),
    h('div', { class: 'mb-4 flex flex-wrap items-start gap-3' }, h('span', { class: 'flex size-11 flex-none items-center justify-center rounded-xl ' + look.tile }, icon(look.icon, 'size-5')),
      h('div', { class: 'min-w-0 flex-1' }, h('h1', { class: 'h-page' }, d.name.replace(/^(invoke_agent|invoke_workflow|team|mission|pipeline) /, '')),
        h('p', { class: 'muted text-sm' }, (KIND_LABEL[d.kind] || d.kind) + (d.who ? ' · ' + d.who : '') + ' · ' + absTime(d.started))),
      d.failed ? pill('failed', 'Failed') : pill('succeeded', 'OK')),
    h('div', { class: 'card card-pad mb-4 grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6', id: 'trace-stats' },
      stat('Took', fmtMillis(d.millis)), stat('Model calls', String(d.model_calls)), stat('Tool calls', String(d.tool_calls)),
      stat('Tokens in', fmtTokens(d.input_tokens)), stat('Tokens out', fmtTokens(d.output_tokens)), stat('Cost', fmtCost(d.cost_usd))),
    d.dropped ? h('p', { class: 'muted mb-3 text-xs' }, d.dropped + ' more spans were not kept: a trace keeps its first 2,000.') : null,
    h('div', { class: 'grid gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]' }, h('div', { class: 'min-w-0' }, waterfall), side));
  if (rows.length) showSpan(rows[0].span);
  return el;
}
