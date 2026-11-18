// Settings › Models and providers (design note 045, part 1). Providers as cards with their mark, status, models,
// spend against the monthly limit and speed; a live log of recent model calls beside them. Adding or changing a
// provider happens in a side panel, so the cards and the log stay in view. Anthropic, OpenAI and OpenAI-compatible
// servers (Ollama, vLLM, OpenRouter…) are built; the other kinds are shown as coming.
import { h, icon, mount, pageHeader, spinner, errorBox, notice, toast, field, relTime, absTime, confirmDialog, drawer, brandMark } from '../lib/dom.js';
import { get, post, put, del, atLeast } from '../lib/api.js';
import { session } from '../app.js';
import { budgetBar } from './ai_use.js';

// What each built kind needs in the side panel.
const KIND = {
  anthropic: { host: 'https://api.anthropic.com', key: 'sk-ant-…' },
  openai: { host: 'https://api.openai.com/v1', key: 'sk-…' },
  'openai-compatible': { host: '', key: 'Only if the server asks for one', needsAddress: true, keyOptional: true,
    example: 'http://localhost:11434/v1 (Ollama), https://openrouter.ai/api/v1, https://api.groq.com/openai/v1' },
};

const STATUS = {
  working: ['bg-emerald-500', 'Working'],
  failing: ['bg-rose-500', 'Failing'],
  untested: ['bg-slate-400', 'Not used yet'],
  'no-key': ['bg-amber-500', 'No key'],
};

export async function providersPage() {
  const el = h('div', {});
  const admin = atLeast(session.role, 'admin');
  let data = { providers: [], kinds: [] };
  const logBox = h('div', { id: 'model-calls' });
  let timer = null;

  async function load() {
    mount(el, pageHeader('Models and providers', ''), spinner());
    try {
      data = await get('/v1/providers');
      await loadCalls();
    } catch (e) { mount(el, pageHeader('Models and providers', ''), errorBox(e.message)); return; }
    render();
  }

  function render() {
    const ps = data.providers;
    const on = ps.reduce((n, p) => n + p.models.filter(m => m.on).length, 0);
    const spent = ps.reduce((n, p) => n + (p.month.cost_usd || 0), 0);
    const added = new Set(ps.map(p => p.kind));
    const more = data.kinds.filter(k => !k.built || !added.has(k.kind));
    mount(el, pageHeader('Models and providers',
      ps.length ? `${ps.length} connected · ${on} model${on === 1 ? '' : 's'} on · ${money(spent)} this month` : 'Connect a provider so agents and workflows can use its models, whatever the assistant is set to.',
      admin ? h('button', { class: 'btn-primary', type: 'button', id: 'provider-add', onclick: () => edit(null, 'anthropic') }, icon('plus', 'size-4'), 'Add a provider') : null),
      !admin ? h('div', { class: 'mb-4' }, notice('info', 'Only administrators can add or change providers.')) : null,
      h('div', { class: 'grid gap-6 2xl:grid-cols-[minmax(0,1fr)_28rem]' },
        h('div', { class: 'space-y-6' },
          ps.length ? h('div', { class: 'grid gap-4 sm:grid-cols-[repeat(auto-fill,minmax(17rem,1fr))]', id: 'provider-cards' }, ps.map(card)) : null,
          more.length ? h('div', {},
            h('h2', { class: 'h-section mb-2' }, ps.length ? 'More providers' : 'Providers'),
            h('div', { class: 'grid gap-3 sm:grid-cols-[repeat(auto-fill,minmax(14rem,1fr))]' }, more.map(kindCard))) : null,
          notice('info', 'A model from a provider is written provider/model, such as anthropic/claude-haiku-5-5, and works for agents and workflows whatever the assistant is set to. Keys are kept encrypted and never shown again. Every call is in the log and counts against the provider’s monthly limit.')),
        h('section', { class: 'card self-start', 'aria-label': 'Recent model calls' },
          h('div', { class: 'flex items-center gap-2 border-b border-slate-200 px-4 py-3 dark:border-slate-800' },
            h('h2', { class: 'font-semibold' }, 'Recent model calls'),
            h('span', { class: 'muted ml-auto text-xs' }, 'live'), h('span', { class: 'size-2 rounded-full bg-emerald-500', 'aria-hidden': 'true' })),
          logBox)));
    clearInterval(timer);
    timer = setInterval(() => { if (!el.isConnected) { clearInterval(timer); return; } loadCalls().catch(() => {}); }, 15000);
  }

  function card(p) {
    const st = STATUS[p.status] || STATUS.untested;
    const models = p.models.filter(m => m.on);
    const limit = p.monthly_usd || 0;
    const spent = p.month.cost_usd || 0;
    const pct = limit ? Math.min(100, Math.round(spent / limit * 100)) : 0;
    const where = hostOf(p.base_url || (KIND[p.kind] || {}).host || '');
    return h('article', { class: 'card card-pad flex flex-col gap-4 ' + (p.status === 'failing' ? 'ring-1 ring-rose-300 dark:ring-rose-800' : ''), 'data-provider': p.id },
      h('div', { class: 'flex items-center gap-3' }, brandMark(p.kind),
        h('div', { class: 'min-w-0 flex-1' },
          h('h3', { class: 'truncate font-semibold' }, p.name),
          h('p', { class: 'muted truncate text-xs' }, [kindLabel(p.kind), where, p.allow_private ? 'private network' : null].filter(Boolean).join(' · '))),
        admin ? h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Change ' + p.name, onclick: () => edit(p) }, icon('edit', 'size-3.5')) : null),
      h('div', { class: 'flex items-center gap-2 text-xs' }, h('span', { class: 'size-2 rounded-full ' + st[0], 'aria-hidden': 'true' }), h('span', { class: 'font-semibold' }, st[1]),
        h('span', { class: 'muted ml-auto' }, models.length + ' model' + (models.length === 1 ? '' : 's') + ' on')),
      p.last_error && p.status === 'failing' ? h('p', { class: 'text-xs text-rose-700 dark:text-rose-400' }, p.last_error) : null,
      limit ? budgetBar({ pct, level: pct >= 100 ? 'stop' : pct >= 80 ? 'warn' : 'ok', text: `${money(spent)} of ${money(limit)} this month` }) : null,
      h('div', { class: 'muted flex justify-between text-xs' },
        h('span', {}, limit ? `${money(spent)} of ${money(limit)}` : `${money(spent)} this month, no limit`),
        h('span', {}, p.median_millis ? secs(p.median_millis) + ' median' : '')),
      models.length ? h('div', { class: 'flex flex-wrap gap-1.5' }, models.map(m => h('span', { class: 'pill-idle font-mono' }, m.id))) : null,
      p.fallback ? h('p', { class: 'muted text-xs' }, 'Falls back to ', h('code', { class: 'font-mono' }, p.fallback)) : null);
  }

  function kindCard(k) {
    return h('article', { class: 'card flex items-center gap-3 p-3 ' + (k.built ? '' : 'opacity-60'), 'data-kind': k.kind },
      brandMark(k.kind, 'sm'),
      h('div', { class: 'min-w-0 flex-1' }, h('div', { class: 'truncate text-sm font-medium' }, k.label), h('div', { class: 'muted truncate text-xs', title: k.how }, k.how)),
      k.built && admin ? h('button', { class: 'btn-secondary btn-sm', type: 'button', onclick: () => edit(null, k.kind) }, 'Connect')
        : k.built ? null : h('span', { class: 'muted flex-none text-[10px] uppercase tracking-wide' }, 'soon'));
  }

  async function loadCalls() {
    const r = await get('/v1/model-calls');
    const calls = r.calls.slice(0, 30);
    mount(logBox, calls.length ? h('ul', { class: 'divide-y divide-slate-100 dark:divide-slate-800' }, calls.map(callRow))
      : h('p', { class: 'muted px-4 py-6 text-sm' }, 'No model calls yet. Calls from agents, workflows, the assistant and tests appear here.'));
  }

  function callRow(c) {
    const kind = (data.providers.find(p => p.id === c.provider) || {}).kind || c.provider;
    const meta = c.error ? c.error
      : [c.input_tokens || c.output_tokens ? `${num(c.input_tokens)} in · ${num(c.output_tokens)} out` : null, c.cost_usd ? money(c.cost_usd, 4) : null, c.millis ? secs(c.millis) : null].filter(Boolean).join(' · ');
    return h('li', { class: 'grid grid-cols-[1.75rem_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-0.5 px-4 py-2.5', 'data-call': c.provider },
      brandMark(kind, 'sm'),
      h('div', { class: 'flex min-w-0 items-baseline gap-2 text-sm' }, h('span', { class: 'truncate font-medium' }, c.who), h('span', { class: 'muted truncate font-mono text-[11px]' }, c.model || c.provider)),
      h('span', { class: c.error ? 'pill-bad' : 'pill-ok' }, c.error ? 'failed' : 'ok'),
      h('span', {}),
      h('span', { class: 'muted truncate text-xs' + (c.error ? ' text-rose-700 dark:text-rose-400' : ''), title: c.error || '' }, meta),
      h('span', { class: 'muted text-right text-xs', title: absTime(c.at) }, relTime(c.at)));
  }

  function edit(p, kind) {
    const k = data.kinds.find(x => x.kind === (p ? p.kind : kind)) || data.kinds[0];
    const known = new Map((k.models || []).map(m => [m.id, m]));
    // Every known model, with the provider's own settings where it has them, plus any it added by name.
    const rows = (p ? p.models : []).map(m => ({ ...known.get(m.id), ...m }));
    // A new provider starts with its two cheapest known models on.
    [...known.values()].forEach((m, i) => { if (!rows.some(r => r.id === m.id)) rows.push({ ...m, on: !p && i < 2 }); });
    const kk = KIND[k.kind] || KIND.anthropic;
    const v = (id, val, attrs = {}) => h('input', { id, class: 'input', value: val ?? '', ...attrs });
    const id = v('provider-id', p ? p.id : uniqueID(k.kind), { disabled: !!p });
    const name = v('provider-name', p ? p.name : k.label);
    const key = h('input', { id: 'provider-key', class: 'input font-mono', type: 'password', autocomplete: 'new-password', placeholder: p?.has_key ? 'A key is saved. Type to replace it.' : kk.key });
    const base = v('provider-base', p?.base_url, { placeholder: kk.host || 'http://localhost:11434/v1' });
    const newModel = v('provider-new-model', '', { class: 'input font-mono text-sm', placeholder: k.kind === 'openai-compatible' ? 'llama3.2:3b or meta-llama/llama-3.3-70b-instruct' : 'Another model by its exact name',
      onkeydown: (ev) => { if (ev.key === 'Enter') { ev.preventDefault(); addModel(); } } });
    const priv = h('input', { id: 'provider-private', type: 'checkbox', class: 'size-4', checked: !!p?.allow_private });
    const limit = v('provider-limit', p?.monthly_usd || '', { type: 'number', min: '0', step: '1', placeholder: 'No limit' });
    const fallback = h('select', { id: 'provider-fallback', class: 'input' });
    const modelList = h('ul', { class: 'divide-y divide-slate-100 rounded-xl border border-slate-200 dark:divide-slate-800 dark:border-slate-700', id: 'provider-models' });
    const err = h('div', {});
    const out = h('div', {});

    function addModel() {
      const name = newModel.value.trim();
      mount(err);
      if (!name) return;
      if (rows.some(r => r.id === name)) { mount(err, errorBox(`${name} is already in the list.`)); return; }
      rows.push({ id: name, on: true, input_usd_per_mtok: 0, output_usd_per_mtok: 0, added: true });
      newModel.value = ''; drawModels(); drawFallback();
    }

    const drawModels = () => mount(modelList, rows.length ? rows.map(m => {
      const cb = h('input', { type: 'checkbox', class: 'size-4', checked: !!m.on, 'aria-label': 'Turn on ' + m.id, onchange: () => { m.on = cb.checked; drawFallback(); } });
      const pin = h('input', { class: 'input w-20 py-1 text-xs', type: 'number', min: '0', step: '0.01', value: m.input_usd_per_mtok, 'aria-label': m.id + ' input price', onchange: () => { m.input_usd_per_mtok = Number(pin.value) || 0; } });
      const pout = h('input', { class: 'input w-20 py-1 text-xs', type: 'number', min: '0', step: '0.01', value: m.output_usd_per_mtok, 'aria-label': m.id + ' output price', onchange: () => { m.output_usd_per_mtok = Number(pout.value) || 0; } });
      return h('li', { class: 'flex flex-wrap items-center gap-3 px-3 py-2.5', 'data-model': m.id },
        h('label', { class: 'flex min-w-0 flex-1 items-center gap-3' }, cb,
          h('span', { class: 'min-w-0' }, h('span', { class: 'block text-sm font-medium' }, m.label || m.id), h('code', { class: 'muted block font-mono text-xs' }, m.id))),
        h('span', { class: 'flex items-center gap-1 text-xs' }, '$', pin, h('span', { class: 'muted' }, 'in'), '$', pout, h('span', { class: 'muted' }, 'out')),
        known.has(m.id) ? null : h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Take ' + m.id + ' off the list', onclick: () => { rows.splice(rows.indexOf(m), 1); drawModels(); drawFallback(); } }, icon('x', 'size-3.5')));
    }) : h('li', { class: 'muted px-3 py-4 text-sm' }, 'Add the models this server offers by their exact names.'));
    const drawFallback = () => {
      const mine = rows.filter(m => m.on).map(m => (p ? p.id : id.value.trim()) + '/' + m.id);
      const others = data.providers.filter(x => !p || x.id !== p.id).flatMap(x => x.models.filter(m => m.on).map(m => x.id + '/' + m.id));
      const cur = p?.fallback || fallback.value || '';
      mount(fallback, h('option', { value: '' }, 'No fallback'), [...others, ...mine].map(r => h('option', { value: r, selected: r === cur }, r)));
    };
    drawModels(); drawFallback();

    const body = () => ({ id: id.value.trim(), kind: k.kind, name: name.value.trim(), api_key: key.value.trim(), base_url: base.value.trim(), allow_private: priv.checked,
      monthly_usd: Number(limit.value) || 0, fallback: fallback.value,
      models: rows.map(m => ({ id: m.id, label: m.label || '', on: !!m.on, input_usd_per_mtok: Number(m.input_usd_per_mtok) || 0, output_usd_per_mtok: Number(m.output_usd_per_mtok) || 0 })) });

    drawer(p ? p.name : 'Connect ' + k.label, (close) => h('div', { class: 'space-y-5' },
      h('div', { class: 'flex items-center gap-3' }, brandMark(k.kind), h('div', {}, h('div', { class: 'font-semibold' }, k.label), h('div', { class: 'muted text-xs' }, k.how))),
      h('div', { class: 'grid grid-cols-2 gap-3' }, field('Id', id, 'Used in model names: id/model'), field('Name', name)),
      kk.needsAddress ? field('API address', base, 'Where the server answers OpenAI-style requests, ending in its version path. For example ' + kk.example + '. HTTPS, or plain HTTP only for this machine.') : null,
      field(kk.keyOptional ? 'API key (optional)' : 'API key', key, kk.keyOptional ? 'A local server usually needs none. A key you give is kept encrypted and never shown again.' : 'Kept encrypted in the vault and never shown again. Calls are billed to this key.'),
      h('section', { class: 'space-y-2' },
        h('div', { class: 'flex items-baseline justify-between' }, h('h3', { class: 'text-sm font-semibold' }, 'Models'), h('span', { class: 'muted text-xs' }, 'Prices in dollars per million tokens')),
        modelList,
        h('div', { class: 'flex gap-2' }, newModel, h('button', { class: 'btn-secondary btn-sm flex-none', type: 'button', id: 'provider-add-model', onclick: addModel }, icon('plus', 'size-3.5'), 'Add')),
        h('p', { class: 'muted text-xs' }, 'Agents and the assistant can only pick models that are turned on.' + (kk.keyOptional ? ' A model on your own server costs nothing here unless you give it a price.' : ''))),
      h('div', { class: 'grid grid-cols-2 gap-3' }, field('Monthly limit ($)', limit, 'At the limit, calls go to the fallback or stop'), field('Fallback', fallback, 'Also used when the provider is down or busy')),
      h('details', { class: 'rounded-xl border border-slate-200 p-3 dark:border-slate-700', open: !!(kk.needsAddress || p?.allow_private) },
        h('summary', { class: 'cursor-pointer text-sm font-medium' }, kk.needsAddress ? 'Network' : 'Address'),
        h('div', { class: 'mt-3 space-y-3' }, kk.needsAddress ? null : field('API address', base, `Leave empty for ${hostOf(kk.host)}. HTTPS only; plain HTTP only for this machine.`),
          h('label', { class: 'flex items-center gap-2 text-sm', for: 'provider-private' }, priv, 'It is on a private network (otherwise private and cloud-internal addresses are refused)'))),
      err, out), {
      subtitle: p ? `${kindLabel(p.kind)} · changed by ${p.updated_by} ${relTime(p.updated_at)}` : 'Add a provider; its models can then be given to agents and the assistant.',
      footer: (close) => [
        p ? h('button', { class: 'btn-ghost mr-auto text-rose-600 dark:text-rose-400', type: 'button', onclick: async () => {
          if (!await confirmDialog({ title: 'Remove ' + p.name + '?', body: 'Its key is deleted. Agents that use its models stop working until they are given another model.', confirmLabel: 'Remove', danger: true })) return;
          try { await del('/v1/providers/' + encodeURIComponent(p.id)); close(); toast('Removed', 'ok'); load(); } catch (e) { mount(err, errorBox(e.message)); }
        } }, icon('trash', 'size-4'), 'Remove') : null,
        p ? h('button', { class: 'btn-secondary', type: 'button', id: 'provider-test', onclick: async (ev) => {
          ev.currentTarget.disabled = true; mount(out, spinner());
          try { const r = await post('/v1/providers/' + encodeURIComponent(p.id) + '/test'); mount(out, notice('ok', `${r.model} answered “${r.reply}” in ${secs(r.millis)} for ${money(r.cost_usd, 5)}.`)); loadCalls(); }
          catch (e) { mount(out, errorBox(e.message)); }
          ev.target.disabled = false;
        } }, icon('bolt', 'size-4'), 'Test') : null,
        h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'),
        h('button', { class: 'btn-primary', type: 'button', id: 'provider-save', onclick: async () => {
          try {
            if (p) await put('/v1/providers/' + encodeURIComponent(p.id), body()); else await post('/v1/providers', body());
            close(); toast(p ? 'Saved' : 'Connected. Press Test to check the key.', 'ok'); load();
          } catch (e) { mount(err, errorBox(e.message)); }
        } }, p ? 'Save' : 'Connect')],
    });
  }

  function uniqueID(kind) {
    let id = kind, n = 2;
    while (data.providers.some(p => p.id === id)) id = kind + '-' + n++;
    return id;
  }
  function kindLabel(kind) { return (data.kinds.find(k => k.kind === kind) || {}).label || kind; }

  await load();
  return el;
}

function money(x, digits) {
  if (!x) return '$0';
  return '$' + x.toFixed(digits ?? (x < 1 ? 3 : 2));
}
function secs(ms) { return ms < 1000 ? ms + ' ms' : (ms / 1000).toFixed(1) + ' s'; }
function num(n) { return n >= 1000 ? (n / 1000).toFixed(1) + 'K' : String(n || 0); }
function hostOf(u) { try { return new URL(u).host; } catch { return u; } }
