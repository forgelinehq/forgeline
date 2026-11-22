package server

import "context"

// The approval gate for autopilot (design note 052). Every place that would ask the person (an AWS change, an agent's own tool,
// an ACP agent's permission request, a card that saves something) asks this one function whether autopilot approves it. What
// autopilot covers is decided here and nowhere else, so a new assistant, agent or approval point gets the same behaviour by
// calling it, and a change to the policy is one change.

type gateKind string

const (
	gateAWSChange      gateKind = "aws_change"      // a change to AWS that the permission check left as a plain ask
	gateAgentTool      gateKind = "agent_tool"      // an agent's own Bash, Edit, Write or fetch (Claude Code or an ACP agent)
	gateSaveDefinition gateKind = "save_definition" // a card that saves a pipeline, skill, agent or workflow
	gateRunPipeline    gateKind = "run_pipeline"    // a card that starts a pipeline run
	gateCapability     gateKind = "capability"      // a card that turns on the sandbox or AWS changes
	gateSecret         gateKind = "secret"          // a vault secret handed to a command
	gateForgelineWrite gateKind = "forgeline_write" // a change to Forgeline itself through the console API (agents, skills, teams, workflows, pipelines, memory)
)

// autopilotCovers is the whole policy. Running a pipeline executes code, a capability card widens what autopilot itself may do,
// and a secret leaves the vault, so those always wait for the person. Protected AWS accounts, rules that deny or ask, and the
// services that always ask never reach the gate: the permission check settles them first.
var autopilotCovers = map[gateKind]bool{gateAWSChange: true, gateAgentTool: true, gateSaveDefinition: true, gateForgelineWrite: true}

// gateRequest is what is about to be approved: the kind, the tool's name, what it will do in a line, and the account if any.
type gateRequest struct {
	Kind    gateKind
	Tool    string
	Detail  string
	Account string
}

// autopilotApproves says whether autopilot approves this request without a card, and records it when it does: the step is
// counted, the audit log says autopilot did it, and the person sees it in the chat. The autopilot is the one in force now in
// this conversation, including one switched on while the message was being worked on.
func (s *Server) autopilotApproves(ctx context.Context, p Principal, g gateRequest) bool {
	if !autopilotCovers[g.Kind] {
		return false
	}
	if g.Kind == gateAgentTool && !s.engineInContainer(ctx) {
		return false // on the server itself a command runs as the server's user, so the person answers each
	}
	ap := liveAutopilot(ctx, p)
	if ap == nil {
		return false
	}
	ap.spend()
	_ = s.audit(ctx, p, "", "assistant.autopilot.approved", string(g.Kind)+"/"+firstOf(g.Account, g.Tool), "autopilot, no card: "+truncate(g.Detail, 300), true)
	ap.announce(truncate(g.Detail, 300), g.Account)
	return true
}

// awsChangeEligible says whether a change is the kind that asks only by default: no rule asked about it, and it is not a service
// that always asks. That is the permission check's call, so it is made there and passed to the gate as the request's kind.
func awsChangeEligible(dec permDecision, command string) bool {
	return dec.Effect == permAsk && dec.Rule == "" && alwaysPattern(command) != ""
}
