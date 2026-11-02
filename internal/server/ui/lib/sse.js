// Forgeline web console: reads a server-sent event stream that was fetched with POST (the browser's
// EventSource can only GET). No imports and no DOM, so it runs under Node in tests.

/**
 * An incremental parser. push(text) takes whatever arrived (a frame may be split across pushes) and
 * calls onEvent(object) for each complete frame whose data is JSON. Comment lines (keep-alives) and
 * frames that are not JSON are ignored; they must never stop the stream.
 */
export function sseParser(onEvent) {
  let buf = '';
  return {
    push(text) {
      buf += String(text).replace(/\r\n?/g, '\n');
      let i;
      while ((i = buf.indexOf('\n\n')) >= 0) {
        const frame = buf.slice(0, i);
        buf = buf.slice(i + 2);
        const data = frame.split('\n').filter(l => l.startsWith('data:')).map(l => l.slice(5).replace(/^ /, '')).join('\n');
        if (!data) continue;
        let ev;
        try { ev = JSON.parse(data); } catch { continue; }
        if (ev && typeof ev === 'object' && !Array.isArray(ev)) onEvent(ev);
      }
    },
  };
}
