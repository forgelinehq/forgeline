// Forgeline web console: editing a blueprint on the design canvas (design note 035, slice 2).
//
// No imports and no DOM, so it runs under Node in tests. Every function returns a new blueprint and leaves the
// one it was given alone, so the canvas can keep the last saved version to compare with. The link rules mirror
// internal/blueprint (Go), which stays the authority: the server checks every save.

export const BLOCK_W = 190, BLOCK_H = 66;
const COL_X = 96, COL_W = 250, ROW_Y = 40, ROW_H = 100;

// What a new block of each kind starts as. Each is valid on its own.
export const DEFAULTS = {
  frontdoor: { name: 'Front door', health_path: '/' },
  service: { name: 'App', image: 'nginx:1.29', cpu: 256, memory: 512, count: 1, port: 80 },
  table: { name: 'Table', partition_key: 'id', partition_type: 'S' },
  bucket: { name: 'Files', versioning: true },
  queue: { name: 'Queue', retention_days: 4 },
  logs: { name: 'Logs', retention_days: 30 },
  existing: { name: 'Existing table', existing: { type: 'table', name: 'my-table' } },
};
const ID_BASE = { frontdoor: 'door', service: 'app', table: 'table', bucket: 'files', queue: 'queue', logs: 'logs', existing: 'existing' };
// Which column a kind sits in: front doors, then apps, then what apps use.
const COLUMN = { frontdoor: 0, service: 1 };

// Fargate's memory sizes (MiB) per CPU size, as in internal/blueprint.
export const FARGATE = {
  256: [512, 1024, 2048], 512: [1024, 2048, 3072, 4096], 1024: [2048, 3072, 4096, 5120, 6144, 7168, 8192],
  2048: Array.from({ length: 13 }, (_, i) => 4096 + i * 1024), 4096: Array.from({ length: 23 }, (_, i) => 8192 + i * 1024),
};
export const LOG_DAYS = [1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653];

const clone = (bp) => JSON.parse(JSON.stringify(bp));
const colOf = (kind) => COLUMN[kind] ?? 2;

/** Gives every block a place when none has one yet (a blueprint made from a starting point or by the API). */
export function autoLayout(bp) {
  const out = clone(bp);
  if (out.blocks.some(b => b.x || b.y)) return out;
  const rows = [0, 0, 0];
  for (const b of out.blocks) {
    const c = colOf(b.kind);
    b.x = COL_X + c * COL_W;
    b.y = ROW_Y + rows[c]++ * ROW_H;
  }
  return out;
}

/** A free spot in the block's column, below what is there. */
function spotFor(bp, kind) {
  const c = colOf(kind), x = COL_X + c * COL_W;
  const below = bp.blocks.filter(b => Math.abs(b.x - x) < COL_W / 2).reduce((m, b) => Math.max(m, b.y + ROW_H), ROW_Y);
  return { x, y: below };
}

export function newID(bp, kind) {
  const base = ID_BASE[kind] || 'block';
  const taken = new Set(bp.blocks.map(b => b.id));
  if (!taken.has(base)) return base;
  for (let n = 2; ; n++) if (!taken.has(base + '-' + n)) return base + '-' + n;
}

/** Adds a block of a kind with its defaults; returns { bp, id }. */
export function addBlock(bp, kind) {
  const out = clone(bp);
  const id = newID(out, kind);
  const d = clone(DEFAULTS[kind]);
  const same = out.blocks.filter(b => b.kind === kind).length;
  if (same) d.name += ' ' + (same + 1);
  out.blocks.push({ id, kind, ...d, ...spotFor(out, kind) });
  return { bp: out, id };
}

/** Removes a block and every link to or from it. */
export function removeBlock(bp, id) {
  const out = clone(bp);
  out.blocks = out.blocks.filter(b => b.id !== id);
  out.links = out.links.filter(l => l.from !== id && l.to !== id);
  return out;
}

/** Changes fields of one block. A field set to '' , null or undefined is removed (Go treats it as unset). */
export function updateBlock(bp, id, fields) {
  const out = clone(bp);
  const b = out.blocks.find(x => x.id === id);
  if (!b) return out;
  for (const [k, v] of Object.entries(fields)) {
    if (v === '' || v === null || v === undefined || v === false || (typeof v === 'number' && Number.isNaN(v))) delete b[k];
    else b[k] = v;
  }
  return out;
}

export function moveBlock(bp, id, x, y) {
  return updateBlock(bp, id, { x: Math.max(0, Math.round(x)), y: Math.max(0, Math.round(y)) });
}

/**
 * What a block may link to now: [{ id, name, access }] where access says the link needs read or readwrite.
 * The same rules as the server: a front door sends traffic to one service that has a port and no other front
 * door; a service uses data (once each) and writes logs to at most one logs block.
 */
export function linkTargets(bp, fromId) {
  const from = bp.blocks.find(b => b.id === fromId);
  if (!from) return [];
  const linked = new Set(bp.links.filter(l => l.from === fromId).map(l => l.to));
  const kindOf = Object.fromEntries(bp.blocks.map(b => [b.id, b.kind]));
  if (from.kind === 'frontdoor') {
    if (linked.size) return [];
    const fronted = new Set(bp.links.filter(l => kindOf[l.from] === 'frontdoor').map(l => l.to));
    return bp.blocks.filter(b => b.kind === 'service' && b.port && !fronted.has(b.id)).map(b => ({ id: b.id, name: b.name, access: false }));
  }
  if (from.kind !== 'service') return [];
  const hasLogs = bp.links.some(l => l.from === fromId && kindOf[l.to] === 'logs');
  return bp.blocks.filter(b => !linked.has(b.id) && (['table', 'bucket', 'queue', 'existing'].includes(b.kind) || (b.kind === 'logs' && !hasLogs)))
    .map(b => ({ id: b.id, name: b.name, access: b.kind !== 'logs' }));
}

export function addLink(bp, from, to, access) {
  const out = clone(bp);
  const l = { from, to };
  if (access) l.access = access;
  out.links.push(l);
  return out;
}

export function removeLink(bp, from, to) {
  const out = clone(bp);
  out.links = out.links.filter(l => !(l.from === from && l.to === to));
  return out;
}

/** The body PUT /v1/blueprints/{id} takes. */
export function toBody(bp, revision) {
  return { name: bp.name, description: bp.description || '', target: bp.target, blocks: bp.blocks, links: bp.links, revision };
}

/** The drawn path of a link: from the right edge of one block to the left edge of the other. */
export function linkPath(a, b) {
  const x1 = a.x + BLOCK_W, y1 = a.y + BLOCK_H / 2, x2 = b.x, y2 = b.y + BLOCK_H / 2;
  if (x2 >= x1) {
    const mid = (x1 + x2) / 2;
    return `M${x1} ${y1} C${mid} ${y1}, ${mid} ${y2}, ${x2} ${y2}`;
  }
  // The target is to the left: leave from the right, loop round and come in from the left.
  return `M${x1} ${y1} C${x1 + 60} ${y1}, ${x2 - 60} ${y2}, ${x2} ${y2}`;
}

/** How big the drawing must be to hold every block, with a margin. */
export function bounds(bp) {
  let w = 600, hgt = 320;
  for (const b of bp.blocks) { w = Math.max(w, b.x + BLOCK_W + 40); hgt = Math.max(hgt, b.y + BLOCK_H + 60); }
  return { w, h: hgt };
}

/** Text cut to fit a block, with an ellipsis. */
export function fit(text, n) {
  const s = String(text || '');
  return s.length > n ? s.slice(0, n - 1) + '…' : s;
}

/** A server message with blocks[2] and links[0] replaced by the names people see on the canvas. */
export function explainError(bp, msg) {
  const name = (id) => bp.blocks.find(b => b.id === id)?.name || id;
  return String(msg || '')
    .replace(/blocks\[(\d+)\]/g, (m, i) => bp.blocks[i] ? '“' + bp.blocks[i].name + '”' : m)
    .replace(/links\[(\d+)\]/g, (m, i) => bp.links[i] ? 'the link from “' + name(bp.links[i].from) + '” to “' + name(bp.links[i].to) + '”' : m);
}

// ---------------------------------------------------------------- previews (slice 3)

const WORST = { remove: 4, replace: 3, change: 2, add: 1 };

/** What a preview does to each block: { blockId: 'add' | 'change' | 'replace' | 'remove' }, the worst of its resources. */
export function planByBlock(plan) {
  const out = {};
  for (const c of plan?.changes || []) {
    if (!c.block) continue;
    if (!out[c.block] || WORST[c.action] > WORST[out[c.block]]) out[c.block] = c.action;
  }
  return out;
}

/**
 * Where a preview stands for the blueprint on screen: 'none', 'planning', 'failed', 'stale' (the blueprint
 * changed since, or it is over an hour old), 'nothing' (ready, changes nothing) or 'ready'.
 */
export function planState(plan, revision, dirty, now = Date.now()) {
  if (!plan) return 'none';
  if (plan.status === 'planning') return 'planning';
  if (plan.status === 'failed') return 'failed';
  if (dirty || plan.revision !== revision || !plan.finished_at || now - new Date(plan.finished_at).getTime() > 3600e3) return 'stale';
  return plan.add + plan.change + plan.remove ? 'ready' : 'nothing';
}

/** The preview's one-line summary. */
export function planWords(plan) {
  const parts = [];
  if (plan.add) parts.push(plan.add + ' to add');
  if (plan.change) parts.push(plan.change + ' to change');
  if (plan.remove) parts.push(plan.remove + ' to remove');
  return parts.length ? parts.join(', ') : 'Nothing to change: AWS already matches the blueprint';
}

/** Places blocks that have no position yet (ones the assistant added), below what is in their column. */
export function placeNew(bp) {
  if (bp.blocks.every(b => !b.x && !b.y)) return autoLayout(bp);
  const out = clone(bp);
  const placed = { ...out, blocks: out.blocks.filter(b => b.x || b.y) };
  for (const b of out.blocks) {
    if (b.x || b.y) continue;
    Object.assign(b, spotFor(placed, b.kind));
    placed.blocks.push(b);
  }
  return out;
}

/**
 * Which way things flow along a link, for the moving dots: 'forward' (from the first block to the second), 'back', or
 * 'both'. A front door sends traffic to its app; an app writes logs; reading data brings it back to the app; reading
 * and writing goes both ways. This is what the blueprint configures, not measured traffic.
 */
export function flowOf(bp, link) {
  const from = bp.blocks.find(b => b.id === link.from), to = bp.blocks.find(b => b.id === link.to);
  if (!from || !to) return null;
  if (from.kind === 'frontdoor' || to.kind === 'logs') return 'forward';
  return link.access === 'readwrite' ? 'both' : 'back';
}
