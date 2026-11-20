// Which page the person is on, for the assistant (note 024): the thing on it, and where it is as the console names it.
//
// No DOM, so it runs under Node in tests.
import { activeSpace, activeTab } from './nav.js';
import { activeItem } from './settingsnav.js';

/** Where a console address is, as the console names it: "Operations › Missions", "Settings › Models and providers". */
export function whereIs(full) {
  const [p, query] = String(full).split('?');
  const sp = activeSpace(p), tab = activeTab(p);
  let label = sp && tab ? (sp.label === tab.label ? sp.label : sp.label + ' › ' + tab.label) : activeItem(p) ? 'Settings › ' + activeItem(p).label : p === '/account' ? 'Your account' : 'Home › Today';
  const sub = new URLSearchParams(query || '').get('tab');
  if (sub) label += ' › ' + sub.charAt(0).toUpperCase() + sub.slice(1).replace(/-/g, ' ');
  return label;
}

/** What the page is about, so the server can attach the right data to the question: the thing on it, and where it is. */
export function pageContext(full) {
  const p = String(full).split('?')[0];
  const where = whereIs(full);
  const PAGES = [
    [/^\/runs\/([^/]+)$/, 'run', 'this run'],
    [/^\/pipelines\/((?!new$)[^/]+)$/, 'pipeline', 'this pipeline'],
    [/^\/agents\/([^/]+)$/, 'agent', 'this agent'],
    [/^\/teams\/([^/]+)$/, 'team', 'this team'],
    [/^\/missions\/([^/]+)$/, 'mission', 'this mission'],
    [/^\/workflows\/((?!new(?:\/|$))[^/]+)(?:\/canvas)?$/, 'workflow', 'this workflow'],
    [/^\/workflow-runs\/([^/]+)$/, 'workflow_run', 'this workflow run'],
    [/^\/proposals\/([^/]+)$/, 'proposal', 'this proposal'],
    [/^\/kubernetes\/([^/]+)$/, 'k8s', 'this cluster'],
  ];
  for (const [re, kind, label] of PAGES) {
    const m = p.match(re);
    if (m) return { kind, id: decodeURIComponent(m[1]), label, path: full, where };
  }
  if (p === '/infrastructure') return { kind: 'infra', id: '', label: 'your infrastructure', path: full, where };
  if (p === '/kubernetes') return { kind: 'k8s', id: '', label: 'your clusters', path: full, where };
  if (p === '/guard') return { kind: 'guard', id: '', label: 'the security findings', path: full, where };
  if (p === '/' || p === '') return { kind: 'none', id: '', label: 'Home', path: full, where };
  return { kind: 'page', id: '', label: where, path: full, where };
}

