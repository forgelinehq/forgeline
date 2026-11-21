package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
)

func TestPermissionRulesDecide(t *testing.T) {
	me, other := Principal{Name: "ada", UserID: "u1", Role: authn.Admin}, Principal{Name: "bob", UserID: "u2", Role: authn.Admin}
	rule := func(scope, owner, effect, pattern string) PermRule {
		return PermRule{ID: pattern + effect, Scope: scope, Owner: owner, Tool: permToolAWS, Pattern: pattern, Effect: effect, By: "x"}
	}
	cmd := "aws ecs update-service --cluster web --desired-count 2"
	for _, c := range []struct {
		name  string
		rules []PermRule
		who   Principal
		cmd   string
		want  string
	}{
		{"nothing says ask", nil, me, cmd, permAsk},
		{"my allow", []PermRule{rule(scopePerson, "u1", permAllow, "aws ecs update-service *")}, me, cmd, permAllow},
		{"another person's allow is not mine", []PermRule{rule(scopePerson, "u2", permAllow, "aws ecs update-service *")}, me, cmd, permAsk},
		{"an organisation allow", []PermRule{rule(scopeOrg, "", permAllow, "aws ecs update-service *")}, me, cmd, permAllow},
		{"a deny beats an allow", []PermRule{rule(scopePerson, "u1", permAllow, "aws ecs update-service *"), rule(scopeOrg, "", permDeny, "aws ecs *")}, me, cmd, permDeny},
		{"my deny beats an organisation allow", []PermRule{rule(scopeOrg, "", permAllow, "aws ecs update-service *"), rule(scopePerson, "u1", permDeny, "aws ecs update-service *")}, me, cmd, permDeny},
		{"an organisation ask cannot be skipped by my allow", []PermRule{rule(scopePerson, "u1", permAllow, "aws ecs update-service *"), rule(scopeOrg, "", permAsk, "aws ecs *")}, me, cmd, permAsk},
		{"the pattern is anchored", []PermRule{rule(scopePerson, "u1", permAllow, "aws ecs update-service")}, me, cmd, permAsk},
		{"a pattern for another operation", []PermRule{rule(scopePerson, "u1", permAllow, "aws ecs delete-service *")}, me, cmd, permAsk},
		{"regular expression characters are plain", []PermRule{rule(scopePerson, "u1", permAllow, "aws ecs .*-service *")}, me, cmd, permAsk},
		{"an allow never covers iam", []PermRule{rule(scopePerson, "u1", permAllow, "aws iam delete-user *")}, me, "aws iam delete-user --user-name x", permAsk},
		{"an allow never covers ssm", []PermRule{rule(scopeOrg, "", permAllow, "aws ssm send-command *")}, me, "aws ssm send-command --instance-ids i-1", permAsk},
		{"a deny still covers iam", []PermRule{rule(scopeOrg, "", permDeny, "aws iam *")}, me, "aws iam delete-user --user-name x", permDeny},
		{"another tool's rule", []PermRule{{Scope: scopeOrg, Tool: "other", Pattern: "aws *", Effect: permDeny}}, me, cmd, permAsk},
	} {
		if got := (permissionsDoc{Rules: c.rules}).decide(c.who, permToolAWS, c.cmd); got.Effect != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, got.Effect, got.Why, c.want)
		}
	}
	_ = other
}

func TestRuleProblems(t *testing.T) {
	for _, c := range []struct {
		r    PermRule
		want string
	}{
		{PermRule{Tool: permToolAWS, Effect: permAllow, Pattern: "aws *"}, "names the service"},
		{PermRule{Tool: permToolAWS, Effect: permAllow, Pattern: "aws * update-service *"}, "names the service"},
		{PermRule{Tool: permToolAWS, Effect: permAllow, Pattern: "aws ecs * *"}, "names the service"},
		{PermRule{Tool: permToolAWS, Effect: permAllow, Pattern: "aws iam delete-user *"}, "always asked"},
		{PermRule{Tool: permToolAWS, Effect: permAllow, Pattern: "aws ssm send-command *"}, "always asked"},
		{PermRule{Tool: permToolAWS, Effect: "maybe", Pattern: "aws ecs update-service *"}, "effect"},
		{PermRule{Tool: permToolAWS, Effect: permAllow, Pattern: "ecs update-service *"}, "starts with"},
		{PermRule{Tool: permToolAWS, Effect: permAllow, Pattern: "aws ecs update-service *\naws x y"}, "one line"},
		{PermRule{Tool: "bash", Effect: permDeny, Pattern: "aws *"}, "only call_aws"},
	} {
		if got := ruleProblem(c.r); !strings.Contains(got, c.want) {
			t.Errorf("%+v: %q, want %q", c.r, got, c.want)
		}
	}
	for _, ok := range []PermRule{
		{Tool: permToolAWS, Effect: permDeny, Pattern: "aws *"},
		{Tool: permToolAWS, Effect: permAsk, Pattern: "aws * delete-*"},
		{Tool: permToolAWS, Effect: permAllow, Pattern: "aws ecs update-service *"},
	} {
		if got := ruleProblem(ok); got != "" {
			t.Errorf("%+v refused: %s", ok, got)
		}
	}
	if alwaysPattern("aws ecs update-service --cluster x") != "aws ecs update-service *" || alwaysPattern("aws iam delete-user --user-name x") != "" || alwaysPattern("aws ecs") != "" {
		t.Error("alwaysPattern")
	}
}

// The whole flow in the chat: a read runs, a change to a protected account is refused with the way forward, and on an
// account marked for it the person is shown the exact command and answers once, always, or no.
func TestAChangeAsksThePersonAndRunsOnlyAfterTheyApprove(t *testing.T) {
	m, st := runbookHarness(t)
	change := lookupReply("call_aws", map[string]any{"cli_command": "aws ecs update-service --cluster web --service api --desired-count 3"})

	// Protected by default: nothing runs, and the reason names the way forward.
	m.ask(m.admin, change)
	if res := m.llm.lastUser(1); !strings.Contains(res, "this account is protected") || !strings.Contains(res, "propose_aws_change") {
		t.Fatalf("protected: %s", res)
	}
	if st.count("ecs update-service") != 0 {
		t.Fatal("a change to a protected account ran")
	}
	m.admin.expect(200, "PUT", "/v1/assistant/permissions/accounts/primary", map[string]any{"own_approval": true})
	m.dev.expect(403, "PUT", "/v1/assistant/permissions/accounts/primary", map[string]any{"own_approval": false})

	// A developer cannot change AWS this way, and an agent cannot.
	m.ask(m.dev, change)
	if res := m.llm.lastUser(1); !strings.Contains(res, "needs an administrator") || st.count("ecs update-service") != 0 {
		t.Errorf("developer: %s", res)
	}

	// An administrator in a plain request has nobody to ask.
	m.ask(m.admin, change)
	if res := m.llm.lastUser(1); !strings.Contains(res, "nobody is here to give it") || st.count("ecs update-service") != 0 {
		t.Errorf("no person present: %s", res)
	}

	// In the chat the exact command is shown, and nothing runs until the answer.
	s1 := m.streamScript(m.admin, change, "Done.")
	id := m.waitPending(t)
	time.Sleep(50 * time.Millisecond)
	if st.count("ecs update-service") != 0 {
		t.Fatal("the change ran before the person answered")
	}
	m.dev.expect(404, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "approve"}) // not theirs
	m.admin.expect(400, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "yes"})
	m.admin.expect(404, "POST", "/v1/assistant/questions/"+id, map[string]any{"answer": "yes"}) // a permission is not a question
	m.admin.expect(200, "POST", "/v1/assistant/approvals/"+id, map[string]any{"decision": "approve"})
	events, res := answerOf(t, s1)
	card := eventsNamed(events, "forgeline.permission")
	if len(card) != 1 || !strings.Contains(card[0]["command"].(string), "aws ecs update-service --cluster web --service api --desired-count 3") || card[0]["pattern"] != "aws ecs update-service *" || card[0]["account"] != "primary" {
		t.Errorf("card: %v", card)
	}
	if st.count("ecs update-service") != 1 || res == nil || strings.Contains(res["content"].(string), "lookup failed") {
		t.Errorf("approved once: %d runs, %v", st.count("ecs update-service"), res)
	}
	// Once is once: asking again asks again.
	s2 := m.streamScript(m.admin, change, "Done.")
	id2 := m.waitPending(t)
	if id2 == id {
		t.Error("an id was reused")
	}
	m.admin.expect(200, "POST", "/v1/assistant/approvals/"+id2, map[string]any{"decision": "deny"})
	if _, res := answerOf(t, s2); res == nil || !strings.Contains(res["content"].(string), "you said no") || st.count("ecs update-service") != 1 {
		t.Errorf("denied: %v, %d runs", res, st.count("ecs update-service"))
	}
	// Always: the next time it runs without asking, and the rule is the person's own, visible and removable.
	s3 := m.streamScript(m.admin, change, "Done.")
	id3 := m.waitPending(t)
	m.admin.expect(200, "POST", "/v1/assistant/approvals/"+id3, map[string]any{"decision": "always"})
	answerOf(t, s3)
	var pm struct{ Rules []PermRule }
	json.Unmarshal(m.admin.expect(200, "GET", "/v1/assistant/permissions", nil), &pm)
	if len(pm.Rules) != 1 || pm.Rules[0].Pattern != "aws ecs update-service *" || pm.Rules[0].Effect != permAllow || pm.Rules[0].Scope != scopePerson {
		t.Fatalf("rules: %+v", pm.Rules)
	}
	m.ask(m.admin, change)
	if st.count("ecs update-service") != 3 {
		t.Errorf("an always-allowed change did not run without asking: %d runs", st.count("ecs update-service"))
	}
	// The other administrator does not have that rule.
	var gm struct{ Rules []PermRule }
	json.Unmarshal(m.admin2.expect(200, "GET", "/v1/assistant/permissions", nil), &gm)
	if len(gm.Rules) != 0 {
		t.Errorf("another person's rule is visible: %+v", gm.Rules)
	}
	m.admin2.expect(404, "DELETE", "/v1/assistant/permissions/rules/"+pm.Rules[0].ID, nil)
	// An organisation deny stops it for everyone, even with the person's allow.
	m.admin2.expect(201, "POST", "/v1/assistant/permissions/rules", map[string]any{"scope": "org", "effect": "deny", "pattern": "aws ecs update-service *"})
	m.ask(m.admin, change)
	if res := m.llm.lastUser(1); !strings.Contains(res, "a org rule denies") || st.count("ecs update-service") != 3 {
		t.Errorf("org deny: %s", res)
	}
	// The audit log has every decision.
	aud := string(m.admin.expect(200, "GET", "/v1/audit?limit=60", nil))
	for _, want := range []string{"assistant.cli.change", "approved once", "approved, and always allowed", "you said no", "allowed by rule", "this account is protected"} {
		if !strings.Contains(aud, want) {
			t.Errorf("audit lacks %q", want)
		}
	}
}

// A change cannot get around the service switches, the secret rules or the shape rules.
func TestAChangeStillMeetsTheServiceSwitchesAndTheCommandRules(t *testing.T) {
	m, st := runbookHarness(t)
	m.admin.expect(200, "PUT", "/v1/assistant/permissions/accounts/primary", map[string]any{"own_approval": true})
	for _, c := range []struct{ cmd, want string }{
		{"aws iam create-access-key --user-name x", "returns a secret or a credential"},
		{"aws iam create-login-profile --user-name x --password y", "returns a secret or a credential"},
		{"aws ecs update-service --cluster x; reboot", "no pipes, redirects"},
		{"aws ecs update-service --cluster x --profile other", "--profile is not allowed"},
		{"aws s3 rm s3://bucket --recursive", "s3 service is not available"},
		{"aws ecs update-service --cli-input-json file://x.json", "--cli-input-json is not allowed"},
	} {
		m.ask(m.admin, lookupReply("call_aws", map[string]any{"cli_command": c.cmd}))
		if res := m.llm.lastUser(1); !strings.Contains(res, c.want) || st.count("update-service") != 0 || st.count("create-access-key") != 0 || st.count("create-login-profile") != 0 {
			t.Errorf("%q: want %q in %s", c.cmd, c.want, res)
		}
	}
	// Look only for the service: no change, even with the person's approval.
	m.admin.expect(200, "PUT", "/v1/integrations/aws/services", map[string]any{"read_only": []string{"ecs"}})
	s1 := m.streamScript(m.admin, lookupReply("call_aws", map[string]any{"cli_command": "aws ecs update-service --cluster web --service api --desired-count 3"}), "ok")
	if _, res := answerOf(t, s1); res == nil || !strings.Contains(res["content"].(string), "Look only") || st.count("update-service") != 0 {
		t.Errorf("look only: %v", res)
	}
	// An eval records a change and never runs it.
	if !isCLIChange(lookupCall{Tool: "call_aws", Args: json.RawMessage(`{"cli_command":"aws ec2 stop-instances --instance-ids i-1"}`)}) || isCLIChange(lookupCall{Tool: "call_aws", Args: json.RawMessage(`{"cli_command":"aws ec2 describe-instances"}`)}) {
		t.Error("isCLIChange")
	}
}

func TestPermissionRulesAPI(t *testing.T) {
	m, _ := runbookHarness(t)
	body := func(m map[string]any) map[string]any { return m }
	m.dev.expect(403, "POST", "/v1/assistant/permissions/rules", body(map[string]any{"scope": "org", "effect": "deny", "pattern": "aws *"}))
	m.v.expect(403, "GET", "/v1/assistant/permissions", nil)
	m.dev.expect(400, "POST", "/v1/assistant/permissions/rules", body(map[string]any{"effect": "allow", "pattern": "aws *"}))
	m.dev.expect(400, "POST", "/v1/assistant/permissions/rules", body(map[string]any{"scope": "everyone", "effect": "deny", "pattern": "aws *"}))
	var r PermRule
	json.Unmarshal(m.dev.expect(201, "POST", "/v1/assistant/permissions/rules", body(map[string]any{"effect": "ask", "pattern": "aws ec2 terminate-instances *"})), &r)
	if r.Scope != scopePerson || r.Owner == "" {
		t.Errorf("rule: %+v", r)
	}
	m.admin.expect(404, "DELETE", "/v1/assistant/permissions/rules/"+r.ID, nil) // not an organisation rule, and not theirs
	m.dev.expect(200, "DELETE", "/v1/assistant/permissions/rules/"+r.ID, nil)
	m.dev.expect(404, "DELETE", "/v1/assistant/permissions/rules/"+r.ID, nil)
	m.admin.expect(404, "PUT", "/v1/assistant/permissions/accounts/nope", map[string]any{"own_approval": true})
	m.admin.expect(400, "PUT", "/v1/assistant/permissions/accounts/primary", map[string]any{})
	for i := 0; i < maxPermRules; i++ {
		m.dev.expect(201, "POST", "/v1/assistant/permissions/rules", body(map[string]any{"effect": "ask", "pattern": "aws ec2 terminate-instances *"}))
	}
	m.dev.expect(409, "POST", "/v1/assistant/permissions/rules", body(map[string]any{"effect": "ask", "pattern": "aws ec2 terminate-instances *"}))
}

// An agent never changes anything through call_aws, whoever started it; and an eval records the attempt without
// running it or refusing it.
func TestAnAgentCannotChangeAWSAndAnEvalRecordsTheAttempt(t *testing.T) {
	m, st := runbookHarness(t)
	m.admin.expect(200, "PUT", "/v1/assistant/permissions/accounts/primary", map[string]any{"own_approval": true})
	m.admin.expect(201, "POST", "/v1/assistant/permissions/rules", map[string]any{"scope": "org", "effect": "allow", "pattern": "aws ec2 stop-instances *"})
	a := newAgent(t, m.admin, agentBody("Changer", nil, []string{"call_aws"}, "T1"))
	stop := lookupReply("call_aws", map[string]any{"cli_command": "aws ec2 stop-instances --instance-ids i-0aaaaaaaaaaaaaaaa"})
	m.llm.script(stop, "done")
	m.admin.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "stop it"})
	if res := m.llm.lastUser(1); !strings.Contains(res, "an agent cannot change anything this way") || st.count("stop-instances") != 0 {
		t.Errorf("agent: %s", res)
	}
	var x EvalSuite
	json.Unmarshal(m.admin.expect(201, "POST", "/v1/ai/evals", map[string]any{"agent": a.ID, "name": "No changes", "attempts": 1, "cases": []map[string]any{
		{"name": "Stops it", "input": "Stop the box.", "checks": []map[string]any{{"kind": "tool_not_called", "value": "call_aws"}}}}}), &x)
	m.llm.script(stop, "I stopped it.")
	var run EvalRun
	json.Unmarshal(m.admin.expect(202, "POST", "/v1/ai/evals/"+x.ID+"/run", map[string]any{}), &run)
	run = waitEvalRun(t, m.admin, run.ID)
	at := run.Results[0].Attempts[0]
	if strings.Join(at.Held, ",") != "call_aws" || at.Pass || st.count("stop-instances") != 0 {
		t.Errorf("eval attempt: %+v, %d runs", at, st.count("stop-instances"))
	}
	if !strings.Contains(m.llm.lastUser(1), "Recorded by an eval run") {
		t.Errorf("the model was not told it was recorded: %s", m.llm.lastUser(1))
	}
}

// An "always" answer is only for a permission that offered it, and a read's approval cannot be answered "always".
func TestAlwaysIsOnlyForAPermissionThatOfferedIt(t *testing.T) {
	var b approvalBook
	read := &pendingApproval{who: "dev", answer: make(chan string, 1)}
	perm := &pendingApproval{who: "dev", kind: "permission", always: true, answer: make(chan string, 1)}
	noAlways := &pendingApproval{who: "dev", kind: "permission", answer: make(chan string, 1)}
	question := &pendingApproval{who: "dev", kind: "question", answer: make(chan string, 1)}
	rid, pid, nid, qid := b.add(read), b.add(perm), b.add(noAlways), b.add(question)
	if b.resolveApproval(rid, "dev", decisionAlways) || b.resolveApproval(nid, "dev", decisionAlways) {
		t.Error("always was accepted where it was not offered")
	}
	if b.resolveApproval(qid, "dev", "approve") {
		t.Error("a question was answered as an approval")
	}
	if b.resolveApproval(pid, "other", decisionAlways) {
		t.Error("someone else answered")
	}
	if !b.resolveApproval(pid, "dev", decisionAlways) || <-perm.answer != decisionAlways {
		t.Error("always was refused where it was offered")
	}
	if !b.resolveApproval(rid, "dev", "approve") || !b.resolveApproval(nid, "dev", "deny") {
		t.Error("plain answers were refused")
	}
}
