// Forgeline web console: what the search box (⌘K) finds and in what order (design note 048, step 4).
//
// No imports of DOM code, so it runs under Node in tests. It finds pages (every tab and Settings entry the person may
// open), things (agents, workflows, teams, skills, pipelines, missions, blueprints, from lists the person may read) and
// a few actions (New agent…). Nothing here decides access: the lists come from the API, which already refused what the
// role may not read, and the pages are the ones the menus show.
import { SPACES, tabsFor } from './nav.js';
import { visibleGroups } from './settingsnav.js';

const RANK = { viewer: 1, developer: 2, admin: 3 };
const atLeast = (role, min) => (RANK[role] || 0) >= (RANK[min] || 99);

/** Every page the role may open: [{ kind: 'page', label, where, path, icon, words }]. */
export function pageEntries(role) {
  const out = [];
  for (const s of SPACES) {
    for (const t of tabsFor(s, role)) {
      if (t.planned || !t.path) continue;
      out.push({ kind: 'page', label: t.label === s.label ? s.label : t.label, where: s.label, path: t.path, icon: t.icon || s.icon, words: s.label });
    }
  }
  for (const g of visibleGroups(role)) {
    for (const i of g.items) {
      if (i.planned || !i.path) continue;
      out.push({ kind: 'page', label: i.label, where: 'Settings › ' + g.title, path: i.path, icon: i.icon || 'gear', words: 'settings ' + (i.note || '') });
    }
  }
  return out;
}

/** Shortcuts that make something new, for the roles that may. */
export function actionEntries(role) {
  return [
    ['New agent', '/agents/new/build', 'developer', 'agents', 'create build'],
    ['New workflow', '/workflows/new/canvas', 'developer', 'workflows', 'create graph flow'],
    ['New pipeline', '/pipelines/new', 'developer', 'pipeline', 'create ci'],
    ['Add an MCP server', '/mcp-servers', 'admin', 'plug', 'connect tools'],
    ['Add a model provider', '/providers', 'developer', 'model', 'llm openai anthropic gemini bedrock'],
  ].filter(([, , min]) => atLeast(role, min))
    .map(([label, path, , icon, words]) => ({ kind: 'action', label, where: 'Create', path, icon, words }));
}

const enc = encodeURIComponent;

/** Things from the API's lists. Each list may be missing (the role may not read it, or it failed). */
export function thingEntries(lists) {
  const out = [];
  const add = (items, f) => { for (const x of items || []) { const e = f(x); if (e) out.push(e); } };
  add(lists.agents, a => a.archived_at ? null : { kind: 'agent', label: a.name, where: 'Agent', path: '/agents/' + enc(a.id), icon: 'agents', words: (a.description || '') + ' ' + a.id });
  add(lists.workflows, w => w.archived_at ? null : { kind: 'workflow', label: w.name, where: 'Workflow', path: '/workflows/' + enc(w.id), icon: 'workflows', words: (w.description || '') + ' ' + w.id });
  add(lists.teams, t => ({ kind: 'team', label: t.name, where: 'Team', path: '/teams/' + enc(t.id), icon: 'team', words: (t.description || t.goal || '') + ' ' + t.id }));
  add(lists.skills, s => ({ kind: 'skill', label: s.name, where: 'Skill', path: '/skills/' + enc(s.id), icon: 'skills', words: (s.description || '') + ' ' + s.id }));
  add(lists.pipelines, p => ({ kind: 'pipeline', label: p.name, where: 'Pipeline', path: '/pipelines/' + enc(p.id), icon: 'pipeline', words: (p.repo || '') + ' ' + p.id }));
  add(lists.missions, m => ({ kind: 'mission', label: m.goal || m.id, where: 'Mission', path: '/missions/' + enc(m.id), icon: 'mission', words: (m.agent_name || '') + ' ' + m.id }));
  add(lists.blueprints, b => ({ kind: 'blueprint', label: b.name, where: 'Blueprint', path: '/design/' + enc(b.id), icon: 'blueprint', words: (b.description || '') + ' ' + b.id }));
  return out.filter(e => e.label);
}

const norm = (s) => String(s || '').toLowerCase().normalize('NFKD').replace(/[̀-ͯ]/g, '');

/** Whether every letter of q appears in t in order ("wfs" in "workflows"). */
function subsequence(q, t) {
  let i = 0;
  for (const c of t) if (c === q[i]) i++;
  return i === q.length;
}

/** How well an entry matches, 0 for not at all. Every word of the query must match somewhere. */
export function score(entry, query) {
  const words = norm(query).split(/\s+/).filter(Boolean);
  if (!words.length) return 0;
  const label = norm(entry.label), rest = norm(entry.where + ' ' + (entry.words || ''));
  let total = 0;
  for (const w of words) {
    let s = 0;
    if (label === w) s = 100;
    else if (label.startsWith(w)) s = 80;
    else if (label.split(/[^a-z0-9]+/).some(x => x.startsWith(w))) s = 60;
    else if (label.includes(w)) s = 40;
    else if (rest.split(/[^a-z0-9]+/).some(x => x.startsWith(w))) s = 20;
    else if (w.length >= 3 && subsequence(w, label)) s = 10;
    if (!s) return 0;
    total += s;
  }
  return total / words.length;
}

const ORDER = { page: 0, action: 1, agent: 2, workflow: 3, team: 4, pipeline: 5, skill: 6, mission: 7, blueprint: 8 };

/** The best matches, best first; with no query, the pages and actions in menu order. */
export function search(entries, query, limit = 12) {
  if (!norm(query).trim()) return entries.filter(e => e.kind === 'page' || e.kind === 'action').slice(0, limit);
  return entries.map(e => ({ e, s: score(e, query) })).filter(x => x.s > 0)
    .sort((a, b) => b.s - a.s || ORDER[a.e.kind] - ORDER[b.e.kind] || a.e.label.length - b.e.label.length)
    .slice(0, limit).map(x => x.e);
}
