// Forgeline web console: the guardrails a plan broke (design note 040), on the canvas preview and the approval.
import { h, icon } from './dom.js';

/** The findings as a list: denies first in red, warnings in amber; a line saying none broke when the list is empty. */
/** Which rule sets checked the plan: the built-in version and each policy pack with its tool version. */
function checkedBy(version, packs) {
  const parts = [version ? 'built-in rules ' + version : null, ...(packs || []).map(p => `${p.name} (${p.tool} ${p.version}${p.skipped ? ', ' + p.skipped + ' skipped' : ''}${p.error ? ', did not run' : ''})`)].filter(Boolean);
  return parts.length ? h('p', { class: 'muted text-xs', id: 'guardrails-by' }, 'Checked by ' + parts.join('; ') + '.') : null;
}

export function guardrailList(findings, version, packs) {
  const fs = findings || [];
  const denies = fs.filter(f => f.level === 'deny').length;
  const head = denies
    ? h('p', { class: 'text-sm font-medium text-rose-700 dark:text-rose-400' }, `Breaks ${denies} guardrail${denies === 1 ? '' : 's'}: it cannot be sent for approval until the blueprint changes.`)
    : h('p', { class: 'text-sm' }, fs.length ? `Guardrails: nothing denied; ${fs.length} warning${fs.length === 1 ? '' : 's'} the approver will read.` : 'Guardrails: nothing to report.');
  return h('div', { class: 'my-2 space-y-1', id: 'guardrails', 'data-version': version || '' }, head,
    fs.length ? h('ul', { class: 'max-h-72 space-y-1 overflow-auto text-xs' }, fs.map(f => h('li', { class: 'flex items-start gap-2', 'data-rule': f.rule },
      h('span', { class: f.level === 'deny' ? 'text-rose-600 dark:text-rose-400' : 'text-amber-600 dark:text-amber-400' }, icon('alert', 'mt-0.5 size-3.5')),
      h('span', {}, h('code', { class: 'break-all' }, f.address), ' · ' + f.text)))) : null,
    checkedBy(version, packs));
}
