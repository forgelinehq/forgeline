package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

// Importing a bundle (note 046, step 2): into another server the skill is created, a tool it lacks is dropped with a
// reason, a model it lacks falls back to the assistant's; importing the same bundle again changes nothing.
func TestImportABundleIntoAnotherServer(t *testing.T) {
	// The first server: an agent with a skill, a remote MCP tool, a provider model and a proposal tool.
	src := newMCPHarness(t)
	ws := httptest.NewServer(&weatherMCP{desc: "Weather for a city"})
	defer ws.Close()
	src.admin.expect(201, "POST", "/v1/mcp-servers", map[string]any{"id": "weather", "name": "Weather", "kind": "remote", "url": ws.URL + "/mcp"})
	src.admin.expect(200, "POST", "/v1/mcp-servers/weather/discover", nil)
	src.admin.expect(200, "PUT", "/v1/mcp-servers/weather/tools", map[string]any{"enabled": []string{"get_weather"}})
	src.admin.expect(201, "POST", "/v1/providers", map[string]any{"id": "pi", "kind": "openai-compatible", "name": "Pi",
		"base_url": "http://127.0.0.1:11434/v1", "models": []map[string]any{{"id": "qwen3", "on": true}}})
	var sk AISkill
	json.Unmarshal(src.admin.expect(201, "POST", "/v1/ai/skills", map[string]any{"name": "Severity scale", "description": "low, medium, high", "body": "## Severity\n- low\n- high"}), &sk)
	body := agentBody("Forecaster", []SkillPin{{ID: sk.ID}}, []string{"list_runs", "mcp_weather_get_weather", "propose_aws_change"}, "T1")
	body["model"] = "pi/qwen3"
	a := newAgent(t, src.admin, body)
	_, bundle := src.dev.do("GET", "/v1/ai/agents/"+a.ID+"/export?format=bundle", nil)

	// The same bundle back into the same server: the agent exists with the same content, so nothing is saved.
	var same importPlan
	json.Unmarshal(src.admin.expect(200, "POST", "/v1/ai/agents/import", map[string]any{"bundle": bundle}), &same)
	if same.Action != "unchanged" || same.AgentID != a.ID || same.Skills[0].Action != "use" || same.Skills[0].ID != sk.ID || len(same.Dropped) != 0 {
		t.Errorf("same server: %+v", same)
	}

	// Another server: a dry run says what will happen and saves nothing.
	dst := newMCPHarness(t)
	var plan importPlan
	json.Unmarshal(dst.admin.expect(200, "POST", "/v1/ai/agents/import", map[string]any{"bundle": bundle, "dry_run": true}), &plan)
	if plan.Action != "create" || plan.Name != "Forecaster" || plan.Model != "" || len(plan.Skills) != 1 || plan.Skills[0].Action != "create" ||
		len(plan.Dropped) != 1 || plan.Dropped[0].Tool != "mcp_weather_get_weather" || strings.Join(plan.Tools, ",") != "list_runs,propose_aws_change" ||
		!strings.Contains(strings.Join(plan.Notes, " "), "pi/qwen3 is not available here") || !strings.HasPrefix(plan.From, a.ID+" v1") {
		t.Errorf("dry run: %+v", plan)
	}
	if out := string(dst.admin.expect(200, "GET", "/v1/ai/agents", nil)); strings.Contains(out, "Forecaster") {
		t.Error("a dry run saved the agent")
	}
	if out := string(dst.admin.expect(200, "GET", "/v1/ai/skills", nil)); strings.Contains(out, "Severity") {
		t.Error("a dry run saved a skill")
	}

	// The import: the skill is created with the bundle's text, the agent pins it, the version says where it came from.
	var done importPlan
	json.Unmarshal(dst.admin.expect(201, "POST", "/v1/ai/agents/import", map[string]any{"bundle": bundle}), &done)
	got := done.Agent
	if got == nil || done.Action != "create" || got.Name != "Forecaster" || len(got.Versions) != 1 {
		t.Fatalf("import: %+v", done)
	}
	v := got.Versions[0]
	if v.Tier != "T1" || v.Model != "" || strings.Join(v.Tools, ",") != "list_runs,propose_aws_change" || len(v.Skills) != 1 || v.Skills[0].ID != done.Skills[0].ID || !strings.HasPrefix(v.Note, "imported from "+a.ID) {
		t.Errorf("imported version: %+v", v)
	}
	var newSk AISkill
	json.Unmarshal(dst.admin.expect(200, "GET", "/v1/ai/skills/"+v.Skills[0].ID, nil), &newSk)
	if newSk.Name != "Severity scale" || newSk.Description != "low, medium, high" || strings.TrimSpace(newSk.Versions[0].Body) != "## Severity\n- low\n- high" {
		t.Errorf("imported skill: %+v", newSk)
	}

	// Again: the skill is found by its text and nothing changes.
	var again importPlan
	json.Unmarshal(dst.admin.expect(200, "POST", "/v1/ai/agents/import", map[string]any{"bundle": bundle}), &again)
	if again.Action != "unchanged" || again.Skills[0].Action != "use" || again.Skills[0].ID != newSk.ID {
		t.Errorf("again: %+v", again)
	}
	if out := string(dst.admin.expect(200, "GET", "/v1/ai/skills", nil)); strings.Count(out, `"name":"Severity scale"`) != 1 {
		t.Errorf("the skill was created twice: %s", out)
	}

	// Audited; only administrators; a bad bundle says what is wrong.
	if au := string(dst.admin.expect(200, "GET", "/v1/audit?action=ai.agent.import", nil)); !strings.Contains(au, a.ID+" v1") || !strings.Contains(au, "1 skill(s) created, 1 tool(s) dropped") {
		t.Errorf("audit: %s", au)
	}
	dst.dev.expect(403, "POST", "/v1/ai/agents/import", map[string]any{"bundle": bundle, "dry_run": true})
	if out := string(dst.admin.expect(400, "POST", "/v1/ai/agents/import", map[string]any{"bundle": []byte("not a zip")})); !strings.Contains(out, "not a zip") {
		t.Errorf("bad bundle: %s", out)
	}
}

// A T0 agent never keeps a proposal tool, and an agent cannot be given ask_agent, whatever a bundle says.
func TestImportDropsToolsTheTierDoesNotAllow(t *testing.T) {
	m := newMCPHarness(t)
	a := newAgent(t, m.admin, agentBody("Reviewer", nil, []string{"list_runs", "propose_aws_change"}, "T1"))
	_, bundle := m.dev.do("GET", "/v1/ai/agents/"+a.ID+"/export?format=bundle", nil)
	bundle = rewriteAgentYAML(t, bundle, func(y string) string {
		y = strings.Replace(y, "tier: T1", "tier: T0", 1)
		return strings.Replace(y, "  - list_runs\n", "  - list_runs\n  - ask_agent\n", 1)
	})
	var plan importPlan
	json.Unmarshal(m.admin.expect(200, "POST", "/v1/ai/agents/import", map[string]any{"bundle": bundle, "dry_run": true}), &plan)
	why := map[string]string{}
	for _, d := range plan.Dropped {
		why[d.Tool] = d.Why
	}
	if strings.Join(plan.Tools, ",") != "list_runs" || !strings.Contains(why["propose_aws_change"], "T1") || !strings.Contains(why["ask_agent"], "other agents") || plan.Action != "version" {
		t.Errorf("plan: %+v", plan)
	}
}

// rewriteAgentYAML changes agent.yaml inside a bundle.
func rewriteAgentYAML(t *testing.T, data []byte, edit func(string) string) []byte {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range z.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		if f.Name == "agent.yaml" {
			b = []byte(edit(string(b)))
		}
		fw, _ := w.Create(f.Name)
		fw.Write(b)
	}
	w.Close()
	return buf.Bytes()
}
