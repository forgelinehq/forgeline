// Forgeline web console: an agent's instructions as sections (design note 048).
//
// No imports and no DOM, so it runs under Node in tests. The model still receives one Markdown text, exactly what
// the preview shows: the sections are only a way to write it. parse() reads any Markdown into sections (the text
// before the first "## " heading is who the agent is; each "## " heading starts a section, known by its title or
// kept as one of your own), and compose() writes them back. compose(parse(text)) gives the same text, apart from
// trailing spaces and runs of blank lines, so switching between sections and plain Markdown loses nothing.

/** The sections the console offers, in the order they are written. `list` sections are edited as items. */
export const SECTIONS = [
  { id: 'role', title: '', label: 'Who it is', icon: 'role', hint: 'One or two sentences: its job and who it works for.',
    example: 'You are the on-call helper for the platform team. You explain failed pipeline runs to developers.' },
  { id: 'goal', title: 'Goal', label: 'Goal', icon: 'goal', hint: 'What a good outcome is, in one or two sentences.',
    example: 'Find why the run failed and say what to do next, so the developer can fix it in minutes.', also: ['purpose', 'objective', 'task'] },
  { id: 'steps', title: 'Steps', label: 'Steps', icon: 'steps', list: 'ordered', hint: 'How it works, in order. One step per line.',
    example: 'Read the failed step and its last log lines.', also: ['process', 'how to work', 'how', 'approach', 'procedure'] },
  { id: 'rules', title: 'Rules', label: 'Rules', icon: 'rules', list: 'bullets', hint: 'What it must always or never do.',
    example: 'Never guess a cause the logs do not show.', also: ['constraints', 'guidelines', 'guardrails', 'do and don\'t', 'boundaries'] },
  { id: 'output', title: 'Output', label: 'Output', icon: 'output', hint: 'What its answer looks like: length, shape, tone.',
    example: 'Three short parts: what failed, the likely cause, one next step. Plain words, no more than 120 words.', also: ['output format', 'format', 'answer', 'response', 'response format'],
    presets: [
      ['Short summary', 'A short summary in plain words, at most five sentences.'],
      ['Bullet points', 'A bulleted list, one finding per bullet, most important first.'],
      ['Table', 'A Markdown table with one row per item and a short note under it.'],
      ['JSON', 'Only a JSON object, with no text around it.'],
    ] },
  { id: 'escalate', title: 'When to stop', label: 'When to stop', icon: 'escalate', list: 'bullets', hint: 'When it stops and asks a person instead of going on.',
    example: 'Stop and ask a person if the fix would change production.', also: ['escalation', 'when to ask', 'stop', 'stopping', 'when to stop and ask a person', 'hand off'] },
  { id: 'examples', title: 'Examples', label: 'Examples', icon: 'examples', hint: 'One or two examples of a question and a good answer.',
    example: 'Question: why did build 42 fail?\nAnswer: The test step ran out of memory…', also: ['example', 'sample', 'samples'], optional: true },
  { id: 'context', title: 'Context', label: 'Background', icon: 'context', hint: 'Facts it should know: systems, names, links, conventions.',
    example: 'Our services run on ECS in eu-west-1. Pipelines are named after the repository.', also: ['background', 'knowledge', 'facts'], optional: true },
];

const byID = Object.fromEntries(SECTIONS.map(s => [s.id, s]));

function known(heading) {
  const h = heading.trim().toLowerCase().replace(/[:.]+$/, '');
  return SECTIONS.find(s => s.title && (s.title.toLowerCase() === h || (s.also || []).includes(h)));
}

const ORDERED = /^\s*\d+[.)]\s+(.*)$/;
const BULLET = /^\s*[-*+]\s+(.*)$/;

/** The items of a list body, or null when the body is anything but a plain one-level list. */
export function listItems(body, kind) {
  const lines = body.split('\n').filter(l => l.trim() !== '');
  if (!lines.length) return [];
  const re = kind === 'ordered' ? ORDERED : BULLET;
  const items = [];
  for (const l of lines) {
    const m = l.match(re);
    if (!m || /^\s/.test(l)) return null;
    items.push(m[1].trim());
  }
  // Only numbered 1, 2, 3… in order, so writing it back gives the same text.
  if (kind === 'ordered' && !lines.every((l, i) => l.trim().startsWith((i + 1) + '. '))) return null;
  if (kind === 'bullets' && !lines.every(l => l.startsWith('- '))) return null;
  return items;
}

/** The body of a list section written from its items. */
export function listBody(items, kind) {
  return items.map(x => x.trim()).filter(Boolean).map((x, i) => (kind === 'ordered' ? (i + 1) + '. ' : '- ') + x).join('\n');
}

function tidy(text) {
  return String(text || '').replace(/\r\n?/g, '\n').split('\n').map(l => l.replace(/\s+$/, '')).join('\n').replace(/\n{3,}/g, '\n\n').trim();
}

/**
 * Sections of a Markdown text: [{ key, id, title, body, gap }]. id is a known section's id, or 'custom'. title is the
 * heading as written (empty for the opening text). key is unique within the list, for the editor.
 */
export function parse(markdown) {
  const text = tidy(markdown);
  const out = [];
  let cur = { id: 'role', title: '', lines: [] };
  let inFence = false;
  const push = () => {
    const body = tidy(cur.lines.join('\n'));
    // Whether a blank line follows the heading, so the text is written back the way it was.
    const gap = !(cur.lines.length && cur.lines[0].trim() !== '');
    if (cur.id !== 'role' || body) out.push({ id: cur.id, title: cur.title, body, gap });
  };
  for (const line of text.split('\n')) {
    if (/^\s*(```|~~~)/.test(line)) inFence = !inFence;
    const m = !inFence && line.match(/^## (.+)$/);
    if (m) {
      push();
      const k = known(m[1]);
      cur = { id: k ? k.id : 'custom', title: m[1].trim(), lines: [] };
    } else cur.lines.push(line);
  }
  push();
  // A known section written twice keeps its second copy as one of your own, so nothing is merged or lost.
  const seen = new Set();
  return out.map((s, i) => {
    let id = s.id;
    if (id !== 'custom') { if (seen.has(id)) id = 'custom'; else seen.add(id); }
    return { key: id === 'custom' ? 'custom-' + i : id, id, title: s.title, body: s.body, gap: s.gap };
  });
}

/** The Markdown the model receives. Empty sections are left out. */
export function compose(sections) {
  const parts = [];
  for (const s of sections) {
    const body = tidy(s.body);
    if (s.id === 'role') { if (body) parts.push(body); continue; }
    const title = (s.title || (byID[s.id] && byID[s.id].title) || '').trim();
    if (!title || !body) continue;
    parts.push('## ' + title + (s.gap === false ? '\n' : '\n\n') + body);
  }
  return parts.join('\n\n');
}

/** Puts a new section where it belongs among the known ones, after any of your own that come before it. */
export function addSection(sections, id) {
  if (sections.some(s => s.id === id)) return sections;
  const order = SECTIONS.map(s => s.id);
  const at = sections.findIndex(s => s.id !== 'custom' && order.indexOf(s.id) > order.indexOf(id));
  const next = { key: id, id, title: id === 'role' ? '' : byID[id].title, body: '' };
  return at < 0 ? [...sections, next] : [...sections.slice(0, at), next, ...sections.slice(at)];
}

/** Adds a section of your own with a title. */
export function addCustom(sections, title) {
  const t = String(title || '').trim().replace(/^#+\s*/, '');
  if (!t) return sections;
  return [...sections, { key: 'custom-' + Date.now().toString(36) + sections.length, id: 'custom', title: t, body: '' }];
}

/** Sections an editor shows: what the text has, plus the standard ones not yet written (empty), in order. */
export function withStandard(sections) {
  let out = sections;
  for (const s of SECTIONS) if (!s.optional) out = addSection(out, s.id);
  return out;
}

/** The known section a parsed one is, or null for one of your own. */
export const sectionInfo = (id) => byID[id] || null;
