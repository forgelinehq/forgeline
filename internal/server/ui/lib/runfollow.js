// A card that started a run follows it until it ends, waits for a person, or has taken long enough, and only then is the
// assistant told. So the assistant neither waits in a loop nor guesses.

export const RUN_KINDS = new Set(['run_pipeline', 'run_workflow', 'run_team']);

const EVERY = 3000;
const PATIENCE = 10 * 60 * 1000;
const MAX_FAILURES = 5;
const OVER = new Set(['succeeded', 'failed', 'cancelled']);

/**
 * Reads the run until there is nothing more to wait for. read() answers the run's view; show(view) draws it; settled(state)
 * is called once, with succeeded, failed, cancelled, waiting, running (still going after a long time) or unknown. A
 * run that waits for a person, or runs long, is still followed after that, so the card stays true. alive() says whether
 * the card is still on the page.
 */
export async function followRun({ read, show, settled, alive, sleep, now = Date.now }) {
  const began = now();
  let told = false, failures = 0;
  const tell = (state) => { if (!told) { told = true; settled(state); } };
  for (;;) {
    try {
      const view = await read();
      failures = 0;
      show(view);
      if (OVER.has(view.status)) return tell(view.status);
      if (view.status === 'waiting') tell('waiting');
      else if (now() - began >= PATIENCE) tell('running');
    } catch (err) {
      failures++;
      if ((err.status >= 400 && err.status < 500) || failures >= MAX_FAILURES) return tell('unknown');
    }
    await sleep(EVERY);
    if (!alive()) return;
  }
}

/** What the assistant is told: how it went, never what the run printed, which is not the person's own words. */
export function outcomeWords(state) {
  return {
    succeeded: 'It finished and succeeded. Read what it produced and tell me.',
    failed: 'It finished and failed. Read why and tell me.',
    cancelled: 'It was cancelled.',
    waiting: 'It is waiting for a person to answer or approve.',
    running: 'It is still running.',
    unknown: 'I could not follow how it went.',
  }[state] || 'I could not follow how it went.';
}
