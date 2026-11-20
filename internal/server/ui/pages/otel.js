// Settings › Connections › OpenTelemetry (design note 049, slice 2): send every trace to an OpenTelemetry backend
// (Grafana, Datadog, Honeycomb, Jaeger, an OpenTelemetry Collector) over OTLP/HTTP. Header values (API keys) go into the
// vault and are never shown again. A person's user name is never sent.
import { h, icon, mount, pageHeader, spinner, errorBox, notice, toast, field, relTime } from '../lib/dom.js';
import { get, put, post } from '../lib/api.js';

const EXAMPLES = [
  ['Local collector or Jaeger', 'http://127.0.0.1:4318', ''],
  ['Grafana Cloud', 'https://otlp-gateway-<zone>.grafana.net/otlp', 'Authorization'],
  ['Honeycomb', 'https://api.honeycomb.io', 'x-honeycomb-team'],
  ['Datadog (OTLP intake)', 'https://otlp.<site>', 'dd-api-key'],
];

export async function otelPage() {
  const el = h('div', {});
  async function load() {
    let v;
    try { v = await get('/v1/settings/otel'); } catch (e) { return mount(el, pageHeader('OpenTelemetry', ''), errorBox(e.message)); }
    const enabled = h('input', { type: 'checkbox', id: 'otel-enabled', class: 'mt-1', checked: v.enabled });
    const endpoint = h('input', { id: 'otel-endpoint', class: 'input font-mono', value: v.endpoint || '', placeholder: 'https://otlp.example.com', spellcheck: 'false' });
    const content = h('input', { type: 'checkbox', id: 'otel-content', class: 'mt-1', checked: v.content });
    const service = h('input', { id: 'otel-service', class: 'input', value: v.service || '', placeholder: 'forgeline', maxlength: '100' });
    let headers = v.headers.map(x => ({ name: x.name, set: x.set, value: null }));
    const hbox = h('div', { class: 'space-y-2', id: 'otel-headers' });
    const drawHeaders = () => mount(hbox, headers.map((x, i) => h('div', { class: 'flex flex-wrap items-center gap-2', 'data-header': String(i) },
      h('input', { class: 'input w-44 font-mono text-sm', value: x.name, placeholder: 'Authorization', 'aria-label': 'Header name', oninput: (e) => { x.name = e.target.value; } }),
      h('input', { class: 'input min-w-0 flex-1 font-mono text-sm', type: 'password', autocomplete: 'new-password', placeholder: x.set ? 'Kept in the vault; type to replace' : 'Value, such as Bearer <key>', 'aria-label': 'Header value', oninput: (e) => { x.value = e.target.value; } }),
      h('button', { type: 'button', class: 'btn-ghost p-1.5', 'aria-label': 'Remove header', onclick: () => { headers.splice(i, 1); drawHeaders(); } }, icon('x', 'size-4')))),
      h('button', { type: 'button', class: 'btn-ghost btn-sm', id: 'otel-add-header', onclick: () => { headers.push({ name: '', set: false, value: '' }); drawHeaders(); } }, icon('plus', 'size-3.5'), 'Add a header'));
    drawHeaders();
    const out = h('div', { id: 'otel-result' });
    const save = h('button', { class: 'btn-primary', type: 'button', id: 'otel-save', onclick: async () => {
      save.disabled = true;
      try {
        await put('/v1/settings/otel', { enabled: enabled.checked, content: content.checked, endpoint: endpoint.value.trim(), service: service.value.trim(),
          headers: headers.filter(x => x.name.trim()).map(x => (x.value ? { name: x.name.trim(), value: x.value } : { name: x.name.trim() })) });
        toast('Saved', 'ok'); load();
      } catch (e) { mount(out, errorBox(e.message)); save.disabled = false; }
    } }, icon('check', 'size-4'), 'Save');
    const test = h('button', { class: 'btn-secondary', type: 'button', id: 'otel-test', onclick: async () => {
      test.disabled = true; mount(out, spinner());
      try { const r = await post('/v1/settings/otel/test'); mount(out, notice('ok', 'Sent a test trace (' + r.trace_id + '). Look for the service in your backend.')); }
      catch (e) { mount(out, errorBox(e.message)); }
      test.disabled = false;
    } }, icon('send', 'size-4'), 'Send a test trace');
    const st = v.stats;
    mount(el,
      pageHeader('OpenTelemetry', 'Send every trace (agent runs, assistant chats, workflow runs) to the observability tools you already use, over OTLP/HTTP.'),
      h('div', { class: 'grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]' },
        h('section', { class: 'card card-pad space-y-4' },
          h('label', { class: 'flex items-start gap-2.5 text-sm', for: 'otel-enabled' }, enabled, h('span', {}, h('span', { class: 'block font-medium' }, 'Send traces'), h('span', { class: 'muted block text-xs' }, 'In the background, in batches. A slow or broken endpoint never slows an agent.'))),
          field('Endpoint', endpoint, 'The OTLP/HTTP address, without /v1/traces. https, or http only for localhost and 127.0.0.1.', 'otel-endpoint'),
          h('div', { class: 'flex flex-wrap gap-1.5' }, EXAMPLES.map(([n, url, hdr]) => h('button', { type: 'button', class: 'rounded-full border border-slate-200 px-2.5 py-0.5 text-xs text-slate-600 hover:border-brand-400 dark:border-slate-700 dark:text-slate-300',
            onclick: () => { endpoint.value = url; if (hdr && !headers.some(x => x.name.toLowerCase() === hdr.toLowerCase())) { headers.push({ name: hdr, set: false, value: '' }); drawHeaders(); } } }, n))),
          field('Service name', service, 'How Forgeline is named in your backend. Empty: forgeline.', 'otel-service'),
          h('div', {}, h('div', { class: 'label' }, 'Headers'), h('p', { class: 'muted -mt-1 mb-2 text-xs' }, 'Sent with every request, such as an API key. Values are kept in the vault and never shown again.'), hbox),
          h('label', { class: 'flex items-start gap-2.5 rounded-lg border border-amber-200 bg-amber-50/60 p-3 text-sm dark:border-amber-500/30 dark:bg-amber-950/30', for: 'otel-content' }, content,
            h('span', {}, h('span', { class: 'block font-medium' }, 'Keep prompts and answers in traces'),
              h('span', { class: 'muted block text-xs' }, 'The question, the answer, and each tool\'s arguments and result, cut to 4 KB, with anything shaped like a credential and every stored secret hidden. They are kept here and sent with the traces. Tool results can hold anything an agent read, so turn this on only where that is acceptable.'))),
          notice('info', 'Always sent: names, models, timings, tokens, cost, errors and roles. Never a person\'s user name; prompts and answers only with the box above ticked.'),
          out,
          h('div', { class: 'flex flex-wrap justify-end gap-2' }, test, save)),
        h('aside', { class: 'card card-pad h-fit space-y-2 text-sm', id: 'otel-stats' },
          h('h2', { class: 'h-section' }, 'Since the server started'),
          h('div', { class: 'flex justify-between' }, h('span', { class: 'muted' }, 'Spans sent'), h('span', { class: 'tabular-nums' }, String(st.sent_spans))),
          h('div', { class: 'flex justify-between' }, h('span', { class: 'muted' }, 'Waiting to send'), h('span', { class: 'tabular-nums' }, String(st.queued_spans))),
          h('div', { class: 'flex justify-between' }, h('span', { class: 'muted' }, 'Dropped'), h('span', { class: 'tabular-nums' }, String(st.dropped_spans))),
          h('div', { class: 'flex justify-between' }, h('span', { class: 'muted' }, 'Last sent'), h('span', {}, st.last_sent && !st.last_sent.startsWith('0001') ? relTime(st.last_sent) : 'never')),
          st.last_error ? errorBox('Last error: ' + st.last_error) : null)));
  }
  mount(el, spinner());
  await load();
  return el;
}
