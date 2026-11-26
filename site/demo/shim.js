// The demo console's stand-in for the server. It replaces fetch before the console starts, answers reads from
// fixtures.json and refuses changes. Plain script, no dependencies; it never ships in the server binary.
(function () {
  const realFetch = window.fetch.bind(window);
  const ISO = /\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?Z/g;
  window.__demoMisses = [];
  let loaded = null;
  function fixtures() {
    if (!loaded) loaded = realFetch('./fixtures.json').then(r => r.json());
    return loaded;
  }
  function json(status, text) {
    return new Response(text, { status, headers: { 'Content-Type': 'application/json' } });
  }
  // Step logs arrive over Server-Sent Events; the recorded events are replayed, then the stream ends.
  window.EventSource = function (address) {
    const path = new URL(address, location.href).pathname;
    const listeners = {};
    const es = {
      onopen: null, onmessage: null, onerror: null, readyState: 0,
      addEventListener(name, fn) { (listeners[name] = listeners[name] || []).push(fn); },
      close() { es.readyState = 2; },
    };
    fixtures().then((fx) => {
      const events = fx.streams && fx.streams[path];
      if (!events) { window.__demoMisses.push(path); if (es.onerror) es.onerror({}); return; }
      es.readyState = 1;
      if (es.onopen) es.onopen({});
      for (const e of events) {
        if (es.readyState === 2) return;
        const ev = { data: e.data };
        if (e.event && e.event !== 'message') (listeners[e.event] || []).forEach((fn) => fn(ev));
        else if (es.onmessage) es.onmessage(ev);
      }
    });
    return es;
  };
  window.fetch = async function (input, init) {
    const url = new URL(typeof input === 'string' ? input : input.url, location.href);
    if (url.origin !== location.origin || !(url.pathname.startsWith('/v1/') || url.pathname === '/healthz')) {
      return realFetch(input, init);
    }
    const path = url.pathname + url.search;
    const method = ((init && init.method) || (typeof input !== 'string' && input.method) || 'GET').toUpperCase();
    const fx = await fixtures();
    if (url.pathname === '/healthz') return json(200, '{"status":"ok"}');
    if (method !== 'GET') {
      return json(403, JSON.stringify({ error: 'This is a demo of the console, so nothing can be changed here.' }));
    }
    const hit = fx.responses['GET ' + path] || fx.responses['GET ' + url.pathname];
    if (!hit) {
      window.__demoMisses.push(path);
      console.warn('demo: no data recorded for GET ' + path);
      return json(404, JSON.stringify({ error: 'The demo has no data for this.' }));
    }
    const shift = Date.now() - Date.parse(fx.recordedAt);
    const body = JSON.stringify(hit.body).replace(ISO, (t) => new Date(Date.parse(t) + shift).toISOString());
    return json(hit.status, body);
  };
})();
