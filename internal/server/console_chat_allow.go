package server

import (
	"context"
	"errors"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// Permissions for commands and files in the sandbox, as coding agents do them (design note 053). A card offers once, "for
// this chat", or no with a note saying what to do instead. "For this chat" is remembered for the person and the
// conversation, in memory only: a new chat, or a restart, asks again. It is the same for the engine's Bash, Write and Edit
// and for Forgeline's own run_shell.

const (
	shellCardTool   = "sandbox_command" // a run_shell command's card; not "run_shell", which is the card that hands out secrets
	scopeChat       = "chat"
	maxShownText    = 20000
	maxChatRuleText = 500
	maxChatRules    = 200
	ruleEdits       = "edits"
)

// shellMeta are characters that can put a second command, a redirect or a substitution into a line.
const shellMeta = ";&|<>`$(){}\\\n\r"

func simpleCommand(c string) bool { return !strings.ContainsAny(c, shellMeta) }

// runsAnything are programs whose arguments say what code runs, so allowing "the same kind" would allow anything: only the
// exact command can be allowed for them.
var runsAnything = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "python": true, "python3": true, "node": true, "deno": true,
	"bun": true, "ruby": true, "perl": true, "php": true, "env": true, "xargs": true, "find": true, "sudo": true, "su": true, "timeout": true, "nohup": true,
	"nice": true, "ssh": true, "scp": true, "eval": true, "exec": true, "source": true, "docker": true, "curl": true, "wget": true, "rm": true, "mv": true,
	"cp": true, "dd": true, "chmod": true, "chown": true, "awk": true, "sed": true, "npx": true, "uvx": true}

var subcommandRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// commandRule is what "for this chat" would allow for a command: its program and subcommand with any arguments ("npm test
// *"), or only exactly this command.
func commandRule(c string) (rule, label string) {
	f := strings.Fields(c)
	if simpleCommand(c) && len(f) >= 2 && !runsAnything[f[0]] && !strings.ContainsAny(f[0], "=/") && subcommandRE.MatchString(f[1]) &&
		!slices.Contains([]string{"exec", "run", "eval"}, f[1]) {
		return "kind:" + f[0] + " " + f[1], f[0] + " " + f[1] + " *"
	}
	return "exact:" + c, c
}

// chatRule is what a card may offer to allow for the rest of the chat, or "" when this must be asked each time: anything
// the card cannot show, a command in the background, and files that are hidden or outside /work.
func chatRule(tool string, in map[string]any) (rule, label string) {
	a, err := planActionOf(tool, in)
	if err != nil {
		return "", ""
	}
	for _, s := range []string{a.Command, a.Path} {
		if strings.IndexFunc(s, hiddenRune) >= 0 {
			return "", ""
		}
	}
	if tool == "Bash" {
		if len(a.Command) > maxChatRuleText {
			return "", ""
		}
		return commandRule(a.Command)
	}
	// A file or folder whose name starts with a full stop configures things (git hooks, shell profiles, tool settings).
	if slices.ContainsFunc(strings.Split(path.Clean(a.Path), "/"), func(p string) bool { return strings.HasPrefix(p, ".") }) {
		return "", ""
	}
	return ruleEdits, "file changes under /work"
}

type chatAllowances struct {
	mu    sync.Mutex
	rules map[string]map[string]bool // person and conversation -> rules
}

func chatKey(ctx context.Context, p Principal) string {
	conv, _ := ctx.Value(autopilotConvKey{}).(string)
	if p.UserID == "" || conv == "" {
		return ""
	}
	return p.UserID + "\x00" + conv
}

func (c *chatAllowances) add(key, rule string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rules == nil {
		c.rules = map[string]map[string]bool{}
	}
	if c.rules[key] == nil {
		c.rules[key] = map[string]bool{}
	}
	if len(c.rules[key]) < maxChatRules {
		c.rules[key][rule] = true
	}
}

// covers says whether something the person allowed earlier in this chat covers this call.
func (c *chatAllowances) covers(key, tool string, in map[string]any) bool {
	rule, _ := chatRule(tool, in)
	if key == "" || rule == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rules[key][rule] {
		return true
	}
	// A command allowed exactly as written stays allowed even where its kind is offered now.
	cmd, _ := in["command"].(string)
	return tool == "Bash" && c.rules[key]["exact:"+cmd]
}

// shownAction is a file change as a card shows it: the content, or the old and new text.
func shownAction(tool string, in map[string]any) []planAction {
	str := func(k string) string {
		v, _ := in[k].(string)
		return cutShown(v)
	}
	switch tool {
	case "Write":
		return []planAction{{Tool: tool, Path: str("file_path"), Content: str("content")}}
	case "Edit":
		all, _ := in["replace_all"].(bool)
		return []planAction{{Tool: tool, Path: str("file_path"), Old: str("old_string"), New: str("new_string"), ReplaceAll: all}}
	}
	return nil
}

func cutShown(s string) string {
	if len(s) <= maxShownText {
		return s
	}
	return truncate(s, maxShownText) + "\n[cut here: the whole text is longer than a card shows]"
}

// readOnlyCommands only look. A single one of them runs without a card, as in other coding agents.
var readOnlyCommands = map[string]bool{"ls": true, "cat": true, "head": true, "tail": true, "wc": true, "pwd": true, "grep": true, "echo": true,
	"which": true, "du": true, "df": true, "stat": true, "id": true, "uname": true, "whoami": true}

func readOnlyCommand(c string) bool {
	f := strings.Fields(c)
	return len(f) > 0 && simpleCommand(c) && readOnlyCommands[f[0]]
}

var (
	errNobodyToApprove = errors.New("this command needs the person's approval and nobody is here to give it, so it did not run; ask in the chat")
	errSaidNo          = errors.New("the person said no, so it did not run")
)

// askShell is run_shell's permission: a command that only looks runs; autopilot and what was allowed for this chat run;
// anything else asks the person.
func (s *Server) askShell(ctx context.Context, p Principal, command, image string) error {
	if readOnlyCommand(command) {
		return nil
	}
	if s.autopilotApproves(ctx, p, gateRequest{Kind: gateAgentTool, Tool: runShellTool, Detail: command}) {
		return nil
	}
	in := map[string]any{"command": command}
	key := chatKey(ctx, p)
	if s.chatAllow.covers(key, "Bash", in) {
		_ = s.audit(ctx, p, "", "assistant.sandbox.command", "sandbox", "allowed for this chat: "+truncate(command, 300), true)
		return nil
	}
	ask := permAskerFrom(ctx)
	if ask == nil {
		return errNobodyToApprove
	}
	rule, label := chatRule("Bash", in)
	if key == "" {
		rule, label = "", ""
	}
	shown := cutShown(command)
	if image != defaultSandboxImage {
		shown = "[in " + image + "] " + shown
	}
	choice, err := ask(permRequest{Tool: shellCardTool, Command: shown, Pattern: label, Scope: scopeChat, Note: true,
		Why: "the assistant wants to run this in your sandbox. Autopilot runs these without asking"})
	if err != nil || choice != decisionOnce && (choice != decisionAlways || rule == "") {
		_ = s.audit(ctx, p, "", "assistant.sandbox.command", "sandbox", "refused: "+truncate(command, 300), false)
		if err != nil {
			return err
		}
		return errSaidNo
	}
	if choice == decisionAlways {
		s.chatAllow.add(key, rule)
	}
	_ = s.audit(ctx, p, "", "assistant.sandbox.command", "sandbox", map[string]string{decisionOnce: "approved once", decisionAlways: "approved, and allowed for this chat"}[choice]+": "+truncate(command, 300), true)
	return nil
}
