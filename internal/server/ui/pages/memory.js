// What the assistant remembers (design note 027): short notes about you and about the system, and your
// conversation history. The person sees everything that is kept, and can edit or delete all of it.
import { h, icon, mount, pageHeader, relTime, spinner, errorBox, toast, confirmDialog, notice, emptyState } from '../lib/dom.js';
import { get, post, put, del } from '../lib/api.js';
import { session } from '../app.js';

const MAX = 300;

export async function memoryPage() {
  const el = h('div', { class: 'max-w-3xl space-y-6' });
  mount(el, spinner());

  async function load() {
    let m, chats, ff = { files: [] };
    try { [m, chats] = await Promise.all([get('/v1/assistant/memory'), get('/v1/assistant/conversations')]); } catch (e) { return mount(el, errorBox(e.message)); }
    try { ff = await get('/v1/assistant/memory/files'); } catch { /* the notes still work without files */ }
    const isAdmin = session.role === 'admin';
    const act = async (fn, ok) => { try { await fn(); if (ok) toast(ok, 'ok'); load(); } catch (e) { toast(e.message, 'error'); } };

    const noteRow = (n, canEdit) => {
      const pending = n.status === 'pending';
      return h('li', { class: 'flex flex-wrap items-start gap-2 px-4 py-3', 'data-note': n.id, 'data-status': n.status },
        h('div', { class: 'min-w-0 flex-1' },
          h('p', { class: 'break-words text-sm' }, n.text),
          h('p', { class: 'muted mt-0.5 text-xs' },
            (n.by === 'assistant' ? 'Noted by the assistant' : 'Written by ' + (n.owner_name || 'a person')) + ' · ' + relTime(n.created),
            n.uses ? ' · helped ' + n.uses + (n.uses === 1 ? ' time' : ' times') : '',
            pending && n.why ? ' · ' + n.why : '')),
        pending ? h('span', { class: 'pill-wait' }, 'Waiting') : null,
        h('div', { class: 'flex gap-1' },
          pending && canEdit ? h('button', { class: 'btn-primary btn-sm', type: 'button', 'data-act': 'approve', onclick: () => act(() => post('/v1/assistant/memory/' + n.id + '/approve'), 'Kept') }, 'Keep') : null,
          canEdit ? h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Edit this note', onclick: () => edit(n) }, icon('edit', 'size-4')) : null,
          canEdit || (pending && n.owner_name === session.name) ? h('button', { class: 'btn-ghost btn-sm text-rose-600 dark:text-rose-400', type: 'button', 'data-act': 'delete', 'aria-label': 'Delete this note',
            onclick: () => act(() => del('/v1/assistant/memory/' + n.id), 'Deleted') }, icon('trash', 'size-4')) : null));
    };

    function edit(n) {
      const row = el.querySelector('[data-note="' + n.id + '"]');
      const ta = h('textarea', { class: 'input w-full', rows: '2', maxlength: String(MAX), 'aria-label': 'Note text' }, n.text);
      mount(row, h('div', { class: 'w-full space-y-2' }, ta, h('div', { class: 'flex gap-2' },
        h('button', { class: 'btn-primary btn-sm', type: 'button', onclick: () => act(() => put('/v1/assistant/memory/' + n.id, { text: ta.value }), 'Saved') }, 'Save'),
        h('button', { class: 'btn-secondary btn-sm', type: 'button', onclick: load }, 'Cancel'))));
      ta.focus();
    }

    const fileRow = (f) => {
      const body = h('pre', { class: 'mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-slate-50 p-3 font-mono text-xs dark:bg-slate-900' }, f.content);
      const editBtn = h('button', { class: 'btn-ghost btn-sm', type: 'button', 'aria-label': 'Edit ' + f.path, onclick: () => {
        const ta = h('textarea', { class: 'input w-full font-mono text-xs', rows: '8', maxlength: '8000', 'aria-label': 'File text' }, f.content);
        mount(box, h('div', { class: 'space-y-2' }, h('code', { class: 'text-xs' }, f.path), ta, h('div', { class: 'flex gap-2' },
          h('button', { class: 'btn-primary btn-sm', type: 'button', onclick: () => act(() => put('/v1/assistant/memory/files', { path: f.path, content: ta.value }), 'Saved') }, 'Save'),
          h('button', { class: 'btn-secondary btn-sm', type: 'button', onclick: load }, 'Cancel'))));
      } }, icon('edit', 'size-4'));
      const box = h('li', { class: 'px-4 py-3', 'data-memfile': f.path, 'data-pending': String(!!f.pending) },
        h('div', { class: 'flex flex-wrap items-center gap-2' },
          h('code', { class: 'w-full break-all text-xs' }, f.path),
          f.pending ? h('span', { class: 'pill-wait' }, 'Waiting') : null,
          h('span', { class: 'muted text-xs' }, (f.by === 'assistant' ? 'Written by the assistant' : 'Written by a person') + ' · ' + relTime(f.updated)),
          f.pending ? h('button', { class: 'btn-primary btn-sm', type: 'button', 'data-act': 'keep', onclick: () => act(() => put('/v1/assistant/memory/files', { path: f.path, keep: true }), 'Kept') }, 'Keep') : null,
          f.can_edit ? editBtn : null,
          f.can_edit ? h('button', { class: 'btn-ghost btn-sm text-rose-600 dark:text-rose-400', type: 'button', 'data-act': 'delete-file', 'aria-label': 'Delete ' + f.path, onclick: () => act(() => del('/v1/assistant/memory/files?path=' + encodeURIComponent(f.path)), 'Deleted') }, icon('trash', 'size-4')) : null),
        f.pending ? h('p', { class: 'muted mt-1 text-xs' }, 'The assistant wrote this after reading data you did not type, so it stays out of its notes until you keep it.') : null,
        h('details', { class: 'mt-1' }, h('summary', { class: 'cursor-pointer text-xs underline' }, 'Show the file'), body));
      return box;
    };
    const fileList = (scope, none) => { const fs = ff.files.filter(f => f.scope === scope); return fs.length ? h('ul', { class: 'divide-y divide-slate-200 dark:divide-slate-800' }, fs.map(fileRow)) : h('p', { class: 'muted px-4 py-6 text-sm' }, none); };

    const text = h('textarea', { id: 'note-text', class: 'input w-full', rows: '2', maxlength: String(MAX), placeholder: 'For example: I work in eu-west-1, or: we call account 1111 prod' });
    const about = h('select', { id: 'note-about', class: 'input w-auto', 'aria-label': 'What the note is about' },
      h('option', { value: 'me' }, 'About me'), h('option', { value: 'system' }, isAdmin ? 'About this system' : 'About this system (an administrator confirms)'));
    const add = h('form', { class: 'space-y-2', onsubmit: (e) => {
      e.preventDefault();
      act(async () => { await post('/v1/assistant/memory', { text: text.value, about: about.value }); }, 'Remembered');
    } }, text, h('div', { class: 'flex flex-wrap items-center gap-2' }, about, h('button', { class: 'btn-primary btn-sm', type: 'submit' }, icon('plus', 'size-3.5'), 'Remember'),
      h('span', { class: 'muted text-xs' }, 'Up to ' + MAX + ' characters. Never a password or a key.')));

    const list = (notes, canEdit, none) => notes.length
      ? h('ul', { class: 'divide-y divide-slate-200 dark:divide-slate-800' }, notes.map(n => noteRow(n, canEdit(n))))
      : h('p', { class: 'muted px-4 py-6 text-sm' }, none);

    mount(el,
      pageHeader('Memory', 'What the assistant remembers about you and this system, and your past conversations.',
        isAdmin ? h('label', { class: 'flex items-center gap-2 text-sm' },
          h('input', { type: 'checkbox', id: 'memory-on', checked: m.enabled, onchange: (e) => act(() => put('/v1/assistant/memory/settings', { enabled: e.target.checked }), e.target.checked ? 'Memory is on' : 'Memory is off') }),
          'Memory is on') : null),
      !m.enabled ? notice('warn', 'Memory is turned off. The assistant is not reading or writing notes. Your conversation history is still kept for you.') : null,
      notice('info', 'Notes are background for the assistant, never instructions: they cannot change what it is allowed to do. It only saves a note about you by itself when you said it in your own words; anything it learns while reading logs or cloud data waits here for you to keep or delete.'),
      h('section', { class: 'card', 'aria-labelledby': 'mem-me' },
        h('div', { class: 'border-b border-slate-200 px-4 py-3 dark:border-slate-800' }, h('h2', { id: 'mem-me', class: 'text-base font-semibold' }, 'About you'),
          h('p', { class: 'muted text-xs' }, 'Only you can see these. Delete anything at any time.')),
        list(m.mine, () => true, 'Nothing yet. Tell the assistant "remember that I work in eu-west-1", or add a note below.'),
        h('div', { class: 'border-t border-slate-200 p-4 dark:border-slate-800' }, add)),
      h('section', { class: 'card', 'aria-labelledby': 'mem-sys' },
        h('div', { class: 'border-b border-slate-200 px-4 py-3 dark:border-slate-800' }, h('h2', { id: 'mem-sys', class: 'text-base font-semibold' }, 'About this system'),
          h('p', { class: 'muted text-xs' }, 'Shared with everyone who uses the assistant. An administrator keeps or removes each one.')),
        list(m.system, () => isAdmin, 'Nothing yet.')),
      h('section', { class: 'card', 'aria-labelledby': 'mem-files', id: 'mem-files-card' },
        h('div', { class: 'border-b border-slate-200 px-4 py-3 dark:border-slate-800' }, h('h2', { id: 'mem-files', class: 'text-base font-semibold' }, 'Files the assistant keeps'),
          h('p', { class: 'muted text-xs' }, 'Longer things it learned: how an environment is laid out, a procedure that worked, how you like things done. Under /memories/me only you see them; under /memories/org everyone does, and only an administrator writes there.')),
        h('div', { class: 'px-4 pt-3 text-xs font-semibold uppercase tracking-wide muted' }, 'Yours'),
        fileList('me', 'None yet. Ask the assistant to remember how something works, and it saves a file here.'),
        h('div', { class: 'px-4 pt-3 text-xs font-semibold uppercase tracking-wide muted' }, 'Shared with everyone'),
        fileList('org', 'None yet.')),
      h('section', { class: 'card', 'aria-labelledby': 'mem-chats' },
        h('div', { class: 'flex flex-wrap items-center justify-between gap-2 border-b border-slate-200 px-4 py-3 dark:border-slate-800' },
          h('div', {}, h('h2', { id: 'mem-chats', class: 'text-base font-semibold' }, 'Your conversations'),
            h('p', { class: 'muted text-xs' }, 'Private to you, including from administrators. Kept up to 90 days, 100 at most. Open them from the clock in the assistant.')),
          chats.length ? h('button', { class: 'btn-secondary btn-sm', type: 'button', id: 'forget-chats', onclick: async () => {
            if (!await confirmDialog({ title: 'Delete all your conversations?', body: 'They are removed for good.', confirmLabel: 'Delete', danger: true })) return;
            act(() => del('/v1/assistant/conversations'), 'Deleted');
          } }, 'Delete all') : null),
        chats.length ? h('ul', { class: 'divide-y divide-slate-200 dark:divide-slate-800' }, chats.slice(0, 20).map(c =>
          h('li', { class: 'flex items-center gap-2 px-4 py-2.5 text-sm' },
            h('span', { class: 'min-w-0 flex-1 truncate' }, c.title || 'Untitled'),
            (c.refs || []).length ? h('span', { class: 'pill-idle' }, c.refs.length + (c.refs.length === 1 ? ' request filed' : ' requests filed')) : null,
            h('span', { class: 'muted shrink-0 text-xs' }, relTime(c.updated))))) : h('p', { class: 'muted px-4 py-6 text-sm' }, 'No conversations yet.')),
      h('div', { class: 'flex justify-end' }, m.mine.length || ff.files.some(f => f.scope === 'me') ? h('button', { class: 'btn-ghost btn-sm text-rose-600 dark:text-rose-400', type: 'button', id: 'forget-me', onclick: async () => {
        if (!await confirmDialog({ title: 'Forget everything about you?', body: 'Every note and file about you is deleted. Notes and files about the system are not touched.', confirmLabel: 'Forget', danger: true })) return;
        act(() => del('/v1/assistant/memory'), 'Forgotten');
      } }, icon('trash', 'size-4'), 'Forget everything about me') : null));
  }
  await load();
  return el;
}
