# 037: Missions

Status: **Part 1 (the mission and its runner) is built; part 2 (the board and the live view) is next.** Date: 2026-11-08. Builds note 014 (agentic missions) on
what exists now: agents and their tools (note 024), AI proposals and the approval rules (notes 016, 032), budgets (036).

## Question

Note 014 describes a mission: a goal in plain words that an agent carries through discover, propose, approve, apply and
verify, with a person approving each change. What is the smallest honest version we can run today, and how do we keep
it from claiming what it did not see or reaching beyond its scope?

## Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| What a mission is | A saved record: the goal, one agent (its version pinned at the start), one AWS account (the scope), who started it, a token budget, its stages and what each produced | Note 014 §1, without the parts that need more machinery (hand-over, several agents) |
| Stages | **Discover** (read only: the agent looks things up and lists findings), **Propose** (an agent that may propose files change proposals for what it can fix; an advisory agent only recommends), **Awaiting approval** (until every proposal it filed is decided), **Verify** (after something was applied, the agent looks again and says per finding whether it is fixed), **Report** (written by Forgeline from the record, not by the model) | Every stage that changes the cloud goes through the existing proposal path; the model never applies |
| Findings must show their source | Each finding names the lookup that showed it. The server keeps only findings whose source is a lookup the agent actually ran in that stage, and counts the rest as **unsupported** (shown on the mission). Evidence in Verify follows the same rule | A model can write a convincing finding it never saw; this is the cheapest check against it |
| Scope enforced on the server | While a mission runs, lookups that read an AWS account (`aws_resources`, `guard_findings`, `aws_summary`, `call_aws`, `aws_script`, `propose_aws_change`) are refused unless they name the mission's account (no account means the primary one) | Note 014 §6: "read tools check the mission scope on the server, not in the prompt" |
| Proposals | Filed with the agent's own tools and tied to the mission (`mission` on the proposal, shown to the approver). A different administrator approves under the approval rules; nothing about approval changes | |
| Budget | A token budget per mission (default 200,000, at most 5,000,000), checked before every model call like the agent budgets (note 036); the agent's own monthly budget still applies. A mission over budget stops with what it has | Note 014 §1 |
| Form of answers | The agent answers with JSON between tags (`<findings>`, `<plan>`, `<evidence>`). If it does not, it is asked once more with the reason; then the stage fails | Same pattern as the canvas assistant |
| Limits | At most 20 findings; at most 5 missions running at once; a mission waits at most 7 days for its proposals, then reports with what was decided | Bounded cost and clutter |
| Who | Developers start missions and cancel their own; administrators cancel any. The agent runs with the starter's role. Audited: start, each stage, cancel, end | Same as workflows |
| A restart | A mission that was running a stage when the server stopped is marked failed ("interrupted by a restart"); one waiting for approval keeps waiting | No half-finished stage is resumed blind |

## Parts

1. **The mission and its runner** (this part): model, stages, the checks above, API.
2. **The board and the live view**: Team › Missions as cards by stage, and a mission page with the stages drawn live, findings, proposals, evidence and the report.

## Not yet

Several agents per mission, hand-over to another person, pre-approved classes (note 014 tier T2), auto-rollback, IaC
adoption (note 014 M-B), and a lead agent that starts missions itself.

## Part 1: tested

With a scripted model and the AWS stand-ins (Go, race detector): a mission discovers (one lookup), keeps the finding
that names it and drops the one naming a lookup that never ran; files a proposal tied to the mission; keeps only plan
items for proposals it really filed and findings that exist; waits while the proposal is undecided; after a different
administrator approves and it is applied, verifies (evidence naming an unrun lookup becomes unknown) and reports from
the record. Scope: a lookup for another account is refused on the server, and a lookup with no account counts as the
primary one. An advisory agent only recommends. The token budget stops a mission before its next call. An agent that
does not answer in the agreed form gets one retry, then the mission fails. Cancel: the starter or an administrator,
and a cancelled mission is never overwritten. A restart fails a mission mid-stage and leaves a waiting one waiting; a
mission waits at most a week. 16 rules removed one at a time, each fails a test.
