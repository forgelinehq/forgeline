package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func (m *mcpHarness) waitMission(id string, until ...string) Mission {
	m.t.Helper()
	var ms Mission
	m.waitFor("mission "+id, func() bool {
		json.Unmarshal(m.dev.expect(200, "GET", "/v1/missions/"+id, nil), &ms)
		for _, s := range until {
			if ms.Status == s {
				return true
			}
		}
		return false
	})
	return ms
}

func findingsReply(text string, f ...map[string]string) string {
	b, _ := json.Marshal(f)
	return text + "\n<findings>" + string(b) + "</findings>"
}

func TestMissionDiscoversProposesWaitsAndVerifies(t *testing.T) {
	m := newMCPHarness(t)
	m.srv.run = (&multiStub{accounts: map[string]string{"dev": "111122223333"}}).run
	m.turnOn()
	a := newAgent(t, m.admin, agentBody("Fixer", nil, []string{"aws_summary", "propose_aws_change"}, "T1"))

	m.v.expect(403, "POST", "/v1/missions", map[string]any{"goal": "check the payments service", "agent": a.ID})
	m.dev.expect(400, "POST", "/v1/missions", map[string]any{"goal": "short", "agent": a.ID})
	m.dev.expect(400, "POST", "/v1/missions", map[string]any{"goal": "check the payments service", "agent": "nope"})
	m.dev.expect(400, "POST", "/v1/missions", map[string]any{"goal": "check the payments service", "agent": a.ID, "account": "nowhere"})

	m.llm.script(
		// discover: one lookup, then findings; one claims a source it never ran
		lookupReply("aws_summary", map[string]any{}),
		findingsReply("The web service runs one task.",
			map[string]string{"title": "web runs a single task", "detail": "desired count 1", "severity": "medium", "source": "aws_summary"},
			map[string]string{"title": "made-up finding", "detail": "never looked", "severity": "high", "source": "guard_findings"}),
		// propose: files one proposal, then the plan (it also claims a proposal it never filed)
		lookupReply("propose_aws_change", map[string]any{"title": "Scale web service", "reason": "one task is a single point of failure", "code": changeScript}),
		`Filed one proposal. <plan>[{"finding": 0, "proposal": "PLACEHOLDER", "recommendation": "run two tasks"}, {"finding": 0, "proposal": "not-mine", "recommendation": "x"}, {"finding": 9, "recommendation": "out of range"}]</plan>`,
	)
	var ms Mission
	json.Unmarshal(m.dev.expect(202, "POST", "/v1/missions", map[string]any{"goal": "Check the payments web service and fix what is fragile", "agent": a.ID}), &ms)
	ms = m.waitMission(ms.ID, msWaiting, msFailed, msDone)
	if ms.Status != msWaiting || ms.Stage != msApproval || len(ms.Findings) != 1 || ms.Unsupported != 1 || len(ms.Proposals) != 1 {
		t.Fatalf("after propose: %+v", ms)
	}
	if ms.Findings[0].Source != "aws_summary" || ms.Findings[0].Severity != "medium" {
		t.Errorf("finding = %+v", ms.Findings[0])
	}
	// Plan items keep only proposals the mission really filed, and drop findings that do not exist.
	if len(ms.Plan) != 2 || ms.Plan[0].Proposal != "" || ms.Plan[1].Proposal != "" {
		t.Errorf("plan = %+v", ms.Plan)
	}
	if len(ms.Steps) != 2 || ms.Steps[0].Stage != msDiscover || strings.Join(ms.Steps[0].Lookups, ",") != "aws_summary" || strings.Contains(ms.Steps[0].Output, "<findings>") {
		t.Errorf("steps = %+v", ms.Steps)
	}
	if ms.Used.Calls != 4 || ms.Used.tokens() == 0 {
		t.Errorf("mission use = %+v", ms.Used)
	}

	// The proposal is tied to the mission and waits for a different administrator.
	p := m.waitProposal(ms.Proposals[0])
	if p.Mission != ms.ID || p.Status != apProposed || p.ProposedBy != "dev" {
		t.Fatalf("proposal = %+v", p)
	}
	m.srv.missionTick(context.Background(), time.Now())
	if got := m.waitMission(ms.ID, msWaiting); got.Stage != msApproval {
		t.Errorf("moved on before the proposal was decided: %+v", got)
	}

	// Approved and applied; the tick moves it to verify, which needs a lookup for each result it claims.
	m.llm.script(
		lookupReply("aws_summary", map[string]any{}),
		`Checked again. <evidence>[{"finding": 0, "result": "fixed", "detail": "two tasks now", "source": "aws_summary"}, {"finding": 0, "result": "fixed", "detail": "also", "source": "guard_findings"}]</evidence>`,
	)
	m.admin2.expect(202, "POST", "/v1/ai/proposals/"+p.ID+"/approve", map[string]any{"hash": p.Hash})
	if done := m.waitProposal(p.ID, apProposed, apApplying); done.Status != apApplied {
		t.Fatalf("apply: %+v", done)
	}
	m.srv.missionTick(context.Background(), time.Now())
	ms = m.waitMission(ms.ID, msDone, msFailed)
	if ms.Status != msDone || len(ms.Evidence) != 2 || ms.Evidence[0].Result != "fixed" || ms.Evidence[1].Result != "unknown" || ms.Unsupported != 2 {
		t.Fatalf("verify: %+v", ms)
	}
	for _, want := range []string{"Findings: 1 (0 high, 1 medium", "2 claims were left out", "Change proposals filed: 1", "1 fixed, 0 still there"} {
		if !strings.Contains(ms.Report, want) {
			t.Errorf("report misses %q: %s", want, ms.Report)
		}
	}
	audit := string(m.admin.expect(200, "GET", "/v1/audit?action=mission", nil))
	for _, want := range []string{"mission.start", "mission.stage", "mission.done"} {
		if !strings.Contains(audit, want) {
			t.Errorf("audit misses %s", want)
		}
	}
}

func TestMissionScopeBudgetAndCancel(t *testing.T) {
	m := newMCPHarness(t)
	m.srv.run = (&multiStub{accounts: map[string]string{"dev": "111122223333"}}).run
	m.turnOn()
	a := newAgent(t, m.admin, agentBody("Reader", nil, []string{"aws_summary", "aws_resources"}, "T0"))

	// A lookup for another account is refused on the server, whatever the agent asks for. A finding resting
	// on a refused lookup still names a lookup that ran, so the refusal is what the agent saw.
	m.llm.script(
		lookupReply("aws_summary", map[string]any{"account": "staging"}),
		findingsReply("Nothing.", nil...),
	)
	var ms Mission
	json.Unmarshal(m.dev.expect(202, "POST", "/v1/missions", map[string]any{"goal": "Look at the primary account only", "agent": a.ID}), &ms)
	ms = m.waitMission(ms.ID, msDone, msFailed)
	if got := m.llm.lastUser(1); !strings.Contains(got, `may only look at the AWS account "primary"`) {
		t.Errorf("an out-of-scope lookup ran: %s", got)
	}
	if ms.Status != msDone || len(ms.Findings) != 0 || !strings.Contains(ms.Report, "found nothing") {
		t.Errorf("mission = %+v", ms)
	}
	// An advisory agent has no proposal stage to file in: findings go straight to recommendations and report.
	m.llm.script(
		lookupReply("aws_resources", map[string]any{}),
		findingsReply("One finding.", map[string]string{"title": "a public bucket", "severity": "high", "source": "aws_resources"}),
		`<plan>[{"finding": 0, "recommendation": "block public access"}]</plan>`,
	)
	json.Unmarshal(m.dev.expect(202, "POST", "/v1/missions", map[string]any{"goal": "Find public buckets please", "agent": a.ID}), &ms)
	ms = m.waitMission(ms.ID, msDone, msFailed)
	if ms.Status != msDone || ms.MayPropose || len(ms.Plan) != 1 || len(ms.Proposals) != 0 || !strings.Contains(m.llm.lastUser(2), "You may not change anything") {
		t.Errorf("advisory mission = %+v", ms)
	}

	// The token budget stops a mission before its next call (each scripted call reports 1500 tokens).
	m.llm.script(lookupReply("aws_summary", map[string]any{}), "never reached")
	json.Unmarshal(m.dev.expect(202, "POST", "/v1/missions", map[string]any{"goal": "Look at everything", "agent": a.ID, "budget_tokens": 1000}), &ms)
	ms = m.waitMission(ms.ID, msStopped, msFailed, msDone)
	if ms.Status != msStopped || !strings.Contains(ms.Error, "token budget") || len(m.llm.reqs) != 1 || !strings.Contains(ms.Report, "stopped early") {
		t.Errorf("budget: %+v (%d calls)", ms, len(m.llm.reqs))
	}
	m.dev.expect(400, "POST", "/v1/missions", map[string]any{"goal": "Look at everything", "agent": a.ID, "budget_tokens": 9_000_000})

	// A mission whose agent does not answer in the agreed form gets one retry, then fails.
	m.llm.script("no findings block", "still none")
	json.Unmarshal(m.dev.expect(202, "POST", "/v1/missions", map[string]any{"goal": "Look at everything", "agent": a.ID}), &ms)
	ms = m.waitMission(ms.ID, msFailed, msDone)
	if ms.Status != msFailed || !strings.Contains(ms.Error, "agreed form") || !strings.Contains(m.llm.lastUser(1), "no <findings> block") {
		t.Errorf("form: %+v", ms)
	}

	// Cancel: the starter or an administrator; a cancelled mission is never overwritten.
	m.admin.createUser("ben", "developer")
	ben := m.login("ben")
	m.srv.docs.Put(context.Background(), kindMission, "ms-wait", Mission{ID: "ms-wait", Goal: "waiting one", By: "dev", Status: msWaiting, Stage: msApproval, StartedAt: time.Now()})
	ben.expect(403, "POST", "/v1/missions/ms-wait/cancel", map[string]any{})
	m.dev.expect(200, "POST", "/v1/missions/ms-wait/cancel", map[string]any{})
	m.dev.expect(409, "POST", "/v1/missions/ms-wait/cancel", map[string]any{})
	got, _ := m.srv.loadMission(context.Background(), "ms-wait")
	if m.srv.saveMission(Mission{ID: "ms-wait", Status: msRunning}) || got.Status != msCancelled {
		t.Errorf("a cancelled mission was overwritten: %+v", got)
	}
	// A restart fails a mission that was in the middle of a stage, and leaves a waiting one waiting.
	m.srv.docs.Put(context.Background(), kindMission, "ms-mid", Mission{ID: "ms-mid", Goal: "mid", By: "dev", Status: msRunning, Stage: msDiscover, StartedAt: time.Now()})
	m.srv.docs.Put(context.Background(), kindMission, "ms-w2", Mission{ID: "ms-w2", Goal: "w", By: "dev", Status: msWaiting, Stage: msApproval, StartedAt: time.Now(), Proposals: []string{"p-missing-but-pending"}})
	ctx, cancel := context.WithCancel(context.Background())
	m.srv.startMissions(ctx)
	cancel()
	mid, _ := m.srv.loadMission(context.Background(), "ms-mid")
	if mid.Status != msFailed || !strings.Contains(mid.Error, "restart") {
		t.Errorf("restart: %+v", mid)
	}
	// Waiting more than a week: it reports with what was decided.
	m.srv.docs.Put(context.Background(), kindAIProposal, "p-pending", AIProposal{ID: "p-pending", Status: apProposed})
	young := Mission{ID: "ms-young", Goal: "young", By: "dev", Status: msWaiting, Stage: msApproval, StartedAt: time.Now(), Proposals: []string{"p-pending"}}
	old := Mission{ID: "ms-old", Goal: "old", By: "dev", Status: msWaiting, Stage: msApproval, StartedAt: time.Now().Add(-8 * 24 * time.Hour), Proposals: []string{"p-pending"}}
	m.srv.docs.Put(context.Background(), kindMission, young.ID, young)
	m.srv.docs.Put(context.Background(), kindMission, old.ID, old)
	m.srv.missionTick(context.Background(), time.Now())
	if got, _ := m.srv.loadMission(context.Background(), "ms-young"); got.Status != msWaiting {
		t.Errorf("a mission with an undecided proposal moved on: %+v", got)
	}
	if got := m.waitMission("ms-old", msDone); got.Stage != msReport {
		t.Errorf("old: %+v", got)
	}
	_ = fmt.Sprint()
}

func TestMissionGate(t *testing.T) {
	in := func(acct string) context.Context {
		return context.WithValue(context.Background(), missionKey{}, &missionRun{id: "m", account: acct})
	}
	call := func(tool, args string) lookupCall { return lookupCall{Tool: tool, Args: json.RawMessage(args)} }
	cases := []struct {
		ctx  context.Context
		call lookupCall
		ok   bool
	}{
		{context.Background(), call("aws_summary", `{"account":"staging"}`), true}, // no mission, no gate
		{in("primary"), call("aws_summary", `{}`), true},
		{in("primary"), call("aws_summary", `{"account":"staging"}`), false},
		{in("staging"), call("aws_summary", `{}`), false}, // no account means the primary, which is out of scope
		{in("staging"), call("call_aws", `{"cli_command":"aws s3 ls","account":"staging"}`), true},
		{in("staging"), call("propose_aws_change", `{"title":"x"}`), false},
		{in("staging"), call("list_runs", `{}`), true}, // not an account tool
	}
	for i, c := range cases {
		if err := missionGate(c.ctx, c.call); (err == nil) != c.ok {
			t.Errorf("case %d (%s %s): err = %v", i, c.call.Tool, c.call.Args, err)
		}
	}
}
