---
title: UAT of teams, schedules, workflows, the canvas, missions and traces, 2026-11-27
---

# Teams, schedules, workflows, workflow canvas, missions and traces, run as a user on the UAT instance

Each guide was followed literally on the maintainer's UAT instance (the Raspberry Pi Forgeline), signed in as the developer test
user, with an administrator action done by the server token only where the guide says an administrator does it. Objects were
named `uat-c-...` and removed afterwards. Account numbers are masked. Results are appended guide by guide as they finish.

Instance: commit `487f373` (the instance's own record of what it runs). Signed in as the developer test user; to see the
administrator screens, a test administrator and a test viewer (`uat-c-` names) were made with the server token on
the Pi and are removed at the end. Model: `claude-haiku-5-5`, three cheap agents with no tools.

A first attempt at this test had left archived `uat-c-` agents and workflows, a deleted team's schedule and its runs on the
instance. They are listed under "Left over" below.

## Teams

| # | Step | Result |
| --- | --- | --- |
| 1 | Teams page as a developer: no **New team**, **Give a goal** on each card | pass |
| 2 | As an administrator, **New team**: name, purpose, lead, two members from the searchable list, **Make the team** | pass; guide wrong on the last button's name ("Save") |
| 3 | As a developer, open the team, type a goal, **Give the goal**: plan, a card per member, answer (both members said "Red", the lead picked red) | pass; 4 model calls (plan, two members, answer) |
| 4 | Audit log: one `ai.team.step` per agent call, actor the developer; model call log: `uat-c-tlead in uat-c-team` and the like | pass (security: members work as the asker) |
| 5 | Role: developer creates a team: 403 "your role (developer) cannot do this; admin is required"; viewer gives a goal: 403 | pass |
| 6 | Limit: goal of 2001 characters and an empty goal: 400 "goal: say what the team should do, in at most 2000 characters" | pass |
| 7 | Troubleshooting: member turned off then a goal: 409 "uat-c-tm2 is turned off; turn it on or change the team" | guide wrong (it said the run fails; the goal is refused) |
| 8 | Troubleshooting "already working", "removal refused while working", lead naming a non-member | not tested (needs a race or a hostile model) |

Guide changes: the **New team** step says administrators and names **Make the team** and **Give the goal**; the 409 row and
the Limits text say the goal is refused; a Limits and verification callout with this check; a note that a new team with the
same name shows the old team's runs.

## Schedules

| # | Step | Result |
| --- | --- | --- |
| 1 | Team page as administrator: **Schedule a goal** panel: goal, "Every month on day 1 at 09:00 UTC", next three runs shown under the picker, **Schedule it** | pass; guide wrong on button names |
| 2 | **Workflows → Schedules**: the row, **Pause**, **Change**, **Run now** for an administrator; none for a developer; picture matches (columns What, When, Next, Last time, Runs as) | pass |
| 3 | **Pause** in the console: row shows **Paused**, button becomes **Resume** | pass |
| 4 | Limits: every 10 minutes: 400 "schedule: every 15 to 10080 minutes"; cron every 5 minutes: 400 "schedule: cron: it would run 5 minutes apart; at most every 15 minutes"; run-as admin: 400 "run_as: viewer or developer: nobody is watching a scheduled run, so it never runs as an administrator"; an 11th schedule: 409 "a team has at most 10 schedules" | pass |
| 5 | Role: developer adds a team schedule or pauses a workflow's: 403; administrator pauses a workflow with no schedule: 409 "this workflow has no schedule; set one in its settings" | pass |
| 6 | Audit: create, delete are `ai.schedule.create` and `ai.schedule.delete`; the console's pause of a team schedule is `ai.schedule.update` | guide wrong (it implied `ai.schedule.pause` for pause) |
| 7 | **Run now**, a schedule firing, the missed-time rules | not tested (a real model call; hours; a restart) |

Guide changes: button names, the next-three-runs line, audit names for team schedules, a Limits and verification callout.

## Workflows

A graph `uat-c-canvas` (agent, two questions) was made in the canvas (see the next section) and used here.

| # | Step | Result |
| --- | --- | --- |
| 1 | **Workflows** page: cards with **Run**; the **Archived** tab only for an administrator, **New workflow** only for an administrator | pass |
| 2 | Developer starts a run with **Run v1**: the agent answers ("Sunshine!"), the run stops at the first question | pass; 1 model call |
| 3 | Who may answer: question with no names, developer answers: 403 "an administrator answers this question"; the page shows "Waiting for an administrator to answer" and no box; an administrator answers | pass |
| 4 | Question naming the test developer: an administrator answers: 403 "only <name> may answer this question"; the developer answers in the page | pass; guide did not give the second message |
| 5 | Run page: steps in order, "Answered by" the test administrator and the test developer | pass |
| 6 | Second start while a run is going: 409 "this workflow already has a run in progress"; delete while running: 409 "uat-c-canvas has a run that is running; let it finish or cancel it first" | pass; guide named a different (webhook) message only |
| 7 | Roles: developer cannot create (403), delete (403) or make the webhook secret (403); viewer cannot start or list (403) | pass |
| 8 | Run-as: an agent with `aws_script` in a workflow run as a developer: 400 naming the tool | pass; the advice in the message ("Run the workflow as a developer if that is enough") is wrong when it already runs as a developer (see bugs) |
| 9 | Webhook: no or bad signature 401 "the signature does not match" (also for an unknown workflow); a signed request for a version without a webhook 409 "the current version of this workflow does not start from a webhook"; 70 KB body 413 "the body is larger than 64 KB" | pass |
| 10 | Cancel: an administrator cancels a running run | pass |
| 11 | Delete (archive): the workflow leaves the list and is under Archived; restore: back, turned off, a start answers 409 "this workflow is turned off" | pass |
| 12 | Stage workflow, 30 webhook runs an hour, a webhook that starts a run, alarm and failed-pipeline triggers | not tested (cost, time, or covered by other notes) |

Guide changes: the second answer message, the two 409 messages, the save refusal quoted, a Troubleshooting row for a start
refused, a second checked-on date in the Limits callout.

## Workflow canvas

| # | Step | Result |
| --- | --- | --- |
| 1 | **New workflow** shows Start wired to End; the **+** on Start opens the side panel (agents, logic, lookups, searchable) | pass |
| 2 | Pick an agent: it is placed between Start and End; panel tabs Agent, Input, Output, Next, Limits; Input holds the task, Output a field `word` added with Enter | pass |
| 3 | Next tab: **Add a node after this one** twice, picking **Ask a person**; Question tab with question, choices and **Who may answer** chips | pass; guide did not name the Ask node's tabs |
| 4 | **Settings**: About, Trigger (by hand, schedule, webhook, failed pipeline run, CloudWatch alarm, five cards), Permissions (Developer or Viewer); chose Viewer | pass |
| 5 | Name, note, **Create workflow**: version 1, graph and run-as as drawn (checked through the API) | pass |
| 6 | **Edit on the canvas**, **Add node** (End as failed), dragged from a node's port to it: a wire was made; **Save version**: v2, two unbranched wires accepted (they start together, as design note 044 says) | pass |
| 7 | Press Delete on a node: nothing happens after a click, because the node has no keyboard focus; it works when the node is focused with Tab; **Remove node** in the panel works | product bug (minor), guide changed |
| 8 | Leave with changes: "Save your changes?" with Keep editing, Discard changes, Save and leave | pass |
| 9 | **Try it** opens the playground with the node's task filled in; sending not run | pass (partly) |
| 10 | Save refusals through the API: 41 nodes "a graph has at most 40 nodes"; "node x cannot be reached from the start"; `node d: a decision needs an "else" wire for when nothing matches` | pass |
| 11 | Picture `workflow-canvas.png` against the page: same layout, tabs and **Settings**, **Save version** | pass |
| 12 | Decide, Merge, Lookup nodes, loops, the 80-wire limit, a run drawn after a failure | not tested here (the 2026-11-17 note ran a 12-node graph with them) |

Guide changes: Delete step, the Ask and End node tabs, a fan-out sentence, a second Limits and verification paragraph.

## Missions

Only the primary AWS account, and an agent that may not propose (so no stage could file a change). A second AWS account is
connected on the instance; it was not used.

| # | Step | Result |
| --- | --- | --- |
| 1 | **Operations → Missions** lists missions with Working, Waiting for your OK, Ended counts and **New mission** | pass |
| 2 | **New mission**: goal, agent, AWS account (Primary, plus one more), token budget 60000, **Start** | pass |
| 3 | The page draws the five stages live, findings with the lookup that showed each ("seen with aws_resources"), "Used 9.9k of 60k tokens in 3 model calls" while running | pass |
| 4 | Finished: 9 findings (1 medium, 2 low, 6 info), 0 left out, no proposals, **Your OK** and **Verify** "not needed", report written by Forgeline ("Findings: 9 ... Used 18319 tokens of 60000") | pass; 5 model calls; the findings were true to the account (three stacks, one bucket, tables, no compute) |
| 5 | Limits: goal of 5 and of 2001 characters: 400 "goal: say what the mission is for in 10 to 2000 characters"; budget 6,000,000: 400 "budget_tokens: up to 5000000 (0 means the default, 200000)"; account that is not connected: 400 "account: no connected AWS account ..." | pass; messages added to the guide |
| 6 | Agent turned off: 409 "uat-c-tm2 is turned off"; cancel an ended mission: 409 "this mission has already ended (done)" | pass |
| 7 | Role: viewer starts or lists missions: 403 "your role (viewer) cannot do this; developer is required" | pass |
| 8 | An agent with no tools given a mission (by mistake, it was meant to be turned off): done in 1 call, report "It found nothing it could show from a lookup" | pass (the report says so honestly) |
| 9 | Picture `missions-done.png`: same layout (stage cards, report, findings, what the agent did) | pass |
| 10 | Wrong-account lookup refusal, proposing agent and approval, 5 at once, 7-day wait, restart | not tested (needs a hostile or proposing agent, which was not allowed; time) |

Guide changes: the three refusal messages, "not needed" stages, and the Limits and verification callout now says what ran
with the real model.

## Traces and OpenTelemetry

Only the traces of the runs above were opened. OTLP was not configured and nothing was sent.

| # | Step | Result |
| --- | --- | --- |
| 1 | **Agents → Runs and traces**: list with Run, Who, When, Took, Model calls, Tools, Tokens, Cost; filters All, Agents, Workflows, Teams, Missions, Pipelines, Assistant, Failed | pass |
| 2 | Open the mission's trace: 19 s, 5 model calls, 3 tool calls, 14.7k tokens in, 3.6k out; `stage discover` and `stage propose` with `invoke_agent`, `chat` and `execute_tool` under them; a click on a bar shows its attributes on the right | pass; no `verify` stage because nothing was applied (guide changed) |
| 3 | Workflow trace (the run that waited for two people): spans start, agent, invoke_agent, chat, ask, ask, end | pass |
| 4 | Team trace: team span, the lead's invoke_agent, each member's invoke_agent and chat, the lead's answer | pass |
| 5 | **Trace** button on a mission, a workflow run and a pipeline run (as a developer) | pass |
| 6 | Security: no span of any trace carried a prompt, an answer, instructions or a tool argument (attribute names listed and checked); the trace attributes include `forgeline.user` locally (the guide says only that sending removes it) | pass |
| 7 | Roles: viewer lists traces: 403; developer reads the OpenTelemetry settings: 403 "admin is required" | pass |
| 8 | **Send a test trace** with nothing saved: 409 "save an endpoint first" | pass |
| 9 | Picture `traces-trace.png` and `traces-opentelemetry.png` against the pages | pass (layout the same; the settings picture shows the content box ticked, the instance has it off, as it is by default) |
| 10 | Oldest trace on the instance is 6 days old, inside the 14 days; the 1,000 limit was not reached (82 traces) | pass (limit not tested) |
| 11 | Sending to a collector, the endpoint refusal for plain http, header limits, "Keep prompts and answers", the dropped counter | not tested (OTLP must not be configured on this instance) |

Guide changes: the mission span row, a verification paragraph.

## What was found

Product bugs (nothing in `internal/` was changed):

1. **A deleted team's schedules and runs stay and come back.** Remove a team that has a schedule, make a new team with the same
   name (the id is made from it): the old schedule ("Say hi.", paused) shows again under **On a schedule**, and the old runs
   under Runs. While the team did not exist, `GET /v1/ai/schedules` listed nothing, so the schedule was invisible and could
   not be removed from the console. Found by chance: a first test attempt had left one. The guide says removing a team keeps
   its past runs; it does not say its schedules also stay, so it now says a new team with the same name shows the old runs.
   Expected: delete a team's schedules with the team, or refuse the removal until they are gone.
2. **Delete after a click does nothing on the canvas.** Click a node, press Delete: the node stays (the node has no keyboard
   focus after a click, the page's focus is the body). With the node focused by Tab it is removed. The guide said "press
   Delete"; it now names **Remove node** and Tab first.
3. **A save refusal gives advice that does not fit.** Saving a workflow run as a developer with an agent that has `aws_script`
   answers `... has aws_script (needs admin), which a run as developer may never use, so every call would be refused. Run the
   workflow as a developer if that is enough, ...`. It already runs as a developer; "if that is enough" is wrong for a tool
   that needs an administrator. Minor wording.
4. Not a bug of the product: `scripts/uat/smoke.mjs` reads `/v1/ai/missions` for the missions list but the API route is
   `/v1/missions`, so the script never visits a mission's page. Not changed (outside this task's files).

Guide corrections (all in `docs/guides/`): Teams (administrator-only **New team**, **Make the team**, **Give the goal**, the 409
refusal for a turned-off agent, same-name runs, callout), Schedules (button names, three next runs, audit names for a team's
pause, callout), Workflows (the named-person refusal message, start and delete 409 messages, save refusal text, callout),
Workflow canvas (Delete step, Ask and End node tabs, fan-out sentence, callout), Missions (three refusal messages, "not
needed" stages, callout), Traces and OpenTelemetry (verify stage, callout).

## What was not tested, and why

- **Run now** on a schedule, a schedule firing at its time, the missed-time rules, a workflow's own schedule: each needs a
  model call, hours, or a server restart.
- A team already working (second goal, removal refused), a lead naming an agent outside its team, a mission asking for another
  account, a proposing agent and its approval (no change was allowed), 5 missions at once, 7-day waits, a restart in a stage:
  they need a hostile or proposing model, a race, or time.
- Decide, Merge and Lookup nodes, loops, the 80-wire limit, stage workflows, the signed webhook that starts a run, the 30-an-hour
  limit, alarm and failed-pipeline triggers (the 2026-11-17 and 2026-11-26 notes ran several of them).
- Sending traces over OTLP and everything on that page except reading it: OTLP must not be configured on this instance.
- **Try it** was opened, not sent.

Model calls: 12 in this run (a team run 4, two workflow runs 2, two missions 6). A first attempt had used some before.

## Cleaned up

Removed: the team and its schedules (the team's ten extra test schedules were deleted as soon as the limit was seen), the
test administrator and the test viewer (deleted through the API), all `uat-c-` agents of this run (archived, which is how
agents are deleted: they are gone for good after 30 days), the workflow `uat-c-canvas` (archived), and its webhook secret
goes with it. A final listing showed no `uat-c` team, user, active agent, active workflow or schedule, and no running mission.
**Left over**, because archive is the only delete and keeps things for 30 days: archived agents and workflows named
`uat-c-` (this run's and the first attempt's), the finished missions and runs, and their traces and audit entries.
