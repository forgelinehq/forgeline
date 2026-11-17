import { h, icon, mount, clear, pageHeader, emptyState, spinner, errorBox, notice, toast, modal, confirmDialog } from '../lib/dom.js';
import { get, post, put, del, atLeast } from '../lib/api.js';
import { session } from '../app.js';

// Team › Library (design note 040, step 8; it was the marketplace): packages of skills, agents, workflows, environment
// templates, plugins, guardrail packs and MCP servers, and the reviewed OpenTofu templates the canvas offers. Each is shown with what it can do before anyone
// installs it, and the install is bound to the fingerprint that was shown.

const tierText = (t) => t === 'T1' ? 'T1 · may file change proposals (a person still approves)' : 'T0 · reads and advises only';

export async function marketplacePage() {
  const admin = atLeast(session.role, 'admin');
  const el = h('div', {});
  let packages = [], git = { configured: false }, tag = '', kind = '', q = '', plugins = { enabled: false, plugins: [] }, secretNames = [], templates = [];

  async function load() {
    try {
      [{ packages, git }, plugins, secretNames] = await Promise.all([get('/v1/market/packages').then(r => ({ packages: r.packages, git: r.git || { configured: false } })), get('/v1/plugins'), get('/v1/secrets').then(r => r.secrets.filter(x => x.source === 'vault').map(x => x.name)).catch(() => [])]);
      templates = (await get('/v1/templates').catch(() => ({ templates: [] }))).templates || [];
    } catch (e) { return mount(el, errorBox(e.message)); }
    draw();
  }

  function draw() {
    const tags = [...new Set(packages.flatMap(p => p.tags || []))].sort();
    const has = { agents: p => (p.agents || []).length > 0, workflows: p => (p.workflows || []).length > 0, tools: p => (p.plugins || []).length + (p.mcp_servers || []).length > 0,
      guardrails: p => (p.policy_packs || []).length > 0, environments: p => (p.env_templates || []).length > 0 };
    const words = (x) => !q || x.toLowerCase().includes(q.toLowerCase());
    const shown = kind === 'templates' ? [] : packages.filter(p => (!tag || (p.tags || []).includes(tag)) && (!kind || has[kind](p)) && words(p.title + ' ' + p.description));
    const shownTemplates = (!kind || kind === 'templates') && !tag ? templates.filter(t => words(t.title + ' ' + t.summary)) : [];
    const search = h('input', { class: 'input w-56', id: 'market-search', type: 'search', placeholder: 'Search packages', 'aria-label': 'Search packages', value: q,
      oninput: (e) => { q = e.target.value; const pos = e.target.selectionStart; draw(); const n = document.querySelector('#market-search'); n.focus(); n.setSelectionRange(pos, pos); } });
    mount(el, pageHeader('Library', 'Ready-made agents, skills, workflows, tools, guardrail packs and templates. You see what each one can do before it is installed, and an install is pinned to the fingerprint you reviewed.', search,
        admin && h('label', { class: 'flex items-center gap-2 text-sm', title: 'Plugins run third-party tools in locked-down containers. Needs Docker.' }, h('input', { type: 'checkbox', id: 'plugins-switch', checked: plugins.enabled,
          onchange: async (e) => { try { await put('/v1/plugins/settings', { enabled: e.target.checked }); toast(e.target.checked ? 'Plugins are on' : 'Plugins are off', 'ok'); } catch (err) { e.target.checked = !e.target.checked; toast(err.message, 'error'); } load(); } }), 'Plugins')),
      git.configured && gitStatus(),
      h('div', { class: 'mb-3 flex flex-wrap gap-2', id: 'library-kinds' }, [['', 'Everything'], ['agents', 'Agents and skills'], ['workflows', 'Workflows'], ['tools', 'Tools'], ['guardrails', 'Guardrail packs'], ['environments', 'Environments'], ['templates', 'Templates']].map(([v, l]) => h('button', { type: 'button', 'data-kind': v, 'aria-pressed': String(kind === v),
        class: 'rounded-lg border px-3 py-1 text-sm font-medium ' + (kind === v ? 'border-brand-500 bg-brand-50 text-brand-700 dark:bg-brand-950/40 dark:text-brand-200' : 'border-slate-200 hover:border-brand-400 dark:border-slate-800'),
        onclick: () => { kind = v; draw(); } }, l))),
      h('div', { class: 'mb-4 flex flex-wrap gap-2' }, [['', 'All'], ...tags.map(t => [t, t])].map(([v, l]) => h('button', { type: 'button', 'data-tag': v, 'aria-pressed': String(tag === v),
        class: 'rounded-full border px-3 py-1 text-sm ' + (tag === v ? 'border-brand-500 bg-brand-50 text-brand-700 dark:bg-brand-950/40 dark:text-brand-200' : 'border-slate-200 muted hover:border-brand-400 dark:border-slate-800'),
        onclick: () => { tag = v; draw(); } }, l))),
      shown.length || shownTemplates.length ? h('div', { class: 'grid gap-4 sm:grid-cols-2 xl:grid-cols-3', id: 'market-grid' }, shown.map(card), shownTemplates.map(templateCard)) : emptyState('plug', 'Nothing matches', 'Try another word or clear the tag.'));
  }

  // A reviewed OpenTofu template: built into Forgeline, offered on the canvas as one block. Nothing to install.
  function templateCard(t) {
    return h('div', { class: 'card flex flex-col p-4', 'data-template': t.ref },
      h('div', { class: 'flex items-start justify-between gap-2' }, h('h3', { class: 'font-semibold' }, t.title), h('span', { class: 'pill-idle' }, 'template')),
      h('p', { class: 'muted mt-1 flex-1 text-sm' }, t.summary),
      h('div', { class: 'muted mt-3 text-xs' }, `OpenTofu template · v${t.version} · built in, reviewed with Forgeline's code · on the canvas in Build › Design`),
      h('div', { class: 'mt-3 flex justify-end' }, h('a', { class: 'btn-secondary btn-sm', href: '/v1/templates/' + encodeURIComponent(t.name) + '/' + encodeURIComponent(t.version) + '/code', target: '_blank', rel: 'noopener' }, 'Read its code')));
  }

  // The signed git source the operator configured: what it reads, which keys it trusts, and when it last fetched.
  function gitStatus() {
    const refresh = admin && h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'market-refresh', onclick: async (e) => {
      e.target.disabled = true;
      try { await post('/v1/market/refresh', {}); toast('Fetched', 'ok'); } catch (err) { toast(err.message, 'error'); }
      load();
    } }, icon('refresh', 'size-3.5'), 'Fetch now');
    return h('div', { class: 'mb-4' }, notice(git.error ? 'warn' : 'info', h('div', { class: 'flex flex-wrap items-center justify-between gap-2', id: 'git-status' },
      h('div', {}, h('strong', {}, 'Signed packages from git. '), h('code', { class: 'text-xs' }, git.url), ` (${git.ref})`, git.commit ? ` · commit ${git.commit}` : '', git.fetched_at ? ' · fetched ' + new Date(git.fetched_at).toLocaleString() : '',
        h('div', { class: 'text-xs' }, 'Trusted keys: ' + ((git.keys || []).join(', ') || 'none') + '. A package is installable only if one of them signed it.'),
        git.error ? h('div', { class: 'text-xs' }, 'Problem: ' + git.error) : null), refresh)));
  }

  function card(p) {
    const inst = p.installed;
    return h('div', { class: 'card flex flex-col p-4', 'data-package': p.id },
      h('div', { class: 'flex items-start justify-between gap-2' }, h('h3', { class: 'font-semibold' }, p.title),
        p.problem ? h('span', { class: 'pill-bad' }, 'damaged') : p.source === 'git' && !inst && !p.update ? h('span', { class: 'pill-idle', title: 'Signed by ' + p.signer }, 'signed') : p.update ? h('span', { class: 'pill-wait' }, 'update') : inst ? h('span', { class: 'pill-ok' }, 'installed') : null),
      h('p', { class: 'muted mt-1 flex-1 text-sm' }, p.description),
      h('div', { class: 'muted mt-3 text-xs' }, `${(p.agents || []).length} agent${(p.agents || []).length === 1 ? '' : 's'} · ${(p.skills || []).length} skill${(p.skills || []).length === 1 ? '' : 's'}${(p.plugins || []).length ? ` · ${p.plugins.length} plugin${p.plugins.length === 1 ? '' : 's'}` : ''}${(p.workflows || []).length ? ` · ${p.workflows.length} workflow${p.workflows.length === 1 ? '' : 's'}` : ''}${(p.env_templates || []).length ? ` · ${p.env_templates.length} environment template${p.env_templates.length === 1 ? '' : 's'}` : ''}${(p.policy_packs || []).length ? ` · ${p.policy_packs.length} guardrail pack${p.policy_packs.length === 1 ? '' : 's'}` : ''}${(p.mcp_servers || []).length ? ` · ${p.mcp_servers.length} MCP server${p.mcp_servers.length === 1 ? '' : 's'}` : ''} · v${p.version} · ${p.publisher}${p.reviewed_by ? ' · reviewed by ' + p.reviewed_by : ''}${p.source === 'git' ? ' · git' : ''}`),
      h('div', { class: 'mt-3 flex justify-end' }, h('button', { class: 'btn-secondary btn-sm', type: 'button', 'data-open': p.id, onclick: () => detail(p) }, 'Review')));
  }

  function detail(p) {
    modal(p.title, (close) => {
      const inst = p.installed;
      const tier = h('select', { id: 'install-tier', class: 'input w-auto', disabled: !!inst }, h('option', { value: 'T0' }, 'T0 · advise only'), p.writes ? h('option', { value: 'T1' }, 'T1 · may file proposals') : null);
      const err = h('div', { class: 'hidden' });
      const grants = {};
      const pluginBlock = (pl) => {
        const g = grants[pl.key] = { network: false, hosts: [], secrets: {} };
        const prev = inst?.plugins?.[pl.key];
        const changed = !!prev && (prev.image !== pl.image || prev.wants_network !== !!pl.network || JSON.stringify(prev.wants_hosts || []) !== JSON.stringify([...(pl.hosts || [])].sort()) || JSON.stringify(prev.wants_secrets || []) !== JSON.stringify([...(pl.secrets || [])].sort()));
        const old = changed ? null : prev;
        return h('div', { class: 'mb-3 rounded-lg border border-slate-200 p-3 dark:border-slate-800', 'data-plugin': pl.key },
          h('div', { class: 'font-medium' }, pl.name), h('div', { class: 'muted text-sm' }, pl.description),
          h('div', { class: 'mt-2 break-all text-xs' }, h('span', { class: 'muted' }, 'Image: '), h('code', {}, pl.image)),
          h('div', { class: 'mt-1 text-xs' }, h('span', { class: 'muted' }, 'Tools: '), pl.tools.map(t => h('code', { class: 'mr-1.5', title: t.description }, t.name))),
          h('div', { class: 'mt-1 text-xs muted' }, 'Runs in a container for each call: read-only, no extra privileges, no network unless you allow it. What it returns is treated as data.'),
          old && admin ? h('div', { class: 'mt-2 text-xs muted' }, `Approved earlier: network ${old.network ? 'allowed' : (old.hosts || []).length ? 'only ' + old.hosts.join(', ') : 'off'}${Object.keys(old.secrets || {}).length ? ', secrets ' + Object.keys(old.secrets).join(', ') : ''}. An update keeps this unless the plugin asks for something different.`) : null,
          changed ? notice('warn', 'This update asks for something different from what you approved (image, network, hosts or secrets). Grant it again below.') : null,
          admin && !old ? h('div', { class: 'mt-2 space-y-2 text-sm' },
            pl.network ? h('label', { class: 'flex items-center gap-2' }, h('input', { type: 'checkbox', 'data-grant-network': pl.key, onchange: (e) => { g.network = e.target.checked; } }), 'Allow it to use the whole network (it asks for this)') : null,
            (pl.hosts || []).length ? h('div', { class: 'space-y-1' },
              h('div', { class: 'text-xs muted' }, 'It asks to reach only these hosts. Each goes through Forgeline\'s proxy, which allows only the names you tick, only to public addresses; the plugin has no other route out and no DNS of its own.'),
              pl.hosts.map(host => h('label', { class: 'flex items-center gap-2' }, h('input', { type: 'checkbox', 'data-grant-host': pl.key + ':' + host, onchange: (e) => { g.hosts = e.target.checked ? [...g.hosts, host] : g.hosts.filter(x => x !== host); } }), h('code', { class: 'text-xs' }, host)))) : null,
            !pl.network && !(pl.hosts || []).length ? h('div', { class: 'muted text-xs' }, 'It does not ask for network access.') : null,
            (pl.secrets || []).map(env => h('label', { class: 'flex flex-wrap items-center gap-2' }, h('code', { class: 'text-xs' }, env), h('span', { class: 'muted' }, 'give it'),
              h('select', { class: 'input w-auto', 'data-grant-secret': env, onchange: (e) => { if (e.target.value) g.secrets[env] = e.target.value; else delete g.secrets[env]; } },
                h('option', { value: '' }, 'nothing'), secretNames.map(n => h('option', { value: n }, n)))))) : null);
      };
      const act = async (fn, done) => {
        err.classList.add('hidden');
        try { await fn(); toast(done, 'ok'); close(); load(); } catch (e) { mount(err, errorBox(e.message)); err.classList.remove('hidden'); }
      };
      return h('div', { class: 'space-y-4', id: 'package-detail' },
        h('p', { class: 'text-sm' }, p.description),
        h('dl', { class: 'grid grid-cols-[auto_1fr] gap-x-6 gap-y-1.5 text-sm' }, h('dt', { class: 'muted' }, 'Publisher'), h('dd', {}, p.publisher), h('dt', { class: 'muted' }, 'Reviewed by'), h('dd', { id: 'package-reviewer' }, p.reviewed_by || 'nobody named'), h('dt', { class: 'muted' }, 'Version'), h('dd', {}, 'v' + p.version + (inst ? ` (installed: v${inst.version})` : '')),
          h('dt', { class: 'muted' }, 'Source'), h('dd', { id: 'package-source' }, p.source === 'git' ? (p.signer ? `git, signed by ${p.signer}` : 'git, not verified') : p.source),
          h('dt', { class: 'muted' }, 'Fingerprint'), h('dd', { class: 'break-all font-mono text-xs', id: 'package-hash' }, p.sha256)),
        p.problem ? notice('error', p.problem) : null,
        admin && p.source === 'git' && /rollback|first one seen/.test(p.problem || '') ? h('div', { class: 'flex items-center justify-between gap-2 text-sm' },
          h('span', { class: 'muted' }, 'If the newer version was a mistake, or its key has been replaced, make this server forget what it has seen.'),
          h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'forget-seen', onclick: async () => {
            if (await confirmDialog({ title: 'Forget the versions seen of ' + p.id.replace(/ \(.*$/, ''), body: 'Older signed versions of this package will be accepted again.', confirmLabel: 'Forget' }))
              act(() => del('/v1/market/seen/' + encodeURIComponent(p.id.replace(/ \(.*$/, ''))), 'Forgotten');
          } }, 'Forget versions seen')) : null,
        (p.agents || []).length ? h('div', {}, h('div', { class: 'h-section mb-2' }, 'What it can do'),
          (p.agents || []).map(a => h('div', { class: 'mb-3 rounded-lg border border-slate-200 p-3 dark:border-slate-800', 'data-agent': a.key },
            h('div', { class: 'font-medium' }, a.name), h('div', { class: 'muted text-sm' }, a.description),
            h('div', { class: 'mt-2 text-xs' }, h('span', { class: 'muted' }, 'Allowed to read: '), a.tools.map(t => h('code', { class: 'mr-1.5' }, t))),
            h('div', { class: 'mt-1 text-xs muted' }, tierText(a.tier))))) : null,
        (p.workflows || []).length ? h('div', {}, h('div', { class: 'h-section mb-2' }, 'Workflows it adds'), (p.workflows || []).map(w => h('div', { class: 'mb-3 rounded-lg border border-slate-200 p-3 dark:border-slate-800', 'data-workflow': w.key },
          h('div', { class: 'font-medium' }, w.name), h('div', { class: 'muted text-sm' }, w.description),
          h('ol', { class: 'mt-2 list-decimal space-y-1 pl-5 text-xs' }, w.stages.map(st => h('li', {}, h('strong', {}, st.name || 'Stage'), st.gate ? ' (waits for an administrator)' : '', ': ', st.steps.map(x => x.agent + ' → ' + x.task).join(' · ')))),
          h('div', { class: 'mt-2 text-xs muted' }, 'Never scheduled. Runs only when someone starts it, as ' + (w.run_as === 'developer' ? 'a developer at T1 and a viewer at T0' : 'a viewer') + '. Whatever its agents propose still needs a different administrator.')))) : null,
        (p.env_templates || []).length ? h('div', {}, h('div', { class: 'h-section mb-2' }, 'Environment templates it adds'), (p.env_templates || []).map(t => h('div', { class: 'mb-3 rounded-lg border border-slate-200 p-3 dark:border-slate-800', 'data-template': t.key },
          h('div', { class: 'font-medium' }, t.title), h('div', { class: 'muted text-sm' }, t.description),
          h('div', { class: 'mt-2 text-xs' }, h('span', { class: 'muted' }, 'Publisher\'s estimate: '), `$${t.monthly_usd} a month · default ${t.default_ttl_hours} h · stack ${t.default_stack}`),
          h('div', { class: 'mt-1 text-xs' }, h('span', { class: 'muted' }, 'Asks for: '), t.parameters.filter(x => !['Owner', 'ExpiresAt'].includes(x.name)).map(x => h('code', { class: 'mr-1.5' }, x.name))),
          h('details', { class: 'mt-2' }, h('summary', { class: 'cursor-pointer text-xs' }, 'CloudFormation body'), h('pre', { class: 'mt-1 max-h-64 overflow-auto whitespace-pre-wrap rounded-lg border border-slate-200 p-3 text-xs dark:border-slate-800' }, t.body)),
          h('div', { class: 'mt-2 text-xs muted' }, 'Always needs an administrator\'s approval, never self-service. No IAM, nested stacks or macros are allowed in it.'))),
          notice('warn', 'The cost is the publisher\'s own estimate, not Forgeline\'s. Read the body before installing.')) : null,
        (p.plugins || []).length ? h('div', {}, h('div', { class: 'h-section mb-2' }, 'Plugins it adds'), (p.plugins || []).map(pluginBlock), plugins.enabled ? null : notice('warn', 'Plugins are turned off. An administrator turns them on at the top of this page; it needs Docker.')) : null,
        (p.policy_packs || []).length ? h('div', {}, h('div', { class: 'h-section mb-2' }, 'Guardrail packs it adds'), p.policy_packs.map(pk => h('div', { class: 'mb-3 rounded-lg border border-slate-200 p-3 dark:border-slate-800', 'data-pack': pk.id },
          h('div', { class: 'font-medium' }, pk.name), h('div', { class: 'muted text-xs' }, ({ trivy: 'Trivy', checkov: 'Checkov', conftest: 'Conftest (Rego)' })[pk.tool] + (pk.deny_at ? ' · denies from ' + pk.deny_at : '') + ((pk.deny || []).length ? ' · always denies ' + pk.deny.join(', ') : '') + ((pk.skip || []).length ? ' · skips ' + pk.skip.join(', ') : '')),
          Object.entries(pk.rules || {}).map(([n, body]) => h('details', { class: 'mt-2' }, h('summary', { class: 'cursor-pointer text-xs' }, n), h('pre', { class: 'mt-1 max-h-64 overflow-auto whitespace-pre-wrap rounded-lg border border-slate-200 p-3 text-xs dark:border-slate-800' }, body))))),
          notice('info', 'Installed turned off. Turn each on under Settings › Policy packs after reading what it would deny.')) : null,
        (p.mcp_servers || []).length ? h('div', {}, h('div', { class: 'h-section mb-2' }, 'Tools it adds (MCP servers)'), p.mcp_servers.map(ms => h('div', { class: 'mb-3 rounded-lg border border-slate-200 p-3 dark:border-slate-800', 'data-mcp': ms.key },
          h('div', { class: 'font-medium' }, ms.name), h('div', { class: 'muted text-sm' }, ms.description),
          h('div', { class: 'mt-2 break-all text-xs' }, h('span', { class: 'muted' }, ms.kind === 'remote' ? 'Address: ' : 'Image: '), h('code', {}, ms.url || ms.image)),
          ms.token ? h('div', { class: 'mt-1 text-xs' }, h('span', { class: 'muted' }, 'Needs: '), ms.token) : h('div', { class: 'mt-1 text-xs muted' }, 'Needs no token.'),
          ms.docs ? h('div', { class: 'mt-1 text-xs' }, h('a', { href: ms.docs, target: '_blank', rel: 'noopener noreferrer', class: 'underline' }, 'What its tools do')) : null)),
          notice('info', 'Installed with no token and none of its tools turned on. Under Settings › MCP servers, add the token, list its tools and turn on the ones you want; tools that change things need you to say so for each.')) : null,
        (p.skills || []).length ? h('div', {}, h('div', { class: 'h-section mb-2' }, 'Skills it adds'), p.skills.map(s => h('details', { class: 'mb-2' }, h('summary', { class: 'cursor-pointer text-sm font-medium' }, s.name),
          h('pre', { class: 'mt-1 whitespace-pre-wrap rounded-lg border border-slate-200 p-3 text-xs dark:border-slate-800' }, s.body)))) : null,
        notice('info', 'Nothing here can change your cloud by itself. At most it can propose a change, and a different administrator approves it.'),
        admin && !p.problem ? h('div', { class: 'space-y-2' }, err,
          h('div', { class: 'flex flex-wrap items-center justify-between gap-2' },
            inst ? h('button', { class: 'btn-ghost btn-sm', type: 'button', id: 'uninstall', onclick: async () => {
              if (await confirmDialog({ title: 'Uninstall ' + p.title, body: 'Its agents and workflows are turned off, and the guardrail packs and MCP servers it added are removed. The skills and agents stay, with their history, so you can delete them yourself.', confirmLabel: 'Uninstall' }))
                act(() => del('/v1/market/install/' + p.id), 'Uninstalled');
            } }, 'Uninstall') : h('label', { class: 'flex items-center gap-2 text-sm' }, h('span', { class: 'muted' }, 'Allow up to'), tier),
            !inst || p.update ? h('button', { class: 'btn-primary', type: 'button', id: 'install', onclick: () => act(() => post('/v1/market/install', { id: p.id, hash: p.sha256, tier: tier.value, grants }), inst ? 'Updated' : 'Installed') }, inst ? 'Update to v' + p.version : 'Install') : null),
          inst ? h('p', { class: 'muted text-xs' }, `Installed by ${inst.by}. Tier stays at ${inst.tier}; an update cannot raise it.`) : null) : null);
    }, { wide: true });
  }

  mount(el, spinner());
  await load();
  return el;
}
