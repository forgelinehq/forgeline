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

## Open product bugs (not fixed here)

1. **Deleting a team does not remove its schedules.** They come back, with the old runs, when a team with the same name is made, and
   while the team does not exist they cannot be listed or removed.
2. **Proposing a blueprint twice** for one ready preview files two pending changes.
3. **A budget refusal answers HTTP 502** to an API caller (`POST /v1/ai/agents/{id}/chat`); it is a refusal and should be a 4xx.
4. **The usage page shows `$0.00`** for an agent whose provider call log shows a cost.
5. **A missing image is explained wrongly.** A step that cannot pull its image exits 125 ("pull access denied") and the failure
   explanation says something rejected the credentials.
6. **The Spend button is shown to a viewer**, whose click would be refused (`developer is required`).
7. **Delete does nothing on a clicked canvas node** (it loses keyboard focus); it works after Tab, or with **Remove node**.
8. **Message wording:** saving a workflow run as a developer with an `aws_script` agent says to run it as a developer; the error for a
   role that cannot be passed points to a guide heading that does not exist.
9. **`GET /v1/runs?pipeline=NAME` filters after cutting the page**, so a filtered page can be short and paging can skip runs.
10. **`forgeline onboard` help** says the token may be an administrator's API token; personal tokens work only on `/mcp`.
11. The assistant invented a separate "Settings → Memory" page, and could not name the path to Evals.

## What this test did not cover

Anything that changes AWS or GitHub beyond the one cheap cycle, anything that costs money for long, SSO, Kubernetes, Google Cloud,
Azure, registries, runbook runs (no test instance), alarm intake setup, approval rules and policy packs (none on the instance, and
the test user may not add them), administrator-only console pages (the test user is a developer), and the guide pictures beyond a
few. The next test should use a second test user who is an administrator, on a branch of the instance that can be reset.
