// Forgeline web console: the logic behind the integrations hub (design note 025).
//
// No imports and no DOM, so it runs under Node in tests. It filters, sorts and words what
// `GET /v1/integrations` returns. It decides nothing about a system's state: the server does.

export const CATEGORIES = ['Source and CI', 'Cloud', 'AI infrastructure', 'Containers', 'AI', 'Observe and notify'];

/** Built systems first, worst first (something to fix is what an administrator came for); planned ones by build order. */
const RANK = { attention: 0, connected: 1, detected: 2, off: 3, next: 4, later: 5 };

export const isPlanned = (it) => it.status === 'next' || it.status === 'later';

export function statusText(it) {
  switch (it.status) {
    case 'connected': return 'Connected';
    case 'attention': return 'Needs attention';
    case 'detected': return 'Found on this server';
    case 'off': return 'Not connected';
    case 'next': return 'Coming soon · next';
    case 'later': return 'Coming soon · later';
    default: return it.status;
  }
}

/** The pill class for a status. The classes are defined in web/tailwind.css. */
export function statusPill(status) {
  return { connected: 'pill-ok', attention: 'pill-wait', detected: 'pill-run', off: 'pill-idle', next: 'pill-idle', later: 'pill-idle' }[status] || 'pill-idle';
}

export function tierText(tier) {
  return { 'built-in': 'Built in', verified: 'Verified', community: 'Community' }[tier] || tier;
}

export function readOnlyText(it) {
  if (it.read_only_by === 'provider') return 'Read-only is enforced by the provider. ' + (it.read_only_note || '');
  if (it.read_only_by === 'forgeline') return 'Read-only is enforced by Forgeline alone. ' + (it.read_only_note || '');
  return it.read_only_note || '';
}

/** Does the integration pass the status filter ('all', a status, or 'soon' for both planned ones), category and search? */
export function matches(it, { status = 'all', category = 'all', query = '' } = {}) {
  if (status !== 'all' && !(status === 'soon' ? isPlanned(it) : it.status === status)) return false;
  if (category !== 'all' && it.category !== category) return false;
  const q = String(query).trim().toLowerCase();
  if (!q) return true;
  return [it.name, it.summary, it.category, it.plan, it.found].some(v => v && String(v).toLowerCase().includes(q));
}

/** Groups into categories in a fixed order, sorted inside each. A category with nothing in it is left out. */
export function group(list) {
  const known = new Set(CATEGORIES);
  const cats = [...CATEGORIES, ...[...new Set(list.map(i => i.category))].filter(c => !known.has(c))];
  return cats.map(c => ({
    category: c,
    items: list.filter(i => i.category === c).sort((a, b) => (RANK[a.status] ?? 9) - (RANK[b.status] ?? 9) || (a.order || 0) - (b.order || 0) || a.name.localeCompare(b.name)),
  })).filter(g => g.items.length);
}

export function counts(list) {
  const c = { connected: 0, attention: 0, detected: 0, planned: 0, off: 0 };
  for (const it of list) { if (isPlanned(it)) c.planned++; else c[it.status] = (c[it.status] || 0) + 1; }
  return c;
}

export function summaryLine(list) {
  const c = counts(list);
  const parts = [`${c.connected} connected`, `${c.attention} need${c.attention === 1 ? 's' : ''} attention`];
  if (c.detected) parts.push(`${c.detected} found on this server`);
  parts.push(`${c.planned} coming soon`);
  return parts.join(' · ');
}

/** The short logo text on a card: the first letters of the first two words, upper case. */
export function logo(name) {
  const w = String(name).replace(/["'()]/g, '').split(/[\s,]+/).filter(x => x && !['and', 'the', '&'].includes(x.toLowerCase()));
  return ((w[0] || '?')[0] + (w[1] ? w[1][0] : (w[0] || '').slice(1, 2))).toUpperCase();
}

/** One sentence about a finished test. */
export function testSummary(res) {
  const parts = [`${res.passed} passed`];
  if (res.warned) parts.push(`${res.warned} to look at`);
  if (res.failed) parts.push(`${res.failed} failed`);
  parts.push(`took ${(res.took_ms / 1000).toFixed(1)} s`);
  if (!res.changed) parts.push('created nothing, changed nothing');
  return parts.join(' · ');
}

/** The integrations that need attention, for the strip at the top of the hub. */
export function attentionList(list) {
  return list.filter(i => i.status === 'attention');
}

/** Plain words for an audit action, for an integration's Activity tab. Unknown actions fall back to their own name. */
export function activityLabel(action) {
  const known = {
    'integration.test': 'Test run', 'integration.github.test': 'Test run', 'integration.assistant.test': 'Test run',
    'integration.gcp.update': 'Connection changed', 'integration.azure.update': 'Connection changed', 'integration.aws.update': 'Connection changed',
    'integration.awsmcp.update': 'Turned on or off', 'integration.assistant.update': 'Settings changed', 'integration.github.update': 'Settings changed',
    'integration.github.delete': 'Removed', 'integration.github.app.start': 'App creation started', 'integration.github.app.install': 'App installed',
    'integration.aws.sso.start': 'Sign-in started', 'integration.aws.sso.signin': 'Signed in', 'integration.aws.sso.signout': 'Signed out', 'integration.aws.sso.denied': 'Sign-in denied',
    'integration.aws.account.add': 'Account added', 'integration.aws.account.remove': 'Account removed', 'integration.aws.account.writes': 'Changes allowed or stopped for an account',
    'k8s.cluster.add': 'Cluster added', 'k8s.cluster.delete': 'Cluster removed',
  };
  return known[action] || String(action).replace(/^integration\./, '').replace(/[._]/g, ' ');
}

const GCP_HINT = 'projects/my-project, folders/1234567890 or organizations/1234567890';
const GCP_SCOPE = /^(projects|folders|organizations)\/[A-Za-z0-9][A-Za-z0-9._:-]{0,62}$/;
const GUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

/** The same check the server makes, so a typo is caught before a round trip. The server stays the authority. */
export function normalizeGcpScope(input) {
  let s = String(input == null ? '' : input).trim();
  if (!s) return { error: 'Give a project, folder or organization, such as ' + GCP_HINT + '.' };
  if (!s.includes('/')) s = 'projects/' + s;
  if (!GCP_SCOPE.test(s)) return { error: 'That is not a scope Google understands. Use ' + GCP_HINT + '.' };
  return { scope: s };
}

/** Subscription ids: lower-cased, no repeats, sorted, each a GUID, at most 20. Empty means every subscription the login can see. */
export function normalizeAzureSubs(list) {
  const out = [];
  for (const x of list || []) {
    const v = String(x).trim().toLowerCase();
    if (!v || out.includes(v)) continue;
    if (!GUID.test(v)) return { error: `"${v.slice(0, 40)}" is not a subscription id.` };
    out.push(v);
  }
  if (out.length > 20) return { error: 'Choose at most 20 subscriptions, or none to read every subscription the login can see.' };
  return { subs: out.sort() };
}

/**
 * What the Access tab shows for one tool. A read tool is Allow, Needs approval (a person present approves each
 * call; an agent run has nobody, so it is refused there) or Blocked. A tool that asks for a change is never
 * Allow: when it is not blocked it is a proposal, which the tab calls "Needs approval".
 */
export function toolChoice(tool) {
  const write = tool.kind === 'write';
  const shown = tool.level === 'block' ? 'block' : write ? 'ask' : tool.level === 'ask' ? 'ask' : 'allow';
  return {
    shown,
    options: [
      { key: 'allow', label: 'Allow', enabled: !write, why: write ? 'A change is always a proposal that a different administrator approves, so it cannot be allowed to run on its own.' : '' },
      { key: 'ask', label: 'Needs approval', enabled: true, why: write ? '' : 'A person present approves each call. Where nobody is present (an agent run, an MCP client) it is refused.' },
      { key: 'block', label: 'Blocked', enabled: true, why: '' },
    ],
  };
}

/**
 * The request body after one click. Blocks and "needs approval" are stored; the rest is left out (a tool that is
 * left out is allowed). A tool that asks for a change is never sent as "allow" or "ask", because the server
 * refuses the first and the second is what it already is.
 */
export function policyRequest(tools, name, key) {
  const out = {};
  for (const t of tools) {
    const level = t.name === name ? key : t.level;
    if (level === 'block') out[t.name] = 'block';
    else if (level === 'ask' && t.kind !== 'write') out[t.name] = 'ask';
  }
  return { tools: out };
}

export function agentStateText(state) {
  return { allowed: 'Allowed', blocked: 'Blocked', not_listed: 'Not listed', needs_person: 'Needs a person' }[state] || state;
}

/**
 * The Connections page's left column (design note 029, frame 2): what is connected (worst first), logins
 * found on the server, and everything else, which lives behind "Add a connection".
 */
export function connectionGroups(list) {
  const sorted = [...(list || [])].sort((a, b) => (RANK[a.status] ?? 9) - (RANK[b.status] ?? 9) || a.name.localeCompare(b.name));
  return {
    connected: sorted.filter(i => i.status === 'connected' || i.status === 'attention'),
    found: sorted.filter(i => i.status === 'detected'),
    rest: sorted.filter(i => i.status === 'off' || isPlanned(i)),
  };
}

/** Which connection the page opens on: the one asked for, else the first connected one, else none (the add view). */
export function defaultConnection(list, wanted) {
  if (wanted && (list || []).some(i => i.id === wanted)) return wanted;
  const g = connectionGroups(list);
  return g.connected.length ? g.connected[0].id : null;
}

/** The levels a person can give an AWS service, in words. */
export const SERVICE_LEVELS = [
  { key: 'off', label: 'Off', text: 'The AI cannot see it' },
  { key: 'read', label: 'Look only', text: 'The AI can read it, never propose a change' },
  { key: 'change', label: 'Change, with approval', text: 'The AI can propose changes; a different administrator approves each one' },
];

/** The body of PUT /v1/integrations/aws/services after setting one service to a level. */
export function serviceLevelsRequest(services, id, level) {
  const lvl = (s) => (s.id === id ? level : s.level || (s.on === false ? 'off' : 'change'));
  return {
    off: (services || []).filter(s => lvl(s) === 'off').map(s => s.id),
    read_only: (services || []).filter(s => lvl(s) === 'read').map(s => s.id),
  };
}
