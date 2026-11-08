// Forgeline web console: the sidebar and the tabs inside each space (design note 029, frame 1).
//
// No imports and no DOM, so it runs under Node in tests. The sidebar holds five spaces, named after the
// job a person is doing: Home (what needs me), Build (what should exist), Ship (getting code out), Team
// (the agents doing the work) and Watch (how it is running). The pages people know are tabs inside a
// space. Setup lives under Settings (settingsnav.js). Routes and role checks are unchanged: a tab is the
// same address it always was, so old links keep working.
//
// A tab with `planned: true` has no page yet. It is shown dimmed and without a link, so a space says what
// is coming without a button that does nothing.

export const SPACES = [
  { id: 'home', label: 'Home', icon: 'home', path: '/', tabs: [
    { path: '/', label: 'Today', min: 'viewer', exact: true },
    { path: '/proposals', label: 'Approvals', min: 'viewer' },
  ] },
  { id: 'build', label: 'Build', icon: 'grid', path: '/environments', tabs: [
    { path: '/design', label: 'Design', min: 'developer' },
    { path: '/environments', label: 'Environments', min: 'developer' },
    { path: '/blueprints', label: 'Blueprints', min: 'developer' },
  ] },
  { id: 'ship', label: 'Ship', icon: 'flow', path: '/pipelines', tabs: [
    { path: '/pipelines', label: 'Pipelines', min: 'viewer', also: ['/runs/'] },
    { path: '/actions', label: 'GitHub Actions', min: 'developer' },
  ] },
  { id: 'team', label: 'Team', icon: 'sparkle', path: '/team', tabs: [
    { path: '/team', label: 'Team', min: 'developer' },
    { path: '/agents', label: 'Agents', min: 'developer', also: ['/skills/', '/workflows/', '/workflow-runs/'] },
    { path: '/missions', label: 'Missions', min: 'developer' },
    { path: '/marketplace', label: 'Marketplace', min: 'developer' },
  ] },
  { id: 'watch', label: 'Watch', icon: 'eye', path: '/map', tabs: [
    { path: '/map', label: 'Map', min: 'viewer' },
    { path: '/infrastructure', label: 'Infrastructure', min: 'viewer' },
    { path: '/kubernetes', label: 'Kubernetes', min: 'viewer' },
    { path: '/guard', label: 'Security', min: 'viewer' },
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
