package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/forgelinehq/forgeline/internal/guardrail"
)

// fakePacks stands in for the pack containers: it records what each was given and answers per tool.
type fakePacks struct {
	mu      sync.Mutex
	images  []string
	plans   []string            // tfplan.json as each container saw it
	rules   []map[string]string // the policy files as each container saw them
	answers map[string]string   // by tool: the standard output
	exits   map[string]int
	err     error
}

func (f *fakePacks) run(_ context.Context, image, dir string, args []string) ([]byte, string, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	plan, _ := os.ReadFile(filepath.Join(dir, guardrail.PackPlan))
	rules := map[string]string{}
	files, _ := os.ReadDir(filepath.Join(dir, guardrail.PackPolicy))
	for _, e := range files {
		b, _ := os.ReadFile(filepath.Join(dir, guardrail.PackPolicy, e.Name()))
		rules[e.Name()] = string(b)
	}
	f.images, f.plans, f.rules = append(f.images, image), append(f.plans, string(plan)), append(f.rules, rules)
	if f.err != nil {
		return nil, "Cannot connect to the Docker daemon", 0, f.err
	}
	for tool, t := range guardrail.Tools {
		if t.Image == image {
			return []byte(f.answers[tool]), "", f.exits[tool], nil
		}
	}
	return nil, "", 0, errors.New("unknown image " + image)
}

const trivyOneHigh = `{"SchemaVersion":2,"Results":[{"Misconfigurations":[{"ID":"AWS-0052","Title":"Load balancers should drop invalid headers","Message":"It does not.","Severity":"HIGH","Status":"FAIL","CauseMetadata":{"Resource":"aws_lb.web"}}]}]}`

func TestPolicyPackSettings(t *testing.T) {
	m := newMCPHarness(t)
	rego := "package main\n# OUR-REGO-TEXT\n"
	good := map[string]any{"packs": []map[string]any{
		{"id": "broad", "name": "Broad", "tool": "trivy", "enabled": true, "deny_at": "critical", "skip": []string{"AWS-0040"}},
		{"id": "ours", "name": "Our rules", "tool": "conftest", "enabled": false, "rules": map[string]string{"org.rego": rego}},
	}}
	m.dev.expect(403, "PUT", "/v1/settings/policy-packs", good)
	var got struct {
		Packs   []guardrail.Pack
		Tools   []guardrail.Tool
		Builtin string
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/settings/policy-packs", nil), &got)
	if len(got.Packs) != 0 || len(got.Tools) != 3 || got.Builtin != guardrail.Version {
		t.Errorf("before any pack: %+v", got)
	}
	for want, body := range map[string]map[string]any{
		"packs[0].tool":     {"packs": []map[string]any{{"id": "a", "name": "A", "tool": "tfsec"}}},
		"packs[1].id":       {"packs": []map[string]any{{"id": "a", "name": "A", "tool": "trivy"}, {"id": "a", "name": "B", "tool": "checkov"}}},
		"packs[0].deny_at":  {"packs": []map[string]any{{"id": "a", "name": "A", "tool": "checkov", "deny_at": "high"}}},
		"packs[0].rules":    {"packs": []map[string]any{{"id": "a", "name": "A", "tool": "conftest"}}},
		"packs: at most 10": {"packs": make([]map[string]any, 11)},
	} {
		if out := string(m.admin.expect(400, "PUT", "/v1/settings/policy-packs", body)); !strings.Contains(out, want) {
			t.Errorf("want a refusal naming %s, got %s", want, out)
		}
	}
	m.admin.expect(200, "PUT", "/v1/settings/policy-packs", good)
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/settings/policy-packs", nil), &got)
	if len(got.Packs) != 2 || got.Packs[1].Rules["org.rego"] != rego {
		t.Errorf("saved packs: %+v", got.Packs)
	}
	a := string(m.admin.expect(200, "GET", "/v1/audit?action=guardrail", nil))
	if !strings.Contains(a, "guardrail.packs") || !strings.Contains(a, "broad (trivy, on, denies from critical, skips AWS-0040)") || strings.Contains(a, "OUR-REGO-TEXT") {
		t.Errorf("the audit entry: %s", a)
	}
}

// Every preview is checked by the packs that are on. A pack's warning reaches the approver with the pack's version;
// a pack's deny, a pack that cannot run and an answer that cannot be read all stop the plan; a preview checked by
// other packs than today's cannot be sent.
func TestPolicyPacksCheckEveryPreview(t *testing.T) {
	m := newMCPHarness(t)
	m.srv.run = (&awsStub{}).run
	tf := &fakeTofu{showExtra: []map[string]any{{"address": "aws_sqs_queue.q", "mode": "managed", "type": "aws_sqs_queue", "name": "q",
		"change": map[string]any{"actions": []string{"create"}, "after": map[string]any{"name": "q", "token": "S3CR3T-IN-PLAN"}, "after_sensitive": map[string]any{"token": true}}}}}
	m.srv.tofuRun = tf.run
	packs := &fakePacks{answers: map[string]string{guardrail.ToolTrivy: trivyOneHigh,
		guardrail.ToolConftest: `[{"filename":"/in/tfplan.json","failures":[{"msg":"queues need an owner tag","metadata":{"address":"aws_sqs_queue.q"}}]}]`},
		exits: map[string]int{guardrail.ToolConftest: 1}}
	m.srv.packRun = packs.run
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
	propose := func(code int) string {
		t.Helper()
		return string(m.dev.expect(code, "POST", "/v1/blueprints/"+made.ID+"/propose", map[string]any{"reason": "first build of the shop"}))
	}
	set := func(ps ...map[string]any) {
		t.Helper()
		m.admin.expect(200, "PUT", "/v1/settings/policy-packs", map[string]any{"packs": ps})
	}
	trivy := map[string]any{"id": "broad", "name": "Broad", "tool": "trivy", "enabled": true}
	conftest := map[string]any{"id": "ours", "name": "Our rules", "tool": "conftest", "enabled": true, "rules": map[string]string{"org.rego": "package main\n"}}
	has := func(p BlueprintPlan, rule, level string) bool {
		for _, f := range p.Guardrails {
			if f.Rule == rule && f.Level == level {
				return true
			}
		}
		return false
	}

	// No pack is on: nothing runs.
	set(map[string]any{"id": "broad", "name": "Broad", "tool": "trivy", "enabled": false})
	if p := plan(); len(p.Packs) != 0 || len(packs.images) != 0 {
		t.Fatalf("a pack that is off ran: %+v", p.Packs)
	}

	// A preview made before a pack was turned on cannot be sent.
	set(trivy)
	if out := propose(409); !strings.Contains(out, "policy packs changed") {
		t.Errorf("a preview no pack checked was accepted: %s", out)
	}

	// Trivy warns: the approver sees it, and which version checked.
	p := plan()
	if !has(p, "trivy:AWS-0052", guardrail.Warn) || len(p.Packs) != 1 || p.Packs[0].Version != guardrail.Tools[guardrail.ToolTrivy].Version || p.Packs[0].Warnings != 1 {
		t.Fatalf("plan = %+v, packs %+v", p.Guardrails, p.Packs)
	}
	if packs.images[0] != guardrail.Tools[guardrail.ToolTrivy].Image {
		t.Errorf("ran %s", packs.images[0])
	}
	if seen := packs.plans[0]; strings.Contains(seen, "S3CR3T-IN-PLAN") || !strings.Contains(seen, "aws_sqs_queue.q") || strings.Contains(seen, "prior_state") {
		t.Errorf("the pack was given the plan with its secrets or its state")
	}
	var c CloudChange
	json.Unmarshal([]byte(propose(202)), &c)
	if !strings.Contains(c.PreviewNote, "Policy packs: Broad (Trivy "+guardrail.Tools[guardrail.ToolTrivy].Version+"): 1 warning(s)") {
		t.Errorf("the approver is not told which pack checked: %q", c.PreviewNote)
	}

	// A Conftest deny stops it, and the rules reach the container.
	set(trivy, conftest)
	p = plan()
	if !has(p, "conftest:ours", guardrail.Deny) || len(p.Packs) != 2 || p.Packs[1].Denies != 1 {
		t.Fatalf("plan = %+v, packs %+v", p.Guardrails, p.Packs)
	}
	if got := packs.rules[len(packs.rules)-1]["org.rego"]; got != "package main\n" {
		t.Errorf("the container saw rules %q", got)
	}
	if out := propose(409); !strings.Contains(out, "queues need an owner tag") {
		t.Errorf("a plan a pack denies was accepted: %s", out)
	}

	// An answer that cannot be read stops it.
	packs.mu.Lock()
	packs.answers[guardrail.ToolConftest], packs.exits[guardrail.ToolConftest] = "", 0
	packs.mu.Unlock()
	if p = plan(); !has(p, packFailedRule, guardrail.Deny) || p.Packs[1].Error == "" {
		t.Fatalf("an unreadable answer passed: %+v", p.Guardrails)
	}
	propose(409)

	// A pack that cannot be started stops it.
	set(trivy)
	packs.mu.Lock()
	packs.err = errors.New("exit status 125")
	packs.mu.Unlock()
	if p = plan(); !has(p, packFailedRule, guardrail.Deny) || !strings.Contains(p.Packs[0].Error, "Docker") {
		t.Fatalf("a pack that did not run passed: %+v %+v", p.Guardrails, p.Packs)
	}
	if out := propose(409); !strings.Contains(out, "could not be run") {
		t.Errorf("a plan no pack checked was accepted: %s", out)
	}
	if a := string(m.admin.expect(200, "GET", "/v1/audit?action=blueprint", nil)); !strings.Contains(a, packFailedRule) {
		t.Errorf("the deny is not audited: %s", a)
	}
}

// The container a pack runs in: no network, nothing writable but /tmp, no capabilities, limits, the plan read-only.
func TestPackContainerIsLockedDown(t *testing.T) {
	args := " " + strings.Join(packDockerArgs("n", "img@sha256:abc", "/data/tmp/pack-1", []string{"test", "x"}), " ") + " "
	for _, want := range []string{" --network none ", " --read-only ", " --cap-drop ALL ", " --security-opt no-new-privileges ", " --user ",
		" --memory 1g ", " --pids-limit 256 ", " --cpus 2 ", " --rm ", " -v /data/tmp/pack-1:/in:ro img@sha256:abc test x "} {
		if !strings.Contains(args, want) {
			t.Errorf("the pack container lacks%s: %s", want, args)
		}
	}
	if strings.Contains(args, "--privileged") || strings.Contains(args, "docker.sock") || strings.Count(args, " -v ") != 1 {
		t.Errorf("the pack container gets more than the plan: %s", args)
	}
}

// TestRealPackContainers runs the three pinned tools for real, through the same code a preview uses, on a plan cut
// from a real EKS preview. It needs Docker and the images (pulled on first use): set FORGELINE_PACK_DOCKER=1.
func TestRealPackContainers(t *testing.T) {
	if os.Getenv("FORGELINE_PACK_DOCKER") == "" {
		t.Skip("set FORGELINE_PACK_DOCKER=1 to run the real Conftest, Trivy and Checkov containers")
	}
	m := newMCPHarness(t)
	show, err := os.ReadFile("../guardrail/testdata/eks-plan.json")
	if err != nil {
		t.Fatal(err)
	}
	rego := "package org\n\nimport rego.v1\n\ndeny contains {\"msg\": \"logs must be kept at least 30 days\", \"address\": rc.address} if {\n\tsome rc in input.resource_changes\n\trc.type == \"aws_cloudwatch_log_group\"\n\trc.change.after.retention_in_days < 30\n}\n"
	m.admin.expect(200, "PUT", "/v1/settings/policy-packs", map[string]any{"packs": []map[string]any{
		{"id": "broad", "name": "Broad", "tool": "trivy", "enabled": true, "deny_at": "critical", "skip": []string{"AWS-0041"}},
		{"id": "wide", "name": "Wide", "tool": "checkov", "enabled": true, "deny": []string{"CKV_AWS_58"}},
		{"id": "ours", "name": "Our rules", "tool": "conftest", "enabled": true, "rules": map[string]string{"org.rego": rego}},
		{"id": "broken", "name": "Broken", "tool": "conftest", "enabled": true, "rules": map[string]string{"bad.rego": "package org\ndeny contains msg if {"}},
	}})
	fs, runs, _, err := m.srv.runPacks(t.Context(), show)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		t.Logf("%s (%s %s): %d denies, %d warnings, %d skipped, error %q", r.Name, r.Tool, r.Version, r.Denies, r.Warnings, r.Skipped, r.Error)
	}
	got := map[string]guardrail.Finding{}
	for _, f := range fs {
		got[f.Rule] = f
	}
	for rule, want := range map[string]guardrail.Finding{
		"trivy:AWS-0040":     {Level: guardrail.Deny, Address: "module.k8s.aws_eks_cluster.this"},
		"trivy:AWS-0039":     {Level: guardrail.Warn, Address: "module.k8s.aws_eks_cluster.this"},
		"checkov:CKV_AWS_58": {Level: guardrail.Deny, Address: "module.k8s.aws_eks_cluster.this"},
		"checkov:CKV_AWS_37": {Level: guardrail.Warn, Address: "module.k8s.aws_eks_cluster.this"},
		"conftest:ours":      {Level: guardrail.Deny, Address: "module.k8s.aws_cloudwatch_log_group.cluster"},
		packFailedRule:       {Level: guardrail.Deny, Address: "Broken"},
	} {
		if f := got[rule]; f.Level != want.Level || f.Address != want.Address {
			t.Errorf("%s: got %+v, want level %s at %s", rule, f, want.Level, want.Address)
		}
	}
	if _, ok := got["trivy:AWS-0041"]; ok || runs[0].Skipped != 1 {
		t.Errorf("the skipped rule was reported: %+v", runs[0])
	}
	if len(runs) != 4 || runs[0].Error+runs[1].Error+runs[2].Error != "" || runs[3].Error == "" {
		t.Errorf("runs: %+v", runs)
	}
}
