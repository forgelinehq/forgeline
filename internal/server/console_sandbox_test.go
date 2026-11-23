package server

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// dockerStub answers the docker calls the sandbox makes and records them.
type dockerStub struct {
	mu       sync.Mutex
	calls    [][]string
	envs     [][]string // the extra environment each call was given
	limits   []time.Duration
	exists   map[string]bool // containers that exist and run
	exitErr  error
	out      string
	stderr   string
	runtimes string
	ps       string
	noImage  bool // the agent image is not built until docker build is called
	// probe is what asking the sandbox whether an agent program is installed ("command -v") answers: empty means a version
	// line, so a test that is not about missing programs is not bothered; engineMissing means it is not there.
	probe string
	// onExec, if set, sees every `docker exec` and may answer it (handled=true) instead of the plain d.out.
	onExec func(args []string) (out []byte, stderr string, err error, handled bool)
}

func (d *dockerStub) run(ctx context.Context, name string, args ...string) ([]byte, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if name != "docker" {
		return []byte(`{"Account":"111122223333","Arn":"arn:aws:sts::111122223333:assumed-role/Admin/ada","UserId":"AROA:ada"}`), "", nil
	}
	d.calls = append(d.calls, append([]string(nil), args...))
	env, _ := ctx.Value(extraEnvKey{}).([]string)
	d.envs = append(d.envs, env)
	lim, _ := ctx.Value(cmdLimitKey{}).(time.Duration)
	d.limits = append(d.limits, lim)
	switch args[0] {
	case "info":
		return []byte(d.runtimes), "", nil
	case "inspect":
		if d.exists[args[len(args)-1]] {
			return []byte("true\n"), "", nil
		}
		return nil, "No such object", exec.Command("false").Run()
	case "run":
		if d.exists == nil {
			d.exists = map[string]bool{}
		}
		d.exists[args[3]] = true
		return []byte("abc123\n"), "", nil
	case "image":
		if d.noImage {
			return nil, "No such image", exec.Command("false").Run()
		}
		return nil, "", nil
	case "build":
		d.noImage = false
		return nil, "", nil
	case "exec":
		if d.onExec != nil {
			if out, e, err, ok := d.onExec(args); ok {
				return out, e, err
			}
		}
		if last := args[len(args)-1]; strings.Contains(last, "command -v ") {
			if d.probe == "" {
				return []byte("agent 1.0.0\n"), "", nil
			}
			return []byte(d.probe + "\n"), "", nil
		}
		return []byte(d.out), d.stderr, d.exitErr
	case "ps":
		return []byte(d.ps), "", nil
	}
	return nil, "", nil
}

func (d *dockerStub) find(sub string) [][]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out [][]string
	for _, c := range d.calls {
		if c[0] == sub {
			out = append(out, c)
		}
	}
	return out
}

func sandboxHarness(t *testing.T) (*mcpHarness, *dockerStub) {
	t.Helper()
	m := newMCPHarness(t)
	d := &dockerStub{runtimes: `{"runc":{"path":"runc"}}`, out: "hello\n"}
	m.srv.run = d.run
	return m, d
}

func shell(args map[string]any) string { return lookupReply(runShellTool, args) }

func TestTheSandboxIsOffUntilAnAdministratorTurnsItOn(t *testing.T) {
	m, d := sandboxHarness(t)
	if help := m.srv.lookupHelp(context.Background(), "developer"); strings.Contains(help, runShellTool) {
		t.Error("run_shell is offered while the sandbox is off")
	}
	m.ask(m.dev, shell(map[string]any{"command": "echo hi"}))
	if res := m.llm.lastUser(1); !strings.Contains(res, "turned off") || len(d.find("run")) != 0 || len(d.find("exec")) != 0 {
		t.Errorf("off: %s", res)
	}
	m.ask(m.dev, "do you have a shell?")
	if sys := m.lastSystem(); !strings.Contains(sys, "switched off") || !strings.Contains(sys, "Assistant permissions") || strings.Contains(sys, "You cannot change anything") {
		t.Errorf("the assistant is not told the shell exists but is off:\n%s", sys)
	}
	m.dev.expect(403, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true})
	m.admin.expect(400, "PUT", "/v1/assistant/sandbox", map[string]any{"network": "everywhere"})
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true})
	m.ask(m.dev, shell(map[string]any{"command": "echo hi"}))
	if res := m.llm.lastUser(1); !strings.Contains(res, "Exit 0.\nhello") {
		t.Errorf("on: %s", res)
	}
	if strings.Contains(m.lastSystem(), "switched off") {
		t.Error("the assistant is told the shell is off while it is on")
	}
}

// What makes it a sandbox is in the arguments of docker run: nothing of the host is mounted, no privileges, limits.
func TestTheSandboxContainerIsHardenedAndTheCommandIsNotRunByAHostShell(t *testing.T) {
	m, d := sandboxHarness(t)
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true})
	cmd := `echo "$(whoami)"; apt-get install -y python3 && python3 -c 'print(1)' | tee /work/out.txt`
	m.ask(m.dev, shell(map[string]any{"command": cmd, "timeout_seconds": 200}))
	runs := d.find("run")
	if len(runs) != 1 {
		t.Fatalf("docker run calls: %v", runs)
	}
	line := " " + strings.Join(runs[0], " ") + " "
	for _, want := range []string{" -d ", " --cap-drop ALL ", " --security-opt no-new-privileges ", " --memory 2g ", " --pids-limit 512 ", " --cpus 2 ", " --network bridge ", " --label forgeline.sandbox=1 ", " ubuntu:24.04 sleep infinity "} {
		if !strings.Contains(line, want) {
			t.Errorf("docker run lacks %q: %s", want, line)
		}
	}
	for _, never := range []string{"--privileged", "docker.sock", "--pid host", "--pid=host", "--network host", "--network=host", "--ipc", "--userns host", "--cap-add ALL", "--cap-add SYS_ADMIN", "--device", " -v /", "--mount", "--volumes-from", "--runtime"} {
		if strings.Contains(line, never) {
			t.Errorf("docker run has %q: %s", never, line)
		}
	}
	mounts := 0
	for i, a := range runs[0] {
		if a == "-v" {
			mounts++
			if !strings.HasSuffix(runs[0][i+1], "-work:/work") || strings.HasPrefix(runs[0][i+1], "/") {
				t.Errorf("a mount other than the workspace volume: %s", runs[0][i+1])
			}
		}
	}
	if mounts != 1 {
		t.Errorf("%d mounts", mounts)
	}
	ex := d.find("exec")
	if len(ex) != 1 {
		t.Fatalf("exec calls: %v", ex)
	}
	a := ex[0]
	if a[len(a)-1] != cmd || a[len(a)-2] != "-c" || a[len(a)-3] != "bash" || a[len(a)-7] != "timeout" || a[len(a)-4] != "200" {
		t.Errorf("exec does not pass the command as one argument to bash -c under timeout: %v", a)
	}
	if d.limits[len(d.limits)-1] < 230*time.Second {
		t.Errorf("a 200 second command was not given more than the usual host command limit: %v", d.limits[len(d.limits)-1])
	}
	// A second call reuses the container; a different image is a different container; gVisor is used when Docker has it.
	m.ask(m.dev, shell(map[string]any{"command": "ls"}))
	if len(d.find("run")) != 1 {
		t.Error("a running sandbox was started again")
	}
	m.ask(m.dev, shell(map[string]any{"command": "ls", "image": "python:3.13"}))
	if r := d.find("run"); len(r) != 2 || r[1][len(r[1])-3] != "python:3.13" {
		t.Errorf("another image: %v", r)
	}
	d.runtimes = `{"runc":{},"runsc":{"path":"/usr/bin/runsc"}}`
	d.exists = nil
	m.ask(m.dev, shell(map[string]any{"command": "ls", "image": "node:22"}))
	if r := d.find("run"); !strings.Contains(" "+strings.Join(r[len(r)-1], " ")+" ", " --runtime runsc ") {
		t.Errorf("gVisor was not used: %v", r[len(r)-1])
	}
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"network": "none"})
	d.exists = nil
	m.ask(m.dev, shell(map[string]any{"command": "ls", "image": "golang:1.27"}))
	if r := d.find("run"); !strings.Contains(" "+strings.Join(r[len(r)-1], " ")+" ", " --network none ") {
		t.Errorf("network none: %v", r[len(r)-1])
	}
}

func TestShellArgumentsAreChecked(t *testing.T) {
	m, d := sandboxHarness(t)
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true})
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"command": ""}, "command: 1 to"},
		{map[string]any{"command": strings.Repeat("x", maxShellCommand+1)}, "command: 1 to"},
		{map[string]any{"command": "ls", "timeout_seconds": maxShellSeconds + 1}, "timeout_seconds"},
		{map[string]any{"command": "ls", "image": "ubuntu; reboot"}, "image:"},
		{map[string]any{"command": "ls", "image": "-v /:/host"}, "image:"},
		{map[string]any{"command": "ls", "image": "UPPER:case"}, "image:"},
		{map[string]any{"command": "ls", "secrets": []string{"A", "B", "C", "D", "E", "F", "G", "H", "I"}}, "secrets: at most"},
	} {
		m.ask(m.dev, shell(c.args))
		if res := m.llm.lastUser(1); !strings.Contains(res, c.want) {
			t.Errorf("%v: want %q in %s", c.args, c.want, res)
		}
	}
	if len(d.find("run"))+len(d.find("exec")) != 0 {
		t.Error("a refused call reached docker")
	}
	// Exit codes, timeouts and output.
	d.exitErr = exec.Command("sh", "-c", "exit 3").Run()
	d.out, d.stderr = "partial\n", "boom"
	m.ask(m.dev, shell(map[string]any{"command": "false"}))
	if res := m.llm.lastUser(1); !strings.Contains(res, "Exit 3.") || !strings.Contains(res, "partial") || !strings.Contains(res, "boom") {
		t.Errorf("exit 3: %s", res)
	}
	d.exitErr = exec.Command("sh", "-c", "exit 124").Run()
	m.ask(m.dev, shell(map[string]any{"command": "sleep 999", "timeout_seconds": 5}))
	if res := m.llm.lastUser(1); !strings.Contains(res, "Timed out after 5 seconds") {
		t.Errorf("timeout: %s", res)
	}
	d.exitErr, d.stderr = nil, ""
	d.out = strings.Repeat("a", maxShellOutput) + "MIDDLE" + strings.Repeat("z", maxShellOutput)
	m.ask(m.dev, shell(map[string]any{"command": "yes"}))
	if res := m.llm.lastUser(1); !strings.Contains(res, "bytes left out") || strings.Contains(res, "MIDDLE") || len(res) > 2*maxShellOutput {
		t.Errorf("long output: %d bytes", len(res))
	}
	d.out = "token: ghp_abcdefghijklmnopqrstuvwxyz0123456789 and aws_secret_access_key=abcdEFGHijkl1234\n"
	m.ask(m.dev, shell(map[string]any{"command": "env"}))
	if res := m.llm.lastUser(1); strings.Contains(res, "ghp_abcdef") || strings.Contains(res, "abcdEFGHijkl1234") {
		t.Errorf("credentials in output: %s", res)
	}
}

// A secret reaches a command only on a card an administrator answers in the chat; the model sees the name, the value goes
// through the environment of the docker process, and it is hidden if the command prints it.
func TestASecretGoesToTheSandboxOnlyAfterTheAdministratorApproves(t *testing.T) {
	old := permAskTimeout
	permAskTimeout = 3 * time.Second
	defer func() { permAskTimeout = old }()
	m, d := sandboxHarness(t)
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true})
	m.admin.expect(201, "PUT", "/v1/secrets/DEPLOY_KEY", map[string]any{"value": "s3cr3t-value-xyz-0001", "description": "test"})
	if _, _, err := m.srv.putSecret(context.Background(), "test", "forgeline.internal.token", "internal-value-0002", ""); err != nil {
		t.Fatal(err)
	}
	withSecret := shell(map[string]any{"command": "echo $DEPLOY_KEY", "secrets": []string{"DEPLOY_KEY"}})

	for _, c := range []struct {
		who  string
		args map[string]any
		want string
	}{
		{"dev", map[string]any{"command": "x", "secrets": []string{"DEPLOY_KEY"}}, "needs an administrator"},
		{"admin", map[string]any{"command": "x", "secrets": []string{"DEPLOY_KEY"}}, "nobody is here to give it"},
		{"admin", map[string]any{"command": "x", "secrets": []string{"NOPE"}}, `no secret "NOPE"`},
		{"admin", map[string]any{"command": "x", "secrets": []string{"forgeline.internal.token"}}, "is not a secret name"},
		{"admin", map[string]any{"command": "x", "secrets": []string{"DEPLOY_KEY", "DEPLOY_KEY"}}, "listed twice"},
	} {
		b := m.admin
		if c.who == "dev" {
			b = m.dev
		}
		m.ask(b, shell(c.args))
		if res := m.llm.lastUser(1); !strings.Contains(res, c.want) {
			t.Errorf("%v: want %q in %s", c.args, c.want, res)
		}
	}
	if len(d.find("exec")) != 0 {
		t.Fatal("a refused secret reached docker")
	}

	d.out = "got s3cr3t-value-xyz-0001 from the environment\n"
	st := m.streamScript(m.admin, withSecret, "Done.")
	id := m.waitPending(t)
	time.Sleep(50 * time.Millisecond)
	if len(d.find("exec")) != 0 {
		t.Fatal("the command ran before the card was answered")
	}
	m.admin.expect(400, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "always"}) // not offered for a secret
	m.admin.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "approve"})
	events, res := answerOf(t, st)
	card := eventsNamed(events, "forgeline.permission")
	if len(card) != 1 || card[0]["tool"] != runShellTool || !strings.Contains(card[0]["command"].(string), "echo $DEPLOY_KEY") || !strings.Contains(card[0]["why"].(string), "DEPLOY_KEY") || card[0]["pattern"] != "" {
		t.Errorf("card: %v", card)
	}
	ex := d.find("exec")
	if len(ex) != 1 {
		t.Fatalf("exec: %v", ex)
	}
	joined := strings.Join(ex[0], " ")
	if strings.Contains(joined, "s3cr3t") || !strings.Contains(joined, "-e DEPLOY_KEY ") {
		t.Errorf("the value is on the command line, or the name is missing: %s", joined)
	}
	if got := d.envs[len(d.envs)-1]; len(got) != 1 || got[0] != "DEPLOY_KEY=s3cr3t-value-xyz-0001" {
		t.Errorf("environment: %v", got)
	}
	if res == nil || strings.Contains(res["content"].(string), "s3cr3t-value") || !strings.Contains(res["content"].(string), "[hidden]") {
		t.Errorf("the value came back to the model: %v", res)
	}
	// Denied: nothing runs.
	before := len(d.find("exec"))
	st = m.streamScript(m.admin, withSecret, "ok")
	id = m.waitPending(t)
	m.admin.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "deny"})
	if _, res := answerOf(t, st); res == nil || !strings.Contains(res["content"].(string), "you said no") || len(d.find("exec")) != before {
		t.Errorf("denied: %v", res)
	}
	aud := string(m.admin.expect(200, "GET", "/v1/audit?limit=60", nil))
	for _, want := range []string{"assistant.sandbox.secrets", "approved once: DEPLOY_KEY", "needs an administrator", "run_shell [secrets DEPLOY_KEY]"} {
		if !strings.Contains(aud, want) {
			t.Errorf("audit lacks %q", want)
		}
	}
	if strings.Contains(aud, "s3cr3t-value") {
		t.Error("the value is in the audit log")
	}
}

func TestAgentsEvalsAndTheAPIAndTheReaper(t *testing.T) {
	m, d := sandboxHarness(t)
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": true})
	// An agent cannot be given the tool, and a run that reaches it anyway is refused.
	m.admin.expect(400, "POST", "/v1/ai/agents", agentBody("Coder", nil, []string{runShellTool}, "T0"))
	// A run that reaches the tool anyway (a stored agent from before, a plugin) is refused where the tool runs.
	scoped := context.WithValue(context.Background(), agentScopeKey{}, &agentScope{tools: map[string]bool{runShellTool: true}})
	if _, err := m.srv.lookupRunShell(scoped, json.RawMessage(`{"command":"ls"}`)); err == nil || !strings.Contains(err.Error(), "an agent cannot use the sandbox") || len(d.find("run"))+len(d.find("exec")) != 0 {
		t.Errorf("an agent ran a command: %v", err)
	}
	if !changesSomething(runShellTool) {
		t.Error("an eval would run code")
	}
	// Status, and a reset that only touches the caller's own containers.
	d.ps = "forgeline-sbx-x-aaa\tubuntu:24.04\tUp 3 minutes\n"
	var st struct {
		Enabled    bool
		Runtime    string
		Docker     bool
		Containers []sandboxContainerJSON
	}
	mustJSON(t, m.dev.expect(200, "GET", "/v1/assistant/sandbox", nil), &st)
	if !st.Enabled || !st.Docker || !strings.Contains(st.Runtime, "hardened container") || len(st.Containers) != 1 {
		t.Errorf("status: %+v", st)
	}
	d.ps = "id1\nid2\n"
	m.dev.expect(200, "POST", "/v1/assistant/sandbox/reset", nil)
	_, _, owner := sandboxNames(Principal{Name: "dev", UserID: ""}, "")
	_ = owner
	for _, c := range d.find("ps") {
		if strings.Contains(strings.Join(c, " "), "label=forgeline.owner=") && strings.Contains(strings.Join(c, " "), "-q") {
			if strings.Contains(strings.Join(c, " "), "label=forgeline.sandbox=1") {
				t.Errorf("reset listed every person's sandboxes: %v", c)
			}
		}
	}
	if len(d.find("rm")) != 2 || len(d.find("volume")) != 1 { // two containers, and the workspace volume
		t.Errorf("rm calls: %v, volume calls: %v", d.find("rm"), d.find("volume"))
	}
	// The reaper adopts what it did not start, then removes what has been idle too long.
	d.ps = "forgeline-sbx-a-1\nforgeline-sbx-b-2\n"
	sandboxMu.Lock()
	sandboxUsed = map[string]time.Time{"forgeline-sbx-a-1": time.Now().Add(-2 * sandboxIdle)}
	sandboxMu.Unlock()
	if n := m.srv.reapSandboxes(context.Background(), time.Now()); n != 1 {
		t.Errorf("reaped %d, want the one idle container", n)
	}
	removed := ""
	for _, c := range d.find("rm") {
		removed += strings.Join(c, " ") + ";"
	}
	if !strings.Contains(removed, "forgeline-sbx-a-1") || strings.Contains(removed, "forgeline-sbx-b-2") {
		t.Errorf("removed: %s", removed)
	}
	m.admin.expect(200, "PUT", "/v1/assistant/sandbox", map[string]any{"enabled": false})
	if n := m.srv.reapSandboxes(context.Background(), time.Now().Add(24*time.Hour)); n != 0 {
		t.Error("the reaper ran while the sandbox is off")
	}
}

func mustJSON(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}
