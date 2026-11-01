// Forgeline web console: a small Markdown parser and the advisory checks for instructions.
//
// This file has no imports and touches no DOM, so it can be tested under Node. It only produces
// plain data (blocks and spans). Rendering is in mdview.js and goes through h(), which creates
// text nodes only. Nothing here understands links, images or raw HTML: such text stays text.

/** Splits one line of text into plain, `code` and **bold** spans. */
export function parseInline(s) {
  const out = [];
  const re = /`([^`\n]+)`|\*\*([^*\n]+)\*\*/g;
  let last = 0, m;
  while ((m = re.exec(s))) {
    if (m.index > last) out.push({ t: 'text', text: s.slice(last, m.index) });
    out.push(m[1] !== undefined ? { t: 'code', text: m[1] } : { t: 'b', text: m[2] });
    last = re.lastIndex;
  }
  if (last < s.length) out.push({ t: 'text', text: s.slice(last) });
  return out;
}

/**
 * Parses text into blocks: h (level 1-3), p, ul, ol (items with an optional task state), quote,
 * code and hr. Line breaks inside a paragraph are kept, so plain text written before this editor
 * existed still reads the way it was written.
 */
export function parseBlocks(src) {
  const lines = String(src).replace(/\r\n?/g, '\n').split('\n');
  const blocks = [];
  let para = [];
  const flush = () => { if (para.length) { blocks.push({ t: 'p', text: para.join('\n') }); para = []; } };
  let i = 0;
  while (i < lines.length) {
    const ln = lines[i];
    let m;
    if (/^```/.test(ln)) {
      flush();
      const lang = ln.slice(3).trim(), buf = [];
      i++;
      while (i < lines.length && !/^```/.test(lines[i])) buf.push(lines[i++]);
      i++; // the closing fence, or the end of the text
      blocks.push({ t: 'code', lang, text: buf.join('\n') });
      continue;
    }
    if ((m = ln.match(/^(#{1,3})\s+(.*)$/))) { flush(); blocks.push({ t: 'h', level: m[1].length, text: m[2] }); i++; continue; }
    if (/^\s*---\s*$/.test(ln)) { flush(); blocks.push({ t: 'hr' }); i++; continue; }
    if (/^>/.test(ln)) {
      flush();
      const buf = [];
      while (i < lines.length && /^>/.test(lines[i])) buf.push(lines[i++].replace(/^>\s?/, ''));
      blocks.push({ t: 'quote', text: buf.join('\n') });
      continue;
    }
    if (/^\s*(?:[-*]|\d+\.)\s+/.test(ln)) {
      flush();
      const ordered = /^\s*\d+\./.test(ln), items = [];
      while (i < lines.length && (m = lines[i].match(/^\s*(?:[-*]|\d+\.)\s+(.*)$/))) {
        let text = m[1], task = null;
        const c = text.match(/^\[([ xX])\]\s+(.*)$/);
        if (c) { task = c[1] !== ' '; text = c[2]; }
        items.push({ text, task });
        i++;
      }
      blocks.push({ t: ordered ? 'ol' : 'ul', items });
      continue;
    }
    if (ln.trim() === '') { flush(); i++; continue; }
    para.push(ln);
    i++;
  }
  flush();
  return blocks;
}

// Words that describe changing things. Tier T0 agents only read and advise.
const CHANGE_WORDS = /\b(delete|terminate|destroy|restart|reboot|deploy|scale (?:up|down)|roll ?back)\b/i;
const STOP_WORDS = /\b(stop|ask|confirm|approval|approve|propos(?:e|al))\b/i;
const CREDENTIAL = /AKIA[0-9A-Z]{16}|-----BEGIN [A-Z ]*PRIVATE KEY-----|\bgh[pousr]_[A-Za-z0-9]{20,}|\bxox[abp]-[A-Za-z0-9-]{10,}/;

/**
 * Advisory checks for agent instructions or skill steps. Returns [{level, text}] where level is
 * 'warn' or 'bad', or a single 'ok' entry when nothing is wrong. These never block a save; the
 * server validates what it receives.
 */
export function checkInstructions(text, { tier = '', max = 0 } = {}) {
  const out = [];
  const t = String(text);
  if (t.trim() === '') return [{ level: 'warn', text: 'Say what this is for. An empty text gives the agent nothing to follow.' }];
  if (CREDENTIAL.test(t)) out.push({ level: 'bad', text: 'This looks like a credential. Text here is stored as plain text and shown to developers. Use Secrets instead.' });
  // Quoted rules ("never delete ...") and code samples are not instructions to act.
  const acting = parseBlocks(t).filter(b => b.t !== 'quote' && b.t !== 'code')
    .map(b => b.text || (b.items || []).map(i => i.text).join('\n')).join('\n');
  const m = tier === 'T0' ? acting.match(CHANGE_WORDS) : null;
  if (m) out.push({ level: 'warn', text: `Tier T0 only reads and advises, but the text mentions "${m[0].toLowerCase()}". Use tier T1 if it should file proposals, or say that it only recommends.` });
  if (!STOP_WORDS.test(t)) out.push({ level: 'warn', text: 'Say when to stop and ask a person.' });
  if (max && t.length > max * 0.9) out.push({ level: 'warn', text: `${t.length} of ${max} characters used.` });
  return out.length ? out : [{ level: 'ok', text: 'No problems found.' }];
}
