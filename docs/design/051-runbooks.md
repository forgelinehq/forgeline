# 051: Runbooks: fix things on servers, through SSM, with approval

Status: **accepted 2026-11-20, every decision as proposed** (end of this note); **slice 1 built 2026-11-21**: runbooks with
review, typed parameters, checks and approved fixes through SSM, `run_runbook` for agents, three examples, the console
pages (see "Built" at the end). Checked against stand-ins only. The alarm intake was built 2026-11-26 (see the end of this note). Starts phase 4 of the product direction (AI SRE:
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

## Built (slice 1, 2026-11-21)

- `console_runbooks.go`: runbooks with versions, review by a different administrator of the exact text (by hash),
  typed parameters (`choice` with letters, digits, spaces and `. _ / : @ + -` only; `int` in a range; names that are
  not shell variables such as `PATH` or `LD_PRELOAD`), targets by instance id or tag (at most 20). A check runs at
  once; a fix is a proposal under the approval rules (kind `runbook`), refused to whoever asked, for another hash, or
  when the runbook changed since. Execution sends `AWS-RunShellScript` with the values as single-quoted environment
  lines before the reviewed script, polls `list-command-invocations`, keeps each instance's output (8,000 characters,
  credentials hidden) and makes a trace. A run whose status cannot be read five times in a row fails with the SSM
  command id; one lost in a restart is reported as failed.
- `run_runbook` for developers and agents: no id lists the reviewed runbooks; a check returns the output; a fix is
  filed. Any field beyond `runbook`, `params` and `reason` is refused. The AWS service switches for virtual machines and
  other services stop agents, not people. In evals the tool is recorded, never run.
- Console: Operations › Runbooks (list, examples, editor with parameter rows, review, run form), a run page with the
  script, output and approval, and fixes on Approvals.
- Checked in a browser against `scripts/dev/fake-aws`: the three examples made, self-review refused, the check run with
  its output, a fix asked for, approved by the other administrator, run, with the audit trail and the trace. Not checked
  against a real instance.
- While testing, `hideSecrets` was found to miss names joined by underscores (`aws_secret_access_key=`, `DB_PASSWORD:`,
  `GITHUB_TOKEN=`); it now catches them everywhere it is used.

## Built (alarm intake, 2026-11-26)

Decision 3 said a CloudWatch alarm posts to the workflow's signed webhook through SNS. SNS can only deliver to a **public HTTPS address**,
and a Forgeline server is usually not one (the Pi is tailnet-only; "local first"), and SNS does not sign with our HMAC but with its own
certificate scheme, which would need subscription handshakes and signature checks. So the alarm reaches Forgeline through **SQS**:
alarm → SNS topic → SQS queue, and Forgeline polls the queue with the AWS connection's credentials (outbound only).

- Settings › Connections › Alarm intake: the queue URL, on/off, a test, a status (last read, error, counts), and the CLI commands that
  make the queue, the topic and the queue's policy (the lock: only that topic may send). A graph workflow's version has `on_alarm`: alarm
  names, `*` allowed, validated, at most ten, part of the version's hash.
- A message is read by a long poll (20 seconds), parsed (an SNS envelope or a raw alarm), and acted on only if it is an alarm going to
  `ALARM` from the queue's own account; everything else is deleted and, if it was not an alarm or came from elsewhere, audited as refused.
  A message id seen before is skipped; each workflow gets at most six alarm runs an hour; a workflow that is off, archived or not a graph
  is never started. The alarm's name, reason, region, metric and dimensions reach the workflow as its `trigger`, cut, and read as data.
- The poller backs off (5 s to 5 min) when the queue cannot be read, reports the error on the page, notices a change of settings at once,
  and stops when the intake is turned off.
- Tests with a fake queue (the first run, each refusal, a repeat, the region used, flapping with one run at a time so only the limit can
  stop it, an erroring queue, the test button); removing each protection failed a test. **Not checked:** a real alarm through real SNS and
  SQS, and the runbook that follows (the disk report, the fixes, the approval) on a real instance.

