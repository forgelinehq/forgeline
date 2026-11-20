# 051: Runbooks: fix things on servers, through SSM, with approval

Status: **accepted 2026-11-20, every decision as proposed** (end of this note); being built. Starts phase 4 of the product direction (AI SRE:
health, alerts, remediation) early, because the maintainer wants a demo now: "a Jenkins which gets out of space every
time and I have Copilot from local who gets into the server by SSM and cleans spaces safely, docker prune and docker
layers, db backups. I wanted to do the same from here, with a hook or even a manual run, but the system is not capable
of doing anything."

## Question

Forgeline's agents only read; every change is a proposal of AWS API calls that a different administrator approves.
SSM is on the list of services an AI proposal may never touch, because a shell command on a host can do anything.
How can an agent clean a full disk on a Jenkins server, from an alarm or by hand, without becoming a remote shell?

## Decisions (proposed)

### 1. Runbooks, not commands

- A **runbook** is a shell script an administrator writes and a **different administrator reviews** before it can run,
  with typed parameters (a choice from a list, or a whole number in a range; never free text), a target (instance ids,
  or a tag such as `forgeline-runbooks=jenkins`), a timeout and an output limit. Every change makes a new version that
  needs review again.
- Agents and the assistant cannot write commands. Their only tool is `run_runbook`: a runbook id and parameter values,
  checked against the runbook's types.
- Two kinds:
  - **Check**: only reads (disk use, the largest folders, `docker system df`, the age of backups). It may not delete,
    move, write or restart anything, and the reviewer confirms that.
  - **Fix**: changes something (`docker system prune` with filters, removing build caches older than N hours, deleting
    backups older than N days while keeping the newest K, vacuuming the journal).
- They run through **SSM Run Command** (`AWS-RunShellScript`) on instances with the SSM agent, with parameter values
  passed as environment variables and quoted by the script, never pasted into it. Output is cut, has credentials
  hidden, and goes into the run, its trace and the audit log.
- The connection needs `ssm:SendCommand` and `ssm:GetCommandInvocation` only on the tagged instances; the note gives the
  IAM policy.

### 2. Who may run what

| | Check | Fix |
| --- | --- | --- |
| By hand | a developer | a proposal a different administrator approves |
| By an agent or a workflow | runs (decision for the maintainer) | a proposal a different administrator approves |
| From an alarm | runs (decision for the maintainer) | a proposal; pre-approval by rule is a decision for the maintainer |

### 3. The Jenkins demo

1. A CloudWatch alarm (disk over 85% on `jenkins-prod`) posts to the workflow's signed webhook through SNS.
2. The workflow runs the check **Disk report**: free space, the largest folders, `docker system df`, backup ages.
3. An agent reads the report and picks fixes with parameters: **Docker cleanup** (stopped containers, dangling images,
   build cache older than 24 hours) and **Old backups** (in `/var/backups/jenkins`, older than 14 days, keep the newest
   5). Each becomes a proposal showing the exact script, the parameter values and the instance.
4. An administrator approves (in the console; Slack later).
5. The fixes run; the check runs again and the report says how much space came back.
6. Every step is a trace; nothing runs that a person did not write and another review.

Forgeline ships these three runbooks as examples, to review and adopt.

### 4. Agents and the AWS CLI

The maintainer asked whether agents should get the AWS CLI when Forgeline's own lookups are not enough, or more
functions. Proposed: both, in that order.

- **Functions first** for what people ask often: they are summarised, paginated, safe for viewers and fit one answer
  (`aws_resources` with filters, `guard_findings`, `inspector_findings`; next: CloudWatch alarms and metrics, ECR images
  with their scan results, cost by service).
- **A read-only CLI as the fallback**: `call_aws` (describe, list and get calls only; no secrets, data or logs) is for
  administrators only today, so it never works in a workflow, which never runs as an administrator. Proposed: open it
  to developers with the same checks, so a workflow running as a developer can use it. A decision for the maintainer.

## Decisions for the maintainer

All three were answered **as proposed** on 2026-11-20: checks run without a per-run approval; every fix needs a
different administrator each time (no pre-approval); read-only `call_aws` is open to developers.

1. May **check** runbooks run without an approval each time (the reviewed script is the approval), from agents,
   workflows and alarms? Proposed: yes.
2. May an approval rule **pre-approve** a named fix runbook from a named alarm (for example Docker cleanup when the
   Jenkins disk alarm fires), or does every fix need a person each time? Proposed: a person each time for now; this
   would change the decision that nothing is applied automatically.
3. Should read-only `call_aws` be open to developers, so workflows running as developers can use it? Proposed: yes.
