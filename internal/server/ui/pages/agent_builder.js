// The agent builder (design note 045, decision 6): what an agent is made of, as parts, with one side panel to pick
// models, skills, MCP servers, tools and guardrails, and the agent to talk to beside it. Every save is a new version.
import { h, icon, mount, spinner, errorBox, notice, toast, field, drawer, brandMark, leaveDialog } from '../lib/dom.js';
import { get, post, atLeast } from '../lib/api.js';
import { navigate } from '../lib/router.js';
import { session } from '../app.js';
import { mdEditor } from '../lib/mdeditor.js';
import { checkInstructions } from '../lib/markdown.js';
import { TIERS } from '../lib/tools.js';
import { TABS, draftFrom, draftBody, changed, pickerGroups, tabFilters, toggle, tabSummary, partChips } from '../lib/builder.js';
import { avatar, chatPanel } from './agents.js';

const TONE = { ok: 'pill-ok', warn: 'pill-wait', info: 'pill-idle' };
const tagPill = (t) => h('span', { class: TONE[t.tone] + ' px-1.5 py-0 text-[11px]' }, t.text);

/** Agents › an agent › Build (or a new agent when params.id is "new"). */
export async function agentBuilderPage({ params }) {
  const el = h('div', {});
  if (!atLeast(session.role, 'admin')) return mount(el, notice('info', 'Only administrators change agents.'));
  mount(el, spinner());
  let ag = null, data;
  try {
    const [skills, tools, models] = await Promise.all([get('/v1/ai/skills').then(r => r.skills), get('/v1/ai/tools').then(r => r.tools), get('/v1/models').then(r => r.models).catch(() => [])]);
    data = { skills, tools, models };
    if (params.id !== 'new') ag = await get('/v1/ai/agents/' + encodeURIComponent(params.id));
  } catch (e) { return mount(el, errorBox(e.message)); }
  const cur = ag && ag.versions.find(v => v.n === ag.current);
  const base = draftFrom(ag, cur);
  let d = draftFrom(ag, cur), problem = '';

  const status = h('span', { class: 'text-xs', id: 'builder-dirty' });
  const modelChip = h('button', { type: 'button', class: 'inline-flex items-center gap-2 rounded-full border border-slate-200 bg-white py-1 pl-1 pr-3 text-sm hover:border-brand-400 dark:border-slate-700 dark:bg-slate-900', id: 'builder-model', onclick: () => openPanel('Models') });
  const parts = h('div', { class: 'space-y-3', id: 'builder-parts' });
  const problemBox = h('div', {});
  const drawStatus = () => mount(status, !ag ? h('span', { class: 'pill-wait' }, 'Not saved yet') : changed(d, base) ? h('span', { class: 'pill-wait' }, 'Draft of v' + (ag.current + 1) + ' · unsaved') : h('span', { class: 'muted' }, 'v' + ag.current + ' is current'));
  const drawModel = () => {
    const m = data.models.find(x => x.ref === d.model);
    mount(modelChip, brandMark(m ? m.kind : 'anthropic', 'sm'), h('span', { class: 'font-medium' }, m ? m.label : d.model || 'The assistant’s model'),
      h('span', { class: 'muted text-xs' }, m ? m.provider_name : d.model ? 'through the assistant' : ''), icon('chevron', 'size-3 rotate-90 text-slate-400'));
  };
  const set = (next) => { d = next; problem = ''; drawStatus(); drawModel(); drawParts(); mount(problemBox, null); };

  // The instructions are edited in place; everything else is picked in the side panel.
  const ins = mdEditor({ id: 'agent-ins', value: d.instructions, maxlength: 4096, height: 'h-44', mode: 'write', placeholder: '## Goal\nFind idle or oversized resources and propose the smallest safe change.\n\n## Rules\n- Look before you suggest anything.',
    check: (t) => checkInstructions(t, { tier: d.tier, max: 4096 }) });
  ins.input.addEventListener('input', () => { d = { ...d, instructions: ins.input.value }; drawStatus(); });

  function part(title, hint, kind, tab, chips, extra = null) {
    return h('section', { class: 'card card-pad space-y-3', 'data-part': tab },
      h('div', { class: 'flex items-start justify-between gap-3' },
        h('div', { class: 'flex min-w-0 items-center gap-3' }, brandMark(kind, 'sm'), h('div', { class: 'min-w-0' }, h('h2', { class: 'font-semibold' }, title), h('p', { class: 'muted text-xs' }, hint))),
        h('button', { type: 'button', class: 'btn-secondary btn-sm', 'data-open': tab, onclick: () => openPanel(tab) }, icon(chips.length || extra ? 'edit' : 'plus', 'size-3.5'), chips.length || extra ? 'Change' : 'Add')),
      chips.length ? h('div', { class: 'flex flex-wrap gap-1.5' }, chips.map(c => h('span', { class: 'inline-flex items-center gap-1 rounded-full border border-slate-200 bg-slate-50 py-0.5 pl-2.5 pr-1 text-xs dark:border-slate-700 dark:bg-slate-800/60', 'data-chip': c.key },
        c.label, h('button', { type: 'button', class: 'rounded-full p-0.5 hover:bg-slate-200 dark:hover:bg-slate-700', 'aria-label': 'Remove ' + c.label, onclick: () => set(toggle(d, tab, c.key)) }, icon('x', 'size-3'))))) : null,
      extra);
  }

  function drawParts() {
    const tier = TIERS.find(t => t.id === d.tier) || TIERS[0];
    const mcp = partChips('MCP servers', data, d), tools = partChips('Tools', data, d), skills = partChips('Skills', data, d);
    mount(parts,
      h('section', { class: 'card card-pad space-y-3', 'data-part': 'Instructions' }, h('div', { class: 'flex items-center gap-3' }, brandMark('forgeline', 'sm'),
        h('div', {}, h('h2', { class: 'font-semibold' }, 'Instructions'), h('p', { class: 'muted text-xs' }, 'What it should do and how, in Markdown. The model receives exactly this text.'))), ins.el),
      part('Skills', skills.length ? 'Step-by-step procedures it follows, each pinned to its version' : 'None yet: procedures it can follow, from Agents › Skills or the Library', 'library', 'Skills', skills),
      part('MCP servers', mcp.length ? 'Read-only tools from connected MCP servers' : data.tools.some(t => t.needs === 'mcp-server') ? 'None picked' : 'No MCP server tools are turned on yet', 'mcp', 'MCP servers', mcp),
      part('Tools', tools.length ? 'What it can look at or ask for in Forgeline and your clouds' : 'Nothing: it can only answer from what you tell it', 'forgeline', 'Tools', tools),
      part('Guardrails', tier.text, 'guard', 'Guardrails', [], h('div', { class: 'flex flex-wrap gap-1.5 text-xs' }, h('span', { class: d.tier === 'T1' ? 'pill-wait' : 'pill-ok' }, tier.title),
        h('span', { class: 'pill-idle', title: 'Works as the person who asks; a different administrator approves changes; tool results are data; its runs are audited' }, icon('lock', 'size-3'), '4 rules always on'),
        ag?.budget?.tokens ? h('span', { class: 'pill-idle' }, 'budget ' + ag.budget.tokens.toLocaleString() + ' tokens a month') : null)));
  }

  // One side panel for every part, with a tab each.
  function openPanel(tab) {
    let query = '', filters = [];
    const foot = h('span', { class: 'muted mr-auto text-xs', id: 'picker-summary' });
    return drawer('Add to this agent', () => {
      const box = h('div', { class: 'space-y-4' });
      const draw = () => {
        const groups = pickerGroups(tab, data, d, query, filters), offer = tabFilters(tab, data, d);
        mount(foot, tabSummary(tab, data, d));
        const search = h('input', { class: 'input', type: 'search', id: 'picker-search', value: query, placeholder: 'Search ' + tab.toLowerCase(), 'aria-label': 'Search', autocomplete: 'off',
          oninput: (e) => { query = e.target.value; const at = e.target.selectionStart; draw(); const s = box.querySelector('#picker-search'); s.focus(); s.setSelectionRange(at, at); } });
        mount(box,
          h('div', { class: 'flex flex-wrap gap-1 border-b border-slate-200 dark:border-slate-800', role: 'tablist' }, TABS.map(t => h('button', { type: 'button', role: 'tab', 'aria-selected': String(t === tab), 'data-tab': t,
            class: '-mb-px border-b-2 px-3 py-2 text-sm font-medium ' + (t === tab ? 'border-brand-600 text-brand-700 dark:text-brand-300' : 'border-transparent text-slate-500 hover:text-slate-900 dark:hover:text-white'),
            onclick: () => { tab = t; query = ''; filters = []; draw(); } }, t))),
          tab !== 'Guardrails' ? search : null,
          offer.length && tab !== 'Guardrails' ? h('div', { class: 'flex flex-wrap gap-1.5', id: 'picker-filters' }, offer.map(f => h('button', { type: 'button', 'aria-pressed': String(filters.includes(f)), 'data-filter': f,
            class: 'rounded-full border px-2.5 py-0.5 text-xs ' + (filters.includes(f) ? 'border-brand-500 bg-brand-50 text-brand-800 dark:bg-brand-500/10 dark:text-brand-200' : 'border-slate-200 text-slate-600 hover:border-slate-300 dark:border-slate-700 dark:text-slate-300'),
            onclick: () => { filters = filters.includes(f) ? filters.filter(x => x !== f) : [...filters, f]; draw(); } }, f))) : null,
          groups.length ? h('div', { class: 'space-y-5', id: 'picker-groups' }, groups.map(g => h('section', { 'data-group': g.id },
            h('div', { class: 'mb-2 flex items-center gap-2' }, brandMark(g.kind, 'sm'), h('div', { class: 'min-w-0' }, h('h3', { class: 'text-sm font-semibold' }, g.label), g.note ? h('p', { class: 'muted text-xs' }, g.note) : null)),
            h('div', { class: 'grid gap-1.5' }, g.items.map(it => {
              const radio = tab === 'Models' || g.radio;
              // A rule that is always on is not a control: a row, not a button.
              return h(it.locked ? 'div' : 'button', { ...(it.locked ? {} : { type: 'button', role: radio ? 'radio' : 'checkbox', 'aria-checked': String(it.on) }), 'data-pick': it.key, 'data-locked': it.locked ? '' : null,
                class: 'flex w-full items-start gap-3 rounded-lg border p-2.5 text-left ' + (it.locked ? 'border-slate-200 bg-slate-50 dark:border-slate-700 dark:bg-slate-800/50' : it.on ? 'border-brand-300 bg-brand-50/70 dark:border-brand-500/50 dark:bg-brand-500/10' : 'border-slate-200 hover:border-slate-300 dark:border-slate-700 dark:hover:border-slate-600') + (it.locked ? ' cursor-default' : ''),
                onclick: it.locked ? null : () => { set(toggle(d, tab, it.key)); draw(); } },
                h('span', { class: 'mt-0.5 flex size-4 flex-none items-center justify-center border ' + (radio ? 'rounded-full ' : 'rounded ') + (it.locked ? 'border-slate-500 bg-slate-500 text-white' : it.on ? 'border-brand-600 bg-brand-600 text-white' : 'border-slate-400') },
                  it.locked ? icon('lock', 'size-2.5') : it.on ? (radio ? h('span', { class: 'size-1.5 rounded-full bg-white' }) : icon('check', 'size-3')) : null),
                h('span', { class: 'min-w-0 flex-1' }, h('span', { class: 'block text-sm font-medium' }, it.label), it.desc ? h('span', { class: 'muted block text-xs' }, it.desc) : null),
                it.tags.length ? h('span', { class: 'flex flex-none flex-wrap justify-end gap-1' }, it.tags.map(tagPill)) : null);
            }))))) : h('p', { class: 'muted text-sm', id: 'picker-empty' }, emptyText(tab, query)),
          tab === 'MCP servers' ? h('a', { class: 'btn-secondary btn-sm', href: '#/mcp-servers', id: 'picker-connect' }, icon('plug', 'size-3.5'), 'Connect a new MCP server') : null,
          tab === 'Models' ? h('a', { class: 'btn-ghost btn-sm', href: '#/settings/providers' }, icon('gear', 'size-3.5'), 'Models and providers') : null);
      };
      draw();
      return box;
    }, { subtitle: d.name || 'A new agent', footer: (close) => [foot, h('button', { type: 'button', class: 'btn-primary btn-sm', id: 'picker-done', onclick: () => close() }, 'Done')] });
  }

  let saved = false;
  async function save(btn, stay = false) {
    btn.disabled = true;
    d = { ...d, instructions: ins.input.value };
    try {
      const out = await post(ag ? '/v1/ai/agents/' + ag.id + '/versions' : '/v1/ai/agents', draftBody(d));
      toast(ag ? 'Saved v' + out.current : 'Agent created', 'ok');
      saved = true;
      if (!stay) navigate('/agents/' + out.id);
      return true;
    } catch (e) { problem = e.message; mount(problemBox, h('div', { class: 'mb-3' }, errorBox(problem))); btn.disabled = false; if (stay) throw e; return false; }
  }

  const name = h('input', { id: 'agent-name', class: 'input w-56 max-w-full font-semibold', maxlength: '60', value: d.name, placeholder: 'Name the agent', 'aria-label': 'Name', oninput: (e) => { d = { ...d, name: e.target.value }; drawStatus(); } });
  const desc = h('input', { id: 'agent-desc', class: 'input w-full', maxlength: '300', value: d.description, placeholder: 'What it is for', 'aria-label': 'What it is for', oninput: (e) => { d = { ...d, description: e.target.value }; drawStatus(); } });
  const note = h('input', { id: 'agent-note', class: 'input w-52 max-w-full', maxlength: '200', placeholder: ag ? 'What changed and why' : 'First version', 'aria-label': 'Note for this version', oninput: (e) => { d = { ...d, note: e.target.value }; } });
  const saveBtn = h('button', { class: 'btn-primary', type: 'button', id: 'agent-save', onclick: () => save(saveBtn) }, icon('check', 'size-4'), ag ? 'Save version' : 'Create agent');

  // Talk to it: the saved version, beside what is being built. Trying an unsaved draft comes with the playground.
  const tryIt = ag && cur ? h('div', { class: 'space-y-2' }, chatPanel(ag, cur), h('p', { class: 'muted px-1 text-xs', id: 'builder-try-note' }, 'Talks to the saved v' + cur.n + '. Save to try your changes.'))
    : h('aside', { class: 'card card-pad muted text-sm' }, 'Create the agent, then talk to it here.');

  drawStatus(); drawModel(); drawParts();
  mount(el,
    h('div', { class: 'mb-4 flex flex-wrap items-center gap-3' },
      h('a', { class: 'btn-ghost btn-sm', href: ag ? '#/agents/' + ag.id : '#/agents', 'aria-label': 'Back' }, icon('chevron', 'size-4 rotate-180'), ag ? ag.name : 'Agents'),
      avatar(d.name || 'New', ag?.id || 'new', 'size-9 text-xs'), name, status, h('div', { class: 'flex-1' }), modelChip, note, saveBtn),
    h('div', { class: 'mb-4' }, desc),
    problemBox,
    h('div', { class: 'grid gap-4 lg:grid-cols-5' },
      h('div', { class: 'min-w-0 lg:col-span-3' }, h('p', { class: 'muted mb-2 text-sm' }, 'What this agent is made of. Add or Change on a part opens the side panel.'), parts),
      h('div', { class: 'min-w-0 lg:col-span-2 lg:sticky lg:top-20 lg:self-start' }, tryIt)));
  // Leaving with a draft that differs from what was opened asks first (an untouched new agent does not).
  el.leaveGuard = () => ({ dirty: !saved && changed({ ...d, instructions: ins.input.value }, base), ask: () => leaveDialog({ what: 'this agent', save: () => save({}, true) }) });
  return el;
}

function emptyText(tab, query) {
  if (query) return 'Nothing matches “' + query + '”.';
  if (tab === 'Models') return 'No models yet. Add a provider under Settings › Models and providers.';
  if (tab === 'Skills') return 'No skills yet. Write one under Agents › Skills or install one from the Library.';
  if (tab === 'MCP servers') return 'No MCP server tools are turned on. An administrator connects a server and turns on its read-only tools.';
  return 'Nothing here.';
}
