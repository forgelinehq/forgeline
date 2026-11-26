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
| [Console basics](console-basics.md) | The sidebar and its spaces, search, the Settings area, what each role sees, the dark theme and phones. |
| [Home and approvals](home-and-approvals.md) | The first page after sign-in: what needs you, and the list of changes waiting for approval. |

## Pipelines and delivery

| Page | What it covers |
| --- | --- |
| [Write pipelines](pipelines.md) | The pipeline file: steps, `needs`, permissions, secrets and building images. |
| [Pipeline editor and runs](pipeline-editor-and-runs.md) | The list of pipelines, the editor with its live graph, versions, and the run page with logs and a failure explanation. |
| [GitHub Actions](github-actions.md) | Start, watch and re-run your GitHub workflows from the console. |
| [Versioning](versioning.md) | How the product, the HTTP API and your saved skills, agents and workflows are versioned. |
| [Agent steps in pipelines](agent-steps.md) | A pipeline step that runs the `claude` CLI in the step's container, with only what the author lists. |

## The assistant

| Page | What it covers |
| --- | --- |
| [Assistant](assistant.md) | The console panel that answers questions, explains failures and prepares pipelines, skills, agents and workflows. |
| [Assistant permissions and autopilot](assistant-permissions-and-autopilot.md) | Changing AWS as you after you approve the exact command, rules decided ahead of time, and autopilot. |
| [Assistant sandbox](assistant-sandbox.md) | A container where the assistant can run commands and code; off until an administrator turns it on. |
| [Agent engines for the assistant](assistant-agent-engines.md) | Handing a conversation to Claude Code or another coding agent over ACP. |
| [Assistant memory](assistant-memory.md) | The files the assistant keeps between conversations, and who may read and write them. |
| [MCP servers](mcp-servers.md) | Adding a remote or local MCP server so agents and the assistant can use its tools. |

## Agents and workflows

| Page | What it covers |
| --- | --- |
| [Agents](agents.md) | A named, versioned agent: instructions, skills, tools, model, tier and an optional monthly budget. |
| [Agent builder](agent-builder.md) | Making and editing an agent as parts, with the agent to talk to beside it. |
| [Agent bundles and registries](agent-bundles.md) | Exporting and importing an agent as a file, and pushing it to a container registry. |
| [Publishing agent versions](publishing-agent-versions.md) | A gate that makes a new version a candidate until an eval suite passes. |
| [Evals](evals.md) | Suites of cases that test an agent before people rely on it. |
| [Teams](teams.md) | A lead and up to eight member agents working on one goal. |
| [Schedules](schedules.md) | Everything that runs by itself, soonest first. |
| [Workflows](workflows.md) | Agents run in an order you define, with decisions, questions and approvals in between. |
| [Workflow canvas](workflow-canvas.md) | Drawing a graph workflow and watching a run drawn on it. |
| [Missions](missions.md) | Giving one agent a goal and one AWS account, in five stages with a person approving. |
| [AI use and budgets](ai-use-and-budgets.md) | What each agent and person used this month, and monthly budgets. |
| [Models and providers](models-and-providers.md) | Connecting model providers, turning models on, spend limits, fallbacks and the call log. |

## Cloud and operations

| Page | What it covers |
| --- | --- |
| [Cloud canvas](cloud-canvas.md) | Drawing what should exist in an AWS account, previewing it, and building or removing it through approval. |
| [Blueprints and environments](blueprints-and-environments.md) | Saved drawings and their code, guardrails, what Forgeline built, and temporary environments with a time to live. |
| [Infrastructure map and services](infrastructure-map-and-services.md) | Read-only views of your cloud accounts and Kubernetes clusters, and what needs a look. |
| [Runbooks](runbooks.md) | Reviewed shell scripts that check or fix servers through AWS Systems Manager. |
| [Alarm intake](alarm-intake.md) | Starting a workflow when a CloudWatch alarm goes off, through an SQS queue Forgeline reads. |
| [Traces and OpenTelemetry](traces-and-opentelemetry.md) | The trace of every run, and sending traces to your observability tool. |

## Security and access

| Page | What it covers |
| --- | --- |
| [People and roles](people-and-roles.md) | The three roles, adding people, and personal API tokens. |
| [Single sign-on](single-sign-on.md) | Signing in with a company login over OpenID Connect, with roles from groups. |
| [Approval rules](approval-rules.md) | Asking for two approvals or named approvers on the changes a rule matches. |
| [Policy packs](policy-packs.md) | Extra checks (Trivy, Checkov, Conftest) on blueprint previews. |
| [Audit log](audit-log.md) | Reading, filtering and downloading the log of who did what, and how long it is kept. |
| [Secrets and memory](secrets-and-memory.md) | Keys and tokens for pipeline steps, and what the assistant remembers about you and the system. |
| [Connections and AI access](connections-and-ai-access.md) | Testing each connection and setting what the AI may do with each service and tool. |
| [People and security](people-and-security.md) | Moved: this page only points to the five pages above. |

## Connect your accounts

| Page | What it covers |
| --- | --- |
| [Connect GitHub](connect-github.md) | Starting pipeline runs from pushes and pull requests, with the result posted back as a commit status. |
| [Connect AWS](connect-aws.md) | Looking at an AWS account and changing it through proposals a different administrator approves. |
| [Connect Google Cloud](connect-gcp.md) | Read-only listing of a Google Cloud project, folder or organization. |
| [Connect Azure](connect-azure.md) | Read-only listing of your Azure subscriptions. |
