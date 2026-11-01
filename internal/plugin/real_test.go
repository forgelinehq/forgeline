package plugin

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRealDocker runs the sample plugin in a real container. Build it with
// scripts/dev/sample-plugin/build.sh and set FORGELINE_SAMPLE_PLUGIN_IMAGE to the id it prints.
func TestRealDocker(t *testing.T) {
	image := os.Getenv("FORGELINE_SAMPLE_PLUGIN_IMAGE")
	if image == "" {
		t.Skip("set FORGELINE_SAMPLE_PLUGIN_IMAGE to run against a real Docker")
	}
	r := &Runner{Docker: "docker", Limits: Limits{Timeout: 60 * time.Second}}
	ctx := context.Background()
	if err := r.Ensure(ctx, image); err != nil {
		t.Fatal(err)
	}
	spec := Spec{ID: "sample", Image: image, Env: map[string]string{"SAMPLE_KEY": "k"}}
	tools, err := r.Tools(ctx, spec)
	if err != nil || len(tools) != 2 {
		t.Fatalf("tools = %v, %v", tools, err)
	}
	if out, err := r.Call(ctx, spec, "shout", map[string]any{"text": "hello"}); err != nil || out != "HELLO" {
		t.Fatalf("shout = %q, %v", out, err)
	}
	out, err := r.Call(ctx, spec, "probe", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"secret=true", "network=false", "dns=false", "writable_root=false", "uid=65534"} {
		if !strings.Contains(out, want) {
			t.Errorf("probe = %q, want %s", out, want)
		}
	}
	// Granting network changes only that.
	spec.Network = true
	if out, _ := r.Call(ctx, spec, "probe", nil); !strings.Contains(out, "writable_root=false") {
		t.Errorf("probe with network = %q", out)
	}
}

// TestRealEgress runs the sample plugin with a host list against a real Docker daemon: the private
// network, the proxy on its gateway, and what the container can and cannot reach.
func TestRealEgress(t *testing.T) {
	image := os.Getenv("FORGELINE_SAMPLE_PLUGIN_IMAGE")
	if image == "" {
		t.Skip("set FORGELINE_SAMPLE_PLUGIN_IMAGE to run against a real Docker")
	}
	// A server on this machine stands in for an allowed host. Names resolve to it through a test
	// resolver, and the proxy is allowed to reach loopback only in the first runner.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "hi from the allowed host") }))
	resolve := func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("127.0.0.1")}, nil }
	allowed := fmt.Sprintf("svc.example.com:%d", port)

	var mu sync.Mutex
	var reports []EgressReport
	r := &Runner{Docker: "docker", Limits: Limits{Timeout: 60 * time.Second}, allowPrivate: true, testResolve: resolve,
		Report: func(_ Spec, rep EgressReport) { mu.Lock(); reports = append(reports, rep); mu.Unlock() }}
	ctx := context.Background()
	if err := r.Ensure(ctx, image); err != nil {
		t.Fatal(err)
	}
	spec := Spec{ID: "sample", Image: image, Hosts: []string{allowed}}
	probe := func(r *Runner, target string) string {
		out, err := r.Call(ctx, spec, "probe", map[string]any{"connect": target})
		if err != nil {
			t.Fatalf("probe %s: %v", target, err)
		}
		return out
	}

	// Listed host: the proxy opens the tunnel and the plugin reads the far end's answer.
	out := probe(r, allowed)
	if !strings.Contains(out, "via_proxy=200 upstream=HTTP/1.0 200 OK") {
		t.Errorf("allowed host: %q", out)
	}
	// Nothing else is reachable: not another name, not another port of the allowed name, not a direct
	// connection, not DNS.
	for _, want := range []string{"network=false", "dns=false", "writable_root=false", "uid=65534"} {
		if !strings.Contains(out, want) {
			t.Errorf("probe = %q, want %s", out, want)
		}
	}
	// A client without this call's credential (another container that can reach the gateway address)
	// is turned away, even for a host on the list.
	if out, err := r.Call(ctx, spec, "probe", map[string]any{"connect": allowed, "noauth": true}); err != nil || !strings.Contains(out, "via_proxy=407") {
		t.Errorf("a request without the call's credential: %q, %v", out, err)
	}
	if out := probe(r, fmt.Sprintf("other.example.com:%d", port)); !strings.Contains(out, "via_proxy=403") {
		t.Errorf("a host that was not granted: %q", out)
	}
	if out := probe(r, fmt.Sprintf("svc.example.com:%d", port+1)); !strings.Contains(out, "via_proxy=403") {
		t.Errorf("a port that was not granted: %q", out)
	}
	if out := probe(r, fmt.Sprintf("127.0.0.1:%d", port)); !strings.Contains(out, "via_proxy=403") {
		t.Errorf("an IP address: %q", out)
	}

	// The same allowed name is refused when the proxy is not allowed to reach private addresses,
	// which is the real configuration: a name that resolves to this machine is never tunnelled to.
	strict := &Runner{Docker: "docker", Limits: Limits{Timeout: 60 * time.Second}, testResolve: resolve, Report: r.Report}
	if out := probe(strict, allowed); !strings.Contains(out, "via_proxy=403") {
		t.Errorf("a name that resolves to loopback was tunnelled: %q", out)
	}

	// Everything was cleaned up: no network and no container is left.
	nets, _ := exec.Command("docker", "network", "ls", "--filter", "label="+sweepLabel, "--format", "{{.Name}}").Output()
	if strings.TrimSpace(string(nets)) != "" {
		t.Errorf("networks left behind: %s", nets)
	}
	left, _ := exec.Command("docker", "ps", "-aq", "--filter", "name=forgeline-plugin-").Output()
	if strings.TrimSpace(string(left)) != "" {
		t.Errorf("containers left behind: %s", left)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reports) == 0 || reports[0].Allowed["svc.example.com"] != 1 || reports[0].Denied != nil && len(reports[0].Denied) != 0 {
		t.Errorf("reports = %+v", reports)
	}
	var foreign int
	for _, rep := range reports {
		foreign += rep.Unauthorized
	}
	if foreign != 1 {
		t.Errorf("requests without the credential were counted %d times, want 1", foreign)
	}
	var denied int
	for _, rep := range reports {
		for _, n := range rep.Denied {
			denied += n
		}
	}
	if denied < 4 {
		t.Errorf("denied %d in total: %+v", denied, reports)
	}
}

// TestRealEgressPublicInternet is the production configuration end to end: no test hooks, the
// system resolver, a real public host. It needs Docker and internet access.
func TestRealEgressPublicInternet(t *testing.T) {
	image := os.Getenv("FORGELINE_SAMPLE_PLUGIN_IMAGE")
	if image == "" || os.Getenv("FORGELINE_REAL_INTERNET") == "" {
		t.Skip("set FORGELINE_SAMPLE_PLUGIN_IMAGE and FORGELINE_REAL_INTERNET=1")
	}
	var mu sync.Mutex
	var denied []string
	r := &Runner{Docker: "docker", Limits: Limits{Timeout: 60 * time.Second},
		Report: func(_ Spec, rep EgressReport) {
			mu.Lock()
			defer mu.Unlock()
			for h := range rep.Denied {
				denied = append(denied, h)
			}
		}}
	ctx := context.Background()
	spec := Spec{ID: "sample", Image: image, Hosts: []string{"example.com:80", "localtest.me:80"}}
	probe := func(target string) string {
		out, err := r.Call(ctx, spec, "probe", map[string]any{"connect": target})
		if err != nil {
			t.Fatalf("probe %s: %v", target, err)
		}
		return out
	}
	if out := probe("example.com:80"); !strings.Contains(out, "via_proxy=200 upstream=HTTP/1.") || strings.Contains(out, "network=true") || strings.Contains(out, "dns=true") {
		t.Errorf("a granted public host: %q", out)
	}
	if out := probe("example.org:80"); !strings.Contains(out, "via_proxy=403") {
		t.Errorf("a host that was not granted: %q", out)
	}
	if out := probe("example.com:443"); !strings.Contains(out, "via_proxy=403") {
		t.Errorf("a port that was not granted: %q", out)
	}
	// localtest.me is a public name whose address is 127.0.0.1: listed, but never tunnelled to.
	if out := probe("localtest.me:80"); !strings.Contains(out, "via_proxy=403") {
		t.Errorf("a granted name that resolves to loopback: %q", out)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(denied) < 3 {
		t.Errorf("denied = %v", denied)
	}
}
