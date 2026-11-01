// Command sample-plugin is a tiny MCP server used to try the plugin runner for real. It offers two
// tools: shout (upper-cases text) and probe (reports what the container can see: whether it has a
// granted secret, the network, DNS and a writable root). probe also takes {"connect": "host:port"}: it
// then asks the proxy named in HTTPS_PROXY for a tunnel to that address and reports the proxy's answer
// (sending the call's credential unless "noauth" is true) and the first line the far end says (see design note 021).
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// viaProxy asks the proxy in HTTPS_PROXY to open a tunnel to target, then says what the far end
// answers to a plain HTTP request.
// The proxy URL carries this call's credential as user info; noauth leaves it out, as a stranger would.
func viaProxy(target string, noauth bool) string {
	u, err := url.Parse(os.Getenv("HTTPS_PROXY"))
	if err != nil || u.Host == "" {
		return "via_proxy=none"
	}
	auth := ""
	if pw, ok := u.User.Password(); ok && !noauth {
		auth = "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+pw)) + "\r\n"
	}
	c, err := net.DialTimeout("tcp", u.Host, 3*time.Second)
	if err != nil {
		return "via_proxy=unreachable"
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", target, target, auth)
	br := bufio.NewReader(c)
	status, _ := br.ReadString('\n')
	f := strings.Fields(status)
	if len(f) < 2 || f[1] != "200" {
		return "via_proxy=" + strings.Join(f[min(1, len(f)):min(2, len(f))], "")
	}
	for { // the rest of the proxy's answer
		if l, err := br.ReadString('\n'); err != nil || l == "\r\n" {
			break
		}
	}
	fmt.Fprintf(c, "GET / HTTP/1.0\r\nHost: %s\r\n\r\n", target)
	first, _ := br.ReadString('\n')
	return "via_proxy=200 upstream=" + strings.TrimSpace(first)
}

func main() {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var m struct {
			ID     *int `json:"id"`
			Method string
			Params struct {
				Name      string
				Arguments map[string]any
			}
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil || m.ID == nil {
			continue
		}
		reply := func(result any) {
			b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *m.ID, "result": result})
			fmt.Println(string(b))
		}
		switch m.Method {
		case "initialize":
			reply(map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "sample", "version": "1"}})
		case "tools/list":
			reply(map[string]any{"tools": []map[string]any{
				{"name": "shout", "description": "upper-case some text", "inputSchema": map[string]any{"type": "object"}},
				{"name": "probe", "description": "report what the container can see", "inputSchema": map[string]any{"type": "object"}},
			}})
		case "tools/call":
			text := ""
			switch m.Params.Name {
			case "shout":
				text = strings.ToUpper(fmt.Sprint(m.Params.Arguments["text"]))
			case "probe":
				_, werr := os.Create("/rootfile")
				_, nerr := net.DialTimeout("tcp", "1.1.1.1:53", 2*time.Second)
				_, derr := net.LookupHost("example.com")
				text = fmt.Sprintf("secret=%t network=%t dns=%t writable_root=%t uid=%d", os.Getenv("SAMPLE_KEY") != "", nerr == nil, derr == nil, werr == nil, os.Getuid())
				if target, ok := m.Params.Arguments["connect"].(string); ok {
					text += " " + viaProxy(target, m.Params.Arguments["noauth"] == true)
				}
			}
			reply(map[string]any{"content": []map[string]any{{"type": "text", "text": text}}})
		}
	}
}
