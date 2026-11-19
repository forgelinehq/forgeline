package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forgelinehq/forgeline/internal/agentfile"
	yaml "go.yaml.in/yaml/v3"
)

// Exporting an agent (note 046, step 1): one fixed version, as a bundle or a Docker Agent file, with no secret in it.
func TestExportAnAgentAsABundleAndADockerFile(t *testing.T) {
	m := newMCPHarness(t)
	ws := &weatherMCP{desc: "Weather for a city"}
	srv := httptest.NewServer(ws)
	defer srv.Close()
	const secret = "tok-0123456789abcdef"
	m.admin.expect(201, "PUT", "/v1/secrets/WEATHER_TOKEN", map[string]any{"value": secret})
	m.admin.expect(201, "POST", "/v1/mcp-servers", map[string]any{"id": "weather", "name": "Weather", "kind": "remote", "url": srv.URL + "/mcp",
		"auth_header": "Authorization", "auth_scheme": "Bearer", "auth_secret": "WEATHER_TOKEN"})
	m.admin.expect(200, "POST", "/v1/mcp-servers/weather/discover", nil)
	m.admin.expect(200, "PUT", "/v1/mcp-servers/weather/tools", map[string]any{"enabled": []string{"get_weather"}})
	m.admin.expect(201, "POST", "/v1/providers", map[string]any{"id": "pi", "kind": "openai-compatible", "name": "Ollama on the Pi",
		"base_url": "http://127.0.0.1:11434/v1", "models": []map[string]any{{"id": "qwen3", "on": true}}})
	var sk AISkill
	json.Unmarshal(m.admin.expect(201, "POST", "/v1/ai/skills", map[string]any{"name": "Severity scale", "description": "low, medium, high", "body": "## Severity\n- low\n- high"}), &sk)
	body := agentBody("Forecaster", []SkillPin{{ID: sk.ID}}, []string{"list_runs", "mcp_weather_get_weather", "propose_aws_change"}, "T1")
	body["model"] = "pi/qwen3"
	a := newAgent(t, m.admin, body)

	// The bundle: agent.yaml that reads back, the skill as an Agent Skills folder, a README that says what is left out.
	resp, raw := m.dev.do("GET", "/v1/ai/agents/"+a.ID+"/export?format=bundle", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/zip" || !strings.Contains(resp.Header.Get("Content-Disposition"), a.ID+"-v1.zip") {
		t.Fatalf("bundle: %d %v %s", resp.StatusCode, resp.Header, raw)
	}
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		files[f.Name] = string(b)
	}
	f, err := agentfile.Parse([]byte(files["agent.yaml"]))
	if err != nil {
		t.Fatalf("%v\n%s", err, files["agent.yaml"])
	}
	if f.Model != "pi/qwen3" || f.Tier != "T1" || f.Source.Fingerprint != a.Versions[0].Hash || len(f.Skills) != 1 || f.Skills[0].Name != "severity-scale" {
		t.Errorf("agent.yaml: %+v", f)
	}
	if md := files["skills/severity-scale/SKILL.md"]; !strings.HasPrefix(md, "---\nname: severity-scale\ndescription: low, medium, high\n") || !strings.Contains(md, "## Severity") {
		t.Errorf("SKILL.md:\n%s", md)
	}
	if rd := files["README.md"]; !strings.Contains(rd, "`list_runs`: a Forgeline lookup") || !strings.Contains(rd, "`propose_aws_change`: it asks for a change") || !strings.Contains(rd, "remote MCP servers (weather)") {
		t.Errorf("README:\n%s", rd)
	}

	// The Docker Agent file: the compatible provider, the MCP server with only the agent's tool and the key as a name.
	resp, raw = m.dev.do("GET", "/v1/ai/agents/"+a.ID+"/export?format=docker&version=1", nil)
	y := string(raw)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/yaml" {
		t.Fatalf("docker: %d %s", resp.StatusCode, y)
	}
	for _, want := range []string{"version: 16", "model: pi/qwen3", "base_url: http://127.0.0.1:11434/v1", "token_key: PI_API_KEY",
		"url: " + srv.URL + "/mcp", "Authorization: Bearer ${env.WEATHER_TOKEN}", "- get_weather", "name: severity-scale", "Left out (they need Forgeline): list_runs, propose_aws_change"} {
		if !strings.Contains(y, want) {
			t.Errorf("missing %q in\n%s", want, y)
		}
	}
	if strings.Contains(y, "set_alert") {
		t.Error("a tool the agent does not have was exported")
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}

	// Never a secret, in either file.
	for name, text := range map[string]string{"bundle": strings.Join([]string{files["agent.yaml"], files["README.md"]}, ""), "docker": y} {
		if strings.Contains(text, secret) {
			t.Errorf("the %s holds the MCP server's secret", name)
		}
	}

	// Audited; viewers cannot; bad input says what is wrong.
	if au := string(m.admin.expect(200, "GET", "/v1/audit?action=ai.agent.export", nil)); !strings.Contains(au, "v1 as bundle") || !strings.Contains(au, "v1 as docker") || !strings.Contains(au, `"actor":"dev"`) {
		t.Errorf("audit: %s", au)
	}
	m.v.expect(403, "GET", "/v1/ai/agents/"+a.ID+"/export?format=bundle", nil)
	if out := string(m.dev.expect(400, "GET", "/v1/ai/agents/"+a.ID+"/export?format=zip", nil)); !strings.Contains(out, "format") {
		t.Errorf("bad format: %s", out)
	}
	m.dev.expect(404, "GET", "/v1/ai/agents/"+a.ID+"/export?format=bundle&version=9", nil)

	// A model whose provider is gone says so instead of guessing.
	m.admin.expect(204, "DELETE", "/v1/providers/pi", nil)
	if out := string(m.dev.expect(409, "GET", "/v1/ai/agents/"+a.ID+"/export?format=docker", nil)); !strings.Contains(out, `there is no provider`) {
		t.Errorf("gone provider: %s", out)
	}
}
