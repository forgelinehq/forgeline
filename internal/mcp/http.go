package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// The Streamable HTTP transport (design note 042), for remote MCP servers. It speaks the current revision,
// 2026-07-28 (stateless: every request carries its protocol version in _meta and in headers; servers implement
// server/discover), and falls back to the 2025-11-25 shape (an initialize handshake and an Mcp-Session-Id) for servers
// that do not, as the specification's backward-compatibility section describes. Only tools are used.

const (
	ModernVersion = "2026-07-28"
	LegacyVersion = "2025-11-25"
	maxHTTPAnswer = 4 << 20
)

// HTTPClient talks to one remote MCP server. Safe for one caller at a time.
type HTTPClient struct {
	url     string
	header  http.Header // sent with every request (authentication); never logged
	hc      *http.Client
	mu      sync.Mutex
	nextID  int
	legacy  bool
	version string
	session string
}

// Errors a caller can tell apart.
var (
	ErrTooLarge = errors.New("the MCP server's answer is too large")
)

// DialHTTP finds out which revision the server speaks and, for an older one, completes its handshake. hc decides where
// connections may go (the caller refuses private addresses and redirects).
func DialHTTP(ctx context.Context, url string, header http.Header, hc *http.Client) (*HTTPClient, error) {
	c := &HTTPClient{url: url, header: header.Clone(), hc: hc, version: ModernVersion}
	status, raw, err := c.post(ctx, "server/discover", "", map[string]any{})
	if err == nil && status == http.StatusOK {
		return c, nil
	}
	if err != nil && !errors.Is(err, errNotModern) {
		return nil, err
	}
	if modernError(raw) {
		return nil, fmt.Errorf("the MCP server refused this client: %s", rpcErrorText(raw))
	}
	// An older server: the handshake, then its version and session on every request.
	c.legacy, c.version = true, LegacyVersion
	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := c.call(ctx, "initialize", "", map[string]any{
		"protocolVersion": LegacyVersion, "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "forgeline", "version": "1"},
	}, &init); err != nil {
		return nil, err
	}
	if init.ProtocolVersion != "" {
		c.version = init.ProtocolVersion
	}
	if _, _, err := c.post(ctx, "notifications/initialized", "", nil); err != nil && !errors.Is(err, errNotModern) {
		return nil, err
	}
	return c, nil
}

// Legacy says whether the server speaks an older revision (for display).
func (c *HTTPClient) Legacy() bool { return c.legacy }

// Version is the protocol version in use.
func (c *HTTPClient) Version() string { return c.version }

// Tools lists the server's tools.
func (c *HTTPClient) Tools(ctx context.Context) ([]Tool, error) {
	var out struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", "", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

// Call runs one tool.
func (c *HTTPClient) Call(ctx context.Context, name string, args map[string]any) (Result, error) {
	var out callResult
	if err := c.call(ctx, "tools/call", name, map[string]any{"name": name, "arguments": args}, &out); err != nil {
		return Result{}, err
	}
	return out.result(), nil
}

var errNotModern = errors.New("not a modern MCP answer")

// call sends a request and decodes its result.
func (c *HTTPClient) call(ctx context.Context, method, name string, params map[string]any, out any) error {
	status, raw, err := c.post(ctx, method, name, params)
	if err != nil && !errors.Is(err, errNotModern) {
		return err
	}
	if status != http.StatusOK {
		if t := rpcErrorText(raw); t != "" {
			return fmt.Errorf("the MCP server refused %s: %s", method, t)
		}
		return fmt.Errorf("the MCP server answered %s with HTTP %d", method, status)
	}
	var m struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("the MCP server's answer to %s is not JSON-RPC", method)
	}
	if m.Error != nil {
		return fmt.Errorf("the MCP server refused %s: %s", method, m.Error.Message)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(m.Result, out)
}

// post sends one JSON-RPC message (params nil: a notification) and returns the HTTP status and, for a request, the
// JSON-RPC response for its id, read from a JSON body or an event stream.
func (c *HTTPClient) post(ctx context.Context, method, name string, params map[string]any) (int, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	id := 0
	if params != nil {
		c.nextID++
		id = c.nextID
		msg["id"] = id
		if !c.legacy {
			p := map[string]any{}
			for k, v := range params {
				p[k] = v
			}
			p["_meta"] = map[string]any{
				"io.modelcontextprotocol/protocolVersion":    c.version,
				"io.modelcontextprotocol/clientCapabilities": map[string]any{},
				"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "forgeline", "version": "1"},
			}
			params = p
		}
		msg["params"] = params
	}
	body, _ := json.Marshal(msg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	for k, vs := range c.header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", c.version)
	if !c.legacy {
		req.Header.Set("Mcp-Method", method)
		if name != "" {
			req.Header.Set("Mcp-Name", headerValue(name))
		}
	}
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, errors.New("the MCP server took too long")
		}
		return 0, nil, fmt.Errorf("could not reach the MCP server: %v", err)
	}
	defer res.Body.Close()
	if sid := res.Header.Get("Mcp-Session-Id"); sid != "" && c.legacy {
		c.session = sid
	}
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		return res.StatusCode, nil, errors.New("the MCP server redirected the request; give its final address instead")
	}
	if id == 0 {
		if res.StatusCode == http.StatusAccepted || res.StatusCode == http.StatusOK {
			return res.StatusCode, nil, nil
		}
		return res.StatusCode, nil, errNotModern
	}
	lim := io.LimitReader(res.Body, maxHTTPAnswer+1)
	if strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		raw, err := readEventStream(lim, id)
		return res.StatusCode, raw, err
	}
	raw, err := io.ReadAll(lim)
	if err != nil {
		return res.StatusCode, nil, err
	}
	if len(raw) > maxHTTPAnswer {
		return res.StatusCode, nil, ErrTooLarge
	}
	if res.StatusCode != http.StatusOK {
		return res.StatusCode, raw, errNotModern
	}
	return res.StatusCode, raw, nil
}

// readEventStream returns the JSON-RPC response for id from an SSE stream, skipping notifications.
func readEventStream(r io.Reader, id int) ([]byte, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxHTTPAnswer)
	var data strings.Builder
	total := 0
	flush := func() ([]byte, bool) {
		d := data.String()
		data.Reset()
		if d == "" {
			return nil, false
		}
		var m struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal([]byte(d), &m) == nil && string(m.ID) == fmt.Sprint(id) {
			return []byte(d), true
		}
		return nil, false
	}
	for sc.Scan() {
		line := sc.Text()
		total += len(line)
		if total > maxHTTPAnswer {
			return nil, ErrTooLarge
		}
		switch {
		case line == "":
			if raw, ok := flush(); ok {
				return raw, nil
			}
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if raw, ok := flush(); ok {
		return raw, nil
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("the MCP server's event stream broke: %v", err)
	}
	return nil, errors.New("the MCP server's event stream ended without an answer")
}

// headerValue carries a value in a header as is when it is plain ASCII, otherwise in the specification's Base64 form.
func headerValue(v string) string {
	safe := v != "" && strings.TrimSpace(v) == v
	for i := 0; i < len(v) && safe; i++ {
		if v[i] < 0x20 || v[i] > 0x7e {
			safe = false
		}
	}
	if safe {
		return v
	}
	return "=?base64?" + base64.StdEncoding.EncodeToString([]byte(v)) + "?="
}

// modernError says whether a body is an error a 2026-07-28 server sends (so the client must not fall back): one that
// lists the versions it supports or says a capability or header is missing.
func modernError(raw []byte) bool {
	var m struct {
		Error *struct {
			Message string `json:"message"`
			Data    struct {
				Supported []string `json:"supported"`
			} `json:"data"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &m) != nil || m.Error == nil {
		return false
	}
	if len(m.Error.Data.Supported) > 0 {
		for _, v := range m.Error.Data.Supported {
			if v == LegacyVersion || v == "2025-06-18" || v == "2025-03-26" {
				return false // it also speaks an older revision: the handshake will do
			}
		}
		return true
	}
	return strings.Contains(m.Error.Message, "MissingRequiredClientCapability") || strings.Contains(m.Error.Message, "HeaderMismatch")
}

func rpcErrorText(raw []byte) string {
	var m struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &m) != nil || m.Error == nil {
		return ""
	}
	return firstLine(m.Error.Message)
}
