package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The AWS map's layout and flags are plain functions (ui/lib/awsmap.js), so they run under Node.
// Skipped where Node is not installed. Each case says what it protects: the map must state only
// what the inventory states.
const awsMapCases = `
import { stateKind, attention, buildMap, matches } from './awsmap.mjs';

let failed = 0;
const eq = (name, got, want) => {
  const g = JSON.stringify(got), w = JSON.stringify(want);
  if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); }
};
const f = (label, value) => ({ label, value });
const res = (id, extra = {}) => ({ id, name: id, ...extra });
const svc = (id, category, resources, extra = {}) => ({ id, label: id.toUpperCase(), category, count: resources.length, resources, ...extra });

// State meaning.
eq('running is ok', stateKind('running'), 'ok');
eq('available is ok', stateKind('available'), 'ok');
eq('CREATE_COMPLETE is ok', stateKind('CREATE_COMPLETE'), 'ok');
eq('rollback is bad, not ok', stateKind('ROLLBACK_COMPLETE'), 'bad');
eq('failed is bad', stateKind('failed'), 'bad');
eq('pending is changing', stateKind('pending'), 'wait');
eq('stopped is just another state', stateKind('stopped'), 'idle');
eq('no state', stateKind(''), 'none');

// What is flagged. A stopped instance is a choice, not a problem, so it is not flagged.
const texts = (id, r) => attention(id, r).map(x => x.level + ':' + x.text);
eq('stopped instance is not flagged', texts('ec2', res('i-1', { state: 'stopped' })), []);
eq('failing resource is bad', texts('cfn', res('s', { state: 'UPDATE_ROLLBACK_FAILED' })), ['bad:State: UPDATE_ROLLBACK_FAILED']);
eq('changing resource is a warning', texts('rds', res('d', { state: 'modifying', fields: [] })), ['warn:State: modifying']);
eq('open security group', texts('sg', res('sg-1', { fields: [f('Open to the internet', 'tcp 22')] })), ['warn:Open to the internet: tcp 22']);
eq('open field on a non-sg is ignored', texts('ec2', res('i-2', { state: 'running', fields: [f('Open to the internet', 'tcp 22')] })), []);
eq('unattached volume', texts('ebs', res('v', { state: 'available', fields: [f('Encrypted', 'true')] })), ['warn:Not attached to any instance']);
eq('attached volume is fine', texts('ebs', res('v', { state: 'in-use', fields: [f('Encrypted', 'true')] })), []);
eq('unencrypted volume', texts('ebs', res('v', { state: 'in-use', fields: [f('Encrypted', 'false')] })), ['warn:Not encrypted']);
eq('public database', texts('rds', res('d', { state: 'available', fields: [f('Publicly accessible', 'true'), f('Encrypted', 'true')] })), ['warn:Publicly accessible']);
eq('private encrypted database', texts('rds', res('d', { state: 'available', fields: [f('Publicly accessible', 'false'), f('Encrypted', 'true')] })), []);

// Layout.
const inv = { services: [
  svc('vpc', 'Network', [res('vpc-a', { name: 'prod', fields: [f('CIDR', '10.0.0.0/16'), f('Default', 'false')] }), res('vpc-d', { fields: [f('Default', 'true')] })]),
  svc('ec2', 'Compute', [res('i-1', { state: 'running', fields: [f('VPC', 'vpc-a')] }), res('i-2', { state: 'running', fields: [f('VPC', 'vpc-a')] }), res('i-3', { state: 'running', fields: [f('VPC', 'vpc-gone')] })]),
  svc('sg', 'Network', [res('sg-1', { fields: [f('VPC', 'vpc-d'), f('Open to the internet', 'tcp 22')] })]),
  svc('s3', 'Storage', [res('bucket-1')]),
  svc('lambda', 'Compute', [res('fn-1', { state: 'Active' })]),
  svc('rds', 'Databases', [], { count: 0 }),
  svc('ecr', 'Containers', [], { count: 0, error: 'AccessDenied' }),
] };
const m = buildMap(inv);
eq('vpc order and counts', m.vpcs.map(v => [v.id, v.name, v.count, v.synthetic]), [['vpc-a', 'prod', 2, false], ['vpc-d', 'vpc-d', 1, false], ['vpc-gone', 'vpc-gone', 1, true]]);
eq('vpc fields', [m.vpcs[0].cidr, m.vpcs[0].isDefault, m.vpcs[1].isDefault], ['10.0.0.0/16', false, true]);
eq('a vpc is a container, not an item in itself', m.vpcs.every(v => v.groups.every(g => g.svc.id !== 'vpc')), true);
eq('resources land in the vpc their field names', m.vpcs[0].groups.map(g => [g.svc.id, g.items.map(r => r.id)]), [['ec2', ['i-1', 'i-2']]]);
eq('an unknown vpc gets a placeholder, not a guess', m.vpcs[2].groups.map(g => g.items.map(r => r.id)), [['i-3']]);
eq('no vpc field means not placed, by category order', m.regional.map(c => [c.category, c.groups.map(g => g.svc.id)]), [['Compute', ['lambda']], ['Storage', ['s3']]]);
eq('empty and failed services are left out', JSON.stringify(m).includes('"ecr"') || JSON.stringify(m).includes('"rds"'), false);
eq('attention lists the flagged ones', m.attention.map(a => a.r.id), ['sg-1']);

// Search keeps containers that hold a match and hides the rest.
const q = buildMap(inv, 'i-1');
eq('search keeps the matching vpc only', q.vpcs.map(v => v.id), ['vpc-a']);
eq('search drops unmatched regional resources', q.regional, []);
eq('search matches tag values', matches(res('x', { tags: { team: 'payments' } }), 'PAYMENTS'), true);

// Failing resources come before warnings.
const sorted = buildMap({ services: [
  svc('sg', 'Network', [res('sg-w', { fields: [f('Open to the internet', 'tcp 22')] })]),
  svc('cfn', 'Management', [res('stack-bad', { state: 'ROLLBACK_COMPLETE' })]),
] });
eq('bad before warn', sorted.attention.map(a => a.r.id), ['stack-bad', 'sg-w']);

if (failed) process.exit(1);
console.log('ok');
`

func TestConsoleAWSMap(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	src, err := uiFiles.ReadFile("ui/lib/awsmap.js")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	// .mjs so Node treats the file as a module, as the browser does.
	if err := os.WriteFile(filepath.Join(tmp, "awsmap.mjs"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	test := filepath.Join(tmp, "cases.mjs")
	if err := os.WriteFile(test, []byte(awsMapCases), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("aws map cases failed:\n%s", out)
	}
}
