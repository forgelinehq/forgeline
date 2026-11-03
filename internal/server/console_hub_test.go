package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The hub's filtering, sorting and wording (ui/lib/hub.js) are plain functions, so they run under Node.
// Skipped where Node is not installed.
const hubCases = `
import { matches, group, counts, summaryLine, logo, testSummary, statusPill, statusText, tierText, readOnlyText, attentionList, isPlanned, CATEGORIES, activityLabel, normalizeGcpScope, normalizeAzureSubs, toolChoice, policyRequest, agentStateText } from './hub.mjs';

let failed = 0;
const eq = (name, got, want) => {
  const g = JSON.stringify(got), w = JSON.stringify(want);
  if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); }
};
const it = (id, status, extra = {}) => ({ id, name: id.toUpperCase(), category: 'Cloud', status, summary: '', ...extra });
const list = [
  it('aws', 'connected', { summary: 'Connected, region ap-south-1' }),
  it('gcp', 'next', { order: 1, plan: 'Cloud Asset Inventory' }),
  it('azure', 'next', { order: 2 }),
  it('old', 'detected', { found: 'A profile named old' }),
  it('gh', 'attention', { category: 'Source and CI' }),
  it('off', 'off'),
  it('later', 'later', { order: 10 }),
];

// Filters.
eq('status all', list.filter(i => matches(i)).length, 7);
eq('status connected', list.filter(i => matches(i, { status: 'connected' })).map(i => i.id), ['aws']);
eq('soon covers next and later', list.filter(i => matches(i, { status: 'soon' })).map(i => i.id), ['gcp', 'azure', 'later']);
eq('attention', list.filter(i => matches(i, { status: 'attention' })).map(i => i.id), ['gh']);
eq('category', list.filter(i => matches(i, { category: 'Source and CI' })).map(i => i.id), ['gh']);
eq('search by name, case blind', list.filter(i => matches(i, { query: 'AzUrE' })).map(i => i.id), ['azure']);
eq('search finds the plan text', list.filter(i => matches(i, { query: 'asset inventory' })).map(i => i.id), ['gcp']);
eq('search finds what was found on the server', list.filter(i => matches(i, { query: 'profile named' })).map(i => i.id), ['old']);
eq('filters combine', list.filter(i => matches(i, { status: 'soon', category: 'Cloud', query: 'g' })).map(i => i.id), ['gcp']);
eq('blank search is no search', list.filter(i => matches(i, { query: '   ' })).length, 7);

// Grouping and order: needs attention first, then connected, found, off, then planned by build order.
const g = group(list);
eq('categories in the fixed order, empty ones left out', g.map(x => x.category), ['Source and CI', 'Cloud']);
eq('worst first, then planned by order', g[1].items.map(i => i.id), ['aws', 'old', 'off', 'gcp', 'azure', 'later']);
eq('something to fix comes before something that works', group([it('ok', 'connected'), it('bad', 'attention'), it('found', 'detected')])[0].items.map(i => i.id), ['bad', 'ok', 'found']);
eq('an unknown category is kept, after the known ones', group([it('x', 'off', { category: 'Brand new' }), it('y', 'off')]).map(x => x.category), ['Cloud', 'Brand new']);
eq('the planned build order beats the alphabet', group([it('b', 'next', { order: 2, name: 'A' }), it('a', 'next', { order: 1, name: 'Z' })])[0].items.map(i => i.id), ['a', 'b']);

// Counts and wording.
eq('counts', counts(list), { connected: 1, attention: 1, detected: 1, planned: 3, off: 1 });
eq('summary line', summaryLine(list), '1 connected · 1 needs attention · 1 found on this server · 3 coming soon');
eq('summary line plural and no detected', summaryLine([it('a', 'attention'), it('b', 'attention')]), '0 connected · 2 need attention · 0 coming soon');
eq('status text', ['connected', 'attention', 'detected', 'off', 'next', 'later'].map(s => statusText({ status: s })), ['Connected', 'Needs attention', 'Found on this server', 'Not connected', 'Coming soon · next', 'Coming soon · later']);
eq('pill classes', ['connected', 'attention', 'detected', 'off', 'next', 'later', 'nope'].map(statusPill), ['pill-ok', 'pill-wait', 'pill-run', 'pill-idle', 'pill-idle', 'pill-idle', 'pill-idle']);
eq('tier text', ['built-in', 'verified', 'community'].map(tierText), ['Built in', 'Verified', 'Community']);
eq('planned', [isPlanned(it('a', 'next')), isPlanned(it('b', 'later')), isPlanned(it('c', 'connected'))], [true, true, false]);
eq('attention list', attentionList(list).map(i => i.id), ['gh']);

// Read-only is always stated with who enforces it.
eq('read-only by the provider', readOnlyText({ read_only_by: 'provider', read_only_note: 'AWS, through the role' }), 'Read-only is enforced by the provider. AWS, through the role');
eq('read-only by Forgeline alone', readOnlyText({ read_only_by: 'forgeline', read_only_note: 'No read-only keys' }), 'Read-only is enforced by Forgeline alone. No read-only keys');
eq('no read-only claim when none is made', readOnlyText({ read_only_note: 'A model is not a tool' }), 'A model is not a tool');

// Logos.
eq('logo two words', logo('Google Cloud'), 'GC');
eq('logo one word', logo('Docker'), 'DO');
eq('logo with a quoted name', logo('AWS profile "old"'), 'AP');
eq('logo skips filler words', logo('GitLab and Bitbucket'), 'GB');
eq('logo skips filler words 2', logo('Bedrock, Vertex AI and Foundry'), 'BV');
eq('logo empty', logo(''), '?');

// Test summary.
eq('test summary', testSummary({ passed: 3, warned: 1, failed: 0, took_ms: 1700, changed: false }), '3 passed · 1 to look at · took 1.7 s · created nothing, changed nothing');
eq('test summary with a failure', testSummary({ passed: 0, warned: 0, failed: 2, took_ms: 200, changed: false }), '0 passed · 2 failed · took 0.2 s · created nothing, changed nothing');
eq('a test that changed something does not claim otherwise', testSummary({ passed: 1, warned: 0, failed: 0, took_ms: 0, changed: true }).includes('created nothing'), false);
eq('the fixed categories match the server', CATEGORIES, ['Source and CI', 'Cloud', 'AI infrastructure', 'Containers', 'AI', 'Observe and notify']);

// Activity wording.
eq('known actions', ['integration.test', 'integration.gcp.update', 'integration.aws.sso.signin', 'k8s.cluster.add'].map(activityLabel), ['Test run', 'Connection changed', 'Signed in', 'Cluster added']);
eq('an unknown action keeps its own name', activityLabel('integration.newthing.do_it'), 'newthing do it');
eq('an action that is not an integration one', activityLabel('k8s.cluster.scale'), 'k8s cluster scale');

// Scope checks that mirror the server.
eq('a bare project id', normalizeGcpScope('shop-prod'), { scope: 'projects/shop-prod' });
eq('a folder', normalizeGcpScope('  folders/123 '), { scope: 'folders/123' });
eq('empty scope', 'error' in normalizeGcpScope(''), true);
eq('a flag is not a scope', 'error' in normalizeGcpScope('projects/x --format=json'), true);
eq('wrong kind', 'error' in normalizeGcpScope('buckets/x'), true);
eq('leading dash', 'error' in normalizeGcpScope('projects/-x'), true);
eq('too long', 'error' in normalizeGcpScope('projects/' + 'a'.repeat(80)), true);
eq('null is empty', 'error' in normalizeGcpScope(null), true);
const A = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa', B = 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb';
eq('subscriptions sorted, lower-cased and unique', normalizeAzureSubs([B.toUpperCase(), A, A, '']), { subs: [A, B] });
eq('no subscriptions means all', normalizeAzureSubs([]), { subs: [] });
eq('not a guid', 'error' in normalizeAzureSubs(['prod']), true);
eq('a flag in front of a guid', 'error' in normalizeAzureSubs(['--x ' + A]), true);
eq('more than twenty', 'error' in normalizeAzureSubs(Array.from({ length: 21 }, (_, i) => String(i + 1).padStart(8, '0') + '-0000-0000-0000-000000000000')), true);

// The Access tab.
const rd = (name, level = 'allow') => ({ name, kind: 'read', level }), wr = (name, level = 'allow') => ({ name, kind: 'write', level });
eq('a read tool shows Allow', toolChoice(rd('a')).shown, 'allow');
eq('a blocked read tool shows Blocked', toolChoice(rd('a', 'block')).shown, 'block');
eq('a write tool that is not blocked shows Needs approval, never Allow', toolChoice(wr('p')).shown, 'ask');
eq('a blocked write tool shows Blocked', toolChoice(wr('p', 'block')).shown, 'block');
eq('a write tool cannot be set to Allow, but can go back to Needs approval after being blocked', toolChoice(wr('p', 'block')).options.map(o => [o.key, o.enabled]), [['allow', false], ['ask', true], ['block', true]]);
eq('a read tool can be Allow, Needs approval or Blocked', toolChoice(rd('a')).options.map(o => [o.key, o.enabled]), [['allow', true], ['ask', true], ['block', true]]);
eq('a read tool set to ask shows Needs approval', toolChoice(rd('a', 'ask')).shown, 'ask');
eq('a blocked read tool stays Blocked even if marked ask', toolChoice(rd('a', 'block')).shown, 'block');
eq('every disabled option says why', ['read', 'write'].every(k => toolChoice({ kind: k, level: 'allow' }).options.filter(o => !o.enabled).every(o => o.why.length > 10)), true);
eq('the read ask option explains that agents and MCP are refused', toolChoice(rd('a')).options[1].why.includes('refused'), true);
const tl = [rd('a'), rd('b', 'block'), rd('c', 'ask'), wr('p')];
eq('blocking one keeps the others as shown', policyRequest(tl, 'a', 'block'), { tools: { a: 'block', b: 'block', c: 'ask' } });
eq('allowing one removes only its entry', policyRequest(tl, 'b', 'allow'), { tools: { c: 'ask' } });
eq('asking sends ask for a read tool and keeps the rest', policyRequest(tl, 'a', 'ask'), { tools: { a: 'ask', b: 'block', c: 'ask' } });
eq('a write tool is never sent as allow or ask', policyRequest([wr('p')], 'p', 'allow'), { tools: {} });
eq('a write tool asked is not sent', policyRequest([wr('p')], 'p', 'ask'), { tools: {} });
eq('a blocked write tool is sent as a block', policyRequest([wr('p')], 'p', 'block'), { tools: { p: 'block' } });
eq('unblocking a write tool sends nothing for it', policyRequest([wr('p', 'block'), rd('a')], 'p', 'ask'), { tools: {} });
eq('agent state words', ['allowed', 'blocked', 'not_listed', 'needs_person', 'x'].map(agentStateText), ['Allowed', 'Blocked', 'Not listed', 'Needs a person', 'x']);

if (failed) process.exit(1);
console.log('ok');
`

func TestConsoleHubLogic(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	src, err := uiFiles.ReadFile("ui/lib/hub.js")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "hub.mjs"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	test := filepath.Join(tmp, "cases.mjs")
	if err := os.WriteFile(test, []byte(hubCases), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("hub cases failed:\n%s", out)
	}
}
