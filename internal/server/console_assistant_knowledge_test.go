package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// knowledgeSources are the documents the assistant knows, copied into knowledge/ to be built into the binary.
func knowledgeSources(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	guides, _ := filepath.Glob("../../docs/guides/*.md")
	for _, g := range append(guides, "../../docs/get-started.md", "../../docs/status.md") {
		if filepath.Base(g) == "index.md" {
			continue
		}
		raw, err := os.ReadFile(g)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.Base(g)] = string(raw)
	}
	return out
}

// TestKnowledgeMatchesTheDocs keeps the assistant's copy of the guides in step with docs/. After editing a guide, run
// FORGELINE_UPDATE_KNOWLEDGE=1 go test ./internal/server -run TestKnowledgeMatchesTheDocs
func TestKnowledgeMatchesTheDocs(t *testing.T) {
	src := knowledgeSources(t)
	if os.Getenv("FORGELINE_UPDATE_KNOWLEDGE") == "1" {
		old, _ := filepath.Glob("knowledge/*.md")
		for _, f := range old {
			os.Remove(f)
		}
		for name, body := range src {
			if err := os.WriteFile(filepath.Join("knowledge", name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	have, _ := filepath.Glob("knowledge/*.md")
	if len(have) != len(src) {
		t.Errorf("knowledge/ has %d files, docs has %d (run with FORGELINE_UPDATE_KNOWLEDGE=1)", len(have), len(src))
	}
	for name, body := range src {
		got, err := os.ReadFile(filepath.Join("knowledge", name))
		if err != nil || string(got) != body {
			t.Errorf("knowledge/%s differs from docs (run with FORGELINE_UPDATE_KNOWLEDGE=1)", name)
		}
	}
}

func TestSearchDocsFindsTheRightSection(t *testing.T) {
	for q, want := range map[string]string{
		"what is a mission":                  "Missions",
		"how do teams work, who is the lead": "Teams",
		"connect ollama local model":         "A free local model",
		"remove what a blueprint built":      "Build",
		"monthly budget for an agent":        "Budgets",
		"approval rules two approvals":       "",
	} {
		secs := searchDocs(q, 3)
		if len(secs) == 0 {
			t.Errorf("%q: nothing found", q)
			continue
		}
		if want != "" && !strings.Contains(secs[0].Heading, want) && !strings.Contains(secs[1].Heading, want) {
			t.Errorf("%q: got %q then %q, want %q in the first two", q, secs[0].Heading, secs[1].Heading, want)
		}
	}
	if len(searchDocs("the and of", 3)) != 0 {
		t.Error("stop words alone matched")
	}
	// Pictures and site link syntax are left out of what the model reads.
	for _, sec := range loadKnowledge() {
		if strings.Contains(sec.Text, "relative_url") || strings.Contains(sec.Text, "![") {
			t.Fatalf("%s › %s still has site syntax", sec.Guide, sec.Heading)
		}
	}
}

// The assistant is told which page the person is on, with the thing on it (ui/lib/pagectx.js).
const pageContextCases = `
import { pageContext } from './pagectx.js';
const eq = (name, got, want) => { if (JSON.stringify(got) !== JSON.stringify(want)) { console.log('FAIL', name, JSON.stringify(got), 'want', JSON.stringify(want)); process.exitCode = 1; } };
const pick = (c) => [c.kind, c.id, c.where];
eq('missions list', pick(pageContext('/missions')), ['page', '', 'Team › Missions']);
eq('one mission', pick(pageContext('/missions/m-42')), ['mission', 'm-42', 'Team › Missions']);
eq('a team', pick(pageContext('/teams/platform-health')), ['team', 'platform-health', 'Team › Teams']);
eq('workflows tab', pick(pageContext('/agents?tab=workflows')), ['page', '', 'Team › Agents › Workflows']);
eq('a workflow canvas', pick(pageContext('/workflows/triage/canvas')), ['workflow', 'triage', 'Team › Workflows']);
eq('workflows', pick(pageContext('/workflows?tab=runs')), ['page', '', 'Team › Workflows › Runs']);
eq('new workflow', pick(pageContext('/workflows/new/canvas'))[0], 'page');
eq('a workflow run', pick(pageContext('/workflow-runs/r1')), ['workflow_run', 'r1', 'Team › Workflows']);
eq('providers', pick(pageContext('/providers')), ['page', '', 'Settings › Models and providers']);
eq('home', pick(pageContext('/')), ['none', '', 'Home › Today']);
eq('a run', pick(pageContext('/runs/abc')), ['run', 'abc', 'Ship › Pipelines']);
eq('new pipeline', pick(pageContext('/pipelines/new'))[0], 'page');
eq('design', pick(pageContext('/design/bp-1')), ['page', '', 'Build › Design']);
eq('security', pick(pageContext('/guard'))[0], 'guard');
`

func TestAssistantKnowsTheOpenPage(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	tmp := t.TempDir()
	for _, f := range []string{"pagectx.js", "nav.js", "settingsnav.js"} {
		src, err := uiFiles.ReadFile("ui/lib/" + f)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(tmp, f), src, 0o600)
	}
	os.WriteFile(filepath.Join(tmp, "package.json"), []byte(`{"type":"module"}`), 0o600)
	test := filepath.Join(tmp, "cases.js")
	os.WriteFile(test, []byte(pageContextCases), 0o600)
	if out, err := exec.Command(node, test).CombinedOutput(); err != nil || len(out) > 0 {
		t.Fatalf("page context cases failed (%v):\n%s", err, out)
	}
}

func TestAssistantIsToldThePageAndWhatTheDocsSayAboutIt(t *testing.T) {
	llm := newScriptedLLM(t, "fine")
	_, _, dev, _ := assistantHarness(t, llm, "x")
	system := func() string {
		llm.mu.Lock()
		defer llm.mu.Unlock()
		s, _ := json.Marshal(llm.reqs[len(llm.reqs)-1]["system"])
		return string(s)
	}
	// On the missions list: the page and the docs section about missions.
	dev.expect(200, "POST", "/v1/assistant/chat", map[string]any{"message": "what is this?", "context": map[string]any{"kind": "page", "path": "/missions", "label": "Team › Missions"}})
	if got := system(); !strings.Contains(got, "Team › Missions (/missions)") || !strings.Contains(got, "Discover") {
		t.Fatalf("system = %s", got)
	}
	// On one mission: where it is, and the mission itself; a missing one is a clear error.
	dev.expect(404, "POST", "/v1/assistant/chat", map[string]any{"message": "what is this mission?", "context": map[string]any{"kind": "mission", "id": "nope", "path": "/missions/nope", "label": "Team › Missions"}})

	// search_docs answers from the guides.
	llm.mu.Lock()
	llm.replies = []string{lookupReply("search_docs", map[string]string{"query": "how do teams split a goal"}), "done"}
	llm.reqs = nil
	llm.mu.Unlock()
	dev.expect(200, "POST", "/v1/assistant/chat", map[string]any{"message": "how do teams work?"})
	if res := llm.lastUser(1); !strings.Contains(res, "The lead plans") {
		t.Fatalf("search_docs result = %s", res)
	}
}
