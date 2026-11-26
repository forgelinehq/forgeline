package scheduler

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/executor"
	"github.com/forgelinehq/forgeline/internal/pipeline"
)

func plan(t *testing.T, steps ...pipeline.Step) *pipeline.Graph {
	t.Helper()
	g, err := pipeline.Plan(&pipeline.Pipeline{Version: pipeline.VersionV1Alpha1, Name: "p", Steps: steps})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func st(id string, needs ...string) pipeline.Step {
	return pipeline.Step{ID: id, Image: "alpine", Run: "true", Needs: needs}
}

func statuses(r *Report) map[string]Status {
	m := map[string]Status{}
	for _, s := range r.Steps {
		m[s.ID] = s.Status
	}
	return m
}

func failing(ids ...string) *executor.Fake {
	return &executor.Fake{Handler: func(_ context.Context, s executor.StepRun, _ io.Writer) (executor.Result, error) {
		for _, id := range ids {
			if s.ID == id {
				return executor.Result{ExitCode: 2}, nil
			}
		}
		return executor.Result{}, nil
	}}
}

func TestRunSuccess(t *testing.T) {
	g := plan(t, st("a"), st("b", "a"), st("c", "a"), st("d", "b", "c"))
	f := &executor.Fake{}
	r := Run(context.Background(), g, f, Options{Parallelism: 4})
	if !r.OK() || r.Err() != nil {
		t.Fatalf("report = %+v, want all succeeded", r.Steps)
	}
	calls := f.Calls()
	if len(calls) != 4 || calls[0].ID != "a" || calls[3].ID != "d" {
		t.Fatalf("calls = %v, want a first and d last", calls)
	}
}

func TestRunFailureSkipsDependentsOnly(t *testing.T) {
	// a fails; b needs a, c needs b (indirect); x is unrelated.
	g := plan(t, st("a"), st("b", "a"), st("c", "b"), st("x"), st("y", "x"))
	r := Run(context.Background(), g, failing("a"), Options{Parallelism: 2})
	want := map[string]Status{"a": StatusFailed, "b": StatusSkipped, "c": StatusSkipped,
		"x": StatusSucceeded, "y": StatusSucceeded}
	got := statuses(r)
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s = %s, want %s", id, got[id], w)
		}
	}
	if !errors.Is(r.Err(), ErrFailed) {
		t.Fatalf("Err() = %v, want ErrFailed", r.Err())
	}
	for _, s := range r.Steps {
		if s.ID == "a" && s.ExitCode != 2 {
			t.Errorf("a exit code = %d, want 2", s.ExitCode)
		}
	}
}

func TestRunTimeout(t *testing.T) {
	slow := st("slow")
	slow.Timeout = "20ms"
	g := plan(t, slow, st("after", "slow"))
	f := &executor.Fake{Handler: func(ctx context.Context, s executor.StepRun, _ io.Writer) (executor.Result, error) {
		if s.Timeout != 20*time.Millisecond {
			t.Errorf("timeout passed = %v, want 20ms", s.Timeout)
		}
		<-ctx.Done()
		return executor.Result{}, ctx.Err()
	}}
	r := Run(context.Background(), g, f, Options{})
	got := statuses(r)
	if got["slow"] != StatusFailed || got["after"] != StatusSkipped {
		t.Fatalf("statuses = %v, want slow failed and after skipped", got)
	}
	if !errors.Is(r.Steps[0].Err, executor.ErrTimeout) {
		t.Fatalf("slow err = %v, want ErrTimeout", r.Steps[0].Err)
	}
}

func TestRunCancellation(t *testing.T) {
	g := plan(t, st("a"), st("b", "a"), st("c"))
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	var once sync.Once
	f := &executor.Fake{Handler: func(ctx context.Context, s executor.StepRun, _ io.Writer) (executor.Result, error) {
		once.Do(func() { close(started) }) // whichever step gets the only slot first
		<-ctx.Done()
		return executor.Result{}, ctx.Err()
	}}
	go func() { <-started; cancel() }()
	r := Run(ctx, g, f, Options{Parallelism: 1})

	for _, s := range r.Steps {
		if s.Status == StatusSucceeded || s.Status == StatusFailed {
			t.Errorf("%s = %s, want canceled or skipped", s.ID, s.Status)
		}
	}
	got := statuses(r)
	if got["a"] != StatusCanceled || got["c"] != StatusCanceled || got["b"] != StatusSkipped {
		t.Fatalf("statuses = %v, want a and c canceled and b skipped", got)
	}
	if r.OK() {
		t.Fatal("OK() = true after cancellation")
	}
}

func TestRunParallelismLimit(t *testing.T) {
	for _, limit := range []int{1, 2, 3} {
		var cur, max atomic.Int32
		f := &executor.Fake{Handler: func(_ context.Context, _ executor.StepRun, _ io.Writer) (executor.Result, error) {
			n := cur.Add(1)
			for {
				m := max.Load()
				if n <= m || max.CompareAndSwap(m, n) {
					break
				}
			}
			time.Sleep(30 * time.Millisecond)
			cur.Add(-1)
			return executor.Result{}, nil
		}}
		g := plan(t, st("a"), st("b"), st("c"), st("d"), st("e"), st("f"))
		if r := Run(context.Background(), g, f, Options{Parallelism: limit}); !r.OK() {
			t.Fatalf("limit %d: %v", limit, r.Err())
		}
		if int(max.Load()) != limit {
			t.Errorf("limit %d: max concurrent = %d, want %d", limit, max.Load(), limit)
		}
	}
}

func TestRunPassesStepFieldsAndLogs(t *testing.T) {
	step := pipeline.Step{ID: "a", Image: "img", Command: []string{"go", "test"},
		Env: map[string]string{"K": "v"}, WorkDir: "/src"}
	f := &executor.Fake{Handler: func(_ context.Context, _ executor.StepRun, logs io.Writer) (executor.Result, error) {
		io.WriteString(logs, "hello")
		return executor.Result{}, nil
	}}
	var mu sync.Mutex
	got := map[string]string{}
	w := func(id string) io.Writer {
		return writerFunc(func(p []byte) { mu.Lock(); got[id] += string(p); mu.Unlock() })
	}
	Run(context.Background(), plan(t, step), f, Options{Workspace: "/ws", Logs: w})

	c := f.Calls()[0]
	if c.Image != "img" || c.Command[0] != "go" || c.Env["K"] != "v" || c.WorkDir != "/src" || c.Workspace != "/ws" {
		t.Fatalf("StepRun = %+v", c)
	}
	if got["a"] != "hello" {
		t.Fatalf("logs = %v, want hello for a", got)
	}
}

type writerFunc func([]byte)

func (w writerFunc) Write(p []byte) (int, error) { w(p); return len(p), nil }

func TestRunCachedCountsAsSuccess(t *testing.T) {
	g := plan(t, st("a"), st("b", "a"))
	f := &executor.Fake{Handler: func(_ context.Context, s executor.StepRun, _ io.Writer) (executor.Result, error) {
		return executor.Result{Cached: s.ID == "a"}, nil
	}}
	r := Run(context.Background(), g, f, Options{})
	got := statuses(r)
	if got["a"] != StatusCached || got["b"] != StatusSucceeded {
		t.Fatalf("statuses = %v, want a cached and b succeeded", got)
	}
	if !r.OK() || r.Err() != nil {
		t.Fatalf("a cached step should count as success: %v", r.Err())
	}
}

func TestRunCallbacks(t *testing.T) {
	// a fails, b needs a (skipped), c is independent.
	g := plan(t, st("a"), st("b", "a"), st("c"))
	var mu sync.Mutex
	started := map[string]bool{}
	done := map[string]Status{}
	opts := Options{
		Parallelism: 2,
		OnStepStart: func(id string) { mu.Lock(); started[id] = true; mu.Unlock() },
		OnStepDone: func(r StepResult) {
			mu.Lock()
			defer mu.Unlock()
			if !started[r.ID] && r.Status != StatusSkipped {
				t.Errorf("%s finished (%s) without a start callback", r.ID, r.Status)
			}
			if _, dup := done[r.ID]; dup {
				t.Errorf("%s reported done twice", r.ID)
			}
			done[r.ID] = r.Status
		},
	}
	Run(context.Background(), g, failing("a"), opts)
	want := map[string]Status{"a": StatusFailed, "b": StatusSkipped, "c": StatusSucceeded}
	for id, w := range want {
		if done[id] != w {
			t.Errorf("done[%s] = %s, want %s", id, done[id], w)
		}
	}
	if started["b"] {
		t.Error("skipped step b got a start callback")
	}
}

func withSecrets(s pipeline.Step, names ...string) pipeline.Step {
	s.Permissions.Secrets = names
	return s
}

func TestStepsReceiveOnlyTheSecretsTheyList(t *testing.T) {
	g := plan(t,
		withSecrets(st("deploy"), "DEPLOY_KEY"),
		withSecrets(st("both"), "DEPLOY_KEY", "NPM_TOKEN"),
		st("plain"))
	f := &executor.Fake{}
	r := Run(context.Background(), g, f, Options{Parallelism: 1, Secrets: map[string]string{
		"DEPLOY_KEY": "deploy-key-value", "NPM_TOKEN": "npm-token-value", "UNUSED": "unused-secret-value"}})
	if !r.OK() {
		t.Fatalf("report = %+v", r.Steps)
	}
	got := map[string]map[string]string{}
	for _, c := range f.Calls() {
		got[c.ID] = c.Secrets
	}
	if len(got["deploy"]) != 1 || got["deploy"]["DEPLOY_KEY"] != "deploy-key-value" {
		t.Errorf("deploy got %v, want only DEPLOY_KEY", got["deploy"])
	}
	if len(got["both"]) != 2 {
		t.Errorf("both got %v, want two secrets", got["both"])
	}
	if len(got["plain"]) != 0 {
		t.Errorf("a step that lists no secrets got %v", got["plain"])
	}
}

func TestMissingSecretFailsTheStepByName(t *testing.T) {
	g := plan(t, withSecrets(st("a"), "NOT_SET"), st("b", "a"))
	f := &executor.Fake{}
	r := Run(context.Background(), g, f, Options{Secrets: map[string]string{"OTHER": "other-secret-value"}})
	s := statuses(r)
	if s["a"] != StatusFailed || s["b"] != StatusSkipped {
		t.Fatalf("statuses = %v, want a failed and b skipped", s)
	}
	err := r.Steps[0].Err.Error()
	if !contains(err, `"NOT_SET"`) || !contains(err, "FORGELINE_SECRET_NOT_SET") {
		t.Errorf("error %q should name the secret and how to set it", err)
	}
	if contains(err, "other-secret-value") {
		t.Errorf("error leaks a value: %q", err)
	}
	if len(f.Calls()) != 0 {
		t.Errorf("a step with a missing secret must not start; calls = %v", f.Calls())
	}
}

func TestSecretsDeniedRefusesOnlyStepsThatAskForSecrets(t *testing.T) {
	g := plan(t, withSecrets(st("deploy"), "DEPLOY_KEY"), st("test"))
	f := &executor.Fake{}
	r := Run(context.Background(), g, f, Options{Parallelism: 1,
		Secrets: map[string]string{"DEPLOY_KEY": "deploy-key-value"}, SecretsDenied: "this run is untrusted"})
	s := statuses(r)
	if s["deploy"] != StatusFailed || s["test"] != StatusSucceeded {
		t.Fatalf("statuses = %v, want deploy failed and test succeeded", s)
	}
	for _, res := range r.Steps {
		if res.ID == "deploy" && (!contains(res.Err.Error(), "DEPLOY_KEY") || !contains(res.Err.Error(), "untrusted")) {
			t.Errorf("error %q should name the secret and the reason", res.Err)
		}
	}
	for _, c := range f.Calls() {
		if len(c.Secrets) != 0 {
			t.Errorf("step %s received secrets %v from a denied run", c.ID, c.Secrets)
		}
		if c.ID == "deploy" {
			t.Error("the refused step must not start")
		}
	}
}

func TestLogsMaskEverySecretValue(t *testing.T) {
	const own, other = "own-secret-value", "other-secret-value"
	g := plan(t, withSecrets(st("a"), "OWN"))
	f := &executor.Fake{Handler: func(_ context.Context, s executor.StepRun, logs io.Writer) (executor.Result, error) {
		// One value split across writes, one the step never asked for, and a tail that
		// looks like the start of a value and is only released when the step ends.
		io.WriteString(logs, "token="+own[:6])
		io.WriteString(logs, own[6:]+"\nother="+other+"\nend own-sec")
		return executor.Result{ExitCode: 1}, nil // a failing step must still flush
	}}
	var out syncBuf
	Run(context.Background(), g, f, Options{
		Secrets: map[string]string{"OWN": own, "OTHER": other},
		Logs:    func(string) io.Writer { return &out },
	})
	got := out.String()
	if contains(got, own) || contains(got, other) {
		t.Fatalf("log leaks a secret: %q", got)
	}
	if want := "token=***\nother=***\nend own-sec"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestNoSecretsLeavesLogsUntouched(t *testing.T) {
	g := plan(t, st("a"))
	f := &executor.Fake{Handler: func(_ context.Context, _ executor.StepRun, logs io.Writer) (executor.Result, error) {
		io.WriteString(logs, "hello *** world")
		return executor.Result{}, nil
	}}
	var out syncBuf
	Run(context.Background(), g, f, Options{Logs: func(string) io.Writer { return &out }})
	if out.String() != "hello *** world" {
		t.Fatalf("log = %q", out.String())
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  []byte
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.b = append(s.b, p...)
	return len(p), nil
}
func (s *syncBuf) String() string { s.mu.Lock(); defer s.mu.Unlock(); return string(s.b) }

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func TestToStepRunCompilesAgent(t *testing.T) {
	s := &pipeline.Step{ID: "review", Image: "img", Agent: &pipeline.Agent{Provider: pipeline.ProviderClaudeCode, Prompt: "look"}}
	run, err := toStepRun(s, "/ws", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Command) == 0 || run.Command[0] != "claude" || run.Command[len(run.Command)-1] != "look" || run.Script != "" {
		t.Fatalf("Command = %q, Script = %q", run.Command, run.Script)
	}
}

func wfStep(id string, needs ...string) pipeline.Step {
	return pipeline.Step{ID: id, Needs: needs, Workflow: &pipeline.Workflow{Repo: "acme/widgets", Workflow: "ci.yml", Ref: "main"}}
}

func TestWorkflowStepNeedsAServerThatCanRunIt(t *testing.T) {
	g := plan(t, wfStep("ci"), st("after", "ci"))
	f := &executor.Fake{}
	r := Run(context.Background(), g, f, Options{})
	s := statuses(r)
	if s["ci"] != StatusFailed || s["after"] != StatusSkipped || len(f.Calls()) != 0 {
		t.Fatalf("statuses = %v, calls = %v", s, f.Calls())
	}
	if !strings.Contains(r.Steps[0].Err.Error(), "only on a Forgeline server") {
		t.Errorf("err = %v", r.Steps[0].Err)
	}
}

func TestWorkflowStepDeniedAndNeverReachesTheExecutorOrTheHook(t *testing.T) {
	called := false
	f := &executor.Fake{}
	r := Run(context.Background(), plan(t, wfStep("ci")), f, Options{
		WorkflowDenied: "runs started by GitHub may not start workflows",
		Workflow:       func(context.Context, *pipeline.Step, io.Writer) error { called = true; return nil },
	})
	if called || len(f.Calls()) != 0 || statuses(r)["ci"] != StatusFailed || !strings.Contains(r.Steps[0].Err.Error(), "may not start workflows") {
		t.Fatalf("called=%v calls=%v report=%+v", called, f.Calls(), r.Steps)
	}
}

func TestWorkflowStepResultsAndTimeoutAndCancel(t *testing.T) {
	run := func(h func(context.Context, *pipeline.Step, io.Writer) error, timeout string, ctx context.Context) StepResult {
		s := wfStep("ci")
		s.Timeout = timeout
		return Run(ctx, plan(t, s), &executor.Fake{}, Options{Workflow: h}).Steps[0]
	}
	ok := func(_ context.Context, _ *pipeline.Step, w io.Writer) error { io.WriteString(w, "hi"); return nil }
	if r := run(ok, "", context.Background()); r.Status != StatusSucceeded {
		t.Errorf("success: %+v", r)
	}
	boom := func(context.Context, *pipeline.Step, io.Writer) error { return errors.New("conclusion failure") }
	if r := run(boom, "", context.Background()); r.Status != StatusFailed || r.Err.Error() != "conclusion failure" {
		t.Errorf("failure: %+v", r)
	}
	wait := func(ctx context.Context, _ *pipeline.Step, _ io.Writer) error { <-ctx.Done(); return ctx.Err() }
	if r := run(wait, "50ms", context.Background()); r.Status != StatusFailed || !strings.Contains(r.Err.Error(), "timed out") {
		t.Errorf("timeout: %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	if r := run(wait, "", ctx); r.Status != StatusCanceled {
		t.Errorf("cancel: %+v", r)
	}
}

func TestWorkflowStepOutputIsMaskedLikeAnyOther(t *testing.T) {
	var out strings.Builder
	hook := func(_ context.Context, _ *pipeline.Step, w io.Writer) error {
		io.WriteString(w, "token is hunter2-secret-value\n")
		return nil
	}
	Run(context.Background(), plan(t, wfStep("ci")), &executor.Fake{}, Options{
		Workflow: hook, Secrets: map[string]string{"X": "hunter2-secret-value"},
		Logs: func(string) io.Writer { return &out },
	})
	if strings.Contains(out.String(), "hunter2") {
		t.Errorf("output = %q", out.String())
	}
}

func TestBuildStepGoesToTheBuilderNeverTheExecutor(t *testing.T) {
	bs := pipeline.Step{ID: "image", Build: &pipeline.Build{Context: "app"}, Timeout: "50ms"}
	f := &executor.Fake{}
	r := Run(context.Background(), plan(t, bs, st("after", "image")), f, Options{})
	if s := statuses(r); s["image"] != StatusFailed || s["after"] != StatusSkipped || len(f.Calls()) != 0 || !strings.Contains(r.Steps[0].Err.Error(), "cannot build") {
		t.Fatalf("without a builder: %v, %v", s, r.Steps[0].Err)
	}
	var got *pipeline.Step
	r = Run(context.Background(), plan(t, bs), f, Options{Build: func(_ context.Context, s *pipeline.Step, w io.Writer) error {
		got = s
		io.WriteString(w, "built")
		return nil
	}})
	if got == nil || got.Build.Context != "app" || statuses(r)["image"] != StatusSucceeded || len(f.Calls()) != 0 {
		t.Fatalf("with a builder: got=%v report=%+v calls=%v", got, r.Steps, f.Calls())
	}
	wait := func(ctx context.Context, _ *pipeline.Step, _ io.Writer) error { <-ctx.Done(); return ctx.Err() }
	if res := Run(context.Background(), plan(t, bs), f, Options{Build: wait}).Steps[0]; res.Status != StatusFailed || !strings.Contains(res.Err.Error(), "timed out") {
		t.Errorf("timeout: %+v", res)
	}
}

func TestEvalStepGoesToTheServerNeverTheExecutor(t *testing.T) {
	es := pipeline.Step{ID: "gate", Eval: &pipeline.Eval{Suite: "triage-regression"}, Timeout: "50ms"}
	f := &executor.Fake{}
	r := Run(context.Background(), plan(t, es, st("after", "gate")), f, Options{})
	if s := statuses(r); s["gate"] != StatusFailed || s["after"] != StatusSkipped || len(f.Calls()) != 0 || !strings.Contains(r.Steps[0].Err.Error(), "only on a Forgeline server") {
		t.Fatalf("without a server: %v, %v", s, r.Steps[0].Err)
	}
	called := false
	hook := func(context.Context, *pipeline.Step, io.Writer) error { called = true; return nil }
	r = Run(context.Background(), plan(t, es), f, Options{EvalDenied: "runs started by GitHub may not run evals", Eval: hook})
	if called || len(f.Calls()) != 0 || statuses(r)["gate"] != StatusFailed || !strings.Contains(r.Steps[0].Err.Error(), "may not run evals") {
		t.Fatalf("denied: called=%v report=%+v", called, r.Steps)
	}
	var got *pipeline.Step
	r = Run(context.Background(), plan(t, es), f, Options{Eval: func(_ context.Context, s *pipeline.Step, w io.Writer) error {
		got = s
		io.WriteString(w, "3 of 3 cases passed")
		return nil
	}})
	if got == nil || got.Eval.Suite != "triage-regression" || statuses(r)["gate"] != StatusSucceeded || len(f.Calls()) != 0 {
		t.Fatalf("with a server: got=%v report=%+v calls=%v", got, r.Steps, f.Calls())
	}
	failing := func(context.Context, *pipeline.Step, io.Writer) error { return errors.New("1 of 3 cases failed") }
	if res := Run(context.Background(), plan(t, es), f, Options{Eval: failing}).Steps[0]; res.Status != StatusFailed || res.Err.Error() != "1 of 3 cases failed" {
		t.Errorf("failure: %+v", res)
	}
	wait := func(ctx context.Context, _ *pipeline.Step, _ io.Writer) error { <-ctx.Done(); return ctx.Err() }
	if res := Run(context.Background(), plan(t, es), f, Options{Eval: wait}).Steps[0]; res.Status != StatusFailed || !strings.Contains(res.Err.Error(), "timed out") {
		t.Errorf("timeout: %+v", res)
	}
}

// Every secret a step is given is reported before its container starts, by name and with the network it has; a step that asked and
// was refused is reported with the reason; a step without secrets is neither.
func TestSecretGrantsAndRefusalsAreReported(t *testing.T) {
	step := func(id, network string, secrets ...string) pipeline.Step {
		return pipeline.Step{ID: id, Image: "img", Run: "x", Permissions: pipeline.Permissions{Secrets: secrets, Network: network}}
	}
	var mu sync.Mutex
	var log []string
	note := func(s string) { mu.Lock(); log = append(log, s); mu.Unlock() }
	ex := &executor.Fake{Handler: func(_ context.Context, s executor.StepRun, _ io.Writer) (executor.Result, error) {
		note("start " + s.ID)
		return executor.Result{}, nil
	}}
	opts := Options{
		Parallelism: 1,
		Secrets:     map[string]string{"B_KEY": "bbbbbbbb-secret", "A_KEY": "aaaaaaaa-secret"},
		OnSecretsGranted: func(id string, names []string, network string) {
			note("grant " + id + " " + strings.Join(names, ",") + " net=" + network)
		},
		OnSecretsRefused: func(id string, names []string, why string) {
			note("refused " + id + " " + strings.Join(names, ",") + ": " + why)
		},
	}
	Run(context.Background(), plan(t, step("plain", ""), step("both", "", "B_KEY", "A_KEY"), step("offline", pipeline.NetworkNone, "A_KEY"), step("missing", "", "NOPE_KEY")), ex, opts)
	got := strings.Join(log, "\n")
	for _, want := range []string{
		"grant both A_KEY,B_KEY net=\nstart both", // names sorted, no values, reported before the container starts
		"grant offline A_KEY net=none\nstart offline",
		`refused missing NOPE_KEY: secret "NOPE_KEY" is not configured`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "grant plain") || strings.Contains(got, "refused plain") || strings.Contains(got, "start missing") || strings.Contains(got, "secret-") {
		t.Errorf("a step without secrets was reported, a refused step started, or a value leaked:\n%s", got)
	}
	// A run that is refused every secret reports each step that asked, and starts none of them.
	log = nil
	opts.SecretsDenied = "runs started by GitHub receive no secrets yet"
	Run(context.Background(), plan(t, step("a", "", "A_KEY"), step("b", "", "B_KEY")), ex, opts)
	got = strings.Join(log, "\n")
	if strings.Count(got, "refused") != 2 || !strings.Contains(got, "runs started by GitHub receive no secrets yet") || strings.Contains(got, "start ") || strings.Contains(got, "grant ") {
		t.Errorf("denied run:\n%s", got)
	}
}

// Every step drops all capabilities and gets back the mode's baseline plus what it lists; the mode decides the baseline.
func TestCapabilitiesFollowTheModeAndTheStep(t *testing.T) {
	plain := &pipeline.Step{ID: "a"}
	asks := &pipeline.Step{ID: "b", Permissions: pipeline.Permissions{Capabilities: []string{"NET_RAW", "CHOWN"}}}
	tests := []struct {
		name  string
		step  *pipeline.Step
		mode  string
		caps  []string
		extra []string
	}{
		{"relaxed is the default", plain, "", executor.RelaxedCapabilities, nil},
		{"strict gives nothing", plain, executor.CapsStrict, nil, nil},
		{"relaxed plus a grant", asks, executor.CapsRelaxed, append(append([]string(nil), executor.RelaxedCapabilities...), "NET_RAW"), []string{"NET_RAW"}},
		{"strict plus grants", asks, executor.CapsStrict, []string{"NET_RAW", "CHOWN"}, []string{"NET_RAW", "CHOWN"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run, err := toStepRun(tt.step, "/ws", tt.mode)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(run.Capabilities, tt.caps) {
				t.Errorf("Capabilities = %v, want %v", run.Capabilities, tt.caps)
			}
			if got := extraCapabilities(tt.step, tt.mode); !slices.Equal(got, tt.extra) {
				t.Errorf("extra = %v, want %v", got, tt.extra)
			}
		})
	}
}
