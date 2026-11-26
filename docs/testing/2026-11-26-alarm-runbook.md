---
title: CloudWatch alarm to runbook, checked on real AWS, 2026-11-26
---

# A real alarm starts a workflow, an agent runs a runbook, an administrator approves the fix

The first run of the alarm intake ([note 051](../design/051-runbooks.md)) against real AWS, on the maintainer's Raspberry Pi
Forgeline (main at `2d9dbee`). Everything was made on the Pi with the server token, as the maintainer allows for Claude's own tests.
The maintainer reviewed both runbooks and approved the fix as `vicky`. Account numbers are masked.

## What was built in AWS (ap-south-1)

A `t3.nano` (the `t4g` types had no capacity in any zone) with the SSM agent, tagged `forgeline-runbooks=jenkins`; an SNS topic;
an SQS queue whose policy lets only that topic send; a CloudWatch alarm on `Forgeline/Test` `DiskUsedPercent` (over 85, one
minute) sending to the topic. The instance held six 120 MB files in `/var/backups/jenkins`.

## Steps and what happened

1. Two runbooks (Disk report, a check; Old backups, a fix), an agent and a graph workflow started by the alarm were filed on the Pi.
   The author was the server token, so a different administrator had to review them. `vicky` reviewed both (10:35).
2. Alarm intake enabled with the queue address; **Test** answered `ok` (queue ARN read).
3. `put-metric-data` of 91 for the instance. About a minute later the alarm went to `ALARM`.
4. Forgeline read the message: audit `alarm.received` "went to ALARM; started 1 workflow(s)" (10:37).
5. The agent ran **Disk report** on the instance through SSM (`runbook.done`: succeeded) and then filed **Old backups** with
   `OLDER_THAN_DAYS=7`, `KEEP=2` (`runbook.propose`, "waits for a different administrator").
6. `vicky` approved it (10:40). The run succeeded.

## Result

| | Before | After |
|---|---|---|
| Files in `/var/backups/jenkins` | 6 (11-21, 11-16, 11-11, 11-06, 11-01, 10-27) | 2 (11-21, 11-16) |
| Disk used | 2.3 G | 1.8 G |

The newest two stayed and the four older ones went, as asked.

## Found along the way

- The first agent instructions named a parameter (`KEEP_NEWEST`) the runbook does not have; the runbook's own is `KEEP`. Fixed in
  the agent's second version before the run. Agents see parameter names only through their instructions, so name them exactly.
- Forgeline on the Pi uses its own AWS profile (`forgeline-default`, short-lived credentials from IAM Roles Anywhere), not the
  `default` profile of the login user there, which is a single sign-on login for another account and had expired. The test
  resources were made in the account Forgeline's profile reads (the intake's account check passed), using a local sign-in
  to that same account for the metric and the cleanup.
