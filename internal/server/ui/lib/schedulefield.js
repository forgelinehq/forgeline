// The schedule picker (design note 047): a preset in words, its time, day and time zone, and the next runs as the
// server works them out, so what is saved is what the person saw.

import { h, mount } from './dom.js';
import { post } from './api.js';
import { PRESETS, INTERVALS, DAYS, toSpec, fromSpec } from './schedule.js';

const localTZ = () => { try { return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'; } catch { return 'UTC'; } };
// The browser's zones, with the one already chosen first: a browser may list a zone by an older name (Asia/Calcutta for
// Asia/Kolkata), and a saved zone must never silently change to another.
function zones(current) {
  let all = [];
  try { all = Intl.supportedValuesOf('timeZone'); } catch { /* an old browser: the ones below */ }
  return [...new Set([current, localTZ(), 'UTC', ...all].filter(Boolean))];
}
const when = (iso) => new Date(iso).toLocaleString(undefined, { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });

/** spec: what is stored ({every_minutes} or {cron, tz}); onChange(spec) on every change. Returns the element. */
export function scheduleField({ spec, onChange, id = 'schedule' }) {
  const f = fromSpec(spec, localTZ());
  const el = h('div', { class: 'space-y-2', id });
  const preview = h('div', { class: 'text-xs', id: id + '-next', 'aria-live': 'polite' });
  let timer = 0, seq = 0;
  const changed = () => { const s = toSpec(f); onChange(s); draw(); clearTimeout(timer); timer = setTimeout(() => check(s), 250); };
  async function check(s) {
    const mine = ++seq;
    if (!s.cron && !s.every_minutes) { mount(preview, h('span', { class: 'muted' }, 'It runs only when someone starts it.')); return; }
    try {
      const r = await post('/v1/ai/schedules/preview', s);
      if (mine !== seq) return;
      mount(preview, h('span', { class: 'muted' }, 'Next runs, in your time: '), h('span', { class: 'font-medium' }, r.next.slice(0, 3).map(when).join(' · ')),
        s.cron && s.tz && s.tz !== 'UTC' ? h('span', { class: 'muted block' }, 'The time is kept in ' + s.tz + ', so it follows that zone\'s daylight saving.') : null);
    } catch (e) { if (mine === seq) mount(preview, h('span', { class: 'text-rose-700 dark:text-rose-300' }, e.message)); }
  }
  const sel = (label, value, options, set, sid) => h('select', { class: 'input w-auto', id: sid, 'aria-label': label, onchange: (e) => { set(e.target.value); changed(); } },
    options.map(([v, t]) => h('option', { value: String(v), selected: String(v) === String(value) }, t)));
  const time = () => h('input', { type: 'time', class: 'input w-32', id: id + '-time', value: f.time, 'aria-label': 'Time', onchange: (e) => { f.time = e.target.value || '09:00'; changed(); } });
  function draw() {
    const row = [];
    if (f.preset === 'every') row.push(sel('How often', f.every, INTERVALS, v => { f.every = Number(v); }, id + '-every'));
    if (f.preset === 'weekly') row.push(sel('Day', f.day, DAYS, v => { f.day = Number(v); }, id + '-day'));
    if (f.preset === 'monthly') row.push(h('input', { type: 'number', min: '1', max: '28', class: 'input w-20', id: id + '-dom', value: String(f.dom), 'aria-label': 'Day of the month', onchange: (e) => { f.dom = Number(e.target.value) || 1; changed(); } }));
    if (['daily', 'weekdays', 'weekly', 'monthly'].includes(f.preset)) row.push(h('span', { class: 'muted self-center text-sm' }, 'at'), time());
    if (f.preset === 'custom') row.push(h('input', { class: 'input w-48 font-mono', id: id + '-cron', value: f.cron, placeholder: '0 9 * * 1-5', 'aria-label': 'Cron expression', onchange: (e) => { f.cron = e.target.value.trim(); changed(); } }));
    const cronish = !['off', 'every'].includes(f.preset);
    mount(el,
      h('div', { class: 'flex flex-wrap gap-2' }, sel('When it runs by itself', f.preset, PRESETS, v => { f.preset = v; }, id + '-preset'), ...row),
      cronish ? h('div', { class: 'flex flex-wrap items-center gap-2' }, h('span', { class: 'muted text-xs' }, 'Time zone'), sel('Time zone', f.tz, zones(f.tz).map(z => [z, z]), v => { f.tz = v; }, id + '-tz')) : null,
      f.preset === 'custom' ? h('p', { class: 'hint' }, 'Five fields: minute hour day-of-month month weekday, such as 0 9 * * 1-5 for weekdays at nine. At most every 15 minutes.') : null,
      preview);
  }
  draw();
  check(toSpec(f));
  return el;
}
