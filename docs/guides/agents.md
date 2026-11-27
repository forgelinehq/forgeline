---
title: Agents
parent: Guides
nav_order: 36
---

# Agents

An agent is a named, versioned definition: instructions, skills it follows, a short list of tools, a model, a tier and an
optional monthly budget. Teams, workflows, missions and schedules run agents. Every agent works as the person who started
it, so it can never do more than that person could.

Use agents to give a repeatable job a fixed shape: "explain failed runs", "watch costs", "check security findings".

## At a glance

| | |
| --- | --- |
| Where in the console | **Agents → Agents** (the **Library** tab, in the same space, has ready-made ones) |
| Needs | A model (by default the assistant's) |
| Design notes | [045](../design/045-agent-studio.md), [046](../design/046-agent-export.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See agents and talk to one | Developer | It answers with its own tools, as you |
| Create an agent, save a new version, roll back | Developer | A version a developer saved runs with at most a developer's rights, whoever runs it ([why](../design/054-developers-write-agents.md)). With a publishing gate, only an administrator rolls back |
| Turn an agent off or on | Developer | What an administrator turned off, only an administrator turns on |
| Delete (archive) or restore an agent | Developer | |
| Create or change a skill | Developer | Skills are listed on the Agents page; a skill no agent version uses can be deleted |
| Set which agent reports to which | Developer | Describes the team; gives no agent power over another |

## How it works

1. An agent has **instructions** (written in sections, with advisory checks, or as plain Markdown), **skills** it follows,
   a short list of **tools** shown in plain words, a **model**, and a **tier**.
2. Every save is a **version**. History is append-only. A version pins its skills at fixed versions. You can roll back, and
   you can talk to an agent beside its page.
3. An agent cannot gain a tool or a higher tier without a new saved version, which is audited.
4. When something runs the agent, it uses the version pinned there. By default it asks the assistant's model; an agent can
   ask its own (a quick one for routing, a stronger one for analysis).

![An agent's page]({{ '/console/agents-page.png' | relative_url }})

### Tiers

| Tier | In the console | What it may do |
| --- | --- | --- |
| T0 | **Advises** | Reads and explains |
| T1 | **Proposes** | Can also draft changes, which a person approves |

A proposal tool on a T0 agent is left out when a bundle is imported.

### Tools

Tools are Forgeline's own lookups, plugin tools, and [MCP server](mcp-servers.md) tools named `mcp_<server>_<tool>`. Each
tab in the builder says whether a tool reads or asks a person to approve. Some tools are limited by the role that runs the
agent:

- `aws_script` and `search_audit` are for administrators only: a developer cannot give an agent either, and an agent
  that follows anything a developer wrote cannot use them.
- `call_aws` runs one read-only AWS CLI call (describe, list and get only; no secrets, data or logs) for developers and
  administrators.
- For security reviews, `guard_findings` (Forgeline's own checks), `inspector_findings` (Amazon Inspector's active
  findings, worst first, with CVE, package, fixed version, exploit and EPSS) and `aws_resources` are open to viewers and
  summarise, so they fit one answer.
- `run_runbook` (under *Security and health*) lets an agent run reviewed [runbooks](runbooks.md).
- `org_memory` reads the organisation's shared notes ([Assistant memory](assistant-memory.md)).

A workflow never runs as an administrator, so its agents may use only the tools a viewer or a developer may. Saving a
workflow whose agent has a tool its run-as role may never use is refused, with the tool named. See
[Workflows](workflows.md).

## Create an agent

1. Open **Agents → Agents** and choose **New agent**, or install one from the **Library**.
2. Fill in the [builder](agent-builder.md): instructions, skills, tools, model and guardrails.
3. Choose **Create agent**. Later saves are **Save version**, each with a note on what changed and why.

You can also ask the [assistant](assistant.md) to write one; it prepares a card you confirm.

## Delete and restore

**Delete** on an agent's page archives it: it is turned off, anyone who reported to it reports to people again, and it is listed under **Archived (n)** at the bottom
of the Agents page. It can be restored for 30 days, after which it is deleted for good. An agent that something still
uses cannot be archived; the message names what, for example `<agent> is still used by ...; turn those off or give them
another agent first`.

## Security model

- **As the asker.** An agent's tools run as the person who started it, with their role.
- **Versions are pinned.** A version cannot gain a tool or a higher tier without a new saved version, which is audited.
- **Tiers.** Only T1 agents can draft changes, and a different administrator approves them. In the builder's playground, a T1
  agent's proposal is shown as a draft, never filed.
- **Budgets.** An agent over its [budget](ai-use-and-budgets.md) stops before its next model call.

## Limits and verification

{: .warning }
**Checked on a real model only in part.** The Anthropic provider was checked on the Pi with the real API on 2026-11-18
([note 045](../design/045-agent-studio.md)). Gemini, Azure OpenAI and AWS Bedrock were tested against stand-ins only, not the real services; see
[Models and providers](models-and-providers.md).
On 2026-11-27 (main at `487f373`) on the UAT instance a developer talked to an agent on a real model (Haiku 5.5), watched versions, rollback, archive and restore, and the role refusals in the console and API. See the [note 2026-11-27](../testing/2026-11-27-uat-assistant-agents.md).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| An agent cannot be deleted | Something still uses it: "... is still used by ...; turn those off or give them another agent first". |
| Saving a workflow is refused with a tool named | The tool is not allowed for the workflow's run-as role. |
| An agent stopped | It used up its monthly budget; its card says **Stopped**. |
| An imported agent lacks a tool | The tool needs something this server does not have, or it is a proposal tool on a T0 agent. |
| An agent did not read a note | It lacks the `org_memory` tool. |

## Related

- [Agent builder](agent-builder.md)
- [Agent bundles](agent-bundles.md)
- [Evals](evals.md) and [Publishing agent versions](publishing-agent-versions.md)
- [Teams](teams.md), [Workflows](workflows.md), [Missions](missions.md)
- [Agent steps in pipelines](agent-steps.md)
