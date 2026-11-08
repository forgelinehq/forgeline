package server

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
)

// putEvent writes an audit event at a chosen time, as audit() would have then.
func putEvent(t *testing.T, h *harness, at time.Time, actor, action, detail string) {
	t.Helper()
	ev := AuditEvent{ID: fmt.Sprintf("%020d-%s", at.UnixNano(), actor), Time: at.UTC(), Actor: actor, Action: action, Detail: detail, Allowed: true}
	if err := h.srv.docs.Put(t.Context(), kindAudit, ev.ID, ev); err != nil {
		t.Fatal(err)
	}
}

func TestAuditExportCSVAndJSONLines(t *testing.T) {
	h := newConsoleHarness(t)
	admin := h.adminBrowser("ada")
	admin.createUser("dev", authn.Developer)
	dev := h.login("dev")
	putEvent(t, h, time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), "grace", "secret.create", "=HYPERLINK(\"http://x\")")
	putEvent(t, h, time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC), "lin", "pipeline.run", "a, \"quoted\" note")

	// Only administrators can export.
	dev.expect(403, "GET", "/v1/audit/export", nil)
	admin.expect(400, "GET", "/v1/audit/export?format=xml", nil)
	admin.expect(400, "GET", "/v1/audit/export?from=yesterday", nil)
	admin.expect(400, "GET", "/v1/audit/export?from=2026-10-03&to=2026-10-01", nil)

	resp, body := admin.do("GET", "/v1/audit/export?from=2026-10-01&to=2026-10-02", nil)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") || !strings.Contains(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("csv export: %d %v", resp.StatusCode, resp.Header)
	}
	rows, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
	if err != nil {
		t.Fatalf("not valid CSV: %v\n%s", err, body)
	}
	if len(rows) != 3 || rows[0][2] != "actor" || rows[1][2] != "grace" || rows[2][2] != "lin" {
		t.Fatalf("rows (oldest first, only the range): %q", rows)
	}
	if rows[1][6] != "'=HYPERLINK(\"http://x\")" {
		t.Errorf("a formula was not defused: %q", rows[1][6])
	}
	if rows[2][6] != "a, \"quoted\" note" {
		t.Errorf("quoting lost: %q", rows[2][6])
	}

	// The range is inclusive of "to"; filters are those of the audit page.
	_, body = admin.do("GET", "/v1/audit/export?format=jsonl&to=2026-10-01", nil)
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	var ev AuditEvent
	if len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &ev) != nil || ev.Actor != "grace" || ev.Detail != "=HYPERLINK(\"http://x\")" {
		t.Fatalf("jsonl up to 1 October: %q", lines)
	}
	_, body = admin.do("GET", "/v1/audit/export?format=jsonl&actor=lin", nil)
	if n := strings.Count(strings.TrimSpace(string(body)), "\n") + 1; n != 1 || !strings.Contains(string(body), "pipeline.run") {
		t.Errorf("actor filter: %s", body)
	}

	// Every export is in the log, with what was taken.
	if a := string(admin.expect(200, "GET", "/v1/audit?action=audit.export", nil)); strings.Count(a, `"audit.export"`) != 3 || !strings.Contains(a, "2 events as csv, 2026-10-01 to 2026-10-02") {
		t.Errorf("exports not audited: %s", a)
	}
	// The audit page takes the same range.
	if a := string(admin.expect(200, "GET", "/v1/audit?from=2026-10-02&to=2026-10-02", nil)); strings.Contains(a, "grace") || !strings.Contains(a, "lin") {
		t.Errorf("audit page range: %s", a)
	}
}

func TestCSVCellDefusesFormulas(t *testing.T) {
	for in, want := range map[string]string{"=1+1": "'=1+1", "+x": "'+x", "-2": "'-2", "@SUM": "'@SUM", "\tx": "'\tx", "ok": "ok", "": "", "a=b": "a=b"} {
		if got := csvCell(in); got != want {
			t.Errorf("csvCell(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAuditRetention(t *testing.T) {
	h := newConsoleHarness(t)
	admin := h.adminBrowser("ada")
	admin.createUser("dev", authn.Developer)
	dev := h.login("dev")
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	putEvent(t, h, now.AddDate(0, 0, -400), "old", "secret.read", "")
	putEvent(t, h, now.AddDate(0, 0, -100), "mid", "secret.read", "")
	putEvent(t, h, now.AddDate(0, 0, -10), "new", "secret.read", "")

	// Kept forever by default: pruning removes nothing.
	if n, err := h.srv.pruneAudit(t.Context(), now); n != 0 || err != nil {
		t.Fatalf("default retention pruned %d (%v)", n, err)
	}
	dev.expect(403, "PUT", "/v1/settings/audit", map[string]any{"retention_days": 90})
	dev.expect(403, "GET", "/v1/settings/audit", nil)
	for _, bad := range []any{map[string]any{}, map[string]any{"retention_days": 7}, map[string]any{"retention_days": 29}, map[string]any{"retention_days": 4000}, map[string]any{"retention_days": -1}} {
		admin.expect(400, "PUT", "/v1/settings/audit", bad)
	}
	admin.expect(200, "PUT", "/v1/settings/audit", map[string]any{"retention_days": 365})
	if n, _ := h.srv.pruneAudit(t.Context(), now); n != 1 {
		t.Fatalf("365 days pruned %d, want 1", n)
	}
	admin.expect(200, "PUT", "/v1/settings/audit", map[string]any{"retention_days": 30})
	if n, _ := h.srv.pruneAudit(t.Context(), now); n != 1 {
		t.Fatalf("30 days pruned %d, want 1", n)
	}
	a := string(admin.expect(200, "GET", "/v1/audit?limit=1000", nil))
	if strings.Contains(a, `"old"`) || strings.Contains(a, `"mid"`) || !strings.Contains(a, `"new"`) {
		t.Errorf("after pruning: %s", a)
	}
	// Each prune and each change of the period is recorded, and survives the prune.
	if !strings.Contains(a, "audit.pruned") || !strings.Contains(a, "keep forever → 365 days") || !strings.Contains(a, "keep 365 days → 30 days") {
		t.Errorf("prunes or changes not audited: %s", a)
	}
	var st auditSettingsJSON
	json.Unmarshal(admin.expect(200, "GET", "/v1/settings/audit", nil), &st)
	if st.RetentionDays != 30 || st.LastPruned != 1 || st.Count == 0 || st.Oldest == nil {
		t.Errorf("settings: %+v", st)
	}

	// A stored period below the minimum (written by hand) still keeps 30 days; unreadable settings prune nothing.
	h.srv.docs.Put(t.Context(), kindSettings, idAuditSettings, auditSettings{RetentionDays: 1})
	if n, _ := h.srv.pruneAudit(t.Context(), now); n != 0 {
		t.Errorf("a 1-day period pruned %d events younger than 30 days", n)
	}
	h.srv.docs.Put(t.Context(), kindSettings, idAuditSettings, "not settings")
	if n, err := h.srv.pruneAudit(t.Context(), now.AddDate(10, 0, 0)); n != 0 || err == nil {
		t.Errorf("unreadable settings: pruned %d, err %v", n, err)
	}
}
