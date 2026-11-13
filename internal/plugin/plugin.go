// Package plugin runs third-party MCP servers shipped as container images (design note 018). Every
// call starts a fresh container and speaks MCP over its standard input and output. What the
// container may do is decided here, by a fixed command line, never by the plugin's own manifest.
package plugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/forgelinehq/forgeline/internal/mcp"
)

var (
	// A pinned image is name@sha256:digest, or a local image's id (sha256:...), which is a content hash too.
	imageRE = regexp.MustCompile(`^([a-z0-9][a-z0-9._/-]{0,200}@)?sha256:[0-9a-f]{64}$`)
	envRE   = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	idRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}$`)
)

// Spec is what an administrator approved for one plugin.
type Spec struct {
	ID      string
	Image   string            // name@sha256:digest or sha256:id; a tag alone is refused
	Network bool              // false is --network none; true is the whole network (not with Hosts)
	Hosts   []string          // if set, only these hosts (name, *.suffix, optional :port) through Forgeline's proxy; no other route out
	Env     map[string]string // granted secrets, passed for one call and never put on a command line
	// ImageUser runs the container as the user its image names (often root) instead of nobody (65534). An
	// administrator opts in per server for images that cannot start otherwise; every other limit still applies.
	ImageUser bool
}

// Limits bound one call.
type Limits struct {
	Memory    string // docker --memory, e.g. "256m"
	CPUs      string // docker --cpus
	Pids      int
	Timeout   time.Duration
	MaxOutput int // bytes of tool text kept
}

// DefaultLimits are used when a Runner has none.
var DefaultLimits = Limits{Memory: "256m", CPUs: "0.5", Pids: 64, Timeout: 30 * time.Second, MaxOutput: 32 << 10}

// Check refuses a spec that could not be run safely.
func (s Spec) Check() error {
	if !idRE.MatchString(s.ID) {
		return fmt.Errorf("plugin id %q is not a lowercase slug", s.ID)
	}
	if !imageRE.MatchString(s.Image) {
		return errors.New("the image must be pinned by digest (name@sha256:...)")
	}
	for k := range s.Env {
		if !envRE.MatchString(k) {
			return fmt.Errorf("%q is not a valid environment variable name", k)
		}
	}
	if len(s.Hosts) > 0 {
		if s.Network {
			return errors.New("a plugin gets either the whole network or a list of hosts, not both")
		}
		if _, err := ParseHosts(s.Hosts); err != nil {
			return err
		}
	}
	return nil
}

// Argv is the whole docker command line for one call. Secret values are not in it: `-e NAME` makes
// docker read the value from its own environment, which keeps them out of the process list.
func Argv(s Spec, l Limits, name string) []string { return argv(s, l, name, "", nil) }

// argv is Argv for a call whose network and proxy settings were prepared: net is the private network
// to attach to (empty means none, or bridge when the spec has Network). proxyEnv entries are
// "NAME=value" (not secret, passed as given) or a bare "NAME" (its value is in the docker client's
// environment, which keeps the proxy credential out of the process list, like a granted secret).
func argv(s Spec, l Limits, name, net string, proxyEnv []string) []string {
	a := []string{"run", "--rm", "-i", "--name", name,
		"--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--init",
		"--pids-limit", fmt.Sprint(l.Pids), "--memory", l.Memory, "--cpus", l.CPUs,
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=16m", "--pull", "never"}
	if !s.ImageUser {
		a = append(a, "--user", "65534:65534")
	}
	switch {
	case net != "":
		a = append(a, "--network", net)
	case s.Network:
		a = append(a, "--network", "bridge")
	default:
		a = append(a, "--network", "none")
	}
	for _, e := range proxyEnv {
		a = append(a, "-e", e)
	}
	keys := make([]string, 0, len(s.Env))
	for k := range s.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a = append(a, "-e", k)
	}
	return append(a, s.Image)
}

// Runner starts plugin containers through a docker binary.
type Runner struct {
	Docker string // path or name of the docker CLI
	Limits Limits
	// ExtraEnv is added to the docker client's environment (settings it needs to reach its daemon).
	ExtraEnv []string
	// Report, when set, is told what the egress proxy did after each call that had a host list.
	Report func(s Spec, r EgressReport)
	// ProxyPorts, when set, is the range of ports the per-call egress proxy listens on (both ends included), so a host
	// firewall can let containers reach just those ports. Zero means any free port.
	ProxyPorts [2]int

	sweepOnce  sync.Once
	sweepKick  chan struct{}
	sweepEvery time.Duration   // 0 means every 5 minutes
	active     map[string]bool // private networks of calls in flight; the sweeper leaves them alone
	// allowPrivate lets the proxy reach loopback; set only by tests that need a server on this machine.
	allowPrivate bool
	testResolve  func(ctx context.Context, host string) ([]net.IP, error)

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (r *Runner) limits() Limits {
	l := r.Limits
	d := DefaultLimits
	if l.Memory == "" {
		l.Memory = d.Memory
	}
	if l.CPUs == "" {
		l.CPUs = d.CPUs
	}
	if l.Pids == 0 {
		l.Pids = d.Pids
	}
	if l.Timeout == 0 {
		l.Timeout = d.Timeout
	}
	if l.MaxOutput == 0 {
		l.MaxOutput = d.MaxOutput
	}
	return l
}

// lock makes calls to one plugin go one at a time.
func (r *Runner) lock(id string) func() {
	r.mu.Lock()
	if r.locks == nil {
		r.locks = map[string]*sync.Mutex{}
	}
	m := r.locks[id]
	if m == nil {
		m = &sync.Mutex{}
		r.locks[id] = m
	}
	r.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// session starts the container, runs fn against it and always removes the container.
func (r *Runner) session(ctx context.Context, s Spec, fn func(context.Context, *mcp.Client, Limits) error) error {
	if err := s.Check(); err != nil {
		return err
	}
	l := r.limits()
	defer r.lock(s.ID)()
	ctx, cancel := context.WithTimeout(ctx, l.Timeout)
	defer cancel()
	b := make([]byte, 6)
	rand.Read(b)
	name := "forgeline-plugin-" + hex.EncodeToString(b)
	netName, proxyEnv, hidden := "", []string(nil), []string(nil)
	var eg *egress
	if len(s.Hosts) > 0 {
		var err error
		if eg, err = r.startEgress(ctx, s); err != nil {
			return err
		}
		defer eg.stop()
		netName, proxyEnv, hidden = eg.network, eg.env, eg.hidden
	}
	cmd := exec.Command(r.Docker, argv(s, l, name, netName, proxyEnv)...)
	cmd.Env = append(append(cleanEnv(s.Env), hidden...), r.ExtraEnv...)
	c, err := mcp.Start(ctx, cmd)
	if err != nil {
		r.kill(name)
		return fmt.Errorf("the plugin did not start: %w", err)
	}
	defer func() { c.Close(); r.kill(name) }()
	err = fn(ctx, c, l)
	if err != nil && eg != nil && eg.proxy != nil && ctx.Err() != nil {
		err = withEgressHint(err, eg.proxy.report())
	}
	return err
}

// withEgressHint explains a call that ran out of time without a single request reaching Forgeline's egress proxy.
// The usual cause is a host firewall (ufw's default, for one) that drops traffic from containers to this machine,
// where the proxy listens; the plugin then waits on a connection that never opens.
func withEgressHint(err error, rep EgressReport) error {
	if len(rep.Allowed)+len(rep.Denied)+len(rep.Failed) > 0 || rep.Unauthorized > 0 {
		return err
	}
	return fmt.Errorf("%w (no request reached Forgeline's egress proxy; if the tool needed the network, a firewall on "+
		"this machine may be blocking containers from reaching it: with ufw, allow traffic from the Docker networks "+
		"to this host)", err)
}

// egress is the private network and proxy set up for one call.
type egress struct {
	r       *Runner
	s       Spec
	network string
	env     []string // for argv: NO_PROXY=... as given, the proxy URLs by name only
	hidden  []string // NAME=value for the docker client's environment (the URLs carry the call's credential)
	proxy   *egressProxy
	cancel  context.CancelFunc
	ln      net.Listener
}

// startEgress creates an internal network for one call, finds its gateway and listens there. If any
// step fails the call is refused: there is no fallback to an open network.
func (r *Runner) startEgress(ctx context.Context, s Spec) (*egress, error) {
	allow, err := ParseHosts(s.Hosts)
	if err != nil {
		return nil, err
	}
	r.StartSweeper(context.Background()) // does not wait: the sweep runs in the background
	b := make([]byte, 6)
	rand.Read(b)
	name := "forgeline-plg-" + hex.EncodeToString(b)
	r.setActive(name, true)
	if out, err := r.docker(ctx, "network", "create", "--internal", "--driver", "bridge", "--label", sweepLabel, name); err != nil {
		r.setActive(name, false)
		return nil, fmt.Errorf("could not set up the plugin's private network: %s", strings.TrimSpace(string(out)))
	}
	e := &egress{r: r, s: s, network: name}
	fail := func(format string, a ...any) (*egress, error) {
		e.stop()
		return nil, fmt.Errorf(format, a...)
	}
	out, err := r.docker(ctx, "network", "inspect", name, "--format", "{{range .IPAM.Config}}{{.Gateway}} {{end}}")
	gw := ""
	for _, f := range strings.Fields(string(out)) {
		if ip := net.ParseIP(f); ip != nil && ip.To4() != nil {
			gw = f
			break
		}
	}
	if err != nil || gw == "" {
		return fail("could not find the private network's gateway address")
	}
	ln, err := r.listenProxy(gw)
	if err != nil {
		return fail("Forgeline cannot listen on the plugin network's gateway (%s), so a host list cannot be enforced here (is the server itself running in a container?): %v", gw, err)
	}
	e.ln = ln
	pctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.proxy = newEgressProxy(allow)
	e.proxy.allowPrivate = r.allowPrivate
	if r.testResolve != nil {
		e.proxy.resolve = r.testResolve
	}
	e.proxy.serve(pctx, ln)
	// The credential is this call's alone; the proxy refuses every request without it (407), so another
	// container that can reach the gateway address cannot use the proxy or be mistaken for the plugin.
	url := "http://forgeline:" + e.proxy.token + "@" + ln.Addr().String()
	e.hidden = []string{"HTTP_PROXY=" + url, "HTTPS_PROXY=" + url, "http_proxy=" + url, "https_proxy=" + url}
	e.env = []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "NO_PROXY=", "no_proxy="}
	return e, nil
}

// stop ends the proxy (closing every tunnel and waiting for them, so nothing moves after the call and
// the report is complete), reports what it did and removes the network. The container is already
// gone by then (it is removed before this runs), so the network is free to go.
func (e *egress) stop() {
	if e.cancel != nil {
		e.cancel()
	}
	if e.ln != nil {
		e.ln.Close()
	}
	if e.proxy != nil {
		e.proxy.shutdown()
		if e.r.Report != nil {
			e.r.Report(e.s, e.proxy.report())
		}
	}
	if e.r.removeNetwork(e.network) {
		e.r.setActive(e.network, false)
		return
	}
	// Still there: leave it to the background sweeper, which is told now.
	e.r.setActive(e.network, false)
	e.r.kickSweep()
}

// removeNetwork removes a network, retrying with a short backoff, and logs what it could not do.
func (r *Runner) removeNetwork(name string) bool {
	var last error
	var out []byte
	delay := 100 * time.Millisecond
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		var err error
		out, err = r.docker(ctx, "network", "rm", name)
		cancel()
		if err == nil {
			return true
		}
		last = err
		time.Sleep(delay)
		delay *= 2
	}
	slog.Warn("could not remove a plugin's private network; the sweeper will retry", "network", name, "err", last, "output", strings.TrimSpace(string(out)))
	return false
}

func (r *Runner) setActive(name string, on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil {
		r.active = map[string]bool{}
	}
	if on {
		r.active[name] = true
	} else {
		delete(r.active, name)
	}
}

func (r *Runner) isActive(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active[name]
}

const sweepLabel = "forgeline.plugin-egress=1"

// StartSweeper removes leftover private networks in the background: now, then every few minutes, and
// whenever a removal failed. It returns at once and starts only once; the server calls it at start
// and a call that uses a host list calls it too, so a call never waits for a sweep.
func (r *Runner) StartSweeper(ctx context.Context) {
	r.sweepOnce.Do(func() {
		r.mu.Lock()
		r.sweepKick = make(chan struct{}, 1)
		kick := r.sweepKick
		every := r.sweepEvery
		r.mu.Unlock()
		if every == 0 {
			every = 5 * time.Minute
		}
		go func() {
			t := time.NewTicker(every)
			defer t.Stop()
			for {
				r.Sweep(ctx)
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				case <-kick:
					time.Sleep(time.Second) // let a failing removal settle
				}
			}
		}()
	})
}

func (r *Runner) kickSweep() {
	r.mu.Lock()
	k := r.sweepKick
	r.mu.Unlock()
	if k != nil {
		select {
		case k <- struct{}{}:
		default:
		}
	}
}

// Sweep removes private networks left behind by a call that was cut short, such as a server that was
// killed, or whose removal failed. Networks of calls in flight are left alone.
func (r *Runner) Sweep(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := r.docker(ctx, "network", "ls", "--filter", "label="+sweepLabel, "--format", "{{.Name}}")
	if err != nil {
		slog.Warn("could not list leftover plugin networks", "err", err)
		return
	}
	for _, n := range strings.Fields(string(out)) {
		if strings.HasPrefix(n, "forgeline-plg-") && !r.isActive(n) {
			if o, err := r.docker(ctx, "network", "rm", n); err != nil {
				slog.Warn("could not remove a leftover plugin network", "network", n, "err", err, "output", strings.TrimSpace(string(o)))
			}
		}
	}
}

// docker runs one docker client command with the clean environment.
func (r *Runner) docker(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.Docker, args...)
	cmd.Env = append(cleanEnv(nil), r.ExtraEnv...)
	return cmd.CombinedOutput()
}

// kill removes a container that may still be running after a timeout. Failing is fine: --rm
// handles the normal exit.
func (r *Runner) kill(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.Docker, "rm", "-f", name)
	cmd.Env = append(cleanEnv(nil), r.ExtraEnv...)
	cmd.Run()
}

// cleanEnv gives the docker client only what it needs to reach the daemon, plus the granted secrets.
func cleanEnv(grants map[string]string) []string {
	var env []string
	for _, k := range []string{"PATH", "HOME", "DOCKER_HOST", "DOCKER_CONFIG", "DOCKER_CONTEXT", "XDG_RUNTIME_DIR"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	for k, v := range grants {
		env = append(env, k+"="+v)
	}
	return env
}

// Tools starts the plugin and lists what it offers, so an install can check the manifest.
func (r *Runner) Tools(ctx context.Context, s Spec) ([]mcp.Tool, error) {
	var out []mcp.Tool
	err := r.session(ctx, s, func(ctx context.Context, c *mcp.Client, _ Limits) (err error) {
		out, err = c.Tools(ctx)
		return
	})
	return out, err
}

// Call runs one tool. The text is cut at MaxOutput; a tool that reports an error is an error.
func (r *Runner) Call(ctx context.Context, s Spec, tool string, args map[string]any) (string, error) {
	var text string
	err := r.session(ctx, s, func(ctx context.Context, c *mcp.Client, l Limits) error {
		res, err := c.Call(ctx, tool, args)
		if err != nil {
			return err
		}
		text = res.Text
		if len(text) > l.MaxOutput {
			text = strings.ToValidUTF8(text[:l.MaxOutput], "") + "\n[output cut]"
		}
		if res.IsError {
			return fmt.Errorf("the plugin reported an error: %s", text)
		}
		return nil
	})
	return text, err
}

// Ping checks that the docker daemon answers.
func (r *Runner) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.Docker, "version", "--format", "{{.Server.Version}}")
	cmd.Env = append(cleanEnv(nil), r.ExtraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker did not answer: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// Ensure makes sure the image is present, pulling it if it is not. A pull by digest cannot return
// different bytes, so what runs is what was pinned.
func (r *Runner) Ensure(ctx context.Context, image string) error {
	run := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, r.Docker, args...)
		cmd.Env = append(cleanEnv(nil), r.ExtraEnv...)
		return cmd.CombinedOutput()
	}
	if err := (Spec{ID: "check", Image: image}).Check(); err != nil {
		return err
	}
	if _, err := run("image", "inspect", "--format", "{{.Id}}", image); err == nil {
		return nil
	}
	if !strings.Contains(image, "@") {
		return errors.New("the image is not on this host and an image id cannot be pulled")
	}
	if out, err := run("pull", "--quiet", image); err != nil {
		return fmt.Errorf("could not pull the image: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// listenProxy listens on the gateway address, on a port in ProxyPorts when that is set.
func (r *Runner) listenProxy(gw string) (net.Listener, error) {
	lo, hi := r.ProxyPorts[0], r.ProxyPorts[1]
	if lo == 0 {
		return net.Listen("tcp", net.JoinHostPort(gw, "0"))
	}
	var last error
	for p := lo; p <= hi; p++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(gw, strconv.Itoa(p)))
		if err == nil {
			return ln, nil
		}
		last = err
	}
	return nil, fmt.Errorf("no free port in %d-%d: %v", lo, hi, last)
}

// ParsePortRange reads "47100-47199" (or one port). Ports below 1024 are refused.
func ParsePortRange(v string) ([2]int, error) {
	lo, hi, found := strings.Cut(strings.TrimSpace(v), "-")
	a, err1 := strconv.Atoi(strings.TrimSpace(lo))
	b := a
	var err2 error
	if found {
		b, err2 = strconv.Atoi(strings.TrimSpace(hi))
	}
	if err1 != nil || err2 != nil || a < 1024 || b > 65535 || b < a {
		return [2]int{}, fmt.Errorf("%q is not a port range such as 47100-47199 (ports 1024 to 65535)", v)
	}
	return [2]int{a, b}, nil
}
