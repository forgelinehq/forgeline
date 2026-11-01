package plugin

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseHosts(t *testing.T) {
	good, err := ParseHosts([]string{"api.example.com", "*.cdn.example.com", "files.example.com:8443", "a-b.c.io"})
	if err != nil || len(good) != 4 || good[0].Port != 443 || good[2].Port != 8443 || good[1].Host != "*.cdn.example.com" {
		t.Fatalf("good = %+v, %v", good, err)
	}
	for _, bad := range []string{"*", "*.com.", "example", "1.2.3.4", "1.2.3.4:443", "[::1]", "api.example.com:0", "api.example.com:70000", "api.example.com:x",
		"Api.Example.com", "api.example.com/path", "http://api.example.com", "api..example.com", "-a.example.com", "a.example.com ", "**.example.com", "*.*.example.com", "a_b.example.com", "api.example.com:443:1"} {
		if _, err := ParseHosts([]string{bad}); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if _, err := ParseHosts([]string{"a.example.com", "a.example.com"}); err == nil {
		t.Error("a repeated host was accepted")
	}
	var many []string
	for i := 0; i < maxHosts+1; i++ {
		many = append(many, fmt.Sprintf("h%d.example.com", i))
	}
	if _, err := ParseHosts(many); err == nil {
		t.Error("too many hosts were accepted")
	}
}

func TestAllowMatching(t *testing.T) {
	exact := Allow{"api.example.com", 443}
	wild := Allow{"*.example.com", 443}
	for _, c := range []struct {
		a    Allow
		host string
		port int
		want bool
	}{
		{exact, "api.example.com", 443, true},
		{exact, "api.example.com", 80, false},
		{exact, "x.api.example.com", 443, false},
		{exact, "apiXexample.com", 443, false},
		{wild, "a.example.com", 443, true},
		{wild, "a.b.example.com", 443, true},
		{wild, "example.com", 443, false},
		{wild, ".example.com", 443, false},
		{wild, "evil-example.com", 443, false},
		{wild, "a.example.com.evil.net", 443, false},
		{wild, "a.example.com", 8443, false},
	} {
		if got := c.a.matches(c.host, c.port); got != c.want {
			t.Errorf("%v matches(%q, %d) = %v, want %v", c.a, c.host, c.port, got, c.want)
		}
	}
}

// upstream is a TCP server that answers each line with "echo: <line>".
func upstream(t *testing.T) (port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					fmt.Fprintf(c, "echo: %s\n", sc.Text())
				}
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// testProxy starts a proxy on loopback whose names all resolve to 127.0.0.1.
func testProxy(t *testing.T, hosts ...string) (*egressProxy, string) {
	return testProxyWith(t, nil, hosts...)
}

func testProxyWith(t *testing.T, tweak func(*egressProxy), hosts ...string) (*egressProxy, string) {
	t.Helper()
	allow, err := ParseHosts(hosts)
	if err != nil {
		t.Fatal(err)
	}
	p := newEgressProxy(allow)
	p.allowPrivate = true
	if tweak != nil {
		tweak(p)
	}
	p.resolve = func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("127.0.0.1")}, nil }
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p.serve(ctx, ln)
	tokens.Store(ln.Addr().String(), p.token)
	return p, ln.Addr().String()
}

// tokens remembers each test proxy's credential by address, so connectVia can send it.
var tokens sync.Map

func authHeader(addr string) string {
	if tok, ok := tokens.Load(addr); ok {
		return "Proxy-Authorization: Bearer " + tok.(string) + "\r\n"
	}
	return ""
}

// connectVia sends CONNECT and returns the status, and the open connection when it succeeded.
func connectVia(t *testing.T, proxy, target string) (int, net.Conn, *bufio.Reader) {
	t.Helper()
	c, err := net.DialTimeout("tcp", proxy, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", target, target, authHeader(proxy))
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("CONNECT %s: %v", target, err)
	}
	return resp.StatusCode, c, br
}

func TestProxyTunnelsOnlyToAllowedNamesAndPorts(t *testing.T) {
	port := upstream(t)
	p, addr := testProxy(t, fmt.Sprintf("allowed.example.com:%d", port), fmt.Sprintf("*.wild.example.com:%d", port))

	code, c, br := connectVia(t, addr, fmt.Sprintf("allowed.example.com:%d", port))
	if code != 200 {
		t.Fatalf("allowed host: %d", code)
	}
	fmt.Fprint(c, "ping\n")
	if line, _ := br.ReadString('\n'); line != "echo: ping\n" {
		t.Errorf("through the tunnel: %q", line)
	}
	if c2, _, _ := connectVia(t, addr, fmt.Sprintf("ALLOWED.Example.com.:%d", port)); c2 != 200 { // case and a trailing dot do not matter
		t.Errorf("normalised name: %d", c2)
	}
	if c3, _, _ := connectVia(t, addr, fmt.Sprintf("x.wild.example.com:%d", port)); c3 != 200 {
		t.Errorf("wildcard: %d", c3)
	}
	for target, why := range map[string]string{
		fmt.Sprintf("other.example.com:%d", port):     "a host that is not listed",
		fmt.Sprintf("wild.example.com:%d", port):      "the apex of a wildcard",
		fmt.Sprintf("allowed.example.com:%d", port+1): "a port that is not listed",
		"allowed.example.com":                         "no port (443 is not listed)",
		fmt.Sprintf("127.0.0.1:%d", port):             "an IP address, even of an allowed server",
		fmt.Sprintf("[::1]:%d", port):                 "an IPv6 literal",
		"allowed.example.com:notaport":                "a bad port",
	} {
		if code, _, _ := connectVia(t, addr, target); code != 403 && code != 400 {
			t.Errorf("%s (%s): %d", target, why, code)
		}
	}
	r := p.report()
	if r.Allowed["allowed.example.com"] != 2 || r.Allowed["x.wild.example.com"] != 1 || r.Denied["other.example.com"] != 1 || r.Denied["127.0.0.1"] != 1 || r.Bytes == 0 {
		t.Errorf("report = %+v", r)
	}
}

func TestProxyNeverTunnelsToAPrivateOrReservedAddress(t *testing.T) {
	allow, _ := ParseHosts([]string{"svc.example.com"})
	for _, answers := range [][]string{
		{"127.0.0.1"}, {"10.1.2.3"}, {"192.168.0.5"}, {"172.16.0.1"}, {"172.31.255.255"}, {"169.254.169.254"}, {"169.254.1.1"}, {"100.64.0.1"}, {"0.0.0.0"},
		{"::1"}, {"fd00::1"}, {"fe80::1"}, {"::ffff:127.0.0.1"}, {"::ffff:10.0.0.1"}, {"224.0.0.1"}, {"240.0.0.1"}, {"198.18.0.1"}, {"192.0.2.1"},
		{"8.8.8.8", "10.0.0.1"}, {"10.0.0.1", "8.8.8.8"}, // one bad answer among good ones refuses the name
	} {
		dials := 0
		p := newEgressProxy(allow)
		p.resolve = func(context.Context, string) ([]net.IP, error) {
			var ips []net.IP
			for _, a := range answers {
				ips = append(ips, net.ParseIP(a))
			}
			return ips, nil
		}
		p.dial = func(context.Context, string, string) (net.Conn, error) { dials++; return nil, io.EOF }
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		ctx, cancel := context.WithCancel(context.Background())
		p.serve(ctx, ln)
		tokens.Store(ln.Addr().String(), p.token)
		code, _, _ := connectVia(t, ln.Addr().String(), "svc.example.com:443")
		cancel()
		if code != 403 || dials != 0 {
			t.Errorf("answers %v: status %d, dials %d", answers, code, dials)
		}
	}
}

func TestProxyDialsTheAddressItResolvedAndLooksUpOnce(t *testing.T) {
	allow, _ := ParseHosts([]string{"svc.example.com"})
	p := newEgressProxy(allow)
	lookups := 0
	var dialed []string
	p.resolve = func(context.Context, string) ([]net.IP, error) {
		lookups++
		return []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("2606:2800:220:1:248:1893:25c8:1946")}, nil
	}
	p.dial = func(_ context.Context, _, addr string) (net.Conn, error) {
		dialed = append(dialed, addr)
		return nil, io.EOF
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.serve(ctx, ln)
	tokens.Store(ln.Addr().String(), p.token)
	if code, _, _ := connectVia(t, ln.Addr().String(), "svc.example.com:443"); code != 502 {
		t.Errorf("status %d", code)
	}
	if lookups != 1 || len(dialed) != 2 || dialed[0] != "93.184.216.34:443" || !strings.HasPrefix(dialed[1], "[2606:2800:220:1:248:1893:25c8:1946]:443") {
		t.Errorf("lookups %d, dialed %v (it must dial the resolved addresses, never the name)", lookups, dialed)
	}
}

func TestProxyForwardsPlainHTTPForAllowedHostsOnly(t *testing.T) {
	hits := 0
	var leaked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		leaked += r.Header.Get("Proxy-Authorization") + r.Header.Get("Proxy-Connection") + r.Header.Get("Keep-Alive")
		w.Header().Set("Proxy-Authenticate", "x")
		fmt.Fprintf(w, "hello %s %s", r.Host, r.URL.Path)
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	p, addr := testProxy(t, fmt.Sprintf("plain.example.com:%d", port))
	proxyURL, _ := url.Parse("http://user:" + p.token + "@" + addr) // makes the client send Proxy-Authorization
	cl := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
	resp, err := cl.Get(fmt.Sprintf("http://plain.example.com:%d/path", port))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != fmt.Sprintf("hello plain.example.com:%d /path", port) || resp.Header.Get("Proxy-Authenticate") != "" {
		t.Errorf("forwarded: %d %q %v", resp.StatusCode, body, resp.Header)
	}
	if leaked != "" {
		t.Errorf("hop-by-hop headers reached the upstream: %q", leaked)
	}
	before := hits
	resp, err = cl.Get(fmt.Sprintf("http://nope.example.com:%d/", port))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 || hits != before {
		t.Errorf("a host that is not listed: %d (upstream hits %d -> %d)", resp.StatusCode, before, hits)
	}
	// A request that is not in absolute http:// form is not a proxy request, and https goes through CONNECT only.
	for name, req := range map[string]string{
		"origin form": "GET /local HTTP/1.1\r\nHost: plain.example.com\r\n" + authHeader(addr) + "\r\n",
		"https form":  fmt.Sprintf("GET https://plain.example.com:%d/ HTTP/1.1\r\nHost: plain.example.com\r\n%s\r\n", port, authHeader(addr)),
	} {
		c, _ := net.Dial("tcp", addr)
		fmt.Fprint(c, req)
		if line, _ := bufio.NewReader(c).ReadString('\n'); !strings.Contains(line, "400") {
			t.Errorf("%s: %q", name, line)
		}
		c.Close()
	}
	if r := p.report(); r.Allowed["plain.example.com"] != 1 || r.Denied["nope.example.com"] != 1 {
		t.Errorf("report = %+v", r)
	}
}

func TestProxyLimitsConcurrentTunnels(t *testing.T) {
	port := upstream(t)
	_, addr := testProxy(t, fmt.Sprintf("allowed.example.com:%d", port))
	for i := 0; i < maxTunnels; i++ {
		if code, _, _ := connectVia(t, addr, fmt.Sprintf("allowed.example.com:%d", port)); code != 200 {
			t.Fatalf("tunnel %d: %d", i, code)
		}
	}
	if code, _, _ := connectVia(t, addr, fmt.Sprintf("allowed.example.com:%d", port)); code != 503 {
		t.Errorf("one more than the limit: %d", code)
	}
}

func TestProxyStopsAtTheByteBudgetAndWhenIdle(t *testing.T) {
	port := upstream(t)
	_, addr := testProxyWith(t, func(p *egressProxy) { p.maxBytes, p.idle = 2000, 300*time.Millisecond }, fmt.Sprintf("allowed.example.com:%d", port))

	code, c, br := connectVia(t, addr, fmt.Sprintf("allowed.example.com:%d", port))
	if code != 200 {
		t.Fatal(code)
	}
	// Idle: nothing is sent, and the proxy itself closes the tunnel (the client's own deadline is later).
	c.SetDeadline(time.Now().Add(3 * time.Second))
	start := time.Now()
	if _, err := br.ReadByte(); err == nil {
		t.Error("an idle tunnel stayed open")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() || time.Since(start) > 2*time.Second {
		t.Errorf("the proxy did not close the idle tunnel (%v after %v)", err, time.Since(start))
	}
	// Budget: a stream beyond it is cut.
	code, c, br = connectVia(t, addr, fmt.Sprintf("allowed.example.com:%d", port))
	if code != 200 {
		t.Fatal(code)
	}
	go func() {
		for i := 0; i < 200; i++ {
			if _, err := fmt.Fprintf(c, "%s\n", strings.Repeat("x", 100)); err != nil {
				return
			}
		}
	}()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	got, _ := io.ReadAll(br)
	if len(got) > 4000 {
		t.Errorf("the tunnel carried %d bytes with a budget of 2000", len(got))
	}
}

func TestProxyReportStaysBounded(t *testing.T) {
	allow, _ := ParseHosts([]string{"ok.example.com"})
	p := newEgressProxy(allow)
	for i := 0; i < 1000; i++ {
		p.count(p.denied, fmt.Sprintf("h%d.evil.example", i))
	}
	if n := len(p.report().Denied); n > 200 {
		t.Errorf("the report holds %d distinct names", n)
	}
}

func TestProxyHandlesManyAtOnce(t *testing.T) {
	port := upstream(t)
	_, addr := testProxy(t, fmt.Sprintf("allowed.example.com:%d", port))
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code, c, br := connectVia(t, addr, fmt.Sprintf("allowed.example.com:%d", port))
			if code != 200 {
				t.Errorf("%d: %d", i, code)
				return
			}
			fmt.Fprintf(c, "n%d\n", i)
			if line, _ := br.ReadString('\n'); line != fmt.Sprintf("echo: n%d\n", i) {
				t.Errorf("%d: %q", i, line)
			}
		}(i)
	}
	wg.Wait()
}

func TestAnIPLiteralIsNeverPermittedEvenIfListed(t *testing.T) {
	p := newEgressProxy([]Allow{{"127.0.0.1", 443}, {"10.0.0.5", 443}})
	for _, h := range []string{"127.0.0.1", "10.0.0.5", "::1"} {
		if p.permitted(h, 443) {
			t.Errorf("%s was permitted", h)
		}
	}
}

// sink accepts connections, counts what it reads and never writes.
func sink(t *testing.T) (port int, received *atomicCount) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	received = &atomicCount{}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					received.add(n)
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, received
}

type atomicCount struct {
	mu sync.Mutex
	n  int
}

func (a *atomicCount) add(n int) { a.mu.Lock(); a.n += n; a.mu.Unlock() }
func (a *atomicCount) get() int  { a.mu.Lock(); defer a.mu.Unlock(); return a.n }

func TestATunnelBusyInOneDirectionIsNotIdle(t *testing.T) {
	port, got := sink(t)
	_, addr := testProxyWith(t, func(p *egressProxy) { p.idle = 300 * time.Millisecond }, fmt.Sprintf("up.example.com:%d", port))
	code, c, _ := connectVia(t, addr, fmt.Sprintf("up.example.com:%d", port))
	if code != 200 {
		t.Fatal(code)
	}
	// A long upload to a server that says nothing for longer than the idle time.
	for i := 0; i < 12; i++ { // 12 x 100ms is 4 idle periods
		if _, err := fmt.Fprintf(c, "chunk %d\n", i); err != nil {
			t.Fatalf("the tunnel was cut after %d chunks although the client was sending: %v", i, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got.get() < 12*8 {
		t.Errorf("the far end received only %d bytes", got.get())
	}
	// Truly quiet both ways: the proxy closes it.
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil || isTimeout(err) {
		t.Errorf("a tunnel quiet in both directions stayed open: %v", err)
	}
}

func TestTheProxyRefusesRequestsWithoutThisCallsCredential(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; fmt.Fprint(w, "ok") }))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	p, addr := testProxy(t, fmt.Sprintf("svc.example.com:%d", port))
	target := fmt.Sprintf("svc.example.com:%d", port)

	do := func(method, header string) int {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		if method == "CONNECT" {
			fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", target, target, header)
		} else {
			fmt.Fprintf(c, "GET http://%s/ HTTP/1.1\r\nHost: %s\r\n%s\r\n", target, target, header)
		}
		resp, err := http.ReadResponse(bufio.NewReader(c), nil)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}
	basic := func(user, pw string) string {
		return "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pw)) + "\r\n"
	}
	bad := []string{"", "Proxy-Authorization: Bearer wrong\r\n", "Proxy-Authorization: Bearer \r\n", basic("forgeline", "wrong"), basic("forgeline", ""),
		basic("", p.token[:len(p.token)-1]), "Proxy-Authorization: Basic !!!\r\n", "Proxy-Authorization: " + p.token + "\r\n", "Proxy-Authorization: Digest " + p.token + "\r\n",
		"Authorization: Bearer " + p.token + "\r\n"} // the right token in the wrong header is not a credential
	for _, h := range bad {
		for _, m := range []string{"CONNECT", "GET"} {
			if code := do(m, h); code != 407 {
				t.Errorf("%s with %q: %d, want 407", m, h, code)
			}
		}
	}
	if hits != 0 {
		t.Errorf("the upstream was reached %d times by unauthenticated requests", hits)
	}
	if r := p.report(); r.Unauthorized != len(bad)*2 || len(r.Allowed) != 0 || !r.Refused() {
		t.Errorf("report = %+v", r)
	}
	// The right credential, as Basic (any user name) or Bearer, for both kinds of request.
	for _, h := range []string{basic("forgeline", p.token), basic("anyone", p.token), "Proxy-Authorization: Bearer " + p.token + "\r\n", "proxy-authorization: bearer " + p.token + "\r\n"} {
		if code := do("GET", h); code != 200 {
			t.Errorf("GET with %q: %d", h, code)
		}
		if code := do("CONNECT", h); code != 200 {
			t.Errorf("CONNECT with %q: %d", h, code)
		}
	}
}

func TestShutdownClosesTunnelsAndWaitsSoTheReportIsComplete(t *testing.T) {
	port := upstream(t)
	p, addr := testProxy(t, fmt.Sprintf("allowed.example.com:%d", port))
	code, c, br := connectVia(t, addr, fmt.Sprintf("allowed.example.com:%d", port))
	if code != 200 {
		t.Fatal(code)
	}
	fmt.Fprint(c, "ping\n")
	if line, _ := br.ReadString('\n'); line != "echo: ping\n" {
		t.Fatalf("%q", line)
	}
	done := make(chan struct{})
	go func() { p.shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not return: the tunnel was left open")
	}
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := br.ReadByte(); err == nil || isTimeout(err) {
		t.Errorf("the client's tunnel was still open after shutdown: %v", err)
	}
	if r := p.report(); r.Allowed["allowed.example.com"] != 1 || r.Bytes == 0 {
		t.Errorf("report = %+v", r)
	}
	// Nothing new gets through once it has ended.
	if c2, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		c2.SetDeadline(time.Now().Add(time.Second))
		fmt.Fprintf(c2, "CONNECT allowed.example.com:%d HTTP/1.1\r\nHost: x\r\n%s\r\n", port, authHeader(addr))
		if resp, err := http.ReadResponse(bufio.NewReader(c2), nil); err == nil && resp.StatusCode == 200 {
			t.Error("a tunnel was opened after shutdown")
		}
		c2.Close()
	}
}

func TestUpstreamFailuresAreFailuresNotRefusals(t *testing.T) {
	allow, _ := ParseHosts([]string{"svc.example.com", "plain.example.com:80"})
	mk := func(resolve func(context.Context, string) ([]net.IP, error), dial func(context.Context, string, string) (net.Conn, error)) (*egressProxy, string) {
		p := newEgressProxy(allow)
		p.resolve, p.dial = resolve, dial
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		p.serve(ctx, ln)
		tokens.Store(ln.Addr().String(), p.token)
		return p, ln.Addr().String()
	}
	good := func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("93.184.216.34")}, nil }
	// DNS failure.
	p, addr := mk(func(context.Context, string) ([]net.IP, error) { return nil, errors.New("no such host") }, nil)
	if code, _, _ := connectVia(t, addr, "svc.example.com:443"); code != 502 {
		t.Errorf("dns failure: %d", code)
	}
	if r := p.report(); r.Failed["svc.example.com"] != 1 || len(r.Denied) != 0 || r.Refused() {
		t.Errorf("a name that did not resolve was counted as denied: %+v", r)
	}
	// Dial failure.
	p, addr = mk(good, func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("refused") })
	if code, _, _ := connectVia(t, addr, "svc.example.com:443"); code != 502 {
		t.Errorf("dial failure: %d", code)
	}
	if r := p.report(); r.Failed["svc.example.com"] != 1 || len(r.Denied) != 0 || r.Refused() {
		t.Errorf("a dial failure was counted as denied: %+v", r)
	}
	// An upstream that accepts and hangs up (plain HTTP: the round trip fails).
	p, addr = mk(good, func(context.Context, string, string) (net.Conn, error) {
		a, b := net.Pipe()
		b.Close()
		return a, nil
	})
	c, _ := net.Dial("tcp", addr)
	c.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprintf(c, "GET http://plain.example.com/ HTTP/1.1\r\nHost: plain.example.com\r\n%s\r\n", authHeader(addr))
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	c.Close()
	if err != nil || resp.StatusCode != 502 {
		t.Fatalf("round trip failure: %v %v", resp, err)
	}
	if r := p.report(); r.Failed["plain.example.com"] != 1 || len(r.Denied) != 0 || r.Refused() {
		t.Errorf("a failed round trip was counted as denied: %+v", r)
	}
	// A private answer and an unlisted name are still refusals.
	p, addr = mk(func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("10.0.0.1")}, nil }, nil)
	connectVia(t, addr, "svc.example.com:443")
	connectVia(t, addr, "other.example.com:443")
	if r := p.report(); r.Denied["svc.example.com"] != 1 || r.Denied["other.example.com"] != 1 || len(r.Failed) != 0 || !r.Refused() {
		t.Errorf("refusals: %+v", r)
	}
}

func TestTheByteBudgetIsSharedAndCutsTransfersVisibly(t *testing.T) {
	// Two concurrent responses of 800 bytes against a budget of 1000: they must not each get 1000.
	var started sync.WaitGroup
	started.Add(2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started.Done()
		started.Wait() // both requests are in before either answers
		w.Header().Set("Content-Length", "800")
		w.Write([]byte(strings.Repeat("x", 800)))
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	// Without a Content-Length the response is chunked and can only be cut mid-body.
	chunked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 8; i++ {
			w.Write([]byte(strings.Repeat("y", 100)))
			w.(http.Flusher).Flush()
		}
	}))
	defer chunked.Close()
	cport := chunked.Listener.Addr().(*net.TCPAddr).Port
	p, addr := testProxyWith(t, func(p *egressProxy) { p.maxBytes = 1000 }, fmt.Sprintf("a.example.com:%d", port), fmt.Sprintf("c.example.com:%d", cport))
	proxyURL, _ := url.Parse("http://forgeline:" + p.token + "@" + addr)
	cl := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}

	var wg sync.WaitGroup
	var mu sync.Mutex
	clean, failures := 0, 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := cl.Get(fmt.Sprintf("http://a.example.com:%d/", port))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures++
				return
			}
			body, rerr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if rerr != nil || resp.StatusCode != 200 || len(body) != 800 {
				failures++ // a failure, or a short body that the client can tell is short
				return
			}
			clean += len(body)
		}()
	}
	wg.Wait()
	if clean > 1000 || failures == 0 {
		t.Errorf("clean bytes %d (budget 1000), failures %d: concurrent responses each got the whole remainder", clean, failures)
	}
	if n := p.bytes.Load(); n > 1000 {
		t.Errorf("the proxy carried %d bytes with a budget of 1000", n)
	}
	// A chunked body cut at the budget is an error to the client, never a clean short 200.
	p2, addr2 := testProxyWith(t, func(p *egressProxy) { p.maxBytes = 350 }, fmt.Sprintf("c.example.com:%d", cport))
	proxyURL2, _ := url.Parse("http://forgeline:" + p2.token + "@" + addr2)
	cl2 := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL2)}, Timeout: 5 * time.Second}
	resp, err := cl2.Get(fmt.Sprintf("http://c.example.com:%d/", cport))
	if err == nil {
		_, err = io.ReadAll(resp.Body)
		resp.Body.Close()
	}
	if err == nil {
		t.Error("a body cut at the budget looked complete to the client")
	}
	if r := p2.report(); r.Denied[budgetKey] != 1 || !r.Refused() {
		t.Errorf("the cut was not counted: %+v", r)
	}
	if r := p.report(); r.Denied[budgetKey] == 0 {
		t.Errorf("the refusal at the budget was not counted: %+v", r)
	}
}

func TestRequestBodiesAndEarlyTunnelBytesCountAgainstTheBudget(t *testing.T) {
	var got atomicCount
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		got.add(int(n))
		fmt.Fprint(w, "done")
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	p, addr := testProxyWith(t, func(p *egressProxy) { p.maxBytes = 1000 }, fmt.Sprintf("a.example.com:%d", port))
	proxyURL, _ := url.Parse("http://forgeline:" + p.token + "@" + addr)
	cl := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 5 * time.Second}
	resp, err := cl.Post(fmt.Sprintf("http://a.example.com:%d/", port), "application/octet-stream", strings.NewReader(strings.Repeat("u", 5000)))
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == 200 {
			t.Error("an upload far over the budget was accepted")
		}
	}
	if got.get() > 1000 {
		t.Errorf("the upstream received %d bytes of a 1000 byte budget", got.get())
	}
	if r := p.report(); r.Denied[budgetKey] != 1 {
		t.Errorf("report = %+v", r)
	}

	// Bytes the client sends right behind its CONNECT are counted too.
	eport, early := sink(t)
	p2, addr2 := testProxyWith(t, func(p *egressProxy) { p.maxBytes = 1000 }, fmt.Sprintf("e.example.com:%d", eport))
	c, _ := net.Dial("tcp", addr2)
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprintf(c, "CONNECT e.example.com:%d HTTP/1.1\r\nHost: x\r\n%s\r\n%s\n", eport, authHeader(addr2), strings.Repeat("z", 3000))
	br := bufio.NewReader(c)
	if _, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect}); err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, br) // the proxy closes the tunnel
	time.Sleep(100 * time.Millisecond)
	if early.get() != 0 {
		t.Errorf("early bytes over the budget were forwarded: %d bytes", early.get())
	}
	if r := p2.report(); r.Denied[budgetKey] != 1 || p2.bytes.Load() > 1000 {
		t.Errorf("report = %+v, bytes %d", r, p2.bytes.Load())
	}
}

func TestAStalledResponseBodyReleasesItsSlot(t *testing.T) {
	stall := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.Write([]byte("start"))
		w.(http.Flusher).Flush()
		<-stall
	}))
	defer srv.Close()
	defer close(stall)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	p, addr := testProxyWith(t, func(p *egressProxy) { p.idle = 300 * time.Millisecond }, fmt.Sprintf("a.example.com:%d", port))
	proxyURL, _ := url.Parse("http://forgeline:" + p.token + "@" + addr)
	cl := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 10 * time.Second}
	start := time.Now()
	resp, err := cl.Get(fmt.Sprintf("http://a.example.com:%d/", port))
	if err == nil {
		_, err = io.ReadAll(resp.Body)
		resp.Body.Close()
	}
	if err == nil || time.Since(start) > 3*time.Second {
		t.Errorf("a stalled body was not cut off (err %v after %v)", err, time.Since(start))
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(p.tunnels) != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(p.tunnels) != 0 {
		t.Errorf("the stalled transfer still holds %d slot(s)", len(p.tunnels))
	}
	if r := p.report(); r.Failed["a.example.com"] != 1 {
		t.Errorf("report = %+v", r)
	}
}

func TestForbiddenIPCoversTransitionRangesAndReservedBlocks(t *testing.T) {
	for _, s := range []string{
		// 6to4, Teredo and NAT64 embed an IPv4 address that may be loopback, private or metadata.
		"2002:7f00:1::1", "2002:a00:1::", "2002:a9fe:a9fe::1", "2002:808:808::1", "2001:0:4136:e378:8000:63bf:3fff:fdd2", "2001::1",
		"64:ff9b::7f00:1", "64:ff9b::a9fe:a9fe", "64:ff9b::8.8.8.8", "64:ff9b:1::a00:1", "64:ff9b:1:ffff::1", "::7f00:1", "::a00:1",
		"192.88.99.1", "192.31.196.5", "192.52.193.1", "192.175.48.9", "::ffff:192.88.99.1",
		// the ones that were already refused
		"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "fd00::1", "::1", "fe80::1", "ff02::1",
	} {
		if !forbiddenIP(net.ParseIP(s)) {
			t.Errorf("%s was allowed", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "93.184.216.34", "1.1.1.1", "2606:4700:4700::1111", "2001:4860:4860::8888", "2a00:1450:4001::1", "192.88.98.1", "192.88.100.1", "64:ff9a::1"} {
		if forbiddenIP(net.ParseIP(s)) {
			t.Errorf("%s (a public address) was refused", s)
		}
	}
	if !forbiddenIP(nil) || !forbiddenIP(net.IP{1, 2, 3}) {
		t.Error("a malformed address was allowed")
	}
}

func TestShutdownWaitsForHandlersStillInFlightAndCancelsTheirDials(t *testing.T) {
	allow, _ := ParseHosts([]string{"svc.example.com"})
	p := newEgressProxy(allow)
	p.resolve = func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("93.184.216.34")}, nil }
	var finished atomic.Bool
	dialing := make(chan struct{})
	p.dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		close(dialing)
		<-ctx.Done() // a dial that would otherwise take its full timeout
		time.Sleep(200 * time.Millisecond)
		finished.Store(true)
		return nil, ctx.Err()
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	p.serve(context.Background(), ln)
	tokens.Store(ln.Addr().String(), p.token)
	go func() {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err == nil {
			defer c.Close()
			fmt.Fprintf(c, "CONNECT svc.example.com:443 HTTP/1.1\r\nHost: x\r\n%s\r\n", authHeader(ln.Addr().String()))
			io.Copy(io.Discard, c)
		}
	}()
	<-dialing
	start := time.Now()
	p.shutdown()
	if !finished.Load() {
		t.Error("shutdown returned while a handler was still running")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("shutdown took %v: the dial was not cancelled", time.Since(start))
	}
}
