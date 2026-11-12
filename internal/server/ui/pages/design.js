// Build › Design (design note 035, slice 2): a blueprint on a canvas. Blocks are drawn in SVG (positions are
// attributes, so the CSP needs no inline styles), dragged to move, and edited in the panel on the right. Every
// change is checked by the server as you go; Save keeps it. Preview and approval are the next slice.
import { h, icon, mount, pageHeader, relTime, spinner, errorBox, notice, toast, emptyState, modal, field, copyButton } from '../lib/dom.js';
import { get, post, put } from '../lib/api.js';
import { navigate } from '../lib/router.js';
import { KINDS, EXISTING, DB_SIZES, CACHE_SIZES, blockFacts, linkWords } from '../lib/blueprint.js';
import { flowPath } from '../lib/flow.js';
import { guardrailList } from '../lib/guardrails.js';
import {
  BLOCK_W, BLOCK_H, FARGATE, LOG_DAYS, autoLayout, addBlock, removeBlock, updateBlock, moveBlock, linkTargets,
  addLink, removeLink, toBody, linkPath, linkPorts, bounds, fit, explainError, planByBlock, planState, planWords, placeNew, flowOf,
} from '../lib/canvas.js';

/** Build › Design without a blueprint: open the one changed last, or say how to make one. */
export async function designHomePage() {
  const el = h('div', {});
  mount(el, spinner());
  let list = [];
  try { list = (await get('/v1/blueprints')).blueprints; } catch (e) { mount(el, pageHeader('Design', ''), errorBox(e.message)); return el; }
  if (list.length) { navigate('/design/' + list[0].id); return el; }
  mount(el, pageHeader('Design', 'Draw what should exist in your cloud.'), h('div', { class: 'card' },
    emptyState('layers', 'Nothing to design yet', 'Make a blueprint first; it opens here on the canvas.', h('a', { class: 'btn-primary', href: '#/blueprints' }, 'Go to Blueprints'))));
  return el;
}

export async function designPage({ params }) {
  const el = h('div', {});
  mount(el, spinner());
  let saved;
  try { saved = await get('/v1/blueprints/' + encodeURIComponent(params.id)); } catch (e) { mount(el, pageHeader('Design', ''), errorBox(e.message)); return el; }
  let others = [], plan = null, built = null, pollTimer = 0;
  try { plan = await get('/v1/blueprints/' + encodeURIComponent(params.id) + '/plan'); } catch { /* no preview yet */ }
  try { built = await get('/v1/blueprints/' + encodeURIComponent(params.id) + '/built'); } catch { /* optional */ }
  try { others = (await get('/v1/blueprints')).blueprints; } catch { /* the switcher is optional */ }
  let repos = [];
  try { repos = ((await get('/v1/integrations/github')).rules || []).map(r => r.repo); } catch { /* typing the name still works */ }

  let revision = saved.revision;
  let bp = autoLayout({ name: saved.name, description: saved.description || '', target: saved.target, blocks: saved.blocks, links: saved.links });
  let clean = JSON.stringify(bp), selected = '', problem = '', checks = saved.checks, checkTimer = 0, checkSeq = 0;
  const dirty = () => JSON.stringify(bp) !== clean;

  const canvasBox = h('div', { class: 'overflow-auto', id: 'design-canvas' });
  const inspector = h('aside', { class: 'card card-pad', id: 'design-inspector', 'aria-label': 'Selected block' });
  const helper = h('section', { class: 'card card-pad', id: 'design-assistant', 'aria-label': 'Assistant' });
  // The conversation with the assistant beside the canvas: what was asked, what it said, and a draft it suggests.
  const talk = [];
  let suggestion = null, asking = false;
  const side = h('div', { id: 'design-side' });
  const tools = h('div', {});
  const bar = h('div', {});

  function change(next, { redraw = true } = {}) {
    bp = next;
    if (redraw) draw();
    clearTimeout(checkTimer);
    checkTimer = setTimeout(check, 350);
    drawBar();
  }

  async function check() {
    const seq = ++checkSeq;
    try {
      const r = await post('/v1/blueprints/check', toBody(bp, revision));
      if (seq !== checkSeq) return; // a newer edit has been checked since
      problem = r.error ? explainError(bp, r.error) : '';
      checks = r.checks || [];
    } catch (e) { problem = e.message; }
    drawSide();
    drawBar();
  }

  async function preview(body = {}) {
    try { plan = await post('/v1/blueprints/' + encodeURIComponent(saved.id) + '/plan', body); } catch (e) { toast(e.message, 'error'); return; }
    drawBar(); drawSide(); draw();
    poll();
  }

  // Removing what it built is a preview too (note 041): it is approved by someone else and exactly that plan is applied.
  async function removal() {
    await modal('Remove what this blueprint built', (close) => {
      let mode = 'keep-data';
      const opt = (value, title, text) => h('label', { class: 'flex cursor-pointer items-start gap-3 rounded-lg border border-slate-200 p-3 dark:border-slate-700' },
        h('input', { type: 'radio', name: 'remove-mode', value, checked: value === mode, class: 'mt-1', onchange: () => { mode = value; } }),
        h('span', {}, h('span', { class: 'block text-sm font-medium' }, title), h('span', { class: 'muted block text-xs' }, text)));
      const snap = h('input', { type: 'checkbox', id: 'remove-snapshot', checked: true });
      return h('div', { class: 'space-y-3', id: 'remove-dialog' },
        h('p', { class: 'text-sm' }, 'Nothing is removed yet. This makes a preview; a different administrator approves it, then exactly that preview is applied. The blueprint itself is kept, so you can build it again.'),
        opt('keep-data', 'Keep the data', 'Removes apps, front doors, queues, caches and logs. Tables, buckets, databases and secrets stay, still protected (they keep costing what they cost).'),
        opt('everything', 'Remove everything, data included', 'Takes two approvals when something is protected: first deletion protection is turned off and the rest removed, then the data is deleted. What tables, buckets and databases hold is lost.'),
        h('label', { class: 'flex items-center gap-2 text-sm' }, snap, 'Keep a final snapshot of each database (it costs a little storage until you delete it in AWS)'),
        h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Keep everything'),
          h('button', { class: 'btn-danger', type: 'button', id: 'remove-go', onclick: () => { close(); preview({ remove: mode, final_snapshot: snap.checked }); } }, 'Preview the removal')));
    });
  }

  function poll() {
    clearTimeout(pollTimer);
    if (plan?.status !== 'planning') return;
    pollTimer = setTimeout(async () => {
      if (!el.isConnected) return; // the person left the page
      try { plan = await get('/v1/blueprints/' + encodeURIComponent(saved.id) + '/plan'); } catch { /* try again */ }
      drawBar(); drawSide(); draw();
      poll();
    }, 1500);
  }

  async function send() {
    await modal('Send for approval', (close) => {
      const reason = h('textarea', { id: 'send-reason', class: 'input', rows: '3', maxlength: '500', placeholder: plan.removal ? 'Why this should be removed' : 'Why this should be built' });
      const err = h('div', {});
      return h('div', { class: 'space-y-4' },
        h('p', { class: 'text-sm' }, planWords(plan) + ' in ' + bp.target.account + ' · ' + bp.target.region + '. A different administrator approves it; then exactly this preview is applied.'),
        field('Why', reason, 'The approver reads this.'), err,
        h('div', { class: 'flex justify-end gap-2' }, h('button', { class: 'btn-secondary', type: 'button', onclick: () => close() }, 'Cancel'),
          h('button', { class: 'btn-primary', type: 'button', id: 'send-go', onclick: async () => {
            try {
              await post('/v1/blueprints/' + encodeURIComponent(saved.id) + '/propose', { reason: reason.value });
              close();
              toast('Sent for approval. It is listed under Infrastructure › Cloud changes.', 'ok');
            } catch (e) { mount(err, errorBox(e.message)); }
          } }, 'Send')));
    });
  }

  async function save() {
    try {
      const r = await put('/v1/blueprints/' + encodeURIComponent(saved.id), toBody(bp, revision));
      revision = r.revision; checks = r.checks; clean = JSON.stringify(bp); problem = '';
      toast('Saved', 'ok');
      draw();
    } catch (e) { problem = explainError(bp, e.message); toast(problem, 'error'); }
    drawSide();
    drawBar();
  }

  function drawBar() {
    const can = dirty() && !problem;
    const ps = planState(plan, revision, dirty());
    mount(bar, h('div', { class: 'mb-4 flex flex-wrap items-center gap-2' },
      h('span', { class: 'inline-flex overflow-hidden rounded-lg border border-slate-300 text-sm dark:border-slate-700' },
        h('span', { class: 'bg-brand-600 px-3 py-1.5 text-white' }, 'AWS'), h('span', { class: 'px-3 py-1.5' }, bp.target.account + ' · ' + bp.target.region)),
      others.length > 1 && h('select', { class: 'input h-9 w-auto py-0 text-sm', 'aria-label': 'Open another blueprint', onchange: (e) => navigate('/design/' + e.target.value) },
        others.map(o => h('option', { value: o.id, selected: o.id === saved.id }, o.name))),
      dirty() ? h('span', { class: 'pill-wait', id: 'design-dirty' }, 'Not saved') : h('span', { class: 'muted text-xs' }, 'Saved'),
      built?.removed ? h('span', { class: 'pill-idle', id: 'design-built', title: 'Everything it built was removed, last by ' + built.updated_by + ' ' + relTime(built.updated_at) }, 'Removed') : null,
      built?.built ? h('span', { class: 'pill-ok', id: 'design-built', title: 'Built ' + built.applies + ' time' + (built.applies === 1 ? '' : 's') + ', last by ' + built.updated_by + ' ' + relTime(built.updated_at) }, 'Built') : null,
      h('span', { class: 'flex-1' }),
      h('a', { class: 'btn-secondary btn-sm', href: '#/blueprints/' + saved.id }, icon('git', 'size-3.5'), 'Code'),
      h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'design-preview', disabled: dirty() || !!problem || ps === 'planning',
        title: dirty() ? 'Save first: the preview is of the saved blueprint' : 'Ask OpenTofu what building this would do in AWS (reads only)', onclick: preview },
        ps === 'planning' ? 'Previewing…' : 'Preview changes'),
      built?.built ? h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'design-remove', disabled: dirty() || ps === 'planning',
        title: 'Preview removing what this blueprint built in AWS', onclick: removal }, icon('trash', 'size-3.5'), 'Remove…') : null,
      h('button', { class: (ps === 'ready' ? 'btn-primary' : 'btn-secondary') + ' btn-sm', type: 'button', id: 'design-send', disabled: ps !== 'ready',
        title: ps === 'ready' ? 'A different administrator approves; then exactly this preview is applied' : ps === 'blocked' ? 'The preview breaks a guardrail; change the blueprint first' : 'Preview the changes first', onclick: send }, 'Send for approval'),
      h('button', { class: 'btn-primary btn-sm', type: 'button', id: 'design-save', disabled: !can, onclick: save }, 'Save')));
  }

  function drawTools() {
    mount(tools, h('div', { class: 'flex flex-wrap items-center gap-1.5 border-b border-slate-200 px-3 py-2 dark:border-slate-800', role: 'toolbar', 'aria-label': 'Add a block' },
      h('span', { class: 'muted mr-1 text-xs' }, 'Add'),
      Object.entries(KINDS).map(([k, v]) =>
        h('button', { type: 'button', class: 'flex items-center gap-1.5 rounded-lg border border-slate-200 px-2 py-1 text-xs hover:border-brand-400 dark:border-slate-700', 'data-add': k,
          onclick: () => { const r = addBlock(bp, k); selected = r.id; change(r.bp); drawInspector(); } }, icon(v.icon, 'size-3.5 text-slate-500'), v.label))));
  }

  function previewCard() {
    const ps = planState(plan, revision, dirty());
    if (ps === 'none') return null;
    const head = h('h2', { class: 'h-section mb-2' }, 'Preview');
    if (ps === 'planning') return h('section', { class: 'card card-pad', id: 'design-plan' }, head, h('p', { class: 'flex items-center gap-2 text-sm' }, spinner(), 'OpenTofu is working out what would change. It only reads.'));
    if (ps === 'failed') return h('section', { class: 'card card-pad', id: 'design-plan' }, head, errorBox(plan.error || 'The preview failed.'));
    const tone = { add: 'pill-ok', change: 'pill-wait', replace: 'pill-bad', remove: 'pill-bad' };
    return h('section', { class: 'card card-pad', id: 'design-plan' }, head,
      ps === 'stale' ? h('div', { class: 'mb-2' }, notice('warn', 'The blueprint changed since this preview, or it is over an hour old. Save and preview again before sending it.')) : null,
      h('p', { class: 'text-sm font-medium', id: 'design-plan-summary' }, planWords(plan)),
      h('p', { class: 'muted mb-2 text-xs' }, 'Planned by ' + plan.by + ' ' + relTime(plan.finished_at) + ' against AWS account ' + plan.aws_account + '.'),
      guardrailList(plan.guardrails, plan.guardrail_version),
      plan.changes.length ? h('details', {}, h('summary', { class: 'cursor-pointer text-sm' }, 'Every resource'),
        h('ul', { class: 'mt-2 max-h-64 space-y-1 overflow-auto text-xs' }, plan.changes.map(c => h('li', { class: 'flex items-center gap-2' },
          h('span', { class: tone[c.action] }, c.action), h('code', { class: 'break-all' }, c.address))))) : null);
  }

  function drawSide() {
    mount(side, previewCard(),
      h('section', { class: 'card card-pad mt-4', id: 'design-checks', 'aria-live': 'polite' }, h('h2', { class: 'h-section mb-2' }, 'What it means'),
        problem ? h('div', { id: 'design-problem' }, errorBox(problem)) : null,
        h('ul', { class: 'mt-2 grid gap-x-6 gap-y-2 text-sm md:grid-cols-2' }, (problem ? [] : checks).map(c => h('li', { class: 'flex items-start gap-2' },
          h('span', { class: c.level === 'ok' ? 'text-emerald-600 dark:text-emerald-400' : 'text-amber-600 dark:text-amber-400' }, icon(c.level === 'ok' ? 'check' : 'alert', 'mt-0.5 size-4')), c.text)))));
  }

  // ---------------------------------------------------------------- the drawing

  function draw() {
    const { w, h: ht } = bounds(bp);
    const byId = Object.fromEntries(bp.blocks.map(b => [b.id, b]));
    const ports = linkPorts(bp);
    const svg = h('svg', { width: String(w), height: String(ht), viewBox: `0 0 ${w} ${ht}`, class: 'block h-auto max-w-full min-w-[36rem] select-none', role: 'group', 'aria-label': 'Blueprint drawing' });
    for (const l of bp.links) {
      const a = byId[l.from], b = byId[l.to];
      if (!a || !b) continue;
      const traffic = a.kind === 'frontdoor', logs = b.kind === 'logs';
      const on = selected && (l.from === selected || l.to === selected);
      svg.append(h('path', { d: linkPath(a, b, ports.get(l.from + '>' + l.to)), fill: 'none', 'stroke-width': on ? '2.5' : '1.75', 'stroke-dasharray': logs ? '5 4' : '',
        class: traffic ? 'stroke-brand-500' : on ? 'stroke-slate-600 dark:stroke-slate-300' : 'stroke-slate-400 dark:stroke-slate-500', 'data-link': l.from + '>' + l.to },
        h('title', {}, linkWords(bp, l))));
      // Moving dots show which way things flow along the link.
      const way = flowOf(bp, l), d = linkPath(a, b, ports.get(l.from + '>' + l.to)), dots = traffic ? 'stroke-brand-500' : 'stroke-slate-500 dark:stroke-slate-300';
      if (way === 'forward' || way === 'both') svg.append(flowPath(d, dots));
      if (way === 'back' || way === 'both') svg.append(flowPath(d, dots, { back: true }));
    }
    // The internet reaches each front door from the left, as in the design.
    for (const b of bp.blocks.filter(x => x.kind === 'frontdoor')) {
      const y = b.y + BLOCK_H / 2, x0 = Math.max(0, b.x - 56), d = `M${x0} ${y} L${b.x} ${y}`;
      svg.append(h('path', { d, fill: 'none', 'stroke-width': '1.75', class: 'stroke-brand-500', 'data-internet': b.id }), flowPath(d, 'stroke-brand-500'),
        h('text', { x: String(x0), y: String(y - 8), class: 'fill-slate-400 text-[10px]' }, 'internet'));
    }
    for (const b of bp.blocks) svg.append(blockNode(b));
    mount(canvasBox, svg);
  }

  // What the current preview does to each block, shown on the block while the preview is current.
  const ACT = { add: ['+ add', 'fill-emerald-600 dark:fill-emerald-400', 'stroke-emerald-500'], change: ['~ change', 'fill-amber-600 dark:fill-amber-400', 'stroke-amber-500'],
    replace: ['replace', 'fill-rose-600 dark:fill-rose-400', 'stroke-rose-500'], remove: ['remove', 'fill-rose-600 dark:fill-rose-400', 'stroke-rose-500'] };
  function blockNode(b) {
    const k = KINDS[b.kind] || { label: b.kind, tech: '' };
    const ps = planState(plan, revision, dirty());
    const act = ps === 'ready' || ps === 'nothing' || ps === 'blocked' ? ACT[planByBlock(plan)[b.id]] : null;
    const on = b.id === selected, existing = b.kind === 'existing';
    const g = h('g', { transform: `translate(${b.x} ${b.y})`, tabindex: '0', role: 'button', 'aria-pressed': String(on), 'aria-label': b.name + ', ' + k.label, 'data-block': b.id, class: 'cursor-grab focus:outline-none' },
      h('rect', { width: String(BLOCK_W), height: String(BLOCK_H), rx: '12', 'stroke-width': on ? '2.5' : '1.25', 'stroke-dasharray': existing ? '5 4' : '',
        class: (existing ? 'fill-slate-50 dark:fill-slate-900/60 ' : 'fill-white dark:fill-slate-900 ') + (on ? 'stroke-brand-500' : act ? act[2] : b.kind === 'frontdoor' ? 'stroke-brand-400' : 'stroke-slate-300 dark:stroke-slate-700') }),
      h('text', { x: '14', y: '24', class: 'fill-slate-900 text-[13px] font-semibold dark:fill-slate-100' }, fit(b.name, 21)),
      h('text', { x: String(BLOCK_W - 12), y: '24', 'text-anchor': 'end', class: act ? act[1] + ' text-[10px] font-semibold uppercase' : 'fill-slate-400 text-[10px] uppercase tracking-wide', 'data-act': act ? act[0] : '' }, act ? act[0] : k.label),
      h('text', { x: '14', y: '44', class: 'fill-slate-500 text-[11px] dark:fill-slate-400' }, fit(k.tech, 28)),
      h('text', { x: '14', y: '58', class: 'fill-slate-500 text-[10px] dark:fill-slate-400' }, fit(blockFacts(b), 33)),
      h('title', {}, b.name + ' · ' + blockFacts(b)));
    // Drag to move; a press without moving selects. Arrow keys move a focused block.
    g.addEventListener('pointerdown', (e) => {
      if (e.button !== 0) return;
      const svg = g.ownerSVGElement, start = { x: e.clientX, y: e.clientY, bx: b.x, by: b.y };
      const scale = svg.getBoundingClientRect().width / Number(svg.getAttribute('width')) || 1;
      let moved = false;
      g.setPointerCapture(e.pointerId);
      const onMove = (ev) => {
        const dx = (ev.clientX - start.x) / scale, dy = (ev.clientY - start.y) / scale;
        if (!moved && Math.hypot(dx, dy) < 4) return;
        moved = true;
        g.setAttribute('transform', `translate(${Math.max(0, start.bx + dx)} ${Math.max(0, start.by + dy)})`);
      };
      const onUp = (ev) => {
        g.removeEventListener('pointermove', onMove);
        g.removeEventListener('pointerup', onUp);
        if (moved) change(moveBlock(bp, b.id, start.bx + (ev.clientX - start.x) / scale, start.by + (ev.clientY - start.y) / scale));
        else select(b.id === selected ? '' : b.id);
      };
      g.addEventListener('pointermove', onMove);
      g.addEventListener('pointerup', onUp);
    });
    g.addEventListener('keydown', (e) => {
      const step = e.shiftKey ? 40 : 10, d = { ArrowLeft: [-step, 0], ArrowRight: [step, 0], ArrowUp: [0, -step], ArrowDown: [0, step] }[e.key];
      if (d) { e.preventDefault(); change(moveBlock(bp, b.id, b.x + d[0], b.y + d[1])); canvasBox.querySelector(`[data-block="${b.id}"]`)?.focus(); }
      else if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); select(b.id === selected ? '' : b.id); }
    });
    return g;
  }

  function select(id) {
    selected = id;
    draw();
    drawInspector();
  }

  // ---------------------------------------------------------------- the assistant

  function drawHelper() {
    const input = h('textarea', { id: 'assist-input', class: 'input min-w-0 flex-1 text-sm', rows: '2', maxlength: '4000', placeholder: 'Change something…', 'aria-label': 'Ask the assistant to change the blueprint', disabled: asking });
    const go = async () => {
      const message = input.value.trim();
      if (!message || asking) return;
      asking = true; suggestion = null;
      talk.push({ role: 'user', content: message });
      drawHelper();
      try {
        const r = await post('/v1/blueprints/' + encodeURIComponent(saved.id) + '/assist', { message, draft: toBody(bp, revision), history: talk.slice(0, -1).slice(-8) });
        talk.push({ role: 'assistant', content: r.reply || (r.draft ? 'Here is a draft.' : '') });
        suggestion = r.draft ? r : null;
        if (r.problem) talk.push({ role: 'note', content: 'Not offered: ' + explainError(bp, r.problem) });
      } catch (e) { talk.push({ role: 'note', content: e.message }); }
      asking = false;
      drawHelper();
      helper.querySelector('#assist-input')?.focus();
    };
    input.addEventListener('keydown', (e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); go(); } });
    const bubble = (t) => h('div', { class: t.role === 'user' ? 'ml-6 rounded-xl rounded-br-sm bg-slate-100 px-3 py-2 dark:bg-slate-800' : t.role === 'note' ? 'text-xs text-amber-700 dark:text-amber-300' : 'mr-2' }, t.content);
    mount(helper,
      h('h2', { class: 'h-section mb-2 flex items-center gap-1.5' }, icon('sparkle', 'size-3.5'), 'Assistant'),
      talk.length ? h('div', { class: 'mb-3 max-h-72 space-y-2 overflow-y-auto text-sm', id: 'assist-talk', 'aria-live': 'polite' }, talk.map(bubble), asking ? h('p', { class: 'muted flex items-center gap-2 text-xs' }, spinner(), 'Thinking…') : null)
        : h('p', { class: 'hint mb-3' }, 'Describe a change in your own words, for example “add a queue the app reads”. The assistant edits a copy; you decide whether to use it, and nothing is built until it is approved.'),
      suggestion ? h('div', { class: 'mb-3 rounded-lg border border-brand-200 bg-brand-50 p-3 text-sm dark:border-brand-500/30 dark:bg-brand-500/10', id: 'assist-suggestion' },
        h('b', { class: 'block' }, 'Suggested changes'),
        suggestion.changes.length ? h('ul', { class: 'mt-1 list-disc space-y-0.5 pl-5' }, suggestion.changes.map(c => h('li', {}, c))) : h('p', { class: 'muted' }, 'Nothing changes.'),
        h('div', { class: 'mt-2 flex gap-2' },
          h('button', { class: 'btn-primary btn-sm', type: 'button', id: 'assist-use', disabled: !suggestion.changes.length, onclick: () => {
            const d = suggestion.draft;
            suggestion = null;
            selected = '';
            change(placeNew({ ...bp, blocks: d.blocks, links: d.links }));
            drawInspector(); drawHelper();
            toast('Used the suggestion. Save to keep it.', 'ok');
          } }, 'Use this'),
          h('button', { class: 'btn-ghost btn-sm', type: 'button', onclick: () => { suggestion = null; drawHelper(); } }, 'Dismiss'))) : null,
      h('div', { class: 'flex items-end gap-2' }, input, h('button', { class: 'btn-primary btn-sm', type: 'button', id: 'assist-send', disabled: asking, 'aria-label': 'Send', onclick: go }, icon('send', 'size-4'))));
  }

  // ---------------------------------------------------------------- the panel

  function input(label, value, onChange, attrs = {}) {
    const id = 'f-' + label.toLowerCase().replace(/[^a-z]+/g, '-');
    const el = h('input', { id, class: 'input', value: value ?? '', ...attrs });
    el.addEventListener('change', () => onChange(attrs.type === 'number' ? (el.value === '' ? '' : Number(el.value)) : el.value.trim()));
    return h('div', {}, h('label', { class: 'label', for: id }, label), el);
  }
  function select_(label, value, options, onChange) {
    const id = 'f-' + label.toLowerCase().replace(/[^a-z]+/g, '-');
    const el = h('select', { id, class: 'input' }, options.map(([v, t]) => h('option', { value: String(v), selected: String(v) === String(value) }, t)));
    el.addEventListener('change', () => onChange(el.value));
    return h('div', {}, h('label', { class: 'label', for: id }, label), el);
  }

  // NAME=value settings, one per line, for an app or an agent.
  function settingsField(b, set) {
    const id = 'f-settings';
    const ta = h('textarea', { id, class: 'input font-mono text-xs', rows: '3', placeholder: 'MODE=production' }, Object.entries(b.env || {}).map(([k2, v]) => k2 + '=' + v).join('\n'));
    ta.addEventListener('change', () => {
      const env = {};
      for (const line of ta.value.split('\n')) { const i = line.indexOf('='); if (i > 0) env[line.slice(0, i).trim()] = line.slice(i + 1); }
      set({ env: Object.keys(env).length ? env : null });
    });
    return h('div', {}, h('label', { class: 'label', for: id }, 'Settings'), ta, h('p', { class: 'hint' }, 'One NAME=value per line. Not for secrets.'));
  }

  // Where the block's code lives (note 040, step 4): the blueprint then makes a private image repository and a push role
  // for that repository's branch, and Forgeline suggests the workflow that builds it.
  function sourceField(b, set) {
    const src = b.source || {};
    const list = h('datalist', { id: 'f-repos' }, repos.map(r => h('option', { value: r })));
    // Setting or clearing the repository adds or removes the fields under it, so the inspector is drawn again.
    const update = (part) => { const next = { ...src, ...part }; set({ source: next.repo ? next : null }); if (!!next.repo !== !!src.repo) setTimeout(drawInspector); };
    return h('div', { class: 'space-y-2 rounded-lg border border-slate-200 p-3 dark:border-slate-700', id: 'f-source' },
      h('div', { class: 'h-section' }, 'Code'),
      input('GitHub repository', src.repo, v => update({ repo: v }), { placeholder: 'owner/name', list: 'f-repos' }), list,
      src.repo ? h('div', { class: 'grid grid-cols-2 gap-2' },
        input('Branch', src.branch, v => update({ branch: v || undefined }), { placeholder: 'main' }),
        input('Folder', src.path, v => update({ path: v || undefined }), { placeholder: '.' })) : null,
      src.repo ? h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'f-delivery', title: 'Suggests the workflow that builds it, for the saved blueprint',
        onclick: () => dirty() ? toast('Save first: the suggestion is for the saved blueprint', 'error') : delivery(b) }, icon('git', 'size-3.5'), 'Build from GitHub…')
        : h('p', { class: 'hint' }, 'Optional. With a repository, the blueprint makes a private image repository for it and a role only that branch can use to push images.'));
  }

  async function delivery(b) {
    let d;
    try { d = await get('/v1/blueprints/' + encodeURIComponent(saved.id) + '/blocks/' + encodeURIComponent(b.id) + '/delivery'); } catch (e) { toast(e.message, 'error'); return; }
    await modal('Build ' + b.name + ' from GitHub', () => h('div', { class: 'space-y-4', id: 'delivery' },
      h('p', { class: 'text-sm' }, d.next),
      h('div', {}, h('div', { class: 'mb-1 flex items-center justify-between' }, h('span', { class: 'h-section' }, d.workflow_path + ' in ' + d.repo), copyButton(d.workflow, 'Copy')),
        h('pre', { class: 'max-h-72 overflow-auto rounded-lg bg-slate-50 p-3 text-xs dark:bg-slate-900', id: 'delivery-workflow' }, d.workflow)),
      h('div', {}, h('div', { class: 'mb-1 flex items-center justify-between' }, h('span', { class: 'h-section' }, 'A Forgeline pipeline that starts it'), copyButton(d.pipeline, 'Copy')),
        h('pre', { class: 'overflow-auto rounded-lg bg-slate-50 p-3 text-xs dark:bg-slate-900', id: 'delivery-pipeline' }, d.pipeline),
        h('p', { class: 'hint mt-1' }, 'Save it under Ship › Pipelines. The repository and branch must be on the GitHub allow list.'))), { xl: true });
  }

  function drawInspector() {
    const b = bp.blocks.find(x => x.id === selected);
    if (!b) {
      mount(inspector, h('h2', { class: 'h-section mb-3' }, 'Blueprint'),
        h('div', { class: 'space-y-3' },
          input('Name', bp.name, v => change({ ...bp, name: v }, { redraw: false })),
          h('p', { class: 'hint' }, 'Pick a block to change it. Drag blocks to move them; arrow keys move the one in focus.')));
      return;
    }
    const set = (fields) => change(updateBlock(bp, b.id, fields));
    const k = KINDS[b.kind];
    const fields = [input('Name', b.name, v => set({ name: v }))];
    switch (b.kind) {
      case 'service':
        fields.push(input('Container image', b.image, v => set({ image: v }), { placeholder: 'nginx:1.29' }),
          h('div', { class: 'grid grid-cols-2 gap-2' },
            select_('CPU', b.cpu, Object.keys(FARGATE).map(c => [c, c / 1024 + ' vCPU']), v => set({ cpu: Number(v), memory: FARGATE[v][0] })),
            select_('Memory', b.memory, (FARGATE[b.cpu] || []).map(m => [m, m / 1024 + ' GB']), v => set({ memory: Number(v) }))),
          h('div', { class: 'grid grid-cols-2 gap-2' },
            input('Tasks', b.count ?? 0, v => set({ count: v }), { type: 'number', min: '0', max: '10' }),
            input('Port', b.port ?? '', v => set({ port: v }), { type: 'number', min: '1', max: '65535', placeholder: 'none' })));
        {
          const cb = h('input', { id: 'f-scale', type: 'checkbox', class: 'size-4', checked: !!b.max });
          cb.addEventListener('change', () => set(cb.checked ? { min: Math.max(1, b.count || 1), max: Math.max(2, (b.count || 1) * 2), cpu_target: 60, count: Math.max(1, b.count || 1) } : { min: null, max: null, cpu_target: null }));
          fields.push(h('label', { class: 'flex items-center gap-2 text-sm', for: 'f-scale' }, cb, 'Scale with load'));
          if (b.max) fields.push(h('div', { class: 'grid grid-cols-3 gap-2' },
            input('Fewest', b.min, v => set({ min: v }), { type: 'number', min: '1', max: '20' }),
            input('Most', b.max, v => set({ max: v }), { type: 'number', min: '1', max: '20' }),
            input('CPU aim %', b.cpu_target ?? 60, v => set({ cpu_target: v }), { type: 'number', min: '20', max: '90' })),
            h('p', { class: 'hint' }, 'Adds tasks when average CPU is above the aim and removes them when it is below. Tasks is where it starts.'));
        }
        fields.push(settingsField(b, set), sourceField(b, set));
        break;
      case 'agent':
        fields.push(input('Container image (ECR)', b.image, v => set({ image: v }), { placeholder: '111122223333.dkr.ecr.' + bp.target.region + '.amazonaws.com/my-agent:1.0' }),
          input('What it does', b.description, v => set({ description: v || null }), { placeholder: 'answers support questions' }),
          settingsField(b, set), sourceField(b, set),
          h('p', { class: 'hint' }, 'Runs on Amazon Bedrock AgentCore. Build the image for arm64, answering on port 8080 at /invocations and /ping. Link it to the tables, files, queues and secrets it may use; it gets exactly those and Bedrock models, nothing else.'));
        break;
      case 'frontdoor':
        fields.push(input('Health check path', b.health_path, v => set({ health_path: v }), { placeholder: '/' }),
          input('Certificate (for HTTPS)', b.certificate, v => set({ certificate: v }), { placeholder: 'arn:aws:acm:…' }));
        break;
      case 'table':
        fields.push(h('div', { class: 'grid grid-cols-[1fr_auto] gap-2' }, input('Key', b.partition_key, v => set({ partition_key: v })),
          select_('Key type', b.partition_type, [['S', 'Text'], ['N', 'Number']], v => set({ partition_type: v }))),
        h('div', { class: 'grid grid-cols-[1fr_auto] gap-2' }, input('Sort key', b.sort_key, v => set({ sort_key: v, sort_type: v ? (b.sort_type || 'S') : null }), { placeholder: 'optional' }),
          select_('Sort type', b.sort_type || 'S', [['S', 'Text'], ['N', 'Number']], v => set({ sort_type: b.sort_key ? v : null }))));
        break;
      case 'bucket': {
        const cb = h('input', { id: 'f-versions', type: 'checkbox', class: 'size-4', checked: !!b.versioning });
        cb.addEventListener('change', () => set({ versioning: cb.checked }));
        fields.push(h('label', { class: 'flex items-center gap-2 text-sm', for: 'f-versions' }, cb, 'Keep old versions of files'));
        break;
      }
      case 'queue':
        fields.push(input('Keep messages (days)', b.retention_days ?? '', v => set({ retention_days: v }), { type: 'number', min: '1', max: '14', placeholder: '4' }));
        break;
      case 'logs':
        fields.push(select_('Keep logs', b.retention_days, LOG_DAYS.map(d => [d, d + ' days']), v => set({ retention_days: Number(v) })));
        break;
      case 'database': {
        const cb = h('input', { id: 'f-multi-az', type: 'checkbox', class: 'size-4', checked: !!b.multi_az });
        cb.addEventListener('change', () => set({ multi_az: cb.checked }));
        fields.push(h('div', { class: 'grid grid-cols-2 gap-2' },
          select_('Engine', b.engine, [['mysql', 'MySQL'], ['postgres', 'PostgreSQL']], v => set({ engine: v })),
          select_('Size', b.size, Object.entries(DB_SIZES).map(([k2, v]) => [k2, k2 + ' (' + v + ')']), v => set({ size: v }))),
        h('div', { class: 'grid grid-cols-2 gap-2' },
          input('Storage (GB)', b.storage_gb, v => set({ storage_gb: v }), { type: 'number', min: '20', max: '1000' }),
          input('Backups (days)', b.backup_days ?? '', v => set({ backup_days: v }), { type: 'number', min: '1', max: '35', placeholder: '7' })),
        h('label', { class: 'flex items-center gap-2 text-sm', for: 'f-multi-az' }, cb, 'Run in two zones'),
        h('p', { class: 'hint' }, 'Private and encrypted, protected from deletion. AWS creates the password and keeps it in Secrets Manager; linked apps get it when they start. Forgeline never sees it.'));
        break;
      }
      case 'cache':
        fields.push(h('div', { class: 'grid grid-cols-2 gap-2' },
          select_('Size', b.size, Object.entries(CACHE_SIZES).map(([k2, v]) => [k2, k2 + ' (' + v + ')']), v => set({ size: v })),
          input('Nodes', b.nodes ?? 1, v => set({ nodes: v === 1 ? null : v }), { type: 'number', min: '1', max: '3' })),
        h('p', { class: 'hint' }, 'Redis, private, encrypted in transit and at rest. Two or three nodes fail over on their own.'));
        break;
      case 'secret':
        fields.push(input('What it holds', b.description, v => set({ description: v }), { placeholder: 'the payments API key' }),
          h('p', { class: 'hint' }, 'Created empty. Set its value in AWS Secrets Manager after it is built; linked apps get it when they start. Forgeline never reads or stores it.'));
        break;
      case 'existing':
        fields.push(select_('What it is', b.existing?.type, Object.entries(EXISTING), v => set({ existing: { ...b.existing, type: v } })),
          input('Its name in AWS', b.existing?.name, v => set({ existing: { ...b.existing, name: v } })),
          h('p', { class: 'hint' }, 'Used as it is. Forgeline never creates, changes or deletes it.'));
        break;
    }

    // Links from and to this block, in words, and the ones it may add.
    const mine = bp.links.filter(l => l.from === b.id || l.to === b.id);
    const targets = linkTargets(bp, b.id);
    let linkUI = null;
    if (targets.length) {
      const to = h('select', { id: 'f-link-to', class: 'input', 'aria-label': 'Link to' }, targets.map(t => h('option', { value: t.id }, t.name)));
      const access = h('select', { id: 'f-link-access', class: 'input w-auto', 'aria-label': 'Access' }, h('option', { value: 'readwrite' }, 'reads and writes'), h('option', { value: 'read' }, 'reads'));
      const path = h('input', { id: 'f-link-path', class: 'input w-28', 'aria-label': 'Path', placeholder: '/api/*' });
      const sync = () => { const t = targets.find(x => x.id === to.value); access.hidden = !t?.access; path.hidden = !t?.path; };
      to.addEventListener('change', sync); sync();
      linkUI = h('div', { class: 'mt-2 flex flex-wrap gap-2' }, to, access, path,
        h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'f-link-add', onclick: () => {
          const t = targets.find(x => x.id === to.value);
          change(addLink(bp, b.id, to.value, t?.access ? access.value : '', t?.path ? path.value.trim() : ''));
          drawInspector();
        } }, icon('link', 'size-3.5'), 'Link'));
    }
    mount(inspector,
      h('div', { class: 'mb-3 flex items-center justify-between gap-2' }, h('h2', { class: 'h-section' }, k.label + ' · ' + k.tech),
        h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Close', onclick: () => select('') }, icon('x', 'size-4'))),
      h('div', { class: 'space-y-3' }, fields),
      h('div', { class: 'mt-5 border-t border-slate-200 pt-4 dark:border-slate-800' }, h('h3', { class: 'h-section mb-2' }, 'Links'),
        mine.length ? h('ul', { class: 'space-y-1 text-sm', id: 'f-links' }, mine.map(l => h('li', { class: 'flex items-center justify-between gap-2' }, linkWords(bp, l),
          h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Remove the link: ' + linkWords(bp, l), onclick: () => { change(removeLink(bp, l.from, l.to)); drawInspector(); } }, icon('x', 'size-3.5'))))) : h('p', { class: 'muted text-xs' }, 'No links yet.'),
        linkUI),
      h('div', { class: 'mt-5 flex justify-end' }, h('button', { class: 'btn-ghost btn-sm text-rose-600 dark:text-rose-400', type: 'button', id: 'f-remove', onclick: () => { const next = removeBlock(bp, b.id); selected = ''; change(next); drawInspector(); } }, icon('trash', 'size-3.5'), 'Remove block')));
  }

  mount(el,
    pageHeader(saved.name, 'Changed by ' + saved.updated_by + ' ' + relTime(saved.updated_at) + '. Drag blocks to arrange them; pick one to change it.'),
    bar,
    h('div', { class: 'grid gap-4 xl:grid-cols-[minmax(0,1fr)_18rem]' },
      h('div', { class: 'min-w-0 space-y-4' }, h('section', { class: 'card overflow-hidden' }, tools, h('div', { class: 'p-2' }, canvasBox)), side),
      h('div', { class: 'space-y-4 self-start' }, helper, inspector)),
    h('p', { class: 'hint mt-3' }, 'Orange lines carry traffic from the internet; grey lines are an app using data; dashed lines are logs; moving dots show which way things flow (as configured, not measured). Dashed blocks already exist and are kept as they are. After a preview, each block says whether it would be added, changed, replaced or removed.'));
  drawBar(); drawTools(); drawSide(); draw(); drawInspector(); drawHelper(); poll();
  return el;
}
