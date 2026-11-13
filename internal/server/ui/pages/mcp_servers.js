// Settings › MCP servers (design note 042): remote servers over HTTPS and local ones as containers. Tools are discovered,
// start off, and are turned on one by one; only read-only tools can be. A tool whose definition changes after it was
// turned on is off again, with the reason shown.
import { h, icon, mount, pageHeader, spinner, errorBox, notice, toast, modal, field, emptyState, relTime, confirmDialog } from '../lib/dom.js';
import { get, post, put, del } from '../lib/api.js';

export async function mcpServersPage() {
  const el = h('div', {});
  let secrets = [];
  async function load() {
    mount(el, pageHeader('MCP servers', 'Connections that give agents and the assistant more tools.'), spinner());
    let list;
    try {
      [list, secrets] = await Promise.all([get('/v1/mcp-servers').then(r => r.servers), get('/v1/secrets').then(r => (r.secrets || []).map(s => s.name)).catch(() => [])]);
    } catch (e) { mount(el, pageHeader('MCP servers', ''), errorBox(e.message)); return; }
    mount(el, pageHeader('MCP servers', 'Connections that give agents and the assistant more tools. A tool is off until you turn it on, and only tools that only read can be turned on.',
      h('button', { class: 'btn-primary', type: 'button', id: 'mcp-add', onclick: () => form(null) }, icon('plus', 'size-4'), 'Add MCP server')),
      list.length ? h('div', { class: 'space-y-4', id: 'mcp-list' }, list.map(card))
        : emptyState('plug', 'No MCP servers yet', 'Add a remote server by its HTTPS address, or a local one as a container image. Agents get its tools only when you add them to an agent.'));
  }

  function card(s) {
    const on = s.tools.filter(t => t.enabled).length;
    const where = s.kind === 'remote' ? s.url : s.image;
    return h('section', { class: 'card', 'data-mcp': s.id },
      h('div', { class: 'flex flex-wrap items-start justify-between gap-3 border-b border-slate-200 px-5 py-3 dark:border-slate-800' },
        h('div', { class: 'min-w-0' },
          h('h2', { class: 'font-display text-lg font-semibold' }, s.name, h('span', { class: 'pill-idle ml-2 align-middle' }, s.kind === 'remote' ? 'remote' : 'local container')),
          h('p', { class: 'muted mt-0.5 break-all font-mono text-xs' }, where),
          h('p', { class: 'muted mt-1 text-xs' }, [
            s.discovered_at ? `Tools read ${relTime(s.discovered_at)}${s.protocol ? ' (' + (s.protocol === 'stdio' ? 'container' : 'MCP ' + s.protocol) + ')' : ''}` : 'Tools not read yet',
            `${on} of ${s.tools.length} on`, s.for_assistant ? 'offered to the assistant' : 'agents only',
            s.auth_secret ? 'token from secret ' + s.auth_secret : null, s.allow_private ? 'may reach a private network' : null,
          ].filter(Boolean).join(' · '))),
        h('div', { class: 'flex flex-wrap gap-2' },
          h('button', { class: 'btn-secondary btn-sm', type: 'button', 'data-discover': s.id, onclick: () => discover(s) }, icon('refresh', 'size-3.5'), 'Read its tools'),
          h('button', { class: 'btn-secondary btn-sm', type: 'button', onclick: () => form(s) }, icon('edit', 'size-3.5'), 'Change'),
          h('button', { class: 'btn-ghost btn-sm text-rose-600 dark:text-rose-400', type: 'button', 'aria-label': 'Remove ' + s.name, title: 'Remove', onclick: () => remove(s) }, icon('trash', 'size-3.5')))),
      s.error ? h('div', { class: 'px-5 pt-3' }, errorBox('The last read failed: ' + s.error)) : null,
      s.tools.length ? h('ul', { class: 'divide-y divide-slate-100 dark:divide-slate-800', id: 'mcp-tools-' + s.id }, s.tools.map(t => tool(s, t)))
        : h('p', { class: 'muted px-5 py-4 text-sm' }, 'Press Read its tools to see what it offers. Nothing is turned on until you choose.'));
  }

  function tool(s, t) {
    const can = !t.problem || t.problem.startsWith('its definition changed');
    const cb = h('input', { type: 'checkbox', class: 'mt-1 size-4', checked: !!t.enabled, disabled: !can && !t.enabled, 'aria-label': 'Turn on ' + t.name,
      onchange: () => setTools(s, t.name, cb.checked) });
    return h('li', { class: 'flex items-start gap-3 px-5 py-3', 'data-tool': t.name },
      cb,
      h('div', { class: 'min-w-0 flex-1' },
        h('div', { class: 'flex flex-wrap items-center gap-2' }, h('code', { class: 'text-sm font-medium' }, t.name),
          t.read_only ? h('span', { class: 'pill-ok' }, 'reads only') : h('span', { class: 'pill-wait' }, 'may change things'),
          t.enabled ? h('span', { class: 'muted text-xs' }, 'agents use it as ' + toolName(s.id, t.name)) : null),
        h('p', { class: 'muted mt-0.5 text-sm' }, t.description || 'No description.'),
        t.problem ? h('p', { class: 'mt-1 text-xs text-amber-700 dark:text-amber-400' }, t.problem) : null));
  }

  async function setTools(s, name, on) {
    const enabled = s.tools.filter(t => t.enabled && t.name !== name).map(t => t.name).concat(on ? [name] : []);
    try { await put('/v1/mcp-servers/' + encodeURIComponent(s.id) + '/tools', { enabled }); toast(on ? 'Turned on' : 'Turned off', 'ok'); } catch (e) { toast(e.message, 'error'); }
    load();
  }

  async function discover(s) {
    try { await post('/v1/mcp-servers/' + encodeURIComponent(s.id) + '/discover'); toast('Read its tools', 'ok'); } catch (e) { toast(e.message, 'error'); }
    load();
  }

  async function remove(s) {
    if (!await confirmDialog({ title: 'Remove ' + s.name + '?', body: 'Agents that use its tools lose them. Their history is kept.', confirmLabel: 'Remove', danger: true })) return;
    try { await del('/v1/mcp-servers/' + encodeURIComponent(s.id)); toast('Removed', 'ok'); } catch (e) { toast(e.message, 'error'); }
    load();
  }

  function form(s) {
    modal(s ? 'Change ' + s.name : 'Add an MCP server', (close) => {
      let kind = s ? s.kind : 'remote';
      const v = (id, val, attrs = {}) => h('input', { id, class: 'input', value: val ?? '', ...attrs });
      const id = v('mcp-id', s?.id, { placeholder: 'weather', disabled: !!s });
      const name = v('mcp-name', s?.name, { placeholder: 'Weather' });
      const url = v('mcp-url', s?.url, { placeholder: 'https://mcp.example.com/mcp' });
      const secret = h('select', { id: 'mcp-secret', class: 'input' }, [h('option', { value: '' }, 'No token')].concat(secrets.map(n => h('option', { value: n, selected: s?.auth_secret === n }, n))));
      const header = v('mcp-header', s?.auth_header || 'Authorization');
      const scheme = v('mcp-scheme', s ? s.auth_scheme : 'Bearer', { placeholder: 'Bearer' });
      const priv = h('input', { id: 'mcp-private', type: 'checkbox', class: 'size-4', checked: !!s?.allow_private });
      const image = v('mcp-image', s?.image, { placeholder: 'ghcr.io/owner/server@sha256:…' });
      const net = h('select', { id: 'mcp-net', class: 'input' }, [['none', 'No network'], ['hosts', 'Only these hosts'], ['all', 'The whole network']].map(([k, l]) =>
        h('option', { value: k, selected: (s?.network ? 'all' : s?.hosts?.length ? 'hosts' : 'none') === k }, l)));
      const hosts = v('mcp-hosts', (s?.hosts || []).join(', '), { placeholder: 'api.example.com, *.example.org' });
      const envs = h('textarea', { id: 'mcp-env', class: 'input font-mono text-xs', rows: '2', placeholder: 'API_KEY=NAME_OF_A_SECRET' }, Object.entries(s?.secrets || {}).map(([k, n]) => k + '=' + n).join('\n'));
      const asst = h('input', { id: 'mcp-assistant', type: 'checkbox', class: 'size-4', checked: !!s?.for_assistant });
      const err = h('div', {});
      const remoteBox = h('div', { class: 'space-y-3' }, field('Address', url, 'Its MCP endpoint over HTTPS (plain HTTP only for this machine).'),
        h('div', { class: 'grid grid-cols-3 gap-2' }, field('Token (from Secrets)', secret), field('Header', header), field('Scheme', scheme)),
        h('label', { class: 'flex items-center gap-2 text-sm', for: 'mcp-private' }, priv, 'It is on a private network (otherwise private and cloud-internal addresses are refused)'));
      const localBox = h('div', { class: 'space-y-3' }, field('Container image', image, 'Pinned by digest. It runs once per call, with the limits plugins have.'),
        h('div', { class: 'grid grid-cols-2 gap-2' }, field('Network', net), field('Hosts', hosts, 'When only some hosts')),
        field('Secrets it gets', envs, 'One ENV_NAME=SECRET_NAME per line, from Settings › Secrets.'));
      const kindBtns = h('div', { class: 'inline-flex overflow-hidden rounded-lg border border-slate-300 text-sm dark:border-slate-700', role: 'group' });
      const drawKind = () => {
        mount(kindBtns, ...[['remote', 'Remote (HTTPS)'], ['local', 'Local (container)']].map(([k, l]) => h('button', { type: 'button', 'aria-pressed': String(kind === k), disabled: !!s,
          class: 'px-3 py-1.5 ' + (kind === k ? 'bg-brand-600 text-white' : 'text-slate-600 dark:text-slate-300'), onclick: () => { kind = k; drawKind(); } }, l)));
        remoteBox.hidden = kind !== 'remote'; localBox.hidden = kind !== 'local';
      };
      drawKind();
      const save = h('button', { class: 'btn-primary', type: 'button', id: 'mcp-save', onclick: async () => {
        const secretsMap = {};
        for (const line of envs.value.split('\n')) { const i = line.indexOf('='); if (i > 0) secretsMap[line.slice(0, i).trim()] = line.slice(i + 1).trim(); }
        const body = { id: id.value.trim(), name: name.value.trim(), kind, for_assistant: asst.checked };
        if (kind === 'remote') Object.assign(body, { url: url.value.trim(), auth_secret: secret.value, auth_header: secret.value ? header.value.trim() : '', auth_scheme: secret.value ? scheme.value.trim() : '', allow_private: priv.checked });
        else Object.assign(body, { image: image.value.trim(), network: net.value === 'all', hosts: net.value === 'hosts' ? hosts.value.split(',').map(x => x.trim()).filter(Boolean) : [], secrets: secretsMap });
        try {
          if (s) await put('/v1/mcp-servers/' + encodeURIComponent(s.id), body); else await post('/v1/mcp-servers', body);
          close(); toast(s ? 'Saved' : 'Added. Press Read its tools next.', 'ok'); load();
        } catch (e) { mount(err, errorBox(e.message)); }
      } }, s ? 'Save' : 'Add');
      return h('div', { class: 'space-y-4' }, kindBtns, h('div', { class: 'grid grid-cols-2 gap-2' }, field('Id', id, 'Used in tool names'), field('Name', name)),
        remoteBox, localBox,
        h('label', { class: 'flex items-center gap-2 text-sm', for: 'mcp-assistant' }, asst, 'Offer its turned-on tools to the assistant (agents get them only when you add them)'),
        notice('info', 'What a tool returns is treated as data, never as instructions. Every call is in the audit log.'), err,
        h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'), save));
    }, { wide: true });
  }

  await load();
  return el;
}

function toolName(server, tool) {
  return 'mcp_' + server.replace(/-/g, '_') + '_' + tool.replace(/[-.]/g, '_');
}
