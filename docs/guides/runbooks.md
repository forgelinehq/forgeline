---
title: Runbooks
parent: Guides
nav_order: 9
---

# Runbooks: check and fix servers through SSM

A runbook is a shell script that checks or fixes something on your servers, run through AWS Systems Manager (SSM)
Run Command. One administrator writes it and a **different administrator reviews** it; only a reviewed version runs.
Agents and the assistant can run runbooks, but they choose a runbook and its values; they never write commands.
See [design note 051](../design/051-runbooks.md).

Go to **Operations → Runbooks**.

## Two kinds

- A **check** only reads: free disk space, the largest folders, `docker system df`, the age of backups. Once
  reviewed, developers, agents and workflows run it without asking each time. Its reviewer confirms it only reads.
- A **fix** changes something: prune Docker, delete old backups. **Every run** waits for a different administrator to
  approve the exact script, values and instances, under **Approvals**. Nothing approves a fix by itself.

## Make one

1. **New runbook**, or **Start from this** on an example. Three examples are built in, for a build server whose disk
   keeps filling up: **Disk report** (a check), **Docker cleanup** and **Old backups** (fixes; the second always keeps
   the newest few files).
2. Write the script. It runs as root through `AWS-RunShellScript`.
3. Add **parameters**: a choice from a list, or a whole number in a range. Never free text, so nothing an agent writes
   reaches the shell as code. A value arrives as an environment variable, single-quoted; quote it in the script
   (`"$BACKUP_DIR"`).
4. Say where it runs: instance ids, or a tag such as `forgeline-runbooks=jenkins` (at most 20 running instances).
5. **Save for review.** A different administrator opens the runbook and approves that version; it becomes what runs.
   A later version waits for its own review, and the reviewed one keeps running until then.

## Run one

On the runbook's page, pick the values and **Run the check**, or **Ask for this fix** with a reason. A run shows each
instance's status, exit code and output (cut at 8,000 characters, credentials hidden), the exact script that was sent,
and a trace.

![A fix waiting for approval]({{ '/console/158-runbook-fix-approve.png' | relative_url }})

Give an agent the tool **run_runbook** (Security and health). It lists the reviewed runbooks, runs checks and reads
their output, and files fixes for approval. An administrator who turns off the AI's access to **Virtual machines** or
**Other services** under the AWS service switches stops agents from running runbooks; people still can.

![A check's output]({{ '/console/159-runbook-check.png' | relative_url }})

## What the AWS connection needs

The instances need the SSM agent and an instance profile that allows it (for example `AmazonSSMManagedInstanceCore`).
The role or user Forgeline uses needs to send commands only to the tagged instances, and read their results:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    { "Effect": "Allow", "Action": "ssm:SendCommand",
      "Resource": "arn:aws:ssm:*:*:document/AWS-RunShellScript" },
    { "Effect": "Allow", "Action": "ssm:SendCommand", "Resource": "arn:aws:ec2:*:*:instance/*",
      "Condition": { "StringEquals": { "aws:ResourceTag/forgeline-runbooks": "jenkins" } } },
    { "Effect": "Allow", "Action": ["ssm:ListCommandInvocations", "ec2:DescribeInstances"], "Resource": "*" }
  ]
}
```

{: .warning }
**Checked only against a stand-in.** The SSM calls were tested with Forgeline's own fakes and
`scripts/dev/fake-aws`, not against a real instance. The example scripts are checked to parse with `bash -n`; they have
not run on a real server. Try them on a test instance first.

## Starting a workflow from a CloudWatch alarm

A CloudWatch alarm can start a workflow, which runs a check, lets an agent pick fixes and sends them for approval. The path is
**alarm → SNS topic → SQS queue → Forgeline**, and Forgeline *reads* the queue. It reads instead of being called because SNS can only
deliver to a public HTTPS address, and a Forgeline server is usually not one: nothing has to reach the server from outside.

1. Under **Settings › Connections › Alarm intake**, copy the commands that make the queue, the topic and the queue's policy (only that
   topic may send to the queue, which is the lock), run them in your account, paste the queue URL, turn the intake on and press
   **Test the queue**. The intake uses the AWS connection's credentials, which need `sqs:ReceiveMessage`, `sqs:DeleteMessage` and
   `sqs:GetQueueAttributes` on that queue.
2. In a graph workflow's **Workflow settings › Trigger**, add the alarm names (or a pattern such as `disk-*`) under **When a CloudWatch
   alarm goes off**. In the alarm, set the topic as an alarm action.
3. When the alarm goes to `ALARM`, the workflow starts with the alarm's details (name, reason, region, metric and its dimensions such as
   the instance id) as its `trigger`, which agents read as data and never as instructions. Going back to `OK` starts nothing.

Safeguards: only a message from the queue's own account is acted on (the others are refused and audited); a message delivered twice is
acted on once; each workflow starts at most six runs an hour from alarms, so a flapping alarm cannot start a run each time; a workflow
that is off, or is not a graph, is never started; and the audit log records each alarm, each refusal and each run started. Anyone who
can send to the queue can start workflows, so keep its policy tight.

{: .warning }
**Checked against a stand-in queue only so far.** The polling, the parsing of SNS-wrapped alarms, the account check, the limits and the
audit entries are tested with a fake `aws`; a real alarm through a real topic and queue has not run yet.
