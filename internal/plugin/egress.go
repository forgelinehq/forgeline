package plugin

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Egress by host (design note 021, part B). A plugin that was granted hosts runs on a private Docker
// network that has no route out. The only address it can reach is the network's gateway, where
// Forgeline listens for the length of one call with the proxy below. The proxy tunnels to a name on
// the allowlist, on an allowed port, and only to a public address it resolved itself.

const (
	maxHosts        = 20
	maxTunnels      = 8
	dialTimeout     = 10 * time.Second
	defaultProxyTLS = 443
)

const (
	defaultEgressBytes int64 = 64 << 20 // total tunnelled in either direction during one call
	defaultTunnelIdle        = 30 * time.Second
)

// budgetKey is the name under which a transfer cut at the byte budget is counted as denied.
const budgetKey = "(byte budget)"

var hostRE = regexp.MustCompile(`^(\*\.)?[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// Allow is one entry of an allowlist: a host name (or *.suffix) and a port.
type Allow struct {
	Host string // lower case, no trailing dot; "*.example.com" matches any name below example.com, not example.com
	Port int
}

func (a Allow) String() string {
	if a.Port == defaultProxyTLS {
		return a.Host
	}
	return fmt.Sprintf("%s:%d", a.Host, a.Port)
}

// ParseHosts reads entries such as "api.example.com", "*.example.com" or "api.example.com:8443".
// An IP address, a bare "*" and a single-label name are refused, so is a list longer than 20.
func ParseHosts(list []string) ([]Allow, error) {
	if len(list) > maxHosts {
		return nil, fmt.Errorf("at most %d hosts", maxHosts)
	}
	seen := map[string]bool{}
	var out []Allow
	for _, e := range list {
		host, port := e, defaultProxyTLS
		if i := strings.LastIndex(e, ":"); i >= 0 {
			p, err := strconv.Atoi(e[i+1:])
			if err != nil || p < 1 || p > 65535 {
				return nil, fmt.Errorf("%q: the port must be 1 to 65535", e)
			}
			host, port = e[:i], p
		}
		if !hostRE.MatchString(host) {
			return nil, fmt.Errorf("%q is not a host name (letters, digits, hyphens, at least two labels; *.name for subdomains; no IP addresses)", e)
		}
		if _, err := netip.ParseAddr(host); err == nil {
			return nil, fmt.Errorf("%q is an IP address; name the host instead", e)
		}
		a := Allow{host, port}
		if seen[a.String()] {
			return nil, fmt.Errorf("%q is listed twice", e)
		}
		seen[a.String()] = true
		out = append(out, a)
	}
	return out, nil
}

// matches reports whether a request for host:port is covered by the entry.
func (a Allow) matches(host string, port int) bool {
	if port != a.Port {
		return false
	}
	if strings.HasPrefix(a.Host, "*.") {
		return strings.HasSuffix(host, a.Host[1:]) && len(host) > len(a.Host)-1
	}
	return host == a.Host
}

// EgressReport says what the proxy did during one call, for the audit log.
type EgressReport struct {
	Allowed map[string]int `json:"allowed"` // host -> connections let through
	Denied  map[string]int `json:"denied"`  // host (as asked for) -> policy refusals: not listed, private address, byte budget
	Failed  map[string]int `json:"failed"`  // host -> allowed, but the name did not resolve, the dial or the transfer failed
	// Unauthorized counts requests that did not carry this call's credential. They did not come from the plugin.
	Unauthorized int   `json:"unauthorized"`
	Bytes        int64 `json:"bytes"`
}

// Refused says whether the proxy turned anything away on policy grounds. Upstream failures for an
// allowed host are not refusals.
func (r EgressReport) Refused() bool { return len(r.Denied) > 0 || r.Unauthorized > 0 }

func (r EgressReport) String() string {
	list := func(m map[string]int) string {
		var s []string
		for h, n := range m {
			s = append(s, fmt.Sprintf("%s×%d", h, n))
		}
		sort.Strings(s)
		return strings.Join(s, ", ")
	}
	return fmt.Sprintf("allowed [%s], denied [%s], failed [%s], %d without the credential, %d bytes", list(r.Allowed), list(r.Denied), list(r.Failed), r.Unauthorized, r.Bytes)
}

// forbiddenIP is every address a plugin must never be tunnelled to, whatever name resolved to it:
// loopback, private ranges, link-local (including the cloud metadata address), unspecified,
// multicast, carrier-grade NAT and other reserved blocks.
func forbiddenIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() || addr.IsUnspecified() || addr.IsInterfaceLocalMulticast() {
		return true
	}
	for _, p := range reservedBlocks {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

var reservedBlocks = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "255.255.255.255/32",
		// Deprecated 6to4 relay anycast and other IETF-reserved IPv4 blocks.
		"192.88.99.0/24", "192.31.196.0/24", "192.52.193.0/24", "192.175.48.0/24",
		// IPv6 ranges that embed an IPv4 address (or route to one): refused whole, whatever they embed.
		"::/96", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16", "2001::/32",
		"100::/64", "2001:db8::/32", "fec0::/10"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

type egressProxy struct {
	allow []Allow
	// resolve and dial are replaced in tests.
	resolve      func(ctx context.Context, host string) ([]net.IP, error)
	dial         func(ctx context.Context, network, addr string) (net.Conn, error)
	allowPrivate bool // tests only: lets the proxy reach loopback
	maxBytes     int64
	idle         time.Duration
	token        string // the credential every request must carry (Proxy-Authorization); random per proxy

	mu           sync.Mutex
	allowed      map[string]int
	denied       map[string]int
	failed       map[string]int
	unauthorized int
	conns        map[net.Conn]struct{}
	closing      bool
	wg           sync.WaitGroup // handlers in flight
	bytes        atomic.Int64
	tunnels      chan struct{}
	srv          *http.Server
	base         context.Context // ends at shutdown
	baseCancel   context.CancelFunc
}

func newEgressProxy(allow []Allow) *egressProxy {
	tok := make([]byte, 24)
	rand.Read(tok)
	base, baseCancel := context.WithCancel(context.Background())
	return &egressProxy{base: base, baseCancel: baseCancel, allow: allow, allowed: map[string]int{}, denied: map[string]int{}, failed: map[string]int{}, conns: map[net.Conn]struct{}{},
		tunnels: make(chan struct{}, maxTunnels), token: hex.EncodeToString(tok),
		maxBytes: defaultEgressBytes, idle: defaultTunnelIdle,
		resolve: func(ctx context.Context, host string) ([]net.IP, error) {
			addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			ips := make([]net.IP, 0, len(addrs))
			for _, a := range addrs {
				ips = append(ips, a.IP)
			}
			return ips, err
		},
		dial: (&net.Dialer{Timeout: dialTimeout}).DialContext}
}

func (p *egressProxy) report() EgressReport {
	p.mu.Lock()
	defer p.mu.Unlock()
	r := EgressReport{Allowed: map[string]int{}, Denied: map[string]int{}, Failed: map[string]int{}, Unauthorized: p.unauthorized, Bytes: p.bytes.Load()}
	for k, v := range p.failed {
		r.Failed[k] = v
	}
	for k, v := range p.allowed {
		r.Allowed[k] = v
	}
	for k, v := range p.denied {
		r.Denied[k] = v
	}
	return r
}

func (p *egressProxy) count(m map[string]int, host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(m) < 200 { // a plugin that asks for endless names must not grow the report without bound
		m[host]++
	} else if _, ok := m[host]; ok {
		m[host]++
	}
}

// normalise lower-cases a name and drops a trailing dot.
func normalise(host string) string { return strings.TrimSuffix(strings.ToLower(host), ".") }

// permitted says whether the allowlist covers host:port.
func (p *egressProxy) permitted(host string, port int) bool {
	if net.ParseIP(host) != nil {
		return false
	}
	for _, a := range p.allow {
		if a.matches(host, port) {
			return true
		}
	}
	return false
}

var (
	errDenied  = errors.New("not on the allowlist")
	errPrivate = errors.New("resolves to a private or reserved address")
	// errUpstream is a failure of an allowed host (DNS, dial, transfer), not a refusal by policy.
	errUpstream = errors.New("upstream failure")
	errBudget   = errors.New("byte budget used up")
)

// connect resolves host, refuses it if any answer is a forbidden address, and dials the address it
// resolved, never the name, so a second lookup cannot give a different answer.
func (p *egressProxy) connect(ctx context.Context, host string, port int) (net.Conn, error) {
	ips, err := p.resolve(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("%w: could not resolve %s", errUpstream, host)
	}
	if !p.allowPrivate {
		for _, ip := range ips {
			if forbiddenIP(ip) {
				return nil, errPrivate
			}
		}
	}
	var last error
	for _, ip := range ips {
		c, err := p.dial(ctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(port)))
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, fmt.Errorf("%w: could not connect to %s: %v", errUpstream, host, last)
}

// authorized checks the per-call credential, sent as Proxy-Authorization Basic (any user name, the
// token as password, which is what a proxy URL with user info produces) or Bearer.
func (p *egressProxy) authorized(r *http.Request) bool {
	scheme, cred, _ := strings.Cut(strings.TrimSpace(r.Header.Get("Proxy-Authorization")), " ")
	cred = strings.TrimSpace(cred)
	tok := ""
	switch strings.ToLower(scheme) {
	case "bearer":
		tok = cred
	case "basic":
		dec, err := base64.StdEncoding.DecodeString(cred)
		if err != nil {
			return false
		}
		_, tok, _ = strings.Cut(string(dec), ":")
	default:
		return false
	}
	return subtle.ConstantTimeCompare([]byte(tok), []byte(p.token)) == 1
}

func (p *egressProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !p.authorized(r) {
		p.mu.Lock()
		p.unauthorized++
		p.mu.Unlock()
		w.Header().Set("Proxy-Authenticate", `Basic realm="forgeline"`)
		http.Error(w, "forgeline: this proxy belongs to one plugin call; its credential is missing or wrong", http.StatusProxyAuthRequired)
		return
	}
	p.mu.Lock()
	if p.closing {
		p.mu.Unlock()
		http.Error(w, "forgeline: the call has ended", http.StatusServiceUnavailable)
		return
	}
	p.wg.Add(1)
	p.mu.Unlock()
	defer p.wg.Done()
	if r.Method == http.MethodConnect {
		p.serveConnect(w, r)
		return
	}
	p.serveForward(w, r)
}

// track remembers a connection so shutdown can close it. It reports false, and closes c, when the
// proxy is already shutting down.
func (p *egressProxy) track(c net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing {
		c.Close()
		return false
	}
	p.conns[c] = struct{}{}
	return true
}

func (p *egressProxy) untrack(c net.Conn) {
	p.mu.Lock()
	delete(p.conns, c)
	p.mu.Unlock()
	c.Close()
}

// shutdown stops accepting, closes every tunnel and every upstream connection, and waits until the
// handlers have returned, so that nothing moves after it and the report is complete.
func (p *egressProxy) shutdown() {
	p.mu.Lock()
	if p.closing {
		p.mu.Unlock()
		p.wg.Wait()
		return
	}
	p.closing = true
	p.baseCancel()
	open := make([]net.Conn, 0, len(p.conns))
	for c := range p.conns {
		open = append(open, c)
	}
	p.mu.Unlock()
	for _, c := range open {
		c.Close()
	}
	if p.srv != nil {
		p.srv.Close()
	}
	p.wg.Wait()
}

// take reserves n bytes of the call's budget, all or nothing. It is shared by every transfer, so
// concurrent ones cannot each spend the whole remainder.
func (p *egressProxy) take(n int64) bool {
	for {
		cur := p.bytes.Load()
		if cur+n > p.maxBytes {
			return false
		}
		if p.bytes.CompareAndSwap(cur, cur+n) {
			return true
		}
	}
}

// upstreamFailed answers for a failed connect: a private address is a refusal, anything else a failure.
func (p *egressProxy) upstreamFailed(w http.ResponseWriter, host string, err error) {
	code := http.StatusBadGateway
	if errors.Is(err, errPrivate) {
		p.count(p.denied, host)
		code = http.StatusForbidden
	} else {
		p.count(p.failed, host)
	}
	http.Error(w, "forgeline: "+err.Error(), code)
}

func splitTarget(hostport string, defPort int) (string, int, bool) {
	host, ps, err := net.SplitHostPort(hostport)
	if err != nil {
		if strings.Contains(hostport, ":") {
			return "", 0, false
		}
		return normalise(hostport), defPort, hostport != ""
	}
	port, err := strconv.Atoi(ps)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, false
	}
	return normalise(host), port, host != ""
}

func (p *egressProxy) serveConnect(w http.ResponseWriter, r *http.Request) {
	host, port, ok := splitTarget(r.Host, defaultProxyTLS)
	if !ok {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !p.permitted(host, port) {
		p.count(p.denied, host)
		http.Error(w, "forgeline: this plugin was not granted "+host, http.StatusForbidden)
		return
	}
	select {
	case p.tunnels <- struct{}{}:
		defer func() { <-p.tunnels }()
	default:
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	ctx, stop := p.requestContext(r)
	defer stop()
	up, err := p.connect(ctx, host, port)
	if err != nil {
		p.upstreamFailed(w, host, err)
		return
	}
	if !p.track(up) {
		return
	}
	defer p.untrack(up)
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "unsupported", http.StatusInternalServerError)
		return
	}
	down, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	if !p.track(down) {
		return
	}
	defer p.untrack(down)
	p.count(p.allowed, host)
	fmt.Fprint(rw, "HTTP/1.1 200 Connection Established\r\n\r\n")
	rw.Flush()
	if n := rw.Reader.Buffered(); n > 0 { // bytes the client sent right behind its CONNECT
		early, _ := rw.Reader.Peek(n)
		if !p.take(int64(n)) {
			p.count(p.denied, budgetKey)
			return
		}
		if _, err := up.Write(early); err != nil {
			return
		}
	}
	p.pipe(down, up)
}

// requestContext is the request's context, also cancelled when the proxy shuts down.
func (p *egressProxy) requestContext(r *http.Request) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(p.base, cancel)
	return ctx, func() { stop(); cancel() }
}

// isTimeout reports whether err is a deadline expiring on a connection.
func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// pipe copies both ways until either side closes or the tunnel as a whole has been quiet for the
// idle time (a tunnel busy in one direction is not idle), within the shared byte budget.
func (p *egressProxy) pipe(a, b net.Conn) {
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	touch := func() { last.Store(time.Now().UnixNano()) }
	quiet := func() time.Duration { return time.Since(time.Unix(0, last.Load())) }
	var wg sync.WaitGroup
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		buf := make([]byte, 32<<10)
		for {
			src.SetReadDeadline(time.Now().Add(p.idle))
			n, err := src.Read(buf)
			if n > 0 {
				touch()
				if !p.take(int64(n)) {
					p.count(p.denied, budgetKey)
					break
				}
				dst.SetWriteDeadline(time.Now().Add(p.idle))
				if _, werr := dst.Write(buf[:n]); werr != nil {
					break
				}
				touch()
			}
			if err != nil {
				if isTimeout(err) && quiet() < p.idle {
					continue // the other direction is alive
				}
				break
			}
		}
		// Closing both ends makes the other copy stop too.
		a.Close()
		b.Close()
	}
	wg.Add(2)
	go cp(a, b)
	go cp(b, a)
	wg.Wait()
}

var hopHeaders = []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"}

// budgetBody counts a request body against the budget.
type budgetBody struct {
	p        *egressProxy
	r        io.ReadCloser
	exceeded *atomic.Bool
}

func (b *budgetBody) Read(buf []byte) (int, error) {
	n, err := b.r.Read(buf)
	if n > 0 && !b.p.take(int64(n)) {
		b.exceeded.Store(true)
		return 0, errBudget
	}
	return n, err
}
func (b *budgetBody) Close() error { return b.r.Close() }

// serveForward handles a plain http:// request in absolute form.
func (p *egressProxy) serveForward(w http.ResponseWriter, r *http.Request) {
	if !r.URL.IsAbs() || r.URL.Scheme != "http" {
		http.Error(w, "forgeline: only http:// requests and CONNECT are proxied", http.StatusBadRequest)
		return
	}
	host, port, ok := splitTarget(r.URL.Host, 80)
	if !ok {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !p.permitted(host, port) {
		p.count(p.denied, host)
		http.Error(w, "forgeline: this plugin was not granted "+host+":"+strconv.Itoa(port), http.StatusForbidden)
		return
	}
	select {
	case p.tunnels <- struct{}{}:
		defer func() { <-p.tunnels }()
	default:
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	ctx, stop := p.requestContext(r)
	defer stop()
	up, err := p.connect(ctx, host, port)
	if err != nil {
		p.upstreamFailed(w, host, err)
		return
	}
	if !p.track(up) {
		return
	}
	defer p.untrack(up)
	// One request per connection, over the address that was checked.
	tr := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return up, nil }, DisableKeepAlives: true, Proxy: nil,
		ResponseHeaderTimeout: p.idle}
	defer tr.CloseIdleConnections()
	out := r.Clone(ctx)
	out.RequestURI = ""
	var exceeded atomic.Bool
	if out.Body != nil && out.Body != http.NoBody {
		out.Body = &budgetBody{p: p, r: out.Body, exceeded: &exceeded}
	}
	for _, h := range hopHeaders {
		out.Header.Del(h)
	}
	resp, err := tr.RoundTrip(out)
	if err != nil {
		if exceeded.Load() {
			p.count(p.denied, budgetKey)
			http.Error(w, "forgeline: the request is over the call's byte budget", http.StatusRequestEntityTooLarge)
			return
		}
		p.count(p.failed, host)
		http.Error(w, "forgeline: the host did not answer", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if left := p.maxBytes - p.bytes.Load(); resp.ContentLength > left {
		p.count(p.denied, budgetKey)
		http.Error(w, "forgeline: the response is over the call's byte budget", http.StatusBadGateway)
		return
	}
	p.count(p.allowed, host)
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	for _, h := range hopHeaders {
		w.Header().Del(h)
	}
	w.WriteHeader(resp.StatusCode)
	rc := http.NewResponseController(w)
	buf := make([]byte, 32<<10)
	for {
		up.SetReadDeadline(time.Now().Add(p.idle)) // a stalled body must not hold a slot
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if !p.take(int64(n)) {
				p.count(p.denied, budgetKey)
				panic(http.ErrAbortHandler) // the headers are out: reset the connection so the client sees a failure, not a short 200
			}
			rc.SetWriteDeadline(time.Now().Add(p.idle))
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			p.count(p.failed, host)
			panic(http.ErrAbortHandler)
		}
	}
}

// serve answers on ln until ctx ends.
func (p *egressProxy) serve(ctx context.Context, ln net.Listener) {
	p.srv = &http.Server{Handler: p, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: p.idle, MaxHeaderBytes: 16 << 10}
	go func() { <-ctx.Done(); p.shutdown() }()
	go p.srv.Serve(ln)
}
