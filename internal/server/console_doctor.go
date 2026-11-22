package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
)

// The assistant's doctor (design note 052): it checks the assistant's own setup and says what is stopping it from doing
// more, with the card that turns each thing on. Reading is open to developers; turning something on is a card an
// administrator confirms, so the assistant can find out why it is held back and ask, instead of saying "read-only".

const (
	doctorTool      = "doctor"
	actEnableShell  = "turn_on_sandbox"
	actAllowChanges = "allow_aws_changes"
)

// lookupDoctor reports on the things that decide what the assistant can do, and what to do about each.
func (s *Server) lookupDoctor(ctx context.Context, _ json.RawMessage) (string, error) {
	p := principalFrom(ctx)
	var b strings.Builder
	line := func(ok bool, what string) {
		mark := "OK"
		if !ok {
			mark = "HELD BACK"
		}
		fmt.Fprintf(&b, "- %s: %s\n", mark, what)
	}

	// The model.
	st := s.loadAssistant(ctx)
	switch st.Backend {
	case assistOff:
		line(false, "the assistant has no model. Fix: an administrator sets one under Settings › Assistant model.")
	case assistProvider:
		prov, model, _ := splitModelRef(st.Model)
		if msg := s.checkModelRef(ctx, st.Model); msg != "" {
			line(false, "the model "+st.Model+" cannot be used: "+msg)
		} else if pv, err := s.loadProvider(ctx, prov); err == nil && (pv.Kind == provBedrock) {
			line(true, "model "+model+" on "+pv.ID+" (Bedrock): works, but writes tool calls as text, which is less reliable than a provider with native tool calling (Anthropic, OpenAI, Gemini).")
		} else {
			line(true, "model "+st.Model+" through a provider: it makes real tool calls.")
		}
	case assistEngine:
		if s.loadSandbox(ctx).Enabled {
			line(true, "Claude Code runs as the agent, inside your sandbox container.")
		} else {
			line(false, "Claude Code runs as the agent on the server itself, so a command you approve runs as the server's user. Fix: prepare_action turn_on_sandbox (an administrator confirms), and it moves into the container.")
		}
	default:
		line(false, "the assistant runs through the "+st.Backend+" backend (a command-line program with its tools turned off). It has to write tool calls as text, which fails in ways that depend on the program and cannot use native tool calling. Fix: an administrator adds an Anthropic, OpenAI or Gemini API key under Settings › Models and providers and picks it under Assistant model.")
	}
	if ps, err := s.listProviders(ctx); err == nil && len(ps) == 0 {
		line(false, "no model provider is connected. Fix: Settings › Models and providers.")
	}

	// The shell.
	sb := s.loadSandbox(ctx)
	if sb.Enabled {
		if _, _, err := s.run(ctx, "docker", "version", "--format", "{{.Server.Version}}"); err != nil {
			line(false, "the sandbox is on but Docker does not answer, so run_shell cannot start. Fix: start Docker on the server.")
		} else {
			line(true, "the sandbox (run_shell) is on.")
		}
	} else {
		line(false, "the sandbox is off, so I cannot run commands or code. Fix: prepare_action turn_on_sandbox (an administrator confirms).")
	}

	// AWS: can it change anything?
	if aws := s.loadAWS(ctx); aws.Enabled {
		d, _ := s.loadPermissions(ctx)
		if d.OwnApproval["primary"] {
			line(true, "the primary AWS connection allows a change after you approve the exact command.")
		} else {
			line(false, "the primary AWS connection is protected, so a change is a proposal a different administrator approves. Fix: prepare_action allow_aws_changes with account primary (an administrator confirms); each change then shows you the command first.")
		}
		if accts, err := s.listAWSAccounts(ctx); err == nil {
			for _, a := range accts {
				if !d.OwnApproval[a.ID] {
					line(false, fmt.Sprintf("AWS account %s (%s) is protected. Fix: prepare_action allow_aws_changes with account %s.", a.Name, a.ID, a.ID))
				}
			}
		}
	} else {
		line(false, "AWS is not connected, so I cannot look at or change it. Fix: Settings › Connections.")
	}

	// Memory and autopilot.
	line(s.memoryOn(ctx), "memory is "+map[bool]string{true: "on: I can keep notes and files between conversations.", false: "off, so I forget everything between conversations. Fix: an administrator turns it on under Settings › Memory."}[s.memoryOn(ctx)])
	line(s.autopilotAllowed(ctx), "autopilot is "+map[bool]string{true: "allowed (the person switches it on in the chat).", false: "turned off for everyone. Fix: an administrator under Settings › Assistant permissions."}[s.autopilotAllowed(ctx)])

	// Recent trouble with the model.
	var log recentCalls
	if s.docs.Get(ctx, kindModelCalls, idRecentCalls, &log) == nil {
		failed, rate, total := 0, 0, 0
		for _, c := range log.Calls {
			if time.Since(c.At) > 24*time.Hour {
				continue
			}
			total++
			if c.Error != "" {
				failed++
				if strings.Contains(c.Error, "429") {
					rate++
				}
			}
		}
		if failed > 0 {
			line(false, fmt.Sprintf("%d of the last %d model calls in a day failed (%d were rate limits). Fix: check the provider's plan and limits under Settings › Models and providers; a fallback model keeps work going.", failed, total, rate))
		}
	}
	if !p.Role.AtLeast(authn.Admin) {
		b.WriteString("\nYou are not an administrator, so turning something on is for one: say what you need and why.\n")
	}
	b.WriteString("\nWhen something is held back and the task needs it, say so plainly, name the fix and prepare the card where there is one; do not just say you cannot.\n")
	return b.String(), nil
}

// --- the cards that turn a capability on ---

func (s *Server) prepareCapability(ctx context.Context, p Principal, act *AssistAction, account string) error {
	if !p.Role.AtLeast(authn.Admin) {
		return fmt.Errorf("only an administrator can turn this on; tell the person what to ask for")
	}
	switch act.Kind {
	case actEnableShell:
		if s.loadSandbox(ctx).Enabled {
			return fmt.Errorf("the sandbox is already on")
		}
		act.Pipeline = "sandbox"
		act.Effects = []string{
			"Turns the sandbox on for everyone who chats with the assistant.",
			"The assistant can then run commands and code in a container Forgeline starts: root inside, the network on, a /work folder that is kept.",
			"The container cannot see this server's files or credentials. A vault secret reaches it only through a card you approve.",
		}
		act.Title = "Turn on the sandbox"
	case actAllowChanges:
		if account == "" {
			account = primaryAccount
		}
		if account != primaryAccount {
			var a AWSAccount
			if s.docs.Get(ctx, kindAWSAccount, account, &a) != nil {
				return fmt.Errorf("no AWS account %q (see aws_accounts)", account)
			}
		}
		if d, _ := s.loadPermissions(ctx); d.OwnApproval[account] {
			return fmt.Errorf("changes are already allowed on %s", account)
		}
		act.Pipeline = account
		act.Effects = []string{
			"Lets the assistant change AWS account " + account + " after you approve each exact command on a card.",
			"Nothing runs without your answer. IAM, Organizations, accounts, SSM and STS always ask, and rules that deny still deny.",
			"You can protect the account again under Settings › Assistant permissions.",
		}
		act.Title = "Allow changes to AWS account " + account + " with my approval"
	}
	return nil
}

func (s *Server) doCapability(ctx context.Context, p Principal, a AssistAction) (string, string, *apiError) {
	if !p.Role.AtLeast(authn.Admin) {
		return "", "", &apiError{403, ErrorJSON{Error: "only an administrator can do this"}}
	}
	switch a.Kind {
	case actEnableShell:
		cur := s.loadSandbox(ctx)
		cur.Enabled, cur.UpdatedBy, cur.UpdatedAt = true, p.Name, time.Now().UTC()
		if err := s.docs.Put(ctx, kindSettings, idSandbox, cur); err != nil {
			return "", "", &apiError{500, ErrorJSON{Error: "could not save the sandbox setting"}}
		}
		return "The sandbox is on. I can run commands and code now.", "/assistant-permissions", nil
	case actAllowChanges:
		permMu.Lock()
		defer permMu.Unlock()
		d, err := s.loadPermissions(ctx)
		if err != nil {
			return "", "", &apiError{500, ErrorJSON{Error: "could not read the permission settings"}}
		}
		if d.OwnApproval == nil {
			d.OwnApproval = map[string]bool{}
		}
		d.OwnApproval[a.Pipeline] = true
		if err := s.docs.Put(ctx, kindSettings, idPermissions, d); err != nil {
			return "", "", &apiError{500, ErrorJSON{Error: "could not save the permission setting"}}
		}
		return "Changes to " + a.Pipeline + " now ask you first, with the exact command.", "/assistant-permissions", nil
	}
	return "", "", badRequest("unknown action")
}
