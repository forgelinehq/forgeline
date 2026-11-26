// Walks every page of a running Forgeline console as a signed-in person and reports what a user would hit:
// a page that throws, shows its error box, is blank, or whose reads fail. Read-only: it only navigates.
//
//   cd scripts/uat && npm ci
//   printf '%s\n' "$PASSWORD" | UAT_URL=https://uat.example:8443 UAT_USER=<test user> node smoke.mjs [outdir]
//
// The password is read from the first line of stdin, never from an argument or a file in the repository, and never
// printed. CHROMIUM_PATH names the browser. The report goes to stdout; screenshots go to outdir (default: none).
// Screenshots of a real instance can show account numbers: never commit them.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const URL_ = (process.env.UAT_URL || '').replace(/\/$/, '');
const USER = process.env.UAT_USER || '';
if (!URL_ || !USER) { console.error('set UAT_URL and UAT_USER; the password comes on stdin'); process.exit(2); }
const password = fs.readFileSync(0, 'utf8').split('\n')[0];
const outdir = process.argv[2];
const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');

/** The routes in the console's own table (internal/server/ui/app.js). */
const routes = [...fs.readFileSync(path.join(ROOT, 'internal/server/ui/app.js'), 'utf8').matchAll(/route\('([^']+)'/g)].map((m) => m[1]);
// A page that takes an id is visited once for each list below that has something in it.
const PARAMS = {
  '/pipelines/:id': ['/v1/pipelines', 'pipelines', (x) => x.id], '/runs/:id': ['/v1/runs', 'runs', (x) => x.id],
  '/agents/:id': ['/v1/ai/agents', 'agents', (x) => x.id], '/skills/:id': ['/v1/ai/skills', 'skills', (x) => x.id],
  '/workflows/:id': ['/v1/ai/workflows', 'workflows', (x) => x.id], '/teams/:id': ['/v1/ai/teams', 'teams', (x) => x.id],
  '/runbooks/:id': ['/v1/runbooks', 'runbooks', (x) => x.id], '/evals/:id': ['/v1/ai/evals', 'suites', (x) => x.id],
  '/blueprints/:id': ['/v1/blueprints', 'blueprints', (x) => x.id], '/design/:id': ['/v1/blueprints', 'blueprints', (x) => x.id],
  '/missions/:id': ['/v1/ai/missions', 'missions', (x) => x.id], '/traces/:id': ['/v1/traces', 'traces', (x) => x.id],
};

const browser = await chromium.launch({ executablePath: process.env.CHROMIUM_PATH || undefined });
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const login = await ctx.request.post(URL_ + '/v1/auth/login', { headers: { 'X-Forgeline-Request': '1' }, data: { username: USER, password } });
if (!login.ok()) { console.error('login failed: ' + login.status()); process.exit(2); }
const me = await (await ctx.request.get(URL_ + '/v1/auth/state')).json();
const role = me.role || me.user?.role || '?';

const targets = [];
for (const r of routes) {
  if (!r.includes(':')) { targets.push(r); continue; }
  const spec = PARAMS[r];
  if (!spec) continue;
  const resp = await ctx.request.get(URL_ + spec[0]);
  if (!resp.ok()) continue;
  const body = await resp.json();
  const list = Array.isArray(body) ? body : body[spec[1]] || [];
  if (list.length) targets.push(r.replace(/:id/, spec[2](list[0])).replace(/\/\/+/g, '/'));
}

const rows = [];
for (const route of targets) {
  const page = await ctx.newPage();
  const problems = [];
  page.on('pageerror', (e) => problems.push('script error: ' + String(e).slice(0, 100)));
  page.on('response', (r) => {
    const u = new globalThis.URL(r.url());
    if (u.pathname.startsWith('/v1/') && r.status() >= 400) problems.push(`${r.request().method()} ${u.pathname} -> ${r.status()}`);
  });
  await page.goto(URL_ + '/#' + route);
  await page.reload();
  await page.waitForLoadState('networkidle').catch(() => {});
  await page.waitForTimeout(500);
  const text = await page.evaluate(() => document.body.innerText);
  let verdict = 'ok';
  if (/do not have access|You do not have access/i.test(text)) verdict = 'not for this role';
  else if (text.trim().length < 40) { verdict = 'blank'; problems.push('the page is blank'); }
  else if (/Something went wrong|Cannot reach the server|Unexpected error/.test(text)) { verdict = 'error shown'; }
  else if (problems.length) verdict = 'reads failed';
  if (outdir) { fs.mkdirSync(outdir, { recursive: true }); await page.evaluate(() => document.body.offsetHeight); await page.screenshot({ path: path.join(outdir, route.replace(/[^\w-]+/g, '_') + '.png') }); }
  rows.push({ route, verdict, problems: [...new Set(problems)] });
  await page.close();
}
await browser.close();

const bad = rows.filter((r) => !['ok', 'not for this role'].includes(r.verdict));
console.log(`# Console smoke on ${new globalThis.URL(URL_).host} as a ${role}\n`);
console.log(`${rows.length} pages visited: ${rows.filter((r) => r.verdict === 'ok').length} ok, ${rows.filter((r) => r.verdict === 'not for this role').length} not for this role, ${bad.length} with a problem.\n`);
if (bad.length) { console.log('| Page | Result | Detail |\n| --- | --- | --- |'); for (const r of bad) console.log(`| \`${r.route}\` | ${r.verdict} | ${r.problems.join('; ') || ''} |`); }
const hidden = rows.filter((r) => r.verdict === 'not for this role').map((r) => '`' + r.route + '`');
if (hidden.length) console.log(`\nNot shown to this role: ${hidden.join(', ')}`);
process.exit(bad.length ? 1 : 0);
