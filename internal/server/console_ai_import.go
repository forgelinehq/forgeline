package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/forgelinehq/forgeline/internal/agentfile"
	"github.com/forgelinehq/forgeline/internal/docstore"
)

// Importing an agent (design note 046, step 2): a Forgeline bundle becomes a new agent, or a new version of the agent
// with the same name, through the same checks as the builder. Skills are matched by their text and created when this
// server has none with that text; tools this server does not have are named and dropped. A dry run says all of that
// and saves nothing, so the console can show it before anyone confirms.

type importRequest struct {
	Bundle []byte `json:"bundle"` // the zip, base64 in JSON
	DryRun bool   `json:"dry_run"`
}

// importSkill is what happens to one of the bundle's skills: "use" an existing skill with the same text, or "create" it.
type importSkill struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Action  string `json:"action"`
	ID      string `json:"id,omitempty"`
	Version int    `json:"version,omitempty"`
	Edited  bool   `json:"edited,omitempty"` // changed after export: its text does not match agent.yaml's sha256
}

type importDrop struct {
	Tool string `json:"tool"`
	Why  string `json:"why"`
}

// importPlan is what an import does (dry run) or did.
type importPlan struct {
	Name    string        `json:"name"`
	Action  string        `json:"action"` // create, version or unchanged
	AgentID string        `json:"agent_id,omitempty"`
	From    string        `json:"from"`
	Tier    string        `json:"tier"`
	Model   string        `json:"model"`
	Tools   []string      `json:"tools"`
	Skills  []importSkill `json:"skills"`
	Dropped []importDrop  `json:"dropped"`
	Notes   []string      `json:"notes"`
	Agent   *AIAgent      `json:"agent,omitempty"`
}

func (s *Server) handleImportAgent(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Bundle) == 0 {
		writeError(w, http.StatusBadRequest, "bundle: the zip, base64 encoded")
		return
	}
	f, read, err := agentfile.ReadBundle(req.Bundle)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bundle: "+err.Error())
		return
	}
	ctx := r.Context()
	plan, areq, creates, existing, msg := s.planImport(ctx, f, read)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if req.DryRun || plan.Action == "unchanged" {
		if plan.Action == "unchanged" {
			plan.Agent = existing
		}
		writeJSON(w, http.StatusOK, plan)
		return
	}

	p, ip := principalFrom(ctx), clientIP(r)
	for i, c := range creates {
		sk, err := s.saveNewSkill(ctx, p, ip, c.req, " (imported)")
		if err != nil {
			s.internalError(w, "saving a skill", err)
			return
		}
		plan.Skills[c.at].ID, plan.Skills[c.at].Version = sk.ID, sk.Current
		areq.Skills[c.at] = SkillPin{ID: sk.ID, N: sk.Current}
		creates[i].id = sk.ID
	}
	v, msg := s.resolveAgent(ctx, areq)
	if msg != "" {
		// The plan checked everything before the skills were saved, so this is a change made meanwhile.
		writeError(w, http.StatusConflict, msg)
		return
	}
	var a AIAgent
	if existing != nil {
		var status int
		if a, status, msg = s.appendAgentVersion(ctx, p, ip, *existing, areq, v, " (imported)"); msg != "" {
			writeError(w, status, msg)
			return
		}
	} else if a, err = s.saveNewAgent(ctx, p, ip, areq, v, " (imported)"); err != nil {
		s.internalError(w, "saving the agent", err)
		return
	}
	plan.AgentID, plan.Agent = a.ID, &a
	s.audit(ctx, p, ip, "ai.agent.import", "agent/"+a.ID, fmt.Sprintf("%s as v%d (%s), %d skill(s) created, %d tool(s) dropped", plan.From, a.Current, plan.Action, len(creates), len(plan.Dropped)), true)
	writeJSON(w, http.StatusCreated, plan)
}

type skillCreate struct {
	at  int // its place in the agent's skills
	req skillRequest
	id  string
}

// planImport works out what an import does without saving anything: the agent request it makes, the skills it creates,
// and the agent it adds a version to (nil for a new one). A message means the bundle cannot be imported here.
func (s *Server) planImport(ctx context.Context, f agentfile.File, read []agentfile.ReadSkill) (importPlan, agentRequest, []skillCreate, *AIAgent, string) {
	from := fmt.Sprintf("%s v%d (fingerprint %.12s)", f.Source.Agent, f.Source.Version, f.Source.Fingerprint)
	plan := importPlan{Name: strings.TrimSpace(f.Name), From: from, Tier: f.Tier, Model: f.Model, Tools: []string{}, Skills: []importSkill{}, Dropped: []importDrop{}, Notes: []string{}}
	areq := agentRequest{Name: plan.Name, Description: f.Description, Instructions: f.Instructions, Tier: f.Tier, Model: f.Model, Note: truncate("imported from "+from, maxNote)}
	if len(read) > maxAgentSkills {
		return plan, areq, nil, nil, fmt.Sprintf("skills: the bundle has %d; an agent can use at most %d", len(read), maxAgentSkills)
	}

	// Skills, by their text: the newest version of any skill whose text is the same, or a new skill.
	docs, err := s.docs.List(ctx, kindAISkill, docstore.ListOptions{})
	if err != nil {
		return plan, areq, nil, nil, "internal error: reading the skills"
	}
	all, _ := docstore.Decode[AISkill](docs)
	var creates []skillCreate
	for i, rs := range read {
		is := importSkill{Name: rs.Name, Title: rs.Title, Action: "create", Edited: rs.Edited}
		if is.Title == "" {
			is.Title = rs.Name
		}
		for _, sk := range all {
			for _, sv := range sk.Versions {
				if strings.TrimSpace(sv.Body)+"\n" == rs.Body && (is.Action == "create" || sv.N > is.Version && sk.ID == is.ID) {
					is.Action, is.ID, is.Version = "use", sk.ID, sv.N
				}
			}
		}
		pin := SkillPin{ID: is.ID, N: is.Version}
		if is.Action == "create" {
			desc := rs.Description
			if len(desc) > maxDefDesc {
				desc = desc[:maxDefDesc-3] + "..."
			}
			sreq := skillRequest{Name: is.Title, Description: desc, Body: rs.Body, Note: truncate("imported from "+from, maxNote)}
			if m := sreq.check(); m != "" {
				return plan, areq, nil, nil, fmt.Sprintf("skills[%d] (%s): %s", i, rs.Name, m)
			}
			creates = append(creates, skillCreate{at: i, req: sreq})
		} else {
			for _, p := range areq.Skills {
				if p.ID == is.ID {
					return plan, areq, nil, nil, fmt.Sprintf("skills[%d] (%s): the same text as another skill in the bundle", i, rs.Name)
				}
			}
		}
		if rs.Edited {
			plan.Notes = append(plan.Notes, fmt.Sprintf("The skill %s was changed after it was exported; the changed text is what is imported.", rs.Name))
		}
		plan.Skills = append(plan.Skills, is)
		areq.Skills = append(areq.Skills, pin)
	}

	// The model: kept when this server has it, otherwise the agent uses the assistant's model.
	if f.Model != "" {
		bad := ""
		if strings.Contains(f.Model, "/") {
			bad = s.checkModelRef(ctx, f.Model)
		} else if !agentModelRE.MatchString(f.Model) {
			bad = "not a model name"
		}
		if bad != "" {
			plan.Notes = append(plan.Notes, fmt.Sprintf("The model %s is not available here (%s), so it uses the assistant's model. Pick one in the builder afterwards.", f.Model, strings.TrimSuffix(bad, ".")))
			plan.Model, areq.Model = "", ""
		}
	}

	// Tools: kept when this server has them and the tier allows them.
	known := map[string]bool{listAgentsTool: true}
	for _, t := range assistantLookups() {
		known[t.name] = true
	}
	for _, n := range append(s.pluginToolNames(ctx), s.extMCPToolNames(ctx)...) {
		known[n] = true
	}
	for _, t := range f.Tools {
		why := ""
		switch {
		case t == askAgentTool:
			why = "an agent cannot call other agents"
		case known[t] && isProposalTool(t) && f.Tier != tierReview:
			why = "only a T1 agent may file proposals"
		case known[t]:
			if !contains(areq.Tools, t) {
				areq.Tools = append(areq.Tools, t)
			}
			continue
		case strings.HasPrefix(t, mcpToolPrefix):
			why = "no MCP server here has this tool; connect the server, then add the tool in the builder"
		case strings.HasPrefix(t, pluginToolPrefix):
			why = "no plugin here has this tool"
		default:
			why = "this server has no such tool"
		}
		plan.Dropped = append(plan.Dropped, importDrop{Tool: t, Why: why})
	}
	plan.Tools = append(plan.Tools, areq.Tools...)

	// The agent with the same name gets a new version; otherwise it is a new agent.
	agents, err := s.listAgents(ctx)
	if err != nil {
		return plan, areq, nil, nil, "internal error: reading the agents"
	}
	var existing *AIAgent
	for i := range agents {
		if !agents[i].archived() && strings.EqualFold(strings.TrimSpace(agents[i].Name), plan.Name) {
			existing = &agents[i]
			break
		}
	}

	// Check the agent as the builder would, with the skills that already exist (new ones are checked above).
	check := areq
	check.Skills = nil
	for i, p := range areq.Skills {
		if plan.Skills[i].Action == "use" {
			check.Skills = append(check.Skills, p)
		}
	}
	v, msg := s.resolveAgent(ctx, check)
	if msg != "" {
		return plan, areq, nil, nil, msg
	}
	plan.Action = "create"
	if existing != nil {
		plan.Action, plan.AgentID = "version", existing.ID
		if len(creates) == 0 {
			v.Skills = areq.Skills
			last := existing.Versions[len(existing.Versions)-1]
			if sameVersionContent(last, v) && existing.Name == plan.Name && existing.Description == areq.Description {
				plan.Action = "unchanged"
			}
		}
		if plan.Action == "version" && len(existing.Versions) >= maxVersions {
			return plan, areq, nil, nil, fmt.Sprintf("the agent %q keeps at most %d versions", existing.Name, maxVersions)
		}
	}
	return plan, areq, creates, existing, ""
}
