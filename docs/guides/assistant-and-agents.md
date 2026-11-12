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

The settings offer four ways to sign in:

- **Anthropic API key**: stored in the vault. Billed to that key. Best for teams.
- **Claude Code login token**: paste the output of `claude setup-token` once. It is stored as the secret `CLAUDE_CODE_OAUTH_TOKEN`, the same one agent steps use. The `claude` program runs in a scratch home directory with only that token, so the server user's own login is not touched.
- **Local `claude` CLI**: uses the login of the person running the server. Meant for a single-person setup.
- **Local `copilot` CLI** (GitHub Copilot): run `copilot login` as the server's user first. Forgeline runs
  `copilot` with no tools, no MCP servers and no custom instructions, in an empty scratch folder, with the question on
  stdin and nothing of the server's environment. Each question uses one Copilot premium request (a free plan has only
  a few a month); leave the model empty to let Copilot choose, or name one. Meant for a single-person setup.

![Settings]({{ '/console/33-assistant-token.png' | relative_url }})

The assistant is a panel that docks at the side (drag its edge, or use the wide button) or floats as a window you can move and resize; it remembers your choice. While it works you see each lookup as a card: what it asked and a cut of what came back (open a card to read it). The answer then appears as cards, with tables shown as tables, and a change it filed appears as a proposal card that follows the proposal's status. A **Stop** button cancels the question. The answer arrives in one piece, not word by word, and it was checked against a stand-in for the `claude` program, not a real model. See [note 024](https://github.com/forgelinehq/forgeline/blob/main/docs/design/024-assistant-agent-surface.md).

Questions are rate limited (20 a minute per person) and audited by page and length, not by text.

The assistant can also **write skills, agents and workflows** for an administrator ("write an agent that explains failed
runs", "make a weekly workflow with the cost watcher"). Nothing is created by asking: it prepares a card that says, in
words the server works out (not the model's), what saving it would do: which tools the agent may use, whether it only
reads or may also draft changes for approval, which skills it follows, when a workflow runs and which stages wait for an
administrator. **Confirm** saves it as version 1, through the same checks as the forms on the Agents page, and the audit
log says it was written by the assistant and confirmed by you. A developer can ask for one, but only an administrator gets
the card, as in the console.

![The assistant writes an agent]({{ '/console/134-assistant-writes-agent.png' | relative_url }})

## Your AI team

**Team → Team** draws your agents as an org chart: you at the top, then the agents that report to you, then
theirs. Each card says what the agent is doing in plain words: working on a workflow, waiting for your OK at a
workflow's gate, asleep until its next scheduled run (and how often it runs), waiting to be asked, or off. It
also shows whether it only advises or may propose changes, and how many tasks it did this week. Below the chart,
the mission strip shows the workflow run in progress (or the last one) stage by stage, with the stage that waits
for you marked **waiting for you**.

Administrators choose who each agent reports to from the menu on its card. A line that would make a loop is
refused. A reporting line describes how your team is organised: a lead does not hand out work yet, and it gives no
agent any power over another. Every change of a line is in the audit log. Each card also shows the agent's budget
for the month (see Budgets below).

![Your AI team]({{ '/console/88-team.png' | relative_url }})

### Missions

Team › **Missions** gives one agent a goal in your own words, for example "check the payments service for anything
fragile and fix what you can", and one AWS account to look at. The mission goes through five stages, drawn live on its
page: **Discover** (the agent only reads), **Propose** (an agent allowed to propose files change proposals for what it can
fix; one that only advises recommends), **Your OK** (a different administrator approves or rejects each proposal; the
approver sees which mission filed it), **Verify** (if anything was applied, the agent looks again and says whether each
finding is fixed) and **Report** (written by Forgeline from the record, not by the model).

Two rules keep a mission honest. Every finding must name the lookup that showed it, and the server keeps only findings
whose lookup the agent really ran: the rest are left out and counted on the page. And the agent may only look at the
account you chose: a lookup for any other account is refused by the server. Each mission has a token budget (200,000 by
default); it stops before its next model call once that is used. You can cancel a mission you started; an administrator
can cancel any.

![A mission waiting for approval]({{ '/console/96-mission-waiting.png' | relative_url }})

![A finished mission]({{ '/console/97-mission-done.png' | relative_url }})

### Budgets

Settings › **AI use and budgets** shows what each agent used this month (tokens, model time, calls, and the cost where
the claude program reports it) and what people used asking the assistant themselves. An administrator can give an agent a
monthly budget in tokens, minutes or both. At 80% the agent's card and the page warn; once a budget is used up the agent
stops before its next model call, its card says **Stopped**, and a workflow step it was running fails with a message
saying why. Raise the budget, or wait for the next month (UTC), to let it run again. Every budget change and every crossing
is in the audit log. Cloud spend an agent causes is not counted here yet.

![AI use and budgets]({{ '/console/95-ai-use.png' | relative_url }})

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
Check Anthropic's terms before using a subscription token (`setup-token`) for automation. An API key is the safer production choice. Codex and Copilot agent steps are not built yet; Copilot can already answer as the assistant.

Designs: [007 agent steps](https://github.com/forgelinehq/forgeline/blob/main/docs/design/007-agent-steps.md), [008 assistant](https://github.com/forgelinehq/forgeline/blob/main/docs/design/008-assistant.md).
