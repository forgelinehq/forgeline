---
title: Agent engines for the assistant
parent: Guides
nav_order: 33
---

# Agent engines for the assistant

Instead of Forgeline's own loop, the assistant can hand a conversation to a coding agent: the real Claude Code, or another
agent over the Agent Client Protocol (ACP): Antigravity, GitHub Copilot CLI, Codex, or your own command. The agent does the
thinking and the work; Forgeline supplies its tools and its permission prompts.

Use it when you want the assistant to plan, use subagents, read files and run commands in a workspace, and carry on from
message to message.

## At a glance

| | |
| --- | --- |
| Where in the console | **Settings → AI → Assistant model**: **Claude Code as the agent** or **An agent over ACP** |
| Needs | The `claude` program and a Claude Code token, or an agent installed in your [sandbox](assistant-sandbox.md) |
| Design notes | [052](../design/052-assistant-that-works-like-a-colleague.md), [053](../design/053-claude-code-as-the-agent.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Use the agent in the chat | Developer | |
| Choose the engine, see its status | Administrator | |
| Install an agent in the sandbox | Administrator | Always a card you confirm, even under autopilot |

## How it works

### Claude Code as the agent

1. Choose **Claude Code as the agent** under **Settings → AI → Assistant model**.
2. Claude Code plans, uses subagents, reads files, runs commands in a workspace kept for you, and carries on from message
   to message.
3. Forgeline is its tools and its permission prompt: it can look at AWS, Kubernetes, findings and runs, keep memory, and
   prepare cards, and each of those keeps its role checks and audit entry. Its own **Bash, Edit and Write ask you on a
   card** with the exact command first; say no and it does not run.

It needs the `claude` program on the server and a Claude Code token (the same one agent steps use) or the server user's
login. With the **sandbox on** (**Settings → AI → Assistant permissions**) the whole of Claude Code runs inside your sandbox
container: its commands and files are in the container's `/work`, which also holds its memory and sessions, and
Forgeline's tools reach it through a socket, not the network. The first message builds the image, which takes a few
minutes. This needs a Claude Code token (the secret `CLAUDE_CODE_OAUTH_TOKEN`). With the sandbox off it runs on the server itself, as the
server's user, so approve each command as you would in a terminal.

### An agent over ACP

Under **Settings → AI → Assistant model**, choose **An agent over ACP**, then **Antigravity CLI (agy)**, **GitHub Copilot CLI**,
**Codex**, or your own command. They run in your sandbox (turn it on first), speak the Agent Client Protocol, and get
Forgeline's tools and its permission cards, just as Claude Code does.

### Streaming

With Claude Code, Copilot, Codex or Antigravity as the agent, the text streams into the chat as the agent writes it,
instead of arriving whole at the end. A message the agent writes after using a tool starts on its own line, and the
conversation history keeps all of what you saw.

## Install an agent

Under the agent's name the page says whether the program is installed in your sandbox, and which version.

1. If it is not, choose **Install the latest**. The page shows the exact command and where it comes from.
2. Confirm. Forgeline runs it as root inside the sandbox only. GitHub Copilot CLI and the Codex adapter come from npm,
   Antigravity from Google's installer script.

The same happens in the chat: if you pick an agent that is not installed and send a message, the assistant does not fail
with "command not found". It says so, prepares an **Install** card for an administrator (anyone else is told whom to ask),
and says what will make the agent work once it is there. It can also prepare that card itself when you ask it to use a
missing agent. Nothing on the server itself changes: the program lives in your sandbox. Claude Code is not installed this
way: the sandbox image brings it, at the version Forgeline was tested with. With the sandbox off it uses the `claude`
program on the server, which Forgeline never installs for you.

## Sign the agent in

An agent signs in **inside the sandbox**, not on the server: a login on the server is not visible in the container. Two
ways, shown on the same page for the agent you chose:

1. **A token in the vault** (Settings → Secrets, or the field on the page). Forgeline passes it to that agent only, in its
   environment: `COPILOT_GITHUB_TOKEN` for Copilot (a fine-grained token with the "Copilot Requests" permission; a classic
   `ghp_` token is not supported), `GEMINI_API_KEY` for Antigravity, `CODEX_API_KEY` for Codex.
2. **Sign in once by hand** with the command the page shows, in a terminal on the server (`docker exec -it ... copilot
   login --device-code`). Copilot asks whether to keep its token in a plain-text file because the sandbox has no keychain;
   answer yes, or the token is dropped.

## Security model

- Forgeline's tools keep their role checks and audit entries when an agent calls them.
- The agent's own Bash, Edit and Write ask you on a card with the exact command, unless autopilot is on inside the sandbox.
- Installing is always a card you confirm, and only an administrator can.
- Agent sign-in lives in the sandbox (`/work/.agent`), which downloads never reach.

## Limits and verification

{: .warning }
**What has been checked against the real programs.** Copilot was installed, signed in by device login and run over ACP in
the sandbox; all three installers were run for real. A token from the vault has not been tried against a real account, and
Codex and Antigravity have not been signed in or run in the sandbox.
On 2026-11-27 (main at `487f373`) only the role (an engine's status is 403 for a developer) and the page labels were checked on the UAT instance; installing and signing in were not. See the [note 2026-11-27](../testing/2026-11-27-uat-assistant-agents.md).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| "command not found" for an agent | It is not installed in your sandbox; use **Install the latest**. |
| The agent is installed but cannot answer | It is not signed in inside the sandbox; use a vault token or sign in by hand. |
| Copilot loses its login | You answered no when it asked to keep the token in a plain-text file. |
| Claude Code cannot start in the sandbox | The first message builds the image, which takes a few minutes; it also needs a Claude Code token. |
| The Install card does not appear for you | Only an administrator can install; others are told whom to ask. |

## Related

- [Assistant](assistant.md)
- [Assistant sandbox](assistant-sandbox.md)
- [Assistant permissions and autopilot](assistant-permissions-and-autopilot.md)
