package server

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
)

func cloudsHarness(t *testing.T) (*mcpHarness, *gcloudStub, *azStub) {
	g := &gcloudStub{assets: gcpAssetsJSON, project: "shop-prod"}
	a := &azStub{rows: []map[string]any{azRow("microsoft.compute/virtualmachines", "vm-web", map[string]any{"properties": map[string]any{"extended": map[string]any{"instanceView": map[string]any{"powerState": map[string]any{"code": "PowerState/running"}}}}})}}
	m := newMCPHarness(t)
	m.srv.run = func(ctx context.Context, name string, args ...string) ([]byte, string, error) {
		switch name {
		case "gcloud":
			return g.run(ctx, name, args...)
		case "az":
			return a.run(ctx, name, args...)
		}
		return integRun(&awsStub{}, true, "")(ctx, name, args...)
	}
	m.admin.expect(200, "PUT", "/v1/integrations/gcp", map[string]any{"enabled": true, "scope": "shop-prod"})
	m.admin.expect(200, "PUT", "/v1/integrations/azure", map[string]any{"enabled": true})
	g.mu.Lock()
	g.searches = 0 // connecting reads once to check; the tests count what the assistant does
	g.mu.Unlock()
	return m, g, a
}

func (g *gcloudStub) searchCount() int { g.mu.Lock(); defer g.mu.Unlock(); return g.searches }

func setPolicy(m *mcpHarness, integ string, tools map[string]string) {
	m.t.Helper()
	m.admin.expect(200, "PUT", "/v1/integrations/"+integ+"/policy", map[string]any{"tools": tools})
}

func TestTheAssistantCanReadGoogleCloudAndAzureAndTheyHaveAPolicy(t *testing.T) {
	m, g, a := cloudsHarness(t)
	rep := m.ask(m.dev, lookupReply("gcp_resources", map[string]string{}))
	if len(rep.Lookups) != 1 || rep.Lookups[0] != "gcp_resources" {
		t.Fatalf("lookups = %v", rep.Lookups)
	}
	res := m.llm.lastUser(1)
	for _, want := range []string{"web-1", "shop-prod", "shop-assets"} {
		if !strings.Contains(res, want) {
			t.Errorf("gcp result lacks %q:\n%s", want, res)
		}
	}
	m.ask(m.dev, lookupReply("gcp_resources", map[string]string{"service": "storage"}))
	if res := m.llm.lastUser(1); !strings.Contains(res, "shop-assets") || strings.Contains(res, "web-1") {
		t.Errorf("the service filter did not narrow the result:\n%s", res)
	}
	m.ask(m.dev, lookupReply("azure_resources", map[string]string{}))
	if res := m.llm.lastUser(1); !strings.Contains(res, "vm-web") {
		t.Errorf("azure result:\n%s", res)
	}
	if g.searchCount() != 1 {
		t.Errorf("the assistant did not use the cached inventory: %d searches", g.searchCount())
	}
	_ = a
	// Both are listed over MCP and have a policy with their tool.
	tok := m.newToken(m.dev, map[string]any{"name": "d"})
	if n := m.toolNames(tok); !n["gcp_resources"] || !n["azure_resources"] {
		t.Errorf("mcp tools = %v", n)
	}
	for integ, tool := range map[string]string{"gcp": "gcp_resources", "azure": "azure_resources"} {
		v := policyOf(t, m.dev, integ)
		if len(v.Tools) != 1 || v.Tools[0].Name != tool || v.Tools[0].Kind != "read" || v.Tools[0].Level != polAllow {
			t.Errorf("%s policy = %+v", integ, v.Tools)
		}
	}
	h := hub(t, m.dev)
	if !h["gcp"].HasPolicy || !h["azure"].HasPolicy {
		t.Error("the hub does not offer an Access tab for Google Cloud and Azure")
	}
	// Blocking it stops the read before any command runs, for everyone.
	g2 := g.searchCount()
	setPolicy(m, "gcp", map[string]string{"gcp_resources": "block"})
	m.srv.gcpInv.entries = nil
	_, err := m.srv.runLookup(context.Background(), Principal{Name: "ada", Role: authn.Admin}, lookupCall{Tool: "gcp_resources", Args: json.RawMessage(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "blocked by the access policy for Google Cloud") {
		t.Errorf("a blocked gcp tool ran: %v", err)
	}
	if g.searchCount() != g2 {
		t.Error("a blocked tool reached gcloud")
	}
	setPolicy(m, "azure", map[string]string{"azure_resources": "block"})
	if _, err := m.srv.runLookup(context.Background(), polDev, lookupCall{Tool: "azure_resources", Args: json.RawMessage(`{}`)}); err == nil || !strings.Contains(err.Error(), "for Azure") {
		t.Errorf("a blocked azure tool ran: %v", err)
	}
}

func TestTheCloudLookupsRefuseWhenNotConnected(t *testing.T) {
	m := newMCPHarness(t)
	for tool, want := range map[string]string{"gcp_resources": "Google Cloud is not connected", "azure_resources": "Azure is not connected"} {
		_, err := m.srv.runLookup(context.Background(), polDev, lookupCall{Tool: tool, Args: json.RawMessage(`{}`)})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s = %v", tool, err)
		}
	}
}

// streamed runs one assistant stream in the background and returns what it produced when it ends.
type streamed struct {
	done chan struct{}
	body []byte
	code int
}

func (m *mcpHarness) stream(c *browser, lookup string) *streamed {
	m.llm.mu.Lock()
	m.llm.replies, m.llm.reqs = []string{lookup, "done"}, nil
	m.llm.mu.Unlock()
	st := &streamed{done: make(chan struct{})}
	go func() {
		defer close(st.done)
		resp, body := c.do("POST", "/v1/assistant/runs", map[string]any{"message": "what is in google cloud?"})
		st.code, st.body = resp.StatusCode, body
	}()
	return st
}

// waitPending returns the id of the one question waiting for an answer.
func (m *mcpHarness) waitPending(t *testing.T) string {
	t.Helper()
	for i := 0; i < 300; i++ {
		m.srv.approvals.mu.Lock()
		for id := range m.srv.approvals.pending {
			m.srv.approvals.mu.Unlock()
			return id
		}
		m.srv.approvals.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no question is waiting for an answer")
	return ""
}

func answerOf(t *testing.T, st *streamed) (events []map[string]any, result map[string]any) {
	t.Helper()
	select {
	case <-st.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not finish")
	}
	events = frames(t, st.body)
	for _, f := range events {
		if f["type"] == "TOOL_CALL_RESULT" {
			result = f
		}
	}
	return
}

func TestAReadThatNeedsApprovalWaitsForThePersonAndRunsOnlyAfterTheyApprove(t *testing.T) {
	m, g, _ := cloudsHarness(t)
	setPolicy(m, "gcp", map[string]string{"gcp_resources": "ask"})
	if v := policyOf(t, m.dev, "gcp"); v.Tools[0].Level != polAsk {
		t.Fatalf("level = %s", v.Tools[0].Level)
	}
	st := m.stream(m.dev, lookupReply("gcp_resources", map[string]string{"service": "compute"}))
	id := m.waitPending(t)
	time.Sleep(50 * time.Millisecond)
	if g.searchCount() != 0 {
		t.Fatal("the tool ran before anyone approved it")
	}
	// The question is somebody else's to answer: another person cannot, and a viewer cannot reach the endpoint.
	m.admin.expect(404, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "approve"})
	m.v.expect(403, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "approve"})
	m.dev.expect(400, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "yes please"})
	m.dev.expect(404, "POST", "/v1/assistant/approvals/not-a-real-id", map[string]any{"decision": "approve"})
	if g.searchCount() != 0 {
		t.Fatal("a refused answer let the tool run")
	}
	m.dev.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "approve"})
	m.dev.expect(404, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "approve"}) // an answer counts once

	events, res := answerOf(t, st)
	if res == nil || !strings.Contains(res["content"].(string), "web-1") {
		t.Fatalf("no result after approval: %v", res)
	}
	var ask, decided map[string]any
	var order []string
	for _, f := range events {
		if f["type"] == "CUSTOM" {
			order = append(order, f["name"].(string))
			switch f["name"] {
			case "forgeline.approval":
				ask = f["value"].(map[string]any)
			case "forgeline.approval_decided":
				decided = f["value"].(map[string]any)
			}
		} else {
			order = append(order, f["type"].(string))
		}
	}
	if ask["id"] != id || ask["tool"] != "gcp_resources" || ask["integration"] != "gcp" || !strings.Contains(ask["args"].(string), "compute") || ask["expiresInSeconds"].(float64) < 30 {
		t.Errorf("the card carries %v", ask)
	}
	if decided["decision"] != "approve" || decided["id"] != id {
		t.Errorf("decided = %v", decided)
	}
	if got := strings.Join(order, ","); !strings.Contains(got, "TOOL_CALL_START,TOOL_CALL_ARGS,TOOL_CALL_END,forgeline.approval,forgeline.approval_decided,TOOL_CALL_RESULT") {
		t.Errorf("the approval does not sit between the call and its result: %s", got)
	}
	aud := string(m.admin.expect(200, "GET", "/v1/audit?action=policy.approval", nil))
	if !strings.Contains(aud, "gcp_resources: approved by dev") || !strings.Contains(aud, "integration/gcp") {
		t.Errorf("the approval is not audited: %s", aud)
	}
	// Asking again asks again: an approval is for one call.
	st2 := m.stream(m.dev, lookupReply("gcp_resources", map[string]string{}))
	id2 := m.waitPending(t)
	if id2 == id {
		t.Error("an approval id was reused")
	}
	m.dev.expect(200, "POST", "/v1/assistant/approvals/"+id2, map[string]any{"decision": "deny"})
	answerOf(t, st2)
}

func TestADeniedOrIgnoredQuestionNeverRunsTheTool(t *testing.T) {
	m, g, _ := cloudsHarness(t)
	setPolicy(m, "gcp", map[string]string{"gcp_resources": "ask"})
	st := m.stream(m.dev, lookupReply("gcp_resources", map[string]string{}))
	m.dev.expect(200, "POST", "/v1/assistant/approvals/"+m.waitPending(t), map[string]any{"decision": "deny"})
	events, res := answerOf(t, st)
	if res == nil || !strings.Contains(res["content"].(string), "denied by dev") {
		t.Fatalf("result = %v", res)
	}
	for _, f := range events {
		if f["name"] == "forgeline.tool_status" && f["value"].(map[string]any)["ok"] != false {
			t.Errorf("a denied call is shown as ok: %v", f)
		}
	}
	if g.searchCount() != 0 {
		t.Error("a denied tool ran")
	}
	// No answer in time is a denial too.
	old := approvalTimeout
	approvalTimeout = 80 * time.Millisecond
	defer func() { approvalTimeout = old }()
	st = m.stream(m.dev, lookupReply("gcp_resources", map[string]string{}))
	_, res = answerOf(t, st)
	if res == nil || !strings.Contains(res["content"].(string), "in time") || g.searchCount() != 0 {
		t.Errorf("an unanswered question: %v, %d searches", res, g.searchCount())
	}
	if aud := string(m.admin.expect(200, "GET", "/v1/audit?action=policy.approval", nil)); !strings.Contains(aud, "denied by dev") || !strings.Contains(aud, "in time") {
		t.Errorf("refusals not audited: %s", aud)
	}
	// A stream that is closed while waiting leaves nothing waiting.
	m.srv.approvals.mu.Lock()
	n := len(m.srv.approvals.pending)
	m.srv.approvals.mu.Unlock()
	if n != 0 {
		t.Errorf("%d questions are still waiting", n)
	}
}

func TestAReadThatNeedsApprovalIsRefusedWhereNobodyIsPresent(t *testing.T) {
	m, g, _ := cloudsHarness(t)
	setPolicy(m, "gcp", map[string]string{"gcp_resources": "ask"})
	ctx := context.Background()
	// A plain chat request has no stream to ask on.
	m.ask(m.dev, lookupReply("gcp_resources", map[string]string{}))
	if res := m.llm.lastUser(1); !strings.Contains(res, "nobody is present") {
		t.Errorf("chat result: %s", res)
	}
	// An MCP client is not offered the tool, and cannot call it.
	tok := m.newToken(m.dev, map[string]any{"name": "d"})
	if m.toolNames(tok)["gcp_resources"] {
		t.Error("a tool that needs a person is offered over MCP")
	}
	if out, isErr := m.tool(tok, "gcp_resources", map[string]any{}); !isErr || !strings.Contains(out, "not available") {
		t.Errorf("mcp call = %q", out)
	}
	// An agent never borrows the person's presence, even when its context carries an approver.
	yes := approver(func(lookupCall) error { return nil })
	agentCtx := context.WithValue(context.WithValue(ctx, approverKey{}, yes), agentScopeKey{}, &agentScope{tools: map[string]bool{"gcp_resources": true}, label: "t@1"})
	if _, err := m.srv.runLookup(agentCtx, polDev, lookupCall{Tool: "gcp_resources", Args: json.RawMessage(`{}`)}); err == nil || !strings.Contains(err.Error(), "nobody is present") {
		t.Errorf("an agent ran a tool that needs a person: %v", err)
	}
	if g.searchCount() != 0 {
		t.Error("a refused tool reached gcloud")
	}
	// With a person present the model is shown the tool; without, it is not.
	withApprover := context.WithValue(ctx, approverKey{}, yes)
	if !strings.Contains(m.srv.lookupHelp(withApprover, authn.Developer), "- gcp_resources ") || strings.Contains(m.srv.lookupHelp(ctx, authn.Developer), "- gcp_resources ") {
		t.Error("the tool list does not follow who is present")
	}
	// The effective-access view says what happens to an agent.
	newAgent(t, m.admin, agentBody("Cloud reader", nil, []string{"gcp_resources"}, "T0"))
	v := policyOf(t, m.admin, "gcp")
	if len(v.Agents) != 1 || v.Agents[0].Tools[0].State != "needs_person" {
		t.Errorf("agents = %+v", v.Agents)
	}
	// Refusals are audited against the integration.
	if aud := string(m.admin.expect(200, "GET", "/v1/audit?action=policy.refused", nil)); !strings.Contains(aud, "nobody is present") {
		t.Errorf("not audited: %s", aud)
	}
}

func TestAChangeToolIsNeverStoredAsAsk(t *testing.T) {
	m, _ := policyHarness(t)
	m.admin.expect(200, "PUT", "/v1/integrations/aws/policy", map[string]any{"tools": map[string]string{toolProposeCloud: "ask", "aws_summary": "ask"}})
	v := policyOf(t, m.admin, "aws")
	if levelOf(v, toolProposeCloud) != polAllow || levelOf(v, "aws_summary") != polAsk {
		t.Errorf("levels = %+v", v.Tools)
	}
	// The change tool is still offered and still a proposal, whatever else needs approval.
	if err := m.srv.policyGate(context.Background(), polDev, lookupCall{Tool: toolProposeCloud}); err != nil {
		t.Errorf("propose_cloud_change was refused: %v", err)
	}
	if aud := string(m.admin.expect(200, "GET", "/v1/audit?action=policy.update", nil)); !strings.Contains(aud, "needs approval: [aws_summary]") {
		t.Errorf("the change is not audited: %s", aud)
	}
	// Nothing is stored for the change tool, and even a policy written by hand cannot make it wait for a person.
	stored, _ := m.srv.loadPolicy(context.Background())
	if _, has := stored.Tools[toolProposeCloud]; has || stored.Tools["aws_summary"] != polAsk {
		t.Errorf("stored = %v", stored.Tools)
	}
	stored.Tools[toolProposeCloud] = polAsk
	m.srv.docs.Put(context.Background(), kindSettings, idPolicy, stored)
	if err := m.srv.policyGate(context.Background(), polDev, lookupCall{Tool: toolProposeCloud}); err != nil {
		t.Errorf("a hand-written ask made the change tool wait for a person: %v", err)
	}
	if m.srv.blockedTools(context.Background())(toolProposeCloud) {
		t.Error("a hand-written ask hid the change tool")
	}
	var e struct{ Error string }
	json.Unmarshal(m.admin.expect(400, "PUT", "/v1/integrations/aws/policy", map[string]any{"tools": map[string]string{"aws_summary": "sometimes"}}), &e)
	if !strings.Contains(e.Error, "allow, ask or block") {
		t.Errorf("error = %q", e.Error)
	}
}

func TestOnlyOneAnswerReachesAWaitingCall(t *testing.T) {
	var b approvalBook
	p := &pendingApproval{who: "dev", answer: make(chan string, 1)}
	id := b.add(p)
	var wg sync.WaitGroup
	wins := make(chan bool, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); wins <- b.resolve(id, "dev", "approve") }()
	}
	wg.Wait()
	close(wins)
	n := 0
	for w := range wins {
		if w {
			n++
		}
	}
	if n != 1 || len(p.answer) != 1 {
		t.Errorf("%d answers got through, %d queued", n, len(p.answer))
	}
	if b.resolve("nope", "dev", "approve") || b.add(&pendingApproval{who: "x", answer: make(chan string, 1)}) == "" {
		t.Error("unknown ids")
	}
}
