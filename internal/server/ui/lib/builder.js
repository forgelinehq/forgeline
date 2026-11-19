// Forgeline web console: the agent builder (design note 045, decision 6). An agent is shown as parts, and one side
// panel with tabs picks what goes in each: Models, Skills, MCP servers, Tools and Guardrails. Pure functions over a
// draft, no DOM, so they run under Node in tests; the server checks every save.

import { groupTools, toolInfo, plainHelp, TIERS } from './tools.js';

export const TABS = ['Models', 'Skills', 'MCP servers', 'Tools', 'Guardrails'];

// Models through the assistant's own connection, by bare name (note 044, part 3).
export const ASSISTANT_MODELS = [
  ['claude-haiku-5-5', 'Claude Haiku 5.5', 'Quick and cheap: routing and short answers'],
  ['claude-sonnet-5-5', 'Claude Sonnet 5.5', 'Strong reasoning for analysis'],
  ['claude-opus-5-5', 'Claude Opus 5.5', 'Most capable of the three; slower'],
  ['claude-fable-5-2', 'Claude Fable 5.2', 'The most capable'],
];

/** A draft from an agent's version (or an empty one for a new agent). */
export function draftFrom(ag, v) {
  return {
    name: ag?.name || '', description: ag?.description || '', instructions: v?.instructions || '', tier: v?.tier || 'T0',
    model: v?.model || '', skills: (v?.skills || []).map(p => ({ id: p.id, n: p.n })), tools: [...(v?.tools || [])], note: '',
  };
}

/** What the server is sent to save the draft as a new version (or a new agent). */
export const draftBody = (d) => ({ name: d.name, description: d.description, instructions: d.instructions, tier: d.tier, model: d.model, note: d.note,
  skills: d.skills.map(p => ({ id: p.id, n: p.n || 0 })), tools: [...d.tools] });

/** Whether the draft differs from what it was made from (the note does not count). */
export function changed(d, base) {
  const a = draftBody(d), b = draftBody(base);
  delete a.note; delete b.note;
  return JSON.stringify(a) !== JSON.stringify(b);
}

const isServerTool = (t) => t.needs === 'mcp-server';

/**
 * One tab of the side panel: groups of items, each with a key, a label, what it does, tags, and whether the draft has
 * it. data: { models (GET /v1/models), skills, tools (GET /v1/ai/tools) }. A query narrows the items; filters are tag
 * texts an item must all carry.
 */
export function pickerGroups(tab, data, d, query = '', filters = []) {
  const q = query.trim().toLowerCase();
  let groups = [];
  if (tab === 'Models') {
    const byProv = new Map();
    for (const m of data.models || []) {
      if (!byProv.has(m.provider)) byProv.set(m.provider, { id: 'p:' + m.provider, label: m.provider_name, kind: m.kind, note: 'Settings › Models and providers', items: [] });
      byProv.get(m.provider).items.push({ key: m.ref, label: m.label, desc: m.ref, on: d.model === m.ref,
        tags: [{ text: m.input_usd_per_mtok || m.output_usd_per_mtok ? `$${m.input_usd_per_mtok} / $${m.output_usd_per_mtok}` : 'no price set', tone: 'info' }] });
    }
    groups = [...byProv.values(), { id: 'assistant', label: 'Through the assistant’s connection', kind: 'anthropic', note: 'Works only while the assistant is on', items: [
      { key: '', label: 'The assistant’s model', desc: 'Whatever the assistant is set to use', on: d.model === '', tags: [] },
      ...ASSISTANT_MODELS.map(([id, label, desc]) => ({ key: id, label, desc, on: d.model === id, tags: [] })),
    ] }];
    // A model the draft names that is no longer offered still shows, so it can be seen and replaced.
    if (d.model && !groups.some(g => g.items.some(i => i.key === d.model))) groups.unshift({ id: 'gone', label: 'No longer offered', kind: 'other', note: 'Pick another', items: [{ key: d.model, label: d.model, desc: 'Not turned on under Models and providers', on: true, tags: [{ text: 'not available', tone: 'warn' }] }] });
  } else if (tab === 'Skills') {
    const pins = Object.fromEntries(d.skills.map(p => [p.id, p.n]));
    groups = [{ id: 'skills', label: 'Skills', kind: 'library', note: 'Agents › Skills, and those installed from the Library', items: (data.skills || []).map(s => {
      const on = s.id in pins, pin = pins[s.id];
      return { key: s.id, label: s.name, desc: s.description || '', on,
        tags: [on && pin && pin < s.current ? { text: `pinned v${pin} · v${s.current} out`, tone: 'warn' } : { text: on && pin ? `v${pin}` : `v${s.current}`, tone: 'info' }] };
    }) }];
  } else if (tab === 'MCP servers') {
    const bySrv = new Map();
    for (const t of (data.tools || []).filter(isServerTool)) {
      if (!bySrv.has(t.server)) bySrv.set(t.server, { id: 's:' + t.server, label: t.server_name || t.server, kind: 'mcp', note: '', items: [] });
      bySrv.get(t.server).items.push({ key: t.name, label: t.tool || t.name, desc: plainHelp(t.help), on: d.tools.includes(t.name), tags: [{ text: 'reads', tone: 'ok' }] });
    }
    groups = [...bySrv.values()].map(g => ({ ...g, note: g.items.length + (g.items.length === 1 ? ' tool' : ' tools') + ' turned on by an administrator' }));
  } else if (tab === 'Tools') {
    groups = groupTools((data.tools || []).filter(t => !isServerTool(t))).map(g => ({ id: 'g:' + g.id, label: g.title, kind: 'forgeline', note: g.note,
      items: g.tools.map(t => ({ key: t.name, label: t.title, desc: plainHelp(t.help), on: d.tools.includes(t.name), tags: toolTags(t) })) }));
  } else if (tab === 'Guardrails') {
    groups = [{ id: 'tier', label: 'How much it may do', kind: 'guard', note: 'Either way it works as the person who asks', radio: true,
      items: TIERS.map(t => ({ key: 'tier:' + t.id, label: t.title, desc: t.text, on: d.tier === t.id, tags: t.id === 'T1' ? [{ text: 'a person approves', tone: 'warn' }] : [{ text: 'reads only', tone: 'ok' }] })) },
    { id: 'always', label: 'Always on', kind: 'guard', note: 'Every agent has these; they cannot be turned off', locked: true, items: ALWAYS.map(([label, desc], i) => ({ key: 'always:' + i, label, desc, on: true, locked: true, tags: [{ text: 'locked', tone: 'info' }] })) }];
  }
  return groups.map(g => ({ ...g, items: g.items.filter(i => (!q || (i.label + ' ' + i.desc + ' ' + g.label).toLowerCase().includes(q)) && filters.every(f => i.tags.some(t => t.text === f))) }))
    .filter(g => g.items.length);
}

const ALWAYS = [
  ['Works as the person who asks', 'With their role, so it can never do more than they could.'],
  ['A different administrator approves changes', 'A change it proposes waits for someone other than the person who asked.'],
  ['What tools return is data', 'Never instructions it must follow, whatever the text says.'],
  ['Its runs are audited', 'Who asked, which version and how many lookups; MCP tool calls and proposals one by one. Model calls are in the providers’ call log.'],
];

function toolTags(t) {
  const out = [];
  out.push(t.asks ? { text: 'a person approves', tone: 'warn' } : t.name === 'prepare_action' ? { text: 'you confirm', tone: 'warn' } : { text: 'reads', tone: 'ok' });
  if (t.needs === 'mcp') out.push({ text: 'needs the AWS MCP Server', tone: 'info' });
  if (t.needs === 'plugins') out.push({ text: 'plugin', tone: 'info' });
  if (t.min && t.min !== 'viewer') out.push({ text: t.min + ' and up', tone: 'info' });
  return out;
}

/** The filters a tab offers: the tag texts its items carry, most common first. */
export function tabFilters(tab, data, d) {
  const n = new Map();
  for (const g of pickerGroups(tab, data, d)) for (const i of g.items) for (const t of i.tags) if (!t.text.startsWith('$') && !/^v\d/.test(t.text) && t.text !== 'locked') n.set(t.text, (n.get(t.text) || 0) + 1);
  return [...n.entries()].sort((a, b) => b[1] - a[1]).map(e => e[0]).slice(0, 4);
}

/** Picks or drops an item of a tab: a model replaces the model; skills and tools toggle; a tier replaces the tier. */
export function toggle(d, tab, key) {
  const out = { ...d, skills: d.skills.map(p => ({ ...p })), tools: [...d.tools] };
  if (tab === 'Models') out.model = key;
  else if (tab === 'Skills') out.skills = out.skills.some(p => p.id === key) ? out.skills.filter(p => p.id !== key) : [...out.skills, { id: key, n: 0 }];
  else if (tab === 'MCP servers' || tab === 'Tools') out.tools = out.tools.includes(key) ? out.tools.filter(x => x !== key) : [...out.tools, key];
  else if (tab === 'Guardrails' && key.startsWith('tier:')) out.tier = key.slice(5);
  return out;
}

/** The footer line of a tab: what the draft has picked there. */
export function tabSummary(tab, data, d) {
  if (tab === 'Models') return d.model ? 'Asks ' + d.model : 'Asks the assistant’s model';
  if (tab === 'Skills') return d.skills.length + (d.skills.length === 1 ? ' skill' : ' skills') + ', each pinned to its version';
  const server = new Set((data.tools || []).filter(isServerTool).map(t => t.name));
  if (tab === 'MCP servers') {
    const mine = (data.tools || []).filter(t => isServerTool(t) && d.tools.includes(t.name));
    const servers = new Set(mine.map(t => t.server)).size;
    return mine.length + (mine.length === 1 ? ' tool' : ' tools') + ' from ' + servers + (servers === 1 ? ' server' : ' servers') + ' · only tools an administrator turned on';
  }
  if (tab === 'Tools') { const n = d.tools.filter(x => !server.has(x)).length; return n + (n === 1 ? ' tool' : ' tools') + ' · pick the fewest it needs'; }
  return (TIERS.find(t => t.id === d.tier) || TIERS[0]).title + ' · 4 rules always on';
}

/** The chips a part shows on the builder: what the draft has in it, by name. */
export function partChips(part, data, d) {
  const server = new Map((data.tools || []).filter(isServerTool).map(t => [t.name, t]));
  if (part === 'Skills') return d.skills.map(p => { const s = (data.skills || []).find(x => x.id === p.id); return { key: p.id, label: (s?.name || p.id) + (p.n ? ' v' + p.n : '') }; });
  if (part === 'MCP servers') return d.tools.filter(x => server.has(x)).map(x => ({ key: x, label: (server.get(x).server_name || server.get(x).server) + ' · ' + (server.get(x).tool || x) }));
  if (part === 'Tools') return d.tools.filter(x => !server.has(x)).map(x => ({ key: x, label: toolInfo(x).title }));
  return [];
}
