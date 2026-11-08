// Forgeline web console: what needs the person on Home (design note 029).
//
// No imports and no DOM, so it runs under Node in tests. It only reorders data the console already
// reads; it never decides who may approve. The server still refuses an approver who proposed the
// change and anyone below administrator, so an item here is a pointer, not a permission.

const SEVERITY_RANK = { critical: 1, high: 2 };

/**
 * One list of things that need attention, most urgent first.
 * src: { proposals, changes, findings, scans, overview, integrations } as the API returns them; any may be
 * missing (the person's role cannot read it, or the read failed).
 * me: { name, role } of the person looking.
 * Each item: { key, rank, tone: 'act'|'bad'|'warn'|'wait', title, detail, href, at }.
 * rank 0 is "you can act now"; higher numbers are less urgent.
 */
export function buildInbox(src, me) {
  const items = [];
  const admin = me && me.role === 'admin';
  const waiting = (kind, list, hrefFor) => {
    for (const p of list || []) {
      if (p.status !== 'proposed') continue;
      const mine = p.proposed_by && me && p.proposed_by === me.name;
      const where = p.account_name ? ' · ' + p.account_name : '';
      if (admin && !mine) {
        items.push({ key: kind + ':' + p.id, rank: 0, tone: 'act', title: 'Approve: ' + p.title,
          detail: 'Proposed by ' + (p.proposed_by || 'someone') + where + (p.risk ? ' · ' + p.risk + ' risk' : ''), href: hrefFor(p), at: p.proposed_at });
      } else {
        items.push({ key: kind + ':' + p.id, rank: 3, tone: 'wait', title: 'Waiting for approval: ' + p.title,
          detail: (mine ? 'You proposed it; a different administrator must approve' : 'Proposed by ' + (p.proposed_by || 'someone')) + where,
          href: hrefFor(p), at: p.proposed_at });
      }
    }
  };
  waiting('proposal', src.proposals, p => '/proposals/' + encodeURIComponent(p.id));
  waiting('change', src.changes, () => '/infrastructure?tab=changes');

  for (const p of (src.overview && src.overview.pipelines) || []) {
    const r = p.latest;
    if (!r || r.status !== 'failed') continue;
    items.push({ key: 'run:' + r.id, rank: 1, tone: 'bad', title: p.name + ' failed',
      detail: 'Last run ' + (r.triggered_by ? 'started by ' + r.triggered_by : 'failed') + (p.success_rate != null ? ' · ' + Math.round(p.success_rate * 100) + '% of recent runs passed' : ''),
      href: '/runs/' + encodeURIComponent(r.id), at: r.created_at });
  }

  for (const f of (src.findings && src.findings.findings) || []) {
    if (f.status !== 'open' || !SEVERITY_RANK[f.severity]) continue;
    items.push({ key: 'finding:' + f.id, rank: SEVERITY_RANK[f.severity], tone: f.severity === 'critical' ? 'bad' : 'warn',
      title: f.title, detail: [f.severity, f.name || f.resource, f.account_name].filter(Boolean).join(' · '),
      href: '/guard', at: f.first_seen });
  }

  for (const i of (src.integrations && src.integrations.integrations) || []) {
    if (i.status !== 'attention') continue;
    items.push({ key: 'integration:' + i.id, rank: 1, tone: 'warn', title: i.name + ' needs attention',
      detail: i.summary || 'Open it to see what to do', href: '/integrations/' + encodeURIComponent(i.id), at: i.tested_at || null });
  }

  // Most urgent first; within a rank, newest first; the key keeps the order stable.
  return items.sort((a, b) => a.rank - b.rank || String(b.at || '').localeCompare(String(a.at || '')) || a.key.localeCompare(b.key));
}

/**
 * The numbers above the inbox. A value of null means "cannot say" (not readable for this role),
 * and the tile is left out rather than shown as zero.
 */
export function pulse(src, inbox) {
  const pipes = (src.overview && src.overview.pipelines) || null;
  const ran = pipes ? pipes.filter(p => p.latest) : [];
  const healthy = ran.filter(p => p.latest.status === 'succeeded').length;
  const findings = src.findings ? (src.findings.findings || []).filter(f => f.status === 'open') : null;
  const serious = findings ? findings.filter(f => SEVERITY_RANK[f.severity]).length : null;
  // No finished scan means "not checked", which must not read as "no problems".
  const scans = src.scans ? (src.scans.scans || []).filter(x => x.status === 'done') : null;
  const ints = src.integrations ? (src.integrations.integrations || []) : null;
  return {
    act: inbox.filter(i => i.rank === 0).length,
    waiting: inbox.filter(i => i.tone === 'wait').length,
    pipelines: pipes ? { healthy, ran: ran.length, total: pipes.length } : null,
    security: findings ? { serious, open: findings.length, scanned: scans ? scans.length > 0 : null, last: scans && scans.length ? scans.map(x => x.finished_at).sort().pop() : null } : null,
    connections: ints ? { connected: ints.filter(i => i.status === 'connected').length, attention: ints.filter(i => i.status === 'attention').length } : null,
  };
}

/** Home's greeting: by the hour of the person's own clock, with the first word of their name. */
export function greeting(hour, name) {
  const part = hour < 5 ? 'Hello' : hour < 12 ? 'Good morning' : hour < 18 ? 'Good afternoon' : 'Good evening';
  const first = String(name || '').trim().split(/\s+/)[0];
  return first ? part + ', ' + first.charAt(0).toUpperCase() + first.slice(1) : part;
}

/** The line under the greeting, from the inbox. */
export function summaryLine(inbox) {
  const act = inbox.filter(i => i.rank === 0).length;
  const problems = inbox.filter(i => i.rank > 0 && i.tone !== 'wait').length;
  const n = act + problems;
  if (!n) return 'Nothing needs you. Everything is quiet.';
  const words = ['', 'One thing needs', 'Two things need', 'Three things need', 'Four things need', 'Five things need'];
  return (words[n] || n + ' things need') + ' you.' + (n <= 3 ? ' Everything else is quiet.' : '');
}

const DONE = { applied: 'ok', succeeded: 'ok', rejected: 'bad', failed: 'bad', canceled: 'idle', cancelled: 'idle', expired: 'idle' };

/**
 * Changes that were decided recently, newest first: AI proposals and console cloud changes.
 * Each: { key, title, detail, tone: 'ok'|'bad'|'idle', at, href }.
 */
export function recentChanges(src, n = 4) {
  const out = [];
  const add = (kind, list, hrefFor) => {
    for (const p of list || []) {
      const tone = DONE[p.status];
      if (!tone) continue;
      const at = p.finished_at || p.decided_at || p.proposed_at || null;
      const who = [p.proposed_by && 'proposed by ' + p.proposed_by, p.decided_by && (p.status === 'rejected' ? 'rejected by ' : 'approved by ') + p.decided_by].filter(Boolean).join(' · ');
      out.push({ key: kind + ':' + p.id, title: p.title, detail: [p.status, p.account_name, who].filter(Boolean).join(' · '), tone, at, href: hrefFor(p) });
    }
  };
  add('proposal', src.proposals, p => '/proposals/' + encodeURIComponent(p.id));
  add('change', src.changes, () => '/infrastructure?tab=changes');
  return out.sort((a, b) => String(b.at || '').localeCompare(String(a.at || '')) || a.key.localeCompare(b.key)).slice(0, n);
}
