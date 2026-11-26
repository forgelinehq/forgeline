---
name: feature-uat
description: Test a Forgeline feature the way a user would, on the maintainer's UAT instance (a Forgeline with real AWS, GitHub and the assistant set up), by following its guide step by step, and record what passed, what the guide got wrong and what is a product bug. Use before calling a feature or its guide done, after a release, or when asked to test or check the quality of a feature.
---

# User-acceptance test of a feature, on the UAT instance

The guide for a feature (`docs/guides/<page>.md`) is the test script. A person who has never seen Forgeline should be able to
do what it says and get what it says. This skill runs the guide as that person on the UAT instance and writes down the truth.
It complements the Go tests: those prove the code; this proves the feature works through the console, with real services, and
that the guide is right.

## The UAT instance

The maintainer runs a second Forgeline as the UAT instance. Its address, how to reach its host, which test user to sign in as and
where that user's password lives are **not in this repository** (CLAUDE.md: no hostnames, usernames or local paths here). They are
in the maintainer's own notes for the instance (your memory, if you have it) or you ask for them. Throughout this skill:
`UAT_URL` is the console's address, `UAT_USER` the test user (a developer, not an administrator account of a person), and the
password is read from a command that prints it, so it never appears in a transcript or a file.

What the instance has: real AWS accounts, the GitHub App, a model provider and the assistant, a developer test user. It does not
have SSO (use `scripts/dev/fakeoidc` on a local server instead), Kubernetes, Google Cloud, Azure or a registry unless the notes say so.
It updates itself from `origin/main`; before testing a merge, check that the instance runs it.

Rules, all from the maintainer:
- Sign in as the test user only. Never act as a person's own account, never take a snapshot of the sign-in page (browsers autofill
  it), never print or commit a password, token, host name or account number. Mask account numbers in anything you keep.
- Batch connections to the host in few sessions and never retry in a tight loop; a host may refuse after many quick ones.
- Firewall changes need the maintainer's yes for the exact rule. Do not touch tunnels or other services on the host.

## What you may do there

| Class | Meaning | Allowed |
| --- | --- | --- |
| R | Reads, or reads through the console | Always |
| L | Makes and removes Forgeline objects (a pipeline, agent, secret, user, workflow) | Yes; remove them afterwards |
| C | Changes AWS or GitHub for real | Only your own test objects, cheap (queues, tables, buckets, log groups; an instance or Fargate task for minutes at most), proposed as the test user and approved the way the maintainer's notes allow, then **removed in the same session**, with evidence kept. Never the maintainer's proposals or anything in a work account. |
| X | Costs real money for long, creates IAM, or is otherwise not covered above | Ask the maintainer first, with the exact action and its cost |

## Procedure for one feature

1. Read the guide and its warning callout. Note what it claims to have been checked and what not.
2. Run the smoke script once per session (below) so you know the console itself is healthy before blaming a feature.
3. Do the guide's **How it works** and task steps literally, in the console in a browser (Playwright, signed in by a `POST` to
   `/v1/auth/login` before any snapshot) and through the API where the guide names it. Do not use knowledge the guide lacks: if
   you must guess a step, the guide is wrong.
4. For each step record: **pass**; **guide wrong** (fix the guide in the same change); **product bug** (write down the exact input
   and output; fix it if small, otherwise tell the maintainer); **not testable here** (say why).
5. Check what a guide promises beyond the happy path, cheaply:
   - **Roles.** The *Who can do what* table: try one action as a lower role (create a viewer user if needed) and expect the
     refusal the table names.
   - **Limits and errors.** Hit one documented limit or validation and compare the exact message to the guide.
   - **Troubleshooting.** Reproduce one or two rows when it is cheap; a row that cannot happen is invented and must go.
   - **Pictures.** Does the page still look like the picture? Labels, tabs, buttons.
   - **Security model.** One refusal the guide promises (a second approver, a blocked tool, a masked value) really refuses.
6. Clean up: remove what you made (design note 041 for blueprints; archive agents and workflows; delete secrets and users).
   Confirm in AWS that nothing is left that costs money.
7. Write `docs/testing/<date>-uat-<feature>.md` in the style of the other notes in `docs/testing/` (what ran, on which commit, steps
   and what happened, what was found, what was not tested), with masked evidence. Update the guide's *Limits and verification*
   callout: the date, the commit, what was run through the console, what still was not.
8. Tell the maintainer in plain words which features passed, which guides you corrected, and which bugs are open.

## Whole-console smoke

```sh
cd scripts/uat && npm ci
<command that prints the test user's password> | UAT_URL=<console address> UAT_USER=<test user> \
  CHROMIUM_PATH=<chromium> node smoke.mjs [screenshot-dir]
```

It visits every console page (and one example of each page that takes an id) and fails on a script error, an error box, a blank
page or a failed read. It only navigates. Screenshots of a real instance can show account numbers: keep them out of the repository.

## Feature matrix

What to run for each guide. Class is the highest in the list.

| Guide | Test | Class |
| --- | --- | --- |
| console-basics, home-and-approvals | Sign in; walk every space and tab; Ctrl K; check each tab against the table; the Home setup card; Approvals lists what the guide says and not cloud changes | R |
| pipelines, pipeline-editor-and-runs, versioning | Write, validate, save and run a 2-step pipeline in Docker; fail one on purpose and read What went wrong; save twice and restore; the version badge | L |
| agent-steps | A pipeline with an `agent:` step (the Claude token present); the refusal when there is no credential | L |
| connect-github, github-actions | Connection test; list workflows and runs of an allowed repository; the allow-list refusal; dispatch only on a test repository and branch the maintainer names | R, C |
| connect-aws, infrastructure-map-and-services | Connection test; Map, Services and Infrastructure tabs read the real account; load spend | R |
| connect-gcp, connect-azure, mcp-servers, registries | Where not set up: the wizard's errors only | R, X |
| cloud-canvas, blueprints-and-environments | Draw a queue and table blueprint; Preview (real OpenTofu); Send for approval; approve; Built shows it; **remove it** and confirm AWS is empty | C |
| runbooks, alarm-intake | List; review as a second administrator; a check on a tagged test instance only if one exists (an instance costs: ask) | R, X |
| policy-packs, approval-rules | Preview with a pack that denies; a rule that makes a change need two approvals | L |
| assistant and its sub-pages | Three real questions (what failed, what is in AWS, how do I ...) checked against facts; a card that writes an agent; a memory file; the sandbox is off by default | L |
| agents, agent-builder, agent-bundles, publishing-agent-versions, evals | Create an agent and a skill; save a second version; export; import under a clean name; an eval suite with a fixture; the gate refuses a candidate until the suite passes | L |
| teams, schedules, workflows, workflow-canvas, missions | A team goal with two members; a graph workflow with an ask node, run and answer it; a paused schedule; a mission on a primary account | L |
| ai-use-and-budgets, models-and-providers | The usage page after the runs above; a monthly budget refuses the next call; the provider call log | L |
| traces-and-opentelemetry | Open the trace of the runs above; OTLP only with a collector the maintainer names | R |
| people-and-roles, secrets-and-memory, audit-log, connections-and-ai-access | Create a viewer and check the 403s; a secret masked in a run; the audit log shows each step above and exports; the AI-access ceiling blocks a tool | L |
| single-sign-on | A local server with `fakeoidc` (not the UAT instance) | L |

## Reporting

Do not write that a feature "works". Write what you did, on which commit, what happened, and what you did not do.
