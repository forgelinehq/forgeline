package trace

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func sample() []Span {
	t0 := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	return []Span{
		{TraceID: strings.Repeat("a", 32), SpanID: strings.Repeat("1", 16), Name: "invoke_agent Triage", Kind: Internal, Start: t0, End: t0.Add(time.Second),
			Attrs: map[string]any{"gen_ai.operation.name": "invoke_agent", "forgeline.user": "ada", "forgeline.role": "developer"}},
		{TraceID: strings.Repeat("a", 32), SpanID: strings.Repeat("2", 16), ParentID: strings.Repeat("1", 16), Name: "chat m", Kind: Client, Start: t0, End: t0.Add(time.Millisecond),
			Error: "model_error", Attrs: map[string]any{"gen_ai.usage.input_tokens": int64(12), "forgeline.cost_usd": 0.25, "x": float64(7), "ok": true}},
	}
}

func TestEncodeIsOTLPJSON(t *testing.T) {
	body, _ := json.Marshal(Encode(ExportConfig{Service: "fl-test", Version: "v1", Drop: []string{"forgeline.user"}}, sample()))
	s := string(body)
	for _, want := range []string{
		`"key":"service.name","value":{"stringValue":"fl-test"}`,
		`"traceId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`, `"parentSpanId":"1111111111111111"`,
		`"kind":3`, `"kind":1`, `"startTimeUnixNano":"1795168800000000000"`,
		`"status":{"code":2,"message":"model_error"}`,
		`"key":"gen_ai.usage.input_tokens","value":{"intValue":"12"}`,
		`"key":"forgeline.cost_usd","value":{"doubleValue":0.25}`, `"key":"x","value":{"intValue":"7"}`, `"key":"ok","value":{"boolValue":true}`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	if strings.Contains(s, "ada") || strings.Contains(s, "forgeline.user") {
		t.Error("a dropped attribute was sent")
	}
}

type collector struct {
	mu     sync.Mutex
	bodies []string
	status int
	auth   string
}

func (c *collector) serve(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, _ := io.ReadAll(r.Body)
	if r.URL.Path != "/otlp/v1/traces" || r.Header.Get("Content-Type") != "application/json" {
		w.WriteHeader(404)
		return
	}
	c.auth = r.Header.Get("Authorization")
	c.bodies = append(c.bodies, string(b))
	if c.status != 0 {
		w.WriteHeader(c.status)
	}
}

func TestExporterSendsInBatchesAndCounts(t *testing.T) {
	c := &collector{}
	srv := httptest.NewServer(http.HandlerFunc(c.serve))
	defer srv.Close()
	e := NewExporter()
	e.every, e.batch = 20*time.Millisecond, 3
	e.Add(sample()) // off: dropped silently, not queued
	if e.Stats().Queued != 0 {
		t.Fatal("queued while off")
	}
	e.Configure(ExportConfig{Endpoint: srv.URL + "/otlp", Headers: map[string]string{"Authorization": "Bearer k"}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.Run(ctx)
	for i := 0; i < 4; i++ {
		e.Add(sample())
	}
	deadline := time.Now().Add(3 * time.Second)
	for e.Stats().Sent < 8 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	st := e.Stats()
	c.mu.Lock()
	defer c.mu.Unlock()
	if st.Sent != 8 || st.Queued != 0 || len(c.bodies) < 3 || c.auth != "Bearer k" || st.LastError != "" {
		t.Errorf("stats %+v, %d bodies, auth %q", st, len(c.bodies), c.auth)
	}
}

func TestExporterDropsWhenFullAndOnPermanentErrors(t *testing.T) {
	c := &collector{status: 400}
	srv := httptest.NewServer(http.HandlerFunc(c.serve))
	defer srv.Close()
	e := NewExporter()
	e.maxQueue, e.batch = 3, 10
	e.Configure(ExportConfig{Endpoint: srv.URL + "/otlp"})
	e.Add(sample())
	e.Add(sample())
	if st := e.Stats(); st.Queued != 3 || st.Dropped != 1 {
		t.Fatalf("full queue: %+v", st)
	}
	start := time.Now()
	e.flushOnce(context.Background())
	st := e.Stats()
	if st.Failed != 1 || st.Dropped != 4 || st.Queued != 0 || !strings.Contains(st.LastError, "400") || time.Since(start) > time.Second {
		t.Errorf("a 400 is not retried: %+v in %s", st, time.Since(start))
	}
	e.Configure(ExportConfig{})
	e.Add(sample())
	if e.Stats().Queued != 0 {
		t.Error("turning export off must stop queueing")
	}
}

func TestCheckEndpoint(t *testing.T) {
	for _, ok := range []string{"https://otlp.example.com", "https://otlp-gateway.grafana.net/otlp", "http://127.0.0.1:4318", "http://localhost:4318/"} {
		if err := CheckEndpoint(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "otlp.example.com", "http://otlp.example.com", "https://u:p@x.com", "https://x.com?a=1", "https://x.com/v1/traces", "ftp://x.com"} {
		if CheckEndpoint(bad) == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

// TestSendReachesJaeger sends spans to a real Jaeger and reads them back. It runs only when FORGELINE_TEST_JAEGER names
// Jaeger's address without a port (127.0.0.1), with OTLP/HTTP on 4318 and the query API on 16686, for example
// `docker run -d -p 127.0.0.1:4318:4318 -p 127.0.0.1:16686:16686 jaegertracing/jaeger:2.23.0`.
func TestSendReachesJaeger(t *testing.T) {
	host := os.Getenv("FORGELINE_TEST_JAEGER")
	if host == "" {
		t.Skip("set FORGELINE_TEST_JAEGER to run")
	}
	spans := sample()
	id := IDFor(time.Now().String())
	for i := range spans {
		spans[i].TraceID = id
	}
	cfg := ExportConfig{Endpoint: "http://" + host + ":4318", Service: "forgeline-test", Drop: []string{"forgeline.user"}}
	if err := Send(context.Background(), http.DefaultClient, cfg, spans); err != nil {
		t.Fatal(err)
	}
	var got string
	for i := 0; i < 50; i++ {
		resp, err := http.Get("http://" + host + ":16686/api/v3/traces/" + id)
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				got = string(b)
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !strings.Contains(got, `"name":"invoke_agent Triage"`) || !strings.Contains(got, `"name":"chat m"`) || !strings.Contains(got, "gen_ai.usage.input_tokens") || strings.Contains(got, "forgeline.user") {
		t.Errorf("Jaeger has: %.600s", got)
	}
}
