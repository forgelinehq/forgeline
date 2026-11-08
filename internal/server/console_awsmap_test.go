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
import { stateKind, attention, buildMap, matches, alarmsFor, health, stackOwners, linksOf, linePairs, stackStateWords } from './awsmap.mjs';

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

// Alarms (note 023, step 2). An alarm belongs to a resource only when namespace and dimensions say so.
const al = (name, state, namespace, dims) => ({ name, state, namespace, dimensions: Object.entries(dims).map(([n, v]) => ({ name: n, value: v })) });
const alarms = [
  al('cpu-hot', 'ALARM', 'AWS/EC2', { InstanceId: 'i-1' }),
  al('cpu-fine', 'OK', 'AWS/EC2', { InstanceId: 'i-2' }),
  al('quiet', 'INSUFFICIENT_DATA', 'AWS/EC2', { InstanceId: 'i-3' }),
  al('db-cpu', 'ALARM', 'AWS/RDS', { DBInstanceIdentifier: 'orders' }),
  al('wrong-ns', 'ALARM', 'Custom/Thing', { InstanceId: 'i-4' }),
  al('web-5xx', 'ALARM', 'AWS/ApplicationELB', { LoadBalancer: 'app/web-alb/abc' }),
  al('svc-web', 'ALARM', 'AWS/ECS', { ClusterName: 'payments', ServiceName: 'web' }),
  al('cluster-cpu', 'OK', 'AWS/ECS', { ClusterName: 'payments' }),
];
const names = (svcId, r) => alarmsFor(alarms, svcId, r).map(a => a.name);
eq('alarm by instance id', names('ec2', res('i-1')), ['cpu-hot']);
eq('no alarm for an instance nobody named', names('ec2', res('i-9')), []);
eq('a similar id is not a match', names('ec2', res('i')), []);
eq('namespace must match', names('ec2', res('i-4')), []);
eq('alarm by db identifier', names('rds', res('orders')), ['db-cpu']);
eq('same id under another service type is not matched', names('ec2', res('orders')), []);
eq('load balancer by arn suffix', names('elb', res('web-alb', { arn: 'arn:aws:elasticloadbalancing:ap-south-1:1:loadbalancer/app/web-alb/abc' })), ['web-5xx']);
eq('load balancer without an arn matches nothing', names('elb', res('web-alb')), []);
eq('ecs service needs its cluster too', names('ecs-services', res('web', { fields: [f('Cluster', 'payments')] })), ['svc-web']);
eq('ecs service in another cluster', names('ecs-services', res('web', { fields: [f('Cluster', 'other')] })), []);
eq('ecs cluster alarm excludes service alarms', names('ecs', res('payments')), ['cluster-cpu']);
eq('a resource without an id matches nothing, even an alarm that lacks the dimension', alarmsFor([al('bare', 'ALARM', 'AWS/EC2', { Other: 'x' })], 'ec2', {}), []);
eq('no alarms given', alarmsFor(null, 'ec2', res('i-1')), []);
eq('unknown service type', alarmsFor(alarms, 'vpc', res('i-1')), []);

eq('health alarm', health(alarms, 'ec2', res('i-1')).kind, 'alarm');
eq('health ok', health(alarms, 'ec2', res('i-2')).kind, 'ok');
eq('health no data is not ok', health(alarms, 'ec2', res('i-3')).kind, 'nodata');
eq('no alarm is none, never ok', health(alarms, 'ec2', res('i-9')).kind, 'none');
eq('an alarm in ALARM wins over an OK one', health([...alarms, al('x', 'OK', 'AWS/EC2', { InstanceId: 'i-1' })], 'ec2', res('i-1')).kind, 'alarm');

eq('alarm flags the resource as bad', attention('ec2', res('i-1', { state: 'running' }), alarms).map(x => x.level + ':' + x.text), ['bad:Alarm: cpu-hot']);
eq('an OK alarm flags nothing', attention('ec2', res('i-2', { state: 'running' }), alarms), []);
eq('no data flags nothing', attention('ec2', res('i-3', { state: 'running' }), alarms), []);
eq('alarms not read flags nothing', attention('ec2', res('i-1', { state: 'running' }), null), []);
const am = buildMap({ services: [svc('ec2', 'Compute', [res('i-1', { state: 'running' }), res('i-2', { state: 'running' })])] }, '', alarms);
eq('map lists the alarmed resource', am.attention.map(a => a.r.id), ['i-1']);

// Stacks and links (note 034): only what CloudFormation and Lambda state.
const sinv = { services: [
  svc('cfn', 'Management', [res('shop', { state: 'ROLLBACK_COMPLETE' }), res('other', { state: 'CREATE_COMPLETE' })]),
  svc('dynamodb', 'Databases', [res('orders'), res('orders-copy')]),
  svc('lambda', 'Compute', [res('shop-worker', { fields: [f('VPC', 'vpc-a')] }), res('feed')]),
  svc('sqs', 'Messaging', [{ id: 'https://sqs.ap-south-1.amazonaws.com/1/jobs', name: 'jobs' }]),
  svc('vpc', 'Network', [res('vpc-a')]),
] };
const links = {
  stacks: [{ name: 'shop', parts: [
    { type: 'AWS::DynamoDB::Table', logical: 'Orders', physical: 'orders', status: 'DELETE_SKIPPED' },
    { type: 'AWS::Lambda::Function', logical: 'Worker', physical: 'shop-worker', status: 'CREATE_COMPLETE' },
    { type: 'AWS::IAM::Role', logical: 'Role', physical: 'shop-role', status: 'CREATE_COMPLETE' },
    { type: 'AWS::DynamoDB::Table', logical: 'Gone', physical: 'not-listed', status: 'CREATE_COMPLETE' },
    { type: 'AWS::EC2::VPC', logical: 'Net', physical: 'vpc-a', status: 'CREATE_COMPLETE' }] }],
  links: [
    { from: { service: 'lambda', id: 'shop-worker' }, to: { service: 'sqs', id: 'jobs' }, kind: 'event-source', state: 'Enabled' },
    { from: { service: 'lambda', id: 'feed' }, to: { id: 'arn:aws:kinesis:x:1:stream/clicks' }, kind: 'event-source', state: 'Enabled' }],
};
const owners = stackOwners(sinv, links);
eq('a part is placed by type and physical id, a VPC never', [...owners.keys()], ['dynamodb|orders', 'lambda|shop-worker']);
eq('a similar name is not a part', owners.has('dynamodb|orders-copy'), false);
const sm = buildMap(sinv, '', null, links);
eq('one box per stack read', sm.stacks.map(s => [s.name, s.count, s.others, s.kept, !!s.resource]), [['shop', 2, 3, 1, true]]);
eq('a stack part sits in its stack, not its VPC', sm.vpcs.find(v => v.id === 'vpc-a').count, 0);
eq('the stack is its box, not a tile; a stack not read stays a tile', sm.regional.find(c => c.category === 'Management').groups[0].items.map(r => r.id), ['other']);
eq('without links nothing changes', buildMap(sinv).stacks, []);
eq('a search hides an empty stack', buildMap(sinv, 'feed', null, links).stacks, []);
const lw = linksOf(sinv, links, 'lambda', res('shop-worker'));
eq('uses resolves to the listed queue', lw.uses.map(u => u.r ? u.r.name : u.text), ['jobs']);
eq('used by, from the other side', linksOf(sinv, links, 'sqs', { id: 'https://sqs.ap-south-1.amazonaws.com/1/jobs', name: 'jobs' }).usedBy.map(u => u.r.id), ['shop-worker']);
eq('an unlisted source keeps its ARN', linksOf(sinv, links, 'lambda', res('feed')).uses, [{ text: 'arn:aws:kinesis:x:1:stream/clicks', state: 'Enabled' }]);
eq('lines only between listed resources', linePairs(sinv, links), [{ from: 'lambda|shop-worker', to: 'sqs|https://sqs.ap-south-1.amazonaws.com/1/jobs', state: 'Enabled' }]);

eq('a rolled-back stack is explained', stackStateWords('ROLLBACK_COMPLETE').includes('only deleted and deployed again'), true);
eq('a stuck stack is explained', stackStateWords('UPDATE_ROLLBACK_FAILED').includes('stuck'), true);
eq('a healthy stack needs no words', stackStateWords('CREATE_COMPLETE'), '');

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
