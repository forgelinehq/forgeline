package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/docstore"
	"github.com/forgelinehq/forgeline/internal/trace"
	"github.com/forgelinehq/forgeline/internal/version"
)

// Sending traces to an OpenTelemetry backend (design note 049, slice 2): Settings › Connections › OpenTelemetry holds
// an OTLP/HTTP endpoint and headers whose values are kept in the vault. Traces then go there in the background, in the
// shape Forgeline keeps them, without a person's user name.

const (
	idOTel         = "otel"
	maxOTelHeaders = 8
)

type otelSettings struct {
	Enabled  bool     `json:"enabled"`
	Endpoint string   `json:"endpoint"`
	Service  string   `json:"service"`
	Headers  []string `json:"headers"` // names; values are in the vault
	// Content keeps prompts, answers and tool arguments and results in traces, with credentials hidden (note 049).
	Content   bool      `json:"content"`
	UpdatedBy string    `json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func otelHeaderSecret(name string) string { return "forgeline.otel.header." + strings.ToLower(name) }

var otelHeaderRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,63}$`)

// Headers Forgeline sets itself, which a setting may not replace.
var reservedHeaders = map[string]bool{"host": true, "content-type": true, "content-length": true, "transfer-encoding": true, "connection": true}

func (s *Server) loadOTel(ctx context.Context) otelSettings {
	var o otelSettings
	if err := s.docs.Get(ctx, kindSettings, idOTel, &o); err != nil && !errors.Is(err, docstore.ErrNotFound) {
		s.log.Error("reading the OpenTelemetry settings", "err", err)
	}
	if o.Headers == nil {
		o.Headers = []string{}
	}
	return o
}

// otelConfig is the exporter's configuration from the settings and the vault; an empty endpoint means off.
func (s *Server) otelConfig(ctx context.Context, o otelSettings) trace.ExportConfig {
	if !o.Enabled || o.Endpoint == "" {
		return trace.ExportConfig{}
	}
	h := map[string]string{}
	for _, n := range o.Headers {
		if v, ok := s.internalSecretValue(ctx, otelHeaderSecret(n)); ok {
			h[n] = v
		}
	}
	return trace.ExportConfig{Endpoint: o.Endpoint, Headers: h, Service: o.Service, Version: version.Version, Drop: []string{attrUser}}
}

// reloadOTel points the exporter at what the settings say now.
func (s *Server) reloadOTel(ctx context.Context) {
	s.otel.Configure(s.otelConfig(ctx, s.loadOTel(ctx)))
}

type otelJSON struct {
	otelSettings
	Headers []otelHeaderJSON  `json:"headers"`
	Stats   trace.ExportStats `json:"stats"`
}

type otelHeaderJSON struct {
	Name string `json:"name"`
	Set  bool   `json:"set"`
}

func (s *Server) otelView(ctx context.Context) otelJSON {
	o := s.loadOTel(ctx)
	v := otelJSON{otelSettings: o, Headers: []otelHeaderJSON{}, Stats: s.otel.Stats()}
	for _, n := range o.Headers {
		_, ok := s.internalSecretValue(ctx, otelHeaderSecret(n))
		v.Headers = append(v.Headers, otelHeaderJSON{Name: n, Set: ok})
	}
	return v
}

func (s *Server) handleGetOTel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.otelView(r.Context()))
}

type otelRequest struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint"`
	Service  string `json:"service"`
	Content  bool   `json:"content"`
	Headers  []struct {
		Name  string  `json:"name"`
		Value *string `json:"value"` // nil keeps the stored value
	} `json:"headers"`
}

func (s *Server) handlePutOTel(w http.ResponseWriter, r *http.Request) {
	var q otelRequest
	if !readJSON(w, r, &q) {
		return
	}
	ctx, who := r.Context(), principalFrom(r.Context())
	q.Endpoint, q.Service = strings.TrimRight(strings.TrimSpace(q.Endpoint), "/"), strings.TrimSpace(q.Service)
	if q.Endpoint != "" {
		if err := trace.CheckEndpoint(q.Endpoint); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else if q.Enabled {
		writeError(w, http.StatusBadRequest, "endpoint: needed to turn sending on")
		return
	}
	if len(q.Service) > 100 {
		writeError(w, http.StatusBadRequest, "service: at most 100 characters")
		return
	}
	if len(q.Headers) > maxOTelHeaders {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("headers: at most %d", maxOTelHeaders))
		return
	}
	old := s.loadOTel(ctx)
	seen := map[string]bool{}
	var names []string
	for i, h := range q.Headers {
		n := strings.TrimSpace(h.Name)
		switch {
		case !otelHeaderRE.MatchString(n):
			writeError(w, http.StatusBadRequest, fmt.Sprintf("headers[%d].name: letters, digits and -, such as Authorization or x-api-key", i))
			return
		case reservedHeaders[strings.ToLower(n)]:
			writeError(w, http.StatusBadRequest, fmt.Sprintf("headers[%d].name: %s is set by Forgeline", i, n))
			return
		case seen[strings.ToLower(n)]:
			writeError(w, http.StatusBadRequest, fmt.Sprintf("headers[%d].name: %s is listed twice", i, n))
			return
		case h.Value != nil && len(*h.Value) > 4096:
			writeError(w, http.StatusBadRequest, fmt.Sprintf("headers[%d].value: at most 4096 characters", i))
			return
		case h.Value != nil && strings.ContainsAny(*h.Value, "\r\n"):
			writeError(w, http.StatusBadRequest, fmt.Sprintf("headers[%d].value: one line", i))
			return
		}
		seen[strings.ToLower(n)] = true
		names = append(names, n)
	}
	for i, h := range q.Headers {
		if h.Value != nil && *h.Value != "" {
			if _, _, err := s.putSecret(ctx, who.Name, otelHeaderSecret(names[i]), *h.Value, "OpenTelemetry header "+names[i]); err != nil {
				s.internalError(w, "saving a header", err)
				return
			}
		}
	}
	for _, n := range old.Headers {
		if !seen[strings.ToLower(n)] {
			s.docs.Delete(ctx, kindSecret, otelHeaderSecret(n))
		}
	}
	sort.Strings(names)
	o := otelSettings{Enabled: q.Enabled, Endpoint: q.Endpoint, Service: q.Service, Headers: names, Content: q.Content, UpdatedBy: who.Name, UpdatedAt: time.Now().UTC()}
	if o.Headers == nil {
		o.Headers = []string{}
	}
	if err := s.docs.Put(ctx, kindSettings, idOTel, o); err != nil {
		s.internalError(w, "saving the settings", err)
		return
	}
	s.reloadOTel(ctx)
	s.auditReq(r, "otel.update", "settings/otel", fmt.Sprintf("sending %v to %s, headers %s, prompts and answers kept %v", o.Enabled, o.Endpoint, strings.Join(o.Headers, ", "), o.Content), true)
	writeJSON(w, http.StatusOK, s.otelView(ctx))
}

// handleTestOTel sends one small trace now, with the saved settings, and says what the endpoint answered.
func (s *Server) handleTestOTel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	o := s.loadOTel(ctx)
	if o.Endpoint == "" {
		writeError(w, http.StatusConflict, "save an endpoint first")
		return
	}
	o.Enabled = true
	cfg := s.otelConfig(ctx, o)
	now := time.Now().UTC()
	id := trace.IDFor("test-" + now.Format(time.RFC3339Nano))
	span := trace.Span{TraceID: id, SpanID: id[:16], Name: "forgeline test", Kind: trace.Internal, Start: now.Add(-50 * time.Millisecond), End: now,
		Attrs: map[string]any{attrOp: "invoke_agent", attrAgentName: "Forgeline test", "forgeline.test": true}}
	tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := trace.Send(tctx, &http.Client{Timeout: 15 * time.Second}, cfg, []trace.Span{span}); err != nil {
		s.auditReq(r, "otel.test", "settings/otel", "failed: "+err.Error(), false)
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.auditReq(r, "otel.test", "settings/otel", "sent test trace "+id, true)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "trace_id": id})
}
