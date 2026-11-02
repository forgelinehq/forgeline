// Forgeline web console: sizes and positions for the assistant panel.
//
// No imports and no DOM, so the rules run under Node in tests. The panel can be docked at the
// right edge (a width) or float (a rectangle). Whatever was saved, or typed by a drag, is made to
// fit the screen it is shown on, so the panel can never end up off screen or unusably small.

export const MIN_DOCK = 320;
export const DEFAULT_DOCK = 520;
export const MIN_FLOAT_W = 320;
export const MIN_FLOAT_H = 280;

const clamp = (v, lo, hi) => Math.min(Math.max(v, lo), hi);
const num = (v, fallback) => (typeof v === 'number' && Number.isFinite(v) ? v : fallback);

/** A docked width: at least 320 px, at most 70% of the window and never more than 1100 px. */
export function dockWidth(w, vw) {
  const max = Math.max(MIN_DOCK, Math.min(Math.round(vw * 0.7), 1100));
  return Math.round(clamp(num(w, DEFAULT_DOCK), MIN_DOCK, max));
}

/** The "wide" size: half the window, within the same limits. */
export function wideDockWidth(vw) { return dockWidth(Math.round(vw * 0.5), vw); }

/** A floating rectangle {x, y, w, h} that lies fully inside the window. Missing values get defaults. */
export function floatRect(r, vw, vh) {
  const w = Math.round(clamp(num(r && r.w, 560), MIN_FLOAT_W, Math.max(MIN_FLOAT_W, vw - 16)));
  const h = Math.round(clamp(num(r && r.h, 680), MIN_FLOAT_H, Math.max(MIN_FLOAT_H, vh - 16)));
  const x = Math.round(clamp(num(r && r.x, vw - w - 24), 8, Math.max(8, vw - w - 8)));
  const y = Math.round(clamp(num(r && r.y, 64), 8, Math.max(8, vh - h - 8)));
  return { x, y, w, h };
}

/** Saved preferences from untrusted storage text: only known fields, only the right types. */
export function readPrefs(text) {
  let o = {};
  try { o = JSON.parse(text || '{}') || {}; } catch { o = {}; }
  const p = { mode: o.mode === 'float' ? 'float' : 'dock', dockW: num(o.dockW, DEFAULT_DOCK), float: {} };
  for (const k of ['x', 'y', 'w', 'h']) if (typeof (o.float || {})[k] === 'number') p.float[k] = o.float[k];
  return p;
}
