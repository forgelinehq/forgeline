package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type otlpCollector struct {
	mu     sync.Mutex
	bodies []string
	keys   []string
}

func newOTLPCollector(t *testing.T) (*otlpCollector, string) {
	c := &otlpCollector{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			w.WriteHeader(404)
			return
		}
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.bodies = append(c.bodies, string(b))
		c.keys = append(c.keys, r.Header.Get("X-Api-Key"))
		c.mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	return c, srv.URL
}

func (c *otlpCollector) all() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.bodies, "\n")
}

// Sending traces to an OpenTelemetry backend (note 049, slice 2): an administrator sets the endpoint and a header
// kept in the vault; a test trace and real traces arrive with the header; a person's user name is never sent.
func TestTracesGoToAnOTLPEndpoint(t *testing.T) {
	m := newMCPHarness(t)
	col, url := newOTLPCollector(t)
	m.dev.expect(403, "GET", "/v1/settings/otel", nil)
	m.dev.expect(403, "PUT", "/v1/settings/otel", map[string]any{"enabled": true, "endpoint": url})
	for _, bad := range []map[string]any{
		{"enabled": true, "endpoint": "http://collector.example.com:4318"},
		{"enabled": true, "endpoint": ""},
		{"enabled": true, "endpoint": url + "/v1/traces"},
		{"enabled": true, "endpoint": url, "headers": []map[string]any{{"name": "Content-Type", "value": "x"}}},
		{"enabled": true, "endpoint": url, "headers": []map[string]any{{"name": "a b", "value": "x"}}},
		{"enabled": true, "endpoint": url, "headers": []map[string]any{{"name": "k", "value": "x\r\ny"}}},
	} {
		m.admin.expect(400, "PUT", "/v1/settings/otel", bad)
	}
	raw := m.admin.expect(200, "PUT", "/v1/settings/otel", map[string]any{"enabled": true, "endpoint": url, "service": "forgeline-test",
		"headers": []map[string]any{{"name": "X-Api-Key", "value": "SEKRIT-KEY"}}})
	if strings.Contains(string(raw), "SEKRIT") || !strings.Contains(string(raw), `"name":"X-Api-Key","set":true`) {
		t.Errorf("settings view: %s", raw)
	}
	if strings.Contains(string(m.admin.expect(200, "GET", "/v1/settings/otel", nil)), "SEKRIT") {
		t.Error("the header value is shown")
	}

	var tr map[string]any
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/settings/otel/test", nil), &tr)
	if tr["ok"] != true || !strings.Contains(col.all(), `"name":"forgeline test"`) || col.keys[0] != "SEKRIT-KEY" {
		t.Fatalf("test trace: %v, %s, %v", tr, col.all(), col.keys)
	}

	// A real agent chat: queued, then sent.
	a := newAgent(t, m.admin, agentBody("Reviewer", nil, []string{"list_runs"}, "T0"))
	m.llm.script("```forgeline-lookup\n{\"tool\":\"list_runs\",\"args\":{}}\n```", "fine")
	m.dev.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "go"})
	m.srv.otel.Flush(context.Background())
	got := col.all()
	for _, want := range []string{`"name":"invoke_agent Reviewer"`, `"name":"execute_tool list_runs"`, `"key":"gen_ai.agent.id","value":{"stringValue":"` + a.ID + `"}`,
		`"key":"service.name","value":{"stringValue":"forgeline-test"}`, `"key":"forgeline.role"`} {
		if !strings.Contains(got, want) {
			t.Errorf("sent traces lack %s", want)
		}
	}
	if strings.Contains(got, "forgeline.user") || strings.Contains(got, `"dev"`) {
		t.Error("a user name was sent")
	}
	var view otelJSON
	json.Unmarshal(m.admin.expect(200, "GET", "/v1/settings/otel", nil), &view)
	if view.Stats.Sent < 2 || view.Stats.LastError != "" {
		t.Errorf("stats: %+v", view.Stats)
	}

	// Removing the header deletes its value; turning sending off stops it.
	m.admin.expect(200, "PUT", "/v1/settings/otel", map[string]any{"enabled": false, "endpoint": url})
	if _, ok := m.srv.internalSecretValue(context.Background(), otelHeaderSecret("X-Api-Key")); ok {
		t.Error("a removed header stayed in the vault")
	}
	n := len(col.bodies)
	m.llm.script("again")
	m.dev.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "go"})
	m.srv.otel.Flush(context.Background())
	if len(col.bodies) != n {
		t.Error("traces were sent while sending was off")
	}
	audit := string(m.admin.expect(200, "GET", "/v1/audit?limit=50", nil))
	if !strings.Contains(audit, "otel.update") || !strings.Contains(audit, "otel.test") {
		t.Error("not audited")
	}
}
