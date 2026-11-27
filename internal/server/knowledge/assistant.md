---
title: Assistant
parent: Guides
nav_order: 30
---

# Assistant

The assistant is a panel in the console that you ask questions. It sees the page you are on, looks things up with
read-only tools, explains failed runs, reviews pipelines, and can prepare a pipeline, skill, agent or workflow for you to
confirm. By default it cannot change anything by itself.

Use it when you want an answer about what you are looking at, or a first draft of something to set up. It is not the same
as an [agent step](agent-steps.md), which is a step in a pipeline that runs inside a container.

## At a glance

| | |
| --- | --- |
| Where in the console | The **Assistant** button in the top bar; set up under **Settings → AI → Assistant model** (also reached from **Settings → Connections → Claude**) |
| Needs | A model: an administrator-enabled model from [Models and providers](models-and-providers.md), an Anthropic API key, a Claude Code login token, or a local `claude` or `copilot` CLI |
| Design notes | [008](../design/008-assistant.md), [024](../design/024-assistant-agent-surface.md), [028](../design/028-assistant-acts-with-confirmation.md), [052](../design/052-assistant-that-works-like-a-colleague.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Ask the assistant | Developer | 20 questions a minute per person |
| Confirm or dismiss a card it prepared | Developer | Only the person it was prepared for; once only; not after 30 minutes |
| Get a card that saves an agent, skill or workflow | Developer | A developer's card cannot give an agent an administrator's tool |
| Choose the sign-in, save it and **Test** it | Administrator | A developer can open the page but not change it |
| Install an agent program it needs (see [Agent engines](assistant-agent-engines.md)) | Administrator | |
| Read your saved conversations | Developer | Only your own; an administrator cannot read another person's |

## How it works

1. You open the panel. It takes the page you are on (a run, a pipeline, the infrastructure) as context. It never sees
   secret values.
2. The assistant asks the server for read-only lookups. Each one shows as a card: what it asked and a cut of what came
   back. Open a card to read it.
3. The answer appears as cards, with tables shown as tables. A change it filed appears as a proposal card that follows
   the proposal's status. The answer arrives in one piece, not word by word.
4. **Stop** cancels the question. Every answer is saved in your conversations.

Text the assistant reads from logs is data, not instructions. On a run whose log said "reply PWNED", it explained the
real error and pointed out the planted line.

![The assistant prepares an agent for you to confirm]({{ '/console/assistant-writes-agent.png' | relative_url }})

### Where it runs

The panel docks at the side (drag its edge, or use the wide button) or floats as a window you can move and resize. It
remembers your choice.

## Set it up

1. Open **Settings → AI → Assistant model** (or **Settings → Connections**, then **Claude**).
2. Choose how to sign in:
   - **A model from Models and providers**: any model an administrator turned on (Anthropic, OpenAI, Gemini, Azure
     OpenAI, AWS Bedrock, or a local server such as Ollama).
   - **Anthropic API key**: stored in the vault and billed to that key. Best for teams.
   - **Claude Code login token**: paste the output of `claude setup-token` once. It is stored as the secret
     `CLAUDE_CODE_OAUTH_TOKEN`, the same one agent steps use. The `claude` program runs in a scratch home directory
     with only that token, so the server user's own login is not touched.
   - **Local `claude` CLI**: uses the login of the person running the server. Meant for a single-person setup.
   - **Local `copilot` CLI** (GitHub Copilot): run `copilot login` as the server's user first. See below.
   - **Claude Code as the agent** and **An agent over ACP** give the assistant a shell and files inside a sandbox; they have
     their own pages: [Agent engines](assistant-agent-engines.md) and [Assistant sandbox](assistant-sandbox.md).
3. Choose **Save**, then **Test**. It sends one short question and shows the reply ("It answered: ...").

### The Copilot CLI

Forgeline runs `copilot` with no tools (`--available-tools` names a tool that does not exist, and it refuses any reply
whose output shows the model was offered tools), no MCP servers and no custom instructions, in an empty scratch folder. Of
the server's environment it passes on only proxy and certificate settings (`HTTPS_PROXY`, `HTTP_PROXY`, `ALL_PROXY`,
`NO_PROXY`, `NODE_EXTRA_CA_CERTS`, `NODE_USE_SYSTEM_CA`, `SSL_CERT_FILE`, `SSL_CERT_DIR`, upper or lower case), which a
corporate network needs. The question goes on stdin when the CLI reads a piped prompt (1.0.107 does), and with `--prompt`
on older CLIs (1.0.34), where it then shows in the process list; flags an older CLI does not list in `--help` are left
out. Each question uses one Copilot premium request (a free plan has only a few a month); leave the model empty to let
Copilot choose, or name one. Meant for a single-person setup.

## Ask it to write things

The assistant can **write skills, agents and workflows** for a developer or administrator ("write an agent that explains failed
runs", "make a weekly workflow with the cost watcher"). Nothing is created by asking.

1. It prepares a card that says, in words the server works out (not the model's), what saving would do: which tools the
   agent may use, whether it only reads or may also draft changes for approval, which skills it follows, when a workflow
   runs and which stages wait for an administrator.
2. **Confirm** saves it as version 1, through the same checks as the forms on the Agents page. The audit log says it was
   written by the assistant and confirmed by you.

Once a workflow exists, the assistant can prepare a **run** of it as a card too, so "build it and run it" needs one
Confirm. The card says which workflow and version will start, with which role its agents run, and that the run uses the
model provider and counts against their budgets; anything an agent proposes still waits for a different administrator. If
the workflow is changed or turned off before you press Confirm, the card refuses to start it. The run is yours, in the
audit log and on the workflow's page.

When one answer prepares several cards, they come **one at a time**: the next appears after you press Confirm or Dismiss
on the one before, with a line saying how many there are. After the last one, your answers go back to the assistant as one
message, so it carries on with the rest of what you asked.

## Plans, questions and long results

- **Plans.** For anything that takes several steps the assistant shows a checklist that it ticks off as it works. A
  question may use up to 40 lookups and 5 minutes; the time it spends waiting for you does not count. A message may be up
  to 4,000 characters. At the limit it says
  what is done and what is left, and you can say "continue".
- **Questions.** When the answer changes what it would do and it cannot find it out, it stops on a question card with two
  to four options and a box for your own words, instead of guessing (for example when two clusters match "Dev01"). The run
  waits up to ten minutes. An agent run never asks; a question that nobody can answer makes the assistant state its
  assumption and carry on. It does not ask permission to read.
- **Long results.** A long answer (a workflow run's report, a big list) is cut at about 12 KB so one lookup cannot fill the
  conversation. The cut says where it stopped and the assistant calls again with an **offset** to read on. If it says a
  block "was cut off", ask it to read the rest.

## Change what already exists

The assistant (and Claude Code or an ACP agent working through it) can update what you already have, not only create new
things. It calls the console's own API as you with the `forgeline_api` tool. It can read anything, and change agents (a new
version, archive, restore), skills, teams, workflows, pipelines and memory. The same checks as the console apply (your
role, the validation, the audit entry), and **a change asks you first** with the exact request, unless
[autopilot](assistant-permissions-and-autopilot.md) is on. It cannot touch users, API tokens, secrets, permission, sandbox and autopilot settings,
the model settings and providers, sign-in, approvals, the marketplace, or anything that starts, runs, pushes, publishes or
sets a budget: those stay in the console.

## When it says it cannot: the doctor

If something stops the assistant (no shell, an AWS account that is protected, memory off, a model that cannot make real
tool calls), it checks its own setup with the **doctor** and tells you what is holding it back. Where an administrator can
fix it with one answer, it prepares a card: **Turn on the sandbox** or **Allow changes to AWS account ... with my
approval**. Press Confirm and it carries on. Neither card widens anything beyond what the settings pages already offer: a
change to AWS still shows you the exact command first, and IAM, Organizations, accounts, SSM and STS always ask.

## Security model

- **Read-only by default.** The assistant runs on the server with no tools of its own; it can ask the server for read-only
  lookups. Changes are cards you confirm, proposals a different administrator approves, or [AWS changes you approve
  command by command](assistant-permissions-and-autopilot.md).
- **Data, not instructions.** Text from logs and cloud data is shown to the model as data.
- **No secret values.** The assistant never sees them.
- **Audited by page and length, not by text.** Questions are rate limited to 20 a minute per person.
- **Your conversations are yours.** An administrator gets a 404 for someone else's.

## Limits and verification

{: .warning }
**Checked mostly against a stand-in for the `claude` program; a real model only in part (below).** The answer arrives in one piece, and the
surface was checked with that stand-in (see [note 024](../design/024-assistant-agent-surface.md)). How well each real model
chooses to plan and when to ask has not been measured yet.
On 2026-11-27 (main at `487f373`) a developer test user asked a real model (Claude Code as the agent) six questions in the console on the UAT instance: failed run, AWS contents and a planted instruction in a log were answered correctly; it invented a second Memory page once and could not name the path of Evals. Not run: setting up the sign-in, confirming a card as an administrator. See the [note 2026-11-27](../testing/2026-11-27-uat-assistant-agents.md).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| **Test** fails | The key or token is wrong, or the local `claude` or `copilot` program is not installed or not signed in for the server's user. |
| A card refuses to start a workflow | The workflow was changed or turned off after the card was prepared. |
| A card does nothing after 30 minutes | A card cannot be confirmed after 30 minutes; ask again. |
| It stops partway through a long task | A question may use up to 40 lookups and 5 minutes; say "continue". |

## Related

- [Assistant permissions and autopilot](assistant-permissions-and-autopilot.md)
- [Assistant sandbox](assistant-sandbox.md)
- [Assistant memory](assistant-memory.md)
- [Agent engines: Claude Code and ACP agents](assistant-agent-engines.md)
- [MCP servers](mcp-servers.md)
- [Models and providers](models-and-providers.md)
- [People and security](people-and-security.md)
