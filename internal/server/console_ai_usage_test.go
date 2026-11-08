package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func usageOf(t *testing.T, b *browser) map[string]UsageJSON {
	t.Helper()
	var r struct{ Usage []UsageJSON }
	json.Unmarshal(b.expect(200, "GET", "/v1/ai/usage", nil), &r)
	out := map[string]UsageJSON{}
	for _, u := range r.Usage {
		out[u.Who] = u
	}
	return out
}

func TestAgentUseIsCountedAndBudgetsStopIt(t *testing.T) {
	m := newMCPHarness(t)
	a := newAgent(t, m.admin, agentBody("Cost watcher", nil, nil, "T0"))
	other := newAgent(t, m.admin, agentBody("Helper", nil, nil, "T0"))
	m.llm.script("one", "two", "three", "four")

	// Each call counts against the agent that made it: input (with cache) and output tokens, and time.
	m.dev.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "go"})
	m.dev.expect(200, "POST", "/v1/assistant/chat", map[string]any{"message": "hello"})
	u := usageOf(t, m.dev)
	if got := u[a.ID]; got.Calls != 1 || got.Input != 1200 || got.Output != 300 || got.Month != time.Now().UTC().Format("2006-01") {
		t.Errorf("agent use = %+v", got)
	}
	if got := u[peopleBucket]; got.Calls != 1 || got.Output != 300 {
		t.Errorf("people's use = %+v", got)
	}
	if _, ok := u[other.ID]; ok {
		t.Errorf("an agent that did nothing has use")
	}
	m.dev.expect(400, "GET", "/v1/ai/usage?month=2026-13", nil)

	// Budgets are for administrators, audited, and shown on the team.
	m.dev.expect(403, "PUT", "/v1/ai/agents/"+a.ID+"/budget", map[string]any{"tokens": 2000})
	m.admin.expect(400, "PUT", "/v1/ai/agents/"+a.ID+"/budget", map[string]any{"tokens": -1})
	var st BudgetStateJSON
	json.Unmarshal(m.admin.expect(200, "PUT", "/v1/ai/agents/"+a.ID+"/budget", map[string]any{"tokens": 1800}), &st)
	if st.Level != "warn" || st.Fraction < 0.8 || st.Fraction >= 1 {
		t.Errorf("1500 of 1800 tokens should warn: %+v", st)
	}
	_, by := teamOf(t, m.dev)
	if b := by["Cost watcher"].Budget; b.Level != "warn" || b.Budget == nil || b.Budget.Tokens != 1800 || b.Used.Output != 300 {
		t.Errorf("team budget = %+v", b)
	}
	// A new version keeps the budget: it is on the agent, not its versions.
	m.admin.expect(201, "POST", "/v1/ai/agents/"+a.ID+"/versions", map[string]any{"instructions": "Find waste.", "tier": "T0", "note": "n"})

	// The next call goes through (it was under the limit before it), then the agent stops.
	m.dev.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "go"})
	out := string(m.dev.expect(502, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "go"}))
	if !strings.Contains(out, "has used its budget") || !strings.Contains(out, "3000 of 1800 tokens") {
		t.Errorf("over budget: %s", out)
	}
	if n := len(m.llm.reqs); n != 3 {
		t.Errorf("the stopped call must not reach the model: %d calls", n)
	}
	// Other agents and people are not affected.
	m.dev.expect(200, "POST", "/v1/ai/agents/"+other.ID+"/chat", map[string]any{"message": "go"})
	audit := string(m.admin.expect(200, "GET", "/v1/audit?action=ai.agent.budget", nil))
	for _, want := range []string{"ai.agent.budget.stop", "Cost watcher: 1800 tokens a month", "Cost watcher has used 3000 of 1800 tokens"} {
		if !strings.Contains(audit, want) {
			t.Errorf("audit misses %q", want)
		}
	}
	// Raising the budget lets it run again; minutes count too; zero clears it.
	m.admin.expect(200, "PUT", "/v1/ai/agents/"+a.ID+"/budget", map[string]any{"tokens": 100000})
	m.llm.script("five")
	m.dev.expect(200, "POST", "/v1/ai/agents/"+a.ID+"/chat", map[string]any{"message": "go"})
	st = BudgetStateJSON{}
	json.Unmarshal(m.admin.expect(200, "PUT", "/v1/ai/agents/"+a.ID+"/budget", map[string]any{"minutes": 1}), &st)
	if st.Level != "ok" || st.Budget.Minutes != 1 || st.Budget.Tokens != 0 {
		t.Errorf("a minutes budget: %+v", st)
	}
	st = BudgetStateJSON{}
	json.Unmarshal(m.admin.expect(200, "PUT", "/v1/ai/agents/"+a.ID+"/budget", map[string]any{}), &st)
	if st.Budget != nil || st.Level != "ok" {
		t.Errorf("cleared: %+v", st)
	}
}

func TestBudgetStateAndCLIOutput(t *testing.T) {
	b := &AgentBudget{Tokens: 1000, Minutes: 10}
	cases := []struct {
		used  modelUsage
		level string
	}{
		{modelUsage{Input: 100}, "ok"},
		{modelUsage{Input: 700, Output: 100}, "warn"},
		{modelUsage{Output: 1000}, "stop"},
		{modelUsage{Millis: 8 * 60000}, "warn"}, // minutes count on their own
		{modelUsage{Millis: 10 * 60000}, "stop"},
	}
	for _, c := range cases {
		if got := budgetState(b, c.used).Level; got != c.level {
			t.Errorf("%+v: %s, want %s", c.used, got, c.level)
		}
	}
	if st := budgetState(nil, modelUsage{Input: 1e9}); st.Level != "ok" || st.Fraction != 0 {
		t.Errorf("no budget never stops: %+v", st)
	}
	reply, use, failed := readCLIOutput([]byte(`{"type":"result","is_error":false,"result":" OK ","total_cost_usd":0.25,"usage":{"input_tokens":2,"cache_creation_input_tokens":100,"cache_read_input_tokens":50,"output_tokens":4}}`))
	if reply != "OK" || failed || use.Input != 152 || use.Output != 4 || use.CostUSD != 0.25 || use.Costed != 1 || use.Unknown {
		t.Errorf("cli json: %q %+v", reply, use)
	}
	if reply, _, failed := readCLIOutput([]byte(`{"type":"result","is_error":true,"result":"Credit balance is too low"}`)); !failed || reply != "Credit balance is too low" {
		t.Errorf("a reported error must fail: %q", reply)
	}
	if reply, use, _ := readCLIOutput([]byte("plain text answer\n")); reply != "plain text answer" || !use.Unknown {
		t.Errorf("plain output: %q %+v", reply, use)
	}
}

const usageWordCases = `
import { tokens, minutes, useLine, costLine, budgetLine, recentMonths } from './usage.mjs';
let failed = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { failed++; console.error('FAIL ' + name + '\n  got  ' + g + '\n  want ' + w); } };
eq('tokens', [950, 1000, 12345, 123456, 4500000, 45000000].map(tokens), ['950', '1k', '12k', '123k', '4.5M', '45M']);
eq('minutes', [30000, 600000, 754000].map(minutes), ['0.5 min', '10 min', '13 min']);
eq('use line', useLine({ input_tokens: 1200, output_tokens: 300, millis: 90000, calls: 3 }), '1.5k tokens · 1.5 min · 3 calls');
eq('one call', useLine({ input_tokens: 1, output_tokens: 1, millis: 0, calls: 1 }), '2 tokens · 0 min · 1 call');
eq('cost unknown', costLine({ calls: 2, costed_calls: 0 }), 'cost not reported');
eq('cost known', costLine({ calls: 2, costed_calls: 2, cost_usd: 0.1234 }), '$0.12');
eq('cost partly known', costLine({ calls: 3, costed_calls: 2, cost_usd: 1.5 }), '$1.50 for 2 of 3 calls');
eq('no budget', budgetLine({ used: {}, level: 'ok' }), null);
eq('warn', budgetLine({ budget: { tokens: 10000 }, used: { input_tokens: 8000, output_tokens: 500 }, fraction: 0.85, level: 'warn' }), { text: '8.5k of 10k tokens this month · nearly used up', pct: 85, level: 'warn' });
eq('stop, both limits, capped at 100', budgetLine({ budget: { tokens: 1000, minutes: 5 }, used: { output_tokens: 3000, millis: 60000 }, fraction: 3, level: 'stop' }),
  { text: '3k of 1k tokens, 1 min of 5 min this month · stopped until next month or a higher budget', pct: 100, level: 'stop' });
eq('months', recentMonths(new Date(Date.UTC(2026, 0, 15)), 3), ['2026-01', '2025-12', '2025-11']);
if (failed) process.exit(1);
`

func TestConsoleUsageWords(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	src, _ := uiFiles.ReadFile("ui/lib/usage.js")
	os.WriteFile(filepath.Join(tmp, "usage.mjs"), src, 0o600)
	test := filepath.Join(tmp, "cases.mjs")
	os.WriteFile(test, []byte(usageWordCases), 0o600)
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil {
		t.Fatalf("usage words failed:\n%s", out)
	}
}

// The assistant is told where things are, so it can answer "how do I…" about the newer pages. A page that moves
// or is renamed should be changed here too.
func TestAssistantKnowsWhereThingsAre(t *testing.T) {
	for _, want := range []string{"Build › Design", "Preview changes", "Watch › Map", "AI use and budgets", "Approval rules", "different administrator approves"} {
		if !strings.Contains(assistantSystem, want) {
			t.Errorf("the assistant's instructions do not mention %q", want)
		}
	}
}
