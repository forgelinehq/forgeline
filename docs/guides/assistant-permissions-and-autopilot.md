---
title: Assistant permissions and autopilot
parent: Guides
nav_order: 31
---

# Assistant permissions and autopilot

The assistant can change AWS as you, after you approve the exact command. Permission rules let you decide ahead of time
which commands are allowed, asked about or denied, and **autopilot** lets it keep going on a task without asking each
time.

Use permissions when you want the assistant to carry out an AWS change you have in mind and you are the person who would
approve it anyway. Every other account stays on the normal path: a proposal that a different administrator approves.

## At a glance

| | |
| --- | --- |
| Where in the console | **Settings → AI → Assistant permissions**; **Autopilot** under the message box |
| Needs | An [AWS connection](connect-aws.md); an account marked **allow with my own approval** |
| Design notes | [028](../design/028-assistant-acts-with-confirmation.md), [052](../design/052-assistant-that-works-like-a-colleague.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Approve an AWS change the assistant asks for | Administrator | Only in the chat with you present, and only on an account marked **allow with my own approval** |
| Mark an account "allow with my own approval" | Administrator | Every account is protected by default |
| Add, see or remove your own rules | Developer | Up to 100 for you |
| Add or remove an organisation rule | Administrator | Up to 100 for the organisation |
| Switch autopilot on or off in a conversation | Developer | |
| Turn autopilot off for everyone | Administrator | Under **Settings → AI → Assistant permissions** |

## How it works

1. A read runs at once.
2. For a change, the assistant shows a card with the command, the account and why it is asking. You choose **Allow
   once**, **Always allow** that kind of command (for example `aws ecs update-service *`), or **Deny**. No answer in five
   minutes is a no.
3. **Always allow** makes a rule that is yours, listed under **Settings → AI → Assistant permissions**. You can remove it.

A change only happens when all of these are true:

- you are an **administrator** (a developer is told to propose the change instead);
- it is the **chat** with you present (an agent or a background run never changes anything this way);
- the account is marked **allow with my own approval** by an administrator on the same page (every account is
  *protected* by default: a change to it is a proposal that a different administrator approves, as before);
- the service is not switched off or set to Look only under Connections → AWS.

### Rules

Rules let you decide ahead of time: allow, ask or deny a command pattern for yourself, or (administrators) for everyone.
`*` stands for any characters.

- A deny always wins. An organisation "ask" cannot be skipped by your own allow.
- An allow must name its service and operation.
- A rule is for AWS commands only and starts with `aws ` (for example `aws ecs update-service *`), up to 200 characters.
  An allow whose service or operation is a `*` is refused when you save it.
- Changes under `iam`, `organizations`, `account`, `ssm`, `sts`, `sso-admin` and `identitystore` always ask and cannot be
  allowed by a rule, and operations that would hand a secret to the assistant are refused.
- Every decision is in the audit log.

![The assistant asks before it changes AWS]({{ '/console/assistant-allow-change.png' | relative_url }})

## Use autopilot

1. Switch **Autopilot** on under the message box. You can do this at any time, also while the assistant is waiting for an
   answer: what it is waiting on is answered yes, and what it asks next runs.
2. Optionally **Set a limit**: minutes, steps, or both (at most 10,080 minutes, 7 days). With no limit it works until you stop it.
3. **Stop autopilot** ends it at once.

A change that would only have asked runs without a card. The chat shows each one ("Autopilot ran: ...") and the audit log
records it. With [Claude Code as the agent](assistant-agent-engines.md) and the sandbox on, autopilot also covers the
agent's own Bash, Edit, Write and fetches inside your container, each one shown and audited. On the server itself (sandbox
off) they still ask.

### What autopilot approves, and what it never does

| Approves | Never approves |
| --- | --- |
| AWS changes the permission check left as a plain ask | Running a pipeline |
| The agent's own commands and file changes inside the sandbox | Turning the sandbox or AWS changes on |
| Changes made through the `forgeline_api` tool | A secret for a command |
| Cards that save a pipeline, skill, agent or workflow (they show as done) | A protected account |
| A memory file, when the conversation has read nothing you did not type | A rule that denies or asks |
| | IAM, Organizations, accounts, SSM, STS, SSO admin and identity store |

Autopilot is on for **one conversation** and never carries over to another, or past a restart. An administrator can turn it
off for everyone under Settings → Assistant permissions.

## Security model

- **You approve the exact command**, as yourself, or a rule you wrote did.
- **Protected by default.** An account that was not marked keeps the proposal path and a different administrator.
- **Fixed floor.** IAM, Organizations, accounts, SSM, STS, SSO admin and identity store always ask, and are never covered by autopilot.
- **Present only.** Agents and background runs never change anything this way.
- **Audited.** Every decision and every autopilot action is in the audit log.

## Limits and verification

{: .warning }
**Checked against a fake AWS CLI only**, not a real account, and only for AWS. `az`, `gcloud` and `kubectl` are next.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| A developer is told to propose the change | Only an administrator can have the assistant change AWS. |
| The change becomes a proposal, not a card | The account is protected, not marked **allow with my own approval**. |
| It still asks under autopilot | It is one of the things autopilot never approves (see the table), or the sandbox is off and the command is the agent's own. |
| The change is refused | The service is switched off or Look only under Connections → AWS, or a rule denies it. |
| The card disappeared | No answer in five minutes is a no. |

## Related

- [Assistant](assistant.md)
- [Assistant sandbox](assistant-sandbox.md)
- [Connect AWS](connect-aws.md)
- [People and security](people-and-security.md)
