// Forgeline web console: blueprints in words (design note 035).
//
// No imports and no DOM, so it runs under Node in tests. It says what each block is and what each link
// does, in the words the canvas and the Blueprints page use, and holds the starting points people pick from.

export const KINDS = {
  frontdoor: { label: 'Front door', tech: 'Load balancer', icon: 'globe' },
  service: { label: 'App', tech: 'Containers (ECS)', icon: 'box' },
  table: { label: 'Table', tech: 'DynamoDB', icon: 'grid' },
  bucket: { label: 'Files', tech: 'S3 bucket', icon: 'folder' },
  queue: { label: 'Queue', tech: 'SQS', icon: 'inbox' },
  logs: { label: 'Logs', tech: 'CloudWatch', icon: 'list' },
  existing: { label: 'Already there', tech: 'kept as it is', icon: 'link' },
};

const EXISTING = { table: 'DynamoDB table', bucket: 'S3 bucket', queue: 'SQS queue' };

/** The small print under a block's name. */
export function blockFacts(b) {
  switch (b.kind) {
    case 'frontdoor': return (b.certificate ? 'HTTPS' : 'HTTP only') + (b.health_path ? ' · checks ' + b.health_path : '');
    case 'service': {
      const vcpu = (b.cpu || 0) / 1024, gb = (b.memory || 0) / 1024;
      return `${b.count || 0} × ${vcpu} vCPU, ${gb} GB · ${b.image}` + (b.port ? ` · port ${b.port}` : '');
    }
    case 'table': return 'key ' + b.partition_key + (b.sort_key ? ', sorted by ' + b.sort_key : '') + ' · pay per request';
    case 'bucket': return 'private · encrypted' + (b.versioning ? ' · keeps old versions' : '');
    case 'queue': return 'encrypted' + (b.retention_days ? ` · keeps messages ${b.retention_days} days` : '');
    case 'logs': return `kept ${b.retention_days} days`;
    case 'existing': return (EXISTING[b.existing?.type] || 'resource') + ' ' + (b.existing?.name || '') + ' · used, never changed';
    default: return '';
  }
}

/** What a link does, as a sentence. */
export function linkWords(bp, l) {
  const by = Object.fromEntries((bp.blocks || []).map(b => [b.id, b]));
  const from = by[l.from], to = by[l.to];
  if (!from || !to) return l.from + ' → ' + l.to;
  if (from.kind === 'frontdoor') return `${from.name} sends traffic to ${to.name}`;
  if (to.kind === 'logs') return `${from.name} writes its logs to ${to.name}`;
  return `${from.name} ${l.access === 'readwrite' ? 'reads and writes' : 'reads'} ${to.name}`;
}

/** Starting points for a new blueprint. Each is valid as it is. */
export const STARTERS = {
  empty: { label: 'Empty', text: 'Start with nothing and add blocks.', blocks: [], links: [] },
  webapp: {
    label: 'Small web app', text: 'A front door, one small app, a table and logs kept 30 days.',
    blocks: [
      { id: 'web', kind: 'frontdoor', name: 'Front door', health_path: '/' },
      { id: 'app', kind: 'service', name: 'App', image: 'nginx:1.29', cpu: 256, memory: 512, count: 1, port: 80 },
      { id: 'data', kind: 'table', name: 'Data', partition_key: 'id', partition_type: 'S' },
      { id: 'logs', kind: 'logs', name: 'Logs', retention_days: 30 },
    ],
    links: [{ from: 'web', to: 'app' }, { from: 'app', to: 'data', access: 'readwrite' }, { from: 'app', to: 'logs' }],
  },
  worker: {
    label: 'Queue worker', text: 'An app that reads jobs from a queue and keeps files in a bucket.',
    blocks: [
      { id: 'jobs', kind: 'queue', name: 'Jobs', retention_days: 4 },
      { id: 'worker', kind: 'service', name: 'Worker', image: 'busybox:1.37', cpu: 256, memory: 512, count: 1 },
      { id: 'files', kind: 'bucket', name: 'Files', versioning: true },
    ],
    links: [{ from: 'worker', to: 'jobs', access: 'readwrite' }, { from: 'worker', to: 'files', access: 'readwrite' }],
  },
};
