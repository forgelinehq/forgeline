// Forgeline web console: the data model behind the AWS map (design note 023).
//
// No imports and no DOM, so it runs under Node in tests. It turns the explorer's inventory
// (`GET /v1/infra/aws/resources`) into a layout. It only uses facts the inventory states: a
// resource is placed inside a VPC when its own "VPC" field names one, and a resource is flagged
// when its state or one of its fields says so. It never infers a relationship or a health.

export const CATEGORIES = ['Compute', 'Containers', 'Storage', 'Databases', 'Network', 'Messaging', 'Management'];

/**
 * What a resource state means. AWS names states differently per service, so match by meaning.
 * Returns 'none' (no state given), 'ok', 'bad', 'wait' or 'idle'.
 */
export function stateKind(state) {
  if (!state) return 'none';
  const s = String(state).toLowerCase();
  if (/^(running|active|available|in-use|in_use|enabled|healthy|.*_complete|create_complete|update_complete)$/.test(s) && !/rollback|delete/.test(s)) return 'ok';
  if (/fail|error|rollback|impaired|deleted|inactive|incompatible/.test(s)) return 'bad';
  if (/pending|creating|provisioning|modifying|starting|updating|stopping|in_progress|in-progress|backing/.test(s)) return 'wait';
  return 'idle';
}

/** The value of a labelled field, or ''. */
export function field(r, label) {
  const f = (r.fields || []).find(x => x.label === label);
  return f ? f.value : '';
}

// Which alarm dimensions name a resource (design note 023, step 2). An alarm belongs to a resource only
// when its namespace and its own dimensions say so; nothing is matched by similar names.
const ALARM_KEYS = {
  ec2: [['AWS/EC2'], r => ({ InstanceId: r.id })],
  ebs: [['AWS/EBS'], r => ({ VolumeId: r.id })],
  rds: [['AWS/RDS'], r => ({ DBInstanceIdentifier: r.id })],
  lambda: [['AWS/Lambda'], r => ({ FunctionName: r.id })],
  dynamodb: [['AWS/DynamoDB'], r => ({ TableName: r.name })],
  sqs: [['AWS/SQS'], r => ({ QueueName: r.name })],
  s3: [['AWS/S3'], r => ({ BucketName: r.id })],
  elb: [['AWS/ApplicationELB', 'AWS/NetworkELB'], r => { const i = (r.arn || '').indexOf('loadbalancer/'); return i < 0 ? null : { LoadBalancer: r.arn.slice(i + 13) }; }],
  'ecs-services': [['AWS/ECS'], r => ({ ServiceName: r.name, ClusterName: field(r, 'Cluster') })],
  ecs: [['AWS/ECS'], r => ({ ClusterName: r.name }), ['ServiceName']],
};

/** The alarms whose own dimensions name this resource. */
export function alarmsFor(alarms, svcId, r) {
  const k = ALARM_KEYS[svcId];
  if (!k || !alarms || !alarms.length) return [];
  const want = k[1](r);
  if (!want || Object.values(want).some(v => !v)) return [];
  const absent = k[2] || [];
  return alarms.filter(a => {
    if (!k[0].includes(a.namespace)) return false;
    const dims = new Map((a.dimensions || []).map(d => [d.name, d.value]));
    return Object.entries(want).every(([n, v]) => dims.get(n) === v) && absent.every(n => !dims.has(n));
  });
}

/**
 * What the alarms say about a resource: 'alarm' (one is in ALARM), 'nodata', 'ok' (it has alarms and
 * every one is OK) or 'none' (no alarm names it). 'none' is not healthy: nothing was measured.
 */
export function health(alarms, svcId, r) {
  const mine = alarmsFor(alarms, svcId, r);
  let kind = 'none';
  if (mine.length) kind = mine.some(a => a.state === 'ALARM') ? 'alarm' : mine.some(a => a.state !== 'OK') ? 'nodata' : 'ok';
  return { kind, alarms: mine };
}

/** Reasons a resource deserves a look, from its state, its fields and (when given) its alarms. Level is 'bad' or 'warn'. */
export function attention(svcId, r, alarms = null) {
  const out = [];
  for (const a of alarmsFor(alarms, svcId, r)) if (a.state === 'ALARM') out.push({ level: 'bad', text: `Alarm: ${a.name}` });
  const kind = stateKind(r.state);
  if (kind === 'bad') out.push({ level: 'bad', text: `State: ${r.state}` });
  else if (kind === 'wait') out.push({ level: 'warn', text: `State: ${r.state}` });
  const open = field(r, 'Open to the internet');
  if (svcId === 'sg' && open) out.push({ level: 'warn', text: `Open to the internet: ${open}` });
  // An EBS volume that is "available" is not attached to any instance.
  if (svcId === 'ebs' && String(r.state).toLowerCase() === 'available') out.push({ level: 'warn', text: 'Not attached to any instance' });
  if ((svcId === 'ebs' || svcId === 'rds') && field(r, 'Encrypted') === 'false') out.push({ level: 'warn', text: 'Not encrypted' });
  if (svcId === 'rds' && field(r, 'Publicly accessible') === 'true') out.push({ level: 'warn', text: 'Publicly accessible' });
  return out;
}

/** Does the resource match a search the way the list view does? */
export function matches(r, query) {
  if (!query) return true;
  const q = query.toLowerCase();
  return [r.name, r.id, r.state, r.arn, ...(r.fields || []).map(f => f.value), ...Object.entries(r.tags || {}).flat()]
    .some(v => v && String(v).toLowerCase().includes(q));
}

function addTo(groups, svc, r) {
  let g = groups.find(x => x.svc.id === svc.id);
  if (!g) { g = { svc, items: [] }; groups.push(g); }
  g.items.push(r);
}

/**
 * Lays the inventory out as { vpcs, regional, attention, total }.
 *   vpcs:      one entry per VPC with the resources whose "VPC" field names it
 *   regional:  everything else, by category
 *   attention: flagged resources, failing ones first
 * Services that could not be listed or are empty are left out; the caller reports those itself.
 */
export function buildMap(inv, query = '', alarms = null) {
  const services = (inv.services || []).filter(s => s.count > 0 && !s.error);
  const vpcs = [];
  const byId = new Map();
  const vpcSvc = services.find(s => s.id === 'vpc');
  for (const r of vpcSvc ? vpcSvc.resources : []) {
    const v = { id: r.id, name: r.name || r.id, cidr: field(r, 'CIDR'), isDefault: field(r, 'Default') === 'true', synthetic: false, groups: [], count: 0, resource: r, svc: vpcSvc };
    vpcs.push(v);
    byId.set(r.id, v);
  }
  const regionalByCat = new Map();
  const flagged = [];
  let total = 0;
  for (const svc of services) {
    for (const r of svc.resources) {
      if (!matches(r, query)) continue;
      total++;
      const why = attention(svc.id, r, alarms);
      if (why.length) flagged.push({ svc, r, why });
      if (svc.id === 'vpc') continue; // a VPC is the container, not an item in one
      const vid = field(r, 'VPC');
      if (vid) {
        let v = byId.get(vid);
        if (!v) { v = { id: vid, name: vid, cidr: '', isDefault: false, synthetic: true, groups: [], count: 0, resource: null, svc: null }; vpcs.push(v); byId.set(vid, v); }
        addTo(v.groups, svc, r);
        v.count++;
      } else {
        if (!regionalByCat.has(svc.category)) regionalByCat.set(svc.category, []);
        addTo(regionalByCat.get(svc.category), svc, r);
      }
    }
  }
  const cats = [...CATEGORIES, ...[...regionalByCat.keys()].filter(c => !CATEGORIES.includes(c))];
  const regional = cats.filter(c => regionalByCat.has(c)).map(c => ({ category: c, groups: regionalByCat.get(c) }));
  flagged.sort((a, b) => (b.why.some(w => w.level === 'bad') ? 1 : 0) - (a.why.some(w => w.level === 'bad') ? 1 : 0));
  // With a search active, hide containers that hold nothing that matches.
  const shown = query ? vpcs.filter(v => v.count > 0 || matches(v.resource || { name: v.name, id: v.id }, query)) : vpcs;
  return { vpcs: shown, regional, attention: flagged, total };
}
