package plugin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func logLines(t *testing.T, log string) []string {
	t.Helper()
	b, _ := os.ReadFile(log)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func linesWith(lines []string, prefix string) []string {
	var out []string
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}

func hostSpec() Spec {
	return Spec{ID: "echo", Image: digest, Hosts: []string{"api.example.com", "*.cdn.example.com:8443"}}
}

func TestHostsRunOnAPrivateNetworkWithAProxyAndCleanUp(t *testing.T) {
	r, log := runner(t, "")
	r.allowPrivate = true
	var mu sync.Mutex
	var reports []EgressReport
	r.Report = func(s Spec, rep EgressReport) { mu.Lock(); reports = append(reports, rep); mu.Unlock() }
	if _, err := r.Call(context.Background(), hostSpec(), "echo", nil); err != nil {
		t.Fatal(err)
	}
	lines := logLines(t, log)
	creates := linesWith(lines, "NET create")
	if len(creates) != 1 || !strings.Contains(creates[0], "--internal") || !strings.Contains(creates[0], "--label forgeline.plugin-egress=1") || !strings.Contains(creates[0], "forgeline-plg-") {
		t.Fatalf("create = %v", creates)
	}
	netName := creates[0][strings.LastIndex(creates[0], " ")+1:]
	argv, _, _ := argvOf(t, log)
	// The container is attached to the private network and nothing else.
	if !has(argv, "--network", netName) || has(argv, "--network", "bridge") || has(argv, "--network", "none") || strings.Count(strings.Join(argv, " "), "--network") != 1 {
		t.Errorf("argv = %v", argv)
	}
	// The proxy address carries this call's credential. It reaches the container through the docker
	// client's environment, never the command line (the process list).
	for _, a := range argv {
		if strings.Contains(a, "@127.0.0.1") || strings.Contains(a, "forgeline:") {
			t.Errorf("the proxy credential is on the command line: %q", a)
		}
	}
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		if !has(argv, "-e", k) {
			t.Errorf("-e %s missing: %v", k, argv)
		}
	}
	raw, _ := os.ReadFile(log)
	var proxyURL string
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(l, "{") {
			var rec struct{ Proxy string }
			json.Unmarshal([]byte(l), &rec)
			proxyURL = rec.Proxy
		}
	}
	u, err := url.Parse(proxyURL)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User == nil {
		t.Fatalf("proxy url = %q (%v)", proxyURL, err)
	}
	if pw, _ := u.User.Password(); len(pw) < 32 {
		t.Errorf("the per-call credential is too short: %q", pw)
	}
	if !has(argv, "-e", "NO_PROXY=") {
		t.Errorf("no NO_PROXY=: a plugin must not be able to bypass the proxy by name: %v", argv)
	}
	// The network is removed after the container.
	rms := linesWith(lines, "NET rm")
	if len(rms) != 1 || !strings.HasSuffix(rms[0], netName) {
		t.Errorf("network not removed: %v", lines)
	}
	idx := func(prefix string) int {
		for i, l := range lines {
			if strings.HasPrefix(l, prefix) {
				return i
			}
		}
		return -1
	}
	if !(idx("NET create") < idx("{") && idx("RM ") < idx("NET rm")) {
		t.Errorf("order: %v", lines)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reports) != 1 || reports[0].Allowed == nil {
		t.Errorf("reports = %+v", reports)
	}
}

func TestAHostListNeverFallsBackToAnOpenNetwork(t *testing.T) {
	// The gateway is not an address of this machine, so the proxy cannot listen there.
	r, log := runner(t, "")
	r.ExtraEnv = append(r.ExtraEnv, "FAKE_GATEWAY=203.0.113.9")
	_, err := r.Call(context.Background(), hostSpec(), "echo", nil)
	if err == nil || !strings.Contains(err.Error(), "cannot listen") {
		t.Fatalf("err = %v", err)
	}
	lines := logLines(t, log)
	if len(linesWith(lines, "{")) != 0 {
		t.Errorf("a container was started: %v", lines)
	}
	if len(linesWith(lines, "NET rm")) != 1 {
		t.Errorf("the network was left behind: %v", lines)
	}
	// No gateway at all, and a network that cannot be created, fail the same way.
	r2, log2 := runner(t, "")
	r2.ExtraEnv = append(r2.ExtraEnv, "FAKE_GATEWAY=none")
	if _, err := r2.Call(context.Background(), hostSpec(), "echo", nil); err == nil || len(linesWith(logLines(t, log2), "{")) != 0 {
		t.Errorf("no gateway: err = %v", err)
	}
	r3, log3 := runner(t, "")
	r3.ExtraEnv = append(r3.ExtraEnv, "FAKE_CREATE_FAILS=1")
	if _, err := r3.Call(context.Background(), hostSpec(), "echo", nil); err == nil || len(linesWith(logLines(t, log3), "{")) != 0 {
		t.Errorf("create fails: err = %v", err)
	}
}

func TestTheNetworkGoesAwayEvenWhenTheCallTimesOut(t *testing.T) {
	r, log := runner(t, "hang")
	r.allowPrivate = true
	r.Limits.Timeout = 700 * time.Millisecond
	if _, err := r.Call(context.Background(), hostSpec(), "echo", nil); err == nil {
		t.Fatal("expected a timeout")
	}
	if n := len(linesWith(logLines(t, log), "NET rm")); n != 1 {
		t.Errorf("network removals = %d", n)
	}
}

func TestSpecChecksHostsBeforeAnythingStarts(t *testing.T) {
	r, log := runner(t, "")
	for name, s := range map[string]Spec{
		"hosts and network": {ID: "echo", Image: digest, Network: true, Hosts: []string{"a.example.com"}},
		"an ip":             {ID: "echo", Image: digest, Hosts: []string{"1.2.3.4"}},
		"a star":            {ID: "echo", Image: digest, Hosts: []string{"*"}},
		"a path":            {ID: "echo", Image: digest, Hosts: []string{"a.example.com/x"}},
	} {
		if _, err := r.Call(context.Background(), s, "echo", nil); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if b, _ := os.ReadFile(log); len(b) != 0 {
		t.Errorf("docker was called: %s", b)
	}
}

func TestACallNeverWaitsForTheSweep(t *testing.T) {
	r, _ := runner(t, "")
	r.ExtraEnv = append(r.ExtraEnv, "FAKE_LS_SLEEP=2s")
	r.allowPrivate = true
	start := time.Now()
	if _, err := r.Call(context.Background(), hostSpec(), "echo", nil); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Errorf("the call took %v: it waited for the sweep", d)
	}
}

func TestTheSweeperRunsInTheBackgroundRepeatsAndSparesCallsInFlight(t *testing.T) {
	r, log := runner(t, "")
	r.ExtraEnv = append(r.ExtraEnv, "FAKE_NETS=forgeline-plg-aaa\nforgeline-plg-bbb\nbridge\nsomeone-elses\n")
	r.sweepEvery = 100 * time.Millisecond
	r.setActive("forgeline-plg-bbb", true) // a call in flight
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.StartSweeper(ctx)
	r.StartSweeper(ctx) // a second start changes nothing
	deadline := time.Now().Add(5 * time.Second)
	for len(linesWith(logLines(t, log), "NET ls")) < 3 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	lines := logLines(t, log)
	if n := len(linesWith(lines, "NET ls")); n < 3 {
		t.Errorf("the sweeper ran %d times; it must repeat", n)
	}
	joined := strings.Join(linesWith(lines, "NET rm"), " ")
	if !strings.Contains(joined, "forgeline-plg-aaa") || strings.Contains(joined, "bbb") || strings.Contains(joined, "bridge") || strings.Contains(joined, "someone-elses") {
		t.Errorf("removed: %v", linesWith(lines, "NET rm"))
	}
}

// lockedBuf is a log sink that is safe to write from the sweeper and read from the test.
type lockedBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func TestNetworkRemovalIsRetriedAndFailureIsLoggedAndSweptLater(t *testing.T) {
	buf := &lockedBuf{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	defer slog.SetDefault(old)

	// Two failures, then success: retried within the call.
	r, log := runner(t, "")
	r.allowPrivate = true
	r.ExtraEnv = append(r.ExtraEnv, "FAKE_RM_FAILS=2")
	if _, err := r.Call(context.Background(), hostSpec(), "echo", nil); err != nil {
		t.Fatal(err)
	}
	if n := len(linesWith(logLines(t, log), "NET rm")); n != 3 {
		t.Errorf("removal attempts = %d, want 3 (two failures, then success)", n)
	}

	// It never succeeds: the failure is logged and the sweeper is told.
	r2, log2 := runner(t, "")
	r2.allowPrivate = true
	r2.ExtraEnv = append(r2.ExtraEnv, "FAKE_RM_FAILS=1000")
	r2.sweepEvery = time.Hour // only a failed removal can make the sweeper run again
	if _, err := r2.Call(context.Background(), hostSpec(), "echo", nil); err != nil {
		t.Fatal(err)
	}
	if n := len(linesWith(logLines(t, log2), "NET rm")); n < 5 {
		t.Errorf("removal attempts = %d, want at least 5", n)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(linesWith(logLines(t, log2), "NET ls")) < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(linesWith(logLines(t, log2), "NET ls")); n < 2 {
		t.Errorf("the sweeper was not told about the network it must retry (%d listings)", n)
	}
	if !strings.Contains(buf.String(), "could not remove a plugin's private network") {
		t.Errorf("the failure was not logged: %q", buf.String())
	}
}

func TestPlainNetworkAndNoNetworkAreUnchanged(t *testing.T) {
	r, log := runner(t, "")
	if _, err := r.Call(context.Background(), Spec{ID: "echo", Image: digest}, "echo", nil); err != nil {
		t.Fatal(err)
	}
	argv, _, _ := argvOf(t, log)
	if !has(argv, "--network", "none") || len(linesWith(logLines(t, log), "NET")) != 0 {
		t.Errorf("argv = %v", argv)
	}
	r2, log2 := runner(t, "")
	if _, err := r2.Call(context.Background(), Spec{ID: "echo", Image: digest, Network: true}, "echo", nil); err != nil {
		t.Fatal(err)
	}
	argv2, _, _ := argvOf(t, log2)
	if !has(argv2, "--network", "bridge") || len(linesWith(logLines(t, log2), "NET")) != 0 {
		t.Errorf("argv = %v", argv2)
	}
}

func TestSpecCheckRefusesBadHostsOnItsOwn(t *testing.T) {
	for _, hosts := range [][]string{{"1.2.3.4"}, {"*"}, {"a.example.com/x"}, {"a.example.com", "a.example.com"}} {
		if err := (Spec{ID: "echo", Image: digest, Hosts: hosts}).Check(); err == nil {
			t.Errorf("%v passed Check", hosts)
		}
	}
	if err := (Spec{ID: "echo", Image: digest, Hosts: []string{"a.example.com", "*.b.example.com:8443"}}).Check(); err != nil {
		t.Error(err)
	}
}

func TestAPrivateNetworkWinsOverTheOpenOne(t *testing.T) {
	l := DefaultLimits
	a := argv(Spec{ID: "echo", Image: digest, Network: true}, l, "n", "forgeline-plg-x", nil)
	if !has(a, "--network", "forgeline-plg-x") || has(a, "--network", "bridge") {
		t.Errorf("argv = %v", a)
	}
}
