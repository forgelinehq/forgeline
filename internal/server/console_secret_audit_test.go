package server

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/executor"
	"github.com/forgelinehq/forgeline/internal/store"
)

const secretPipeline = `version: v1alpha1
name: ships
steps:
  - id: ship
    image: alpine:3.24
    run: ./ship
    permissions:
      secrets: [DEPLOY_KEY]
  - id: offline
    image: alpine:3.24
    run: ./sign
    permissions:
      secrets: [DEPLOY_KEY]
      network: none
  - id: plain
    image: alpine:3.24
    run: echo
`

// A secret given to a step is in the audit log (names, never values, and the network the step has); a step with secrets and network
// access also gets a record of its own; a refusal is in the log as refused; a step without secrets leaves nothing.
func TestSecretGrantsAndRefusalsAreInTheAuditLog(t *testing.T) {
	const value = "deploy-key-value-1234"
	h := newHarness(t, &executor.Fake{Handler: func(_ context.Context, s executor.StepRun, _ io.Writer) (executor.Result, error) {
		return executor.Result{}, nil
	}},
		func(c *Config) { c.Secrets = map[string]string{"DEPLOY_KEY": value} })
	_, run := h.submit(secretPipeline, nil)
	h.waitStatus(run.ID, "succeeded")
	audit := func(action string) []AuditEvent {
		var out struct {
			Events []AuditEvent `json:"events"`
		}
		_, b := h.do("GET", "/v1/audit?limit=200&action="+action, testToken, nil, "")
		jsonUnmarshal(b, &out)
		return out.Events
	}
	grants := audit("run.secret.grant")
	if len(grants) != 2 {
		t.Fatalf("grants = %+v", grants)
	}
	byTarget := map[string]AuditEvent{}
	for _, e := range grants {
		byTarget[e.Target] = e
		if strings.Contains(e.Detail, value) {
			t.Errorf("the value is in the audit log: %s", e.Detail)
		}
		if !e.Allowed || !strings.HasPrefix(e.Actor, "run "+run.ID) {
			t.Errorf("grant = %+v", e)
		}
	}
	ship, offline := byTarget["run/"+run.ID+"/ship"], byTarget["run/"+run.ID+"/offline"]
	if !strings.Contains(ship.Detail, "secrets DEPLOY_KEY") || !strings.Contains(ship.Detail, "network egress") || !strings.Contains(ship.Detail, "run started by") {
		t.Errorf("ship: %+v", ship)
	}
	if !strings.Contains(offline.Detail, "network none") {
		t.Errorf("offline: %+v", offline)
	}
	if _, plain := byTarget["run/"+run.ID+"/plain"]; plain {
		t.Error("a step without secrets was recorded as given some")
	}
	combo := audit("run.secret.network")
	if len(combo) != 1 || combo[0].Target != "run/"+run.ID+"/ship" || !strings.Contains(combo[0].Detail, "can also reach the network") {
		t.Errorf("the secrets-plus-network record: %+v", combo)
	}

	// A step that asks for a secret that is not configured is refused, and the refusal is in the log.
	_, run2 := h.submit(strings.ReplaceAll(secretPipeline, "DEPLOY_KEY", "OTHER_KEY"), nil)
	h.waitStatus(run2.ID, "failed")
	refused := audit("run.secret.refused")
	if len(refused) == 0 || refused[0].Allowed || !strings.Contains(refused[0].Detail, "OTHER_KEY") || !strings.Contains(refused[0].Detail, "is not configured") {
		t.Errorf("refusals: %+v", refused)
	}
}

// A run GitHub started asks for a secret and is refused: that is in the log as refused, with how the run started.
func TestARefusedSecretForAGitHubRunIsInTheAuditLog(t *testing.T) {
	h := newHarness(t, &executor.Fake{}, func(c *Config) { c.Secrets = map[string]string{"DEPLOY_KEY": "deploy-key-value-1234"} })
	r, err := h.store.CreateRun(context.Background(), store.Run{PipelineName: "ships", PipelineYAML: secretPipeline, Trigger: store.TriggerGitHub}, []string{"ship", "offline", "plain"})
	if err != nil {
		t.Fatal(err)
	}
	h.waitStatus(r.ID, "failed")
	var out struct {
		Events []AuditEvent `json:"events"`
	}
	_, b := h.do("GET", "/v1/audit?limit=100&action=run.secret", testToken, nil, "")
	jsonUnmarshal(b, &out)
	var refused, granted int
	for _, e := range out.Events {
		switch e.Action {
		case "run.secret.refused":
			refused++
			if e.Allowed || !strings.Contains(e.Detail, "receive no secrets") || !strings.Contains(e.Detail, "run started by github") || strings.Contains(e.Detail, "deploy-key-value") {
				t.Errorf("refusal: %+v", e)
			}
		case "run.secret.grant", "run.secret.network":
			granted++
		}
	}
	if refused != 2 || granted != 0 {
		t.Errorf("refused %d, granted %d: %+v", refused, granted, out.Events)
	}
}

// The audit log can be read back past the newest events: a page that is full, and a filter whose scan window ended before the log did,
// both say where to go on from, and following that reaches everything, once, newest first.
func TestTheAuditLogPagesBackWithACursor(t *testing.T) {
	h := newHarness(t, &executor.Fake{})
	ctx := context.Background()
	for i := 0; i < 25; i++ {
		action := "x.new"
		if i < 3 {
			action = "x.old" // the three oldest
		}
		if err := h.srv.audit(ctx, Principal{Name: "t"}, "", action, "target", "", true); err != nil {
			t.Fatal(err)
		}
	}
	type page struct {
		Events []AuditEvent `json:"events"`
		Next   string       `json:"next"`
	}
	get := func(query string) page {
		t.Helper()
		resp, b := h.do("GET", "/v1/audit?"+query, testToken, nil, "")
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s = %d %s", query, resp.StatusCode, b)
		}
		var p page
		jsonUnmarshal(b, &p)
		return p
	}

	// Plain paging: pages of 10 until the log ends, each older than the one before.
	seen := map[string]bool{}
	var last string
	cursor, pages := "", 0
	for {
		q := "limit=10"
		if cursor != "" {
			q += "&before=" + cursor
		}
		p := get(q)
		pages++
		for _, e := range p.Events {
			if seen[e.ID] {
				t.Errorf("event %s on two pages", e.ID)
			}
			if last != "" && e.ID >= last {
				t.Errorf("not newest first: %s after %s", e.ID, last)
			}
			seen[e.ID], last = true, e.ID
		}
		if p.Next == "" || pages > 10 {
			break
		}
		cursor = p.Next
	}
	if len(seen) != 25 {
		t.Errorf("paging reached %d events, want 25 (%d pages)", len(seen), pages)
	}

	// A filter whose scan window is shorter than the log: the first answer has nothing but says where to go on, and following it
	// finds all three old events and then ends.
	old := auditScanWindow
	auditScanWindow = 10
	defer func() { auditScanWindow = old }()
	var found []string
	cursor, pages = "", 0
	for {
		q := "action=x.old"
		if cursor != "" {
			q += "&before=" + cursor
		}
		p := get(q)
		pages++
		if pages == 1 && (len(p.Events) != 0 || p.Next == "") {
			t.Errorf("the first window should hold no match and offer a cursor: %+v", p)
		}
		for _, e := range p.Events {
			found = append(found, e.ID)
		}
		if p.Next == "" || pages > 10 {
			break
		}
		cursor = p.Next
	}
	if len(found) != 3 || pages < 3 {
		t.Errorf("found %d old events in %d pages", len(found), pages)
	}

	// A filtered page that is full resumes right after its last event, not at the end of the scan window: nothing is skipped.
	got := map[string]bool{}
	cursor, pages = "", 0
	for {
		q := "action=x.new&limit=5"
		if cursor != "" {
			q += "&before=" + cursor
		}
		p := get(q)
		pages++
		for _, e := range p.Events {
			got[e.ID] = true
		}
		if p.Next == "" || pages > 20 {
			break
		}
		cursor = p.Next
	}
	if len(got) != 22 {
		t.Errorf("filtered paging reached %d of 22 events in %d pages", len(got), pages)
	}

	// The cursor is an event id, nothing else.
	for _, bad := range []string{"x", "../etc", "123", "00000000000000000000-" + strings.Repeat("a", 40)} {
		if resp, _ := h.do("GET", "/v1/audit?before="+bad, testToken, nil, ""); resp.StatusCode != 400 {
			t.Errorf("before=%q was accepted", bad)
		}
	}
}
