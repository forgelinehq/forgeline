package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
)

// Agents over the Agent Client Protocol (design note 053, slice 3). ACP is the protocol editors use to drive a coding agent:
// JSON-RPC over the agent's standard input and output. It is the same two seams as the Claude Code engine, in a standard
// form, so one client serves Antigravity (`agy acp`), the Copilot CLI, Codex and any other agent that speaks it: Forgeline gives
// the agent its tools as an MCP server in `session/new`, and answers its `session/request_permission` with the same card the
// person sees for every other tool. The agent runs in the person's sandbox container.

const (
	acpProtocolVersion = 1
	acpMaxSeconds      = 3600
)

// harnessCommands are the ready-made agents: the command, run by `sh -c` in the container, that starts one in ACP mode. The
// program has to be installed in the sandbox (the assistant can do it: "install the Antigravity CLI").
var harnessCommands = map[string]string{
	"antigravity": `export PATH=/work/.antigravity/bin:$HOME/.local/bin:$PATH; exec agy acp`,
	"copilot":     `exec copilot --acp`,
	"codex":       `exec codex-acp`,
}

// harnessHTTPMCP are the agents that ignore a stdio MCP server in session/new and take only an HTTP one. The Copilot CLI
// (1.0.107) is one: given a stdio server it starts nothing and says it has no such tool, checked against the real program,
// while an HTTP server works end to end. Their tools come through a listener on the container's loopback.
var harnessHTTPMCP = map[string]bool{"copilot": true}

// checkHarness validates the choice of agent; "" when it is fine.
func checkHarness(harness, cmd string) string {
	if harness == "custom" {
		cmd = strings.TrimSpace(cmd)
		switch {
		case cmd == "":
			return "a custom agent needs the command that starts it in ACP mode, such as `myagent acp`"
		case len(cmd) > 300 || strings.ContainsAny(cmd, "\r\n\x00"):
			return "the command must be one line of at most 300 characters"
		}
		return ""
	}
	if _, ok := harnessCommands[harness]; !ok {
		return `harness must be "antigravity", "copilot", "codex" or "custom"`
	}
	return ""
}

func (s *Server) harnessCommand(st assistantSettings) string {
	if st.Harness == "custom" {
		return strings.TrimSpace(st.HarnessCmd)
	}
	return harnessCommands[st.Harness]
}

// --- a small JSON-RPC client over the agent's pipes ---

type acpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *acpError) Error() string { return fmt.Sprintf("%s (%d)", e.Message, e.Code) }

type acpMsg struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *acpError       `json:"error,omitempty"`
}

type acpConn struct {
	w       io.Writer
	wmu     sync.Mutex
	mu      sync.Mutex
	next    int
	pending map[string]chan acpMsg
	closed  chan struct{}
	onNote  func(method string, params json.RawMessage)
	onReq   func(method string, params json.RawMessage) (any, *acpError)
}

func newACPConn(r io.Reader, w io.Writer, onNote func(string, json.RawMessage), onReq func(string, json.RawMessage) (any, *acpError)) *acpConn {
	c := &acpConn{w: w, pending: map[string]chan acpMsg{}, closed: make(chan struct{}), onNote: onNote, onReq: onReq}
	go func() {
		defer close(c.closed)
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			var m acpMsg
			if json.Unmarshal(sc.Bytes(), &m) != nil {
				continue
			}
			switch {
			case m.Method != "" && len(m.ID) > 0: // a request from the agent
				go func(m acpMsg) {
					res, rerr := c.onReq(m.Method, m.Params)
					out := map[string]any{"jsonrpc": "2.0", "id": m.ID}
					if rerr != nil {
						out["error"] = rerr
					} else {
						out["result"] = res
					}
					c.write(out)
				}(m)
			case m.Method != "": // a notification
				c.onNote(m.Method, m.Params)
			default: // an answer to one of ours
				c.mu.Lock()
				ch := c.pending[string(m.ID)]
				delete(c.pending, string(m.ID))
				c.mu.Unlock()
				if ch != nil {
					ch <- m
				}
			}
		}
	}()
	return c
}

func (c *acpConn) write(v any) {
	b, _ := json.Marshal(v)
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.w.Write(append(b, '\n'))
}

// call sends a request and waits for its answer, the end of the agent, or the context.
func (c *acpConn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan acpMsg, 1)
	c.pending[fmt.Sprint(id)] = ch
	c.mu.Unlock()
	c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	select {
	case m := <-ch:
		if m.Error != nil {
			return nil, m.Error
		}
		return m.Result, nil
	case <-c.closed:
		return nil, errors.New("the agent ended before answering")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *acpConn) notify(method string, params any) {
	c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// --- one message to an ACP agent ---

// askACP runs the chosen agent in the person's sandbox for one chat message and returns its answer.
func (s *Server) askACP(ctx context.Context, p Principal, conv, message, pageContext string, lines []helpLine, obs assistObserver) (string, error) {
	st := s.loadAssistant(ctx)
	command := s.harnessCommand(st)
	if command == "" {
		return "", errors.New("no agent is chosen: pick one under Settings › Assistant model")
	}
	if !s.engineInContainer(ctx) {
		return "", errors.New("agents over ACP run in the sandbox: an administrator turns it on under Settings › Assistant permissions")
	}
	sbx := s.loadSandbox(ctx)
	if sbx.Network == sandboxNetNone {
		return "", errors.New("the agent must reach its model, so the sandbox network cannot be set to none")
	}
	run := &engineRun{ctx: ctx, p: p, lines: lines, set: buildNativeSet(lines), obs: obs}
	tok := engineToken()
	engineRuns.mu.Lock()
	engineRuns.m[tok] = run
	engineRuns.mu.Unlock()
	defer func() { engineRuns.mu.Lock(); delete(engineRuns.m, tok); engineRuns.mu.Unlock() }()

	ctr, _, err := s.engineContainer(ctx, p)
	if err != nil {
		return "", err
	}
	// A ready-made agent that is not installed in the sandbox is found now, not by a "command not found" from the shell: the
	// person is offered the install as a card and told what will make it work.
	spec, known := engineSpecFor(st.Harness)
	if known {
		if installed, _, perr := s.probeEngine(ctx, ctr, spec); perr == nil && !installed {
			return s.missingEngineReply(ctx, p, spec), nil
		}
	}
	argv := []string{"exec", "-i", "-w", "/work", "-e", "HOME=/work/.agent"}
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	// The token that signs the chosen agent in, from the vault, in the environment (never an argument). A custom agent gets the
	// Gemini key, as before.
	tokenName, tokenEnv := geminiKeyName, geminiKeyName
	if known {
		tokenName, tokenEnv = spec.TokenName, spec.TokenEnv
	}
	apiKey, haveKey := "", false
	if tokenName != "" {
		apiKey, haveKey = s.internalSecretValue(ctx, tokenName)
	}
	if haveKey {
		argv = append(argv, "-e", tokenEnv)
		env = append(env, tokenEnv+"="+apiKey)
	}
	wall := time.Duration(acpMaxSeconds) * time.Second
	if ap := liveAutopilot(ctx, p); ap != nil {
		wall = 12 * time.Hour
	}
	httpMCP := harnessHTTPMCP[st.Harness]
	port := 20000 + rand.IntN(40000)
	if httpMCP {
		// The agent takes only an HTTP MCP server: start a listener on the container's loopback that forwards to the
		// engine socket, for as long as the agent runs. The token path goes in through the environment, never an argument.
		// A random port and a hard time limit mean a crashed run cannot leave a proxy that a later run collides with.
		argv = append(argv, "-e", "FORGELINE_MCP_PATH")
		env = append(env, "FORGELINE_MCP_PATH=/mcp/engine/"+tok)
		command = fmt.Sprintf("FORGELINE_MCP_SOCKET=/run/forgeline/engine.sock timeout %d /opt/forgeline/forgeline mcp --listen 127.0.0.1:%d >/dev/null 2>&1 & P=$!; trap 'kill $P 2>/dev/null' EXIT INT TERM; %s",
			int(wall.Seconds())+60, port, strings.TrimPrefix(command, "exec "))
	}
	argv = append(argv, ctr, "sh", "-c", command)
	cctx, cancel := context.WithTimeout(ctx, wall)
	defer cancel()
	cmd := s.engineCmd(cctx, "docker", argv...)
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var errb limitedBuffer
	errb.max = 1 << 14
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		return "", err
	}
	defer func() { stdin.Close(); _ = cmd.Wait() }()

	var (
		mu      sync.Mutex
		answer  acpAnswer
		replay  bool
		toolIDs = map[string]string{}
		ownIDs  = map[string]bool{} // tool calls of Forgeline's own tools: they have their own checks, so no card for them
	)
	conn := newACPConn(stdout, stdin,
		func(method string, params json.RawMessage) {
			if method != "session/update" {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if replay {
				return // a resumed session replays its history; it is not this answer
			}
			s.acpUpdate(params, &answer, toolIDs, obs)
			noteOwnCall(params, run.set, ownIDs)
		},
		func(method string, params json.RawMessage) (any, *acpError) {
			if method == "session/request_permission" {
				return s.acpPermission(run, params, func(id string) bool { mu.Lock(); defer mu.Unlock(); return ownIDs[id] }), nil
			}
			return nil, &acpError{Code: -32601, Message: "Forgeline does not provide " + method + "; use the tools you were given"}
		})

	initRes, err := conn.call(cctx, "initialize", map[string]any{
		"protocolVersion":    acpProtocolVersion,
		"clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": false, "writeTextFile": false}, "terminal": false},
		"clientInfo":         map[string]string{"name": "forgeline", "title": "Forgeline", "version": "1"},
	})
	if err != nil {
		return "", s.acpFail("starting the agent", err, errb.b.String())
	}
	var caps struct {
		AgentCapabilities struct {
			LoadSession         bool `json:"loadSession"`
			SessionCapabilities struct {
				Resume json.RawMessage `json:"resume"`
			} `json:"sessionCapabilities"`
		} `json:"agentCapabilities"`
		AuthMethods []struct {
			ID string `json:"id"`
		} `json:"authMethods"`
	}
	_ = json.Unmarshal(initRes, &caps)

	mcp := []any{map[string]any{"name": "forgeline", "command": "/opt/forgeline/forgeline", "args": []string{"mcp"},
		"env": []map[string]string{{"name": "FORGELINE_MCP_SOCKET", "value": "/run/forgeline/engine.sock"}, {"name": "FORGELINE_MCP_PATH", "value": "/mcp/engine/" + tok}}}}
	if httpMCP {
		mcp = []any{map[string]any{"type": "http", "name": "forgeline", "url": fmt.Sprintf("http://127.0.0.1:%d/mcp/engine/%s", port, tok), "headers": []any{}}}
	}
	newSession := func() (string, error) {
		res, err := conn.call(cctx, "session/new", map[string]any{"cwd": "/work", "mcpServers": mcp})
		if err != nil {
			return "", err
		}
		var out struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(res, &out)
		if out.SessionID == "" {
			return "", errors.New("the agent did not start a session")
		}
		return out.SessionID, nil
	}
	authenticate := func() error {
		for _, m := range caps.AuthMethods {
			if m.ID == "gemini-api-key" && haveKey && tokenName == geminiKeyName {
				_, err := conn.call(cctx, "authenticate", map[string]string{"methodId": m.ID})
				return err
			}
		}
		login := spec.Login
		if !known {
			login = st.HarnessCmd
		}
		msg := "the agent is not signed in. " + howToMakeItWork(spec, haveKey)
		if !known {
			msg = "the agent is not signed in. Sign in once inside the sandbox: run `docker exec -it -e HOME=/work/.agent " + ctr + " sh -c '" + login + "'` on the server and follow the agent's sign-in, or store a Gemini API key in the vault as " + geminiKeyName
		} else {
			msg += " The container is " + ctr + " (docker exec -it -e HOME=/work/.agent " + ctr + " sh -c '" + login + "')."
		}
		return errors.New(msg)
	}
	sid := s.acpSession(ctx, conv, st.Harness)
	var sessionID string
	if sid != "" && (caps.AgentCapabilities.LoadSession || len(caps.AgentCapabilities.SessionCapabilities.Resume) > 0) {
		method := "session/load"
		if len(caps.AgentCapabilities.SessionCapabilities.Resume) > 0 {
			method = "session/resume"
		}
		mu.Lock()
		replay = true
		mu.Unlock()
		_, rerr := conn.call(cctx, method, map[string]any{"sessionId": sid, "cwd": "/work", "mcpServers": mcp})
		mu.Lock()
		replay = false
		mu.Unlock()
		if rerr == nil {
			sessionID = sid
		}
	}
	if sessionID == "" {
		sessionID, err = newSession()
		var ae *acpError
		if errors.As(err, &ae) && (ae.Code == -32000 || strings.Contains(strings.ToLower(ae.Message), "auth")) {
			if aerr := authenticate(); aerr != nil {
				return "", aerr
			}
			sessionID, err = newSession()
		}
		if err != nil {
			return "", s.acpFail("starting a session", err, errb.b.String())
		}
	}
	s.saveACPSession(ctx, conv, st.Harness, sessionID)

	prompt := message
	if sys := s.engineInstructions(ctx, pageContext); sys != "" {
		prompt = sys + "\n\n---\n\nThe person's message:\n" + message
	}
	done := make(chan struct{})
	go func() { // a person who leaves cancels the turn
		select {
		case <-ctx.Done():
			conn.notify("session/cancel", map[string]string{"sessionId": sessionID})
		case <-done:
		}
	}()
	res, err := conn.call(cctx, "session/prompt", map[string]any{"sessionId": sessionID, "prompt": []map[string]string{{"type": "text", "text": prompt}}})
	close(done)
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.New("the person left before the agent finished")
		}
		return "", s.acpFail("the agent stopped", err, errb.b.String())
	}
	var out struct {
		StopReason string `json:"stopReason"`
	}
	_ = json.Unmarshal(res, &out)
	mu.Lock()
	text := strings.TrimSpace(answer.b.String())
	mu.Unlock()
	switch {
	case text == "" && out.StopReason == "refusal":
		return "", errors.New("the agent refused to answer")
	case text == "":
		return "", errors.New("the agent ended its turn without an answer (" + out.StopReason + ")")
	}
	return text, nil
}

// missingEngineReply is what the chat says when the chosen agent is not installed in the sandbox: it prepares the install card
// for an administrator (installing software is theirs) and tells everyone else who to ask.
func (s *Server) missingEngineReply(ctx context.Context, p Principal, sp engineSpec) string {
	head := sp.Label + " is not installed in your sandbox yet, so I cannot use it as the assistant's agent."
	if !p.Role.AtLeast(authn.Admin) {
		return head + " An administrator can install the latest version from Settings › Assistant model; then send your message again."
	}
	raw, _ := json.Marshal(map[string]string{"action": actInstallEngine, "harness": sp.Harness})
	if _, err := s.lookupPrepareAction(ctx, raw); err != nil {
		return head + " I could not prepare the install: " + err.Error() + "."
	}
	if obs := lookupObserver(ctx); obs != nil {
		obs(assistEvent{Kind: evFlush}) // the card is shown now, with this answer, not after a tool call that never happens
	}
	_, tokenSet := s.internalSecretValue(ctx, sp.TokenName)
	return head + " I prepared a card to install the latest version (from " + sp.Source + "): press Confirm, then send your message again.\n\n" + howToMakeItWork(sp, sp.TokenName != "" && tokenSet)
}

func (s *Server) acpFail(what string, err error, stderr string) error {
	msg := what + ": " + err.Error()
	if line := firstLine(stderr); line != "" {
		msg += " (" + truncate(line, 200) + ")"
	}
	return errors.New(msg)
}

const geminiKeyName = "GEMINI_API_KEY"

func (s *Server) acpSession(ctx context.Context, conv, harness string) string {
	return s.engineSession(ctx, "acp-"+harness+"-"+conv)
}

func (s *Server) saveACPSession(ctx context.Context, conv, harness, sid string) {
	s.saveEngineSession(ctx, "acp-"+harness+"-"+conv, sid)
}

// engineInstructions is what the agent is told about Forgeline, the person's notes and the page they are on, sent with the
// message because an ACP agent has its own system prompt.
func (s *Server) engineInstructions(ctx context.Context, pageContext string) string {
	sys := engineSystem + modeGuidance(ctx)
	if mt := memTurnFrom(ctx); mt != nil && mt.block != "" {
		sys += "\n\n<memory>\n" + memoryPreamble + strings.ReplaceAll(mt.block, "</memory>", "") + "</memory>"
	}
	if pageContext != "" {
		sys += "\n\n<untrusted_context>\n" + strings.ReplaceAll(pageContext, "</untrusted_context>", "</untrusted_context_>") + "\n</untrusted_context>"
	}
	return "<forgeline_instructions>\n" + sys + "\n</forgeline_instructions>"
}

// acpUpdate turns one session/update into what the chat shows.
// acpAnswer is the agent's answer as it arrives: its messages, one after another.
type acpAnswer struct {
	b      strings.Builder
	lastID string // the id of the message the last piece belonged to
}

func (s *Server) acpUpdate(params json.RawMessage, answer *acpAnswer, toolIDs map[string]string, obs assistObserver) {
	var in struct {
		Update struct {
			Kind       string          `json:"sessionUpdate"`
			MessageID  string          `json:"messageId"`
			Content    json.RawMessage `json:"content"`
			ToolCallID string          `json:"toolCallId"`
			Title      string          `json:"title"`
			Name       string          `json:"name"`
			ToolKind   string          `json:"kind"`
			Status     string          `json:"status"`
			RawInput   json.RawMessage `json:"rawInput"`
			Entries    []struct {
				Content string `json:"content"`
				Status  string `json:"status"`
			} `json:"entries"`
		} `json:"update"`
	}
	if json.Unmarshal(params, &in) != nil {
		return
	}
	u := in.Update
	switch u.Kind {
	case "agent_message_chunk":
		var c struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(u.Content, &c) == nil && c.Type == "text" && c.Text != "" {
			piece := c.Text
			// A new message (after a tool, say) starts on its own line, not glued to the one before.
			if u.MessageID != "" && answer.lastID != "" && u.MessageID != answer.lastID && answer.b.Len() > 0 {
				piece = "\n\n" + piece
			}
			if u.MessageID != "" {
				answer.lastID = u.MessageID
			}
			answer.b.WriteString(piece)
			if obs != nil {
				obs(assistEvent{Kind: evText, Content: piece}) // shown as it is written
			}
		}
	case "tool_call":
		name := firstOf(u.Name, u.ToolKind, "tool")
		if isForgelineTool(u.Name) || isForgelineTool(u.Title) {
			toolIDs[u.ToolCallID] = "" // Forgeline shows its own tools when they run
			return
		}
		toolIDs[u.ToolCallID] = name
		if obs != nil {
			obs(assistEvent{Kind: evToolStart, ID: "acp-" + u.ToolCallID, Tool: name, Args: truncate(firstOf(u.Title, string(u.RawInput)), argsMax)})
		}
	case "tool_call_update":
		name, known := toolIDs[u.ToolCallID]
		if !known || name == "" || obs == nil {
			return
		}
		if u.Status == "completed" || u.Status == "failed" {
			obs(assistEvent{Kind: evToolResult, ID: "acp-" + u.ToolCallID, Tool: name, Content: truncate(acpContentText(u.Content), previewMax), OK: u.Status == "completed"})
		}
	case "plan":
		if obs != nil && len(u.Entries) > 0 {
			var items []planItem
			for _, e := range u.Entries {
				st := map[string]string{"completed": "done", "in_progress": "doing"}[e.Status]
				if st == "" {
					st = "todo"
				}
				items = append(items, planItem{Text: truncate(e.Content, maxPlanText), Status: st})
			}
			if b, err := json.Marshal(items); err == nil {
				obs(assistEvent{Kind: evPlan, Content: string(b)})
			}
		}
	}
}

// noteOwnCall records a tool call that is one of Forgeline's own tools: the agent names it after the MCP server and the tool
// (forgeline-list_pipelines, forgeline_list_pipelines, forgeline list_pipelines), and the tool must be one we offered, so a
// tool of some other server whose name merely contains "forgeline" gets no free pass.
func noteOwnCall(params json.RawMessage, set *nativeSet, own map[string]bool) {
	var in struct {
		Update struct {
			Kind  string `json:"sessionUpdate"`
			ID    string `json:"toolCallId"`
			Title string `json:"title"`
			Name  string `json:"name"`
		} `json:"update"`
	}
	if json.Unmarshal(params, &in) != nil || in.Update.Kind != "tool_call" || set == nil {
		return
	}
	for _, name := range []string{in.Update.Title, in.Update.Name} {
		low := strings.ToLower(strings.TrimSpace(name))
		if !strings.HasPrefix(low, "forgeline") || len(low) <= len("forgeline")+1 {
			continue
		}
		rest := low[len("forgeline"):]
		if c := rest[0]; c != '-' && c != '_' && c != ' ' && c != '.' && c != ':' {
			continue
		}
		if _, ok := set.byWire[strings.TrimSpace(rest[1:])]; ok {
			own[in.Update.ID] = true
			return
		}
	}
}

func isForgelineTool(s string) bool { return strings.Contains(strings.ToLower(s), "forgeline") }

func acpContentText(raw json.RawMessage) string {
	var blocks []struct {
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var b strings.Builder
	for _, x := range blocks {
		b.WriteString(x.Content.Text)
	}
	return b.String()
}

// acpPermission answers the agent's request to use one of its own tools: the person's card, or autopilot inside the sandbox.
func (s *Server) acpPermission(run *engineRun, params json.RawMessage, isOwn func(toolCallID string) bool) any {
	var in struct {
		ToolCall struct {
			ID       string         `json:"toolCallId"`
			Title    string         `json:"title"`
			Kind     string         `json:"kind"`
			RawInput map[string]any `json:"rawInput"`
		} `json:"toolCall"`
		Options []struct {
			ID   string `json:"optionId"`
			Kind string `json:"kind"`
		} `json:"options"`
	}
	_ = json.Unmarshal(params, &in)
	pick := func(kinds ...string) any {
		for _, k := range kinds {
			for _, o := range in.Options {
				if o.Kind == k {
					return map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": o.ID}}
				}
			}
		}
		return map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}
	}
	// Forgeline's own tools run through Forgeline's checks and its own cards, so the agent's request to use one is not a
	// second question for the person. (Copilot asks before every tool, Forgeline's included, with the bare tool name as
	// the title; the call it belongs to was announced as "forgeline-<tool>".)
	if isOwn != nil && isOwn(in.ToolCall.ID) {
		return pick("allow_once", "allow_always")
	}
	tool := map[string]string{"execute": "Bash", "edit": "Write", "delete": "Write", "move": "Write", "fetch": "WebFetch", "search": "WebSearch"}[in.ToolCall.Kind]
	if tool == "" {
		tool = firstOf(in.ToolCall.Kind, "tool")
	}
	desc := in.ToolCall.Title
	if c, ok := in.ToolCall.RawInput["command"].(string); ok && c != "" {
		desc = c
	}
	if modeFrom(run.ctx) == modePlan && in.ToolCall.Kind != "fetch" {
		_ = s.audit(run.ctx, run.p, "", "assistant.mode.refused", "assistant", "plan only: "+truncate(desc, 300), false)
		return pick("reject_once", "reject_always")
	}
	if s.autopilotApproves(run.ctx, run.p, gateRequest{Kind: gateAgentTool, Tool: tool, Detail: desc}) {
		return pick("allow_once", "allow_always")
	}
	ask := permAskerFrom(run.ctx)
	if ask == nil {
		return pick("reject_once", "reject_always")
	}
	choice, err := ask(permRequest{Tool: tool, Command: desc, Why: "the agent wants to run its own tool in your sandbox. Autopilot runs these without asking"})
	if err != nil || choice == decisionDenied {
		_ = s.audit(run.ctx, run.p, "", "assistant.engine.tool", tool, "refused: "+truncate(desc, 300), false)
		return pick("reject_once", "reject_always")
	}
	_ = s.audit(run.ctx, run.p, "", "assistant.engine.tool", tool, "approved: "+truncate(desc, 300), true)
	return pick("allow_once", "allow_always")
}

var _ = filepath.Join

func firstOf(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
