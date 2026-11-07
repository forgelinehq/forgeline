// Forgeline web console: the sidebar (design note 029).
//
// No imports and no DOM, so it runs under Node in tests. The sidebar is grouped by the job a person
// is doing: Home (what needs me), Ship (getting code out), Build (what should exist), Team (the
// agents doing the work) and Watch (how it is running). Setup pages live under Settings
// (settingsnav.js). The routes and role checks are the same as before; only the grouping changed.

export const NAV = [ // `group` is the sidebar heading; the Home entries have none and sit at the top
  { path: '/', label: 'Home', icon: 'home', min: 'viewer' },
  { path: '/proposals', label: 'Approvals', icon: 'check', min: 'viewer' },
  { group: 'Ship', path: '/pipelines', label: 'Pipelines', icon: 'flow', min: 'viewer', also: ['/pipelines/', '/runs/'] },
  { group: 'Ship', path: '/actions', label: 'GitHub Actions', icon: 'bolt', min: 'developer' },
  { group: 'Build', path: '/environments', label: 'Environments', icon: 'clock', min: 'developer' },
  { group: 'Team', path: '/agents', label: 'Agents', icon: 'sparkle', min: 'developer', also: ['/agents/', '/skills/', '/workflows/', '/workflow-runs/'] },
  { group: 'Team', path: '/marketplace', label: 'Marketplace', icon: 'plug', min: 'developer' },
  { group: 'Watch', path: '/infrastructure', label: 'Infrastructure', icon: 'server', min: 'viewer' },
  { group: 'Watch', path: '/kubernetes', label: 'Kubernetes', icon: 'box', min: 'viewer', also: ['/kubernetes/'] },
  { group: 'Watch', path: '/guard', label: 'Guard', icon: 'shield', min: 'viewer' },
];

const RANK = { viewer: 1, developer: 2, admin: 3 };

/** The entries a role may open, in order. */
export function navFor(role) {
  const r = RANK[role] || 0;
  return NAV.filter(i => r >= (RANK[i.min] || 99));
}

/** Is this entry the current page? A query string is ignored. */
export function isActivePath(item, path) {
  const p = String(path).split('?')[0];
  if (item.path === '/') return p === '/';
  return p === item.path || p.startsWith(item.path + '/') || (item.also || []).some(a => p.startsWith(a));
}
