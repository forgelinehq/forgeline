// The cards one answer prepared are shown one at a time, and the assistant hears back once, after the last of them.
// Sending an answer after each card made it carry on while the others were still waiting.

const MAX_MESSAGE = 3900; // the server takes 4000 characters
const END = ' Carry on.';

/** answers holds what the person decided, by card id; unsent is what the assistant has not been told yet. */
export function newQueue() { return { answers: new Map(), unsent: [] }; }

/**
 * What to draw: the cards already decided (or done by autopilot), the one to decide now, and the titles still to come.
 * Position and total count only the cards a person decides.
 */
export function queueView(actions, queue) {
  const mine = actions.filter(a => !a.done);
  const waiting = mine.filter(a => !queue.answers.has(a.id));
  const current = waiting[0] || null;
  const shown = actions.filter(a => a.done || queue.answers.has(a.id) || a === current);
  return { shown, current, next: waiting.slice(1).map(a => a.title || 'Waiting for you'), position: mine.length - waiting.length + 1, total: mine.length };
}

/** Records a decision once: verb is confirm, dismiss or failed, and text is the result or the error. */
export function decide(queue, action, verb, text) {
  if (queue.answers.has(action.id)) return;
  queue.answers.set(action.id, verb);
  queue.unsent.push({ title: action.title || 'that', verb, text: String(text || '') });
}

/**
 * The message to send the assistant, or '' while it is still answering or a card is still waiting. It empties what it
 * returns, so each decision is told once.
 */
export function takeReply(queue, actions, running) {
  if (running || !queue.unsent.length || queueView(actions, queue).current) return '';
  const room = Math.floor((MAX_MESSAGE - END.length) / queue.unsent.length);
  const parts = queue.unsent.map(({ title, verb, text }) => {
    const said = verb === 'confirm' ? 'I confirmed "' + title + '". ' + text
      : verb === 'dismiss' ? 'I dismissed "' + title + '", so go on without it.'
        : 'Confirming "' + title + '" failed: ' + text;
    return said.trim().slice(0, room - 1);
  });
  queue.unsent = [];
  return parts.join(' ').slice(0, MAX_MESSAGE - END.length) + END;
}
