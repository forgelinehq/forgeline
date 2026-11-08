---
title: Status
nav_order: 5
---

# Status

**Pre-alpha**, maintained by one person. Built to be tried on a laptop or a small server. Updated 2026-11-08.

## What works

Everything below is built and tested with the race detector, and the console in a real browser.

| Area | What is there |
| --- | --- |
| Pipelines | Typed YAML, steps in Docker, parallel steps, a cache, versions, live logs, a failure explained, GitHub triggers and commit statuses, GitHub Actions |
| Cloud | AWS sign-in (IAM Identity Center, profiles, IAM Roles Anywhere), several accounts, an explorer and the Watch map, Google Cloud and Azure read-only, Kubernetes read-only |
| Changes | Every change is a proposal a different administrator approves: stacks, environments, the AWS calls an agent drafted, and blueprints (OpenTofu, with exactly the previewed plan applied) |
| AI | The assistant (Claude by API key, the claude program, or a Claude Code token), agents with skills and tools, workflows with gates, missions, the canvas assistant, memory, per-service switches for the AI, an access policy per tool, budgets per agent |
| People and security | Roles, single sign-on with OpenID Connect, approval rules, an audit log with export and retention, secrets that are write-only and masked |
| Docs | This site, with search, a Markdown copy of each page and `llms.txt` |

## Checked on real services

- AWS sign-in, and an approved deploy and delete of a stack, on a real account.
- A GitHub App created and installed on github.com.
- The assistant on a real Claude Code login token, and (2026-11-08) its use counted from the real claude program's report.
- AI-proposed changes in two real AWS accounts (2026-11-07): one proposal per account, approved by a different
  administrator, applied exactly as approved.
- The Watch map on two real accounts (2026-11-08): stacks, their parts and what they kept after a rollback.
- A blueprint previewed with real OpenTofu 1.13.9 on a Raspberry Pi and, after a different administrator approved it,
  built in a real account (2026-11-08).
- A mission with the real Claude model on the Pi (2026-11-08): it reported correctly, citing the lookup it ran.

## Not checked against the real service yet

- A real GitHub webhook delivery and commit status.
- The assistant's API-key mode against the real Anthropic API.
- Single sign-on against a real provider (Google, Entra ID, Okta); built and tested against a stand-in.
- A blueprint with an app (ECS) built on AWS: it needs a scoped IAM permission ([note 035](https://github.com/forgelinehq/forgeline/blob/main/docs/design/035-blueprints.md)).
- A mission that files a proposal, with the real model.
- TLS behind a real proxy, and more than one server sharing a database.

## Not built yet

See the [roadmap]({{ '/roadmap/' | relative_url }}). The next items are: a local small-model option for the assistant (and onboarding by
conversation if it proves reliable), removing what a blueprint built,
forwarding the audit log to a SIEM, and a lead agent that hands out work. Coding engines other than Claude Code (Codex,
Copilot) come later.
