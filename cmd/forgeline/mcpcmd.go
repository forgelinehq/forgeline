package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

const mcpHelp = `usage: forgeline mcp [--server URL] [--listen ADDR]

A stdio MCP server for clients that cannot speak HTTP. It reads one JSON-RPC message per line on
standard input, sends each to the Forgeline server's /mcp endpoint, and writes the answers to standard
output. Create the token on the Account page (API tokens).

Environment:
  FORGELINE_MCP_TOKEN  a personal API token (required; the shared server token does not work)
  FORGELINE_SERVER     server URL, if --server is not given

In the agent sandbox (FORGELINE_MCP_SOCKET and FORGELINE_MCP_PATH set), --listen 127.0.0.1:PORT serves the same
endpoint over HTTP on this machine's loopback instead of over standard input, for agents that only take HTTP MCP servers.
`

const mcpLineLimit = 1 << 20

// runMCP implements `forgeline mcp`.
func runMCP(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, mcpHelp) }
	server := fs.String("server", getenv("FORGELINE_SERVER"), "server URL")
	listen := fs.String("listen", "", "serve over HTTP on this loopback address (agent sandbox only)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *listen != "" {
		sock := getenv("FORGELINE_MCP_SOCKET")
		if sock == "" {
			fmt.Fprintln(stderr, "forgeline: --listen is for the agent sandbox: FORGELINE_MCP_SOCKET and FORGELINE_MCP_PATH must be set")
			return 2
		}
		return serveMCPHTTP(ctx, *listen, sock, getenv("FORGELINE_MCP_PATH"), stderr, nil)
	}
	if sock := getenv("FORGELINE_MCP_SOCKET"); sock != "" {
		return runMCPSocket(ctx, sock, getenv("FORGELINE_MCP_PATH"), stdin, stdout, stderr)
	}
	token := getenv("FORGELINE_MCP_TOKEN")
	if *server == "" || token == "" {
		fmt.Fprintln(stderr, "forgeline: set --server (or FORGELINE_SERVER) and FORGELINE_MCP_TOKEN")
		return 2
	}
	u, err := url.Parse(strings.TrimRight(*server, "/") + "/mcp")
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		fmt.Fprintln(stderr, "forgeline: the server URL must be http:// or https://")
		return 2
	}
	if u.Scheme == "http" && !isLoopback(u.Hostname()) {
		fmt.Fprintln(stderr, "forgeline: the token would cross the network in clear text; use an https:// server URL (http:// is only for this machine)")
		return 2
	}
	client := &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return bridge(ctx, client, u.String(), token, stdin, stdout, stderr)
}

// runMCPSocket is the same bridge for the agent engine (design note 053): Claude Code runs in the sandbox container, which has
// no network route to the Forgeline server, and reaches it through a unix socket the server mounted into the container. The
// path carries a token for one running message, so there is no bearer token here.
func runMCPSocket(ctx context.Context, sock, path string, stdin io.Reader, stdout, stderr io.Writer) int {
	if !strings.HasPrefix(path, "/mcp/engine/") || strings.ContainsAny(path, " ?#\n") {
		fmt.Fprintln(stderr, "forgeline: FORGELINE_MCP_PATH must be /mcp/engine/<token>")
		return 2
	}
	client := &http.Client{
		Timeout: 10 * time.Minute, // a tool can wait for the person to answer a card
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return bridge(ctx, client, "http://forgeline"+path, "", stdin, stdout, stderr)
}

// serveMCPHTTP is the same bridge for an agent that takes only HTTP MCP servers (the Copilot CLI ignores a stdio one): it
// listens on the container's loopback and forwards every request to the engine endpoint on the unix socket. The path is
// fixed to the one token, whatever the client asks for, and the client's own credentials are dropped; only this machine
// can connect because the address must be loopback. ready, if not nil, is told the address once it listens.
func serveMCPHTTP(ctx context.Context, listen, sock, path string, stderr io.Writer, ready func(net.Addr)) int {
	if !strings.HasPrefix(path, "/mcp/engine/") || strings.ContainsAny(path, " ?#\n") {
		fmt.Fprintln(stderr, "forgeline: FORGELINE_MCP_PATH must be /mcp/engine/<token>")
		return 2
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil || !isLoopback(host) {
		fmt.Fprintln(stderr, "forgeline: --listen must be a loopback address such as 127.0.0.1:18081")
		return 2
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		fmt.Fprintf(stderr, "forgeline: %v\n", err)
		return 1
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme, pr.Out.URL.Host, pr.Out.Host = "http", "forgeline", "forgeline"
			pr.Out.URL.Path, pr.Out.URL.RawPath, pr.Out.URL.RawQuery = path, "", ""
			pr.Out.Header.Del("Authorization")
		},
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		}, ResponseHeaderTimeout: 10 * time.Minute}, // a tool can wait for the person to answer a card
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			fmt.Fprintf(stderr, "forgeline: %v\n", err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	srv := &http.Server{Handler: rp, ReadHeaderTimeout: 10 * time.Second}
	go func() { <-ctx.Done(); srv.Close() }()
	if ready != nil {
		ready(ln.Addr())
	}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(stderr, "forgeline: %v\n", err)
		return 1
	}
	return 0
}

// bridge reads one JSON-RPC message per line, sends each to the endpoint and prints the answers.
func bridge(ctx context.Context, client *http.Client, endpoint, token string, stdin io.Reader, stdout, stderr io.Writer) int {
	rd := bufio.NewReaderSize(stdin, 64<<10)
	for {
		line, tooLong, err := readLine(rd)
		if tooLong {
			// The id is unknowable, so answer with a null id like a parse error, and carry on.
			stdout.Write([]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"message larger than 1 MiB"}}` + "\n"))
		} else if line = bytes.TrimSpace(line); len(line) > 0 {
			if out := forwardMCP(ctx, client, endpoint, token, line); out != nil {
				stdout.Write(append(out, '\n'))
			}
		}
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(stderr, "forgeline: reading standard input: %v\n", err)
				return 1
			}
			return 0
		}
	}
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// readLine reads one line, keeping at most mcpLineLimit bytes. A longer line is read to its end and
// reported as tooLong, so one oversized message does not end the session.
func readLine(rd *bufio.Reader) (line []byte, tooLong bool, err error) {
	for {
		part, isPrefix, e := rd.ReadLine()
		if e != nil {
			return line, tooLong, e
		}
		if !tooLong {
			if len(line)+len(part) > mcpLineLimit {
				line, tooLong = nil, true
			} else {
				line = append(line, part...)
			}
		}
		if !isPrefix {
			return line, tooLong, nil
		}
	}
}

// forwardMCP sends one message and returns the line to print, or nil when there is nothing to say
// (a notification the server accepted).
func forwardMCP(ctx context.Context, c *http.Client, endpoint, token string, msg []byte) []byte {
	var head struct {
		ID json.RawMessage `json:"id"`
	}
	_ = json.Unmarshal(msg, &head)
	fail := func(text string) []byte {
		if len(head.ID) == 0 {
			return nil
		}
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": head.ID, "error": map[string]any{"code": -32000, "message": text}})
		return b
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(msg))
	if err != nil {
		return fail("bad server address")
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fail("could not reach the Forgeline server")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusAccepted {
		return nil
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(body, &probe) == nil && probe["jsonrpc"] != nil {
		return bytes.TrimSpace(body)
	}
	var e struct{ Error string }
	_ = json.Unmarshal(body, &e)
	if e.Error == "" {
		e.Error = fmt.Sprintf("the server answered %d", resp.StatusCode)
	}
	return fail(e.Error)
}
