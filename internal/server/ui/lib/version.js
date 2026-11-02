// Forgeline web console: how a server version is shown. No imports and no DOM, so it runs under Node
// in tests. A release build reports "0.1.0" and a build from git reports "v0.1.0-4-gabc123"; both
// are shown with exactly one leading "v".
export function versionLabel(v) {
  const s = String(v || '').trim();
  if (!s) return 'unknown';
  return /^v/i.test(s) ? 'v' + s.slice(1) : 'v' + s;
}
