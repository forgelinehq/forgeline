package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// standIn is a small MCP server over HTTP. modern answers the 2026-07-28 way (no handshake, version in _meta and
// headers); otherwise it is a 2025-11-25 server that needs initialize and a session id. sse answers with an event
// stream, with a notification before the response.
type standIn struct {
	modern, sse bool
	mu          sync.Mutex
	seen        []http.Header
	bodies      []map[string]any
}

func (s *standIn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	json.Unmarshal(raw, &m)
	s.mu.Lock()
	s.seen, s.bodies = append(s.seen, r.Header.Clone()), append(s.bodies, m)
	s.mu.Unlock()
	method, _ := m["method"].(string)
	id := m["id"]
	reply := func(result any) {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
		if s.sse {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", `{"jsonrpc":"2.0","method":"notifications/progress","params":{}}`)
			fmt.Fprintf(w, "data: %s\n\n", b)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}
	if !s.modern {
		switch {
		case method == "server/discover":
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"jsonrpc":"2.0","id":null,"error":{"code":-32000,"message":"Bad Request: Server not initialized"}}`)
			return
		case method == "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-42")
			reply(map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "old"}})
			return
		case method == "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case r.Header.Get("Mcp-Session-Id") != "sess-42":
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"jsonrpc":"2.0","id":null,"error":{"code":-32000,"message":"Bad Request: No valid session ID provided"}}`)
			return
		}
	} else {
		params, _ := m["params"].(map[string]any)
		meta, _ := params["_meta"].(map[string]any)
		if r.Header.Get("MCP-Protocol-Version") != ModernVersion || meta["io.modelcontextprotocol/protocolVersion"] != ModernVersion || r.Header.Get("Mcp-Method") != method {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"jsonrpc":"2.0","id":null,"error":{"code":-32001,"message":"HeaderMismatch"}}`)
			return
		}
	}
	switch method {
	case "server/discover":
		reply(map[string]any{"supportedVersions": []string{ModernVersion}, "capabilities": map[string]any{"tools": map[string]any{}}})
	case "tools/list":
		reply(map[string]any{"tools": []map[string]any{
			{"name": "get_weather", "description": "Weather for a city", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}}, "annotations": map[string]any{"readOnlyHint": true}},
			{"name": "delete_city", "description": "Deletes a city", "inputSchema": map[string]any{"type": "object"}},
		}})
	case "tools/call":
		params, _ := m["params"].(map[string]any)
		args, _ := params["arguments"].(map[string]any)
		reply(map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("sunny in %v", args["city"])}}})
	default:
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"jsonrpc":"2.0","id":null,"error":{"code":-32601,"message":"Method not found"}}`)
	}
}

func TestHTTPClientSpeaksBothRevisions(t *testing.T) {
	for _, c := range []struct {
		name        string
		modern, sse bool
	}{{"modern json", true, false}, {"modern stream", true, true}, {"legacy json", false, false}, {"legacy stream", false, true}} {
		t.Run(c.name, func(t *testing.T) {
			s := &standIn{modern: c.modern, sse: c.sse}
			srv := httptest.NewServer(s)
			defer srv.Close()
			hdr := http.Header{"Authorization": {"Bearer secret-token"}}
			cl, err := DialHTTP(context.Background(), srv.URL, hdr, srv.Client())
			if err != nil {
				t.Fatal(err)
			}
			if cl.Legacy() == c.modern {
				t.Errorf("legacy = %v", cl.Legacy())
			}
			tools, err := cl.Tools(context.Background())
			if err != nil || len(tools) != 2 || !tools[0].Annotations.ReadOnly || tools[1].Annotations.ReadOnly {
				t.Fatalf("tools = %+v, %v", tools, err)
			}
			res, err := cl.Call(context.Background(), "get_weather", map[string]any{"city": "Pune"})
			if err != nil || res.Text != "sunny in Pune" {
				t.Fatalf("call = %+v, %v", res, err)
			}
			for _, h := range s.seen {
				if h.Get("Authorization") != "Bearer secret-token" || !strings.Contains(h.Get("Accept"), "text/event-stream") {
					t.Errorf("headers = %v", h)
				}
			}
			last := s.seen[len(s.seen)-1]
			if c.modern && (last.Get("Mcp-Method") != "tools/call" || last.Get("Mcp-Name") != "get_weather") {
				t.Errorf("modern call headers = %v", last)
			}
			if !c.modern && last.Get("Mcp-Session-Id") != "sess-42" {
				t.Errorf("legacy call lost its session: %v", last)
			}
		})
	}
}

func TestHTTPClientRefusesRedirectsAndHugeAnswers(t *testing.T) {
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	hc := redirect.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if _, err := DialHTTP(context.Background(), redirect.URL, nil, hc); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Errorf("a redirect must be refused: %v", err)
	}
	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"x":"`+strings.Repeat("a", maxHTTPAnswer+10)+`"}}`)
	}))
	defer huge.Close()
	if _, err := DialHTTP(context.Background(), huge.URL, nil, huge.Client()); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("an oversized answer must be refused: %v", err)
	}
}

func TestHeaderValueUsesBase64WhenNotPlainASCII(t *testing.T) {
	for in, want := range map[string]string{"get_weather": "get_weather", "météo": "=?base64?bcOpdMOpbw==?=", " padded": "=?base64?IHBhZGRlZA==?=", "a\r\nX: y": "=?base64?YQ0KWDogeQ==?="} {
		if got := headerValue(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
