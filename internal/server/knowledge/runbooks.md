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

{: .note }
The plan for alarms: a CloudWatch alarm posts to a workflow's signed webhook through SNS, the workflow runs **Disk
report**, an agent picks the fixes, and an administrator approves them. Workflows can already run a check with
`run_runbook`; a ready-made alarm workflow is next.
