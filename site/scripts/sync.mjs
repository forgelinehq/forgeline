// Copies the Markdown in ../docs into the Starlight site, so docs/ stays the one place pages are written (and keeps
// reading well on GitHub). No dependencies: it runs before `astro build`.
//
//  - Each page gets a `title`: its front-matter title, or its first `# ` heading (which is then removed, because
//    Starlight prints the title as the page heading). Jekyll-only keys (nav_order, parent, permalink...) are dropped.
//  - `{{ '/path' | relative_url }}` becomes the path under the site's base.
//  - Kramdown callouts (`{: .note }` before a paragraph) become Starlight asides.
//  - Links to other .md files in docs/ become links to their pages; links that leave docs/ go to GitHub.
//  - Screenshots in docs/console are copied to public/console.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const SITE = path.resolve(here, '..');
const DOCS = path.resolve(SITE, '..', 'docs');
const OUT = path.join(SITE, 'src', 'content', 'docs');
const PUBLIC = path.join(SITE, 'public');
const BASE = '/forgeline';
const REPO = 'https://github.com/forgelinehq/forgeline/blob/main/';
// Written for people working on Forgeline, or replaced by a page of the site itself.
const SKIP = new Set(['PLAN.md', 'index.md']);
const KEEP = new Set(['title', 'description', 'template', 'hero', 'sidebar', 'tableOfContents', 'banner', 'draft']);
const ASIDE = { note: 'note', important: 'tip', warning: 'caution', tip: 'tip', caution: 'caution', danger: 'danger' };

/** The page's address under the site, from its path in docs/ (lower case; README and index are the folder). */
export function slugOf(rel) {
  let s = rel.replace(/\\/g, '/').replace(/\.md$/i, '').toLowerCase();
  s = s.replace(/(^|\/)(readme|index)$/, '');
  return s;
}

/** Splits front matter (only simple `key: value` lines are read) from the body. */
export function splitFront(text) {
  const m = text.match(/^---\n([\s\S]*?)\n---\n?/);
  if (!m) return { front: {}, raw: '', body: text };
  const front = {};
  for (const line of m[1].split('\n')) {
    const kv = line.match(/^([A-Za-z_]+):\s*(.*)$/);
    if (kv) front[kv[1]] = kv[2].replace(/^["']|["']$/g, '');
  }
  return { front, raw: m[1], body: text.slice(m[0].length) };
}

/** Rewrites one page. `rel` is its path inside docs/. */
export function transform(rel, text, exists = () => true) {
  const { front, body: b0 } = splitFront(text);
  let body = b0.replace(/^\s+/, '');
  let title = front.title;
  const h1 = body.match(/^# (.+)\n/);
  if (h1 && (!title || h1[1].trim().toLowerCase() === title.trim().toLowerCase() || !front.title)) {
    title = title || h1[1].trim();
    body = body.slice(h1[0].length).replace(/^\s+/, "");
  }
  if (!title) title = path.basename(rel, '.md');
  if (rel === 'console/README.md') title = 'Screenshots';

  // Jekyll's relative_url filter.
  body = body.replace(/\{\{\s*'([^']*)'\s*\|\s*relative_url\s*\}\}/g, (_, p) => BASE + p);
  // Kramdown callouts: the attribute line applies to the paragraph after it.
  body = body.replace(/^\{:\s*\.([a-z]+)\s*\}\n((?:.+\n?)+)/gm, (_, kind, para) => `:::${ASIDE[kind] || 'note'}\n${para.trimEnd()}\n:::\n`);
  // Relative links to other files.
  const dir = path.posix.dirname(rel);
  body = body.replace(/\]\((?!https?:|mailto:|#|\/)([^)\s]+?)(#[^)\s]*)?\)/g, (all, target, hash = '') => {
    const resolved = path.posix.normalize(path.posix.join(dir, target));
    if (resolved.startsWith('..')) return `](${REPO}${resolved.replace(/^(\.\.\/)+/, '')}${hash})`;
    if (/\.md$/i.test(resolved)) {
      if (SKIP.has(resolved) && resolved !== 'index.md') return `](${REPO}docs/${resolved}${hash})`;
      const slug = slugOf(resolved);
      return `](${BASE}/${slug ? slug + '/' : ''}${hash})`;
    }
    if (exists(resolved)) return `](${BASE}/${resolved}${hash})`;
    return all;
  });

  const keep = Object.entries(front).filter(([k]) => KEEP.has(k) && k !== 'title');
  const head = ['---', 'title: ' + JSON.stringify(title), ...keep.map(([k, v]) => `${k}: ${JSON.stringify(v)}`),
    'editUrl: ' + JSON.stringify('https://github.com/forgelinehq/forgeline/edit/main/docs/' + rel), '---', ''].join('\n');
  return head + body;
}

function walk(dir, rel = '') {
  const out = [];
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const r = rel ? rel + '/' + e.name : e.name;
    if (e.isDirectory()) out.push(...walk(path.join(dir, e.name), r));
    else out.push(r);
  }
  return out;
}

function main() {
  fs.rmSync(OUT, { recursive: true, force: true });
  fs.rmSync(path.join(PUBLIC, 'console'), { recursive: true, force: true });
  const files = walk(DOCS);
  const has = new Set(files);
  let pages = 0, images = 0;
  for (const rel of files) {
    const src = path.join(DOCS, rel);
    if (/\.md$/i.test(rel)) {
      if (SKIP.has(rel)) continue;
      // README.md and index.md are their folder's page; everything else keeps its (lower-case) path.
      const isFolderPage = /^(readme|index)\.md$/i.test(path.basename(rel));
      const dest = path.join(OUT, isFolderPage ? path.join(slugOf(rel), 'index.md') : rel.toLowerCase());
      fs.mkdirSync(path.dirname(dest), { recursive: true });
      fs.writeFileSync(dest, transform(rel, fs.readFileSync(src, 'utf8'), (p) => has.has(p)));
      pages++;
    } else if (/^console\/.+\.(png|jpe?g|gif|svg|webp)$/i.test(rel)) {
      const dest = path.join(PUBLIC, rel);
      fs.mkdirSync(path.dirname(dest), { recursive: true });
      fs.copyFileSync(src, dest);
      images++;
    }
  }
  // The home page is written for the site itself (a hero and cards), so it lives here, not in docs/.
  fs.copyFileSync(path.join(SITE, 'src', 'home.mdx'), path.join(OUT, 'index.mdx'));
  console.log(`sync: ${pages} pages and ${images} images from docs/, and the home page`);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) main();
