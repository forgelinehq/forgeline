package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/forgelinehq/forgeline/internal/guardrail"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTofu stands in for OpenTofu. Its state is the list of resource addresses that exist; a plan adds what
// the code has and the state lacks, and removes what the state has and the code lacks.
type fakeTofu struct {
	mu        sync.Mutex
	calls     []string
	envs      [][]string
	dirs      []string
	failApply bool
	showExtra []map[string]any // more resource changes for show to report (guardrail tests)
}

func (f *fakeTofu) run(_ context.Context, dir string, env []string, args ...string) ([]byte, string, error) {
	f.mu.Lock()
	f.calls, f.envs, f.dirs = append(f.calls, strings.Join(args, " ")), append(f.envs, env), append(f.dirs, dir)
	fail, extra := f.failApply, f.showExtra
	f.mu.Unlock()
	read := func(name string) []byte { b, _ := os.ReadFile(filepath.Join(dir, name)); return b }
	write := func(name, body string) { os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600) }
	codeAddrs := func() []string {
		var doc struct{ Resource map[string]map[string]any }
		json.Unmarshal(read("main.tf.json"), &doc)
		var out []string
		for typ, m := range doc.Resource {
			for name := range m {
				out = append(out, typ+"."+name)
			}
		}
		sort.Strings(out)
		return out
	}
	stateAddrs := func() map[string]bool {
		var st struct{ Addresses []string }
		json.Unmarshal(read("terraform.tfstate"), &st)
		m := map[string]bool{}
		for _, a := range st.Addresses {
			m[a] = true
		}
		return m
	}
	switch args[0] {
	case "init":
		if strings.Contains(strings.Join(args, " "), "-lockfile=readonly") && len(read(".terraform.lock.hcl")) == 0 {
			return nil, "no lock file", errors.New("exit 1")
		}
		if len(read(".terraform.lock.hcl")) == 0 {
			write(".terraform.lock.hcl", "# providers hashicorp/aws 6.72.0")
		}
	case "plan":
		write("plan.bin", "PLAN-FILE:"+string(read("terraform.tfstate")))
	case "show":
		have := stateAddrs()
		type rc struct {
			Address, Mode, Type, Name string
			Change                    struct{ Actions []string } `json:"change"`
		}
		var out []rc
		want := map[string]bool{}
		for _, a := range codeAddrs() {
			want[a] = true
			typ, name, _ := strings.Cut(a, ".")
			r := rc{Address: a, Mode: "managed", Type: typ, Name: name}
			r.Change.Actions = []string{"create"}
			if have[a] {
				r.Change.Actions = []string{"no-op"}
			}
			out = append(out, r)
		}
		for a := range have {
			if !want[a] {
				typ, name, _ := strings.Cut(a, ".")
				r := rc{Address: a, Mode: "managed", Type: typ, Name: name}
				r.Change.Actions = []string{"delete"}
				out = append(out, r)
			}
		}
		all := []any{}
		for _, r := range out {
			all = append(all, r)
		}
		for _, e := range extra {
			all = append(all, e)
		}
		b, _ := json.Marshal(map[string]any{"resource_changes": all})
		return b, "", nil
	case "apply":
		if !strings.HasPrefix(string(read("plan.bin")), "PLAN-FILE:") {
			return nil, "not the saved plan", errors.New("exit 1")
		}
		addrs := codeAddrs()
		if fail {
			addrs = addrs[:1] // made one thing, then failed
		}
		b, _ := json.Marshal(map[string]any{"addresses": addrs})
		write("terraform.tfstate", string(b))
		if fail {
			return nil, "Error: creating ECS Service: AccessDenied", errors.New("exit 1")
		}
	case "output":
		return []byte(`{"web_url":{"value":"http://shop-123.elb.amazonaws.com"}}`), "", nil
	}
	return nil, "", nil
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestBlueprintPreviewApprovalAndApply(t *testing.T) {
	t.Setenv("FORGELINE_SECRET_FOR_TEST", "must-not-leak")
	m := newMCPHarness(t)
	stub := &awsStub{}
	m.srv.run = stub.run
	tf := &fakeTofu{}
	m.srv.tofuRun = tf.run
	m.srv.cloud.setInterval(10 * time.Millisecond)
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})

	var made BlueprintJSON
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/blueprints", bpBody("Shop")), &made)
	id := made.ID
	m.v.expect(403, "POST", "/v1/blueprints/"+id+"/plan", nil)
	if out := strings.TrimSpace(string(m.dev.expect(200, "GET", "/v1/blueprints/"+id+"/plan", nil))); out != "null" {
		t.Errorf("no preview yet should be null, got %s", out)
	}
	m.dev.expect(409, "POST", "/v1/blueprints/"+id+"/propose", map[string]any{"reason": "first build of the shop"}) // no preview yet

	plan := func() BlueprintPlan {
		t.Helper()
		m.dev.expect(202, "POST", "/v1/blueprints/"+id+"/plan", nil)
		var p BlueprintPlan
		waitFor(t, "the preview", func() bool {
			json.Unmarshal(m.dev.expect(200, "GET", "/v1/blueprints/"+id+"/plan", nil), &p)
			return p.Status != planPlanning
		})
		return p
	}
	p := plan()
	if p.Status != planReady || p.Add == 0 || p.Remove != 0 || p.AWSAccount != "111122223333" {
		t.Fatalf("plan = %+v", p)
	}
	blocks := map[string]bool{}
	for _, c := range p.Changes {
		blocks[c.Block] = true
		if c.Address == "aws_ecs_service.app" && c.Block != "app" {
			t.Errorf("the service belongs to block app: %+v", c)
		}
	}
	if !blocks["app"] || !blocks["web"] || !blocks["data"] || !blocks[""] {
		t.Errorf("blocks in the plan: %v", blocks)
	}
	// OpenTofu sees only what it needs, in a directory that is gone afterwards.
	for _, env := range tf.envs {
		all := strings.Join(env, "\n")
		if strings.Contains(all, "must-not-leak") || strings.Contains(all, "FORGELINE") {
			t.Fatalf("tofu saw the server's environment:\n%s", all)
		}
		for _, want := range []string{"AWS_PROFILE=dev", "AWS_REGION=ap-south-1", "TF_PLUGIN_CACHE_DIR=", "TF_IN_AUTOMATION=1"} {
			if !strings.Contains(all, want) {
				t.Errorf("tofu env misses %s", want)
			}
		}
	}
	for _, d := range tf.dirs {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("the work directory %s was left behind", d)
		}
	}
	// The plan file is kept sealed, never in the clear.
	var raw map[string]any
	m.srv.docs.Get(t.Context(), kindBPPlan, id, &raw)
	if s, _ := raw["sealed"].(string); s == "" || strings.Contains(s, "PLAN-FILE") {
		t.Errorf("the plan file is not sealed: %.60q", s)
	}
	if strings.Contains(string(m.dev.expect(200, "GET", "/v1/blueprints/"+id+"/plan", nil)), "sealed") {
		t.Errorf("the API shows the sealed plan")
	}

	// A change to the blueprint makes the preview stale.
	upd := bpBody("Shop")
	upd["revision"] = 1
	m.dev.expect(200, "PUT", "/v1/blueprints/"+id, upd)
	if out := string(m.dev.expect(409, "POST", "/v1/blueprints/"+id+"/propose", map[string]any{"reason": "first build of the shop"})); !strings.Contains(out, "changed after the preview") {
		t.Errorf("a stale preview was accepted: %s", out)
	}
	plan()

	propose := func() CloudChange {
		t.Helper()
		var c CloudChange
		json.Unmarshal(m.dev.expect(202, "POST", "/v1/blueprints/"+id+"/propose", map[string]any{"reason": "first build of the shop"}), &c)
		waitFor(t, "the proposal", func() bool {
			json.Unmarshal(m.admin.expect(200, "GET", "/v1/cloud/changes/"+c.ID, nil), &c)
			return c.Status != stPreparing
		})
		return c
	}
	c1 := propose()
	c2 := propose() // the same preview, filed twice
	if c1.Status != stProposed || c1.Kind != kindBlueprintApply || !strings.Contains(c1.Title, "Build blueprint Shop") || len(c1.Preview) != p.Add || c1.Preview[0].Action != "Add" {
		t.Fatalf("proposal = %+v", c1)
	}
	if c1.highRisk() {
		t.Errorf("adding things is not high risk")
	}
	m.dev.expect(403, "POST", "/v1/cloud/changes/"+c1.ID+"/approve", map[string]any{}) // developers do not approve
	m.admin.expect(202, "POST", "/v1/cloud/changes/"+c1.ID+"/approve", map[string]any{"note": "ok"})
	waitFor(t, "the apply", func() bool {
		json.Unmarshal(m.admin.expect(200, "GET", "/v1/cloud/changes/"+c1.ID, nil), &c1)
		return c1.Status != stExecuting
	})
	if c1.Status != stSucceeded || !strings.Contains(c1.Result, "web_url: http://shop-123") {
		t.Fatalf("apply = %s: %s", c1.Status, c1.Result)
	}
	var calls []string
	tf.mu.Lock()
	calls = append(calls, tf.calls...)
	tf.mu.Unlock()
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "init -input=false -no-color -lockfile=readonly") || !strings.Contains(joined, "apply -input=false -no-color -lock=false plan.bin") {
		t.Errorf("apply must use the saved plan with the planned providers:\n%s", joined)
	}
	var built map[string]any
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/blueprints/"+id+"/built", nil), &built)
	if built["built"] != true || built["applies"] != float64(1) {
		t.Errorf("built = %v", built)
	}
	m.srv.docs.Get(t.Context(), kindBPState, id, &raw)
	if s, _ := raw["sealed"].(string); s == "" || strings.Contains(s, "aws_ecs_service") {
		t.Errorf("the state is not sealed")
	}

	// The second proposal was planned from the state before the apply: it must not run.
	m.admin.expect(202, "POST", "/v1/cloud/changes/"+c2.ID+"/approve", map[string]any{})
	waitFor(t, "the stale apply", func() bool {
		json.Unmarshal(m.admin.expect(200, "GET", "/v1/cloud/changes/"+c2.ID, nil), &c2)
		return c2.Status != stExecuting
	})
	if c2.Status != stFailed || !strings.Contains(c2.Result, "built again") {
		t.Errorf("a stale plan ran: %s %s", c2.Status, c2.Result)
	}
	if out := string(m.dev.expect(409, "POST", "/v1/blueprints/"+id+"/propose", map[string]any{"reason": "again please"})); !strings.Contains(out, "built since the preview") {
		t.Errorf("an old preview was accepted after a build: %s", out)
	}
	// Built: nothing left to change, the draft cannot be deleted, the target cannot move.
	if p := plan(); p.Add+p.Change+p.Remove != 0 {
		t.Errorf("after the build the plan should be empty: %+v", p)
	}
	m.dev.expect(409, "POST", "/v1/blueprints/"+id+"/propose", map[string]any{"reason": "nothing to do"})
	m.admin.expect(409, "DELETE", "/v1/blueprints/"+id, nil)
	moved := bpBody("Shop")
	moved["revision"] = 2
	moved["target"] = map[string]any{"cloud": "aws", "account": "primary", "region": "eu-west-1"}
	m.dev.expect(409, "PUT", "/v1/blueprints/"+id, moved)

	// Removing a block is high risk under the approval rules.
	less := bpBody("Shop")
	less["revision"] = 2
	less["blocks"] = less["blocks"].([]map[string]any)[:2]
	less["links"] = less["links"].([]map[string]any)[:1]
	m.dev.expect(200, "PUT", "/v1/blueprints/"+id, less)
	if p := plan(); p.Remove == 0 {
		t.Fatalf("removing a block should remove resources: %+v", p)
	}
	if c := propose(); !c.highRisk() || !strings.Contains(c.Title, "to remove") {
		t.Errorf("a removal is high risk: %+v", c.Preview)
	}
}

func TestBlueprintFailedApplyKeepsItsState(t *testing.T) {
	m := newMCPHarness(t)
	stub := &awsStub{}
	m.srv.run = stub.run
	tf := &fakeTofu{failApply: true}
	m.srv.tofuRun = tf.run
	m.srv.cloud.setInterval(10 * time.Millisecond)
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	var made BlueprintJSON
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/blueprints", bpBody("Shop")), &made)
	m.dev.expect(202, "POST", "/v1/blueprints/"+made.ID+"/plan", nil)
	var p BlueprintPlan
	waitFor(t, "the preview", func() bool {
		json.Unmarshal(m.dev.expect(200, "GET", "/v1/blueprints/"+made.ID+"/plan", nil), &p)
		return p.Status != planPlanning
	})
	var c CloudChange
	json.Unmarshal(m.admin.expect(202, "POST", "/v1/blueprints/"+made.ID+"/propose", map[string]any{"reason": "build it please"}), &c)
	waitFor(t, "the proposal", func() bool {
		json.Unmarshal(m.admin.expect(200, "GET", "/v1/cloud/changes/"+c.ID, nil), &c)
		return c.Status == stProposed
	})
	// The administrator who proposed it cannot approve it; another one can.
	m.admin.expect(403, "POST", "/v1/cloud/changes/"+c.ID+"/approve", map[string]any{})
	m.admin2.expect(202, "POST", "/v1/cloud/changes/"+c.ID+"/approve", map[string]any{})
	waitFor(t, "the apply", func() bool {
		json.Unmarshal(m.admin.expect(200, "GET", "/v1/cloud/changes/"+c.ID, nil), &c)
		return c.Status != stExecuting
	})
	if c.Status != stFailed || !strings.Contains(c.Result, "AccessDenied") {
		t.Fatalf("apply = %s %s", c.Status, c.Result)
	}
	var built map[string]any
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/blueprints/"+made.ID+"/built", nil), &built)
	if built["built"] != true {
		t.Fatalf("a failed apply that made something must keep its state: %v", built)
	}
	// The next preview starts from what exists.
	tf.mu.Lock()
	tf.failApply = false
	tf.mu.Unlock()
	m.dev.expect(202, "POST", "/v1/blueprints/"+made.ID+"/plan", nil)
	waitFor(t, "the second preview", func() bool {
		json.Unmarshal(m.dev.expect(200, "GET", "/v1/blueprints/"+made.ID+"/plan", nil), &p)
		return p.Status != planPlanning
	})
	if p.Status != planReady || p.StateSerial != 1 || p.Add != len(p.Changes) || p.Add < 2 {
		t.Errorf("second plan = %+v", p)
	}
}

func TestTofuDownloadIsChecked(t *testing.T) {
	dir := t.TempDir()
	if err := installTofu([]byte("not the release"), tofuSums["linux_amd64"], dir); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("a wrong download was accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tofu")); !os.IsNotExist(err) {
		t.Errorf("a refused download left a binary")
	}
}

// A plan that breaks a guardrail is shown with the reason, cannot be sent for approval, and is audited; a plan
// that only warns can be, and the approver sees the warnings (note 040).
func TestBlueprintGuardrails(t *testing.T) {
	m := newMCPHarness(t)
	m.srv.run = (&awsStub{}).run
	tf := &fakeTofu{showExtra: []map[string]any{{"address": "aws_iam_user.bot", "mode": "managed", "type": "aws_iam_user", "name": "bot",
		"change": map[string]any{"actions": []string{"create"}, "after": map[string]any{"name": "bot"}}}}}
	m.srv.tofuRun = tf.run
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	var made BlueprintJSON
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/blueprints", bpBody("Shop")), &made)
	plan := func() BlueprintPlan {
		t.Helper()
		m.dev.expect(202, "POST", "/v1/blueprints/"+made.ID+"/plan", nil)
		var p BlueprintPlan
		waitFor(t, "the preview", func() bool {
			json.Unmarshal(m.dev.expect(200, "GET", "/v1/blueprints/"+made.ID+"/plan", nil), &p)
			return p.Status != planPlanning
		})
		return p
	}
	p := plan()
	if p.Status != planReady || !guardrail.Denied(p.Guardrails) || p.Guardrails[0].Rule != "no-iam-users" || p.GuardrailVersion == "" {
		t.Fatalf("plan = %+v", p.Guardrails)
	}
	out := string(m.dev.expect(409, "POST", "/v1/blueprints/"+made.ID+"/propose", map[string]any{"reason": "first build of the shop"}))
	if !strings.Contains(out, "breaks a guardrail") || !strings.Contains(out, "aws_iam_user.bot") {
		t.Errorf("a denied plan was accepted: %s", out)
	}
	if a := string(m.admin.expect(200, "GET", "/v1/audit?action=blueprint", nil)); !strings.Contains(a, "blueprint.guardrail.deny") {
		t.Errorf("the deny is not audited")
	}

	// Warnings only: the change carries them to the approver.
	tf.mu.Lock()
	tf.showExtra = []map[string]any{{"address": "aws_iam_role_policy.wide", "mode": "managed", "type": "aws_iam_role_policy", "name": "wide",
		"change": map[string]any{"actions": []string{"create"}, "after": map[string]any{"policy": `{"Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"arn:aws:s3:::b"}]}`}}}}
	tf.mu.Unlock()
	if p = plan(); guardrail.Denied(p.Guardrails) {
		t.Fatalf("only warnings expected: %+v", p.Guardrails)
	}
	// A preview kept from before guardrails existed is not enough.
	var sp storedPlan
	m.srv.docs.Get(t.Context(), kindBPPlan, made.ID, &sp)
	sp.GuardrailVersion = ""
	m.srv.docs.Put(t.Context(), kindBPPlan, made.ID, sp)
	if out := string(m.dev.expect(409, "POST", "/v1/blueprints/"+made.ID+"/propose", map[string]any{"reason": "first build of the shop"})); !strings.Contains(out, "before guardrails") {
		t.Errorf("an unchecked preview was accepted: %s", out)
	}
	sp.GuardrailVersion = guardrail.Version
	m.srv.docs.Put(t.Context(), kindBPPlan, made.ID, sp)
	var c CloudChange
	json.Unmarshal(m.dev.expect(202, "POST", "/v1/blueprints/"+made.ID+"/propose", map[string]any{"reason": "first build of the shop"}), &c)
	found := false
	for _, f := range c.Guardrails {
		found = found || (f.Address == "aws_iam_role_policy.wide" && f.Level == guardrail.Warn)
	}
	if !found || !strings.Contains(c.PreviewNote, "Guardrails "+guardrail.Version) {
		t.Errorf("the approver does not see the warnings: %+v %q", c.Guardrails, c.PreviewNote)
	}
}
