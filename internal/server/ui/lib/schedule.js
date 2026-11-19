// Schedules in words (design note 047). A person picks a preset ("Weekdays at 09:00"); the server stores minutes or a
// cron expression with a time zone. fromSpec reads a stored one back into a preset, or "custom" for any other cron.

export const PRESETS = [
  ['off', 'Only when started by hand'],
  ['every', 'Every few minutes or hours'],
  ['daily', 'Every day at'],
  ['weekdays', 'Weekdays (Monday to Friday) at'],
  ['weekly', 'Every week on'],
  ['monthly', 'Every month on day'],
  ['custom', 'Custom (cron expression)'],
];
export const INTERVALS = [[15, 'Every 15 minutes'], [30, 'Every 30 minutes'], [60, 'Every hour'], [120, 'Every 2 hours'], [360, 'Every 6 hours'], [720, 'Every 12 hours'], [1440, 'Every 24 hours'], [10080, 'Every 7 days']];
export const DAYS = [[1, 'Monday'], [2, 'Tuesday'], [3, 'Wednesday'], [4, 'Thursday'], [5, 'Friday'], [6, 'Saturday'], [0, 'Sunday']];

const hm = (time) => { const [h, m] = String(time || '09:00').split(':').map(Number); return [Number.isFinite(h) ? h : 9, Number.isFinite(m) ? m : 0]; };
const pad = (n) => String(n).padStart(2, '0');

/** A form's choice as what the server stores: { every_minutes } or { cron, tz }, or {} for by hand only. */
export function toSpec(f) {
  const [h, m] = hm(f.time);
  switch (f.preset) {
    case 'every': return { every_minutes: Number(f.every) || 60 };
    case 'daily': return { cron: `${m} ${h} * * *`, tz: f.tz || 'UTC' };
    case 'weekdays': return { cron: `${m} ${h} * * 1-5`, tz: f.tz || 'UTC' };
    case 'weekly': return { cron: `${m} ${h} * * ${Number(f.day ?? 1)}`, tz: f.tz || 'UTC' };
    case 'monthly': return { cron: `${m} ${h} ${Math.min(28, Math.max(1, Number(f.dom) || 1))} * *`, tz: f.tz || 'UTC' };
    case 'custom': return { cron: String(f.cron || '').trim(), tz: f.tz || 'UTC' };
  }
  return {};
}

/** What the server stored, as a form's choice. */
export function fromSpec(spec, defaultTZ = 'UTC') {
  const base = { preset: 'off', every: 60, time: '09:00', day: 1, dom: 1, cron: '', tz: spec?.tz || defaultTZ };
  if (!spec) return base;
  if (Number(spec.every_minutes) > 0) return { ...base, preset: 'every', every: Number(spec.every_minutes) };
  if (!spec.cron) return base;
  const f = spec.cron.trim().split(/\s+/);
  const num = (s) => /^\d+$/.test(s) ? Number(s) : null;
  const [m, h] = [num(f[0]), num(f[1])];
  if (f.length === 5 && m !== null && h !== null && m < 60 && h < 24 && f[3] === '*') {
    const time = pad(h) + ':' + pad(m);
    if (f[2] === '*' && f[4] === '*') return { ...base, preset: 'daily', time };
    if (f[2] === '*' && f[4] === '1-5') return { ...base, preset: 'weekdays', time };
    if (f[2] === '*' && num(f[4]) !== null && num(f[4]) <= 6) return { ...base, preset: 'weekly', time, day: num(f[4]) };
    if (num(f[2]) !== null && num(f[2]) <= 28 && f[4] === '*') return { ...base, preset: 'monthly', time, dom: num(f[2]) };
  }
  return { ...base, preset: 'custom', cron: spec.cron };
}

/** A stored schedule in a few words: "weekdays at 09:00 (Europe/Berlin)". */
export function describe(spec) {
  const f = fromSpec(spec);
  const tz = f.tz && f.tz !== 'UTC' ? ' (' + f.tz + ')' : ' UTC';
  switch (f.preset) {
    case 'every': return (INTERVALS.find(([v]) => v === f.every) || [0, 'every ' + f.every + ' minutes'])[1].toLowerCase();
    case 'daily': return 'daily at ' + f.time + tz;
    case 'weekdays': return 'weekdays at ' + f.time + tz;
    case 'weekly': return (DAYS.find(([v]) => v === f.day) || [0, 'day ' + f.day])[1] + 's at ' + f.time + tz;
    case 'monthly': return 'monthly on day ' + f.dom + ' at ' + f.time + tz;
    case 'custom': return 'cron ' + f.cron + tz;
  }
  return 'only by hand';
}
