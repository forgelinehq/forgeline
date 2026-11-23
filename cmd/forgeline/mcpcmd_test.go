package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPProxyForwardsMessagesWithTheTokenAndNeverPrintsIt(t *testing.T) {
	var seen []string
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/mcp" || r.Method != "POST" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		seen = append(seen, string(b))
		switch {
		case strings.Contains(string(b), "notifications/"):
			w.WriteHeader(http.StatusAccepted)
		case strings.Contains(string(b), "boom"):
			http.Error(w, `{"error":"sign in required"}`, http.StatusUnauthorized)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}` + "\n"))
		}
	}))
	defer srv.Close()
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		``,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":7,"method":"boom"}`,
	}, "\n") + "\n"
	var out, errb bytes.Buffer
	env := map[string]string{"FORGELINE_MCP_TOKEN": "flt_secretsecretsecret", "FORGELINE_SERVER": srv.URL}
	code := runMCP(context.Background(), nil, strings.NewReader(in), &out, &errb, func(k string) string { return env[k] })
	if code != 0 || len(seen) != 3 || auth != "Bearer flt_secretsecretsecret" {
		t.Fatalf("code %d, seen %v, auth %q, stderr %s", code, seen, auth, errb.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"result"`) || !strings.Contains(lines[1], `"id":7`) || !strings.Contains(lines[1], "sign in required") {
		t.Errorf("stdout = %q", out.String())
	}
	if strings.Contains(out.String()+errb.String(), "flt_secret") {
		t.Error("the token was printed")
	}
}

func TestMCPProxyRefusesToStartWithoutATokenOrWithABadAddress(t *testing.T) {
	run := func(env map[string]string, args ...string) (int, string) {
		var out, errb bytes.Buffer
		code := runMCP(context.Background(), args, strings.NewReader(""), &out, &errb, func(k string) string { return env[k] })
		return code, errb.String()
	}
	if code, msg := run(map[string]string{"FORGELINE_SERVER": "http://x"}); code != 2 || !strings.Contains(msg, "FORGELINE_MCP_TOKEN") {
		t.Errorf("no token: %d %s", code, msg)
	}
	if code, _ := run(map[string]string{"FORGELINE_MCP_TOKEN": "flt_x"}); code != 2 {
		t.Errorf("no server: %d", code)
	}
	if code, msg := run(map[string]string{"FORGELINE_MCP_TOKEN": "flt_x"}, "--server", "file:///etc/passwd"); code != 2 || !strings.Contains(msg, "http") {
		t.Errorf("a file URL: %d %s", code, msg)
	}
	// A server that cannot be reached becomes a JSON-RPC error for a request, silence for a notification.
	var out bytes.Buffer
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	env := map[string]string{"FORGELINE_MCP_TOKEN": "flt_x", "FORGELINE_SERVER": dead.URL}
	runMCP(context.Background(), nil, strings.NewReader(`{"jsonrpc":"2.0","id":4,"method":"ping"}`+"\n"+`{"jsonrpc":"2.0","method":"notifications/x"}`+"\n"), &out, io.Discard, func(k string) string { return env[k] })
	if !strings.Contains(out.String(), `"id":4`) || strings.Count(out.String(), "\n") != 1 {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestMCPProxyRefusesPlainHTTPToARemoteHostButAllowsThisMachine(t *testing.T) {
	for _, tc := range []struct {
		url  string
		code int
	}{{"http://forgeline.example.com", 2}, {"http://10.0.0.5:8080", 2}, {"http://localhost:1", 0}, {"http://127.0.0.1:1", 0}, {"http://[::1]:1", 0}} {
		var errb bytes.Buffer
		env := map[string]string{"FORGELINE_MCP_TOKEN": "flt_x", "FORGELINE_SERVER": tc.url}
		code := runMCP(context.Background(), nil, strings.NewReader(""), io.Discard, &errb, func(k string) string { return env[k] })
		if code != tc.code || tc.code == 2 && !strings.Contains(errb.String(), "clear text") {
			t.Errorf("%s: code %d, %s", tc.url, code, errb.String())
		}
	}
}

func TestMCPProxyAnswersAnOversizedMessageAndKeepsGoing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{}}`))
	}))
	defer srv.Close()
	huge := `{"jsonrpc":"2.0","id":1,"method":"x","params":"` + strings.Repeat("a", mcpLineLimit+100) + `"}`
	in := huge + "\n" + `{"jsonrpc":"2.0","id":2,"method":"ping"}` + "\n"
	var out bytes.Buffer
	env := map[string]string{"FORGELINE_MCP_TOKEN": "flt_x", "FORGELINE_SERVER": srv.URL}
	code := runMCP(context.Background(), nil, strings.NewReader(in), &out, io.Discard, func(k string) string { return env[k] })
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if code != 0 || len(lines) != 2 || !strings.Contains(lines[0], "larger than 1 MiB") || !strings.Contains(lines[1], `"id":2`) {
		t.Errorf("code %d, out %q", code, out.String())
	}
}

func TestTheMCPBridgeCanTalkToAUnixSocketWithTheEngineToken(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "e.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var gotPath, gotAuth string
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "notifications/") {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
	})}
	go srv.Serve(ln)
	defer srv.Close()
	var out, errb bytes.Buffer
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n" + `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	env := map[string]string{"FORGELINE_MCP_SOCKET": sock, "FORGELINE_MCP_PATH": "/mcp/engine/abc123"}
	if code := runMCP(t.Context(), nil, in, &out, &errb, func(k string) string { return env[k] }); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if gotPath != "/mcp/engine/abc123" || gotAuth != "" {
		t.Errorf("path %q, authorization %q", gotPath, gotAuth)
	}
	if got := strings.TrimSpace(out.String()); got != `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}` {
		t.Errorf("output: %q (a notification must print nothing)", got)
	}
	// Only the engine's own path is allowed: the socket is not a way to reach anything else on the server.
	env["FORGELINE_MCP_PATH"] = "/v1/users"
	errb.Reset()
	if code := runMCP(t.Context(), nil, strings.NewReader(""), &out, &errb, func(k string) string { return env[k] }); code != 2 || !strings.Contains(errb.String(), "/mcp/engine/") {
		t.Errorf("another path: %d %s", code, errb.String())
	}
}

// An agent that takes only HTTP MCP servers (the Copilot CLI ignores a stdio one, checked against the real program) reaches
// Forgeline's tools through a listener on the container's loopback, which forwards to the engine socket.
func TestTheMCPBridgeCanListenOnLoopbackAndForwardToTheEngineSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "e.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var gotPath, gotAuth, gotQuery, gotBody string
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotPath, gotAuth, gotQuery, gotBody = r.URL.Path, r.Header.Get("Authorization"), r.URL.RawQuery, string(b)
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":7,"result":{"tools":[]}}`)
	})}
	go srv.Serve(ln)
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	addr := make(chan net.Addr, 1)
	done := make(chan int, 1)
	go func() {
		done <- serveMCPHTTP(ctx, "127.0.0.1:0", sock, "/mcp/engine/abc123", io.Discard, func(a net.Addr) { addr <- a })
	}()
	a := <-addr
	// The client names another path, a query and its own credentials: none of it reaches the engine.
	req, _ := http.NewRequest("POST", "http://"+a.String()+"/v1/users?x=1", strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer client-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(out), `"tools"`) {
		t.Errorf("answer %d %s", resp.StatusCode, out)
	}
	if gotPath != "/mcp/engine/abc123" || gotAuth != "" || gotQuery != "" || !strings.Contains(gotBody, "tools/list") {
		t.Errorf("engine saw path %q, authorization %q, query %q, body %q", gotPath, gotAuth, gotQuery, gotBody)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Errorf("exit %d", code)
	}

	// It listens only on this machine, and only for the engine's own path.
	var errb bytes.Buffer
	env := map[string]string{"FORGELINE_MCP_SOCKET": sock, "FORGELINE_MCP_PATH": "/mcp/engine/abc123"}
	for name, args := range map[string][]string{"all interfaces": {"--listen", "0.0.0.0:18081"}, "another host": {"--listen", "10.0.0.5:18081"}, "no port": {"--listen", "127.0.0.1"}} {
		errb.Reset()
		if code := runMCP(t.Context(), args, strings.NewReader(""), io.Discard, &errb, func(k string) string { return env[k] }); code != 2 || !strings.Contains(errb.String(), "loopback") {
			t.Errorf("%s: %d %s", name, code, errb.String())
		}
	}
	errb.Reset()
	if code := runMCP(t.Context(), []string{"--listen", "127.0.0.1:0"}, strings.NewReader(""), io.Discard, &errb, func(string) string { return "" }); code != 2 || !strings.Contains(errb.String(), "FORGELINE_MCP_SOCKET") {
		t.Errorf("without the sandbox: %d %s", code, errb.String())
	}
	env["FORGELINE_MCP_PATH"] = "/v1/users"
	errb.Reset()
	if code := runMCP(t.Context(), []string{"--listen", "127.0.0.1:0"}, strings.NewReader(""), io.Discard, &errb, func(k string) string { return env[k] }); code != 2 || !strings.Contains(errb.String(), "/mcp/engine/") {
		t.Errorf("another path: %d %s", code, errb.String())
	}
}
