---
title: Guides
nav_order: 3
has_children: true
---

# Guides

One page for each feature: what it is, who can use it, how it works, its limits and how to fix it when it does not.
Each page says what was checked against the real service and what was not.

## Console

| Page | What it covers |
| --- | --- |
| [Console basics](console-basics.md) | The web console is where you do almost everything in Forgeline. |
| [Home and approvals](home-and-approvals.md) | **Home** is the first page after sign-in. |

## Pipelines and delivery

| Page | What it covers |
| --- | --- |
| [Write pipelines](pipelines.md) | A pipeline is a list of steps. |
| [Pipeline editor and runs](pipeline-editor-and-runs.md) | This page covers the console side of pipelines: the list of pipelines, the editor with its live graph, versions, and the run page with its logs and its explanation of a failure. |
| [GitHub Actions](github-actions.md) | Use **Actions** in the console to start, watch and re-run your GitHub workflows without leaving Forgeline. |
| [Versioning](versioning.md) | Forgeline versions three different things, and they move independently: the product, the HTTP API, and the skills, agents and workflows you save. |
| [Agent steps in pipelines](agent-steps.md) | An agent step is a pipeline step that runs the vendor's own `claude` CLI inside the step's container. |

## The assistant

| Page | What it covers |
| --- | --- |
| [Assistant](assistant.md) | The assistant is a panel in the console that you ask questions. |
| [Assistant permissions and autopilot](assistant-permissions-and-autopilot.md) | The assistant can change AWS as you, after you approve the exact command. |
| [Assistant sandbox](assistant-sandbox.md) | When no lookup answers, the assistant can run commands and write and run code in a sandbox: a container Forgeline starts for you. |
| [Agent engines for the assistant](assistant-agent-engines.md) | Instead of Forgeline's own loop, the assistant can hand a conversation to a coding agent: the real Claude Code, or another agent over the Agent Client Protocol (ACP): Antigravity, GitHub Copilot CLI, Codex, or your own command. |
| [Assistant memory](assistant-memory.md) | The assistant keeps files: how an environment is laid out, what a name like "Dev01" means, a procedure that worked, how you like things done. |
| [MCP servers](mcp-servers.md) | An administrator can add an MCP (Model Context Protocol) server so agents and the assistant can use its tools. |

## Agents and workflows

| Page | What it covers |
| --- | --- |
| [Agents](agents.md) | An agent is a named, versioned definition: instructions, skills it follows, a short list of tools, a model, a tier and an optional monthly budget. |
| [Agent builder](agent-builder.md) | The builder is where you make and edit an [agent](agents.md). |
| [Agent bundles and registries](agent-bundles.md) | You can take an [agent](agents.md) out of Forgeline as a file, bring one in, and push one to a container registry so anyone with access can run it with `docker agent run`. |
| [Publishing agent versions](publishing-agent-versions.md) | By default, saving an agent makes the new version current at once. |
| [Evals](evals.md) | An eval suite tests an agent before people rely on it. |
| [Teams](teams.md) | A team is a **lead** and up to eight **members**, all [agents](agents.md). |
| [Schedules](schedules.md) | **Schedules** lists everything that runs by itself: workflows on their schedule and goals given to teams at set times, soonest first. |
| [Workflows](workflows.md) | A workflow runs agents in an order you define, with decisions, questions for people and approvals in between. |
| [Workflow canvas](workflow-canvas.md) | The canvas is where you draw a graph [workflow](workflows.md), n8n-style, and where a run is drawn afterwards. |
| [Missions](missions.md) | A mission gives one agent a goal in your own words, for example "check the payments service for anything fragile and fix what you can", and one AWS account to look at. |
| [AI use and budgets](ai-use-and-budgets.md) | **Usage and budgets** shows what each agent used this month (tokens, model time and calls, and the cost where the `claude` program reports it) and what people used asking the assistant themselves. |
| [Models and providers](models-and-providers.md) | Agents, teams, workflows and the assistant need a model to answer. |

## Cloud and operations

| Page | What it covers |
| --- | --- |
| [Cloud canvas](cloud-canvas.md) | The canvas is where you draw what should exist in an AWS account: a front door, apps on ECS, tables, file buckets, queues, logs, databases, caches and secrets, joined by links. |
| [Blueprints and environments](blueprints-and-environments.md) | A **blueprint** is a saved drawing of what should exist in one AWS account, with the OpenTofu code it generates and a plain-words summary of what it means. |
| [Infrastructure map and services](infrastructure-map-and-services.md) | These pages show what is running in your cloud accounts, read-only. |
| [Runbooks](runbooks.md) | A runbook is a reviewed shell script that checks or fixes something on your servers, run through AWS Systems Manager (SSM). |
| [Alarm intake](alarm-intake.md) | A CloudWatch alarm can start a workflow. |
| [Traces and OpenTelemetry](traces-and-opentelemetry.md) | Every assistant chat, agent run, workflow run, team run, mission and pipeline run is recorded as a **trace**: what started it, who, how long it took, its model calls and tool calls, tokens and cost, and whether it failed. |

## Security and access

| Page | What it covers |
| --- | --- |
| [People and roles](people-and-roles.md) | Every person who signs in to Forgeline has one of three roles: viewer, developer or administrator. |
| [Single sign-on](single-sign-on.md) | People can sign in with your company login instead of a Forgeline password: Google Workspace, Microsoft Entra ID, Okta, Keycloak or any provider that speaks OpenID Connect. |
| [Approval rules](approval-rules.md) | Every change to your cloud already needs an administrator who did not propose it. |
| [Policy packs](policy-packs.md) | Every blueprint preview is checked by Forgeline's built-in guardrails. |
| [Audit log](audit-log.md) | The audit log records who did what in Forgeline, including everything that was refused and every failed sign-in. |
| [Secrets and memory](secrets-and-memory.md) | Two Settings pages hold things Forgeline keeps for you. |
| [Connections and AI access](connections-and-ai-access.md) | **Settings → Connections** lists every system Forgeline is connected to: GitHub, AWS, the Docker host, Claude and the rest. |

## Connect your accounts

| Page | What it covers |
| --- | --- |
| [Connect GitHub](connect-github.md) | Pushes to chosen branches start runs of the `forgeline.yaml` at the repository root, and the result is posted back as a commit status. |
| [Connect AWS](connect-aws.md) | Forgeline connects to one AWS account to look at it (stacks, instances, resources, spend) and to change it through proposals that a different administrator approves. |
| [Connect Google Cloud](connect-gcp.md) | Forgeline can list the resources of a Google Cloud project, folder or organization, read-only. |
| [Connect Azure](connect-azure.md) | Forgeline can list the resources of your Azure subscriptions, read-only. |

