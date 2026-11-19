package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/agentfile"
)

// Exporting an agent (design note 046, step 1): one fixed version as a Forgeline bundle (agent.yaml and an Agent Skills
// folder per skill) or as a Docker Agent file. Nothing is read from the vault: keys become environment variable names.

func (s *Server) handleExportAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, err := s.loadAgent(ctx, r.PathValue("id"))
	if err != nil {
		s.defError(w, "agent", err)
		return
	}
	n := a.Current
	if q := r.URL.Query().Get("version"); q != "" {
		if n, err = strconv.Atoi(q); err != nil {
			writeError(w, http.StatusBadRequest, "version: a number")
			return
		}
	}
	var v *AgentVersion
	for i := range a.Versions {
		if a.Versions[i].N == n {
			v = &a.Versions[i]
		}
	}
	if v == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("version: %s has no version %d", a.ID, n))
		return
	}
	format := r.URL.Query().Get("format")
	if format != "bundle" && format != "docker" {
		writeError(w, http.StatusBadRequest, `format: "bundle" or "docker"`)
		return
	}
	ex, msg := s.exportable(ctx, a, *v)
	if msg != "" {
		writeError(w, http.StatusConflict, msg)
		return
	}
	var body []byte
	var ctype, name string
	if format == "bundle" {
		body, err = agentfile.Bundle(ex.agent, bundleReadme(ex))
		ctype, name = "application/zip", fmt.Sprintf("%s-v%d.zip", a.ID, v.N)
	} else {
		body, err = agentfile.DockerYAML(ex.agent, ex.model, ex.mcps, ex.left)
		ctype, name = "application/yaml", fmt.Sprintf("%s-v%d-docker-agent.yaml", a.ID, v.N)
	}
	if err != nil {
		s.internalError(w, "exporting the agent", err)
		return
	}
	s.auditReq(r, "ai.agent.export", "agent/"+a.ID, fmt.Sprintf("v%d as %s", v.N, format), true)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

type agentExport struct {
	agent agentfile.Agent
	model agentfile.ModelSource
	mcps  []agentfile.RemoteMCP
	left  []agentfile.LeftOut
}

// exportable gathers what a version is made of: its pinned skills' text, its model's provider, and its tools sorted
// into remote MCP servers a copy can call and the rest, which need Forgeline (note 046, decision 3).
func (s *Server) exportable(ctx context.Context, a AIAgent, v AgentVersion) (agentExport, string) {
	ex := agentExport{agent: agentfile.Agent{ID: a.ID, Name: a.Name, Description: a.Description, Instructions: v.Instructions, Tier: v.Tier,
		Model: v.Model, Fingerprint: v.Hash, Version: v.N, Tools: v.Tools, At: time.Now()}}
	for _, pin := range v.Skills {
		sk, err := s.loadSkill(ctx, pin.ID)
		if err != nil {
			return ex, fmt.Sprintf("skill %q is gone; pin another version of the agent", pin.ID)
		}
		found := false
		for _, sv := range sk.Versions {
			if sv.N == pin.N {
				ex.agent.Skills = append(ex.agent.Skills, agentfile.SkillDoc{ID: sk.ID, Title: sk.Name, Description: sk.Description, Body: sv.Body, Version: sv.N})
				found = true
			}
		}
		if !found {
			return ex, fmt.Sprintf("skill %q has no version %d", pin.ID, pin.N)
		}
	}
	if prov, model, ok := splitModelRef(v.Model); ok {
		p, err := s.loadProvider(ctx, prov)
		if err != nil {
			return ex, fmt.Sprintf("model: there is no provider %q any more; pick another model first", prov)
		}
		kind := p.Kind
		if kind == provOpenAI && p.BaseURL != "" && p.BaseURL != defaultOpenAI {
			kind = provOpenAICompat
		}
		ex.model = agentfile.ModelSource{ProviderID: p.ID, Kind: kind, BaseURL: p.BaseURL, Model: model}
	} else {
		ex.model = agentfile.ModelSource{Model: v.Model}
	}

	servers, _ := s.listMCPServers(ctx)
	byTool := map[string]struct {
		srv  MCPServer
		tool string
	}{}
	for _, srv := range servers {
		for _, t := range srv.Tools {
			byTool[mcpToolName(srv.ID, t.Name)] = struct {
				srv  MCPServer
				tool string
			}{srv, t.Name}
		}
	}
	remote := map[string]*agentfile.RemoteMCP{}
	for _, name := range v.Tools {
		switch m, ok := byTool[name]; {
		case ok && m.srv.Kind == "remote" && m.srv.URL != "":
			if remote[m.srv.ID] == nil {
				remote[m.srv.ID] = &agentfile.RemoteMCP{ID: m.srv.ID, URL: m.srv.URL, AuthHeader: m.srv.AuthHeader, AuthScheme: m.srv.AuthScheme, AuthSecret: m.srv.AuthSecret}
			}
			remote[m.srv.ID].Tools = append(remote[m.srv.ID].Tools, m.tool)
		case ok:
			ex.left = append(ex.left, agentfile.LeftOut{Tool: name, Why: "an MCP server that runs as a container on the Forgeline server"})
		case strings.HasPrefix(name, mcpToolPrefix):
			ex.left = append(ex.left, agentfile.LeftOut{Tool: name, Why: "its MCP server or tool is no longer connected"})
		case strings.HasPrefix(name, pluginToolPrefix):
			ex.left = append(ex.left, agentfile.LeftOut{Tool: name, Why: "a Forgeline plugin, which runs on the Forgeline server"})
		case strings.HasPrefix(name, "propose_") || name == "request_environment" || name == "prepare_action":
			ex.left = append(ex.left, agentfile.LeftOut{Tool: name, Why: "it asks for a change, which only Forgeline's approval can carry out"})
		default:
			ex.left = append(ex.left, agentfile.LeftOut{Tool: name, Why: "a Forgeline lookup, which needs the server"})
		}
	}
	for _, m := range remote {
		ex.mcps = append(ex.mcps, *m)
	}
	sort.Slice(ex.mcps, func(i, j int) bool { return ex.mcps[i].ID < ex.mcps[j].ID })
	return ex, ""
}

// bundleReadme says what a bundle holds, what a copy outside Forgeline cannot do, and how to use the parts.
func bundleReadme(ex agentExport) string {
	a := ex.agent
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nVersion %d of a Forgeline agent, exported %s. Fingerprint `%s`.\n\n", a.Name, a.Version, a.At.UTC().Format("2006-01-02"), a.Fingerprint)
	if a.Description != "" {
		b.WriteString(a.Description + "\n\n")
	}
	b.WriteString("## What is here\n\n- `agent.yaml`: the agent as Forgeline keeps it: instructions, model, tier, tools and skills. Its schema is\n  `" + agentfile.SchemaFile + "` in Forgeline's repository.\n")
	if len(a.Skills) > 0 {
		b.WriteString("- `skills/`: each skill it follows, as an Agent Skills folder (agentskills.io). Claude Code, Copilot, Cursor and\n  other tools read them as they are; copy a folder into, for example, `.claude/skills/` or `.agents/skills/`.\n")
	}
	b.WriteString("\nNo key or secret is in this bundle.\n\n## Outside Forgeline\n\n")
	b.WriteString("Download the Docker Agent file from the agent's page to run it with `docker agent run`. A copy outside Forgeline\ncan only read: ")
	if len(ex.mcps) > 0 {
		var ids []string
		for _, m := range ex.mcps {
			ids = append(ids, m.ID)
		}
		b.WriteString("it keeps the tools of its remote MCP servers (" + strings.Join(ids, ", ") + ") ")
	} else {
		b.WriteString("it has no tools ")
	}
	b.WriteString("and answers from what it is told.\n")
	if len(ex.left) > 0 {
		b.WriteString("\nThese need Forgeline and are not in a copy:\n\n")
		for _, l := range ex.left {
			fmt.Fprintf(&b, "- `%s`: %s\n", l.Tool, l.Why)
		}
	}
	return b.String()
}
