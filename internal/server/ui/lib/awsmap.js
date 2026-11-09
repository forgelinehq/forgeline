// Forgeline web console: the data model behind the AWS map (design note 023).
//
// No imports and no DOM, so it runs under Node in tests. It turns the explorer's inventory
// (`GET /v1/infra/aws/resources`) into a layout. It only uses facts the inventory states: a
// resource is placed inside a VPC when its own "VPC" field names one, and a resource is flagged
// when its state or one of its fields says so. It never infers a relationship or a health.

export const CATEGORIES = ['Compute', 'Containers', 'Storage', 'Databases', 'Network', 'Messaging', 'AI', 'Management'];

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

/** What a CloudFormation stack state means for the person looking at it, or '' when it needs no words. */
export function stackStateWords(state) {
  const s = String(state || '').toUpperCase();
  if (s === 'ROLLBACK_COMPLETE') return 'Its first deploy failed and AWS undid it. A stack in this state cannot be updated, only deleted and deployed again.';
  if (s === 'UPDATE_ROLLBACK_COMPLETE') return 'Its last update failed and AWS put back the version before, which is what runs now.';
  if (s.endsWith('_FAILED')) return 'AWS could not finish the last change to this stack, and it is stuck until someone acts on it.';
  if (s.endsWith('_IN_PROGRESS')) return 'AWS is changing this stack right now.';
  return '';
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

// What CloudFormation calls a resource type, as the inventory's service id (design note 034). A VPC is left
// out: it stays a container of its own. Types not here are counted as parts the map does not list.
export const CFN_TYPES = {
  'AWS::DynamoDB::Table': 'dynamodb', 'AWS::Lambda::Function': 'lambda', 'AWS::S3::Bucket': 's3', 'AWS::EC2::Instance': 'ec2',
  'AWS::EC2::Volume': 'ebs', 'AWS::EC2::SecurityGroup': 'sg', 'AWS::ECR::Repository': 'ecr', 'AWS::RDS::DBInstance': 'rds',
  'AWS::SQS::Queue': 'sqs', 'AWS::SNS::Topic': 'sns', 'AWS::Logs::LogGroup': 'logs', 'AWS::ElasticLoadBalancingV2::LoadBalancer': 'elb',
  'AWS::ECS::Cluster': 'ecs', 'AWS::ECS::Service': 'ecs-services',
};

/** An index from "service|id, name or ARN" to { svc, r }, so a link or a stack part finds its resource exactly. */
export function indexResources(inv) {
  const idx = new Map();
  for (const svc of inv.services || []) for (const r of svc.resources || []) {
    for (const k of [r.id, r.name, r.arn]) if (k && !idx.has(svc.id + '|' + k)) idx.set(svc.id + '|' + k, { svc, r });
  }
  return idx;
}

/**
 * Which stack made each resource, from `GET /v1/infra/aws/links`: a Map from keyOf-style "svc|id" to
 * { stack, part }. Only parts whose type and physical id name a listed resource are placed.
 */
export function stackOwners(inv, links) {
  const idx = indexResources(inv), out = new Map();
  for (const st of links?.stacks || []) for (const p of st.parts || []) {
    const svcId = CFN_TYPES[p.type];
    const hit = svcId && p.physical ? idx.get(svcId + '|' + p.physical) : null;
    if (hit) out.set(hit.svc.id + '|' + hit.r.id, { stack: st.name, part: p });
  }
  return out;
}

/**
 * What a resource uses and what uses it, from the links AWS states: { uses: [end], usedBy: [end] } where an
 * end is { svc, r } for a listed resource or { text } (its ARN) for one the map does not list, plus `state`.
 */
export function linksOf(inv, links, svcId, r) {
  const idx = indexResources(inv), uses = [], usedBy = [];
  const me = e => e.service === svcId && (e.id === r.id || e.id === r.name || e.id === r.arn);
  const resolve = e => (e.service && idx.get(e.service + '|' + e.id)) || { text: e.id };
  for (const l of links?.links || []) {
    if (me(l.from)) uses.push({ ...resolve(l.to), state: l.state });
    else if (me(l.to)) usedBy.push({ ...resolve(l.from), state: l.state });
  }
  return { uses, usedBy };
}

/** The pairs of map keys ("svc|id") to draw a line between, for the links AWS states between listed resources. */
export function linePairs(inv, links) {
  const idx = indexResources(inv), out = [];
  for (const l of links?.links || []) {
    const a = l.from.service && idx.get(l.from.service + '|' + l.from.id), b = l.to.service && idx.get(l.to.service + '|' + l.to.id);
    if (a && b) out.push({ from: a.svc.id + '|' + a.r.id, to: b.svc.id + '|' + b.r.id, state: l.state });
  }
  return out;
}

function addTo(groups, svc, r) {
  let g = groups.find(x => x.svc.id === svc.id);
  if (!g) { g = { svc, items: [] }; groups.push(g); }
  g.items.push(r);
}

/**
 * Lays the inventory out as { stacks, vpcs, regional, attention, total }.
 *   stacks:    one entry per CloudFormation stack that was read (when `links` is given), with the listed
 *              resources it made; a resource a stack made sits in its stack, not in its VPC
 *   vpcs:      one entry per VPC with the resources whose "VPC" field names it
 *   regional:  everything else, by category
 *   attention: flagged resources, failing ones first
 * Services that could not be listed or are empty are left out; the caller reports those itself.
 */
export function buildMap(inv, query = '', alarms = null, links = null) {
  const services = (inv.services || []).filter(s => s.count > 0 && !s.error);
  const vpcs = [];
  const byId = new Map();
  const vpcSvc = services.find(s => s.id === 'vpc');
  for (const r of vpcSvc ? vpcSvc.resources : []) {
    const v = { id: r.id, name: r.name || r.id, cidr: field(r, 'CIDR'), isDefault: field(r, 'Default') === 'true', synthetic: false, groups: [], count: 0, resource: r, svc: vpcSvc };
    vpcs.push(v);
    byId.set(r.id, v);
  }
  // Stacks that were read become containers; the stack's own entry in the inventory is their header.
  const owners = links ? stackOwners(inv, links) : new Map();
  const stacks = [], stackBy = new Map();
  const cfn = services.find(s => s.id === 'cfn');
  for (const st of links?.stacks || []) {
    const res = cfn?.resources.find(r => r.id === st.name) || null;
    const listed = [...owners.values()].filter(o => o.stack === st.name).length;
    const v = { id: st.name, name: st.name, resource: res, svc: cfn || null, error: st.error || '', groups: [], count: 0,
      others: (st.parts || []).length - listed, kept: (st.parts || []).filter(p => p.status === 'DELETE_SKIPPED').length };
    stacks.push(v);
    stackBy.set(st.name, v);
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
      if (svc.id === 'cfn' && stackBy.has(r.id)) continue; // shown as its box
      const own = owners.get(svc.id + '|' + r.id);
      if (own && stackBy.has(own.stack)) {
        const st = stackBy.get(own.stack);
        addTo(st.groups, svc, r);
        st.count++;
        continue;
      }
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
  const shownStacks = query ? stacks.filter(v => v.count > 0 || matches(v.resource || { name: v.name, id: v.id }, query)) : stacks;
  return { stacks: shownStacks, vpcs: shown, regional, attention: flagged, total };
}

/** The family a stack's name belongs to: its first two words (datadog-forwarder-irsvc-dryrun is datadog-forwarder). */
export function familyKey(name) {
  const words = String(name || '').split(/[-_]/).filter(Boolean);
  return words.length >= 3 ? words.slice(0, 2).join('-') : '';
}

/**
 * Sorts stacks for a large account (note 040's map, made to fit): stacks that made nothing on the map and need no
 * look go to `quiet`; stacks whose names share a family with at least `min` others become one family; the rest stay
 * single. Order is kept within each list. A stack in trouble is never quiet.
 */
export function arrangeStacks(stacks, alarms = null, min = 3) {
  const quiet = [], rest = [];
  for (const v of stacks) {
    const troubled = v.error || (v.resource && attention('cfn', v.resource, alarms).length);
    (v.count === 0 && !troubled ? quiet : rest).push(v);
  }
  const byKey = new Map();
  for (const v of rest) {
    const k = familyKey(v.name);
    if (!k) continue;
    if (!byKey.has(k)) byKey.set(k, []);
    byKey.get(k).push(v);
  }
  const families = [], single = [], placed = new Set();
  for (const v of rest) {
    const k = familyKey(v.name), fam = k && byKey.get(k);
    if (fam && fam.length >= min) {
      if (!placed.has(k)) { placed.add(k); families.push({ key: k, stacks: fam, count: fam.reduce((n, x) => n + x.count, 0) }); }
    } else single.push(v);
  }
  return { families, single, quiet };
}

/** A stack's contents in one line: "Lambda functions 1 · S3 buckets 1". */
export function contentsLine(v) {
  return v.groups.map(g => g.svc.label + ' ' + g.items.length).join(' · ');
}
