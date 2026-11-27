---
title: Alarm intake
parent: Guides
nav_order: 10
---

# Alarm intake

A CloudWatch alarm can start a workflow. The workflow runs a [runbook](runbooks.md) check, lets an agent pick fixes, and
sends them for approval. Nothing is changed until a person approves.

The path is **alarm → SNS topic → SQS queue → Forgeline**, and Forgeline *reads* the queue. It reads instead of being
called because SNS can only deliver to a public HTTPS address, and a Forgeline server usually is not one. Nothing has to
reach the server from outside.

## At a glance

| | |
| --- | --- |
| Where | **Settings → Connections → Alarm intake**, and **Trigger** in a graph workflow's settings |
| Needs | An [AWS connection](connect-aws.md), an SQS queue and an SNS topic |
| Design | [Note 051](../design/051-runbooks.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See the intake settings and status | Developer | Through the API only (`GET /v1/settings/alarm-intake`); the console page needs an administrator (a developer sees `You do not have access to this page`) |
| Turn the intake on or off, set the queue, **Test the queue** | Administrator | Needs the AWS connection |
| Add alarm names to a workflow's trigger | Administrator | Saving a workflow version is an administrator action; it must be a graph workflow |

## How it works

1. Forgeline asks the queue for messages in the background, using the AWS connection's credentials.
2. A message is an SNS notification (or a raw alarm) naming the alarm. Anything else in the queue is deleted and audited
   as `alarm.refused`.
3. When the alarm goes to `ALARM`, Forgeline starts every workflow whose trigger names it. A trigger is an exact alarm
   name or a pattern with `*`, such as `disk-*`.
4. The workflow runs as the role it was saved with and files any fix as a proposal for a different administrator.

## Set it up

1. Under **Alarm intake**, copy the commands that make the queue, the topic and the queue's policy. The policy lets only
   that topic send to the queue, which is the lock. Run them in your account.
2. Paste the queue URL into **Queue address**, turn on **Read the queue**, choose **Save**, then **Test the queue**.
   The test needs a saved address; it asks AWS for the queue's attributes without reading a message.
3. In a graph workflow's **Workflow settings → Trigger**, add the alarm names (or a pattern such as `disk-*`) under
   **When a CloudWatch alarm goes off**, and save the workflow.
4. In CloudWatch, set the topic as an action of the alarm.

The AWS connection's credentials need `sqs:ReceiveMessage`, `sqs:DeleteMessage` and `sqs:GetQueueAttributes` on that
queue.

## What the workflow receives

The workflow starts with the alarm's details (name, state, reason, description, region, account, time, namespace, metric
and its dimensions such as the instance id) as its `trigger`. Agents read it as data, never as instructions. A change to
`OK` or `INSUFFICIENT_DATA` starts nothing.

Worked example: an alarm `disk-jenkins` on `DiskUsedPercent` goes to `ALARM`. A workflow with the trigger `disk-*`
starts, its agent runs the **Disk report** check on the instance named in the dimensions and files **Docker cleanup** as
a fix. Nothing changes until a different administrator approves it.

## Safeguards

- An alarm that names another AWS account than the queue's is refused and audited (`alarm.refused`). An alarm with no
  account in it is not checked this way, so the queue policy is what keeps other senders out.
- A message delivered twice is acted on once (Forgeline remembers what it has seen for 24 hours).
- Each workflow starts at most six runs an hour from alarms, so a flapping alarm cannot start a run every time.
- A workflow that is off, archived or not a graph workflow is never started.
- The audit log records each alarm, each refusal and each run started.

Anyone who can send to the queue can start workflows, so keep its policy tight.

## Limits and verification

{: .warning }
**Checked once against real AWS.** A real alarm through a real topic and queue started a workflow, an agent ran a check
and filed a fix, and an administrator approved it (2026-11-26, [test note](../testing/2026-11-26-alarm-runbook.md)). The
limits and the account check are tested with a fake `aws` only. On 2026-11-27 (commit `487f373`) the settings were read on the UAT instance, where
the intake is off and no queue is set, so nothing was started or tested ([note](../testing/2026-11-27-uat-cloud-security.md)).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| Save is refused with `queue_url: an SQS queue address such as https://sqs.us-east-1.amazonaws.com/123456789012/forgeline-alarms` | The address is not an SQS queue URL. |
| Save is refused with `connect AWS first: the intake uses the AWS connection's credentials` | There is no AWS connection. |
| **Test the queue** says `save the queue address first` | The address was typed but not saved. |
| **Test the queue** says `the queue could not be reached: ...` | The credentials lack SQS permissions, or the queue is in another account or region. |
| The alarm fires but nothing starts | The workflow is off, is not a graph, or its trigger names do not match the alarm. |
| A refusal in the audit log | The alarm came from another account, or the message was not an alarm. |
| Fewer runs than alarms | The six-an-hour limit per workflow applies; the audit line `alarm.received` says `too many runs this hour`. |

## Related

- [Runbooks](runbooks.md)
- [Workflows](workflows.md) and the [workflow canvas](workflow-canvas.md)
- [Connect AWS](connect-aws.md)
- [Audit log](audit-log.md)
