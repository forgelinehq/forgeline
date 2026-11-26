// Records the demo console's fixtures from a real server (design note 054): starts Forgeline on a throwaway data
// directory, seeds it through its API, opens the console pages in a browser and keeps every GET /v1 answer.
//
//   cd site && npm run record          (needs Go, Docker for the seeded runs, and a Chromium)
//
// FORGELINE_BIN uses a built binary; CHROMIUM_PATH names the browser (default: the one playwright-core finds).
import { spawn, execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const here = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(here, '..', '..');
const PORT = 18090 + Math.floor(Math.random() * 500);
const BASE = `http://127.0.0.1:${PORT}`;
const TOKEN = 'demo-record-token-' + Math.random().toString(36).slice(2, 12);
const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'fl-record-'));

// The pages the guides show. Adding a page to the guides means adding its route here.
export const ROUTES = ['/', '/pipelines', '/pipelines/shop-build', '/runs/{run:shop-build}', '/runs/{run:nightly-scan}',
  '/agents', '/workflows', '/proposals', '/secrets', '/people', '/audit', '/settings', '/step-capabilities'];

const SHOP = `version: v1alpha1
name: shop-build
steps:
  - id: build
    image: alpine:3.24
    run: echo "compiling shop" && sleep 1
  - id: unit-tests
    image: alpine:3.24
    needs: [build]
    run: echo "42 tests passed"
  - id: lint
    image: alpine:3.24
    needs: [build]
    run: echo "no problems found"
  - id: package
    image: alpine:3.24
    needs: [unit-tests, lint]
    run: echo "shop-1.4.0.tar.gz written"
`;
const NIGHTLY = `version: v1alpha1
name: nightly-scan
steps:
  - id: fetch
    image: alpine:3.24
    run: echo "fetching the dependency list"
  - id: scan
    image: alpine:3.24
    needs: [fetch]
    run: echo "found 1 vulnerable dependency" >&2 && exit 1
  - id: report
    image: alpine:3.24
    needs: [scan]
    run: echo "sending the report"
`;

async function call(method, p, body) {
  const r = await fetch(BASE + p, { method, headers: { Authorization: 'Bearer ' + TOKEN, 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
  const text = await r.text();
  if (!r.ok) throw new Error(`${method} ${p}: ${r.status} ${text}`);
  return text ? JSON.parse(text) : null;
}

async function seed() {
  await call('POST', '/v1/auth/setup', { token: TOKEN, username: 'admin', name: 'Alex Morgan', password: 'a-demo-password-1234' });
  const runs = {};
  for (const [name, yaml] of [['shop-build', SHOP], ['nightly-scan', NIGHTLY]]) {
    await call('POST', '/v1/pipelines', { yaml, description: 'first version' });
    const started = await call('POST', `/v1/pipelines/${name}/run`);
    const id = started.id || started.run_id || (started.run && started.run.id);
    for (let i = 0; i < 90; i++) {
      const run = await call('GET', `/v1/runs/${id}`);
      if (['succeeded', 'failed', 'done', 'error', 'canceled'].includes(run.status)) break;
      await new Promise(r => setTimeout(r, 1000));
    }
    runs[name] = id;
  }
  return runs;
}

/** A recording must never carry the maintainer's machine or a real account into the repository. */
export function leaks(text) {
  const found = [];
  if (text.includes(os.homedir())) found.push('the home directory');
  if (text.includes(os.hostname())) found.push('this machine\'s name');
  if (text.includes(os.tmpdir() + '/fl-record')) found.push('the recording\'s data directory');
  if (/\/home\/[a-z][\w-]*\//.test(text)) found.push('a /home path');
  if (/"\d{12}"|:\d{12}[:/]/.test(text)) found.push('an AWS account number');
  return found;
}

async function main() {
  const bin = process.env.FORGELINE_BIN || path.join(tmp, 'forgeline');
  if (!process.env.FORGELINE_BIN) execFileSync('go', ['build', '-o', bin, './cmd/forgeline'], { cwd: ROOT, stdio: 'inherit' });
  const log = fs.openSync(path.join(tmp, 'server.log'), 'w');
  const server = spawn(bin, ['server', '--listen', `127.0.0.1:${PORT}`, '--data-dir', path.join(tmp, 'data')], { env: { ...process.env, FORGELINE_TOKEN: TOKEN }, stdio: ['ignore', log, log] });
  try {
    for (let i = 0; i < 50; i++) { try { await fetch(BASE + '/healthz'); break; } catch { await new Promise(r => setTimeout(r, 200)); } }
    const runs = await seed();
    const browser = await chromium.launch({ executablePath: process.env.CHROMIUM_PATH || undefined });
    const page = await (await browser.newContext({ viewport: { width: 1280, height: 800 } })).newPage();
    const login = await page.request.post(BASE + '/v1/auth/login', { headers: { 'X-Forgeline-Request': '1' }, data: { username: 'admin', password: 'a-demo-password-1234' } });
    if (!login.ok()) throw new Error('login failed: ' + login.status());
    const responses = {};
    page.on('response', async (r) => {
      const u = new URL(r.url());
      if (r.request().method() !== 'GET' || !u.pathname.startsWith('/v1/')) return;
      let body = null;
      try { body = await r.json(); } catch { return; } // streams and other non-JSON answers are not recorded
      responses['GET ' + u.pathname + u.search] = { status: r.status(), body };
    });
    for (const route of ROUTES) {
      const real = route.replace(/\{run:([\w-]+)\}/, (_, n) => runs[n]);
      await page.goto(BASE + '/#' + real);
      await page.waitForLoadState('networkidle');
      await page.waitForTimeout(400);
    }
    await browser.close();
    // Step logs are Server-Sent Events, which the browser's answers above do not hold as JSON: keep them as events.
    const streams = {};
    for (const id of Object.values(runs)) {
      const run = await call('GET', `/v1/runs/${id}`);
      for (const st of run.steps || []) {
        const p = `/v1/runs/${id}/steps/${st.id}/logs`;
        const text = await (await fetch(BASE + p, { headers: { Authorization: 'Bearer ' + TOKEN, Accept: 'text/event-stream' } })).text();
        streams[p] = text.split('\n\n').filter(Boolean).map((block) => {
          const ev = { event: 'message', data: '' };
          for (const line of block.split('\n')) {
            if (line.startsWith('event:')) ev.event = line.slice(6).trim();
            else if (line.startsWith('data:')) ev.data += line.slice(5).replace(/^ /, '');
          }
          return ev;
        });
      }
    }
    // What identifies the machine the recording ran on is replaced by made-up values.
    const rt = responses['GET /v1/infra/runtime'];
    if (rt) { rt.body.server.data_dir = '/var/lib/forgeline'; rt.body.docker.host = 'build-host-1'; }
    const sorted = Object.fromEntries(Object.entries(responses).sort(([a], [b]) => a.localeCompare(b)));
    const out = { recordedAt: new Date().toISOString(), responses: sorted, streams };
    const text = JSON.stringify(out, null, 1) + '\n';
    const bad = leaks(text);
    if (bad.length && process.env.RECORD_DEBUG) console.error(text.match(/.{0,60}(\/home\/|\b\d{12}\b).{0,40}/g));
    if (bad.length) throw new Error('the recording contains ' + bad.join(', ') + '; fix the seed, not the check');
    fs.writeFileSync(path.join(here, 'fixtures.json'), text);
    console.log(`record: ${Object.keys(sorted).length} answers from ${ROUTES.length} pages -> demo/fixtures.json`);
  } finally {
    server.kill();
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) main().catch((e) => { console.error(e); process.exit(1); });
