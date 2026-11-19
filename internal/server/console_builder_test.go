package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The agent builder's side panel (ui/lib/builder.js) is plain functions, run under Node. Skipped where Node is missing.
const builderCases = `
import { draftFrom, draftBody, changed, pickerGroups, tabFilters, toggle, tabSummary, partChips } from './builder.js';
let failed = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); } };

const data = {
  models: [
    { ref: 'anthropic/claude-haiku-5-5', provider: 'anthropic', provider_name: 'Anthropic', kind: 'anthropic', label: 'Claude Haiku 5.5', input_usd_per_mtok: 1, output_usd_per_mtok: 5 },
    { ref: 'pi/qwen3', provider: 'pi', provider_name: 'Ollama on the Pi', kind: 'openai-compatible', label: 'qwen3', input_usd_per_mtok: 0, output_usd_per_mtok: 0 },
  ],
  skills: [{ id: 'sev', name: 'Severity scale', description: 'low, medium, high', current: 3 }, { id: 'rb', name: 'Runbook reading', current: 1 }],
  tools: [
    { name: 'list_runs', help: '{} See runs', min: 'viewer' },
    { name: 'propose_aws_change', help: 'Draft a change', min: 'developer' },
    { name: 'mcp_gh_search_issues', help: 'Search issues', min: 'developer', needs: 'mcp-server', server: 'gh', server_name: 'GitHub', tool: 'search_issues' },
    { name: 'mcp_gh_get_commit', help: 'Read a commit', min: 'developer', needs: 'mcp-server', server: 'gh', server_name: 'GitHub', tool: 'get_commit' },
    { name: 'mcp_docs_search', help: 'Search docs', min: 'developer', needs: 'mcp-server', server: 'docs', server_name: 'AWS Knowledge', tool: 'search' },
  ],
};
const ag = { name: 'Triage', description: 'd', versions: [] };
const v = { instructions: 'Look first.', tier: 'T0', model: '', skills: [{ id: 'sev', n: 2 }], tools: ['list_runs', 'mcp_gh_search_issues'] };
let d = draftFrom(ag, v);
const base = draftFrom(ag, v);

// Models: one group per provider, then the assistant's connection; one is picked.
let gs = pickerGroups('Models', data, d);
eq('model groups', gs.map(g => g.label), ['Anthropic', 'Ollama on the Pi', 'Through the assistant’s connection']);
eq('the assistant model is picked', gs[2].items[0].on, true);
d = toggle(d, 'Models', 'pi/qwen3');
eq('picking a model replaces it', [d.model, pickerGroups('Models', data, d)[1].items[0].on], ['pi/qwen3', true]);
eq('a model no longer offered still shows', pickerGroups('Models', data, { ...d, model: 'gone/x' })[0].items[0].key, 'gone/x');

// Skills: pinned versions kept, a newer one is said; a new pick takes the latest.
gs = pickerGroups('Skills', data, d);
eq('pinned skill', gs[0].items[0].tags[0].text, 'pinned v2 · v3 out');
d = toggle(d, 'Skills', 'rb');
eq('a new skill takes the latest', d.skills, [{ id: 'sev', n: 2 }, { id: 'rb', n: 0 }]);

// MCP servers: tools grouped by server; the Tools tab leaves them out.
gs = pickerGroups('MCP servers', data, d);
eq('servers', gs.map(g => [g.label, g.items.map(i => i.label + (i.on ? '*' : ''))]), [['GitHub', ['search_issues*', 'get_commit']], ['AWS Knowledge', ['search']]]);
eq('tools tab has no server tools', pickerGroups('Tools', data, d).flatMap(g => g.items.map(i => i.key)), ['list_runs', 'propose_aws_change']);
d = toggle(d, 'MCP servers', 'mcp_docs_search');
eq('mcp summary', tabSummary('MCP servers', data, d), '2 tools from 2 servers · only tools an administrator turned on');
eq('mcp chips', partChips('MCP servers', data, d).map(c => c.label), ['GitHub · search_issues', 'AWS Knowledge · search']);
eq('tool chips', partChips('Tools', data, d).map(c => c.label), ['See recent runs']);

// Search and filters.
eq('search', pickerGroups('MCP servers', data, d, 'commit').flatMap(g => g.items.map(i => i.key)), ['mcp_gh_get_commit']);
eq('filters offered', tabFilters('Tools', data, d), ['reads', 'a person approves', 'developer and up']);
eq('a filter narrows', pickerGroups('Tools', data, d, '', ['a person approves']).flatMap(g => g.items.map(i => i.key)), ['propose_aws_change']);

// Guardrails: the tier is a choice; the rules always on cannot be picked off.
d = toggle(d, 'Guardrails', 'tier:T1');
eq('tier', d.tier, 'T1');
eq('locked rules stay', toggle(d, 'Guardrails', 'always:0'), d);
eq('locked shown', pickerGroups('Guardrails', data, d)[1].items.every(i => i.locked && i.on), true);

// What is saved, and whether anything changed.
eq('changed', [changed(base, base), changed(d, base), changed({ ...base, note: 'x' }, base)], [false, true, false]);
eq('body', Object.keys(draftBody(d)), ['name', 'description', 'instructions', 'tier', 'model', 'note', 'skills', 'tools']);
eq('toggle leaves the draft alone', base.tools, ['list_runs', 'mcp_gh_search_issues']);
if (failed) process.exit(1);
`

func TestConsoleAgentBuilder(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	for _, f := range []string{"builder.js", "tools.js"} {
		src, err := uiFiles.ReadFile("ui/lib/" + f)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(tmp, f), src, 0o600)
	}
	os.WriteFile(filepath.Join(tmp, "package.json"), []byte(`{"type":"module"}`), 0o600)
	os.WriteFile(filepath.Join(tmp, "cases.js"), []byte(builderCases), 0o600)
	if out, err := exec.Command(node, filepath.Join(tmp, "cases.js")).CombinedOutput(); err != nil {
		t.Fatalf("agent builder cases failed: %v\n%s", err, out)
	}
}
