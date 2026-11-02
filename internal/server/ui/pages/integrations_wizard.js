// The connect wizard inside an integration's popup (design note 025): the same few steps for every system.
// It is offered for Google Cloud and Azure, which Forgeline signs in to through the login already on the
// server, so the steps are: choose what to read, connect (the server proves it can read it first), test.
// Other systems keep their own connect flows and the popup links to them.
import { h, icon, mount, spinner, errorBox, notice } from '../lib/dom.js';
import { get, put, post } from '../lib/api.js';
import { normalizeGcpScope, normalizeAzureSubs } from '../lib/hub.js';
import { renderChecks } from './integration_parts.js';

export const wizardFor = (id) => id === 'gcp' || id === 'azure';

const STEPS = ['Choose', 'Connect', 'Test'];

/** Returns the wizard element. onDone() is called once the connection is saved, so the hub can refresh. */
export function connectWizard(it, { close, onDone }) {
  const root = h('div', { class: 'space-y-4', id: 'connect-wizard', 'data-integration': it.id });
  const state = { step: 0, scope: '', chosen: new Set(), available: null, detected: '', error: '', busy: false };
  const isGcp = it.id === 'gcp';

  async function start() {
    mount(root, spinner());
    try {
      const cfg = await get('/v1/integrations/' + it.id);
      if (isGcp) { state.detected = cfg.detected_project || ''; state.scope = state.detected; } else state.available = cfg.available || [];
    } catch (e) { return mount(root, errorBox(e.message)); }
    draw();
  }

  const bar = () => h('div', { class: 'flex flex-wrap gap-2', 'aria-label': 'Steps' }, STEPS.map((n, i) => h('div', {
    class: 'flex items-center gap-2 rounded-full border px-3 py-1 text-xs ' + (i === state.step ? 'border-brand-500 font-medium' : 'border-slate-200 text-slate-500 dark:border-slate-700'), 'aria-current': i === state.step ? 'step' : null },
    h('span', { class: 'flex size-5 items-center justify-center rounded-full text-[11px] ' + (i < state.step ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-300' : i === state.step ? 'bg-brand-600 text-white' : 'bg-slate-100 dark:bg-slate-800') }, i < state.step ? '✓' : String(i + 1)), n)));

  function need() {
    const code = (t) => h('code', { class: 'font-mono text-xs' }, t);
    return h('div', { class: 'rounded-lg bg-slate-50 p-3 text-sm dark:bg-slate-800/60' }, h('div', { class: 'mb-1 font-medium' }, 'What the login on the server needs'),
      h('ul', { class: 'list-disc space-y-1 pl-5 text-slate-600 dark:text-slate-300' }, isGcp
        ? [h('li', {}, 'You have run ', code('gcloud auth login'), ' as the server\'s user.'), h('li', {}, 'It has the Cloud Asset Viewer role (', code('roles/cloudasset.viewer'), ') on the scope.'), h('li', {}, 'The Cloud Asset API (', code('cloudasset.googleapis.com'), ') is enabled.')]
        : [h('li', {}, 'You have run ', code('az login --use-device-code'), ' as the server\'s user.'), h('li', {}, 'It has the Reader role on the subscriptions.'), h('li', {}, 'The Resource Graph extension is installed: ', code('az extension add --name resource-graph'), '.')]));
  }

  function choose() {
    const err = h('p', { class: 'hidden text-sm text-rose-600 dark:text-rose-400', role: 'alert', id: 'wizard-error' });
    const next = () => {
      let r;
      if (isGcp) { r = normalizeGcpScope(state.scope); if (r.scope) state.scope = r.scope; } else { r = normalizeAzureSubs([...state.chosen]); }
      if (r.error) { err.textContent = r.error; err.classList.remove('hidden'); return; }
      state.step = 1; state.error = ''; draw();
    };
    let body;
    if (isGcp) {
      const input = h('input', { id: 'wizard-scope', class: 'input font-mono', value: state.scope, placeholder: 'my-project', 'aria-describedby': 'wizard-scope-help', oninput: (e) => { state.scope = e.target.value; } });
      body = h('div', { class: 'space-y-2' }, h('label', { for: 'wizard-scope', class: 'block text-sm font-medium' }, 'What should Forgeline see?'), input,
        h('p', { id: 'wizard-scope-help', class: 'muted text-xs' }, 'A project id, or projects/…, folders/… or organizations/…. ' + (state.detected ? 'The server\'s gcloud default project is filled in.' : '')));
    } else {
      const subs = state.available;
      body = h('div', { class: 'space-y-2' }, h('div', { class: 'text-sm font-medium' }, 'Which subscriptions?'),
        subs.length ? h('div', { class: 'space-y-1.5', role: 'group', 'aria-label': 'Subscriptions' }, subs.map(s => h('label', { class: 'flex items-center gap-2 text-sm' },
          h('input', { type: 'checkbox', class: 'size-4 rounded', 'data-sub': s.id, checked: state.chosen.has(s.id), onchange: (e) => { if (e.target.checked) state.chosen.add(s.id); else state.chosen.delete(s.id); } }),
          h('span', { class: 'font-medium' }, s.name), h('span', { class: 'muted font-mono text-xs' }, s.id))))
          : h('p', { class: 'muted text-sm' }, 'No subscription is visible to the login yet. Sign in on the server first; you can still continue and read every subscription the login can see.'),
        h('p', { class: 'muted text-xs' }, 'Choose none to read every subscription the login can see. Narrower is safer.'));
    }
    return h('div', { class: 'space-y-4' }, body, need(), err,
      h('div', { class: 'flex gap-2' }, h('button', { type: 'button', class: 'btn-primary', id: 'wizard-next', onclick: next }, 'Continue'), h('button', { type: 'button', class: 'btn-ghost', onclick: () => close() }, 'Cancel')));
  }

  function connect() {
    const what = isGcp ? state.scope : (state.chosen.size ? state.chosen.size + ' subscription' + (state.chosen.size === 1 ? '' : 's') : 'every subscription the login can read');
    const out = h('div', { class: 'space-y-4' }, h('p', { class: 'text-sm' }, 'Forgeline will prove the login can read ', h('strong', {}, what), ' before it saves anything. It only reads.'));
    if (state.error) out.append(errorBox(state.error));
    out.append(h('div', { class: 'flex gap-2' },
      h('button', { type: 'button', class: 'btn-secondary', onclick: () => { state.step = 0; draw(); } }, 'Back'),
      h('button', { type: 'button', class: 'btn-primary', id: 'wizard-connect', disabled: state.busy, onclick: async (e) => {
        e.target.disabled = true; state.busy = true;
        try {
          await put('/v1/integrations/' + it.id, isGcp ? { enabled: true, scope: state.scope } : { enabled: true, subscriptions: [...state.chosen] });
          state.step = 2; state.error = ''; onDone && onDone();
        } catch (x) { state.error = x.message; }
        state.busy = false; draw();
      } }, icon('check', 'size-4'), 'Connect')));
    return out;
  }

  function test() {
    const box = h('div', { class: 'space-y-1.5', 'aria-live': 'polite', id: 'integration-test' }, spinner());
    const run = async () => {
      mount(box, spinner());
      try { renderChecks(box, await post('/v1/integrations/' + it.id + '/checks')); } catch (e) { mount(box, errorBox(e.message)); }
    };
    run();
    return h('div', { class: 'space-y-4' }, notice('info', 'Connected. This test only reads, and it runs again on its own from the card.'), box,
      h('div', { class: 'flex flex-wrap gap-2' }, h('button', { type: 'button', class: 'btn-secondary', onclick: run }, 'Run again'),
        h('a', { class: 'btn-primary', href: '#' + it.manage, onclick: () => close() }, 'Open the explorer')));
  }

  function draw() {
    mount(root, bar(), [choose, connect, test][state.step]());
  }
  start();
  return root;
}
