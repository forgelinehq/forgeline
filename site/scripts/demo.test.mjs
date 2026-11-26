// Tests for the demo build (node --test): the console's own page and scripts must work from a folder of the site.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { rebase, demoPage } from './demo.mjs';

test('absolute /ui/ addresses in scripts become relative', () => {
  assert.equal(rebase("h('img', { src: '/ui/favicon.svg' })"), "h('img', { src: './ui/favicon.svg' })");
  assert.equal(rebase("fetch('/v1/runs')"), "fetch('/v1/runs')", 'API calls are left for the shim');
});

test('the demo page loads the shim before the console and points at its own copy of the files', () => {
  const html = demoPage(fs.readFileSync(new URL('../../internal/server/ui/index.html', import.meta.url), 'utf8'));
  assert.ok(!/(href|src)="\/ui\//.test(html), 'no absolute /ui/ address is left');
  assert.ok(html.indexOf('shim.js') > 0 && html.indexOf('shim.js') < html.indexOf('type="module"'));
});

test('the shim answers reads from fixtures, shifts times, refuses changes and records misses', async () => {
  const src = fs.readFileSync(new URL('../demo/shim.js', import.meta.url), 'utf8');
  const recordedAt = new Date(Date.now() - 3600e3).toISOString();
  const fx = { recordedAt, responses: { 'GET /v1/x': { status: 200, body: { at: recordedAt } } } };
  const realFetch = async () => new Response(JSON.stringify(fx));
  const win = { fetch: realFetch };
  new Function('window', 'location', 'console', src)(win, { href: 'http://h/demo/', origin: 'http://h' }, { warn() {} });
  const ok = await (await win.fetch('/v1/x')).json();
  assert.ok(Date.now() - Date.parse(ok.at) < 5000, 'a time an hour old at recording is now');
  assert.equal((await win.fetch('/v1/x', { method: 'POST' })).status, 403);
  assert.equal((await win.fetch('/v1/missing')).status, 404);
  assert.deepEqual(win.__demoMisses, ['/v1/missing']);
});

test('the site search does not index the demo console', () => {
  assert.ok(demoPage('<body class="x">').includes('data-pagefind-ignore'));
});
