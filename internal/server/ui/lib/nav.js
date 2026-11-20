// Forgeline web console: the sidebar and the tabs inside each space (design note 048, which replaces the spaces of
// note 029).
//
// No imports and no DOM, so it runs under Node in tests. The sidebar holds seven spaces, named after the things
// people own: Home (what needs me), Agents, Workflows, Delivery (pipelines), Cloud (what should exist and what does),
// Operations (how it is running) and Security. The pages people know are tabs inside a space, each with an icon.
// Setup lives under Settings (settingsnav.js). Routes and role checks are unchanged: a tab is the same address it
// always was, so old links keep working.
//
// A tab with `planned: true` has no page yet. It is shown dimmed and without a link, so a space says what
// is coming without a button that does nothing.

export const SPACES = [
  { id: 'home', label: 'Home', icon: 'home', path: '/', tabs: [
    { path: '/', label: 'Today', icon: 'inbox', min: 'viewer', exact: true },
    { path: '/proposals', label: 'Approvals', icon: 'approval', min: 'viewer', also: ['/pushes/'] },
  ] },
  { id: 'agents', label: 'Agents', icon: 'agents', path: '/agents', tabs: [
    { path: '/agents', label: 'Agents', icon: 'agents', min: 'developer', also: ['/skills/'] },
    { path: '/team', label: 'Teams', icon: 'team', min: 'developer', also: ['/teams/'] },
    { path: '/traces', label: 'Runs and traces', icon: 'trace', min: 'developer' },
    { path: '/library', label: 'Library', icon: 'library', min: 'developer', also: ['/marketplace'] },
    { path: '/ai-use', label: 'Usage and budgets', icon: 'usage', min: 'developer' },
  ] },
  { id: 'workflows', label: 'Workflows', icon: 'workflows', path: '/workflows', tabs: [
    { path: '/workflows', label: 'Workflows', icon: 'workflows', min: 'developer', also: ['/workflow-runs/'] },
    { path: '/schedules', label: 'Schedules', icon: 'schedule', min: 'developer' },
  ] },
  { id: 'delivery', label: 'Delivery', icon: 'delivery', path: '/pipelines', tabs: [
    { path: '/pipelines', label: 'Pipelines', icon: 'pipeline', min: 'viewer', also: ['/runs/'] },
    { path: '/actions', label: 'GitHub Actions', icon: 'github', min: 'developer' },
  ] },
  { id: 'cloud', label: 'Cloud', icon: 'cloud', path: '/design', tabs: [
    { path: '/design', label: 'Canvas', icon: 'design', min: 'developer' },
    { path: '/environments', label: 'Environments', icon: 'environment', min: 'developer' },
    { path: '/blueprints', label: 'Blueprints', icon: 'blueprint', min: 'developer' },
    { path: '/built', label: 'Resources', icon: 'resource', min: 'developer' },
    { path: '/map', label: 'Map', icon: 'map', min: 'viewer' },
  ] },
  { id: 'operations', label: 'Operations', icon: 'operations', path: '/services', tabs: [
    { path: '/services', label: 'Services', icon: 'service', min: 'viewer' },
    { path: '/infrastructure', label: 'Infrastructure', icon: 'infrastructure', min: 'viewer' },
    { path: '/kubernetes', label: 'Kubernetes', icon: 'kubernetes', min: 'viewer' },
    { path: '/missions', label: 'Missions', icon: 'mission', min: 'developer' },
  ] },
  { id: 'security', label: 'Security', icon: 'security', path: '/guard', tabs: [
    { path: '/guard', label: 'Findings', icon: 'findings', min: 'viewer' },
    { path: '/approval-rules', label: 'Approval rules', icon: 'approval', min: 'developer' },
    { path: '/policy-packs', label: 'Policy packs', icon: 'policy', min: 'developer' },
    { path: '/resource-types', label: 'Resource types', icon: 'resource', min: 'developer' },
    { path: '/audit', label: 'Audit log', icon: 'audit', min: 'admin' },
  ] },
];

const RANK = { viewer: 1, developer: 2, admin: 3 };
const allowed = (role, min) => (RANK[role] || 0) >= (RANK[min] || 99);

/** Does this tab own the path? A query string is ignored; `exact` tabs match only themselves. */
export function tabMatches(tab, path) {
  if (!tab.path) return false;
  const p = String(path).split('?')[0];
  if (tab.exact) return p === tab.path;
  return p === tab.path || p.startsWith(tab.path + '/') || (tab.also || []).some(a => p.startsWith(a));
}

/** The tabs of a space this role may see: the ones it may open, plus planned ones (never links). */
export function tabsFor(space, role) {
  return space.tabs.filter(t => t.planned || allowed(role, t.min));
}

/**
 * The spaces this role sees, each with its visible tabs and the address the sidebar links to (its first
 * tab the role may open). A space with nothing the role may open is left out.
 */
export function spacesFor(role) {
  return SPACES.map(s => {
    const tabs = tabsFor(s, role);
    const first = tabs.find(t => !t.planned);
    return first ? { ...s, tabs, path: first.path } : null;
  }).filter(Boolean);
}

/** The space a path belongs to, or null (Settings pages, the account page). */
export function activeSpace(path) {
  for (const s of SPACES) if (s.tabs.some(t => tabMatches(t, path))) return s;
  return null;
}

/** The tab a path belongs to within its space, or null. */
export function activeTab(path) {
  const s = activeSpace(path);
  return s ? s.tabs.find(t => tabMatches(t, path)) || null : null;
}
