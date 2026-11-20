// Forgeline web console: how a trace is drawn (design note 049). No DOM, so it runs under Node in tests.

export const KIND_LABEL = { agent: 'Agent run', workflow: 'Workflow run', assistant: 'Assistant chat', model: 'Model call' };

/** Spans in the order to draw them: each parent followed by its children, oldest first, with how deep each is. */
export function spanTree(spans) {
  const list = [...(spans || [])].sort((a, b) => new Date(a.start) - new Date(b.start));
  const ids = new Set(list.map(s => s.span_id));
  const kids = new Map();
  const roots = [];
  for (const s of list) {
    if (s.parent_id && ids.has(s.parent_id)) {
      if (!kids.has(s.parent_id)) kids.set(s.parent_id, []);
      kids.get(s.parent_id).push(s);
    } else roots.push(s);
  }
  const out = [];
  const walk = (s, depth) => { out.push({ span: s, depth }); for (const c of kids.get(s.span_id) || []) walk(c, depth + 1); };
  for (const r of roots) walk(r, 0);
  return out;
}

/** What a span is, in words, with its icon and colours: by its GenAI operation, a workflow node, or a trace's kind. */
export function spanLook(s) {
  const op = s.attrs && s.attrs['gen_ai.operation.name'];
  const node = s.attrs && s.attrs['forgeline.node.kind'];
  const kind = s.kind && !['internal', 'client'].includes(s.kind) ? s.kind : '';
  if (op === 'chat' || kind === 'model') return { label: 'Model call', icon: 'model', tile: 'bg-violet-100 text-violet-700 dark:bg-violet-500/15 dark:text-violet-300', bar: 'bg-violet-500' };
  if (op === 'execute_tool') return { label: 'Tool call', icon: 'tool', tile: 'bg-amber-100 text-amber-800 dark:bg-amber-500/15 dark:text-amber-300', bar: 'bg-amber-500' };
  if (op === 'invoke_workflow' || kind === 'workflow') return { label: 'Workflow', icon: 'workflows', tile: 'bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300', bar: 'bg-sky-500' };
  if (node) return { label: 'Node (' + node + ')', icon: { agent: 'agents', lookup: 'lookup', decide: 'decide', ask: 'ask', end: 'end', start: 'start', merge: 'merge' }[node] || 'next', tile: 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300', bar: 'bg-slate-400' };
  if (kind === 'assistant') return { label: 'Assistant', icon: 'sparkle', tile: 'bg-brand-50 text-brand-700 dark:bg-brand-500/15 dark:text-brand-200', bar: 'bg-brand-500' };
  return { label: 'Agent run', icon: 'agents', tile: 'bg-brand-50 text-brand-700 dark:bg-brand-500/15 dark:text-brand-200', bar: 'bg-brand-500' };
}

export function fmtMillis(ms) {
  ms = Number(ms) || 0;
  if (ms < 1000) return Math.round(ms) + ' ms';
  if (ms < 60000) return (ms / 1000).toFixed(ms < 10000 ? 1 : 0) + ' s';
  const m = Math.floor(ms / 60000), s = Math.round((ms % 60000) / 1000);
  if (m < 60) return m + ' min' + (s ? ' ' + s + ' s' : '');
  const hr = Math.floor(m / 60);
  return hr + ' h ' + (m % 60) + ' min';
}

export function fmtTokens(n) {
  n = Number(n) || 0;
  if (n >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, '') + 'M';
  if (n >= 1e3) return (n / 1e3).toFixed(1).replace(/\.0$/, '') + 'k';
  return String(n);
}

export function fmtCost(usd) {
  usd = Number(usd) || 0;
  if (!usd) return '—';
  return usd < 0.01 ? '<$0.01' : '$' + usd.toFixed(2);
}
