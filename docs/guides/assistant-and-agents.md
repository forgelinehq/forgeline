---
title: Assistant and agents
parent: Guides
nav_order: 6
---

# Assistant and agents

Two different things that both use Claude.

| | Assistant | Agent step |
| --- | --- | --- |
| What | A panel in the console you ask questions | A step in a pipeline |
| Can do | **Read only.** Explain a failed run, review a pipeline, answer questions about the page, draft a pipeline into the editor (never saved for you) | Whatever tools the pipeline author lists. Read-only by default |
| Runs | On the server, with no tools of its own; it can ask the server for read-only lookups | Inside the step's container |

## The assistant

Click **Assistant** in the top bar. It sees the page you are on (a run, a pipeline, the infrastructure) but never secret values. Text it reads from logs is treated as data, not instructions: on a run whose log said "reply PWNED", it explained the real error and pointed out the planted line.

![Assistant]({{ '/console/21-assistant.png' | relative_url }})

Set it up under **Settings → Connections**: open **Claude** (under Add a connection until it is set up) and choose **Connect** (or **Open settings**). A **Test connection** button in the same popup sends one short question to check it.

The settings offer three ways to sign in:

- **Anthropic API key**: stored in the vault. Billed to that key. Best for teams.
- **Claude Code login token**: paste the output of `claude setup-token` once. It is stored as the secret `CLAUDE_CODE_OAUTH_TOKEN`, the same one agent steps use. The `claude` program runs in a scratch home directory with only that token, so the server user's own login is not touched.
- **Local `claude` CLI**: uses the login of the person running the server. Meant for a single-person setup.

![Settings]({{ '/console/33-assistant-token.png' | relative_url }})

The assistant is a panel that docks at the side (drag its edge, or use the wide button) or floats as a window you can move and resize; it remembers your choice. While it works you see each lookup as a card: what it asked and a cut of what came back (open a card to read it). The answer then appears as cards, with tables shown as tables, and a change it filed appears as a proposal card that follows the proposal's status. A **Stop** button cancels the question. The answer arrives in one piece, not word by word, and it was checked against a stand-in for the `claude` program, not a real model. See [note 024](https://github.com/forgelinehq/forgeline/blob/main/docs/design/024-assistant-agent-surface.md).

Questions are rate limited (20 a minute per person) and audited by page and length, not by text.

## Your AI team

**Team → Team** draws your agents as an org chart: you at the top, then the agents that report to you, then
theirs. Each card says what the agent is doing in plain words: working on a workflow, waiting for your OK at a
workflow's gate, asleep until its next scheduled run (and how often it runs), waiting to be asked, or off. It
also shows whether it only advises or may propose changes, and how many tasks it did this week. Below the chart,
the mission strip shows the workflow run in progress (or the last one) stage by stage, with the stage that waits
for you marked **waiting for you**.

Administrators choose who each agent reports to from the menu on its card. A line that would make a loop is
refused. A reporting line describes how your team is organised: a lead does not hand out work yet, and it gives no
agent any power over another. Every change of a line is in the audit log. Spending limits per agent are planned;
Forgeline does not yet measure what each agent spends.

![Your AI team]({{ '/console/88-team.png' | relative_url }})

## Agents you define

Under **Team → Agents** you can define named agents with instructions, skills and a short list of tools. The instructions are written in Markdown, with a live preview and advisory checks; the model receives exactly the text you wrote. See the [Console tour]({{ '/guides/console-tour/' | relative_url }}#agents).

## Agent steps

```yaml
steps:
  - id: review
    image: forgeline-agent-claude      # built from deploy/agent/Dockerfile
    agent:
      provider: claude-code
      prompt: Review the last commit and list risks.
      maxTurns: 5
    permissions:
      network: egress
      secrets: [CLAUDE_CODE_OAUTH_TOKEN]
```

Forgeline builds the command line and runs the vendor's own `claude` CLI in the container. It never reads or refreshes your login. Create the credential with `claude setup-token` (or use `ANTHROPIC_API_KEY`) and store it under **Secrets**. Your own `~/.claude` is never mounted.

![Agent step]({{ '/console/20-agent-step.png' | relative_url }})

{: .warning }
Check Anthropic's terms before using a subscription token (`setup-token`) for automation. An API key is the safer production choice. Codex and Copilot providers are not built yet.

Designs: [007 agent steps](https://github.com/forgelinehq/forgeline/blob/main/docs/design/007-agent-steps.md), [008 assistant](https://github.com/forgelinehq/forgeline/blob/main/docs/design/008-assistant.md).
