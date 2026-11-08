package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/forgelinehq/forgeline/internal/blueprint"
)

const blueprintWordCases = `
import { blockFacts, linkWords, STARTERS, KINDS } from './blueprint.mjs';
let failed = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); } };
eq('service facts', blockFacts({ kind: 'service', image: 'nginx:1.29', cpu: 512, memory: 1024, count: 2, port: 80 }), '2 × 0.5 vCPU, 1 GB · nginx:1.29 · port 80');
eq('worker facts', blockFacts({ kind: 'service', image: 'busybox', cpu: 256, memory: 512, count: 1 }), '1 × 0.25 vCPU, 0.5 GB · busybox');
eq('plain http is said', blockFacts({ kind: 'frontdoor' }), 'HTTP only');
eq('https', blockFacts({ kind: 'frontdoor', certificate: 'arn:x', health_path: '/health' }), 'HTTPS · checks /health');
eq('table', blockFacts({ kind: 'table', partition_key: 'id', sort_key: 'at' }), 'key id, sorted by at · pay per request');
eq('existing is never changed', blockFacts({ kind: 'existing', existing: { type: 'table', name: 'answers' } }), 'DynamoDB table answers · used, never changed');
const bp = { blocks: [{ id: 'w', kind: 'frontdoor', name: 'Door' }, { id: 'a', kind: 'service', name: 'App' }, { id: 't', kind: 'table', name: 'Data' }, { id: 'l', kind: 'logs', name: 'Logs' }] };
eq('traffic', linkWords(bp, { from: 'w', to: 'a' }), 'Door sends traffic to App');
eq('read', linkWords(bp, { from: 'a', to: 't', access: 'read' }), 'App reads Data');
eq('readwrite', linkWords(bp, { from: 'a', to: 't', access: 'readwrite' }), 'App reads and writes Data');
eq('logs', linkWords(bp, { from: 'a', to: 'l' }), 'App writes its logs to Logs');
eq('every kind has words', ['frontdoor', 'service', 'table', 'bucket', 'queue', 'logs', 'existing'].every(k => KINDS[k] && KINDS[k].label), true);
if (failed) process.exit(1);
console.log(JSON.stringify(STARTERS));
`

func TestConsoleBlueprintWords(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	src, err := uiFiles.ReadFile("ui/lib/blueprint.js")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(tmp, "blueprint.mjs"), src, 0o600)
	test := filepath.Join(tmp, "cases.mjs")
	os.WriteFile(test, []byte(blueprintWordCases), 0o600)
	out, err := exec.Command(node, test).Output()
	if err != nil {
		t.Fatalf("blueprint word cases failed: %v\n%s", err, out)
	}
	// Every starting point the console offers is a valid blueprint, by the server's own rules.
	var starters map[string]struct {
		Blocks []blueprint.Block `json:"blocks"`
		Links  []blueprint.Link  `json:"links"`
	}
	if err := json.Unmarshal(out, &starters); err != nil || len(starters) == 0 {
		t.Fatalf("starters: %v\n%s", err, out)
	}
	for name, s := range starters {
		b := blueprint.Blueprint{ID: "x", Name: name, Target: blueprint.Target{Cloud: "aws", Account: "primary", Region: "ap-south-1"}, Blocks: s.Blocks, Links: s.Links}
		if err := b.Validate(); err != nil {
			t.Errorf("starter %s is not valid: %v", name, err)
		}
	}
}
