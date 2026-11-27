package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The search box (design note 048, step 4): what it finds for a role, and in what order.
const paletteCases = `import { pageEntries, actionEntries, thingEntries, search, score } from './palette.js';
let failed = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); } };
const labels = (r) => r.map(e => e.label);

const admin = [...pageEntries('admin'), ...actionEntries('admin')];
const viewer = [...pageEntries('viewer'), ...actionEntries('viewer')];
eq('a viewer finds no agent pages, settings or actions', [viewer.some(e => e.path === '/agents'), viewer.some(e => e.path === '/people'), viewer.filter(e => e.kind === 'action').length], [false, false, 0]);
eq('an administrator finds the audit log and settings', [admin.some(e => e.path === '/audit'), admin.some(e => e.path === '/registries')], [true, true]);
eq('a developer makes agents, workflows and pipelines, but does not connect an MCP server', actionEntries('developer').map(e => e.label), ['New agent', 'New workflow', 'New pipeline', 'Add a model provider']);

const things = thingEntries({
  agents: [{ id: 'run-explainer', name: 'Run explainer', description: 'Explains failed runs' }, { id: 'old', name: 'Old one', archived_at: '2026-11-01' }],
  workflows: [{ id: 'explain-failed-runs', name: 'Explain failed runs' }],
  pipelines: [{ id: 'web', name: 'web-deploy', repo: 'acme/web' }],
  teams: null,
});
eq('archived agents are left out; missing lists are fine', labels(things), ['Run explainer', 'Explain failed runs', 'web-deploy']);
eq('paths', things.map(e => e.path), ['/agents/run-explainer', '/workflows/explain-failed-runs', '/pipelines/web']);

const all = [...admin, ...things];
eq('a page by its name', search(all, 'audit')[0].path, '/audit');
eq('a word start beats a substring', labels(search(all, 'expl')).slice(0, 2), ['Explain failed runs', 'Run explainer']);
eq('every word must match, by name or description', labels(search(all, 'run failed')), ['Explain failed runs', 'Run explainer']);
eq('a word that matches nothing removes it', labels(search(all, 'web acme')), ['web-deploy']);
eq('the space name finds its tabs', search(all, 'security').some(e => e.path === '/policy-packs'), true);
eq('letters in order find a page', search(all, 'rgstr').some(e => e.path === '/registries'), true);
eq('nothing for nonsense', search(all, 'zzqx'), []);
eq('empty query lists pages first, no things', search(all, '', 50).every(e => e.kind === 'page' || e.kind === 'action'), true);
eq('description words count, less than the name', [score(things[0], 'explains') > 0, score(things[0], 'explains') < score(things[0], 'run')], [true, true]);
eq('accents and case do not matter', score({ label: 'Café ops', where: '' }, 'CAFE'), 80);
if (failed) process.exit(1);
`

func TestConsoleSearchBox(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	for _, f := range []string{"palette.js", "nav.js", "settingsnav.js"} {
		src, err := uiFiles.ReadFile("ui/lib/" + f)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(tmp, f), src, 0o600)
	}
	os.WriteFile(filepath.Join(tmp, "package.json"), []byte(`{"type":"module"}`), 0o600)
	test := filepath.Join(tmp, "cases.mjs")
	os.WriteFile(test, []byte(paletteCases), 0o600)
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("search box cases failed:\n%s", out)
	}
}
