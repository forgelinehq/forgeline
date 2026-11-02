package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/authn"
)

func policyOf(t *testing.T, b *browser, integ string) PolicyJSON {
	t.Helper()
	var v PolicyJSON
	if err := json.Unmarshal(b.expect(200, "GET", "/v1/integrations/"+integ+"/policy", nil), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func levelOf(v PolicyJSON, tool string) string {
	for _, t := range v.Tools {
		if t.Name == tool {
			return t.Level
		}
	}
	return "missing"
}

var polDev = Principal{Name: "dev", Role: authn.Developer}

func policyHarness(t *testing.T) (*mcpHarness, *awsStub) {
	m := newMCPHarness(t)
	stub := &awsStub{}
	m.srv.run = integRun(stub, true, "")
	m.admin.expect(200, "PUT", "/v1/integrations/aws", map[string]any{"enabled": true, "profile": "dev", "region": "ap-south-1"})
	return m, stub
}

func TestPolicyDefaultsToAllowAndListsEachIntegrationsTools(t *testing.T) {
	m, _ := policyHarness(t)
	v := policyOf(t, m.dev, "aws")
	if !v.Readable || len(v.Tools) < 5 {
		t.Fatalf("aws policy = %+v", v)
	}
	for _, tl := range v.Tools {
		if tl.Level != polAllow {
			t.Errorf("%s starts at %s; nothing is blocked until someone blocks it", tl.Name, tl.Level)
		}
	}
	if k := map[string]string{}; true {
		for _, tl := range v.Tools {
			k[tl.Name] = tl.Kind
		}
		if k["call_aws"] != "read" || k["aws_summary"] != "read" || k[toolProposeCloud] != "write" {
			t.Errorf("kinds = %v", k)
		}
	}
	mcp := policyOf(t, m.dev, "aws-mcp")
	if levelOf(mcp, "aws_script") != polAllow || levelOf(mcp, "propose_aws_change") != polAllow || levelOf(mcp, "call_aws") != "missing" {
		t.Errorf("aws-mcp tools = %+v", mcp.Tools)
	}
	k8s := policyOf(t, m.dev, "kubernetes")
	if levelOf(k8s, "k8s_clusters") != polAllow || levelOf(k8s, "k8s_resources") != polAllow || len(k8s.Tools) != 2 {
		t.Errorf("kubernetes tools = %+v", k8s.Tools)
	}
	// Forgeline's own tools belong to no connection and are never listed.
	for _, v := range []PolicyJSON{v, mcp, k8s} {
		for _, tl := range v.Tools {
			if tl.Name == "list_runs" || tl.Name == "search_audit" {
				t.Errorf("%s is Forgeline's own tool and has no policy", tl.Name)
			}
		}
	}
	m.v.expect(403, "GET", "/v1/integrations/aws/policy", nil)
	m.admin.expect(404, "GET", "/v1/integrations/github/policy", nil)
	m.admin.expect(404, "GET", "/v1/integrations/nope/policy", nil)
}

func TestABlockedToolNeverRunsAndEveryRouteIsGated(t *testing.T) {
	m, stub := policyHarness(t)
	ctx := context.Background()
	if _, err := m.srv.runLookup(ctx, polDev, lookupCall{Tool: "aws_accounts", Args: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("before blocking: %v", err)
	}
	m.admin.expect(200, "PUT", "/v1/integrations/aws/policy", map[string]any{"tools": map[string]string{"aws_summary": "block", "aws_resources": "block", "aws_accounts": "allow"}})

	before := len(stub.calls)
	for _, tool := range []string{"aws_summary", "aws_resources"} {
		_, err := m.srv.runLookup(ctx, polDev, lookupCall{Tool: tool, Args: json.RawMessage(`{}`)})
		if err == nil || !strings.Contains(err.Error(), "blocked by the access policy for AWS") {
			t.Errorf("%s was not refused: %v", tool, err)
		}
	}
	if len(stub.calls) != before {
		t.Errorf("a blocked tool reached the command line: %v", stub.calls[before:])
	}
	// A tool that is not blocked still works, and so does the same connection's other tools.
	if _, err := m.srv.runLookup(ctx, polDev, lookupCall{Tool: "aws_accounts", Args: json.RawMessage(`{}`)}); err != nil {
		t.Errorf("an allowed tool was refused: %v", err)
	}
	// The administrator is not exempt, and neither is an agent whose own list includes the tool.
	if _, err := m.srv.runLookup(ctx, Principal{Name: "ada", Role: authn.Admin}, lookupCall{Tool: "aws_summary", Args: json.RawMessage(`{}`)}); err == nil {
		t.Error("an administrator ran a blocked tool")
	}
	agentCtx := context.WithValue(ctx, agentScopeKey{}, &agentScope{tools: map[string]bool{"aws_summary": true}, label: "t@1"})
	if _, err := m.srv.runLookup(agentCtx, polDev, lookupCall{Tool: "aws_summary", Args: json.RawMessage(`{}`)}); err == nil || !strings.Contains(err.Error(), "blocked by the access policy") {
		t.Errorf("an agent that lists the tool still ran it: %v", err)
	}
	// Blocked tools are not shown to the model, and not offered over MCP.
	help := m.srv.lookupHelp(ctx, authn.Admin)
	if strings.Contains(help, "- aws_summary ") || strings.Contains(help, "- aws_resources ") || !strings.Contains(help, "- aws_accounts ") || !strings.Contains(help, "- list_runs ") {
		t.Errorf("lookup help = %s", help)
	}
	names := map[string]bool{}
	for _, tl := range m.srv.mcpTools(ctx, authn.Developer) {
		names[tl.Name] = true
	}
	if names["aws_summary"] || names["aws_resources"] || !names["aws_accounts"] {
		t.Errorf("mcp tools = %v", names)
	}
	// Every refusal is audited against the integration, and shows in its Activity.
	var refused int
	for _, e := range activityOf(t, m.admin, "aws") {
		if e.Action == "policy.refused" && !e.Allowed && strings.Contains(e.Detail, "blocked by the access policy") {
			refused++
		}
	}
	if refused != 4 { // two direct, the administrator, the agent
		t.Errorf("%d refusals in the activity, want 4", refused)
	}
	// Unblocking puts it back.
	m.admin.expect(200, "PUT", "/v1/integrations/aws/policy", map[string]any{"tools": map[string]string{}})
	if _, err := m.srv.runLookup(ctx, polDev, lookupCall{Tool: "aws_summary", Args: json.RawMessage(`{}`)}); err != nil {
		t.Errorf("after unblocking: %v", err)
	}
}

func TestAWriteToolCanBeBlockedButNeverSetToAllow(t *testing.T) {
	m, _ := policyHarness(t)
	m.admin.expect(200, "PUT", "/v1/integrations/aws/mcp", map[string]any{"enabled": true})
	var e struct{ Error string }
	json.Unmarshal(m.admin.expect(400, "PUT", "/v1/integrations/aws-mcp/policy", map[string]any{"tools": map[string]string{"propose_aws_change": "allow"}}), &e)
	if !strings.Contains(e.Error, "always a proposal") {
		t.Errorf("error = %q", e.Error)
	}
	m.admin.expect(400, "PUT", "/v1/integrations/aws/policy", map[string]any{"tools": map[string]string{toolProposeCloud: "allow"}})
	m.admin.expect(200, "PUT", "/v1/integrations/aws-mcp/policy", map[string]any{"tools": map[string]string{"propose_aws_change": "block"}})
	m.admin.expect(200, "PUT", "/v1/integrations/aws/policy", map[string]any{"tools": map[string]string{toolProposeCloud: "block"}})
	ctx := context.Background()
	if err := m.srv.policyGate(ctx, polDev, toolProposeCloud); err == nil {
		t.Error("propose_cloud_change (used over MCP) ran although it is blocked")
	}
	if _, err := m.srv.runLookup(ctx, polDev, lookupCall{Tool: "propose_aws_change", Args: json.RawMessage(`{"title":"a change","reason":"for the test run","code":"x"}`)}); err == nil || !strings.Contains(err.Error(), "blocked by the access policy for the AWS MCP Server") {
		t.Errorf("propose_aws_change = %v", err)
	}
	for _, tl := range m.srv.mcpTools(ctx, authn.Developer) {
		if tl.Name == toolProposeCloud || tl.Name == "propose_aws_change" {
			t.Errorf("%s is still offered over MCP", tl.Name)
		}
	}
}

func TestOnlyAdministratorsChangeThePolicyAndOnlyWithKnownTools(t *testing.T) {
	m, _ := policyHarness(t)
	m.dev.expect(403, "PUT", "/v1/integrations/aws/policy", map[string]any{"tools": map[string]string{"aws_summary": "block"}})
	m.v.expect(403, "PUT", "/v1/integrations/aws/policy", map[string]any{"tools": map[string]string{"aws_summary": "block"}})
	for _, c := range []struct {
		integ string
		tools map[string]string
	}{
		{"aws", map[string]string{"nope": "block"}},
		{"aws", map[string]string{"k8s_clusters": "block"}}, // another integration's tool
		{"aws", map[string]string{"list_runs": "block"}},    // Forgeline's own tool
		{"aws", map[string]string{"aws_summary": "maybe"}},  // not a level
		{"aws", map[string]string{"aws_summary": "ask"}},    // needs-approval is not available for reads yet
		{"kubernetes", map[string]string{"aws_summary": "block"}},
	} {
		m.admin.expect(400, "PUT", "/v1/integrations/"+c.integ+"/policy", map[string]any{"tools": c.tools})
	}
	m.admin.expect(404, "PUT", "/v1/integrations/github/policy", map[string]any{"tools": map[string]string{}})
	// Nothing was changed by the refused requests.
	if v := policyOf(t, m.admin, "aws"); levelOf(v, "aws_summary") != polAllow {
		t.Errorf("a refused request changed the policy: %+v", v.Tools)
	}
	// A change keeps the other integrations' blocks, and is audited with before and after.
	m.admin.expect(200, "PUT", "/v1/integrations/kubernetes/policy", map[string]any{"tools": map[string]string{"k8s_resources": "block"}})
	m.admin.expect(200, "PUT", "/v1/integrations/aws/policy", map[string]any{"tools": map[string]string{"call_aws": "block"}})
	m.admin.expect(200, "PUT", "/v1/integrations/aws/policy", map[string]any{"tools": map[string]string{"aws_summary": "block"}})
	if v := policyOf(t, m.admin, "kubernetes"); levelOf(v, "k8s_resources") != polBlock {
		t.Errorf("changing AWS undid Kubernetes: %+v", v.Tools)
	}
	if v := policyOf(t, m.admin, "aws"); levelOf(v, "call_aws") != polAllow || levelOf(v, "aws_summary") != polBlock {
		t.Errorf("a request sets this integration's tools, it does not add to them: %+v", v.Tools)
	}
	aud := string(m.admin.expect(200, "GET", "/v1/audit?action=policy.update", nil))
	if !strings.Contains(aud, "blocked: [aws_summary] (was [call_aws])") {
		t.Errorf("the change is not audited with before and after: %s", aud)
	}
	// The hub says which integrations have a policy and how many tools are blocked.
	h := hub(t, m.dev)
	if !h["aws"].HasPolicy || !h["kubernetes"].HasPolicy || h["docker"].HasPolicy || h["github"].HasPolicy || h["lambda"].HasPolicy {
		t.Errorf("has_policy: aws %v k8s %v docker %v github %v lambda %v", h["aws"].HasPolicy, h["kubernetes"].HasPolicy, h["docker"].HasPolicy, h["github"].HasPolicy, h["lambda"].HasPolicy)
	}
	if w := strings.Join(h["aws"].Warnings, " "); !strings.Contains(w, "1 tool is blocked") {
		t.Errorf("aws warnings = %q", w)
	}
}

func TestAnUnreadablePolicyFailsClosedButAMissingOneAllows(t *testing.T) {
	m, stub := policyHarness(t)
	ctx := context.Background()
	if _, err := m.srv.runLookup(ctx, polDev, lookupCall{Tool: "aws_accounts", Args: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("a policy that was never set must allow: %v", err)
	}
	m.srv.docs.Put(ctx, kindSettings, idPolicy, "this is not a policy") // stored, but not something it can read
	before := len(stub.calls)
	_, err := m.srv.runLookup(ctx, polDev, lookupCall{Tool: "aws_accounts", Args: json.RawMessage(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Errorf("an unreadable policy must refuse: %v", err)
	}
	if len(stub.calls) != before {
		t.Errorf("a refused tool reached the command line: %v", stub.calls[before:])
	}
	if v := policyOf(t, m.admin, "aws"); v.Readable || levelOf(v, "call_aws") != polBlock {
		t.Errorf("view = %+v", v)
	}
	m.admin.expect(409, "PUT", "/v1/integrations/aws/policy", map[string]any{"tools": map[string]string{"call_aws": "block"}}) // not overwritten blindly
	if help := m.srv.lookupHelp(ctx, authn.Admin); strings.Contains(help, "- aws_accounts ") || !strings.Contains(help, "- list_runs ") {
		t.Errorf("help = %s", help)
	}
	// Forgeline's own tools are not affected by the policy.
	if _, err := m.srv.runLookup(ctx, polDev, lookupCall{Tool: "list_pipelines", Args: json.RawMessage(`{}`)}); err != nil {
		t.Errorf("a Forgeline tool was refused because the policy is unreadable: %v", err)
	}
	if w := strings.Join(hub(t, m.dev)["aws"].Warnings, " "); !strings.Contains(w, "cannot be read") {
		t.Errorf("the hub does not say the policy is unreadable: %q", w)
	}
}

func TestEffectiveAccessSaysWhyForEachAgentAndTool(t *testing.T) {
	m, _ := policyHarness(t)
	newAgent(t, m.admin, agentBody("Reviewer", nil, []string{"aws_summary", "call_aws"}, "T0"))
	newAgent(t, m.admin, agentBody("Reader", nil, []string{"aws_accounts"}, "T0"))
	m.admin.expect(200, "PUT", "/v1/integrations/aws/policy", map[string]any{"tools": map[string]string{"call_aws": "block"}})
	v := policyOf(t, m.admin, "aws")
	byName := map[string]PolicyAgentJSON{}
	for _, a := range v.Agents {
		byName[a.Name] = a
	}
	state := func(agent, tool string) (string, string) {
		for _, x := range byName[agent].Tools {
			if x.Tool == tool {
				return x.State, x.Reason
			}
		}
		return "missing", ""
	}
	if s, why := state("Reviewer", "aws_summary"); s != "allowed" || !strings.Contains(why, "not blocked") {
		t.Errorf("reviewer aws_summary = %s (%s)", s, why)
	}
	if s, why := state("Reviewer", "call_aws"); s != "blocked" || !strings.Contains(why, "access policy") {
		t.Errorf("reviewer call_aws = %s (%s)", s, why)
	}
	if s, why := state("Reviewer", "aws_resources"); s != "not_listed" || !strings.Contains(why, "tool list") {
		t.Errorf("reviewer aws_resources = %s (%s)", s, why)
	}
	if s, _ := state("Reader", "aws_accounts"); s != "allowed" {
		t.Errorf("reader aws_accounts = %s", s)
	}
	// Blocked beats a listed tool for the agent too, and the list of agents is the same for every tool.
	if s, _ := state("Reader", "call_aws"); s != "blocked" {
		t.Errorf("a blocked tool is blocked for an agent that does not list it too: %s", s)
	}
	if len(byName["Reader"].Tools) != len(v.Tools) {
		t.Errorf("each agent should get a row per tool")
	}
}

// What an AI tool connected over MCP sees: a blocked tool is not offered, and calling it anyway is refused and
// audited. Unblocking puts it back. (The gate inside the call is a second layer; the offer list is the first.)
func TestAMcpClientCannotUseOrSeeABlockedTool(t *testing.T) {
	m := newMCPHarness(t)
	m.turnOn()
	dev := m.newToken(m.dev, map[string]any{"name": "d"})
	args := map[string]any{"kind": "instance-action", "instance_id": "i-0123456789abcdef0", "action": "stop", "reason": "It is idle over the weekend"}

	if !m.toolNames(dev)[toolProposeCloud] || !m.toolNames(dev)["aws_accounts"] {
		t.Fatalf("before blocking the tools should be offered: %v", m.toolNames(dev))
	}
	m.admin.expect(200, "PUT", "/v1/integrations/aws/policy", map[string]any{"tools": map[string]string{toolProposeCloud: "block", "aws_accounts": "block"}})
	names := m.toolNames(dev)
	if names[toolProposeCloud] || names["aws_accounts"] || !names["list_runs"] {
		t.Errorf("a blocked tool is still offered, or Forgeline's own tools vanished: %v", names)
	}
	if out, isErr := m.tool(dev, toolProposeCloud, args); !isErr || !strings.Contains(out, "not available") {
		t.Errorf("a blocked tool ran over MCP: %q", out)
	}
	var list struct{ Changes []CloudChange }
	json.Unmarshal(m.admin.expect(200, "GET", "/v1/cloud/changes", nil), &list)
	if len(list.Changes) != 0 {
		t.Errorf("a blocked proposal tool still filed a proposal: %+v", list.Changes)
	}
	if !strings.Contains(m.auditText(), "mcp.denied") {
		t.Error("the refused call was not audited")
	}
	m.admin.expect(200, "PUT", "/v1/integrations/aws/policy", map[string]any{"tools": map[string]string{}})
	if out, isErr := m.tool(dev, toolProposeCloud, args); isErr || !strings.Contains(out, "Nothing has run") {
		t.Errorf("after unblocking: %q", out)
	}
}
