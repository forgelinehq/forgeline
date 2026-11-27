package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/executor"
	"github.com/forgelinehq/forgeline/internal/schedule"
)

// nineInBerlin is the first 09:00 in Berlin after now.
func nineInBerlin(t *testing.T) time.Time {
	t.Helper()
	c, _ := schedule.Parse("0 9 * * *")
	loc, _ := time.LoadLocation("Europe/Berlin")
	return c.Next(time.Now(), loc)
}

// A workflow on a cron schedule runs at its time (or up to an hour late), once; paused, it does not; resumed, it waits
// for its next time.
func TestWorkflowCronScheduleAndPause(t *testing.T) {
	m := newMCPHarness(t)
	a := newAgent(t, m.admin, agentBody("Worker", nil, nil, "T0"))
	body := wfBody("Morning", []WFStage{{Steps: []WFStep{step(a.ID, "check")}}})
	for cron, want := range map[string]string{"*/5 * * * *": "5 minutes apart", "0 9 * *": "5 fields"} {
		body["cron"], body["tz"] = cron, "Europe/Berlin"
		if out := string(m.admin.expect(400, "POST", "/v1/ai/workflows", body)); !strings.Contains(out, want) {
			t.Errorf("%s: %s", cron, out)
		}
	}
	body["cron"], body["tz"] = "0 9 * * *", "Mars/Olympus"
	m.admin.expect(400, "POST", "/v1/ai/workflows", body)
	body["tz"] = "Europe/Berlin"
	w := newWorkflow(t, m.admin, body)
	if v := w.Versions[0]; v.Cron != "0 9 * * *" || v.TZ != "Europe/Berlin" {
		t.Fatalf("version %+v", v)
	}
	m.llm.script("ok")
	ctx := context.Background()
	runs := func() []WFRun {
		var out struct{ Runs []WFRun }
		json.Unmarshal(m.dev.expect(200, "GET", "/v1/ai/workflow-runs?workflow="+w.ID, nil), &out)
		return out.Runs
	}
	nine := nineInBerlin(t)
	m.srv.scheduleTick(ctx, nine.Add(-10*time.Minute))
	if len(runs()) != 0 {
		t.Fatal("ran before its time")
	}
	m.srv.scheduleTick(ctx, nine.Add(5*time.Minute))
	if rs := runs(); len(rs) != 1 || rs[0].Trigger != "schedule" {
		t.Fatalf("at its time: %+v", rs)
	}
	m.waitRun(runs()[0].ID, wfSucceeded)
	m.srv.scheduleTick(ctx, nine.Add(20*time.Minute))
	if len(runs()) != 1 {
		t.Error("ran twice for one time")
	}

	// The list says when it runs next; a developer can read it but not pause it.
	var list struct{ Schedules []scheduleItem }
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/ai/schedules", nil), &list)
	if len(list.Schedules) != 1 || list.Schedules[0].Kind != "workflow" || list.Schedules[0].Describe != "cron 0 9 * * * (Europe/Berlin)" || len(list.Schedules[0].Next) != 3 {
		t.Errorf("list %+v", list.Schedules)
	}
	m.dev.expect(403, "POST", "/v1/ai/workflows/"+w.ID+"/schedule/pause", nil)
	m.admin.expect(204, "POST", "/v1/ai/workflows/"+w.ID+"/schedule/pause", nil)
	m.srv.scheduleTick(ctx, nine.Add(24*time.Hour+5*time.Minute))
	if len(runs()) != 1 {
		t.Error("a paused schedule ran")
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/ai/schedules", nil), &list)
	if !list.Schedules[0].Paused || len(list.Schedules[0].Next) != 0 {
		t.Errorf("paused in the list: %+v", list.Schedules[0])
	}
	m.admin.expect(204, "POST", "/v1/ai/workflows/"+w.ID+"/schedule/resume", nil)
	m.srv.scheduleTick(ctx, nine.Add(48*time.Hour+5*time.Minute))
	if len(runs()) != 2 {
		t.Errorf("resumed, it did not run at its next time: %d runs", len(runs()))
	}
	if au := string(m.admin.expect(200, "GET", "/v1/audit?action=ai.schedule.pause", nil)); !strings.Contains(au, "workflow/"+w.ID) {
		t.Errorf("audit: %s", au)
	}

	// The preview says what a schedule means, or what is wrong with it.
	var pv struct {
		Describe string
		Next     []time.Time
	}
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/ai/schedules/preview", map[string]any{"cron": "0 9 * * 1-5", "tz": "Asia/Kolkata"}), &pv)
	if len(pv.Next) != 5 || pv.Next[0].In(time.FixedZone("IST", 19800)).Hour() != 9 {
		t.Errorf("preview %+v", pv)
	}
	m.dev.expect(400, "POST", "/v1/ai/schedules/preview", map[string]any{"cron": "* * * * *"})
}

// A team schedule gives the team its goal at its times, as the role an administrator chose, never as an administrator.
func TestTeamSchedule(t *testing.T) {
	h := newHarness(t, &executor.Fake{})
	admin := h.adminBrowser("ada")
	model := &teamModel{}
	srv := model.server(t)
	admin.expect(201, "POST", "/v1/providers", anthropicProvider("anthropic", srv.URL, "claude-haiku-5-5"))
	mk := func(name string) AIAgent {
		b := agentBody(name, nil, nil, "T0")
		b["model"] = "anthropic/claude-haiku-5-5"
		return newAgent(t, admin, b)
	}
	lead, member := mk("Lead"), mk("Reader")
	var tm Team
	json.Unmarshal(admin.expect(201, "POST", "/v1/ai/teams", map[string]any{"name": "Night watch", "purpose": "p", "lead": lead.ID, "members": []string{member.ID}}), &tm)
	model.plan = `{"note":"n","tasks":[{"member":"` + member.ID + `","task":"Look at last night's runs"}]}`

	good := map[string]any{"goal": "Check last night's failed runs", "cron": "0 9 * * *", "tz": "Europe/Berlin", "run_as": "viewer"}
	for name, change := range map[string]func(map[string]any){
		"as admin": func(b map[string]any) { b["run_as"] = "admin" },
		"no goal":  func(b map[string]any) { b["goal"] = " " },
		"no time":  func(b map[string]any) { delete(b, "cron"); delete(b, "tz") },
		"too often": func(b map[string]any) {
			b["cron"] = "*/10 * * * *"
		},
	} {
		b := map[string]any{}
		for k, v := range good {
			b[k] = v
		}
		change(b)
		if resp, out := admin.do("POST", "/v1/ai/teams/"+tm.ID+"/schedules", b); resp.StatusCode != 400 {
			t.Errorf("%s: %d %s", name, resp.StatusCode, out)
		}
	}
	admin.createUser("dev", authn.Developer)
	dev := h.login("dev")
	dev.expect(403, "POST", "/v1/ai/teams/"+tm.ID+"/schedules", good)
	var ts TeamSchedule
	json.Unmarshal(admin.expect(201, "POST", "/v1/ai/teams/"+tm.ID+"/schedules", good), &ts)

	ctx := context.Background()
	nine := nineInBerlin(t)
	teamRuns := func() []TeamRun {
		var out struct{ Runs []TeamRun }
		json.Unmarshal(admin.expect(200, "GET", "/v1/ai/teams/"+tm.ID, nil), &out)
		return out.Runs
	}
	h.srv.teamScheduleTick(ctx, nine.Add(-time.Minute))
	if len(teamRuns()) != 0 {
		t.Fatal("ran before its time")
	}
	h.srv.teamScheduleTick(ctx, nine.Add(2*time.Minute))
	rs := teamRuns()
	if len(rs) != 1 || rs[0].Trigger != "schedule" || rs[0].By != "schedule:"+ts.ID || rs[0].Goal != good["goal"] || rs[0].RunAs != "viewer" {
		t.Fatalf("scheduled run: %+v", rs)
	}
	if r := waitTeamRun(t, admin, rs[0].ID); r.Status != trDone {
		t.Fatalf("run %+v", r)
	}
	h.srv.teamScheduleTick(ctx, nine.Add(30*time.Minute))
	if len(teamRuns()) != 1 {
		t.Error("ran twice for one time")
	}
	var list struct{ Schedules []scheduleItem }
	json.Unmarshal(dev.expect(200, "GET", "/v1/ai/schedules", nil), &list)
	if len(list.Schedules) != 1 || list.Schedules[0].Kind != "team" || list.Schedules[0].LastRun != rs[0].ID || list.Schedules[0].RunAs != "viewer" {
		t.Errorf("list %+v", list.Schedules)
	}

	// Paused, it skips its time; deleted, it is gone. Both audited.
	good["paused"] = true
	admin.expect(200, "PUT", "/v1/ai/team-schedules/"+ts.ID, good)
	h.srv.teamScheduleTick(ctx, nine.Add(24*time.Hour+time.Minute))
	if len(teamRuns()) != 1 {
		t.Error("a paused team schedule ran")
	}
	dev.expect(403, "DELETE", "/v1/ai/team-schedules/"+ts.ID, nil)
	admin.expect(204, "DELETE", "/v1/ai/team-schedules/"+ts.ID, nil)
	if au := string(admin.expect(200, "GET", "/v1/audit?action=ai.schedule.update", nil)); !strings.Contains(au, "paused") {
		t.Errorf("audit: %s", au)
	}
}

// Deleting a team takes its schedules and runs with it: they are keyed by the team's id, so a team made later with the same
// name must not find them again (a schedule nobody could list or remove, still starting runs by itself).
func TestDeletingATeamRemovesItsSchedulesAndRuns(t *testing.T) {
	h := newHarness(t, &executor.Fake{})
	admin := h.adminBrowser("ada")
	model := &teamModel{}
	srv := model.server(t)
	admin.expect(201, "POST", "/v1/providers", anthropicProvider("anthropic", srv.URL, "claude-haiku-5-5"))
	mk := func(name string) AIAgent {
		b := agentBody(name, nil, nil, "T0")
		b["model"] = "anthropic/claude-haiku-5-5"
		return newAgent(t, admin, b)
	}
	lead, member := mk("Lead"), mk("Reader")
	makeTeam := func() Team {
		var tm Team
		json.Unmarshal(admin.expect(201, "POST", "/v1/ai/teams", map[string]any{"name": "Night watch", "purpose": "p", "lead": lead.ID, "members": []string{member.ID}}), &tm)
		return tm
	}
	tm := makeTeam()
	model.plan = `{"note":"n","tasks":[{"member":"` + member.ID + `","task":"Look"}]}`
	admin.expect(201, "POST", "/v1/ai/teams/"+tm.ID+"/schedules", map[string]any{"goal": "Check", "cron": "0 9 * * *", "tz": "Europe/Berlin", "run_as": "viewer", "paused": true})
	var run TeamRun
	json.Unmarshal(admin.expect(202, "POST", "/v1/ai/teams/"+tm.ID+"/runs", map[string]any{"goal": "Say hi"}), &run)
	waitTeamRun(t, admin, run.ID)

	admin.expect(204, "DELETE", "/v1/ai/teams/"+tm.ID, nil)
	again := makeTeam()
	if again.ID != tm.ID {
		t.Fatalf("expected the same id for the same name, got %q and %q", again.ID, tm.ID)
	}
	var detail struct {
		Runs      []TeamRun
		Schedules []TeamSchedule
	}
	json.Unmarshal(admin.expect(200, "GET", "/v1/ai/teams/"+again.ID, nil), &detail)
	if len(detail.Runs) != 0 || len(detail.Schedules) != 0 {
		t.Errorf("a new team with the old name found %d runs and %d schedules of the deleted one", len(detail.Runs), len(detail.Schedules))
	}
	if left, _ := h.srv.listTeamSchedules(context.Background()); len(left) != 0 {
		t.Errorf("schedules left in the store: %+v", left)
	}
}
