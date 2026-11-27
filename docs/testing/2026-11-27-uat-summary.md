---
title: UAT of the guides on the real instance, 2026-11-27
---

# What a user-acceptance test of the guides found

Every guide was followed step by step on the maintainer's UAT instance (real AWS, the GitHub App, a model provider and the
assistant), as a developer test user, on `main` at `487f373`, using the `feature-uat` skill. Four notes have the steps and
results: [console, pipelines, GitHub and AWS](2026-11-27-uat-console-pipelines.md), [assistant, agents, evals and
budgets](2026-11-27-uat-assistant-agents.md), [teams, workflows, schedules, missions and traces](2026-11-27-uat-workflows-teams.md),
and [cloud canvas and security](2026-11-27-uat-cloud-security.md). Account numbers and addresses are left out.

## Result

| Area | Guides | Passed | Guide was wrong | Not testable there |
| --- | --- | --- | --- | --- |
| Console, pipelines, GitHub, AWS reads | 11 | 77 | 4 | 11 groups (they change AWS or GitHub, cost money or change instance settings) |
| Assistant, agents, evals, budgets | 12 | 61 | 5 | builder and import dialogs, registries, settings changes |
| Teams, workflows, schedules, missions, traces | 6 | 50 | 4 | firing schedules, hostile-model cases, OTLP |
| Cloud canvas, security, audit | 11 | 67 | 3 | rules and packs (none on the instance), SSO, runbook runs |

The real-AWS cycle (draw a queue, a table and a log group; preview with OpenTofu; send for approval; approve; build; remove in two
approved steps) worked end to end, and read-only checks afterwards found nothing left in AWS.

**What the guides got wrong, now fixed:** a button name (**Make the team**, **Schedule a goal**), who may open a page (the agent
builder, the audit log and the connection Activity tab need an administrator), what a refusal says (a personal token on `/v1`
answers `sign in required`), a stale path in a budget message, an image that is not on the instance, and several missing errors,
limits and Limits-and-verification notes. Each tested guide's callout now says the date, the commit and what ran.

**Security promises that held:** a viewer and a developer got the documented 403s; a request without `X-Forgeline-Request` was
refused; an approver had to differ from the proposer; the plan applied was the one previewed; a secret printed by a step showed as
`***`; a disabled account's session and tokens stopped at once; a step with `network: none` reached nothing; a planted instruction in
a log was named as log text and not followed; an unsigned webhook got 401.

## Product bugs, and what became of them

Found by this test; fixed afterwards with a test for each that fails without its fix, except where noted.

| # | Bug | Result |
| --- | --- | --- |
| 1 | Deleting a team left its schedules and runs, which came back with a team of the same name | Fixed: the team's schedules and run history are removed with it |
| 2 | Proposing a blueprint twice files two pending changes | By design: an existing test files the same preview twice to prove a stale second plan cannot run. Documented in the guide |
| 3 | A budget refusal answered HTTP 502 | Fixed: `429` |
| 4 | The usage page showed `$0.00` for calls that cost a fraction of a cent | Fixed: four decimals under one cent |
| 5 | A missing image was explained as a rejected credential | Fixed: the pull error in the output is recognised first |
| 6 | The Spend button looked available to a viewer | Not a bug: it is disabled; it now says why |
| 7 | Delete did nothing on a clicked canvas node | Fixed: the clicked node keeps the keyboard focus (checked in a browser) |
| 8 | Two misleading messages (a developer's run told to run as a developer; a guide heading that does not exist) | Fixed |
| 9 | `GET /v1/runs?pipeline=NAME` filtered after cutting the page | Fixed: pages are read until the pipeline has a full page of its own runs |
| 10 | `forgeline onboard` help said an administrator's API token works | Fixed: only the server token does |
| 11 | The assistant invented a Memory page and could not name Evals | Fixed: its page list now names every tab of the console, and a test keeps it so |

## What this test did not cover

Anything that changes AWS or GitHub beyond the one cheap cycle, anything that costs money for long, SSO, Kubernetes, Google Cloud,
Azure, registries, runbook runs (no test instance), alarm intake setup, approval rules and policy packs (none on the instance, and
the test user may not add them), administrator-only console pages (the test user is a developer), and the guide pictures beyond a
few. The next test should use a second test user who is an administrator, on a branch of the instance that can be reset.
