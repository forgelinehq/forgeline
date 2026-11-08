package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/docstore"
)

// Audit export and retention (design note 033). An administrator can download the audit log, or part of it,
// as CSV or JSON Lines for an auditor or a SIEM, and can choose how long it is kept. By default it is kept
// forever. A retention period is at least 30 days, pruning runs in the background, and every prune and every
// export is itself recorded in the log.

const (
	idAuditSettings    = "audit"
	minAuditRetention  = 30
	maxAuditRetention  = 3650
	auditPruneEvery    = time.Hour
	auditPruneBatch    = 500
	maxAuditExportRows = 200000
)

type auditSettings struct {
	RetentionDays int       `json:"retention_days"` // 0: keep forever
	UpdatedAt     time.Time `json:"updated_at,omitzero"`
	UpdatedBy     string    `json:"updated_by,omitempty"`
	LastPrune     time.Time `json:"last_prune,omitzero"`
	LastPruned    int       `json:"last_pruned,omitempty"`
}

func (s *Server) loadAuditSettings(ctx context.Context) (auditSettings, error) {
	var st auditSettings
	err := s.docs.Get(ctx, kindSettings, idAuditSettings, &st)
	if errors.Is(err, docstore.ErrNotFound) {
		return auditSettings{}, nil
	}
	return st, err
}

// auditFilter is what the audit page and the export share: a person, an action prefix, refused only, and a
// time range [from, to).
type auditFilter struct {
	actor, action string
	denied        bool
	from, to      time.Time
}

func parseAuditFilter(r *http.Request) (auditFilter, error) {
	q := r.URL.Query()
	f := auditFilter{actor: strings.ToLower(q.Get("actor")), action: q.Get("action"), denied: q.Get("denied") == "1"}
	day := func(name string) (time.Time, error) {
		v := q.Get(name)
		if v == "" {
			return time.Time{}, nil
		}
		t, err := time.Parse(time.DateOnly, v)
		if err != nil {
			return t, fmt.Errorf("%s must be a date like 2026-11-08", name)
		}
		return t, nil
	}
	var err error
	if f.from, err = day("from"); err != nil {
		return f, err
	}
	if f.to, err = day("to"); err != nil {
		return f, err
	}
	if !f.to.IsZero() {
		f.to = f.to.AddDate(0, 0, 1) // "to" is the last day included
	}
	if !f.from.IsZero() && !f.to.IsZero() && !f.from.Before(f.to) {
		return f, errors.New("from must not be after to")
	}
	return f, nil
}

func (f auditFilter) match(e AuditEvent) bool {
	switch {
	case f.actor != "" && strings.ToLower(e.Actor) != f.actor:
		return false
	case f.action != "" && !strings.HasPrefix(e.Action, f.action):
		return false
	case f.denied && e.Allowed:
		return false
	case !f.from.IsZero() && e.Time.Before(f.from):
		return false
	case !f.to.IsZero() && !e.Time.Before(f.to):
		return false
	}
	return true
}

// csvCell keeps a spreadsheet from running a cell as a formula: the log holds text people typed (names, notes,
// reasons), and a cell starting with = + - @ or a tab would be evaluated when an auditor opens the file.
func csvCell(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func (s *Server) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "jsonl" {
		writeError(w, http.StatusBadRequest, "format must be csv or jsonl")
		return
	}
	f, err := parseAuditFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	docs, err := s.docs.List(r.Context(), kindAudit, docstore.ListOptions{})
	if err != nil {
		s.internalError(w, "reading the audit log", err)
		return
	}
	events, err := docstore.Decode[AuditEvent](docs)
	if err != nil {
		s.internalError(w, "reading the audit log", err)
		return
	}
	out := make([]AuditEvent, 0, len(events))
	for _, e := range events { // oldest first: ids start with the time
		if f.match(e) {
			out = append(out, e)
		}
	}
	if len(out) > maxAuditExportRows {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("%d events match; export at most %d at a time by choosing a shorter range", len(out), maxAuditExportRows))
		return
	}
	// Record the export before handing the file out, so nobody can take a copy unseen.
	detail := fmt.Sprintf("%d events as %s", len(out), format)
	if q := r.URL.Query(); q.Get("from") != "" || q.Get("to") != "" {
		detail += ", " + q.Get("from") + " to " + q.Get("to")
	}
	if err := s.audit(r.Context(), principalFrom(r.Context()), clientIP(r), "audit.export", "audit", detail, true); err != nil {
		s.internalError(w, "recording the export", err)
		return
	}
	name := "forgeline-audit-" + time.Now().UTC().Format("2006-01-02")
	w.Header().Set("Cache-Control", "no-store")
	if format == "jsonl" {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.jsonl"`)
		enc := json.NewEncoder(w)
		for _, e := range out {
			enc.Encode(e)
		}
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.csv"`)
	cw := csv.NewWriter(w)
	cw.Write([]string{"id", "time", "actor", "role", "action", "target", "detail", "allowed", "ip"})
	for _, e := range out {
		cw.Write([]string{e.ID, e.Time.Format(time.RFC3339Nano), csvCell(e.Actor), e.Role, csvCell(e.Action), csvCell(e.Target), csvCell(e.Detail), strconv.FormatBool(e.Allowed), e.IP})
	}
	cw.Flush()
}

type auditSettingsJSON struct {
	auditSettings
	Oldest *time.Time `json:"oldest,omitempty"`
	Count  int        `json:"count"`
}

func (s *Server) handleGetAuditSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.loadAuditSettings(r.Context())
	if err != nil {
		s.internalError(w, "reading the audit settings", err)
		return
	}
	out := auditSettingsJSON{auditSettings: st}
	if docs, err := s.docs.List(r.Context(), kindAudit, docstore.ListOptions{}); err == nil {
		out.Count = len(docs)
		if ev, err := docstore.Decode[AuditEvent](docs[:min(1, len(docs))]); err == nil && len(ev) == 1 {
			out.Oldest = &ev[0].Time
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePutAuditSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RetentionDays *int `json:"retention_days"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.RetentionDays == nil {
		writeError(w, http.StatusBadRequest, "retention_days is required (0 keeps the log forever)")
		return
	}
	d := *req.RetentionDays
	if d != 0 && (d < minAuditRetention || d > maxAuditRetention) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("retention_days must be 0 (keep forever) or %d to %d", minAuditRetention, maxAuditRetention))
		return
	}
	st, err := s.loadAuditSettings(r.Context())
	if err != nil {
		s.internalError(w, "reading the audit settings", err)
		return
	}
	before := st.RetentionDays
	st.RetentionDays, st.UpdatedAt, st.UpdatedBy = d, time.Now().UTC(), principalFrom(r.Context()).Name
	if err := s.docs.Put(r.Context(), kindSettings, idAuditSettings, st); err != nil {
		s.internalError(w, "saving the audit settings", err)
		return
	}
	word := func(n int) string {
		if n == 0 {
			return "forever"
		}
		return strconv.Itoa(n) + " days"
	}
	s.auditReq(r, "audit.retention", "audit", "keep "+word(before)+" → "+word(d), true)
	writeJSON(w, http.StatusOK, st)
}

// pruneAudit deletes events older than the retention period. It never deletes anything when the log is kept
// forever, when the settings cannot be read, or anything younger than the minimum period, and it records
// what it removed.
func (s *Server) pruneAudit(ctx context.Context, now time.Time) (int, error) {
	st, err := s.loadAuditSettings(ctx)
	if err != nil {
		return 0, err
	}
	if st.RetentionDays == 0 {
		return 0, nil
	}
	cutoff := now.AddDate(0, 0, -max(st.RetentionDays, minAuditRetention))
	removed := 0
	for {
		docs, err := s.docs.List(ctx, kindAudit, docstore.ListOptions{Limit: auditPruneBatch})
		if err != nil {
			return removed, err
		}
		events, err := docstore.Decode[AuditEvent](docs)
		if err != nil {
			return removed, err
		}
		n := 0
		for _, e := range events {
			if !e.Time.Before(cutoff) {
				break
			}
			if err := s.docs.Delete(ctx, kindAudit, e.ID); err != nil {
				return removed, err
			}
			n++
		}
		removed += n
		if n < len(events) || len(events) < auditPruneBatch {
			break
		}
	}
	if removed > 0 {
		s.audit(ctx, Principal{Name: "forgeline"}, "", "audit.pruned", "audit",
			fmt.Sprintf("%d events older than %s removed (kept %d days)", removed, cutoff.Format(time.DateOnly), st.RetentionDays), true)
	}
	if cur, err := s.loadAuditSettings(ctx); err == nil { // reread, so a change saved meanwhile is kept
		cur.LastPrune, cur.LastPruned = now, removed
		s.docs.Put(ctx, kindSettings, idAuditSettings, cur)
	}
	return removed, nil
}

// startAuditPruner prunes once at start and then every hour.
func (s *Server) startAuditPruner(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		prune := func() {
			if _, err := s.pruneAudit(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
				s.log.Error("pruning the audit log", "err", err)
			}
		}
		prune()
		t := time.NewTicker(auditPruneEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				prune()
			}
		}
	}()
}
