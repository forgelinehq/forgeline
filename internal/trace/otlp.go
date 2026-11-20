package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OTLP over HTTP with JSON (https://opentelemetry.io/docs/specs/otlp/): spans go to <endpoint>/v1/traces in batches.

// Exporter sends spans to an OTLP/HTTP endpoint in the background. A slow or broken endpoint never blocks the caller:
// Add only queues, and when the queue is full the oldest spans are dropped and counted.
type Exporter struct {
	mu       sync.Mutex
	cfg      ExportConfig
	queue    []Span
	stats    ExportStats
	wake     chan struct{}
	client   *http.Client
	maxQueue int
	batch    int
	every    time.Duration
}

// ExportConfig is where spans go and how.
type ExportConfig struct {
	Endpoint string            // https://otlp.example.com or http://127.0.0.1:4318; /v1/traces is added
	Headers  map[string]string // sent with every request (an API key for the backend)
	Service  string            // service.name
	Version  string            // service.version
	Drop     []string          // attribute names never sent (a person's user name)
}

// ExportStats is what the exporter has done since it started.
type ExportStats struct {
	Sent      int64     `json:"sent_spans"`
	Dropped   int64     `json:"dropped_spans"`
	Failed    int64     `json:"failed_batches"`
	Queued    int       `json:"queued_spans"`
	LastError string    `json:"last_error,omitempty"`
	LastSent  time.Time `json:"last_sent"`
	LastTry   time.Time `json:"last_try"`
}

// NewExporter starts nothing until Run is called.
func NewExporter() *Exporter {
	return &Exporter{wake: make(chan struct{}, 1), client: &http.Client{Timeout: 15 * time.Second}, maxQueue: 5000, batch: 500, every: 5 * time.Second}
}

// Configure sets where spans go; an empty endpoint turns export off and empties the queue.
func (e *Exporter) Configure(c ExportConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = c
	if c.Endpoint == "" {
		e.queue = nil
	}
}

// Stats is a copy of the counters.
func (e *Exporter) Stats() ExportStats {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.stats
	s.Queued = len(e.queue)
	return s
}

// Add queues spans for sending, when export is on.
func (e *Exporter) Add(spans []Span) {
	e.mu.Lock()
	if e.cfg.Endpoint == "" {
		e.mu.Unlock()
		return
	}
	e.queue = append(e.queue, spans...)
	if over := len(e.queue) - e.maxQueue; over > 0 {
		e.queue = e.queue[over:]
		e.stats.Dropped += int64(over)
	}
	full := len(e.queue) >= e.batch
	e.mu.Unlock()
	if full {
		select {
		case e.wake <- struct{}{}:
		default:
		}
	}
}

// Run sends batches until ctx ends: every few seconds, or sooner when a batch is full.
func (e *Exporter) Run(ctx context.Context) {
	t := time.NewTicker(e.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-e.wake:
		}
		for e.flushOnce(ctx) {
		}
	}
}

// Flush sends everything queued now, batch by batch, and returns when the queue is empty or a batch failed.
func (e *Exporter) Flush(ctx context.Context) {
	for e.flushOnce(ctx) {
		if e.Stats().LastError != "" {
			return
		}
	}
}

// flushOnce sends one batch and says whether more is waiting.
func (e *Exporter) flushOnce(ctx context.Context) bool {
	e.mu.Lock()
	cfg := e.cfg
	n := min(len(e.queue), e.batch)
	if cfg.Endpoint == "" || n == 0 {
		e.mu.Unlock()
		return false
	}
	batch := append([]Span(nil), e.queue[:n]...)
	e.mu.Unlock()
	var err error
	for try := 0; try < 3; try++ {
		if try > 0 {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(time.Duration(try) * 2 * time.Second):
			}
		}
		if err = Send(ctx, e.client, cfg, batch); err == nil {
			break
		}
		var pe *permanentError
		if errors.As(err, &pe) {
			break
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stats.LastTry = time.Now().UTC()
	// The queue may have moved on (dropped from the front) while sending; remove what was sent by count from what is left.
	drop := min(n, len(e.queue))
	e.queue = e.queue[drop:]
	if err != nil {
		e.stats.Failed++
		e.stats.Dropped += int64(n)
		e.stats.LastError = err.Error()
		return len(e.queue) > 0
	}
	e.stats.Sent += int64(n)
	e.stats.LastSent = e.stats.LastTry
	e.stats.LastError = ""
	return len(e.queue) > 0
}

type permanentError struct{ msg string }

func (p *permanentError) Error() string { return p.msg }

// CheckEndpoint says what is wrong with an OTLP/HTTP endpoint: https, or http only on this machine; no user name, query
// or fragment. A path is allowed (some backends put OTLP under one) and /v1/traces is added to it.
func CheckEndpoint(raw string) error {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("endpoint: an address such as https://otlp.example.com or http://127.0.0.1:4318")
	}
	h := u.Hostname()
	loop := h == "localhost" || h == "127.0.0.1" || h == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && loop) {
		return errors.New("endpoint: must use https (plain http only for localhost or 127.0.0.1)")
	}
	if strings.HasSuffix(u.Path, "/v1/traces") {
		return errors.New("endpoint: leave out /v1/traces; Forgeline adds it")
	}
	return nil
}

// Send posts spans to the endpoint once. A 4xx other than 408 and 429 is permanent: retrying will not help.
func Send(ctx context.Context, client *http.Client, cfg ExportConfig, spans []Span) error {
	body, err := json.Marshal(Encode(cfg, spans))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.Endpoint, "/")+"/v1/traces", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sending traces: %v", err)
	}
	defer resp.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode/100 == 2 {
		return nil
	}
	e := fmt.Sprintf("the endpoint answered %d", resp.StatusCode)
	if m := strings.TrimSpace(string(msg)); m != "" {
		e += ": " + m
	}
	if resp.StatusCode/100 == 4 && resp.StatusCode != 408 && resp.StatusCode != 429 {
		return &permanentError{e}
	}
	return errors.New(e)
}

// --- the OTLP JSON encoding ---

type kv struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

func anyValue(v any) any {
	switch x := v.(type) {
	case string:
		return map[string]any{"stringValue": x}
	case bool:
		return map[string]any{"boolValue": x}
	case int:
		return map[string]any{"intValue": strconv.FormatInt(int64(x), 10)}
	case int64:
		return map[string]any{"intValue": strconv.FormatInt(x, 10)}
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			// Numbers that came back from JSON are float64; whole ones are sent as integers, as they were recorded.
			return map[string]any{"intValue": strconv.FormatInt(int64(x), 10)}
		}
		return map[string]any{"doubleValue": x}
	}
	return map[string]any{"stringValue": fmt.Sprint(v)}
}

func attrs(m map[string]any, drop map[string]bool) []kv {
	keys := make([]string, 0, len(m))
	for k := range m {
		if !drop[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]kv, 0, len(keys))
	for _, k := range keys {
		out = append(out, kv{Key: k, Value: anyValue(m[k])})
	}
	return out
}

// Encode is the ExportTraceServiceRequest for spans, as OTLP/JSON writes it (ids in hex, times as strings of
// nanoseconds, kinds and status codes as numbers).
func Encode(cfg ExportConfig, spans []Span) map[string]any {
	drop := map[string]bool{}
	for _, d := range cfg.Drop {
		drop[d] = true
	}
	service := cfg.Service
	if service == "" {
		service = "forgeline"
	}
	res := map[string]any{"service.name": service, "telemetry.sdk.name": "forgeline", "telemetry.sdk.language": "go"}
	if cfg.Version != "" {
		res["service.version"] = cfg.Version
	}
	out := make([]map[string]any, 0, len(spans))
	for _, s := range spans {
		kind := 1 // SPAN_KIND_INTERNAL
		if s.Kind == Client {
			kind = 3
		}
		o := map[string]any{"traceId": s.TraceID, "spanId": s.SpanID, "name": s.Name, "kind": kind,
			"startTimeUnixNano": strconv.FormatInt(s.Start.UnixNano(), 10), "endTimeUnixNano": strconv.FormatInt(s.End.UnixNano(), 10),
			"attributes": attrs(s.Attrs, drop)}
		if s.ParentID != "" {
			o["parentSpanId"] = s.ParentID
		}
		if s.Error != "" {
			o["status"] = map[string]any{"code": 2, "message": s.Error} // STATUS_CODE_ERROR
		}
		out = append(out, o)
	}
	return map[string]any{"resourceSpans": []any{map[string]any{
		"resource":   map[string]any{"attributes": attrs(res, nil)},
		"scopeSpans": []any{map[string]any{"scope": map[string]any{"name": "forgeline", "version": cfg.Version}, "spans": out}},
	}}}
}
