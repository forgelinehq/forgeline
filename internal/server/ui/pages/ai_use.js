// Settings › AI use and budgets (design note 036): what each agent and people used per month, and each
// agent's monthly budget. At 80% the console warns; once a budget is used up the agent stops before its next
// model call. Tokens are as the model reports them; cost only where the claude program reports it.
import { h, mount, pageHeader, spinner, errorBox, notice, toast, modal, field } from '../lib/dom.js';
import { get, put, atLeast } from '../lib/api.js';
import { session } from '../app.js';
import { useLine, costLine, budgetLine, recentMonths } from '../lib/usage.js';

const BAR = { ok: 'bg-emerald-500', warn: 'bg-amber-500', stop: 'bg-rose-500' };

export function budgetBar(line) {
  const fill = h('div', { class: 'h-full rounded-full ' + BAR[line.level] });
  fill.style.width = Math.max(2, line.pct) + '%'; // the CSSOM, not a style attribute, so the CSP allows it
  return h('div', { class: 'h-1.5 w-full overflow-hidden rounded-full bg-slate-200 dark:bg-slate-800', role: 'meter', 'aria-valuemin': '0', 'aria-valuemax': '100', 'aria-valuenow': String(line.pct), 'aria-label': line.text }, fill);
}

export async function aiUsePage({ query }) {
  const el = h('div', {});
  const admin = atLeast(session.role, 'admin');
  const months = recentMonths();
  const month = months.includes(query.month) ? query.month : months[0];
  const header = () => pageHeader('AI use and budgets', 'What each agent used, and how much it may use in a month. An agent over its budget stops before its next model call.',
    h('select', { class: 'input w-auto', id: 'use-month', 'aria-label': 'Month', onchange: (e) => { location.hash = '#/ai-use?month=' + e.target.value; } },
      months.map(m => h('option', { value: m, selected: m === month }, m))));
  mount(el, header(), spinner());
  let use, team;
  try {
    [use, team] = await Promise.all([get('/v1/ai/usage?month=' + month), get('/v1/ai/team')]);
  } catch (e) { mount(el, header(), errorBox(e.message)); return el; }
  const byWho = Object.fromEntries(use.usage.map(u => [u.who, u]));
  const thisMonth = month === months[0];

  async function setBudget(m) {
    const b = m.budget.budget || {};
    await modal('Budget for ' + m.name, (close) => {
      const tok = h('input', { id: 'b-tokens', class: 'input', type: 'number', min: '0', step: '1000', value: b.tokens || '', placeholder: 'no limit' });
      const min = h('input', { id: 'b-minutes', class: 'input', type: 'number', min: '0', value: b.minutes || '', placeholder: 'no limit' });
      const err = h('div', {});
      return h('div', { class: 'space-y-4' },
        h('p', { class: 'text-sm' }, 'Per calendar month (UTC). At 80% the console warns; when either limit is reached, ' + m.name + ' stops before its next model call until next month or a higher budget. Leave both empty for no limit.'),
        h('div', { class: 'grid grid-cols-2 gap-3' }, field('Tokens', tok, 'Input and output, as the model reports them'), field('Minutes', min, 'Time spent waiting for the model')),
        err,
        h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'),
          h('button', { class: 'btn-primary', type: 'button', id: 'b-save', onclick: async () => {
            try {
              await put('/v1/ai/agents/' + encodeURIComponent(m.id) + '/budget', { tokens: Number(tok.value) || 0, minutes: Number(min.value) || 0 });
              close(); toast('Budget saved', 'ok'); el.replaceWith(await aiUsePage({ query: { month } }));
            } catch (e) { mount(err, errorBox(e.message)); }
          } }, 'Save')));
    });
  }

  const rows = team.members.map(m => {
    const u = byWho[m.id];
    const line = thisMonth ? budgetLine(m.budget) : null;
    return h('tr', { 'data-agent': m.id },
      h('td', {}, h('a', { href: '#/agents/' + m.id, class: 'font-medium hover:underline' }, m.name)),
      h('td', { class: 'text-sm' }, u ? useLine(u) : h('span', { class: 'muted' }, 'nothing')),
      h('td', { class: 'muted text-xs' }, u ? costLine(u) : ''),
      h('td', { class: 'min-w-56 text-sm' }, line ? h('div', { class: 'space-y-1' }, budgetBar(line), h('span', { class: 'text-xs' + (line.level === 'ok' ? ' muted' : line.level === 'warn' ? ' text-amber-700 dark:text-amber-300' : ' text-rose-700 dark:text-rose-300') }, line.text))
        : h('span', { class: 'muted text-xs' }, m.budget.budget ? 'budget set' : 'no budget')),
      h('td', {}, admin && h('button', { class: 'btn-ghost btn-sm', type: 'button', 'data-budget': m.id, onclick: () => setBudget(m) }, 'Set budget')));
  });
  const people = byWho._people;
  mount(el, header(),
    h('div', { class: 'mb-4' }, notice('info', 'Tokens are counted as the model reports them, cost only where the claude program reports it. Cloud spend an agent causes (for example a change it proposed) is not counted here.')),
    h('section', { class: 'card overflow-x-auto', id: 'use-table' }, h('table', { class: 'tbl' },
      h('thead', {}, h('tr', {}, ['Agent', 'Used in ' + month, 'Cost', 'Budget', ''].map(x => h('th', {}, x)))),
      h('tbody', {}, rows, h('tr', { 'data-agent': '_people' },
        h('td', { class: 'font-medium' }, 'People', h('span', { class: 'muted block text-xs font-normal' }, 'questions people ask the assistant')),
        h('td', { class: 'text-sm' }, people ? useLine(people) : h('span', { class: 'muted' }, 'nothing')),
        h('td', { class: 'muted text-xs' }, people ? costLine(people) : ''), h('td', { class: 'muted text-xs' }, 'no budget'), h('td', {}))))),
    use.usage.some(u => u.blind_calls) ? h('p', { class: 'hint mt-3' }, 'Some calls did not report their tokens (the model program sent plain text), so the totals are low by those calls.') : null);
  return el;
}

