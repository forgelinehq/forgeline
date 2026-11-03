// The Access tab of an integration's popup (design note 026): which of its tools may run, and what each agent
// can actually do. Blocked beats everything. Only administrators change it; developers can read it.
import { h, mount, spinner, errorBox, toast, notice } from '../lib/dom.js';
import { get, put } from '../lib/api.js';
import { toolChoice, policyRequest, agentStateText } from '../lib/hub.js';

const SEG = { allow: 'bg-emerald-50 font-medium text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300', ask: 'bg-amber-50 font-medium text-amber-700 dark:bg-amber-500/15 dark:text-amber-300', block: 'bg-rose-50 font-medium text-rose-700 dark:bg-rose-500/15 dark:text-rose-300' };
const STATE = { allowed: 'pill-ok', blocked: 'pill-bad', not_listed: 'pill-idle', needs_person: 'pill-wait' };

export function accessPane(it, admin, onChange) {
  const box = h('div', { class: 'space-y-4', id: 'integration-access' }, spinner());
  let view = null;

  async function load() {
    try { view = await get('/v1/integrations/' + encodeURIComponent(it.id) + '/policy'); } catch (e) { return mount(box, errorBox(e.message)); }
    draw();
  }

  async function set(name, key) {
    try { view = await put('/v1/integrations/' + encodeURIComponent(it.id) + '/policy', policyRequest(view.tools, name, key)); toast(key === 'block' ? name + ' is blocked' : key === 'ask' ? name + ' needs approval' : name + ' is allowed', 'ok'); onChange && onChange(); }
    catch (e) { toast(e.message, 'error'); }
    draw();
  }

  const toolIsWrite = (name) => (view.tools.find(t => t.name === name) || {}).kind === 'write';

  function row(t) {
    const c = toolChoice(t);
    const seg = h('div', { class: 'inline-flex overflow-hidden rounded-lg border border-slate-300 text-xs dark:border-slate-700', role: 'group', 'aria-label': 'Access for ' + t.name }, c.options.map(o => h('button', {
      type: 'button', 'data-tool': t.name, 'data-level': o.key, 'aria-pressed': String(c.shown === o.key), disabled: !o.enabled || !admin, title: o.why || null,
      class: 'border-l border-slate-300 px-2.5 py-1 first:border-l-0 dark:border-slate-700 ' + (c.shown === o.key ? SEG[o.key] : 'text-slate-500') + ((!o.enabled || !admin) && c.shown !== o.key ? ' opacity-40' : ''),
      onclick: () => { if (c.shown !== o.key) set(t.name, o.key); } }, o.label)));
    return h('tr', { 'data-tool-row': t.name },
      h('td', { class: 'py-2 pr-3 align-top' }, h('code', { class: 'font-mono text-xs font-semibold' }, t.name), h('div', { class: 'muted text-xs' }, t.help)),
      h('td', { class: 'py-2 pr-3 align-top' }, h('span', { class: t.kind === 'write' ? 'pill-wait' : 'pill-run' }, t.kind)),
      h('td', { class: 'py-2 align-top' }, seg));
  }

  function draw() {
    const blocked = view.tools.filter(t => t.level === 'block').length, asks = view.tools.filter(t => t.level === 'ask').length;
    mount(box,
      !view.readable ? notice('warn', 'The stored access policy cannot be read, so every tool of this integration is refused until it is fixed. It will not be changed over from here.') : null,
      h('div', {}, h('div', { class: 'h-section mb-1' }, 'The ceiling for this connection'),
        h('p', { class: 'muted text-xs' }, 'No agent, assistant or MCP client can use a blocked tool, whatever its own list says. A tool that needs approval asks the person in the assistant before each call, and is refused wherever nobody is present (an agent run, an MCP client). Changes are never Allow: they stay proposals that a different administrator approves. '
          + (admin ? '' : 'Only administrators can change this.'))),
      h('div', { class: 'overflow-x-auto' }, h('table', { class: 'w-full text-sm' }, h('thead', {}, h('tr', { class: 'muted text-left text-xs uppercase' }, h('th', { class: 'pb-1 pr-3' }, 'Tool'), h('th', { class: 'pb-1 pr-3' }, 'Kind'), h('th', { class: 'pb-1' }, 'Access'))),
        h('tbody', { class: 'divide-y divide-slate-100 dark:divide-slate-800' }, view.tools.map(row)))),
      h('p', { class: 'muted text-xs', id: 'access-summary' }, (blocked ? `${blocked} of ${view.tools.length} tools blocked.` : 'Nothing is blocked.') + (asks ? ` ${asks} need${asks === 1 ? 's' : ''} your approval each time.` : '') + (view.updated_by ? ' Last changed by ' + view.updated_by + '.' : '')),
      h('div', {}, h('div', { class: 'h-section mb-1' }, 'What each agent can do'),
        view.agents.length ? h('div', { class: 'overflow-x-auto' }, h('table', { class: 'w-full text-sm', id: 'access-agents' },
          h('thead', {}, h('tr', { class: 'muted text-left text-xs uppercase' }, h('th', { class: 'pb-1 pr-3' }, 'Agent'), view.tools.map(t => h('th', { class: 'pb-1 pr-3 font-mono normal-case' }, t.name)))),
          h('tbody', { class: 'divide-y divide-slate-100 dark:divide-slate-800' }, view.agents.map(a => h('tr', { 'data-agent': a.id },
            h('td', { class: 'py-2 pr-3 font-medium' }, a.name, h('span', { class: 'muted ml-1 text-xs' }, a.tier)),
            a.tools.map(x => h('td', { class: 'py-2 pr-3' }, h('span', { class: STATE[x.state] || 'pill-idle', title: x.reason, 'data-state': x.state }, agentStateText(x.state)))))))))
          : h('p', { class: 'muted text-sm' }, 'No agent is set up yet. Agents get a tool list of their own; this view shows how it combines with the ceiling above.')),
      h('p', { class: 'muted text-xs' }, 'Blocked by this ceiling, or not in the agent\'s own tool list, or allowed. Hover a state for the reason.'));
  }

  load();
  return box;
}
