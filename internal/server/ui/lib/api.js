// Forgeline web console: API client.
import { sseParser } from './sse.js';

export class ApiError extends Error {
  constructor(status, message, body) {
    super(message);
    this.status = status;
    this.body = body || {};
    this.problems = (body && body.problems) || [];
  }
}

const listeners = new Set();
/** Called with no arguments when the server says the session ended. */
export function onSignedOut(fn) { listeners.add(fn); return () => listeners.delete(fn); }

/**
 * Calls the API. With a signal (usually pageSignal()), a request the person no longer waits for is aborted and its
 * promise never settles, so the code of a page that was left never runs on with it (and never shows an error).
 */
export async function api(method, path, body, signal) {
  const init = { method, headers: {}, credentials: 'same-origin', signal };
  if (method !== 'GET' && method !== 'HEAD') init.headers['X-Forgeline-Request'] = '1';
  if (body !== undefined) {
    init.headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  let resp;
  try {
    resp = await fetch(path, init);
  } catch (e) {
    if (signal && signal.aborted) return new Promise(() => {});
    throw new ApiError(0, 'Cannot reach the server. Check that it is running and try again.');
  }
  if (resp.status === 204) return null;
  let text;
  try { text = await resp.text(); } catch (e) {
    if (signal && signal.aborted) return new Promise(() => {});
    throw new ApiError(0, 'The connection to the server broke. Try again.');
  }
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { /* not JSON */ }
  if (!resp.ok) {
    if (resp.status === 401 && !path.startsWith('/v1/auth/')) listeners.forEach(fn => fn());
    throw new ApiError(resp.status, (data && data.error) || resp.status + ' ' + resp.statusText, data);
  }
  return data;
}

/**
 * POST and read the answer as a server-sent event stream, calling onEvent for each event. Checks that
 * fail before the stream starts come back as ordinary JSON errors and are thrown as ApiError, the
 * same as api(). Aborting through `signal` throws an AbortError.
 */
export async function streamPost(path, body, onEvent, signal) {
  let resp;
  try {
    resp = await fetch(path, { method: 'POST', credentials: 'same-origin', signal, body: JSON.stringify(body === undefined ? {} : body),
      headers: { 'X-Forgeline-Request': '1', 'Content-Type': 'application/json', Accept: 'text/event-stream' } });
  } catch (e) {
    if (e && e.name === 'AbortError') throw e;
    throw new ApiError(0, 'Cannot reach the server. Check that it is running and try again.');
  }
  if (!resp.ok) {
    let data = null;
    try { data = JSON.parse(await resp.text()); } catch { /* not JSON */ }
    if (resp.status === 401) listeners.forEach(fn => fn());
    throw new ApiError(resp.status, (data && data.error) || resp.status + ' ' + resp.statusText, data);
  }
  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  const parser = sseParser(onEvent);
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    parser.push(decoder.decode(value, { stream: true }));
  }
}

export const get = (p, opts) => api('GET', p, undefined, opts && opts.signal);
export const post = (p, b) => api('POST', p, b === undefined ? {} : b);
export const put = (p, b) => api('PUT', p, b);
export const patch = (p, b) => api('PATCH', p, b);
export const del = (p) => api('DELETE', p);

/** Whether a role meets a minimum. Mirrors the server, which is the real check. */
export function atLeast(role, min) {
  const rank = { viewer: 1, developer: 2, admin: 3 };
  return (rank[role] || 0) >= (rank[min] || 99);
}
