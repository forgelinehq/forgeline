import { h, mount, icon, errorBox, field } from '../lib/dom.js';
import { post } from '../lib/api.js';

function frame(title, subtitle, body) {
  return h('div', { class: 'flex min-h-screen items-center justify-center px-4 py-10' },
    h('div', { class: 'w-full max-w-md' },
      h('div', { class: 'mb-6 flex flex-col items-center text-center' },
        h('img', { src: '/ui/favicon.svg', alt: '', class: 'mb-3 size-12' }),
        h('h1', { class: 'text-2xl font-semibold tracking-tight' }, title), h('p', { class: 'muted mt-1 text-sm' }, subtitle)),
      h('div', { class: 'card card-pad' }, body),
      h('p', { class: 'muted mt-6 text-center text-xs' }, 'Forgeline · delivery where AI agents run safely')));
}

function form(fields, submitLabel, onSubmit) {
  const err = h('div', { class: 'hidden', 'aria-live': 'polite' });
  const btn = h('button', { class: 'btn-primary w-full', type: 'submit' }, submitLabel);
  const f = h('form', { class: 'space-y-4', novalidate: true, onsubmit: async (e) => {
    e.preventDefault();
    err.classList.add('hidden'); btn.disabled = true;
    try { await onSubmit(); } catch (ex) {
      mount(err, errorBox(ex.message)); err.classList.remove('hidden'); btn.disabled = false;
    }
  } }, fields, err, btn);
  return f;
}

// What a failed single sign-on means to the person (the server audits the full reason; design note 031).
const SSO_ERRORS = {
  off: 'Single sign-on is not set up on this server.',
  provider: 'The company login could not be reached, or it said no. Try again, or ask an administrator.',
  state: 'That sign-in expired or was started in another browser. Try again.',
  token: 'The company login sent back something Forgeline could not verify. Ask an administrator to check the single sign-on settings.',
  refused: 'You signed in at your company, but this server does not let you in. Ask an administrator for access.',
};

export function loginPage(root, done, sso) {
  const user = h('input', { id: 'username', class: 'input', autocomplete: 'username', autofocus: true, required: true });
  const pass = h('input', { id: 'password', class: 'input', type: 'password', autocomplete: 'current-password', required: true });
  const m = /^#sso-error=([a-z]+)$/.exec(location.hash);
  if (m) history.replaceState(null, '', location.pathname); // the code is shown once; a reload starts clean
  const ssoBox = sso && sso.enabled ? h('div', { class: 'space-y-4' },
    h('a', { href: '/v1/auth/oidc/start', id: 'sso-start', class: 'btn-primary w-full' }, icon('lock', 'size-4'), 'Sign in with ' + sso.name),
    h('div', { class: 'flex items-center gap-3 text-xs text-slate-400' }, h('span', { class: 'h-px flex-1 bg-slate-200 dark:bg-slate-700' }), 'or with a Forgeline account', h('span', { class: 'h-px flex-1 bg-slate-200 dark:bg-slate-700' }))) : null;
  mount(root, frame('Sign in to Forgeline', sso && sso.enabled ? 'Use your company login, or a Forgeline account.' : 'Use your account to continue.', h('div', { class: 'space-y-4' },
    m ? errorBox(SSO_ERRORS[m[1]] || 'Single sign-on did not work. Try again.') : null,
    ssoBox,
    form([field('Username', user), field('Password', pass)], 'Sign in', async () => {
      await post('/v1/auth/login', { username: user.value, password: pass.value });
      done();
    }))));
  if (!(sso && sso.enabled)) user.focus();
}

export function setupPage(root, done) {
  // `forgeline server` prints a link ending in #token=...; use it, then drop it from the address bar.
  const fromLink = /^#token=([A-Za-z0-9_-]+)$/.exec(location.hash);
  if (fromLink) history.replaceState(null, '', location.pathname + location.search);
  const token = h('input', { id: 'token', class: 'input font-mono', type: 'password', autocomplete: 'off', required: true, value: fromLink ? fromLink[1] : '' });
  const user = h('input', { id: 'username', class: 'input', autocomplete: 'username', required: true, value: 'admin' });
  const name = h('input', { id: 'name', class: 'input', autocomplete: 'name' });
  const pass = h('input', { id: 'password', class: 'input', type: 'password', autocomplete: 'new-password', required: true });
  const pass2 = h('input', { id: 'password2', class: 'input', type: 'password', autocomplete: 'new-password', required: true });
  mount(root, frame('Welcome to Forgeline', 'Create the first administrator to finish setting up this server.', h('div', { class: 'space-y-4' },
    h('div', { class: 'flex items-start gap-2 rounded-lg bg-brand-50 px-3 py-2.5 text-sm text-brand-900 dark:bg-brand-500/10 dark:text-brand-100' },
      icon('shield', 'mt-0.5 size-4'), h('div', {}, 'To prove you control this server, paste its token. It is the value of ', h('code', { class: 'font-mono text-xs' }, 'FORGELINE_TOKEN'), ', or the one printed when the server started.')),
    form([
      field('Server token', token), field('Username', user), field('Your name (optional)', name),
      field('Password', pass, 'At least 12 characters.'), field('Repeat the password', pass2),
    ], 'Create administrator', async () => {
      if (pass.value !== pass2.value) throw new Error('The two passwords are different.');
      await post('/v1/auth/setup', { token: token.value.trim(), username: user.value, name: name.value, password: pass.value });
      done();
    }))));
  (fromLink ? user : token).focus();
}
