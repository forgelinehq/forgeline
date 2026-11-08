// Settings › Sign-in (SSO): single sign-on with OpenID Connect (design note 031). Administrators only.
// The client secret is write-only: the page says whether one is stored, never what it is.
import { h, icon, mount, pageHeader, spinner, errorBox, notice, toast, field } from '../lib/dom.js';
import { get, put, post } from '../lib/api.js';

const ROLES = [['viewer', 'Viewer'], ['developer', 'Developer'], ['admin', 'Administrator']];

export async function ssoPage() {
  const el = h('div', {});
  mount(el, pageHeader('Sign-in (SSO)', 'Let people sign in with your company login (Google Workspace, Microsoft Entra ID, Okta, Keycloak or any OpenID Connect provider).'), spinner());
  let v;
  try { v = await get('/v1/settings/sso'); } catch (e) { mount(el, errorBox(e.message)); return el; }

  const input = (id, value, attrs = {}) => h('input', { id, class: 'input', value: value || '', autocomplete: 'off', ...attrs });
  const enabled = h('input', { id: 'sso-enabled', type: 'checkbox', class: 'size-4 accent-brand-600', checked: v.enabled });
  const name = input('sso-name', v.name, { placeholder: 'Acme login', maxlength: '40' });
  const issuer = input('sso-issuer', v.issuer, { placeholder: 'https://accounts.google.com', type: 'url' });
  const clientId = input('sso-client', v.client_id);
  const secret = input('sso-secret', '', { type: 'password', placeholder: v.has_secret ? 'Stored. Leave empty to keep it' : 'From your provider' });
  const address = input('sso-address', v.address || location.origin, { type: 'url' });
  const claim = input('sso-claim', v.groups_claim || 'groups');
  const allowNew = h('input', { id: 'sso-allow-new', type: 'checkbox', class: 'size-4 accent-brand-600', checked: v.allow_new });
  const defRole = h('select', { id: 'sso-default-role', class: 'input' }, [['', 'Nobody: refuse them'], ...ROLES].map(([k, l]) => h('option', { value: k, selected: (v.default_role || '') === k }, l)));
  const domains = input('sso-domains', (v.domains || []).join(', '), { placeholder: 'acme.com, acme.co.uk' });
  const redirect = h('code', { class: 'break-all rounded bg-slate-100 px-2 py-1 font-mono text-xs dark:bg-slate-800', id: 'sso-redirect' });
  const showRedirect = () => { redirect.textContent = address.value.replace(/\/+$/, '') + '/v1/auth/oidc/callback'; };
  address.addEventListener('input', showRedirect); showRedirect();

  // Group → role rows.
  const rows = h('div', { class: 'space-y-2', id: 'sso-mappings' });
  const addRow = (m = { group: '', role: 'developer' }) => {
    const g = h('input', { class: 'input', value: m.group, placeholder: 'platform-admins', 'data-map-group': '' });
    const r = h('select', { class: 'input w-44', 'data-map-role': '' }, ROLES.map(([k, l]) => h('option', { value: k, selected: m.role === k }, l)));
    const row = h('div', { class: 'flex items-center gap-2' }, g, r, h('button', { type: 'button', class: 'btn-ghost btn-sm', 'aria-label': 'Remove this mapping', onclick: () => row.remove() }, icon('trash', 'size-4')));
    rows.append(row);
  };
  (v.mappings || []).forEach(addRow);
  if (!(v.mappings || []).length) addRow({ group: '', role: 'developer' });

  const testOut = h('div', { class: 'text-sm', 'aria-live': 'polite', id: 'sso-test' });
  const testBtn = h('button', { type: 'button', class: 'btn-secondary', id: 'sso-test-run', onclick: async () => {
    testBtn.disabled = true; mount(testOut, spinner());
    try {
      const r = await post('/v1/settings/sso/test', { issuer: issuer.value });
      mount(testOut, r.ok ? notice('ok', 'Found the provider. Sign-in goes to ', h('code', { class: 'text-xs' }, r.authorization_endpoint), r.pkce ? '. It supports PKCE.' : '. It does not say it supports PKCE (S256), which Forgeline uses; sign-in may fail.')
        : errorBox('Could not read the provider at that address: ' + r.error));
    } catch (e) { mount(testOut, errorBox(e.message)); }
    testBtn.disabled = false;
  } }, icon('bolt', 'size-4'), 'Test');

  const err = h('div', { class: 'hidden' });
  const save = h('button', { type: 'button', class: 'btn-primary', id: 'sso-save', onclick: async () => {
    err.classList.add('hidden'); save.disabled = true;
    const mappings = [...rows.children].map(row => ({ group: row.querySelector('[data-map-group]').value.trim(), role: row.querySelector('[data-map-role]').value })).filter(m => m.group);
    try {
      v = await put('/v1/settings/sso', { enabled: enabled.checked, name: name.value, issuer: issuer.value, client_id: clientId.value, client_secret: secret.value,
        address: address.value, groups_claim: claim.value, mappings, allow_new: allowNew.checked, default_role: defRole.value,
        domains: domains.value.split(',').map(d => d.trim()).filter(Boolean) });
      secret.value = ''; secret.placeholder = v.has_secret ? 'Stored. Leave empty to keep it' : 'From your provider';
      toast(v.ready ? 'Saved. Single sign-on is on.' : 'Saved.', 'ok'); drawStatus();
    } catch (e) { mount(err, errorBox(e.message)); err.classList.remove('hidden'); }
    save.disabled = false;
  } }, 'Save');

  const status = h('div', { id: 'sso-status' });
  const drawStatus = () => mount(status, v.ready
    ? notice('ok', 'Single sign-on is on. The sign-in page shows “Sign in with ' + (v.name || 'your company login') + '”. Password sign-in still works for Forgeline accounts.')
    : notice('info', v.enabled ? 'Saved as on, but not complete: fill in the issuer, client ID, client secret and address.' : 'Single sign-on is off. People sign in with Forgeline accounts.'));
  drawStatus();

  const section = (title, text, ...kids) => h('section', { class: 'card card-pad space-y-4' }, h('div', {}, h('h2', { class: 'font-display text-lg font-bold' }, title), text ? h('p', { class: 'muted text-sm' }, text) : null), ...kids);
  mount(el, pageHeader('Sign-in (SSO)', 'Let people sign in with your company login (Google Workspace, Microsoft Entra ID, Okta, Keycloak or any OpenID Connect provider).'),
    h('div', { class: 'space-y-5' }, status,
      section('1. Register Forgeline with your provider', 'Create an app (a "web application" or "OIDC client") at your provider, with this redirect address. Copy its client ID and secret.',
        h('div', { class: 'flex flex-wrap items-center gap-2' }, redirect, h('button', { type: 'button', class: 'btn-secondary btn-sm', onclick: async () => {
          try { await navigator.clipboard.writeText(redirect.textContent); toast('Copied', 'ok'); } catch { toast('Copy is blocked in this browser; select the text instead', 'error'); }
        } }, icon('copy', 'size-3.5'), 'Copy')),
        field('The address people reach Forgeline at', address, 'https, or http only on this machine. The redirect address above is built from it.', 'sso-address')),
      section('2. Connect to the provider', null,
        h('div', { class: 'grid gap-4 sm:grid-cols-2' },
          h('div', { class: 'sm:col-span-2' }, field('Issuer', h('div', { class: 'flex gap-2' }, issuer, testBtn), 'The provider\'s issuer URL, for example https://accounts.google.com or https://login.microsoftonline.com/<tenant>/v2.0.', 'sso-issuer')),
          field('Client ID', clientId, null, 'sso-client'), field('Client secret', secret, 'Stored encrypted and never shown again.', 'sso-secret'),
          field('Button text', name, 'Shown as “Sign in with …”.', 'sso-name')),
        testOut),
      section('3. Who may come in, and with which role', 'Forgeline knows a person by the provider\'s own id for them, never by email alone. Roles are applied at every sign-in, so a group change takes effect next time. The last administrator is never demoted.',
        h('label', { class: 'flex items-start gap-2 text-sm' }, allowNew, h('span', {}, h('span', { class: 'font-medium' }, 'Let new people in'), h('span', { class: 'muted block text-xs' }, 'Make an account at a person\'s first sign-in. Off: only people who already have an SSO account here.'))),
        h('div', {}, h('div', { class: 'label' }, 'Roles from groups'), h('p', { class: 'hint mb-2' }, 'The highest matching role wins.'), rows,
          h('button', { type: 'button', class: 'btn-ghost btn-sm mt-2', onclick: () => addRow() }, icon('plus', 'size-3.5'), 'Add a group')),
        h('div', { class: 'grid gap-4 sm:grid-cols-2' },
          field('When no group matches', defRole, null, 'sso-default-role'),
          field('Groups claim', claim, 'The token field that lists groups. Usually "groups".', 'sso-claim'),
          h('div', { class: 'sm:col-span-2' }, field('Allowed email domains', domains, 'Optional, comma separated. When set, the email must be verified and in one of these.', 'sso-domains')))),
      section('4. Turn it on', null,
        h('label', { class: 'flex items-center gap-2 text-sm font-medium' }, enabled, 'Single sign-on is on'),
        err, h('div', { class: 'flex justify-end' }, save))));
  return el;
}
