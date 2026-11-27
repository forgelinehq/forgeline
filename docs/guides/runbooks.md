---
title: Runbooks
parent: Guides
nav_order: 9
---

# Runbooks

A runbook is a reviewed shell script that checks or fixes something on your servers, run through AWS Systems Manager
(SSM). One administrator writes it, a different administrator reviews it, and only the reviewed text ever runs.
People, agents and workflows can start a runbook, but none of them can change what it does.

Use runbooks when an operational task is routine and risky enough to deserve a second pair of eyes: a build server
whose disk keeps filling up, a stuck service that needs a restart, a backup folder that needs pruning.

## At a glance

| | |
| --- | --- |
| Where | **Operations → Runbooks** |
| Runs on | EC2 instances, through SSM `AWS-RunShellScript`, as root |
| Needs | An [AWS connection](connect-aws.md) and the SSM agent on the instances |
| Design | [Note 051](../design/051-runbooks.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See runbooks and their runs | Developer | |
| Write a runbook or a new version | Administrator | |
| Review (approve) a version | Administrator | Not the version's author |
| Run a **check** | Developer | Needs a reviewed version |
| Ask for a **fix** | Developer | Needs a reason; waits for approval |
| Approve or reject a fix | Administrator | Not whoever asked for it; the [approval rules](approval-rules.md) also apply |
| Withdraw a fix before it is decided | Developer | Whoever asked, or an administrator |

## How it works

1. An administrator saves a runbook. The version waits for **review**.
2. A different administrator reads the exact script and approves it. The approval is tied to the script's hash, so
   any later edit is a new version that needs its own review. The reviewed version keeps running until then.
3. Someone picks a runbook and fills in its parameters.
   - A **check** starts at once and shows each instance's output.
   - A **fix** is filed under **Home → Approvals** with your reason. A different administrator sees the exact script,
     values and instances and approves or rejects. Nothing approves a fix automatically.
4. Forgeline sends the command through SSM, collects the output (cut at 8,000 characters, credentials hidden) and
   records the run, with a trace, in the audit log.

### Checks and fixes

| | Check | Fix |
| --- | --- | --- |
| Changes anything | No: it only reads | Yes |
| Runs when asked | Immediately | After a different administrator approves |
| Typical use | Free disk space, largest folders, `docker system df`, backup age | Prune Docker, delete old backups |

## Write a runbook

1. Choose **New runbook**, or **Start from this** on a built-in example. The examples target a build server whose disk
   keeps filling up: **Disk report** (a check), **Docker cleanup** and **Old backups** (fixes; the second always keeps
   the newest few files).
2. Write the script (up to 8,000 characters). It runs as root, with each parameter set as an environment variable.
3. Add **parameters** (see below), say **where it runs**, and set a time limit (30 to 3,600 seconds; 300 if you leave it).
4. Choose **Save for review**. A different administrator opens the runbook and approves that version.

### Parameters

A runbook has at most 8 parameters. A parameter is a **choice** from a list or a **whole number** in a range, never free
text, so nothing an agent writes can reach the shell as code. A value outside the list or range is refused, for example
`KEEP: a whole number from 1 to 100`. A value arrives as a single-quoted environment variable; quote it in the script
(`"$BACKUP_DIR"`).

### Where it runs

Name the instance ids, or a tag key and value such as `forgeline-runbooks=jenkins`. A run reaches at most 20 instances.
A tag that matches more than 20 running instances is refused (`25 instances have the tag; a run reaches at most 20`),
not cut down, and a tag that matches none says `no running instance has the tag forgeline-runbooks=jenkins`.

## Run a runbook

Open the runbook, pick the values, then **Run the check** or **Ask for this fix**. A fix needs a reason of 10 to 800
characters; the approver reads it. Worked example: on the **Docker cleanup** fix, set `OLDER_THAN_HOURS` to 24, give the
reason "Jenkins disk at 93%, 24.9 GB of images reclaimable", and another administrator approves it. The run page shows
each instance's status, exit code and output, **The exact script sent**, and a link to the trace.

![A check's output: each instance's status, exit code and output]({{ '/console/runbooks-check-output.png' | relative_url }})

A fix shows the same page with the approver's controls: the instances, the values, the reason and the exact script,
and the **Approve and run** and **Reject** buttons. When the rules ask for two approvals the first button reads
**Approve (1 of 2)**.

![A fix waiting for a different administrator to approve it]({{ '/console/runbooks-approve-fix.png' | relative_url }})

## Use runbooks from agents

Give an agent the tool **run_runbook** (under *Security and health*). It can list the reviewed runbooks, run checks and
read their output, and file fixes for approval. It chooses a runbook and its values; it never writes commands.

Turning off the AI's access to **Virtual machines and networks** or **Other services** in the AWS service switches stops
agents from running runbooks. People still can.

## Permissions in AWS

The instances need the SSM agent and an instance profile that allows it, for example `AmazonSSMManagedInstanceCore`.
The role or user Forgeline uses needs to send commands only to the tagged instances and read their results:

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

## Security model

- **Two people.** The author cannot review their own version, and the person who asks for a fix cannot approve it.
- **Exact text.** A review and an approval each bind to the script's hash. If the runbook changes after a fix is filed,
  the approval is refused (`the runbook changed since this was proposed; ask for it again`).
- **No free text.** Parameters are typed, so an agent's words never become shell code.
- **Bounded.** A run is limited to 20 instances, a time limit and 8,000 characters of output. A fix that nobody approves
  within 24 hours expires (`this fix was proposed more than 24 hours ago; ask for it again`).
- **Audited.** Reviews, runs, approvals, rejections and cancellations are all in the audit log.

## Limits and verification

{: .warning }
**Checked on one real instance.** The SSM calls were tested with fakes and `scripts/dev/fake-aws`. On 2026-11-26 the
Disk report and Old backups ran on a real `t3.nano` (see [the test note](../testing/2026-11-26-alarm-runbook.md)). The
other example scripts have only been checked to parse with `bash -n`. Try them on a test instance first.
On 2026-11-27 (commit `487f373`) the list, a runbook's page, its version and its review were read as a developer on the UAT instance; no
runbook was run, written or reviewed there ([note](../testing/2026-11-27-uat-cloud-security.md)).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| The run is refused with `no version of this runbook has been reviewed yet, so it cannot run` | Nobody has approved a version yet. |
| `you wrote this version; a different administrator reviews it` | You tried to review your own version. |
| `you asked for this fix; a different administrator approves it` | You tried to approve your own fix. |
| A run stays queued | The instance has no SSM agent or instance profile, or the tag does not match. |
| An approval is refused | The runbook changed since the fix was filed, or the approver is the person who asked. |
| An agent cannot run runbooks (`an administrator turned off the AI's access to virtual machines or other AWS services`) | The AWS service switches for **Virtual machines and networks** or **Other services** are off. |
| Output ends early | Output is cut at 8,000 characters; print less, or narrow the command. |

## Related

- [Alarm intake: start a workflow from a CloudWatch alarm](alarm-intake.md)
- [Connect AWS](connect-aws.md)
- [Approval rules](approval-rules.md)
- [API: runbooks](../api/reference.md)
