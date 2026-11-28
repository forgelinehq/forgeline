// Forgeline web console: how the assistant works on the next message, as other coding agents offer it. Pure, so it runs
// under Node in tests.

export const MODES = [
  { id: 'ask', label: 'Ask', title: 'Ask before changing anything' },
  { id: 'edits', label: 'Accept edits', title: 'File changes under /work in the sandbox run without asking; commands and other changes still ask' },
  { id: 'plan', label: 'Plan only', title: 'Look and plan; nothing is changed until you have the plan carried out' },
];

/** What a message tells the server about the mode. Autopilot has its own switch and takes the place of the others. */
export function modeFields(mode, apOn, limits) {
  if (apOn) return { autopilot: limits };
  return mode === 'edits' || mode === 'plan' ? { mode } : {};
}

/** What to offer under an answer: only a finished plan can be carried out, asking as before or accepting file changes. */
export function carryOutChoices(sentMode, status) {
  if (sentMode !== 'plan' || status !== 'finished') return [];
  return [{ mode: 'ask', label: 'Carry out, asking as I go' }, { mode: 'edits', label: 'Carry out, accepting file changes' }];
}

export const CARRY_OUT = 'Carry out the plan above.';
