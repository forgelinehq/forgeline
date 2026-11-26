// The docs must not rot quietly (node --test): every relative link, anchor-less page link and picture in docs/ must
// point at something that exists, and every picture file must be used by some page. Runs in CI with the sync tests.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const DOCS = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..', 'docs');
const walk = (d) => fs.readdirSync(d, { withFileTypes: true }).flatMap((e) => e.isDirectory() ? walk(path.join(d, e.name)) : [path.join(d, e.name)]);
const pages = walk(DOCS).filter((f) => f.endsWith('.md'));

test('every relative link in docs/ points at a file that exists', () => {
  const bad = [];
  for (const f of pages) {
    for (const m of fs.readFileSync(f, 'utf8').matchAll(/\]\((?!https?:|mailto:|#|\/|\{)([^)\s#]+)/g)) {
      if (!fs.existsSync(path.resolve(path.dirname(f), m[1]))) bad.push(`${path.relative(DOCS, f)} -> ${m[1]}`);
    }
  }
  assert.deepEqual(bad, [], 'fix or remove these links');
});

test('every picture a page shows exists, and every picture file is shown somewhere', () => {
  const shown = new Set(), bad = [];
  for (const f of pages) {
    const text = fs.readFileSync(f, 'utf8');
    for (const m of text.matchAll(/\{\{ '\/console\/([^']+)' \| relative_url \}\}/g)) shown.add(m[1]);
    for (const m of text.matchAll(/\]\((?!https?:|\{|\/)([^)\s#]+\.(?:png|jpe?g|gif|svg|webp))/g)) shown.add(path.basename(m[1]));
  }
  const files = fs.readdirSync(path.join(DOCS, 'console')).filter((n) => /\.(png|jpe?g|gif|svg|webp)$/.test(n));
  for (const n of shown) if (!files.includes(n)) bad.push('missing: ' + n);
  for (const n of files) if (!shown.has(n)) bad.push('unused: ' + n + ' (delete it; git keeps the history)');
  assert.deepEqual(bad, []);
});
