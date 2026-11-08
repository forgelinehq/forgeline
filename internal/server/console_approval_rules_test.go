package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/authn"
)

func TestApprovalRuleMatchingAndStrictestWins(t *testing.T) {
	rs := approvalRules{Rules: []approvalRule{
		{Name: "prod two", Accounts: []string{"primary"}, Approvals: 2},
		{Name: "leads", Kinds: []string{"ai"}, Approvers: []string{"ada", "grace", "lin"}, Approvals: 1},
		{Name: "deletes", Kinds: []string{"cloud"}, HighRisk: true, Approvers: []string{"grace", "lin"}, Approvals: 2},
	}}
	for name, tc := range map[string]struct {
		kind, account string
		high          bool
		needed        int
		approvers     []string
		rules         []string
	}{
		"nothing matches":         {"cloud", "old-account", false, 1, nil, nil},
		"primary, an AI change":   {"ai", "", false, 2, []string{"ada", "grace", "lin"}, []string{"prod two", "leads"}},
		"old account, AI":         {"ai", "old-account", false, 1, []string{"ada", "grace", "lin"}, []string{"leads"}},
		"a stack deletion":        {"cloud", "old-account", true, 2, []string{"grace", "lin"}, []string{"deletes"}},
		"a normal console change": {"cloud", "old-account", false, 1, nil, nil},
		"primary deletion":        {"cloud", "primary", true, 2, []string{"grace", "lin"}, []string{"prod two", "deletes"}},
	} {
		n := rs.needFor(tc.kind, tc.account, tc.high)
		if n.Needed != tc.needed || strings.Join(n.Approvers, ",") != strings.Join(tc.approvers, ",") || strings.Join(n.Rules, ",") != strings.Join(tc.rules, ",") {
			t.Errorf("%s: got %+v", name, n)
		}
	}
	// Two lists that share nobody leave nobody: the strictest, not the loosest.
	none := approvalRules{Rules: []approvalRule{{Name: "a", Approvers: []string{"ada"}, Approvals: 1}, {Name: "b", Approvers: []string{"grace"}, Approvals: 1}}}.needFor("ai", "", false)
	if none.Approvers == nil || len(none.Approvers) != 0 || none.mayApprove("ada") || none.mayApprove("grace") {
		t.Errorf("disjoint approver lists: %+v", none)
	}
	if !(approvalNeed{}).mayApprove("anyone") {
		t.Error("no approver list means any administrator")
	}
}

func approvalOf(t *testing.T, b *browser, id string) AIProposal {
	t.Helper()
	var p AIProposal
	json.Unmarshal(b.expect(200, "GET", "/v1/ai/proposals/"+id, nil), &p)
	return p
}

func TestTwoApprovalsRunOnlyAtTheSecondFromAnotherAdministrator(t *testing.T) {
	m := newMCPHarness(t)
	m.turnOn()
	m.admin.createUser("lin", authn.Admin)
	lin := m.harness.login("lin")

	// No rules: one approval, as before.
	if v := approvalOf(t, m.v, mustPropose(t, m)); v.Approval == nil || v.Approval.Needed != 1 {
		t.Fatalf("no rules: %+v", v.Approval)
	}
	m.admin.expect(200, "PUT", "/v1/settings/approvals", map[string]any{"rules": []map[string]any{{"name": "Primary needs two", "accounts": []string{"primary"}, "approvals": 2}}})

	id := mustPropose(t, m)
	p := approvalOf(t, m.v, id)
	if p.Approval == nil || p.Approval.Needed != 2 || len(p.Approval.Given) != 0 || strings.Join(p.Approval.Rules, ",") != "Primary needs two" {
		t.Fatalf("approval state: %+v", p.Approval)
	}
	// The first approval is recorded; nothing runs.
	m.admin2.expect(200, "POST", "/v1/ai/proposals/"+id+"/approve", map[string]any{"hash": p.Hash, "note": "looks right"})
	p = approvalOf(t, m.v, id)
	if p.Status != apProposed || len(p.Approvals) != 1 || p.Approvals[0].By != "grace" || p.Approvals[0].Note != "looks right" || m.mcp.count("aws___run_script") != 0 {
		t.Fatalf("after one approval: status %s, approvals %+v, runs %d", p.Status, p.Approvals, m.mcp.count("aws___run_script"))
	}
	// The same person cannot approve twice; the proposer still cannot approve at all.
	m.admin2.expect(409, "POST", "/v1/ai/proposals/"+id+"/approve", map[string]any{"hash": p.Hash})
	if m.mcp.count("aws___run_script") != 0 {
		t.Fatal("a repeated approval ran the change")
	}
	own := mustProposeAs(t, m, m.admin)
	m.admin.expect(403, "POST", "/v1/ai/proposals/"+own+"/approve", map[string]any{"hash": approvalOf(t, m.v, own).Hash})
	// A second, different administrator completes it, and it runs.
	lin.expect(202, "POST", "/v1/ai/proposals/"+id+"/approve", map[string]any{"hash": p.Hash})
	done := m.waitProposal(id, apApplying)
	if done.Status != apApplied || done.DecidedBy != "lin" || len(done.Approvals) != 2 {
		t.Errorf("after two approvals: %+v", done)
	}
	if a := string(m.admin.expect(200, "GET", "/v1/audit", nil)); !strings.Contains(a, "approval 1 of 2 recorded") || !strings.Contains(a, "approval.rules") {
		t.Error("the audit log does not show the first approval or the rule change")
	}
}

func TestNamedApproversAndValidation(t *testing.T) {
	m := newMCPHarness(t)
	m.turnOn()
	m.admin.createUser("lin", authn.Admin)
	lin := m.harness.login("lin")
	// Only administrators change rules; developers can read them.
	m.dev.expect(403, "PUT", "/v1/settings/approvals", map[string]any{"rules": []any{}})
	m.dev.expect(200, "GET", "/v1/settings/approvals", nil)
	for name, rule := range map[string]map[string]any{
		"no name":                  {"approvals": 1},
		"three approvals":          {"name": "x", "approvals": 3},
		"unknown account":          {"name": "x", "approvals": 1, "accounts": []string{"nope"}},
		"unknown kind":             {"name": "x", "approvals": 1, "kinds": []string{"terraform"}},
		"approver not an admin":    {"name": "x", "approvals": 1, "approvers": []string{"dev"}},
		"two approvals, one named": {"name": "x", "approvals": 2, "approvers": []string{"lin"}},
	} {
		if resp, _ := m.admin.do("PUT", "/v1/settings/approvals", map[string]any{"rules": []any{rule}}); resp.StatusCode != 400 {
			t.Errorf("%s: %d, want 400", name, resp.StatusCode)
		}
	}
	m.admin.expect(200, "PUT", "/v1/settings/approvals", map[string]any{"rules": []map[string]any{{"name": "Leads only", "kinds": []string{"ai"}, "approvers": []string{"lin"}, "approvals": 1}}})
	id := mustPropose(t, m)
	p := approvalOf(t, m.v, id)
	if strings.Join(p.Approval.Approvers, ",") != "lin" {
		t.Errorf("approvers shown: %+v", p.Approval)
	}
	m.admin2.expect(403, "POST", "/v1/ai/proposals/"+id+"/approve", map[string]any{"hash": p.Hash})
	if m.mcp.count("aws___run_script") != 0 {
		t.Fatal("someone the rule does not name approved it")
	}
	lin.expect(202, "POST", "/v1/ai/proposals/"+id+"/approve", map[string]any{"hash": p.Hash})
	if done := m.waitProposal(id, apApplying); done.Status != apApplied {
		t.Errorf("the named approver could not approve: %+v", done)
	}
	// An unreadable rule set refuses approval rather than falling back to one approval.
	m.srv.docs.Put(t.Context(), kindSettings, idApprovalRules, "not rules")
	id2 := mustPropose(t, m)
	lin.expect(409, "POST", "/v1/ai/proposals/"+id2+"/approve", map[string]any{"hash": approvalOf(t, m.v, id2).Hash})
}

func TestConsoleCloudChangesFollowTheRules(t *testing.T) {
	c := newCloudHarness(t)
	c.connectAWS()
	c.admin.createUser("lin", authn.Admin)
	lin := c.harness.login("lin")
	c.admin.expect(200, "PUT", "/v1/settings/approvals", map[string]any{"rules": []map[string]any{{"name": "Console changes need two", "kinds": []string{"cloud"}, "approvals": 2}}})
	ch := c.waitChange(c.proposeDeploy(c.dev, nil).ID, stPreparing)
	if ch.Status != stProposed || ch.Approval == nil || ch.Approval.Needed != 2 {
		t.Fatalf("proposed: %s %+v", ch.Status, ch.Approval)
	}
	c.admin.expect(202, "POST", "/v1/cloud/changes/"+ch.ID+"/approve", map[string]any{})
	var after CloudChange
	json.Unmarshal(c.admin.expect(200, "GET", "/v1/cloud/changes/"+ch.ID, nil), &after)
	if after.Status != stProposed || len(after.Approvals) != 1 || c.cli.called("cloudformation execute-change-set") != 0 {
		t.Fatalf("after one approval: %s %+v, executes %d", after.Status, after.Approvals, c.cli.called("cloudformation execute-change-set"))
	}
	c.admin.expect(409, "POST", "/v1/cloud/changes/"+ch.ID+"/approve", map[string]any{})
	lin.expect(202, "POST", "/v1/cloud/changes/"+ch.ID+"/approve", map[string]any{})
	if done := c.waitChange(ch.ID, stProposed, stExecuting); done.Status != stSucceeded || done.DecidedBy != "lin" {
		t.Errorf("after two approvals: %+v", done)
	}
	// AI-only rules do not touch console changes.
	c.admin.expect(200, "PUT", "/v1/settings/approvals", map[string]any{"rules": []map[string]any{{"name": "AI needs two", "kinds": []string{"ai"}, "approvals": 2}}})
	two := c.waitChange(c.proposeDeploy(c.dev, func(m map[string]any) { m["reason"] = "a second demo stack for the team" }).ID, stPreparing)
	if two.Approval == nil || two.Approval.Needed != 1 {
		t.Errorf("an AI-only rule applied to a console change: %+v", two.Approval)
	}
}

func mustProposeAs(t *testing.T, m *mcpHarness, b *browser) string {
	t.Helper()
	_, id := m.propose(b, changeScript)
	if id == "" {
		t.Fatal("no proposal was filed")
	}
	return id
}

func mustPropose(t *testing.T, m *mcpHarness) string { return mustProposeAs(t, m, m.dev) }
