// Forgeline web console: AI use and budgets in words (design note 036).
//
// No imports and no DOM, so it runs under Node in tests.

/** A token count people can read: 950, 12.3k, 4.5M. */
export function tokens(n) {
  n = Number(n) || 0;
  if (n < 1000) return String(n);
  if (n < 1e6) return (n / 1e3).toFixed(n < 1e4 ? 1 : 0).replace(/\.0$/, '') + 'k';
  return (n / 1e6).toFixed(n < 1e7 ? 1 : 0).replace(/\.0$/, '') + 'M';
}

/** Model time in minutes: 0.4 min, 12 min. */
export function minutes(ms) {
  const m = (Number(ms) || 0) / 60000;
  return (m < 10 ? m.toFixed(1) : Math.round(m)).toString().replace(/\.0$/, '') + ' min';
}

/** What a month's use came to, in one line. */
export function useLine(u) {
  const parts = [tokens((u.input_tokens || 0) + (u.output_tokens || 0)) + ' tokens', minutes(u.millis), u.calls + (u.calls === 1 ? ' call' : ' calls')];
  return parts.join(' · ');
}

/** What it cost, only as far as it is known. */
export function costLine(u) {
  if (!u.costed_calls) return 'cost not reported';
  const c = '$' + u.cost_usd.toFixed(2);
  return u.costed_calls === u.calls ? c : `${c} for ${u.costed_calls} of ${u.calls} calls`;
}

/** The budget line on a card: { text, pct, level } or null when there is no budget. */
export function budgetLine(state) {
  const b = state?.budget;
  if (!b) return null;
  const used = state.used || {};
  const parts = [];
  if (b.tokens) parts.push(`${tokens((used.input_tokens || 0) + (used.output_tokens || 0))} of ${tokens(b.tokens)} tokens`);
  if (b.minutes) parts.push(`${minutes(used.millis)} of ${b.minutes} min`);
  const pct = Math.min(100, Math.round((state.fraction || 0) * 100));
  const tail = state.level === 'stop' ? ' · stopped until next month or a higher budget' : state.level === 'warn' ? ' · nearly used up' : '';
  return { text: parts.join(', ') + ' this month' + tail, pct, level: state.level };
}

/** The last few months, newest first, as YYYY-MM. */
export function recentMonths(now = new Date(), n = 6) {
  const out = [];
  for (let i = 0; i < n; i++) {
    const d = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - i, 1));
    out.push(d.toISOString().slice(0, 7));
  }
  return out;
}
