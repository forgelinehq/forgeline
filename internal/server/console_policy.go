package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/docstore"
)

// Access policy for integration tools (design note 026). A ceiling per connection: each tool that reaches an
// integration is Allow (the default, and today's behaviour) or Blocked. Blocked beats everything, and a tool
// that asks for a change can never be Allow, because a change is always a proposal that a different
// administrator approves. Every way to run a tool passes through policyGate, so a blocked tool never reaches
// a command line or a proxy.

const (
	idPolicy      = "access-policy"
	polAllow      = "allow"
	polBlock      = "block"
	polAsk        = "ask" // a read tool that needs a person present to approve each call
	toolKindRead  = "read"
	toolKindWrite = "write"
)

type accessPolicy struct {
	Tools    map[string]string   `json:"tools"`              // block or ask; a tool with no entry is allowed
	Services map[string][]string `json:"services,omitempty"` // per integration, the services turned off for the AI (note 030)
	// ServicesRead lists, per integration, the services the AI may read but never propose a change to.
	ServicesRead map[string][]string `json:"services_read,omitempty"`
	UpdatedAt    time.Time           `json:"updated_at"`
	UpdatedBy    string              `json:"updated_by"`
}

// integrationOfTool says which integration a tool reaches, or "" for Forgeline's own tools (runs, pipelines,
// audit, agents), which are not part of any connection and have no policy.
func integrationOfTool(name string) string {
	switch {
	case name == toolProposeCloud || name == "aws_summary" || name == "aws_resources" || name == "aws_accounts" || name == "call_aws":
		return "aws"
	case strings.HasPrefix(name, "k8s_"):
		return "kubernetes"
	case name == "gcp_resources":
		return "gcp"
	case name == "azure_resources":
		return "azure"
	case needsMCP(name):
		return "aws-mcp"
	}
	return ""
}

// isWriteTool marks the tools that ask for a change. They file proposals and can never be Allow.
func isWriteTool(name string) bool { return name == toolProposeAWS || name == toolProposeCloud }

// loadPolicy reads the policy. A policy that was never set is empty (everything allowed); one that cannot be
// read is an error, and the gate fails closed on it.
func (s *Server) loadPolicy(ctx context.Context) (accessPolicy, error) {
	var p accessPolicy
	err := s.docs.Get(ctx, kindSettings, idPolicy, &p)
	if errors.Is(err, docstore.ErrNotFound) {
		return accessPolicy{Tools: map[string]string{}}, nil
	}
	if err != nil {
		return accessPolicy{}, err
	}
	if p.Tools == nil {
		p.Tools = map[string]string{}
	}
	return p, nil
}

// policyDecision is the one place the rule lives: what the ceiling says about a tool.
func (s *Server) policyDecision(ctx context.Context, tool string) (level, why string) {
	integ := integrationOfTool(tool)
	if integ == "" {
		return polAllow, ""
	}
	p, err := s.loadPolicy(ctx)
	if err != nil {
		return polBlock, "the access policy could not be read, so " + tool + " is refused until it can"
	}
	switch p.Tools[tool] {
	case polBlock:
		return polBlock, tool + " is blocked by the access policy for " + integrationLabel(integ)
	case polAsk:
		if !isWriteTool(tool) { // a change is always a proposal; "ask" is how it already works
			return polAsk, tool + " needs approval under the access policy for " + integrationLabel(integ)
		}
	}
	return polAllow, ""
}

func integrationLabel(id string) string {
	return map[string]string{"aws": "AWS", "aws-mcp": "the AWS MCP Server", "kubernetes": "Kubernetes", "gcp": "Google Cloud", "azure": "Azure"}[id]
}

// policyGate is called before any integration tool runs. A refusal is audited against the integration, so its
// Activity tab shows it. A tool that needs approval waits for the person who asked, if one is present (the
// console's assistant stream); anywhere nobody is present (an agent, an MCP client, a plain chat request) it is
// refused, and the refusal says so.
func (s *Server) policyGate(ctx context.Context, p Principal, c lookupCall) error {
	level, why := s.policyDecision(ctx, c.Tool)
	ip, _ := ctx.Value(mcpIPKey{}).(string)
	target := "integration/" + integrationOfTool(c.Tool)
	switch level {
	case polBlock:
		_ = s.audit(ctx, p, ip, "policy.refused", target, why, false)
		return errors.New(why)
	case polAsk:
		ap := approverFrom(ctx)
		if ap == nil {
			why += ", and nobody is present to approve it here (an agent, an MCP client or a plain request has no one)"
			_ = s.audit(ctx, p, ip, "policy.refused", target, why, false)
			return errors.New(why)
		}
		if err := ap(c); err != nil {
			_ = s.audit(ctx, p, ip, "policy.approval", target, c.Tool+": "+err.Error(), false)
			return err
		}
		_ = s.audit(ctx, p, ip, "policy.approval", target, c.Tool+": approved by "+p.Name, true)
	}
	return nil
}

// blockedTools is the set of integration tools the caller of this context cannot use, read once, for lists that
// show the model or an MCP caller what it may use. A tool that needs approval is hidden where nobody is present
// to approve it. If the policy cannot be read every integration tool is hidden.
func (s *Server) blockedTools(ctx context.Context) func(tool string) bool {
	p, err := s.loadPolicy(ctx)
	present := approverFrom(ctx) != nil
	return func(tool string) bool {
		if integrationOfTool(tool) == "" {
			return false
		}
		if err != nil || p.Tools[tool] == polBlock {
			return true
		}
		return p.Tools[tool] == polAsk && !isWriteTool(tool) && !present
	}
}

// --- the API ---

// PolicyToolJSON is one tool of an integration and its level.
type PolicyToolJSON struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`  // read or write
	Level   string `json:"level"` // allow or block; a write tool that is not blocked is a proposal
	MinRole string `json:"min_role"`
	Help    string `json:"help"`
}

// PolicyAgentToolJSON says what one agent can do with one tool, and why.
type PolicyAgentToolJSON struct {
	Tool   string `json:"tool"`
	State  string `json:"state"` // allowed, blocked or not_listed
	Reason string `json:"reason"`
}

// PolicyAgentJSON is one agent's effective access to the integration's tools.
type PolicyAgentJSON struct {
	ID    string                `json:"id"`
	Name  string                `json:"name"`
	Tier  string                `json:"tier"`
	Tools []PolicyAgentToolJSON `json:"tools"`
}

// PolicyJSON is an integration's policy and the effective access it gives.
type PolicyJSON struct {
	Integration string                 `json:"integration"`
	Tools       []PolicyToolJSON       `json:"tools"`
	Agents      []PolicyAgentJSON      `json:"agents"`
	UpdatedBy   string                 `json:"updated_by,omitempty"`
	Readable    bool                   `json:"readable"`           // false when the stored policy cannot be read: every tool is then refused
	Services    []AWSServiceSwitchJSON `json:"services,omitempty"` // AWS only: which services the AI may use (note 030)
}

func toolHelp(h string) string {
	if strings.HasPrefix(h, "{") {
		if i := strings.Index(h, "} "); i > 0 {
			h = h[i+2:]
		}
	}
	return truncate(h, 140)
}

// policyTools lists the tools that reach an integration.
func policyTools(integration string) []PolicyToolJSON {
	var out []PolicyToolJSON
	for _, t := range assistantLookups() {
		if integrationOfTool(t.name) == integration {
			k := toolKindRead
			if isWriteTool(t.name) {
				k = toolKindWrite
			}
			out = append(out, PolicyToolJSON{Name: t.name, Kind: k, MinRole: string(t.min), Help: toolHelp(t.help)})
		}
	}
	if integration == "aws" { // reached only over MCP, so it is not in assistantLookups
		out = append(out, PolicyToolJSON{Name: toolProposeCloud, Kind: toolKindWrite, MinRole: string(authn.Developer),
			Help: "files a change proposal for a stack deploy, stack deletion or instance start, stop or reboot (used by AI tools that connect over MCP)"})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Server) policyView(ctx context.Context, integration string) PolicyJSON {
	p, perr := s.loadPolicy(ctx)
	v := PolicyJSON{Integration: integration, Tools: policyTools(integration), Agents: []PolicyAgentJSON{}, Readable: perr == nil, UpdatedBy: p.UpdatedBy}
	if integration == "aws" && perr == nil {
		v.Services = servicesView(p)
	}
	for i := range v.Tools {
		v.Tools[i].Level = polAllow
		switch {
		case perr != nil || p.Tools[v.Tools[i].Name] == polBlock:
			v.Tools[i].Level = polBlock
		case p.Tools[v.Tools[i].Name] == polAsk && v.Tools[i].Kind == toolKindRead:
			v.Tools[i].Level = polAsk
		}
	}
	agents, err := s.listAgents(ctx)
	if err != nil {
		return v
	}
	for _, a := range agents {
		if a.Disabled {
			continue
		}
		cur, _ := a.version(a.Current)
		has := map[string]bool{}
		for _, t := range cur.Tools {
			has[t] = true
		}
		pa := PolicyAgentJSON{ID: a.ID, Name: a.Name, Tier: cur.Tier, Tools: []PolicyAgentToolJSON{}}
		for _, t := range v.Tools {
			x := PolicyAgentToolJSON{Tool: t.Name}
			switch {
			case t.Level == polBlock:
				x.State, x.Reason = "blocked", "Blocked by the access policy for this connection"
			case !has[t.Name]:
				x.State, x.Reason = "not_listed", "Not in this agent's tool list"
			case t.Level == polAsk:
				x.State, x.Reason = "needs_person", "Needs a person to approve each call, so it is refused in an agent run where nobody is present"
			case t.Kind == toolKindWrite:
				x.State, x.Reason = "allowed", "Allowed, as a proposal that a different administrator approves"
			default:
				x.State, x.Reason = "allowed", "In this agent's tool list, and not blocked"
			}
			pa.Tools = append(pa.Tools, x)
		}
		v.Agents = append(v.Agents, pa)
	}
	return v
}

func (s *Server) handleGetPolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if len(policyTools(id)) == 0 {
		writeError(w, http.StatusNotFound, "that integration has no tools to set a policy for")
		return
	}
	writeJSON(w, http.StatusOK, s.policyView(r.Context(), id))
}

func (s *Server) handlePutPolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tools := policyTools(id)
	if len(tools) == 0 {
		writeError(w, http.StatusNotFound, "that integration has no tools to set a policy for")
		return
	}
	var req struct {
		Tools map[string]string `json:"tools"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	known := map[string]PolicyToolJSON{}
	for _, t := range tools {
		known[t.Name] = t
	}
	for name, level := range req.Tools {
		t, ok := known[name]
		switch {
		case !ok:
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%q is not a tool of %s", name, integrationLabel(id)))
			return
		case level != polAllow && level != polBlock && level != polAsk:
			writeError(w, http.StatusBadRequest, fmt.Sprintf("the level of %s must be allow, ask or block", name))
			return
		case t.Kind == toolKindWrite && level == polAllow:
			writeError(w, http.StatusBadRequest, name+" asks for a change, so it is always a proposal that a different administrator approves; it can be blocked, but not set to allow")
			return
		}
	}
	// The policy is read before it is changed. If it cannot be read it is not overwritten blindly.
	cur, err := s.loadPolicy(r.Context())
	if err != nil {
		writeError(w, http.StatusConflict, "the stored access policy cannot be read, so it will not be changed over; fix or remove it first")
		return
	}
	before := stateOf(cur, id)
	for _, t := range tools { // this integration's tools are set from the request; others are left alone
		delete(cur.Tools, t.Name)
	}
	for name, level := range req.Tools {
		if level == polBlock || level == polAsk && known[name].Kind == toolKindRead { // a write tool's "ask" is what it already is
			cur.Tools[name] = level
		}
	}
	cur.UpdatedAt, cur.UpdatedBy = time.Now().UTC(), principalFrom(r.Context()).Name
	if err := s.docs.Put(r.Context(), kindSettings, idPolicy, cur); err != nil {
		s.internalError(w, "saving the policy", err)
		return
	}
	s.auditReq(r, "policy.update", "integration/"+id, fmt.Sprintf("blocked: [%s], needs approval: [%s] (was blocked [%s], needs approval [%s])", strings.Join(stateOf(cur, id)[0], ", "), strings.Join(stateOf(cur, id)[1], ", "), strings.Join(before[0], ", "), strings.Join(before[1], ", ")), true)
	writeJSON(w, http.StatusOK, s.policyView(r.Context(), id))
}

func blockedOf(p accessPolicy, integration string) []string { return stateOf(p, integration)[0] }

// stateOf lists an integration's blocked tools and its tools that need approval.
func stateOf(p accessPolicy, integration string) [2][]string {
	var out [2][]string
	for _, t := range policyTools(integration) {
		switch {
		case p.Tools[t.Name] == polBlock:
			out[0] = append(out[0], t.Name)
		case p.Tools[t.Name] == polAsk && t.Kind == toolKindRead:
			out[1] = append(out[1], t.Name)
		}
	}
	sort.Strings(out[0])
	sort.Strings(out[1])
	return out
}
