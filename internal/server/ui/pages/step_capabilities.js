// Settings › Step container limits (design note 005, T2): every pipeline step runs with all Linux capabilities dropped. This page
// chooses what every step gets back. A step can list more in its own permissions, and each such grant is in the audit log.
import { h, icon, mount, pageHeader, spinner, errorBox, notice, toast } from '../lib/dom.js';
import { get, put } from '../lib/api.js';

const MODES = [
  { id: 'relaxed', title: 'Relaxed (the default for now)', text: 'Every step drops all capabilities and gets back a small set that ordinary builds and installs use. Raw network sockets, device nodes, file capabilities, capability changes and chroot stay dropped.' },
  { id: 'strict', title: 'Strict', text: 'Every step drops all capabilities and gets back nothing. A step that needs one lists it in its own permissions.capabilities. Safest; a step that quietly relied on root powers will fail with "Operation not permitted", and the fix is one line in that step.' },
];

export async function stepCapabilitiesPage() {
  const el = h('div', {});
  async function load() {
    let v;
    try { v = await get('/v1/settings/step-capabilities'); } catch (e) { return mount(el, pageHeader('Step container limits', ''), errorBox(e.message)); }
    const out = h('div', { id: 'caps-result' });
    const rows = MODES.map((m) => {
      const input = h('input', { type: 'radio', name: 'caps-mode', id: 'caps-' + m.id, value: m.id, class: 'mt-1', checked: v.mode === m.id });
      input.addEventListener('change', async () => {
        try { await put('/v1/settings/step-capabilities', { mode: m.id }); toast('Saved', 'ok'); load(); }
        catch (e) { mount(out, errorBox(e.message)); }
      });
      return h('label', { class: 'flex items-start gap-2.5 text-sm', for: 'caps-' + m.id }, input,
        h('span', {}, h('span', { class: 'block font-medium' }, m.title), h('span', { class: 'muted block text-xs' }, m.text)));
    });
    mount(el,
      pageHeader('Step container limits', 'Pipeline steps run in containers. Every one drops all Linux capabilities; this chooses what comes back.'),
      h('section', { class: 'card card-pad max-w-3xl space-y-4' },
        ...rows, out,
        h('div', { class: 'text-sm' }, h('p', { class: 'font-medium' }, 'Every step gets now'),
          h('p', { class: 'muted font-mono text-xs', id: 'caps-baseline' }, v.baseline.length ? v.baseline.join(', ') : 'nothing')),
        h('div', { class: 'text-sm' }, h('p', { class: 'font-medium' }, 'A step can ask for (permissions.capabilities)'),
          h('p', { class: 'muted font-mono text-xs' }, v.grantable.join(', '))),
        notice('info', 'Applies to the next run. Agent and assistant sandboxes always drop everything. Each time a step asks for more than the setting gives, the audit log records it (run.capability.grant). If a step fails with "Operation not permitted", add the capability it needs to that step.'),
        v.updated_by ? h('p', { class: 'muted text-xs' }, 'Changed by ' + v.updated_by) : null));
  }
  mount(el, spinner());
  await load();
  return el;
}
