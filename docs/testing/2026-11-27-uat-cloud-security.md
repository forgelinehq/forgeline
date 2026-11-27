---
title: UAT of the cloud and security guides on the UAT instance, 2026-11-27
---

# UAT of the cloud and security guides



LEFT IN AWS: removed (confirmed 05:31 with read-only calls: no SQS queue, DynamoDB tables 12 before and 12 after, log groups 2 before and 2 after)

## Blueprint cycle (cloud-canvas, blueprints-and-environments)

Instance commit: 487f373 (the Pi's current-sha at the start). Signed in as the developer test user; changes approved with the server token on the Pi (audit shows `token`).

1. POST /v1/blueprints: queue (4 days), table (partition key `id`, type `S`), logs (14 days). 201 with checks. A first try with `partition_type":"string"` was refused `blocks[1].partition_type: S (text) or N (number)`.
2. POST plan, polled: ready, 3 to add, 0 change, 0 remove, no guardrail findings.
3. POST propose: change made; the plan is applied exactly. Approved with the token; `executing`, then `succeeded` (about 45 s). GET built: `built true`. AWS showed one queue; names are `fl-<hash>-<block id>`, not the blueprint name.
4. Removal, `{"remove":"everything"}`: first plan 0 change, 2 remove (turn protection off, remove queue and logs), approved, succeeded; second plan 1 remove (the table), approved, succeeded; a third plan answered 409 `nothing this blueprint built is left to remove`; built answered `removed true`.
5. DELETE blueprint: 204. List shows none left with the `uat-d-` prefix.

Before/after in AWS (ap-south-1, primary): SQS queues none / none; DynamoDB tables 12 / 12; log groups 2 / 2.

### Results: cloud-canvas (pass 9, guide wrong 1, product bug 1, not testable 4)

- Pass: blocks and OpenTofu preview of a queue, table and logs; the 409 texts `preview the changes first`, `the blueprint changed after the preview; preview again`, `this blueprint is busy with a preview; try again when it ends`; stale save `the developer test user changed this blueprint after you opened it; reload to see their version`; removal in two approvals, then 409; Canvas page matches the picture (toolbar, Code, Preview changes, Send for approval, Assistant, Blueprint panel); a viewer is refused later (see people-and-roles).
- Guide wrong, fixed: the reason of 5 to 500 characters (`say why in 5 to 500 characters; the approver reads it`) was not in the guide; the way to withdraw a proposal (**Withdraw this proposal**, API `cancel`) was not either. Both added to step 4.
- Product bug (small): **Send for approval** can be called twice for the same finished preview. The second call made a second pending change for the same plan (first with reason `Short`, then the real one); both sat `proposed`, and approving one left the other approvable. I withdrew the extra one by hand (`cancel`). Input: two POST /v1/blueprints/{id}/propose in a row with a ready plan. Expected: refuse the second or reuse the first.
- Not testable here: Suggest a repository, Build from GitHub, Run a new image (no test repository), the Assistant panel (not asked), templates, Fargate Spot, Agent block, the "Allow this push" administrator action.
- Not tried: delete a blueprint that still has built things, and delete another person's blueprint (an attempt to delete a blueprint of another user was stopped by the safety classifier as unwanted; the refusal text `only <author> or an administrator can delete this blueprint` is unverified).

### Results: blueprints-and-environments (pass 11, guide wrong 0, not testable 3)

- Pass: Blueprints list and the page text (blocks, region, "to look at" count); the preview with built-in guardrails (`Guardrails builtin-2: no rule denied it`); Built data after the apply (`built true`, `applies 1`) and after removal (`built false, removed true`); the generated code shows the AWS provider with `forgeline:blueprint` and `forgeline:managed-by` tags; **Resources** page opens; environments: list empty, 5 templates with the stated names and prices, the policy read (self-service off, 24 h, 168 h, 2 per person, $40: the guide's defaults), estimate for Developer box 4 h answers `Self-service is off, so an administrator approves each environment.` (matches Troubleshooting row); developer role sees the pages read-only.
- Role: a developer is refused a settings write with `your role (developer) cannot do this; admin is required` (403).
- Not tested: requesting, extending or deleting an environment (not allowed in this run); the environment-policy write refusal (I did not send a write against the instance's policy); **Check against AWS** was not clicked.
- Security promise checked: the approval needed a principal other than the proposer (the change shows `proposed_by the developer test user`, approvals `token`); the plan applied was exactly the previewed 3 adds (`Applied: 3 added, 0 changed, 0 removed`).

### Results: policy-packs (pass 4, not testable 3)

- Pass: **Security → Policy packs** opens read-only for a developer with the sentence `Only administrators can change the packs.`; the API lists the three tools with pinned versions (Trivy 0.76.2, Checkov 3.4.5, Conftest 0.71.1) and the built-in rule set `builtin-2`; a developer's write is refused 403 (`admin is required`); the preview of the cycle named the built-in set only.
- Not testable here: no pack is enabled on the instance and I may not change packs, so no pack finding, no `pack-failed` row, no limits (10 packs, 10 files).
- Guide note (not changed): `policy-packs.md` and `approval-rules.md` both have `nav_order: 7`.

### Results: approval-rules (pass 4, not testable 3)

- Read: the instance has no rules (`rules []`, last set 2026-11-26 by `token`). The page says `No rules: Every change needs one administrator who did not propose it.`, matching the guide's "With no rules, nothing changes". With zero rules the cycle's change was approved by one administrator (`approvals` has one entry) as the guide says.
- Role: developer write refused 403.
- Not testable here: two approvals, named approvers, strictest-rule-wins, error texts (needs rules; not allowed to add any). The picture shows a rule that the instance does not have.

### Results: people-and-roles (pass 12, guide wrong 1, not testable 3)

All with the server token as administrator, the viewer `uat-d-viewer` and the developer test user.

- Pass: create (201); duplicate `that username is taken`; short password `password must be at least 12 characters`. The viewer reads runs and pipelines (200) and is refused: secrets list and a blueprint create with `your role (viewer) cannot do this; developer is required`; users, audit and a secret write with `admin is required` (all 403). A password reset ended the old session at once (401). A personal token (viewer) was made (shown once, `flt_...`); a token above the viewer's own role was refused `a token cannot have a higher role than you have`; the token worked on `/mcp` (`tools/list` 200) and on no `/v1` path (`sign in required`, 401). Role change to developer: the same cookie kept working (secrets list 200), the token still worked. Disable: cookie 401, token 401, login `wrong username or password`. Enable then delete: 204. After the delete the user is gone and no tokens of it remain.
- Guide wrong, fixed: the guide said a token cannot make a token with `sign in to the console as a person to create a token`. That text is what the server token gets. A personal token gets `sign in required`. The guide now says both.
- Page: **People** and **Sign-in (SSO)** show `You do not have access to this page` for a developer, as the Troubleshooting row says.
- Not testable here: the People page in the console (needs the administrator account), the last-administrator refusal (would need to touch the maintainer's accounts), login throttling, the 10-token limit.

### Results: secrets-and-memory (pass 9, not testable 3)

- Pass: `UAT_D_SECRET` created (version 1); a developer sees the name, description, who and when, never a value (a search of the list for the value found nothing); developer write refused (`admin is required`); name `1BAD` refused with the guide's text; value `abc` refused with `the value must be at least 8 characters so it can be masked in logs`; a run of a step that lists the secret and prints it showed `the secret is ***`, and a variable it did not list printed empty; the audit log has `run.secret.grant`, `secret.create`, `secret.rotate` (version 2) and `secret.delete` (detail `still requested by: ` and nothing); deleted, and the list no longer has it.
- Memory page read as a developer: three parts as the guide says (About you, About this system, Your conversations), plus a "Files the assistant keeps" part with `/memories/me` and `/memories/org`.
- Not testable here: a secret from `FORGELINE_SECRET_<NAME>` (none on the instance, and I may not change its environment); memory writes (not asked); the 16 KiB and newline limits.

### Results: audit-log (pass 8, not testable 2)

- Pass: the events of this session are in the log with the names the guides give: `blueprint.create`, `blueprint.update`, `blueprint.preview`, `blueprint.delete`, `cloud.propose`, `cloud.approve`, `cloud.execute`, `cloud.cancel`, `user.create`, `user.update`, `user.delete`, `secret.create`, `secret.rotate`, `secret.delete`, `run.secret.grant`, `token.create`, `token.revoke`, `login`, `login.failed` (the disabled account's attempt), and `access.denied` for the viewer's refusals. Filters by action prefix and `from` date work. CSV (with an `ip` column) and JSON Lines downloads gave the same four secret events, oldest first. Each download wrote `audit.export` with its count (`4 events as csv, 2026-11-27 to `). `format=xml` is refused `format must be csv or jsonl`. A developer gets 403 on the log and the export.
- Not tested: the audit page in the browser (administrator only); retention (left alone as told).

### Results: runbooks (read only; pass 5, not testable 4)

- Pass: the list shows two runbooks (a check and a fix) and the three built-in examples with the names in the guide; the check's page shows the version, `by token`, `reviewed by vicky`, the script, the parameters as choices with defaults, the time limit and the script's hash; a developer sees **Run the check** (not pressed).
- Not tested: running, writing, reviewing (an attempt to try the author-cannot-review refusal with the server token was stopped by the safety classifier and not retried), approving a fix, the 20-instance and 8,000-character limits.

### Results: alarm-intake (read only; pass 1, guide wrong 1, not testable 3)

- Read: the settings say `enabled false`, no queue, AWS connected, nothing received. So the test was skipped as told.
- Guide wrong, fixed: the table said a developer can see the intake settings; that is true of the API only. The console page needs an administrator (a developer sees `You do not have access to this page`).
- Not testable: set-up, **Test the queue**, workflow triggers (no intake on the instance, and no settings changes allowed).

### Results: single-sign-on (read only; pass 1, not testable 5)

- Read through the API as the administrator token: `enabled false`, no provider, `has_secret false`, `groups_claim groups`; the console page is for administrators. The page title `Sign-in (SSO)` matches the guide. Everything else needs a provider (use `scripts/dev/fakeoidc` on a local server).

### Results: connect-aws (permissions part, read only; pass 3, product text 1)

- The accounts list has two accounts (Primary with writes on, an old one). The build ran with the instance's profile for queue, table and logs with no IAM change, which agrees with the guide (these are covered by `PowerUserAccess`).
- The console does not show the IAM policy (the guide holds it alone), so there was nothing to compare it with in the console. I compared it with the code: its `iam:PassedToService` list (Bedrock AgentCore, ECS tasks, EKS, Firehose, Lambda, Step Functions) is what the blueprint code and the templates pass roles to; the Go tests for blueprints and the server pass.
- Product text, minor: the error shown when a role cannot be passed says to see `Connect AWS › Building blueprints`; the guide's sections are **Permissions for blueprints** and **Permissions to build**. I did not edit code.

## Found

Product bugs and rough edges, none fixed here:

1. Two **Send for approval** calls on one ready preview make two pending changes (see cloud-canvas). I withdrew the extra one.
2. The reason text for a failed role pass points to a heading (`Building blueprints`) that does not exist in the guide.

Guide corrections: cloud-canvas (reason length, how to withdraw a proposal), people-and-roles (the two token refusal texts), alarm-intake (a developer reads the settings through the API only). Each tested guide's Limits and verification callout got the date, the commit and what ran.

## Not tested, and why

- Pack findings and every rule behaviour of approval rules (none on the instance; I may not add them): two approvals, named approvers.
- Environments: requesting, extending, deleting; the self-service policy write.
- The People, Audit and SSO pages in the console (administrator only; I only had the developer test user), runbook execution and review, alarm intake, SSO sign-in.
- Console clicking of the cloud cycle: it was done through the API; the canvas, blueprint list and Resources pages were opened and compared with the pictures.
- Building a database, an app block, templates, image builds.

## Cleanup

- AWS: nothing left (see the blueprint cycle). The Forgeline blueprint is deleted. An earlier blueprint `uat-d-queue-table` of the lost attempt had been removed at 05:24 (audit shows its removal approved), before this run.
- Forgeline objects: user `uat-d-viewer` deleted, secret `UAT_D_SECRET` deleted, one cancelled change remains in the history, run `uat-d-secret` remains in the run list (runs cannot be deleted).
