---
title: Status
nav_order: 5
---

# Status

**Pre-alpha**, maintained by one person. Built to be tried on a laptop or a small server. Updated 2026-11-27.

## What works

Everything below is built and tested with the race detector, and the console in a real browser.

| Area | What is there |
| --- | --- |
| Pipelines | Typed YAML, steps in Docker with all Linux capabilities dropped and a trust level per run (pull requests get no secrets), parallel steps, a cache, versions, live logs, a failure explained, GitHub triggers and commit statuses, GitHub Actions, a source repository for console runs, image builds with BuildKit that push to ECR only for trusted pushes |
| Blueprints | A canvas of blocks (front door, apps on ECS or Fargate Spot, tables, buckets, queues, logs, MySQL and Postgres, Redis, secrets, an agent on Bedrock AgentCore, things that already exist), OpenTofu previews, exactly the previewed plan applied after approval, removal through the same approval, a block's GitHub repository suggested from its Dockerfiles, running a newly pushed image in one step |
| Templates | Reviewed, versioned OpenTofu modules added like blocks: Lambda function, Step Functions workflow, data pipeline (Firehose to S3 with Athena), EKS cluster with namespaces; a Resource block for any AWS resource type an administrator allows |
| Guardrails | Every preview is checked before it can be sent: built-in deny and warn rules, plus policy packs (Trivy, Checkov, your own Conftest rules) |
| Cloud | AWS sign-in (IAM Identity Center, profiles, IAM Roles Anywhere), several accounts, the Watch map, a page per AWS service, Google Cloud and Azure read-only, Kubernetes read-only, what Forgeline built checked against AWS |
| Changes | Every change is a proposal a different administrator approves: stacks, environments, blueprints and the AWS calls an agent drafted, with approval rules (two approvals, named approvers) |
| Models | Providers: Anthropic, OpenAI, Gemini, Azure OpenAI, AWS Bedrock and OpenAI-compatible servers (Ollama, vLLM, OpenRouter…), with keys in the vault, prices, a monthly limit and a fallback per provider, and a live call log. Each agent can have its own `provider/model` |
| AI | The assistant (with lookups, memory, and cards that write skills, agents and workflows for you to confirm), agents with skills, tools and versions, teams with a lead that splits the work, workflows drawn on a canvas (agents, lookups, decisions, questions for people, merges and loops), missions, MCP servers, per-service switches for the AI, an access policy per tool, budgets per agent, deleted agents kept 30 days |
| Assistant | Plans and asks, runs commands in a sandbox container (off until an administrator turns it on), autopilot, memory as files, Claude Code or another agent over the Agent Client Protocol as its engine |
| Quality and traces | Evals with a publish gate, ready-made attack cases and an `eval` pipeline step; every run as a trace, sent to an OpenTelemetry backend |
| Operations | Runbooks: reviewed scripts run on servers through SSM, every fix approved by a different administrator; a CloudWatch alarm can start a workflow |
| Agent sharing | Download an agent as a bundle or a Docker Agent file, import a bundle, push to a registry after approval |
| Library | Ready-made agents, skills, teams, tools (MCP servers), guardrail packs and environment templates, installed as versions you can review |
| People and security | Roles, single sign-on with OpenID Connect, approval rules, an audit log with export and retention that records every secret grant, secrets that are write-only and masked |
| Docs | This site, with search, a Markdown copy of each page and `llms.txt` |

## Checked on real services

- AWS sign-in, and an approved deploy and delete of a stack, on a real account.
- A GitHub App created and installed on github.com.
- The assistant on a real Claude Code login token, and its use counted from the real claude program's report.
- AI-proposed changes in two real AWS accounts (2026-11-07), each approved by a different administrator and applied
  exactly as approved.
- The Watch map on two real accounts (2026-11-08).
- Blueprints with real OpenTofu on a Raspberry Pi and a real account: a queue, a table and a bucket built and removed
  (keeping the data, then everything); an app behind a front door; GitHub Actions building and pushing with the role the
  blueprint made (2026-11-08 to 2026-11-12).
- Templates on a real account (2026-11-15 to 2026-11-17): a Lambda function, a Step Functions workflow, a data pipeline
  queried with Athena, and an EKS cluster with namespaces, each built after approval and removed.
- Policy packs (Trivy, Checkov, Conftest) on real previews on the Pi (2026-11-17).
- A mission with the real Claude model on the Pi (2026-11-08): it reported correctly, citing the lookup it ran.
- Agent workflows drawn on the canvas, run with the real model on the Pi, including a model per agent and starting from
  a signed webhook and from a failed pipeline run (2026-11-17).
- The Anthropic provider with the real API on the Pi (2026-11-18): the Test button, and an agent answering through it.
- Traces sent to a local Jaeger (2026-11-20).
- The Docker Agent export loaded by the real `docker-agent` program.
- A short trial of a Gemini model in the assistant on the Pi (2026-11-22).
- The Copilot CLI over the Agent Client Protocol (2026-11-23): its handshake and a tool call, and installing it in the
  sandbox container.
- Step capabilities: eleven typical steps with real Docker on amd64 (2026-11-26,
  [results]({{ '/testing/2026-11-26-step-capabilities/' | relative_url }})).
- A real CloudWatch alarm starting a workflow, a runbook check by an agent and an approved fix on a real instance
  (2026-11-26, [results]({{ '/testing/2026-11-26-alarm-runbook/' | relative_url }})).
- The guides followed as a user on the Pi (2026-11-27): the console, pipelines, reading GitHub Actions, the assistant,
  agents, evals, teams, workflows, missions, traces and security refusals, and one blueprint built and removed on a real
  account ([summary and gaps]({{ '/testing/2026-11-27-uat-summary/' | relative_url }})).

## Not checked against the real service yet

- The OpenAI, Azure OpenAI, AWS Bedrock and OpenAI-compatible providers (tested against stand-ins).
- A real GitHub webhook delivery and commit status; starting or cancelling a real GitHub Actions workflow.
- Single sign-on against a real provider (Google, Entra ID, Okta); built and tested against a stand-in.
- Codex and Antigravity as the assistant's agent.
- Pushing an agent to a hosted registry (checked with a local one), and traces sent to a hosted backend.
- Approval rules with real second approvers on a running server, and most administrator-only pages in a user test.
- The Agent block on Bedrock AgentCore: the test account's AgentCore quota is zero.
- TLS behind a real proxy, and more than one server sharing a database.

## Not built yet

See the [roadmap]({{ '/roadmap/' | relative_url }}). Among the larger pieces: memory, process and CPU limits for pipeline
steps; scanner steps, signing and SBOMs; canary and blue-green delivery; importers for Jenkins and GitHub Actions;
publishing agents (an MCP toolset with agent keys, an A2A card); comparing models and trying unsaved drafts in the agent
builder; code mode in a local sandbox. Google Cloud and Azure blocks come after the AWS work is done.
