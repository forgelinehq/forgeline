// Forgeline web console: dots moving along a line in a diagram, to show which way something flows.
//
// One line, drawn on top of the plain line it follows. It never claims measured traffic: callers draw it for what the
// configuration says flows (a front door's traffic, an enabled event source, a step that is running). People who ask
// their system for less motion see still dots (motion-safe).
import { h } from './dom.js';

/** A path of moving dots along `d`, forward (start to end) or back. `cls` sets its colour (a stroke-* class). */
export function flowPath(d, cls, { back = false, width = 5 } = {}) {
  return h('path', {
    d, fill: 'none', 'stroke-width': String(width), 'stroke-linecap': 'round', 'stroke-dasharray': '0.1 11.9', 'aria-hidden': 'true',
    class: 'pointer-events-none ' + cls + ' ' + (back ? 'motion-safe:animate-flow-back' : 'motion-safe:animate-flow'),
    'data-flow': back ? 'back' : 'forward',
  });
}

/** A thin bar with a short piece sliding across it: this stage is being worked on now. */
export function workingBar(cls = 'bg-sky-500') {
  return h('span', { class: 'mt-2 block h-1 w-full overflow-hidden rounded-full bg-sky-100 dark:bg-sky-500/20', role: 'progressbar', 'aria-label': 'working' },
    h('span', { class: 'block h-full w-1/3 rounded-full motion-safe:animate-slide ' + cls }));
}

/** A small pulsing dot: this is waiting for a person. */
export function waitingDot() {
  return h('span', { class: 'mr-1 inline-block size-2 rounded-full bg-brand-500 align-middle motion-safe:animate-pulse-dot', 'aria-hidden': 'true' });
}
