package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runTeamCard(id, goal string) string {
	return lookupReply("prepare_action", map[string]any{"action": "run_team", "team": id, "goal": goal})
}

// A team run card gives the team the goal it showed, as the person, only on Confirm; then the panel follows the run.
func TestARunTeamCardStartsWhatItShowedAndCanBeFollowed(t *testing.T) {
	m := newMCPHarness(t)
	m.turnOn()
	lead := newAgent(t, m.admin, agentBody("Lead", nil, nil, "T0"))
	member := newAgent(t, m.admin, agentBody("Member", nil, nil, "T0"))
	var tm Team
	json.Unmarshal(m.admin.expect(201, "POST", "/v1/ai/teams", map[string]any{"name": "Reviewers", "purpose": "p", "lead": lead.ID, "members": []string{member.ID}}), &tm)

	for name, reply := range map[string]string{"unknown team": runTeamCard("nope", "go"), "no goal": runTeamCard(tm.ID, " "), "goal too long": runTeamCard(tm.ID, strings.Repeat("x", maxTeamGoal+1))} {
		if cards := actionsIn(m.run(m.dev, map[string]any{"message": name}, reply)); len(cards) != 0 {
			t.Errorf("%s: a card was prepared: %v", name, cards)
		}
	}
	cards := actionsIn(m.run(m.dev, map[string]any{"message": "run them"}, runTeamCard(tm.ID, "Review the pipeline")))
	if len(cards) != 1 || cards[0]["kind"] != "run_team" || cards[0]["detail"] != "Review the pipeline" {
		t.Fatalf("cards = %v", cards)
	}
	id := cards[0]["id"].(string)
	if fx, _ := json.Marshal(cards[0]["effects"]); !strings.Contains(string(fx), "Reviewers") || !strings.Contains(string(fx), "developer role") {
		t.Errorf("effects = %s", fx)
	}
	var runs struct {
		Runs []TeamRun `json:"runs"`
	}
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/ai/teams/"+tm.ID, nil), &runs)
	if len(runs.Runs) != 0 {
		t.Fatal("a run started before the person confirmed")
	}
	m.dev.expect(409, "GET", "/v1/assistant/actions/"+id+"/run", nil) // nothing has started
	m.admin.expect(404, "POST", "/v1/assistant/actions/"+id+"/confirm", nil)

	m.llm.script(`{"note":"one","tasks":[{"member":"`+member.ID+`","task":"Look"}]}`, "The member's finding.", "The verdict.")
	var a AssistAction
	json.Unmarshal(m.dev.expect(200, "POST", "/v1/assistant/actions/"+id+"/confirm", nil), &a)
	if a.Status != actDone || !strings.HasPrefix(a.Link, "/teams/"+tm.ID+"?run=") {
		t.Fatalf("action = %+v", a)
	}
	run := waitTeamRun(t, m.dev, runIDOf(a))
	if run.By != "dev" || run.Goal != "Review the pipeline" {
		t.Errorf("run = %+v", run)
	}
	var v RunViewJSON
	json.Unmarshal(m.dev.expect(200, "GET", "/v1/assistant/actions/"+id+"/run", nil), &v)
	if v.Status != "succeeded" || v.Title != "Reviewers" || v.Output != "The verdict." || v.Link != a.Link || len(v.Steps) != 2 || v.Steps[0] != (RunViewStep{Name: "Lead plans", Status: "succeeded"}) || v.Steps[1] != (RunViewStep{Name: "Member", Status: "succeeded"}) {
		t.Errorf("view = %+v", v)
	}
	m.admin.expect(404, "GET", "/v1/assistant/actions/"+id+"/run", nil) // a card is its owner's

	// The card says which rights the run will have: an administrator's own for their team, a developer's once a developer
	// wrote any part of it.
	says := func(want string) {
		t.Helper()
		got := actionsIn(m.run(m.admin, map[string]any{"message": want}, runTeamCard(tm.ID, "As "+want)))
		if fx, _ := json.Marshal(got[0]["effects"]); !strings.Contains(string(fx), want+" role") {
			t.Errorf("an administrator's card for the %s case: %s", want, fx)
		}
	}
	says("admin")
	m.dev.expect(200, "PUT", "/v1/ai/teams/"+tm.ID, map[string]any{"name": "Reviewers", "purpose": "a developer's words", "lead": lead.ID, "members": []string{member.ID}})
	says("developer")

	// A team, or an agent in it, that changes after the card was prepared is not run from that card.
	for name, change := range map[string]func(){
		"agent": func() {
			b := agentBody("Member", nil, nil, "T0")
			b["instructions"] = "Something else."
			m.admin.expect(201, "POST", "/v1/ai/agents/"+member.ID+"/versions", b)
		},
		"team": func() {
			m.admin.expect(200, "PUT", "/v1/ai/teams/"+tm.ID, map[string]any{"name": "Reviewers", "purpose": "another", "lead": lead.ID, "members": []string{member.ID}})
		},
	} {
		cards = actionsIn(m.run(m.dev, map[string]any{"message": name}, runTeamCard(tm.ID, "Review "+name)))
		if len(cards) != 1 {
			t.Fatalf("%s: cards = %v", name, cards)
		}
		change()
		if out := string(m.dev.expect(409, "POST", "/v1/assistant/actions/"+cards[0]["id"].(string)+"/confirm", nil)); !strings.Contains(out, "changed since this card was prepared") {
			t.Errorf("%s: %s", name, out)
		}
	}
}

// The panel follows a run card until it ends or waits, and tells the assistant once, without the run's own text.
const runFollowCases = `
import { followRun, outcomeWords, RUN_KINDS } from './runfollow.mjs';

let failed = 0;
const eq = (name, got, want) => {
  const g = JSON.stringify(got), w = JSON.stringify(want);
  if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); }
};
// follow reads the answers in turn (an object, or an Error to throw), keeping the last one, with a clock that moves.
async function follow(answers, { step = 1000, goneAfter = 50 } = {}) {
  const shown = [], told = [];
  let i = 0, t = 0;
  await followRun({
    read: async () => { const a = answers[Math.min(i++, answers.length - 1)]; if (a instanceof Error) throw a; return a; },
    show: (v) => shown.push(v.status), settled: (s) => told.push(s),
    alive: () => i < goneAfter, sleep: async () => { t += step; }, now: () => t,
  });
  return { shown, told, reads: i };
}
const run = { status: 'running' }, ok = { status: 'succeeded' }, wait = { status: 'waiting' };
const err = (status) => Object.assign(new Error('x'), { status });

eq('until it ends', await follow([run, run, ok]), { shown: ['running', 'running', 'succeeded'], told: ['succeeded'], reads: 3 });
eq('failed and cancelled end it too', [(await follow([{ status: 'failed' }])).told, (await follow([{ status: 'cancelled' }])).told], [['failed'], ['cancelled']]);
eq('waiting is said once and still followed', await follow([wait, wait, run, ok]), { shown: ['waiting', 'waiting', 'running', 'succeeded'], told: ['waiting'], reads: 4 });
const long = await follow([run], { step: 5 * 60 * 1000, goneAfter: 6 });
eq('a long run is reported once, and followed while the card is there', [long.told, long.reads], [['running'], 6]);
eq('a passing error is tried again', (await follow([err(0), err(502), ok])).told, ['succeeded']);
eq('a refusal ends it', await follow([err(404), ok]), { shown: [], told: ['unknown'], reads: 1 });
eq('too many errors end it', (await follow([err(500)])).reads, 5);
eq('a card taken off the page stops', (await follow([run], { goneAfter: 2 })).told, []);
eq('the kinds', [...RUN_KINDS], ['run_pipeline', 'run_workflow', 'run_team']);
for (const s of ['succeeded', 'failed', 'cancelled', 'waiting', 'running', 'unknown', 'surprise']) eq('words for ' + s, outcomeWords(s).length > 10, true);
if (failed) process.exit(1);
`

func TestRunCardsAreFollowed(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	src, err := uiFiles.ReadFile("ui/lib/runfollow.js")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "runfollow.mjs"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	test := filepath.Join(tmp, "cases.mjs")
	if err := os.WriteFile(test, []byte(runFollowCases), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("run follow cases failed:\n%s", out)
	}
}
