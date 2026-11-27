---
title: Workflows
parent: Guides
nav_order: 42
---

# Workflows

A workflow runs agents in an order you define, with decisions, questions for people and approvals in between. A workflow
is either a **graph** (drawn on the [canvas](workflow-canvas.md)) or made of **stages** of agent steps, each with an
optional gate. It starts by hand, on a schedule, from a signed webhook, after a pipeline run fails, or when a CloudWatch
alarm goes off.

Use a workflow when one agent is not enough or when a person has to answer or approve part way: a morning check that pages
someone when severity is high, a weekly review, a response to an alarm.

## At a glance

| | |
| --- | --- |
| Where in the console | **Workflows** (cards, with a **Runs** tab and an **Archived** tab) |
| Needs | One or more [agents](agents.md) |
| Design notes | [044](../design/044-agent-graphs.md), [047](../design/047-schedules.md), [051](../design/051-runbooks.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See workflows and runs | Developer | |
| Start a run | Developer | One at a time per workflow: "this workflow already has a run in progress" |
| Create, save a version, roll back, turn off or on | Developer | Agent versions are pinned at save. A workflow an administrator saved with a gated stage or a question is only an administrator's to change, and what an administrator turned off only an administrator turns on |
| Delete (archive) or restore | Developer | Refused while a run is going or waiting: "<workflow> has a run that is running; let it finish or cancel it first" |
| Release a run waiting at a stage gate | Administrator | |
| Answer a question in a graph run | Developer | If the node names answerers, only those people (an administrator not named is refused: "only <name> may answer this question"). If it names none, only an administrator: "an administrator answers this question". A person who may not answer sees "Waiting for an administrator to answer" and no answer box |
| Cancel a run | Developer | Its starter, or an administrator: "only the person who started it, or an administrator, can cancel a run" |
| Make the webhook secret | Administrator | Shown once; the old one stops working |

## How it works

1. A developer or administrator saves a workflow as version 1. Each save is a new version with its agent versions pinned.
2. Something starts it (see below). Agents read what started it under `trigger`, as data.
3. The run goes through its nodes (or stages). Agents work with the tools their run-as role may use.
4. A question for a person waits at the top of the run until it is answered.
5. Anything an agent proposes goes to **Approvals**, and a different administrator approves it.

### Ways to start

| Trigger | How it works |
| --- | --- |
| By hand | **Run** on the workflow |
| On a schedule | See [Schedules](schedules.md) |
| Signed webhook | **Webhook secret** on the workflow's page shows the secret once. Send `X-Forgeline-Signature-256` (or GitHub's `X-Hub-Signature-256`), `sha256=` and the hex HMAC-SHA256 of the body. A bad signature, unknown workflow or missing secret all answer 401 "the signature does not match". The body is at most 64 KB. 30 webhook runs an hour at most |
| After a pipeline run fails | Name the pipelines (or `*`); the failed run is the `trigger` |
| A CloudWatch alarm | See [Alarm intake](alarm-intake.md) |

### Stages and gates

A workflow made of **stages** still runs as before. Each stage has agent steps and an optional gate. A run waiting at a
gate starts its next stage when an administrator releases it. **Edit on the canvas** turns one into a graph, as a new
version.

### Run-as role

A workflow never runs as an administrator: it runs as a viewer or a developer (set on the canvas, in **Settings → Permissions** of that workflow). Its
agents may use only the tools that role may use. Saving a workflow whose agent has a tool its run-as role may never use is
refused, with the tool named. For example `aws_script` and `search_audit` are for administrators only, and a workflow
running as a viewer may not use `call_aws` or proposal tools either. `call_aws` runs one read-only AWS CLI call for
developers and administrators, so a workflow running as a developer can use it.

## Archive and restore

**Delete** on a workflow's page archives it: it is turned off and listed under **Archived**, and can be restored for 30
days, then it is deleted for good. Its past runs stay.

## Limits

A stage workflow has at most 6 stages of 4 steps each (a task is at most 4,000 characters). A graph has at most 40 nodes and
80 wires, 4 nodes at once, 10 visits to one node and 50 steps a run. The newest 200 runs are kept.

## Security model

- Never runs as an administrator.
- A tool the run-as role may not use is refused at save.
- Agents read the trigger as data, never as instructions.
- Webhooks are signed, rate limited (30 runs an hour) and answer 401 in the same way for every failure.
- Proposals an agent makes wait for a different administrator.

## Limits and verification

{: .warning }
**Graphs were checked with the real model on 2026-11-17**, on the maintainer's Pi, with two read-only agents and a
12-node graph ([test note](../testing/2026-11-17-agent-graphs.md)). Other graphs and the alarm trigger have not all been run
against a real model; see [Alarm intake](alarm-intake.md) for its own check.

**Checked again through the console on 2026-11-27**, on the UAT instance at commit `487f373`, with the real model: a graph
drawn in the canvas (an agent and two questions) was started by a developer, one question was answered by an
administrator and the other by the one person it named, and each refusal in the table above was seen (a developer on a
question for administrators, an administrator on a question for a named person, a second start, a delete while running,
and 403 for a developer on create, delete and the webhook secret and for a viewer on start). Saving was refused with the
messages in Troubleshooting for 41 nodes, an unreachable node, a decision without `else` and an agent with `aws_script` in
a workflow run as a developer; the webhook answered 401 for no or a wrong signature, 409 for a workflow whose version has no
webhook and 413 for a body over 64 KB. Not run: a stage workflow, the 30-an-hour webhook limit, a webhook that starts a
run, and the alarm and failed-pipeline triggers. Delete archived the workflow (it left the list and showed under
**Archived**), and restoring brought it back turned off ("this workflow is turned off" on a start). See the
[test note](../testing/2026-11-27-uat-workflows-teams.md).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| Saving is refused, naming a tool | The tool is not allowed for the workflow's run-as role, for example "agent \"x\" v1 has aws_script (needs admin), which a run as developer may never use". |
| Start is refused with 409 "this workflow already has a run in progress" | A run is going or waiting for an answer; let it finish or cancel it. |
| A webhook answers 401 | Wrong signature, unknown workflow, or no secret made yet. |
| A webhook answers 409 | "the current version of this workflow does not start from a webhook", "this workflow is turned off", or "a run of this workflow is already in progress". |
| A webhook answers 413 | The body is over 64 KB. |
| A webhook answers 429 | "this workflow already started 30 times from its webhook in the last hour". |
| A run is stuck waiting | A question or an approval is waiting for an answer at the top of the run. |
| Delete is refused | A run is going or waiting. |

## Related

- [Workflow canvas](workflow-canvas.md)
- [Schedules](schedules.md)
- [Alarm intake](alarm-intake.md)
- [Agents](agents.md)
- [Runbooks](runbooks.md)
