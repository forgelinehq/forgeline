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

The settings offer five ways to sign in:

- **A model from Models and providers**: any model an administrator turned on under Settings › Models and providers
  (Anthropic, OpenAI, Gemini, Azure OpenAI, AWS Bedrock, or a local server such as Ollama). See
  [Models and providers]({{ '/guides/models-and-providers/' | relative_url }}).

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

## Teams

**Team › Teams** holds teams of agents. A team is a **lead** and up to eight **members**, and an agent can be in several
teams. An administrator makes one with **New team**: a side panel asks for a name, a purpose (the lead reads it), the
lead, and the members, picked from a searchable list of agents.

Open a team and **give it a goal** in your own words ("why did the last deploys fail, and is anything wrong
underneath?"). Then:

1. **The lead plans.** It gives each member who can help one task, and says in a sentence how it split the work. It may
   only hand work to its own members, once each; anything else in its plan is dropped.
2. **The members work at the same time** (three at once), each with its own model, skills, tools and monthly budget, and
   as the person who gave the goal, so no agent can do more than you could.
3. **The lead writes the answer** from what the members found. Their answers reach it as data, never as instructions,
   so a member that read something hostile in a log cannot steer the lead.

The run's page shows the plan, a card per member with its task, what it looked up and what it found, and the team's
answer, live. A change a member proposes goes to Approvals like any other. A run fails before anything is spent when a
member is turned off. Every agent call is audited and appears in the model call log as *agent in team*. Teams are kept
with their last 50 runs; removing a team keeps its agents.

The library's **Incident triage** and **Platform review** packages install as teams. Below the teams, every agent is
listed with what it is doing now (working, waiting for your OK, asleep until its next run, waiting to be asked, or off),
which teams it is in, and what it used this month.

**Deleting an agent** (Delete on its page) archives it: it is turned off and listed under Agents › Archived, and can be
restored for 30 days, after which it is deleted for good. An agent that a team, an enabled workflow or an unfinished
mission still uses cannot be deleted; the message says which.

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

## Workflows

A workflow runs agents in an order you draw. **Team › Workflows** lists them as cards (what each is made of, how it
starts, its last run), with a **Runs** tab for every run of every workflow and an **Archived** tab. **New workflow** opens
an n8n-style canvas that fills the page: press a node's **+** (or **Add node**) and pick from a searchable side panel of
your agents, the logic nodes and the lookups; the new node is wired in after the one you came from. Drag from a node's
port to another node to wire them, pick a node to set it in the panel beside the canvas, and press Delete to remove it.
Drag the background to move around, scroll with Ctrl (⌘ on a Mac) to zoom, and **Fit** shows the whole graph. How the
workflow starts and what it may do are under **Settings**.

| Node | What it does |
| --- | --- |
| **Agent** | An agent does a task; it can write named fields (such as `severity`) into the run's shared state |
| **Lookup** | One read-only lookup, with no model |
| **Decide** | Takes one wire, by a rule on what earlier nodes wrote or by an agent choosing |
| **Ask a person** | Waits for an answer or an approval |
| **Merge** | Waits for parallel branches |
| **End** | Finishes the run |

Loops are allowed: each node runs at most its limit (3 unless you say up to 10), and a run at most 50 steps. A run is
drawn on the same canvas: nodes turn green or red, the wires taken are bold, and picking a node shows what went in and
what came out each time it ran. A workflow starts by hand, on a schedule, from a signed webhook, or after a pipeline run
fails; agents read what started it under `trigger`. A schedule is every few hours, daily, on weekdays, weekly or monthly
at a time, or a cron expression, in a time zone you pick; a team can be given a goal on a schedule too. **Team ›
Schedules** lists everything that runs by itself, with pause and run now (design note 047). Workflows made of stages still run, and **Edit on the canvas** turns
one into a graph. **Delete** on a workflow's page archives it: it can be restored for 30 days, then it is deleted for
good; its past runs stay.

![A workflow as a graph]({{ '/console/134-workflow-graph.png' | relative_url }})
![A run on the graph]({{ '/console/135-workflow-run-graph.png' | relative_url }})

## MCP servers you add

Under **Settings › Connections › MCP servers** an administrator adds an MCP server: a **remote** one by its HTTPS
address (with a token or header value taken from Secrets, never shown again), or a **local** one as a container image
pinned by digest, which runs like a plugin: a fresh container per call, no network unless you grant it (all of it, or
only named hosts), secrets from the vault as environment variables.

Press **Read its tools** to see what it offers. Every tool starts off. A tool its server marks read-only can be turned
on. Many servers do not mark their tools at all: if you have checked that such a tool only reads, turning it on asks you
to confirm, and the tool shows **marked by** you here and in the audit log. A tool its server says changes things
(`readOnlyHint: false`) or is destructive can never be turned on, because it would bypass approvals. When you turn a tool on, its definition is pinned: if the server later
offers a different description or arguments, the tool turns itself off (and loses your mark) until you read it again.
Changing a server's address or image turns all its tools off.

A local server runs as the unprivileged user nobody. Some images (Docker's `mcp/time` and `mcp/fetch`, for example)
keep their program where only root can reach it and fail with `Permission denied`; for those, tick **Run as the image's
own user**. The file system stays read-only, with no extra privileges and the same network. If a local server with a
host list always runs out of time, see `FORGELINE_EGRESS_PORTS` in
[Get started]({{ '/get-started/' | relative_url }}#server-settings).

A turned-on tool is named `mcp_<server>_<tool>`. Agents get it only when you add it to their tools; the assistant gets
it only when the server is marked **Offer its turned-on tools to the assistant**, and only for developers and above.
Arguments are checked against the tool's own schema before the call, every call is in the audit log, and what the tool
returns is shown to the model as untrusted data. A remote server on a private or cloud-internal address is refused
unless you tick **It is on a private network**; redirects are never followed. See
[design note 042](https://github.com/forgelinehq/forgeline/blob/main/docs/design/042-mcp-servers.md).

![MCP servers]({{ '/console/136-mcp-servers.png' | relative_url }})

## Agents you define

Under **Team → Agents** you can define named agents with instructions, skills and a short list of tools. The instructions are written in Markdown, with a live preview and advisory checks; the model receives exactly the text you wrote. **New agent** and **Edit** open the builder, where one side panel picks the model, skills, MCP server tools, tools and guardrails. **Download** exports a version as a Forgeline bundle (with each skill as an Agent Skills folder) or a Docker Agent file, with no key in either; **Import** on the Agents page brings a bundle back in, showing first what it will create and leave out. See the [Console tour]({{ '/guides/console-tour/' | relative_url }}#agents).

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
