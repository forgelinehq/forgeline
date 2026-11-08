// Forgeline web console: the Settings area's menu (design note 025).
//
// No imports and no DOM, so it runs under Node in tests. Setup pages live here, apart from the
// daily work in the sidebar. An entry with `planned: true` has no page yet: it is shown, dimmed and
// without a link, so the menu says what is coming without a button that does nothing.

export const SETTINGS = [
  { title: 'Workspace', items: [
    { path: '/people', label: 'People and roles', note: 'Who can sign in and what each person may do', min: 'admin' },
    { path: '/sso', label: 'Sign-in (SSO)', note: 'Let people sign in with the company login, with roles from their groups', min: 'admin' },
  ] },
  { title: 'Connections', items: [
    { path: '/integrations', label: 'Connections', note: 'GitHub, AWS, Docker, Kubernetes, Claude, and what the AI may do with each', min: 'developer', also: ['/integrations/'] },
    { path: '/secrets', label: 'Secrets', note: 'Keys and tokens, stored encrypted and never shown again', min: 'developer' },
  ] },
  { title: 'AI', items: [
    { path: '/integrations/claude/settings', label: 'Models', note: 'Which model answers the assistant and agents', min: 'developer' },
    { path: '/memory', label: 'Memory', note: 'What the assistant remembers about you and this system, and your conversations', min: 'developer' },
    { label: 'Budgets and limits', note: 'How much each agent may spend', planned: true },
  ] },
  { title: 'Security', items: [
    { path: '/audit', label: 'Audit log', note: 'Who did what, including refusals and failed sign-ins', min: 'admin' },
    { path: '/approval-rules', label: 'Approval rules', note: 'Which changes need two approvals, and who may approve them', min: 'developer' },
  ] },
  { title: 'Developer', items: [
    { path: '/account', label: 'API tokens', note: 'Personal tokens for AI tools that connect over MCP', min: 'viewer' },
  ] },
];

const RANK = { viewer: 1, developer: 2, admin: 3 };
const atLeast = (role, min) => (RANK[role] || 0) >= (RANK[min] || 99);

/** The menu a person sees: pages they may open, plus planned entries. A group with nothing in it is left out. */
export function visibleGroups(role) {
  return SETTINGS.map(g => ({ title: g.title, items: g.items.filter(i => i.planned || atLeast(role, i.min)) }))
    .filter(g => g.items.some(i => !i.planned));
}

/** Pages that belong to Settings, so the sidebar can highlight the gear and the pages can show the menu. */
export function isSettingsPath(path) {
  const p = String(path).split('?')[0];
  if (p === '/settings') return true;
  return SETTINGS.some(g => g.items.some(i => i.path && i.path !== '/account' && (p === i.path || p.startsWith(i.path + '/'))));
}

/** The menu entry for a path (the longest matching one), or null. */
export function activeItem(path) {
  const p = String(path).split('?')[0];
  let best = null;
  for (const g of SETTINGS) for (const i of g.items) {
    if (!i.path) continue;
    if (p === i.path || p.startsWith(i.path + '/')) { if (!best || i.path.length > best.path.length) best = i; }
  }
  return best;
}

/** Can this role open Settings at all? Viewers have nothing to set up. */
export function canOpenSettings(role) {
  return atLeast(role, 'developer');
}
