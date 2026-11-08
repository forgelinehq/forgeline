package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Home's inbox (ui/lib/inbox.js) and the sidebar (ui/lib/nav.js) are plain data and functions, so
// they run under Node. Skipped where Node is not installed.
const homeCases = `
import { buildInbox, pulse, greeting, summaryLine, recentChanges } from './inbox.mjs';
import { SPACES, spacesFor, tabsFor, activeSpace, activeTab, tabMatches } from './nav.mjs';

let failed = 0;
const eq = (name, got, want) => {
  const g = JSON.stringify(got), w = JSON.stringify(want);
  if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); }
};

const admin = { name: 'vicky', role: 'admin' };
const dev = { name: 'dana', role: 'developer' };
const prop = (id, by, status = 'proposed', at = '2026-11-07T10:00:00Z') => ({ id, title: 'Create queue ' + id, status, proposed_by: by, proposed_at: at, risk: 'normal', account_name: 'Old account' });
const src = {
  proposals: [prop('p1', 'claude-agent'), prop('p2', 'vicky'), prop('p3', 'claude-agent', 'applied'), prop('p4', 'claude-agent', 'rejected')],
  changes: [{ id: 'c1', title: 'Deploy demo', status: 'proposed', proposed_by: 'dana', proposed_at: '2026-11-07T09:00:00Z' }, { id: 'c2', title: 'Old', status: 'succeeded', proposed_by: 'dana' }],
  findings: { findings: [
    { id: 'f1', title: 'Root has no MFA', severity: 'high', status: 'open', name: 'root account', account_name: 'Default account', first_seen: '2026-11-04T00:00:00Z' },
    { id: 'f2', title: 'Bucket is public', severity: 'critical', status: 'open', name: 'b', first_seen: '2026-11-05T00:00:00Z' },
    { id: 'f3', title: 'Logs kept forever', severity: 'low', status: 'open' },
    { id: 'f4', title: 'Accepted risk', severity: 'high', status: 'accepted' },
  ] },
  overview: { pipelines: [
    { name: 'build', latest: { id: 'r1', status: 'failed', triggered_by: 'dana', created_at: '2026-11-07T08:00:00Z' }, success_rate: 0.5 },
    { name: 'deploy', latest: { id: 'r2', status: 'succeeded' } },
    { name: 'new' },
  ] },
  integrations: { integrations: [{ id: 'kubernetes', name: 'Kubernetes', status: 'attention', summary: 'Cluster unreachable' }, { id: 'aws', name: 'AWS', status: 'connected' }, { id: 'gcp', name: 'Google Cloud', status: 'off' }] },
};

// Approvals: an administrator is asked to approve only what someone else proposed.
const a = buildInbox(src, admin);
eq('admin acts on proposals by others', a.filter(i => i.tone === 'act').map(i => i.key), ['proposal:p1', 'change:c1']);
eq('admin waits on their own proposal', a.find(i => i.key === 'proposal:p2').tone, 'wait');
eq('decided proposals and changes are not listed', a.some(i => ['proposal:p3', 'proposal:p4', 'change:c2'].includes(i.key)), false);
eq('a proposal links to its page', a.find(i => i.key === 'proposal:p1').href, '/proposals/p1');
eq('a cloud change links to the changes tab', a.find(i => i.key === 'change:c1').href, '/infrastructure?tab=changes');

// A developer is never asked to approve; they see what is waiting.
const d = buildInbox(src, dev);
eq('a developer has nothing to approve', d.filter(i => i.tone === 'act').length, 0);
eq('a developer sees their own change waiting', d.find(i => i.key === 'change:c1').detail.startsWith('You proposed it'), true);
eq('no session means no approvals', buildInbox(src, null).filter(i => i.tone === 'act').length, 0);

// Problems: failed last runs, open serious findings, connections needing attention.
eq('only a pipeline whose last run failed', a.filter(i => i.key.startsWith('run:')).map(i => i.title), ['build failed']);
eq('only open high and critical findings', a.filter(i => i.key.startsWith('finding:')).map(i => i.key), ['finding:f2', 'finding:f1']);
eq('a critical finding is bad, a high one a warning', [a.find(i => i.key === 'finding:f2').tone, a.find(i => i.key === 'finding:f1').tone], ['bad', 'warn']);
eq('a connection that needs attention', a.filter(i => i.key.startsWith('integration:')).map(i => i.href), ['/integrations/kubernetes']);

// Order: things you can act on, then critical and broken things, then high findings, then waiting.
eq('ranks never go down the list', a.every((i, n) => n === 0 || a[n - 1].rank <= i.rank), true);
eq('first is an approval, last is waiting', [a[0].tone, a[a.length - 1].tone], ['act', 'wait']);
eq('newest first within a rank', a.filter(i => i.rank === 0).map(i => i.key), ['proposal:p1', 'change:c1']);

// Missing sources: the inbox says less, never something wrong.
eq('nothing readable, nothing listed', buildInbox({}, admin), []);
const pl = pulse({ overview: src.overview }, []);
eq('unreadable sources give no tile', [pl.security, pl.connections], [null, null]);
eq('pipelines count only those that ran', pl.pipelines, { healthy: 1, ran: 2, total: 3 });
const full = pulse(src, a);
eq('pulse counts', [full.act, full.waiting, full.security.serious, full.security.open, full.connections], [2, 1, 2, 3, { connected: 1, attention: 1 }]);
eq('unknown scans say nothing about scanning', full.security.scanned, null);
eq('no finished scan is "not scanned", not "no problems"', pulse({ findings: { findings: [] }, scans: { scans: [{ status: 'running' }] } }, []).security.scanned, false);
const scanned = pulse({ findings: { findings: [] }, scans: { scans: [{ status: 'done', finished_at: '2026-11-04T00:00:00Z' }, { status: 'done', finished_at: '2026-11-06T00:00:00Z' }] } }, []).security;
eq('the latest finished scan', [scanned.scanned, scanned.last], [true, '2026-11-06T00:00:00Z']);

// Greeting, summary and recent changes.
eq('greetings by hour', [greeting(3, 'vicky'), greeting(9, 'vicky'), greeting(14, 'Ada Lovelace'), greeting(20, ''), greeting(9, null)], ['Hello, Vicky', 'Good morning, Vicky', 'Good afternoon, Ada', 'Good evening', 'Good morning']);
eq('summary', [summaryLine([]), summaryLine(a.slice(0, 2)), summaryLine([{ rank: 3, tone: 'wait' }])], ['Nothing needs you. Everything is quiet.', 'Two things need you. Everything else is quiet.', 'Nothing needs you. Everything is quiet.']);
const rc = recentChanges({ proposals: [
  { id: 'x1', title: 'Queue', status: 'applied', proposed_by: 'claude-agent', decided_by: 'vicky', finished_at: '2026-11-07T19:55:29Z', account_name: 'Old account' },
  { id: 'x2', title: 'Waiting', status: 'proposed' },
  { id: 'x3', title: 'No', status: 'rejected', decided_by: 'vicky', decided_at: '2026-11-07T20:00:00Z' },
], changes: [{ id: 'c9', title: 'Deploy', status: 'succeeded', finished_at: '2026-11-06T10:00:00Z' }] });
eq('recent: decided only, newest first', rc.map(r => r.key), ['proposal:x3', 'proposal:x1', 'change:c9']);
eq('recent detail', rc[1].detail, 'applied · Old account · proposed by claude-agent · approved by vicky');
eq('recent tones', rc.map(r => r.tone), ['bad', 'ok', 'ok']);
eq('rejected says rejected', rc[0].detail, 'rejected · rejected by vicky');
eq('recent limit', recentChanges({ proposals: Array.from({ length: 9 }, (_, i) => ({ id: 'p' + i, title: 't', status: 'applied' })) }).length, 4);

// The sidebar: five spaces, the pages as tabs inside them, the same role checks as before.
const ids = role => spacesFor(role).map(s => s.id);
eq('a viewer has no Build or Team (nothing there a viewer may open)', ids('viewer'), ['home', 'ship', 'watch']);
eq('a developer sees all five', ids('developer'), ['home', 'build', 'ship', 'team', 'watch']);
eq('an unknown role sees nothing', ids('nobody'), []);
eq('a space links to its first tab the role may open', spacesFor('developer').map(s => s.path), ['/', '/environments', '/pipelines', '/team', '/map']);
eq('a viewer sees only the Ship tabs they may open', tabsFor(SPACES.find(s => s.id === 'ship'), 'viewer').map(t => t.label), ['Pipelines']);
eq('planned tabs are listed and never links', tabsFor(SPACES.find(s => s.id === 'build'), 'developer').map(t => (t.planned ? '~' : '') + t.label), ['~Design', 'Environments', 'Blueprints']);
eq('planned tabs have no path', SPACES.every(s => s.tabs.every(t => !t.planned || !t.path)), true);
eq('every page belongs to its space', ['/', '/proposals/abc', '/environments', '/pipelines/new', '/runs/r1', '/actions', '/team', '/agents/x', '/skills/y', '/workflow-runs/z', '/marketplace', '/map', '/infrastructure?tab=aws', '/kubernetes/k1', '/guard'].map(p => activeSpace(p)?.id),
  ['home', 'home', 'build', 'ship', 'ship', 'ship', 'team', 'team', 'team', 'team', 'team', 'watch', 'watch', 'watch', 'watch']);
eq('settings pages belong to no space', ['/settings', '/integrations', '/people', '/account'].map(p => activeSpace(p)), [null, null, null, null]);
eq('Home matches only itself', [tabMatches(SPACES[0].tabs[0], '/'), tabMatches(SPACES[0].tabs[0], '/pipelines')], [true, false]);
eq('active tab', [activeTab('/skills/a')?.label, activeTab('/guard')?.label, activeTab('/agentsx')?.label, activeTab('/team')?.label], ['Agents', 'Security', undefined, 'Team']);
eq('a path that only starts like a page is not that page', activeSpace('/pipelinesx'), null);

if (failed) process.exit(1);
console.log('ok');
`

func TestConsoleHomeInboxAndNav(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	for _, f := range []string{"inbox", "nav"} {
		src, err := uiFiles.ReadFile("ui/lib/" + f + ".js")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmp, f+".mjs"), src, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	test := filepath.Join(tmp, "cases.mjs")
	if err := os.WriteFile(test, []byte(homeCases), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("home cases failed:\n%s", out)
	}
}
