package guardrail

import (
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// eksAddresses are the addresses the EKS plan behind the fixtures changes (some of them).
var eksAddresses = []string{"module.k8s.aws_cloudwatch_log_group.cluster", "module.k8s.aws_eks_cluster.this", "module.k8s.aws_eks_node_group.nodes",
	`module.k8s.aws_iam_role.cluster`, `module.k8s.aws_eks_access_entry.editors["arn"]`}

func byRule(fs []Finding) map[string]Finding {
	m := map[string]Finding{}
	for _, f := range fs {
		m[f.Rule] = f
	}
	return m
}

// TestPackFindings reads what each tool really printed for an EKS plan (the fixtures are cut from real runs).
func TestPackFindings(t *testing.T) {
	// Trivy: severities, and module resources named with a hash.
	trivy := Pack{ID: "broad", Name: "Broad", Tool: ToolTrivy}
	fs, skipped, err := trivy.Findings(fixture(t, "trivy-eks.json"), 0, eksAddresses)
	if err != nil || skipped != 0 || len(fs) != 7 {
		t.Fatalf("trivy: %d findings, %d skipped, %v", len(fs), skipped, err)
	}
	for _, f := range fs {
		if f.Level != Warn {
			t.Errorf("without deny_at every Trivy finding warns: %+v", f)
		}
	}
	if f := byRule(fs)["trivy:AWS-0040"]; f.Address != "module.k8s.aws_eks_cluster.this" || !strings.Contains(f.Text, "(critical)") {
		t.Errorf("Trivy's hashed name is not the plan's address: %+v", f)
	}
	trivy.DenyAt, trivy.Skip, trivy.Deny = "high", []string{"AWS-0041"}, []string{"AWS-0017"}
	fs, skipped, _ = trivy.Findings(fixture(t, "trivy-eks.json"), 0, eksAddresses)
	got := byRule(fs)
	if skipped != 1 || got["trivy:AWS-0041"].Rule != "" {
		t.Errorf("a skipped rule is still reported: %d skipped, %+v", skipped, got["trivy:AWS-0041"])
	}
	for rule, level := range map[string]string{"trivy:AWS-0040": Deny, "trivy:AWS-0039": Deny, "trivy:AWS-0038": Warn, "trivy:AWS-0017": Deny} {
		if got[rule].Level != level {
			t.Errorf("%s: level %q, want %q (deny from high, AWS-0017 listed)", rule, got[rule].Level, level)
		}
	}
	if fs[0].Level != Deny {
		t.Error("denies come first")
	}

	// Checkov: no severities offline; a listed rule denies.
	checkov := Pack{ID: "wide", Name: "Wide", Tool: ToolCheckov, Deny: []string{"CKV_AWS_39"}}
	fs, _, err = checkov.Findings(fixture(t, "checkov-eks.json"), 0, eksAddresses)
	if err != nil || len(fs) != 6 {
		t.Fatalf("checkov: %d findings, %v", len(fs), err)
	}
	got = byRule(fs)
	if got["checkov:CKV_AWS_39"].Level != Deny || got["checkov:CKV_AWS_38"].Level != Warn || got["checkov:CKV_AWS_39"].Address != "module.k8s.aws_eks_cluster.this" {
		t.Errorf("checkov levels or address: %+v", got)
	}
	if _, _, err := checkov.Findings([]byte(`{"passed": 0, "failed": 0, "skipped": 0, "parsing_errors": 0, "resource_count": 0, "checkov_version": "3.4.5"}`), 0, nil); err != nil {
		t.Errorf("a plan with nothing Checkov knows is not an error: %v", err)
	}

	// Conftest: the pack's own deny and warn rules.
	conftest := Pack{ID: "org", Name: "Our rules", Tool: ToolConftest}
	fs, _, err = conftest.Findings(fixture(t, "conftest-eks.json"), 1, eksAddresses)
	if err != nil || len(fs) != 2 || fs[0].Level != Deny || fs[1].Level != Warn || fs[0].Rule != "conftest:org" || fs[0].Address != "Our rules" {
		t.Fatalf("conftest: %+v, %v", fs, err)
	}
	fs, _, err = conftest.Findings([]byte(`[{"filename":"/in/tfplan.json","failures":[{"msg":"no","metadata":{"address":"module.k8s.aws_eks_cluster.this"}}]}]`), 1, eksAddresses)
	if err != nil || len(fs) != 1 || fs[0].Address != "module.k8s.aws_eks_cluster.this" {
		t.Errorf("a rule's own address is not shown: %+v, %v", fs, err)
	}
	if fs, _, err = conftest.Findings(fixture(t, "conftest-pass.json"), 0, nil); err != nil || len(fs) != 0 {
		t.Errorf("a passing run: %+v, %v", fs, err)
	}
}

// TestPackAnswerThatCannotBeReadIsAnError: a pack that did not answer properly must never count as "nothing found".
func TestPackAnswerThatCannotBeReadIsAnError(t *testing.T) {
	for name, c := range map[string]struct {
		tool string
		out  string
		exit int
	}{
		"conftest, nothing printed":      {ToolConftest, "", 0},
		"conftest, an error status":      {ToolConftest, `[{"filename":"x","successes":1}]`, 2},
		"conftest, no file checked":      {ToolConftest, `[]`, 0},
		"conftest, words after":          {ToolConftest, `[{"filename":"x"}] [{"filename":"y"}]`, 0},
		"trivy, a failed run":            {ToolTrivy, `{"SchemaVersion":2,"Results":[]}`, 1},
		"trivy, not a report":            {ToolTrivy, `{"Results":[]}`, 0},
		"trivy, a log line":              {ToolTrivy, `FATAL something`, 0},
		"checkov, a failed run":          {ToolCheckov, `{"passed":0,"failed":0}`, 2},
		"checkov, an empty object":       {ToolCheckov, `{}`, 0},
		"checkov, an empty list":         {ToolCheckov, `[]`, 0},
		"checkov, counts with a failure": {ToolCheckov, `{"passed":0,"failed":3}`, 0},
		"checkov, failures left out":     {ToolCheckov, `{"summary":{"failed":2},"results":{"failed_checks":[]}}`, 0},
		"an unknown tool":                {"other", `[]`, 0},
	} {
		if fs, _, err := (Pack{ID: "p", Name: "P", Tool: c.tool}).Findings([]byte(c.out), c.exit, nil); err == nil {
			t.Errorf("%s: read as %d findings, want an error", name, len(fs))
		}
	}
}

func TestPackFindingsAreCapped(t *testing.T) {
	var b strings.Builder
	b.WriteString(`[{"filename":"x","warnings":[`)
	for i := 0; i < 150; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"msg":"w"}`)
	}
	b.WriteString(`]},{"filename":"y","failures":[{"msg":"the one deny"}]}]`) // the deny is read last
	fs, _, err := (Pack{ID: "p", Name: "P", Tool: ToolConftest}).Findings([]byte(b.String()), 1, nil)
	if err != nil || len(fs) != maxPackFindings+1 {
		t.Fatalf("%d findings, %v", len(fs), err)
	}
	if fs[0].Level != Deny || !strings.Contains(fs[len(fs)-1].Text, "51 more") {
		t.Errorf("the deny must survive the cap, and the rest be counted: first %+v, last %+v", fs[0], fs[len(fs)-1])
	}
}

// TestReduceHidesSecrets: a pack is third-party code, so it gets no sensitive value and no earlier state.
func TestReduceHidesSecrets(t *testing.T) {
	plan := `{"format_version":"1.2","terraform_version":"1.13.9",
	 "variables":{"pw":{"value":"VAR-SECRET"}},
	 "prior_state":{"values":{"root_module":{"resources":[{"values":{"password":"STATE-SECRET"}}]}}},
	 "output_changes":{"o":{"after":"OUTPUT-SECRET"}},
	 "planned_values":{"outputs":{"o":{"sensitive":true,"value":"PLANNED-OUTPUT-SECRET"},"p":{"sensitive":false,"value":"shown"}},
	   "root_module":{"resources":[{"address":"aws_db_instance.db","values":{"password":"PLANNED-SECRET","port":5432},"sensitive_values":{"password":true}}],
	     "child_modules":[{"resources":[{"address":"module.m.aws_x.y","values":{"list":[{"token":"CHILD-SECRET","keep":"kept"}]},"sensitive_values":{"list":[{"token":true}]}}]}]}},
	 "resource_changes":[{"address":"aws_db_instance.db","type":"aws_db_instance","change":{"actions":["update"],
	   "before":{"password":"BEFORE-SECRET","port":5432},"after":{"password":"AFTER-SECRET","port":5433,"tags":{"k":"TAG-SECRET"}},
	   "before_sensitive":{"password":true},"after_sensitive":{"password":true,"tags":true}}}],
	 "configuration":{"root_module":{}}}`
	out, err := Reduce([]byte(plan))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, secret := range []string{"VAR-SECRET", "STATE-SECRET", "OUTPUT-SECRET", "PLANNED-SECRET", "CHILD-SECRET", "BEFORE-SECRET", "AFTER-SECRET", "TAG-SECRET"} {
		if strings.Contains(s, secret) {
			t.Errorf("the pack's copy of the plan still holds %s", secret)
		}
	}
	for _, kept := range []string{`"port":5433`, `"port":5432`, `"keep":"kept"`, `"value":"shown"`, `"configuration"`, `"address":"aws_db_instance.db"`, `"password":"(sensitive)"`} {
		if !strings.Contains(s, kept) {
			t.Errorf("the pack's copy lost %s:\n%s", kept, s)
		}
	}
	if _, err := Reduce([]byte("not json")); err == nil {
		t.Error("a plan that cannot be read must be an error")
	}
}

func TestPackValidate(t *testing.T) {
	rego := map[string]string{"org.rego": "package main\n"}
	ok := []Pack{
		{ID: "broad", Name: "Broad", Tool: ToolTrivy, DenyAt: "critical", Skip: []string{"AWS-0040"}, Deny: []string{"AWS-0017"}},
		{ID: "wide", Name: "Wide", Tool: ToolCheckov, Deny: []string{"CKV_AWS_39", "CKV2_AWS_5"}},
		{ID: "org-1", Name: "Our rules", Tool: ToolConftest, Rules: rego},
	}
	for _, p := range ok {
		if err := p.Validate(); err != nil {
			t.Errorf("%s: %v", p.ID, err)
		}
	}
	for want, p := range map[string]Pack{
		"id:":             {ID: "Bad ID", Name: "x", Tool: ToolTrivy},
		"name:":           {ID: "a", Name: " ", Tool: ToolTrivy},
		"tool:":           {ID: "a", Name: "x", Tool: "tfsec"},
		"deny_at: only":   {ID: "a", Name: "x", Tool: ToolCheckov, DenyAt: "high"},
		"deny_at: \"big": {ID: "a", Name: "x", Tool: ToolTrivy, DenyAt: "big"},
		"skip[0]":         {ID: "a", Name: "x", Tool: ToolTrivy, Skip: []string{"--flag x"}},
		"deny: a Conf":    {ID: "a", Name: "x", Tool: ToolConftest, Rules: rego, Deny: []string{"x"}},
		"rules: only":     {ID: "a", Name: "x", Tool: ToolTrivy, Rules: rego},
		"rules: a Conf":   {ID: "a", Name: "x", Tool: ToolConftest},
		`rules["../x.re`:  {ID: "a", Name: "x", Tool: ToolConftest, Rules: map[string]string{"../x.rego": "package main"}},
		`rules["a.rego"]`: {ID: "a", Name: "x", Tool: ToolConftest, Rules: map[string]string{"a.rego": "  "}},
		`rules["b.rego"]`: {ID: "a", Name: "x", Tool: ToolConftest, Rules: map[string]string{"b.rego": strings.Repeat("x", MaxRuleFileSize+1)}},
	} {
		if err := p.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want an error with %q, got %v", want, err)
		}
	}
}

// TestPackCommandsFetchNothing: the container has no network, so each tool is told not to try.
func TestPackCommandsFetchNothing(t *testing.T) {
	for tool, flags := range map[string][]string{
		ToolTrivy:    {"--skip-check-update", "--skip-version-check", "/in/tfplan.json"},
		ToolCheckov:  {"--skip-download", "--soft-fail", "terraform_plan", "/in/tfplan.json"},
		ToolConftest: {"/in/policy", "/in/tfplan.json", "json"},
	} {
		cmd := " " + strings.Join(Pack{Tool: tool}.Command(), " ") + " "
		for _, f := range flags {
			if !strings.Contains(cmd, " "+f+" ") {
				t.Errorf("%s: the command lacks %s: %s", tool, f, cmd)
			}
		}
		if !strings.Contains(Tools[tool].Image, "@sha256:") {
			t.Errorf("%s: the image is not named by digest", tool)
		}
	}
}
