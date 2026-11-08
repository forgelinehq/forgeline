# 030: Service switches for the AI

Status: **Built for AWS, with three levels (Off, Look only, Change with approval) since 2026-11-08.** Date: 2026-11-07. Builds on notes 025 (integrations hub), 026 (access policy),
016 and 021 (AWS MCP Server, per-account writes) and 029 (console redesign). Facts about the code are from
`origin/main` at `d7a9450`.

## Question

An administrator who connects AWS wants to say "the AI may work with S3, ECS and ECR, and nothing else".
The access policy (note 026) works per **tool** (`call_aws`, `aws_script`, `propose_aws_change`), and one
tool reaches every AWS service, so it cannot say that. How do we let a person switch AWS services on and
off for the AI, in plain words, without adding a second way to reach AWS?

## Decisions

| Decision | Choice |
| --- | --- |
| What a switch is | One per AWS service group, in plain words with the technical name beside it: File storage (S3), Containers (ECS), Image registry (ECR), Virtual machines and networks (EC2, EBS, VPC), Functions (Lambda), Databases (RDS), Tables (DynamoDB), Load balancers (ELB), Queues (SQS), Notifications (SNS), Stacks (CloudFormation), Logs and alarms (CloudWatch), Costs (Cost Explorer), and **Other services** for every AWS service not listed |
| Default | Everything on. Nothing stored means today's behaviour. **Other services** exists so a service missing from the list is never silently allowed once someone turns it off |
| Who it governs | The AI: the assistant, agents and MCP clients. People still see everything in the explorer and on the map; the switches are about what the AI may touch, not what a person may look at |
| Where it is enforced | Next to the policy gate in `runLookup`, the one place every tool call passes, and in the MCP server's `propose_cloud_change`. The service of a call is read before it runs: `call_aws` from the CLI command, `aws_script` and `propose_aws_change` from the calls `checkScript` already requires to be literal (and the sandbox's own report must match them), `propose_cloud_change` from its kind (stacks are CloudFormation, instance actions EC2), `aws_summary` reads stacks and instances |
| Reads that list many services | `aws_resources` and `guard_findings` leave out what is off, and end with a line naming what was left out, so the model can tell the person instead of guessing |
| Approval | An AI proposal for a service turned off after it was filed is refused at approval (409), and nothing runs |
| Failing safely | An unreadable policy refuses (the policy gate already does for every AWS tool; the approval path checks it too). Unknown switch names are refused with 400 |
| Storage and API | In the same stored policy as the tool levels, under `services`, so one read gives both. Saved through its own `PUT /v1/integrations/aws/services`, administrators only, so saving tool levels never resets services and the other way round. Audited as `policy.services` with before and after |
| Console | The AWS Access tab starts with "Services the AI may use": a switch per service, read-only for developers |
| Coding engines, Codex and Copilot | Not part of this note. The maintainer will add them later; they will run under the same switches because they will reach AWS only through these tools |

## Look only (added 2026-11-08, frame 2 of note 029)

The screen frames the maintainer confirmed show three levels per service. The third, **Look only**, is stored as
`services_read` beside `services` (off). A look-only service can be read like any other, but `propose_aws_change`
and `propose_cloud_change` refuse it before anything is filed, and approval refuses an AI proposal for it if it was
set to Look only after the proposal was filed. A service cannot be both off and look only (400). The console shows
the three levels as one control per service in the AWS connection's **Services** tab. Tested: reads still work;
proposals and MCP cloud changes are refused; approval refuses; overlap refused; saved and shown; each rule removed
in turn fails a test (six mutations).

## Decisions recorded for the redesign (maintainer, 2026-11-07)

The maintainer chose an enterprise focus and left the remaining choices from note 029 to us:

- **No automatic apply**, not even in sandboxes. Every change stays a proposal approved by a different
  administrator. Enterprises expect this, and "approval rules" (who may approve which change) is the way to
  relax it later, not auto-apply.
- **OpenTofu** for the code blueprints will generate; CloudFormation stays where it is used.
- **Our own graph layout**, grown from `ui/lib/dag.js`; dagre (MIT) only if needed; no elkjs (EPL-2.0).
- Codex and Copilot CLI engines: later.
- Next, in order: single sign-on (OIDC), approval rules, audit export; then blueprints and the design canvas.

## Not covered

- Per-account switches (production on, sandbox off). One set applies to every AWS account today.
- The context the assistant gets from the page a person is looking at (for example the Infrastructure page)
  is what that person sees, and is not filtered.
- Cloud changes filed over MCP are checked when filed, not again at approval: a cloud change does not record
  that the AI filed it.
- Switches for Kubernetes, Google Cloud and Azure.

## What was checked, and what was not

Checked by tests: the mapping of AWS API and inventory names to switches (unknown names are Other); that an
off service is refused in `call_aws`, `aws_script`, both proposal tools and `aws_summary` before anything
reaches the CLI or the MCP server, for administrators and agents alike; that resources and findings leave it
out and say so; that approval refuses an earlier proposal, and works again once the service is back on;
that an unreadable policy refuses approval; administrators only; unknown switches refused; tool levels and
services saved independently; audit entries. Eleven rules were removed in turn and each made a test fail. The
gate's own fail-closed branch cannot be reached by a test, because the policy gate refuses first; that is
defence in depth, not the guarantee.

Checked in a browser: see the pull request. Not checked against real AWS beyond the existing write test.
