import { h, icon, mount, spinner, errorBox, notice, toast, field } from '../lib/dom.js';
import { get, put, post } from '../lib/api.js';

/** The assistant's settings card on the Connections page. */
export async function assistantSettings(box, admin) {
  mount(box, spinner());
  let cfg;
  try { cfg = await get('/v1/assistant'); } catch (e) { return mount(box, errorBox(e.message)); }

  const backend = h('select', { id: 'as-backend', class: 'input', disabled: !admin },
    h('option', { value: 'off', selected: cfg.backend === 'off' }, 'Off'),
    h('option', { value: 'provider', selected: cfg.backend === 'provider' }, 'A model from Models and providers'),
    h('option', { value: 'api', selected: cfg.backend === 'api' }, 'Anthropic API key (recommended for teams)'),
    h('option', { value: 'claude-token', selected: cfg.backend === 'claude-token' }, 'Claude Code login token (from claude setup-token)'),
    h('option', { value: 'cli', selected: cfg.backend === 'cli' }, 'The claude program on this server (one person only)'),
    h('option', { value: 'copilot', selected: cfg.backend === 'copilot' }, 'The copilot program on this server (GitHub Copilot, one person only)'));
  const model = h('input', { id: 'as-model', class: 'input font-mono', value: cfg.backend === 'provider' ? '' : cfg.model, disabled: !admin });
  const modelField = field('Model', model, 'Leave as is unless you know you need another.');
  // With a provider, the model is picked from the ones turned on under Settings › Models and providers (note 045).
  const provModel = h('select', { id: 'as-provider-model', class: 'input', disabled: !admin }, h('option', { value: cfg.backend === 'provider' ? cfg.model : '' }, cfg.backend === 'provider' ? cfg.model : 'Loading…'));
  const provField = field('Model', provModel, h('span', {}, 'Models turned on under ', h('a', { href: '#/providers', class: 'text-brand-600 hover:underline dark:text-brand-300' }, 'Models and providers'), '.'));
  get('/v1/models').then(r => mount(provModel, r.models.length ? r.models.map(m => h('option', { value: m.ref, selected: m.ref === cfg.model }, `${m.provider_name} · ${m.label}`))
    : h('option', { value: '' }, 'No models turned on yet'))).catch(() => {});
  const key = h('input', { id: 'as-key', class: 'input font-mono', type: 'password', autocomplete: 'new-password', placeholder: cfg.has_key ? 'A key is saved. Type to replace it.' : 'sk-ant-…', disabled: !admin });
  const keyField = field('API key', key, 'Stored encrypted and never shown again. Calls are billed to this key.');
  const tok = h('input', { id: 'as-token', class: 'input font-mono', type: 'password', autocomplete: 'new-password', placeholder: cfg.has_token ? 'A token is saved. Type to replace it.' : 'Paste the output of: claude setup-token', disabled: !admin });
  const tokField = field('Claude Code token', tok, 'Stored encrypted and never shown again. It is the same secret (CLAUDE_CODE_OAUTH_TOKEN) that Claude Code agent steps use. The program runs in a scratch home directory, so the server user\'s own login is not touched.');
  const cliWarn = notice('warn', 'This runs the ', h('code', { class: 'font-mono text-xs' }, 'claude'), ' program on the server as the server\'s own user, with that user\'s login, for everyone who can use the assistant. Use it only on a server for one person (a laptop). For a team, use an API key: sharing one personal subscription is not what it is licensed for.');
  const copilotWarn = notice('warn', 'This runs GitHub\'s ', h('code', { class: 'font-mono text-xs' }, 'copilot'), ' program on the server with the server user\'s login (run ', h('code', { class: 'font-mono text-xs' }, 'copilot login'), ' there first), with no tools. Each question uses one Copilot premium request; a free plan has only a few a month. Leave the model empty to let Copilot choose. Use it only on a server for one person.');
  const err = h('div', { class: 'mt-4 hidden' });
  const out = h('div', { class: 'mt-4' });
  // Copilot picks its own model when the field is empty; a Claude name left from another backend would only be
  // ignored, so it is cleared, and the field says what empty means.
  const fitModel = () => {
    if (backend.value === 'copilot' && /^(claude-|sonnet$|opus$|haiku$)/.test(model.value.trim())) model.value = '';
    model.placeholder = backend.value === 'copilot' ? 'Empty: Copilot chooses' : '';
  };
  const sync = () => { fitModel(); provField.classList.toggle('hidden', backend.value !== 'provider'); modelField.classList.toggle('hidden', backend.value === 'provider'); keyField.classList.toggle('hidden', backend.value !== 'api'); tokField.classList.toggle('hidden', backend.value !== 'claude-token'); cliWarn.classList.toggle('hidden', backend.value !== 'cli'); copilotWarn.classList.toggle('hidden', backend.value !== 'copilot'); };
  backend.addEventListener('change', sync); sync();

  const save = h('button', { class: 'btn-primary', type: 'submit', disabled: !admin }, icon('check', 'size-4'), 'Save');
  const test = h('button', { class: 'btn-secondary', type: 'button', disabled: !cfg.ready, onclick: async () => {
    test.disabled = true; mount(out, spinner());
    try { const r = await post('/v1/integrations/assistant/test'); mount(out, notice('ok', 'It answered: ', r.reply)); }
    catch (e) { mount(out, errorBox(e.message)); }
    test.disabled = false;
  } }, icon('bolt', 'size-4'), 'Test');

  mount(box, h('form', { class: 'card card-pad', novalidate: true, onsubmit: async (ev) => {
    ev.preventDefault(); err.classList.add('hidden'); save.disabled = true;
    try {
      await put('/v1/integrations/assistant', { backend: backend.value, model: backend.value === 'provider' ? provModel.value : model.value, api_key: key.value, claude_token: tok.value });
      toast('Assistant settings saved', 'ok');
      assistantSettings(box, admin);
    } catch (e) { mount(err, errorBox(e.message)); err.classList.remove('hidden'); save.disabled = false; }
  } },
    h('div', { class: 'mb-5 flex items-center justify-between' }, h('h2', { class: 'flex items-center gap-2 text-base font-semibold' }, icon('sparkle'), 'Assistant'),
      cfg.ready ? h('span', { class: 'pill-ok' }, 'Ready') : h('span', { class: 'pill-idle' }, cfg.backend === 'off' ? 'Off' : 'Needs a key or token')),
    h('p', { class: 'muted mb-5 text-sm' }, 'A helper in the top bar that answers questions about the page you are on: why a run failed, what a pipeline does, what is running in AWS. It can only read, never sees secret values, and anything it drafts is text until you save it.'),
    h('div', { class: 'space-y-5' }, field('Which assistant to use', backend), cliWarn, copilotWarn, keyField, tokField, provField, modelField),
    err, admin && h('div', { class: 'mt-6 flex items-center gap-2' }, save, test), out));
}
