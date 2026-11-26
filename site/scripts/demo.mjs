// Builds the live console for the documentation: copies internal/server/ui into public/demo/ and adds the shim and
// the recorded fixtures. Runs before `astro build` (see package.json). Nothing here is committed; see design note 054.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const SITE = path.resolve(here, '..');
const UI = path.resolve(SITE, '..', 'internal', 'server', 'ui');
const OUT = path.join(SITE, 'public', 'demo');

/** The console's JavaScript asks for `/ui/...` from the server root; in the demo that folder sits next to the page. */
export function rebase(js) {
  return js.replace(/(['"`])\/ui\//g, '$1./ui/');
}

/** index.html for the demo: the server's own page with relative addresses and the shim loaded first. */
export function demoPage(html) {
  return html
    .replace(/(href|src)="\/ui\//g, '$1="./ui/')
    .replace('<script type="module"', '<script src="./shim.js"></script>\n  <script type="module"')
    .replace('<body ', '<body data-pagefind-ignore ') // the site's search should not index a console page
    .replace('<title>Forgeline</title>', '<title>Forgeline (demo)</title>');
}

function copyTree(from, to) {
  fs.mkdirSync(to, { recursive: true });
  for (const e of fs.readdirSync(from, { withFileTypes: true })) {
    const s = path.join(from, e.name), d = path.join(to, e.name);
    if (e.isDirectory()) copyTree(s, d);
    else if (e.name === 'index.html') continue;
    else if (e.name.endsWith('.js')) fs.writeFileSync(d, rebase(fs.readFileSync(s, 'utf8')));
    else fs.copyFileSync(s, d);
  }
}

function main() {
  fs.rmSync(OUT, { recursive: true, force: true });
  copyTree(UI, path.join(OUT, 'ui'));
  fs.writeFileSync(path.join(OUT, 'index.html'), demoPage(fs.readFileSync(path.join(UI, 'index.html'), 'utf8')));
  fs.copyFileSync(path.join(SITE, 'demo', 'shim.js'), path.join(OUT, 'shim.js'));
  const fx = path.join(SITE, 'demo', 'fixtures.json');
  if (!fs.existsSync(fx)) fs.writeFileSync(fx, JSON.stringify({ recordedAt: new Date().toISOString(), responses: {} }));
  fs.copyFileSync(fx, path.join(OUT, 'fixtures.json'));
  console.log('demo: the console copied to public/demo/');
}

if (process.argv[1] === fileURLToPath(import.meta.url)) main();
