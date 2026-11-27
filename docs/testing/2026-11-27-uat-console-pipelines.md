---
title: Console, pipelines, GitHub and AWS guides on the UAT instance, 2026-11-27
---

# Console basics, pipelines, agent steps, GitHub, AWS and connections, run as a new user

Eleven guides were followed step by step on the maintainer's UAT instance (a Forgeline on a Raspberry Pi with real AWS, the GitHub
App and the assistant), which ran main at `487f373`. All steps were made as the developer test user the developer test user, through the
console in a browser and through the API where a guide names it. A viewer test user `uat-a-viewer` was created for the role
checks and deleted afterwards. Only Forgeline objects named `uat-a-...` were made, and they were removed. No AWS or GitHub change
was made. Account numbers are masked. Results are appended guide by guide.

Result words: pass; guide wrong (fixed in the same change); product bug; not testable here.

## 1. Console basics

Run as the developer test user (developer) and `uat-a-viewer` (viewer).

| Step | Result |
|---|---|
| Sidebar has Home, Agents, Workflows, Delivery, Cloud, Operations, Security; Settings at the bottom | pass |
| Tabs of each space against the guide's table (Home: Today, Approvals; Agents: six; Workflows: two; Delivery: two; Cloud: five; Operations: five; Security: Findings, Approval rules, Policy packs, Resource types, and no Audit log for a developer) | pass |
| Viewer sidebar: Home, Delivery, Cloud (Map only), Operations (Services, Infrastructure, Kubernetes), Security (Findings); no Settings link | pass |
| Ctrl K lists pages with nothing typed; `sec` finds Secrets, Findings and more; `wfs` finds Workflows; `zzzqq` shows "Nothing matches"; `/` opens search when no box has focus | pass |
| Viewer search lists pages and pipelines, not agents or workflows | pass |
| Settings shows Workspace, Connections, AI and Developer with a one-line note each | pass |
| Theme button switches to dark and the choice survives a reload; on a 390 px screen the sidebar hides and the menu button shows | pass |
| Role refusal: viewer opens People, Audit, Settings and gets "You do not have access to this page"; viewer `GET /v1/ai/agents` and `POST /v1/pipelines` answer 403 "your role (viewer) cannot do this; developer is required" | pass |
| Security: `POST /v1/pipelines` without `X-Forgeline-Request` answers 403 "missing X-Forgeline-Request header" | pass |
| Troubleshooting row "Creating an API token fails with sign in to the console as a person": a personal token used on `POST /v1/tokens` answers 401 "sign in required", not the 403 text. A personal token works only on `/mcp`, so it never reaches that check. The 403 text is for the server token: `POST /v1/tokens` with the server token, run on the server, answered `403 sign in to the console as a person to create a token`, as the guide says | pass |
| Picture of search | pass (the picture shows an administrator, with Audit log) |

Created and removed: four personal API tokens named `x`, `uat-a-tok` and `uat-a-tok2` (the list is empty again).
Guide change: the Limits and verification callout now says what was run.

## 2. Home and approvals

| Step | Result |
|---|---|
| Home: Ask box, greeting ("Three things need you"), the four tiles, Needs you (failed run, two high findings, "Waiting for approval" for a change the test user proposed), Start something, Recent changes (four), Docker/server/disk strip, Pipelines and Recent runs tables | pass |
| Spend tile shows a dash with "Load from AWS" (not pressed: costs a Cost Explorer call) | pass, not loaded |
| Set up card: all seven steps done on this instance, so the card is hidden as the guide says; **Settings → Setup** lists the seven steps | pass (appearing of the card not testable here) |
| Viewer Home: no Ask box, Spend or AI team tile; Start something says to ask an administrator for the developer role | pass |
| Approvals list for developer: AI proposals plus Runbook fixes; viewer sees the proposals only, no runbook fixes | pass |
| One proposal: who, reason, service/operation/arguments, exact script, sha256, approver note, what ran | pass |
| A cloud change (another tester's blueprint build) is on Home but not on the Approvals list | pass |
| Role refusal: developer `POST /v1/ai/proposals/<id>/approve` answers 403 "your role (developer) cannot do this; admin is required" | pass |
| Approve, reject, withdraw, two approvals, 24-hour expiry, Skip a step, the own-proposal refusal | not testable here: the instance has no administrator account Claude may use, and approving as the server token is not for this test |
| Picture `home-today.png` | guide wrong: the caption said greeting, tiles, Needs you; the picture shows the Ask box and the setup card. Caption fixed |
| Picture `home-approvals.png` | pass |

Guide changes: the caption of the Home picture; the Limits and verification callout (the stale remark about "marketplace" is gone:
Start something now says "library").
## 3. Write pipelines (`pipelines.md`)

| Step | Result |
|---|---|
| Write a two-step pipeline (`uat-a-two`, alpine, `needs`, `network: none`) in the console editor; the graph shows Valid with both steps and "no network"; **Create & run**; run succeeded in 3 s, step `two` output `hello-two` plus the container hostname | pass |
| Same pipeline written with `POST /v1/runs` (unsaved) | pass |
| Validation messages through `POST /v1/pipelines/validate`: id `Build` ("must match ^[a-z][a-z0-9-]{0,62}$"), `run` and `command` together, a secret also in `env`, `workflow` with an image, three problems at once with their paths, a missing secret plus the two warnings (secrets with network; mutable tag), a relative `workdir`, capability `SYS_ADMIN` (the message lists the 14 allowed) | pass, each matches the guide |
| `permissions.network: none`: a step trying `wget http://1.1.1.1` printed BLOCKED | pass (security promise) |
| A secret (`UAT_A_SECRET`, made through the Pi server token, deleted afterwards) requested by a step and printed: the log shows `the value is ***` | pass (security promise) |
| Viewer: validate 200; save, run, restore 403 "developer is required"; delete 403 "admin is required"; secrets list 403 | pass (roles table) |
| Run against a repository (source), `build` and ECR push, `eval`, GitHub-started trust rules, strict capabilities, `forgeline run`, QEMU | not testable here: they need a repository the maintainer names, a push, an ECR repository, an eval suite or a laptop; class C or X |
| Picture `pipelines.png` (source fields) | the editor still has **Source repository** and **Branch** as described |

Guide change: the Limits and verification callout only.

## 4. Pipeline editor and runs (`pipeline-editor-and-runs.md`)

| Step | Result |
|---|---|
| List shows pipeline, steps, version, latest run, last changed; **New pipeline** opens the two-step example | pass |
| Unknown `needs` shows `steps[1].needs[0]` "unknown step "x"" in the Problems panel, **Create** disabled | pass |
| Add step form: Kind (Script, agent, workflow, build), id, image, command, Runs after, network, time limit, secrets list | pass |
| Run page: who, trigger "ui · v1", created, duration, source "none", step graph, output | pass |
| Failed run `uat-a-fail` (a command that does not exist): "pipeline did not succeed: boom (failed), after (skipped)", **What went wrong**: "Step boom failed with exit code 127", the 127 cause text exactly as the guide, "Did not run because of it: after", End of the output; `GET /v1/runs/{id}/failure` agrees | pass |
| **Cancel** on a running run: confirmation "Cancel this run? Steps that are running will be stopped." then Canceled, "canceled by user", **Run again** shown | pass |
| Save twice in the editor, **History** lists versions with **View** and **Restore**; restore of version 1 made version 4 with the old text | pass |
| Errors: rename in the YAML (400 with the guide's text), duplicate name (409), stale save with an old `base_version` (409 with `current`), restoring a version not kept (404 "version 99 is not kept (the last 20 are)") | pass |
| After 25 saves: history holds 20 (oldest 5); version 1 is not kept | pass |
| Viewer: editor shows only **History**, list shows no Run | pass |
| **This pipeline changed** dialog in the browser, Source repository, Follow, Ask the assistant, 1 MiB limit, administrator delete button | not tested (the 409 behind the dialog was checked; the others need a repository, an administrator or a very large file) |
| Picture `pipeline-run-failed.png` | pass (the live page also has a **Trace** button for developers) |

## 5. Versioning (`versioning.md`)

| Step | Result |
|---|---|
| Version shown in **Operations → Infrastructure → Platform**: `v0.1.0-202-g487f373`, matches the instance's commit | pass |
| Pipeline versions: last 20 kept, restore saves a copy as the newest, "version N is not kept (the last 20 are)", a no-change save adds nothing | pass |
| Role: a viewer cannot restore (403) | pass |
| Skill, agent and workflow versions (save, rollback, 100 limit, "v2 available"), audit entries, signed packages, cutting a release | not testable here: they need an administrator or a maintainer, or would change the instance |

Found: the Platform tab says "Run history: In memory (lost on restart)". The guide does not mention it; it is a fact of this instance, not a guide error.

## 6. Agent steps (`agent-steps.md`)

The instance has a `CLAUDE_CODE_OAUTH_TOKEN` in its vault and an agent image tagged `forgeline/agent:1`; it has no image named
`forgeline-agent-claude`, which the guide uses.

| Step | Result |
|---|---|
| Validation: no credential ("an agent step must request one of: CLAUDE_CODE_OAUTH_TOKEN, ANTHROPIC_API_KEY"), `network: none` ("an agent step needs network access to reach its provider"), provider `codex` ("unknown provider ... supported: claude-code"), `maxTurns: 101` ("must be between 1 and 100, got 101") | pass, each equals the guide's Troubleshooting row |
| The guide's example as written (`image: forgeline-agent-claude`) | guide wrong: no such image unless the reader built it (the guide says to). Run failed with exit code 125 "pull access denied". Row rewritten |
| The same step on `forgeline/agent:1`, prompt "Reply with the single word OK", `maxTurns: 1` | pass: output `OK`, run succeeded in 5 s |
| Default read-only tools: a prompt that asks to run `echo` with Bash | pass: the agent answered that Bash needed approval and was not allowed; the command did not run (security promise) |
| Credential not stored: the step lists `ANTHROPIC_API_KEY` | pass: fails in under a second with `secret "ANTHROPIC_API_KEY" is not configured; set FORGELINE_SECRET_ANTHROPIC_API_KEY in the environment of the process that runs steps`. Not in the guide: row added |
| Viewer runs a pipeline | pass, 403 |
| Agent step in a GitHub-started run, listed `Edit` or `Bash(...)`, `model`, building `deploy/agent` | not tested here (needs a GitHub run, or would change the server's Docker) |

Product bug (minor): the failed run above, exit code 125 and "pull access denied for forgeline-agent-claude", shows under What went
wrong "Something rejected the credentials. Check the secret's value, and that the step lists it in permissions.secrets." The cause
is a missing image. Reproduce: save any pipeline with `image: does-not-exist-uat` and `run: echo hi`, run it, open
`GET /v1/runs/<id>/failure`. Input: the guide's agent example as written. Output: `causes` holds the credentials text. The
text of the output is right; the guess is wrong.

Cost: three agent steps of one to three turns. Created and removed: pipelines `uat-a-*` (removal listed at the end).

## 7. Connect GitHub (`connect-github.md`)

The instance has a GitHub App connected, 18 repositories on the allow list, no deliveries yet. Nothing was changed.

| Step | Result |
|---|---|
| Settings → Connections lists GitHub as working; the GitHub page opens with the allow list, **Secrets for pushes** (off on all rows), the webhook instructions (payload URL, content type, events) and **Recent deliveries** ("No deliveries yet") | pass |
| **Test connection** as the Pi server token (an administrator; read-only): `ok`, 18 of 18 "readable with this token" | pass |
| Role: developer `POST /v1/integrations/github/test`, `PUT /v1/integrations/github` and `POST .../app/start` answer 403 "your role (developer) cannot do this; admin is required"; the page says "Only administrators can change integrations." | pass |
| Webhook without a signature and with a wrong one: `401 {"error":"missing or invalid signature"}` (security promise) | pass |
| Create the App, install it, the token route, rules edits, a real delivery, a commit status, forks ignored, 404 and 503 webhook answers | not tested here: they change the maintainer's GitHub or settings |
| Picture `connect-github.png` | partly: the "Connect with a GitHub App · Connected" card is not on the page a developer sees (not verifiable as an administrator here); the allow list, webhook box and secret field match |

Guide change: the Limits and verification callout. The old text said the test read 19 repositories; it reads 18 today.

## 8. GitHub Actions (`github-actions.md`)

Read only. The instruction was not to dispatch anything, and nothing was dispatched, canceled or re-run.

| Step | Result |
|---|---|
| **Delivery → GitHub Actions** lists the allowed repositories in a menu; choosing one shows its workflows and the latest runs (status, run, branch, event, who, when) | pass |
| A repository with a workflow (`chandra_web_profile`, three runs): opening run 3 shows its three jobs with their steps, **View log**, **Re-run all jobs**, **Open on GitHub** (none pressed except View log) | pass |
| Empty states: "No workflows / This repository has no files under .github/workflows", "No runs yet" | pass |
| Troubleshooting "the GitHub App does not have the Actions permission yet": 9 repositories (the private ones) answer `502 GitHub refused: the GitHub App does not have the Actions permission yet. Approve it on the App's page, then try again`, the guide's text; the public ones list fine | pass (the permission has not been approved on this App) |
| Allow list: `GET /v1/actions/workflows?repo=<not allowed>` answers `403 that repository is not on the GitHub allow list` | pass |
| Role: viewer `GET /v1/actions/repos` 403; the page says "You do not have access to this page" | pass |
| View log of a run from July: the panel shows `GitHub answered 410: Server Error` | guide gap: GitHub expires old logs. Row added to Troubleshooting. Observation: the text "Server Error" is GitHub's, and says nothing about expiry |
| **Run** with inputs and a branch, the branch refusal, **Cancel run**, re-runs, the rate limit, a `workflow` step, fork tag | not tested here: they change GitHub (class C) or need a branch the maintainer names |
| Picture `github-actions.png` (a fork's run tagged) | page layout matches; the fork tag was not seen (no fork run) |

Guide changes: the callout and one Troubleshooting row.

## 9. Connect AWS (`connect-aws.md`)

Read only. The instance reads a primary account through the server's own role and has one added account. Account numbers are masked.

| Step | Result |
|---|---|
| Connections page: AWS is Connected, region ap-south-1, server profile, extra accounts 1, "Read-only is enforced by the provider", "Only administrators can run a test" | pass |
| **Operations → Infrastructure → AWS account**: identity, account switch (Primary, Old account), resource count by service, Map and List, stacks table with **Deploy the Forgeline demo** and **Propose deletion** (developer), EC2 list, **Changed through Forgeline** (five past proposals, who proposed, who approved) | pass |
| Connection test (`POST /v1/integrations/aws/checks`, run on the server with the server token): `passed 3, warned 0, failed 0` (identity confirmed, VPC list read, alarms read), `changed: false` | pass |
| Role: developer `POST /v1/integrations/aws/checks`, `PUT /v1/integrations/aws`, `POST /v1/aws/accounts` answer 403 "admin is required" | pass |
| Role: viewer sees the account, the map and the stacks but no **Deploy the Forgeline demo** and no **Propose deletion** | pass |
| Spend (billed by AWS): left unloaded. Viewer `GET /v1/infra/aws/cost` answers 403 "developer is required" | pass for the API. Observation: a viewer still sees the **Load last 30 days** button under Spend. The guide says only developers see spend; the button should be hidden for a viewer. Not clicked, because that was not needed |
| Sign in with AWS (device code), server profile change, add, allow changes for and remove an account, AWS MCP Server, a proposal, IAM policy | not tested here: they change the connection (class X) or AWS (class C). The IAM policy was not run |
| Troubleshooting "map says what is missing", "Cost data is not available", "Not reachable", code expiry | not reproduced here: the role has the read permissions, spend was not loaded |
| Picture `connect-aws.png` (a proposed deploy) | not reproduced: no proposal was made |

Guide change: the Limits and verification callout (the paragraph another note had added is kept).

## 10. Infrastructure map and services (`infrastructure-map-and-services.md`)

| Step | Result |
|---|---|
| **Cloud → Map**: title, "21 in 7 services · region ap-south-1 · read 1m ago", Map and List, Refresh, legend, "0 CloudWatch alarms, 0 in alarm. A resource with no alarm is not shown as healthy." | pass |
| Stack box: `AiInterviewerStack` (ROLLBACK_COMPLETE) holds six DynamoDB tables; `env-verify` (REVIEW_IN_PROGRESS) is a tile; both listed under "2 resources need a look"; VPC box with its security group | pass |
| Pick the stack: side panel says which stack, "Its first deploy failed and AWS undid it. A stack in this state cannot be updated, only deleted and deployed again.", what it made, identifier, **Ask the assistant to fix it** (not pressed: it would draft a proposal) | pass |
| Account switch to the Old account; the Services page follows the same account | pass |
| **Operations → Services**: tiles by category (Compute, Containers, Storage, Databases, Network, Messaging, Management) with counts; the footer "Region ap-south-1. Global services such as S3 are listed too." | pass |
| Open the S3 tile: its own page with four buckets, search, alarms line, Changed through Forgeline | pass |
| **Operations → Infrastructure → Platform and AWS account**; **Operations → Kubernetes** shows "No clusters yet / An administrator can add clusters." | pass |
| A tile that says "not listed", cache and Refresh timing, lines between a function and a queue, alarms, a Kubernetes cluster, Google Cloud, Azure, the 25-stack and 200-resource limits | not tested: nothing on this instance shows them (no Lambda, no alarms, no cluster, GCP and Azure not connected) |
| Viewer reads the map and services (200) | pass |

Guide change: the Limits and verification callout.

## 11. Connections and AI access (`connections-and-ai-access.md`)

Read only: the instruction was not to change connections, approval rules or other instance settings, so no switch or tool setting
was changed.

| Step | Result |
|---|---|
| Settings → Connections: "Connected · 5" (AWS, AWS MCP Server, Claude, Docker host, GitHub) each Working; "Found on this server": two AWS profiles by name only, "Found, not connected"; **Add a connection** ("14 more ...") | pass |
| AWS connection tabs: Overview, Services, What the AI may do | pass for the three; guide wrong about **Activity**: it is shown only to administrators (`integrations_hub.js`). Guide now says so |
| Overview: "Only administrators can run a test", the last test with three results, times, and "3 passed · took 4.4 s · created nothing, changed nothing" | pass |
| Test connection as an administrator (server token, on the server): 3 of 3 passed, `changed: false`; the console's Last test line showed it two minutes later "by token" | pass |
| Services tab: S3, ECS, ECR, EC2/EBS/VPC, Lambda, RDS, DynamoDB, ELB, SQS, SNS, ... each with Off, Look only, Change with approval, "Only administrators can change this" | pass |
| What the AI may do: "The ceiling for this connection", tools with kind (read) and Allow, Needs approval, Blocked, and the explanation of approval and "Changes are never Allow" | pass |
| Activity (administrator read): `integration.test` with the actor and result for my test and earlier tests; `policy.services` and `policy.refused` ("Tables (DynamoDB) is set to Look only for the AI ..., so no change to it can be proposed") from earlier days | pass (each test is audited; a refusal is shown) |
| Role: developer `GET .../aws/activity`, `PUT .../aws/policy`, `PUT .../aws/services` answer 403 "your role (developer) cannot do this; admin is required"; the viewer has no Settings (see console basics) | pass |
| Changing a switch or tool, "a changing tool cannot be Allow", fail closed, Claude test (sends a question), Google Cloud and Azure | not tested here: they change the maintainer's settings or are not set up on this instance. The refusal of an Off or Look-only service was only seen as the earlier `policy.refused` audit entry, not provoked again |
| Picture `connections-services.png` | pass (labels Off, Look only, Change with approval) |

Guide changes: **Activity** is for administrators; the callout.

## Found

Guide corrections (all made in this change): the Home picture caption (it showed the setup card, not tiles); the agent-steps image
row (exit code 125, "pull access denied") and a new row for a credential that is not stored; a Troubleshooting row for a GitHub log
that answers 410; **Activity** is administrators only (connections guide); the Limits and verification callouts of all eleven guides.

Product bugs and observations (no code was changed):

1. Failure explanation for a missing image. Input: a pipeline step with `image: does-not-exist-uat` and `run: echo hi`, run through
   `POST /v1/runs`. Output of `GET /v1/runs/<id>/failure`: headline "Step s failed with exit code 125", `causes`:
   "Something rejected the credentials. Check the secret's value, and that the step lists it in permissions.secrets.", while the
   `tail` says "pull access denied for does-not-exist-uat". The cause text is wrong for this case (exit code 125 is Docker's own
   failure to start the container).
2. A viewer sees **Load last 30 days** under Spend on the AWS account tab, but `GET /v1/infra/aws/cost` answers 403 "developer is
   required" for a viewer. The button should be hidden (not clicked, so no AWS call was made).
3. Observation: a personal API token used on `POST /v1/tokens` answers `401 sign in required`, not the `403 sign in to the
   console as a person` text the API reference promises for "a token". A personal token only works on `/mcp`, so it never reaches
   that check; the server token does, and gets the 403. Documentation could say "the server token".
4. Observation: the Platform tab says "Run history: In memory (lost on restart)". Not a bug in the guides; a fact to know.
5. Observation: the GitHub App on the instance has no Actions permission, so 9 private repositories answer 502 on the Actions page
   (the guide's documented message). The maintainer has to approve the permission on the App's page to test **Run**.

## Not tested, and why

- Anything that changes AWS or GitHub (class C), costs money or changes instance settings (class X): dispatching, cancelling or
  re-running a workflow; proposing or approving a cloud change; loading spend (billed); connecting or changing AWS or GitHub; changing
  a service switch or a tool setting; the IAM Identity Center sign-in; adding an account.
- Approving, rejecting and withdrawing proposals, two approvals and skipping a setup step: no administrator account for Claude.
- GitHub-started runs (pull-request trust, push secrets), `build` and ECR push, `eval` steps, a source repository, `forgeline run`.
- Skill, agent and workflow versions (need an administrator), signed packages, releases.
- Screen reader and a real phone (a 390 px window was used).

## Cleanup

Removed: pipelines `uat-a-two`, `uat-a-fail`, `uat-a-sleep`, `uat-a-secret` (the others were run without saving; their runs stay in the
history, as the guides say), the secret `UAT_A_SECRET`, the viewer `uat-a-viewer`, and four personal API tokens made in the console
checks. Checked afterwards: one pipeline (`hello-pi`, the maintainer's), one secret (`CLAUDE_CODE_OAUTH_TOKEN`), two people, no tokens.
Nothing was created in AWS or GitHub. The agent steps used a few model turns of the instance's Claude login.
