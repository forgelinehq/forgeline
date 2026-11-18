package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// What the assistant knows about Forgeline itself. The guides under docs/ are copied into knowledge/ (a test keeps the
// copies in step: FORGELINE_UPDATE_KNOWLEDGE=1 go test ./internal/server -run TestKnowledgeMatchesTheDocs) and built
// into the binary, split into sections by heading. search_docs finds the sections that match a question, and the page
// the person is on brings the section about that page, so the assistant answers from the documentation instead of
// guessing. The guides are trusted text written by the project, like the assistant's own instructions.

//go:embed knowledge/*.md
var knowledgeFS embed.FS

type docSection struct {
	Guide, Heading, Text string
	words                map[string]int
	size                 int
}

var (
	knowledgeOnce sync.Once
	knowledge     []docSection
	wordRE        = regexp.MustCompile(`[a-z0-9][a-z0-9-]*`)
	imageLineRE   = regexp.MustCompile(`(?m)^!\[[^\]]*\]\([^)]*\)\s*$`)
	liquidLinkRE  = regexp.MustCompile(`\(\{\{ '([^']*)' \| relative_url \}\}([^)]*)\)`)
)

// stop words are left out of matching; they say nothing about which section fits.
var docStopWords = map[string]bool{"the": true, "and": true, "for": true, "are": true, "was": true, "this": true, "that": true, "with": true,
	"what": true, "how": true, "can": true, "does": true, "you": true, "your": true, "from": true, "about": true, "into": true,
	"its": true, "has": true, "have": true, "not": true, "but": true, "all": true, "any": true, "one": true, "get": true,
	"use": true, "used": true, "when": true, "where": true, "which": true, "who": true, "why": true, "there": true, "then": true,
	"them": true, "they": true, "here": true, "page": true, "forgeline": true, "please": true, "tell": true, "show": true}

func loadKnowledge() []docSection {
	knowledgeOnce.Do(func() {
		files, _ := knowledgeFS.ReadDir("knowledge")
		for _, f := range files {
			raw, err := knowledgeFS.ReadFile("knowledge/" + f.Name())
			if err != nil {
				continue
			}
			knowledge = append(knowledge, splitGuide(string(raw))...)
		}
	})
	return knowledge
}

// splitGuide cuts a guide into sections at "## " headings, without front matter, pictures or site link syntax.
func splitGuide(src string) []docSection {
	if strings.HasPrefix(src, "---\n") {
		if end := strings.Index(src[4:], "\n---\n"); end >= 0 {
			src = src[4+end+5:]
		}
	}
	src = imageLineRE.ReplaceAllString(src, "")
	src = liquidLinkRE.ReplaceAllString(src, "($1$2)")
	guide := ""
	var out []docSection
	cur := docSection{}
	flush := func() {
		cur.Text = strings.TrimSpace(cur.Text)
		if cur.Text != "" {
			cur.Guide = guide
			out = append(out, cur)
		}
	}
	parent := ""
	for _, line := range strings.Split(src, "\n") {
		switch {
		case strings.HasPrefix(line, "# ") && guide == "":
			guide = strings.TrimSpace(line[2:])
		case strings.HasPrefix(line, "## "):
			flush()
			parent = strings.TrimSpace(line[3:])
			cur = docSection{Heading: parent}
		case strings.HasPrefix(line, "### "):
			flush()
			cur = docSection{Heading: strings.TrimSpace(line[4:])}
			if parent != "" {
				cur.Heading = parent + " › " + cur.Heading
			}
		default:
			cur.Text += line + "\n"
		}
	}
	flush()
	for i := range out {
		out[i].words = map[string]int{}
		for _, w := range docWords(out[i].Guide + " " + out[i].Heading + " " + out[i].Text) {
			out[i].words[w]++
		}
		out[i].size = len(out[i].Text)
		if out[i].Heading == "" {
			out[i].Heading = "Overview"
		}
	}
	return out
}

func docWords(s string) []string {
	var out []string
	for _, w := range wordRE.FindAllString(strings.ToLower(s), -1) {
		w = strings.TrimSuffix(w, "s") // missions and mission, teams and team
		if len(w) >= 3 && !docStopWords[w] {
			out = append(out, w)
		}
	}
	return out
}

// searchDocs returns the sections that best match a question, best first.
func searchDocs(query string, n int) []docSection {
	terms := docWords(query)
	if len(terms) == 0 {
		return nil
	}
	type scored struct {
		s     docSection
		score float64
	}
	var all []scored
	for _, sec := range loadKnowledge() {
		head := map[string]bool{}
		for _, w := range docWords(sec.Guide + " " + sec.Heading) {
			head[w] = true
		}
		score, hit := 0.0, 0
		for _, t := range terms {
			c := sec.words[t]
			if c == 0 {
				continue
			}
			hit++
			score += math.Min(float64(c), 6)
			if head[t] {
				score += 6
			}
		}
		if hit == 0 {
			continue
		}
		score *= float64(hit) / float64(len(terms)) // sections that match more of the question first
		score /= 1 + math.Log1p(float64(sec.size)/4000)
		all = append(all, scored{sec, score})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	var out []docSection
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, all[i].s)
	}
	return out
}

func formatSections(secs []docSection, each int) string {
	var b strings.Builder
	for _, s := range secs {
		fmt.Fprintf(&b, "### %s › %s\n%s\n\n", s.Guide, s.Heading, truncate(s.Text, each))
	}
	return b.String()
}

func (s *Server) lookupSearchDocs(_ context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Query string `json:"query"`
	}
	if json.Unmarshal(raw, &a) != nil || strings.TrimSpace(a.Query) == "" {
		return "", errors.New(`give a query, such as {"query":"how do teams work"}`)
	}
	secs := searchDocs(a.Query, 3)
	if len(secs) == 0 {
		return "The documentation has nothing that matches. Say so rather than guess.", nil
	}
	return "From Forgeline's documentation:\n\n" + formatSections(secs, 3500), nil
}

// --- teams, workflows, missions and models, for the assistant ---

func (s *Server) lookupListTeams(ctx context.Context, _ json.RawMessage) (string, error) {
	ts, err := s.listTeams(ctx)
	if err != nil {
		return "", errors.New("could not read the teams")
	}
	if len(ts) == 0 {
		return "There are no teams. An administrator makes one under Team › Teams, or installs one from the library.", nil
	}
	var b strings.Builder
	for _, t := range ts {
		fmt.Fprintf(&b, "- id %q: %s. Lead %s, members %s. %s", t.ID, t.Name, t.Lead, strings.Join(t.Members, ", "), t.Purpose)
		if runs, _ := s.teamRunList(ctx, t.ID, 1); len(runs) > 0 {
			fmt.Fprintf(&b, " Last goal (%s, %s): %s", runs[0].Status, runs[0].Started.Format("2006-01-02 15:04 UTC"), truncate(runs[0].Goal, 120))
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func (s *Server) lookupGetTeam(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		ID string `json:"id"`
	}
	json.Unmarshal(raw, &a)
	return s.teamSummary(ctx, a.ID)
}

func (s *Server) teamSummary(ctx context.Context, id string) (string, error) {
	t, err := s.loadTeam(ctx, id)
	if err != nil {
		return "", errors.New("no such team")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Team %q (id %s). Purpose: %s\nLead: %s. Members: %s.\n", t.Name, t.ID, t.Purpose, t.Lead, strings.Join(t.Members, ", "))
	runs, _ := s.teamRunList(ctx, t.ID, 3)
	for i, r := range runs {
		fmt.Fprintf(&b, "\nRun %s (%s), goal given by %s on %s: %s\n", r.ID, r.Status, r.By, r.Started.Format("2006-01-02 15:04 UTC"), r.Goal)
		if r.PlanNote != "" {
			fmt.Fprintf(&b, "Plan: %s\n", r.PlanNote)
		}
		for _, tt := range r.Tasks {
			fmt.Fprintf(&b, "- %s (%s): %s", tt.MemberName, tt.Status, tt.Task)
			if i == 0 && tt.Output != "" {
				fmt.Fprintf(&b, "\n  Found: %s", truncate(tt.Output, 700))
			}
			b.WriteByte('\n')
		}
		if r.Answer != "" && i == 0 {
			fmt.Fprintf(&b, "Answer: %s\n", truncate(r.Answer, 1500))
		}
		if r.Error != "" {
			fmt.Fprintf(&b, "Error: %s\n", r.Error)
		}
	}
	return truncate(b.String(), maxAssistContext), nil
}

func (s *Server) lookupListWorkflows(ctx context.Context, _ json.RawMessage) (string, error) {
	wfs, err := s.listWorkflows(ctx)
	if err != nil {
		return "", errors.New("could not read the workflows")
	}
	if len(wfs) == 0 {
		return "There are no workflows.", nil
	}
	var b strings.Builder
	for _, w := range wfs {
		kind := "stages"
		if v, ok := w.version(w.Current); ok && v.Graph != nil {
			kind = fmt.Sprintf("a graph of %d nodes", len(v.Graph.Nodes))
		}
		fmt.Fprintf(&b, "- id %q: %s (%s, version %d, off %v). %s\n", w.ID, w.Name, kind, w.Current, w.Disabled, truncate(w.Description, 160))
	}
	return b.String(), nil
}

func (s *Server) lookupGetWorkflow(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		ID string `json:"id"`
	}
	json.Unmarshal(raw, &a)
	return s.workflowSummary(ctx, a.ID)
}

func (s *Server) workflowSummary(ctx context.Context, id string) (string, error) {
	w, err := s.loadWorkflow(ctx, id)
	if err != nil {
		return "", errors.New("no such workflow")
	}
	v, _ := w.version(w.Current)
	var b strings.Builder
	fmt.Fprintf(&b, "Workflow %q (id %s), version %d, off %v. %s\n", w.Name, w.ID, w.Current, w.Disabled, w.Description)
	if v.Graph != nil {
		b.WriteString("Nodes:\n")
		for _, n := range v.Graph.Nodes {
			fmt.Fprintf(&b, "- %s (%s)", orText(n.Name, n.ID), n.Kind)
			if n.Agent != "" {
				fmt.Fprintf(&b, " agent %s: %s", n.Agent, truncate(n.Task, 160))
			}
			b.WriteByte('\n')
		}
		b.WriteString("Wires:\n")
		for _, e := range v.Graph.Edges {
			fmt.Fprintf(&b, "- %s → %s %s\n", e.From, e.To, e.When)
		}
	}
	for _, st := range v.Stages {
		fmt.Fprintf(&b, "Stage %s:\n", st.Name)
		for _, sp := range st.Steps {
			fmt.Fprintf(&b, "- agent %s: %s\n", sp.Agent, truncate(sp.Task, 160))
		}
	}
	return truncate(b.String(), maxAssistContext), nil
}

func (s *Server) workflowRunSummary(ctx context.Context, id string) (string, error) {
	var r WFRun
	if err := s.docs.Get(ctx, kindWorkflowRun, id, &r); err != nil {
		return "", errors.New("no such workflow run")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Run %s of workflow %q (version %d), %s, started by %s (%s) at %s.\n", r.ID, r.WorkflowName, r.Version, r.Status, r.By, r.Trigger, r.StartedAt.Format("2006-01-02 15:04 UTC"))
	if r.Error != "" {
		fmt.Fprintf(&b, "Error: %s\n", r.Error)
	}
	for _, st := range r.Stages {
		for _, sp := range st.Steps {
			fmt.Fprintf(&b, "- %s / %s (%s): %s %s\n", st.Name, sp.Agent, sp.Status, truncate(sp.Output, 500), sp.Error)
		}
	}
	j, _ := json.Marshal(r)
	if b.Len() < 400 { // a graph run keeps its visits in fields this summary does not walk
		b.WriteString("Record (JSON): " + truncate(string(j), 8<<10))
	}
	return truncate(b.String(), maxAssistContext), nil
}

func (s *Server) lookupListMissions(ctx context.Context, _ json.RawMessage) (string, error) {
	ms, err := s.listMissions(ctx)
	if err != nil {
		return "", errors.New("could not read the missions")
	}
	if len(ms) == 0 {
		return "There are no missions.", nil
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].StartedAt.After(ms[j].StartedAt) })
	var b strings.Builder
	for i, m := range ms {
		if i == 20 {
			break
		}
		fmt.Fprintf(&b, "- id %q: %s, agent %s, %s at stage %s, %d findings, started by %s on %s\n", m.ID, truncate(m.Goal, 140), m.AgentName, m.Status, m.Stage, len(m.Findings), m.By, m.StartedAt.Format("2006-01-02"))
	}
	return b.String(), nil
}

func (s *Server) lookupGetMission(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		ID string `json:"id"`
	}
	json.Unmarshal(raw, &a)
	return s.missionSummary(ctx, a.ID)
}

func (s *Server) missionSummary(ctx context.Context, id string) (string, error) {
	var m Mission
	if err := s.docs.Get(ctx, kindMission, id, &m); err != nil {
		return "", errors.New("no such mission")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Mission %s. Goal: %s\nAgent %s (version %d, may propose %v), AWS account %s, started by %s on %s. Status %s at stage %s.\n",
		m.ID, m.Goal, m.AgentName, m.AgentN, m.MayPropose, m.Account, m.By, m.StartedAt.Format("2006-01-02 15:04 UTC"), m.Status, m.Stage)
	if m.Error != "" {
		fmt.Fprintf(&b, "Error: %s\n", m.Error)
	}
	for _, f := range m.Findings {
		fmt.Fprintf(&b, "- [%s] %s (from %s): %s\n", f.Severity, f.Title, f.Source, truncate(f.Detail, 300))
	}
	if m.Unsupported > 0 {
		fmt.Fprintf(&b, "%d claims were left out because they named no lookup the agent ran.\n", m.Unsupported)
	}
	if len(m.Proposals) > 0 {
		fmt.Fprintf(&b, "Proposals filed: %s\n", strings.Join(m.Proposals, ", "))
	}
	if m.Report != "" {
		fmt.Fprintf(&b, "Report:\n%s\n", truncate(m.Report, 3000))
	}
	return truncate(b.String(), maxAssistContext), nil
}

func (s *Server) lookupListModels(ctx context.Context, _ json.RawMessage) (string, error) {
	ps, err := s.listProviders(ctx)
	if err != nil {
		return "", errors.New("could not read the providers")
	}
	st := s.loadAssistant(ctx)
	var b strings.Builder
	fmt.Fprintf(&b, "The assistant uses: %s %s.\n", st.Backend, st.Model)
	if len(ps) == 0 {
		b.WriteString("No model providers are connected (Settings › Models and providers).\n")
	}
	for _, p := range ps {
		var on []string
		for _, m := range p.Models {
			if m.On {
				on = append(on, p.ID+"/"+m.ID)
			}
		}
		fmt.Fprintf(&b, "- provider %s (%s): models on: %s", p.ID, p.Kind, strings.Join(on, ", "))
		if p.MonthlyUSD > 0 {
			fmt.Fprintf(&b, "; limit $%.2f a month, spent $%.2f", p.MonthlyUSD, s.providerSpend(ctx, p.ID, monthOf(time.Now())).CostUSD)
		}
		if p.Fallback != "" {
			fmt.Fprintf(&b, "; fallback %s", p.Fallback)
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// pageSummary is what the assistant is told about the page the person is on, for pages without data of their own:
// where it is, and what the documentation says about it.
func pageSummary(label, path string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The person is on the page %s (%s).\n", label, path)
	if secs := searchDocs(label, 2); len(secs) > 0 {
		b.WriteString("What the documentation says about it:\n\n" + formatSections(secs, 2500))
	}
	return b.String()
}
