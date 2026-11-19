package agentfile

import (
	"archive/zip"
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	yaml "go.yaml.in/yaml/v3"
)

var update = flag.Bool("update", false, "rewrite the committed schema")

func schemaPath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", SchemaFile)
}

func TestSchemaMatchesCommittedFile(t *testing.T) {
	got, err := Schema()
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile(schemaPath(), got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(schemaPath())
	if err != nil || string(want) != string(got) {
		t.Fatalf("committed agent schema is out of date (%v); run: go test ./internal/agentfile -run TestSchemaMatchesCommittedFile -update", err)
	}
}

func sample() Agent {
	return Agent{ID: "cloud-lead", Name: "Cloud lead", Description: "Leads the cloud check", Instructions: "Look first.\nThen say what you found.",
		Tier: "T1", Model: "anthropic/claude-haiku-5-5", Fingerprint: strings.Repeat("ab", 32), Version: 3,
		Tools: []string{"list_runs", "mcp_gh_search_issues"},
		Skills: []SkillDoc{{ID: "severity-scale", Title: "Severity scale", Description: "The low, medium, high scale", Body: "## Severity\n- low\n- high", Version: 2},
			{ID: "Runbook_Reading", Title: "Runbook reading", Body: "Find the runbook.", Version: 1}},
		At: time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)}
}

func TestAgentFileRoundTrips(t *testing.T) {
	f := FileOf(sample())
	y, err := Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(y)
	if err != nil {
		t.Fatalf("%v\n%s", err, y)
	}
	if back.Name != "Cloud lead" || back.Instructions != f.Instructions || len(back.Skills) != 2 || back.Skills[1].Name != "runbook-reading" || back.Source.Version != 3 {
		t.Errorf("read back %+v", back)
	}
	if !strings.HasPrefix(string(y), "# A Forgeline agent") || strings.Index(string(y), "version:") > strings.Index(string(y), "name:") {
		t.Errorf("fields out of order:\n%s", y)
	}
}

func TestParseRefusesWhatIsWrong(t *testing.T) {
	good := FileOf(sample())
	for name, c := range map[string]struct {
		edit func(*File)
		want string
	}{
		"version":      {func(f *File) { f.Version = "v2" }, "version"},
		"tier":         {func(f *File) { f.Tier = "T9" }, "tier"},
		"no name":      {func(f *File) { f.Name = " " }, "name"},
		"skill name":   {func(f *File) { f.Skills[0].Name = "Bad Name" }, "skills[0].name"},
		"skill path":   {func(f *File) { f.Skills[0].Path = "../x/SKILL.md" }, "skills[0].path"},
		"twice":        {func(f *File) { f.Skills[1] = f.Skills[0] }, "listed twice"},
		"instructions": {func(f *File) { f.Instructions = "" }, "instructions"},
	} {
		f := good
		f.Skills = append([]Skill(nil), good.Skills...)
		c.edit(&f)
		y, _ := Marshal(f)
		if _, err := Parse(y); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", name, err, c.want)
		}
	}
	if _, err := Parse([]byte("version: v1alpha1\nname: x\ntier: T0\ninstructions: x\nsource: {agent: a, version: 1, fingerprint: x, exported_at: x}\nsecret: 1\n")); err == nil {
		t.Error("an unknown field was accepted")
	}
}

func TestSkillMDFollowsAgentSkills(t *testing.T) {
	md, err := SkillMD(sample().Skills[1])
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(md, "---\n", 3)
	if len(parts) != 3 || parts[0] != "" {
		t.Fatalf("no front matter:\n%s", md)
	}
	var front map[string]any
	if err := yaml.Unmarshal([]byte(parts[1]), &front); err != nil {
		t.Fatal(err)
	}
	if front["name"] != "runbook-reading" || front["description"] != "Runbook reading" || strings.TrimSpace(parts[2]) != "Find the runbook." {
		t.Errorf("front %v body %q", front, parts[2])
	}
	long := sample().Skills[0]
	long.Description = strings.Repeat("x", 2000)
	md, _ = SkillMD(long)
	if strings.Count(md, "x") > 1024+10 {
		t.Error("description longer than the spec allows")
	}
	for in, want := range map[string]string{"A--b__c": "a-b-c", "---": "skill", strings.Repeat("a", 80): strings.Repeat("a", 64)} {
		if got := SkillName(in); got != want {
			t.Errorf("SkillName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBundleHasTheFiles(t *testing.T) {
	data, err := Bundle(sample(), "# readme\n")
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	files := map[string]string{}
	for _, f := range z.File {
		names = append(names, f.Name)
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		files[f.Name] = string(b)
	}
	if strings.Join(names, ",") != "agent.yaml,skills/severity-scale/SKILL.md,skills/runbook-reading/SKILL.md,README.md" {
		t.Errorf("files %v", names)
	}
	f, err := Parse([]byte(files["agent.yaml"]))
	if err != nil {
		t.Fatal(err)
	}
	body := strings.SplitN(files[f.Skills[0].Path], "---\n", 3)[2]
	if Hash(strings.TrimLeft(body, "\n")) != f.Skills[0].SHA256 {
		t.Error("the skill's hash does not match its text")
	}
}

func TestDockerYAMLForEveryProvider(t *testing.T) {
	mcp := []RemoteMCP{{ID: "gh", URL: "https://mcp.example.com/mcp", AuthHeader: "Authorization", AuthScheme: "Bearer", AuthSecret: "github-token", Tools: []string{"search_issues"}}}
	left := []LeftOut{{Tool: "list_runs", Why: "a Forgeline lookup"}}
	for kind, want := range map[string][]string{
		"anthropic":         {"model: anthropic/claude-haiku-5-5", "ANTHROPIC_API_KEY"},
		"openai":            {"model: openai/claude-haiku-5-5", "OPENAI_API_KEY"},
		"gemini":            {"model: google/claude-haiku-5-5"},
		"bedrock":           {"model: amazon-bedrock/claude-haiku-5-5"},
		"azure-openai":      {"model: main", "provider: azure", "base_url: https://res.example.com", "AZURE_API_KEY"},
		"openai-compatible": {"model: pi-ollama/claude-haiku-5-5", "base_url: https://res.example.com", "token_key: PI_OLLAMA_API_KEY"},
		"":                  {"model: anthropic/claude-haiku-5-5"},
	} {
		y, err := DockerYAML(sample(), ModelSource{ProviderID: "pi-ollama", Kind: kind, BaseURL: "https://res.example.com", Model: "claude-haiku-5-5"}, append([]RemoteMCP(nil), mcp...), left)
		if err != nil {
			t.Fatal(err)
		}
		s := string(y)
		for _, w := range append(want, "version: 16", "Authorization: Bearer ${env.GITHUB_TOKEN}", "ref: gh", "- search_issues", "name: severity-scale", "Left out (they need Forgeline): list_runs") {
			if !strings.Contains(s, w) {
				t.Errorf("%q: missing %q in\n%s", kind, w, s)
			}
		}
		var parsed map[string]any
		if err := yaml.Unmarshal(y, &parsed); err != nil {
			t.Fatalf("%q: not YAML: %v", kind, err)
		}
		if _, ok := parsed["agents"].(map[string]any)["root"]; !ok {
			t.Errorf("%q: no root agent", kind)
		}
	}
	y, _ := DockerYAML(sample(), ModelSource{}, nil, nil)
	if s := string(y); !strings.Contains(s, "model: anthropic/claude-sonnet-5-5") || strings.Contains(s, "mcps:") {
		t.Errorf("assistant's model, no MCP:\n%s", s)
	}
}
