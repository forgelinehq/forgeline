package server

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/authn"
)

// A developer deletes what they created and nothing else, an administrator's or another developer's; an administrator
// deletes anything. Who created a thing does not change when somebody else saves it.
func TestADeveloperDeletesOnlyWhatTheyCreated(t *testing.T) {
	m := newMCPHarness(t)
	m.admin.createUser("dana", authn.Developer)
	dana := m.login("dana")
	id := func(out []byte) string {
		var v struct {
			ID string `json:"id"`
		}
		json.Unmarshal(out, &v)
		if v.ID == "" {
			t.Fatalf("no id in %s", out)
		}
		return v.ID
	}
	worker := newAgent(t, m.admin, agentBody("Worker", nil, nil, "T0"))
	n := 0
	// Each kind makes a thing as someone and says where it is deleted and what a delete answers.
	kinds := map[string]struct {
		make func(c *browser) string
		ok   int
	}{
		"pipeline": {func(c *browser) string {
			n++
			return "/v1/pipelines/" + id(c.expect(201, "POST", "/v1/pipelines", map[string]any{"yaml": "version: v1alpha1\nname: p" + string(rune('a'+n)) + "\nsteps:\n  - id: a\n    image: alpine:3.22\n    run: \"true\"\n"}))
		}, 204},
		"skill": {func(c *browser) string {
			n++
			return "/v1/ai/skills/" + id(c.expect(201, "POST", "/v1/ai/skills", map[string]any{"name": "Skill " + string(rune('a'+n)), "description": "d", "body": "steps"}))
		}, 204},
		"agent": {func(c *browser) string {
			n++
			return "/v1/ai/agents/" + newAgent(t, c, agentBody("Agent "+string(rune('a'+n)), nil, nil, "T0")).ID
		}, 200},
		"workflow": {func(c *browser) string {
			n++
			return "/v1/ai/workflows/" + newWorkflow(t, c, wfBody("Flow "+string(rune('a'+n)), []WFStage{{Steps: []WFStep{step(worker.ID, "look")}}})).ID
		}, 200},
		"eval suite": {func(c *browser) string {
			n++
			return "/v1/ai/evals/" + id(c.expect(201, "POST", "/v1/ai/evals", map[string]any{"agent": worker.ID, "name": "Suite " + string(rune('a'+n)), "cases": []map[string]any{{"name": "ok", "input": "go", "checks": []map[string]any{{"kind": "contains", "value": "ok"}}}}}))
		}, 204},
		"team": {func(c *browser) string {
			n++
			lead := newAgent(t, m.admin, agentBody("Lead "+string(rune('a'+n)), nil, nil, "T0"))
			return "/v1/ai/teams/" + id(c.expect(201, "POST", "/v1/ai/teams", map[string]any{"name": "Team " + string(rune('a'+n)), "purpose": "p", "lead": lead.ID, "members": []string{worker.ID}}))
		}, 204},
	}
	for name, k := range kinds {
		theirs, other, mine := k.make(m.admin), k.make(dana), k.make(m.dev)
		for _, path := range []string{theirs, other} {
			if out := string(m.dev.expect(403, "DELETE", path, nil)); !strings.Contains(out, "or an administrator can delete") {
				t.Errorf("%s: %s", name, out)
			}
		}
		m.dev.expect(k.ok, "DELETE", mine, nil)
		m.admin.expect(k.ok, "DELETE", other, nil)
		m.admin.expect(k.ok, "DELETE", theirs, nil)
	}
	if aud := string(m.admin.expect(200, "GET", "/v1/audit?action=pipeline.delete", nil)); !strings.Contains(aud, "refused: not the creator") {
		t.Errorf("the refusal is not in the audit log: %s", aud)
	}

	// Saving somebody's thing does not make it yours: an agent, a team and a schedule keep their creator.
	theirAgent := newAgent(t, dana, agentBody("Hers", nil, nil, "T0"))
	changed := agentBody("Hers", nil, nil, "T0")
	changed["instructions"] = "Mine now?"
	newVersion(t, m.dev, theirAgent.ID, changed)
	m.dev.expect(403, "DELETE", "/v1/ai/agents/"+theirAgent.ID, nil)
	team := map[string]any{"name": "Night", "purpose": "p", "lead": worker.ID, "members": []string{theirAgent.ID}}
	tm := id(dana.expect(201, "POST", "/v1/ai/teams", team))
	team["id"] = tm
	m.dev.expect(200, "PUT", "/v1/ai/teams/"+tm, team)
	m.dev.expect(403, "DELETE", "/v1/ai/teams/"+tm, nil)
	sched := map[string]any{"goal": "look", "every_minutes": 60, "run_as": "viewer"}
	sid := id(dana.expect(201, "POST", "/v1/ai/teams/"+tm+"/schedules", sched))
	if list := string(m.dev.expect(200, "GET", "/v1/ai/schedules", nil)); !strings.Contains(list, `"created_by":"dana"`) {
		t.Errorf("the schedule list does not say who made it, so the console cannot offer Delete: %s", list)
	}
	m.dev.expect(200, "PUT", "/v1/ai/team-schedules/"+sid, sched)
	m.dev.expect(403, "DELETE", "/v1/ai/team-schedules/"+sid, nil)
	dana.expect(204, "DELETE", "/v1/ai/team-schedules/"+sid, nil)

	// A team holding somebody else's schedule is not its creator's to delete: the schedule would go with it.
	sid = id(m.admin.expect(201, "POST", "/v1/ai/teams/"+tm+"/schedules", sched))
	if out := string(dana.expect(403, "DELETE", "/v1/ai/teams/"+tm, nil)); !strings.Contains(out, "schedule made by someone else") {
		t.Errorf("a team with an administrator's schedule: %s", out)
	}
	m.admin.expect(204, "DELETE", "/v1/ai/team-schedules/"+sid, nil)

	// A record from before creators were kept is an administrator's to delete.
	var old Team
	if err := m.srv.docs.Get(context.Background(), kindTeam, tm, &old); err != nil {
		t.Fatal(err)
	}
	old.CreatedBy = ""
	if err := m.srv.docs.Put(context.Background(), kindTeam, tm, old); err != nil {
		t.Fatal(err)
	}
	if out := string(dana.expect(403, "DELETE", "/v1/ai/teams/"+tm, nil)); !strings.Contains(out, "only an administrator") {
		t.Errorf("no creator: %s", out)
	}
	m.admin.expect(204, "DELETE", "/v1/ai/teams/"+tm, nil)
}

// The console offers Delete to the same people the server lets delete.
func TestTheConsoleOffersDeleteToTheCreatorAndAdministrators(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	src, err := uiFiles.ReadFile("ui/lib/api.js")
	if err != nil {
		t.Fatal(err)
	}
	sse, err := uiFiles.ReadFile("ui/lib/sse.js")
	if err != nil {
		t.Fatal(err)
	}
	src = []byte(strings.ReplaceAll(string(src), "./sse.js", "./sse.mjs"))
	dir := t.TempDir()
	check := `import { mayDelete } from './api.mjs';
const got = [[{ role: 'admin', name: 'a' }, 'x'], [{ role: 'admin', name: 'a' }, ''], [{ role: 'developer', name: 'd' }, 'd'], [{ role: 'developer', name: 'd' }, 'x'], [{ role: 'developer', name: 'd' }, ''], [{ role: 'developer', name: '' }, ''], [{ role: 'developer', name: 'd' }, undefined]].map(([u, c]) => mayDelete(u, c));
const want = [true, true, true, false, false, false, false];
if (JSON.stringify(got) !== JSON.stringify(want)) { console.error(JSON.stringify(got)); process.exit(1); }
`
	for name, body := range map[string][]byte{"api.mjs": src, "sse.mjs": sse, "check.mjs": []byte(check)} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command(node, filepath.Join(dir, "check.mjs")).CombinedOutput(); err != nil {
		t.Errorf("%s", out)
	}
	for page, creator := range map[string]string{"editor": "saved?.created_by", "agents": "ag.versions[0]?.by", "workflows": "wf.versions[0]?.by", "schedules": "it.created_by", "team": "t.created_by", "evals": "x.created_by"} {
		b, _ := uiFiles.ReadFile("ui/pages/" + page + ".js")
		if !strings.Contains(string(b), "mayDelete(session, "+creator+")") {
			t.Errorf("%s.js does not hide Delete from someone who may not", page)
		}
	}
	if b, _ := uiFiles.ReadFile("ui/pages/agents.js"); !strings.Contains(string(b), "mayDelete(session, sk.versions[0]?.by)") {
		t.Error("the skill page does not hide Delete from someone who may not")
	}
}
