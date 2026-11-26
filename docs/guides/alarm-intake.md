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
| Who | Administrator |
| Design | [Note 051](../design/051-runbooks.md) |

## Set it up

1. Under **Alarm intake**, copy the commands that make the queue, the topic and the queue's policy. The policy lets only
   that topic send to the queue, which is the lock. Run them in your account.
2. Paste the queue URL, turn the intake on and choose **Test the queue**.
3. In a graph workflow's **Workflow settings → Trigger**, add the alarm names (or a pattern such as `disk-*`) under
   **When a CloudWatch alarm goes off**.
4. In CloudWatch, set the topic as an action of the alarm.

The AWS connection's credentials need `sqs:ReceiveMessage`, `sqs:DeleteMessage` and `sqs:GetQueueAttributes` on that
queue.

## What happens

When the alarm goes to `ALARM`, the workflow starts with the alarm's details (name, reason, region, metric and its
dimensions such as the instance id) as its `trigger`. Agents read it as data, never as instructions. Going back to `OK`
starts nothing.

## Safeguards

- Only a message from the queue's own account is acted on; others are refused and audited.
- A message delivered twice is acted on once.
- Each workflow starts at most six runs an hour from alarms, so a flapping alarm cannot start a run every time.
- A workflow that is off, or that is not a graph, is never started.
- The audit log records each alarm, each refusal and each run started.

Anyone who can send to the queue can start workflows, so keep its policy tight.

## Limits and verification

{: .warning }
**Checked once against real AWS.** A real alarm through a real topic and queue started a workflow, an agent ran a check
and filed a fix, and an administrator approved it (2026-11-26, [test note](../testing/2026-11-26-alarm-runbook.md)). The
limits and the account check are tested with a fake `aws` only.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| **Test the queue** fails | The credentials lack the SQS permissions, or the URL is not an SQS queue. |
| The alarm fires but nothing starts | The workflow is off, is not a graph, or its trigger names do not match the alarm. |
| A refusal in the audit log | The message came from another account. |
| Fewer runs than alarms | The six-an-hour limit per workflow applies. |
