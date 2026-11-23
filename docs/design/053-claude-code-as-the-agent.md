# 053: Claude Code as the agent

Status: slice 1 built (2026-11-22). Follows note 052.

## Why

Note 052 tried to make the assistant work like a colleague by growing our own loop: a text protocol, a step budget, a plan, a
sandbox, memory. Each piece works, and the whole is still not what Claude Code, Copilot or an agent such as Hermes or OpenClaw
feels like to use. That is not a surprise: those are agent harnesses with years of work in the loop, context management,
subagents and tool use, and the models are trained on them. Matching them by imitation is the wrong goal for Forgeline.

Forgeline's job is the part they do not do: running an agent against real infrastructure safely. That is who may do what,
approvals by the right person, secrets that never reach a model, accounts, audit, budgets, and the same behaviour on a laptop
and in a team. So the assistant should **run** a real harness and be its control plane.

## Decision

A new assistant backend, `claude-agent`. For each chat message the server starts `claude -p` headless (stream-json), resumes the
conversation's session, and gives it:

- **Forgeline's tools over MCP**, at a per-run URL on the loopback address that exists only while the run lasts and answers only
  to the same machine. They are every lookup the assistant has (AWS, Kubernetes, Guard and Inspector, runs, agents and workflows,
  memory, `doctor`, `ask_user`, `update_plan`, `prepare_action` cards), run as the signed-in person, with the same role
  checks, permission cards for AWS changes, audit entries and observers as the chat loop.
- **Forgeline as the permission prompt** (`--permission-prompt-tool`). Claude Code's own Bash, Edit, Write and fetch tools are not
  pre-allowed; each asks Forgeline, which shows the person a card with the exact command and waits. A no, or nobody to ask, is a no.
  Reading (Read, Glob, Grep, the todo list) and Forgeline's own tools do not ask: they have their own checks.
- **A workspace and a home of its own** per person, kept between conversations, with only the Claude Code token (or a copy of the
  server user's credentials) inside. None of the server user's settings, hooks, plugins or MCP servers load, and none of the
  server's token, master key or cloud credentials are in its environment.

The engine's steps show in the chat as tool steps like the others, its final answer is the reply, and its session id is kept
per conversation so the next message continues.

## What is and is not done

Built: the runner, the MCP endpoint, the permission prompt, session resume, the settings choice, tests with a stand-in program
for every check (each removed in turn and a test failed), the guide.

Not built, in this order:

1. **Run it in the sandbox container** (built, slice 2, 2026-11-22; see below), so Bash is in a container and not on the server. Today a Bash the person approved runs as
   the server's user in the person's workspace. Needs `claude` in the image, the loopback endpoint reachable from the
   container (a mounted socket and a small bridge), and the token passed in the environment.
2. **Partial messages**: the answer arrives whole; stream it as it is written.
3. **Autopilot for the engine**: an envelope that approves its Bash and Edit inside the workspace, never outside it.
4. **Other engines**: Codex CLI, the Copilot CLI and **Google's Antigravity CLI (`agy`)** through the same two seams (tools over MCP, permission prompt). Google replaced Gemini CLI with Antigravity CLI on 2026-06-18 for unpaid-tier and Google One users, so Antigravity is the Google engine to build, not Gemini CLI. The maintainer has `agy` set up with a Gemini Pro plan. To check before building: whether `agy` runs headless, takes MCP servers, and has a permission-prompt hook like Claude Code's `--permission-prompt-tool`; if it has no prompt hook, its tools can only be limited to the sandbox container, not asked one by one.
5. **Memory**: Claude Code's own memory and CLAUDE.md in the person's home, next to Forgeline's memory files.

## Security notes

- The loopback endpoint trusts the run token and the machine. A process on the same machine that learns a token can call the
  person's tools for the length of a run: it is random, never logged, and removed when the run ends.
- Claude Code's tools on the host are only as safe as the person's answer on each card until step 1 is done. The settings page says
  so, and the sandbox, not the host, is where this should end up.

## Built (slice 2, 2026-11-22): the agent in the sandbox

- With the sandbox on, `claude` runs inside the person's sandbox container (`docker exec`), in `/work`, with its home at
  `/work/.agent` so sessions persist. Its Bash and file tools therefore act on the container, not the server. The container
  is the same kind as `run_shell`'s (no host mounts of the server's files, dropped capabilities, no Docker socket), from an
  image Forgeline builds once on first use (`forgeline/agent:1`: Node 24, Claude Code 2.1.330, git, curl, python, ripgrep).
- The container has no route to the Forgeline server. Its tools come through a **unix socket** the server serves per person and
  mounts read-only into the container, with a copy of the server's own binary mounted read-only as the stdio MCP bridge in
  front of it (`forgeline mcp` with `FORGELINE_MCP_SOCKET`). The socket serves only the engine endpoint, and the path carries a
  token for one running message. The binary is copied to the data directory first, because the updater replaces the running
  binary by name and a container outlives a version.
- The token goes in through the environment of `docker exec`, never an argument; the system prompt (which may hold log text and
  memory) goes in a file in the mounted directory and is removed after the run. The server user's login is not carried into a
  container: it needs the Claude Code token in the vault.
- If the sandbox network is set to none, the agent cannot reach its model and says so. With the sandbox off, the engine runs on
  the server as in slice 1, and the doctor says so.
- Not checked in a unit test: that the container's user can connect to the socket (the server's own user is the same user in
  the tests). That is checked by a real run on the Pi.

## Built (slice 3, 2026-11-22): any agent over the Agent Client Protocol

- The Claude Code engine is two seams: Forgeline's tools handed to the agent as an MCP server, and Forgeline answering the agent's
  permission requests. ACP (agentclientprotocol.com) is the standard form of both: JSON-RPC over the agent's standard input and
  output; `session/new` takes the MCP servers; the agent asks `session/request_permission` before a tool and streams
  `session/update`. VS Code's harnesses (Copilot, Claude, Codex) and Zed work the same way, so one client serves them all.
- A new backend, `acp-agent`, with a choice of agent: Antigravity (`agy acp`), GitHub Copilot CLI (`copilot --acp`), Codex
  (`codex-acp`), or a custom one-line command. The agent runs in the person's sandbox container, as the Claude Code engine does,
  and gets Forgeline's tools over the same socket and bridge. Sandbox on is required.
- The client: `initialize` (no file or terminal methods offered, so the agent uses its own tools in the container), `session/new`,
  or `session/resume`/`session/load` to continue the conversation's session (the replayed history is dropped), `session/prompt`
  with the Forgeline instructions, the person's notes and the page in front of the message, `session/cancel` if the person leaves.
  `agent_message_chunk` is the answer; thoughts are not shown; `tool_call` and `tool_call_update` become steps (Forgeline's
  own tools are shown by Forgeline, not twice); `plan` becomes the plan.
- A permission request becomes the same card, worded by kind (execute is a command, edit a file change, fetch a web request).
  Allow once selects the agent's `allow_once`, deny its `reject_once`; nobody to ask is a rejection. Autopilot answers it inside the
  sandbox, audited.
- **Signing in.** The container is separate from the server: a login on the server host is not seen inside it, by design. Sign in
  once inside the sandbox (`docker exec -it -e HOME=/work/.agent <container> ...`, the message names the exact command), or give
  the agent a Gemini API key by storing `GEMINI_API_KEY` in the vault: Forgeline passes it in the environment and calls
  `authenticate` with the `gemini-api-key` method. Checked against the real `agy acp` for `initialize` (it lists Google, Gemini
  Enterprise, API key and Agent Platform sign-ins) and `session/new` (it answers "Authentication required" until signed in).
- Not yet: streaming the answer as it is written, other sign-in methods from the console, and a real turn against a signed-in
  agent (all tested so far with a stand-in agent that speaks ACP).

## Checked against the real Copilot CLI (2026-11-23)

`scripts/dev/acp-probe.py` drives a real ACP agent with the messages Forgeline's client sends, gives it a one-tool MCP server,
and says whether the agent called it (one model request on the agent's own account). Run against the Copilot CLI
(1.0.107, then 1.0.114 after it updated itself):

- **It speaks ACP as Forgeline expects**: `initialize` (protocol 1, `loadSession`, `authMethods: copilot-login`), `session/new`,
  `session/prompt`, streamed `agent_message_chunk`, `tool_call` / `tool_call_update`, `usage_update`, `stopReason: end_turn`.
- **It ignores a stdio MCP server in `session/new`** and says it has no such tool; `/mcp list` says "No MCP servers
  configured" either way. The standard says an agent must take stdio; this one does not. It takes **HTTP** (it advertises
  `mcpCapabilities: {http, sse}`), and that works end to end: its client sends `server/discover`, `initialize`,
  `tools/list` and `tools/call` to the URL, the tool is announced as `forgeline-<tool>`, and the answer uses the result.
- **It asks permission before every tool, Forgeline's own included**, with the bare tool name as the title and kind `other`.
  Forgeline would have shown a card for each lookup ("the agent wants to run its own tool").
- A stand-in agent could not show either, because it was written from the same assumptions as the client.

Built from that: an agent that takes only HTTP (`copilot`) gets Forgeline's tools as an HTTP MCP server on the container's
loopback. `forgeline mcp --listen 127.0.0.1:PORT` forwards to the engine socket, with the path fixed to the run's token and
the client's credentials dropped; the server starts it in the container beside the agent on a random port, stops it when the
agent ends (and after a time limit if that fails), and passes the token in the environment, never an argument. A request
to use one of Forgeline's own tools is allowed without a card, matched by the tool call's id and by a name that is
`forgeline` plus a tool Forgeline offered (a similarly named tool of another server still asks). The sign-in message names
`copilot login` for Copilot.

**Not checked:** the whole path inside the sandbox container (the agent installed there and signed in with `copilot login`
inside the container, Docker, the socket). Everything up to that is checked against the real program or the tests.

