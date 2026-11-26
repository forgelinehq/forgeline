---
title: Approval rules
parent: Guides
nav_order: 7
---

# Approval rules

Every change to your cloud already needs an administrator who did not propose it. Approval rules let you ask for more
where it matters: two approvals, or only named administrators.

Use them for accounts or kinds of change where one reviewer is not enough, for example a production account, or
changes the assistant drafted.

## At a glance

| | |
| --- | --- |
| Where in the console | **Security → Approval rules**; waiting changes under **Approvals** |
| Applies to | Cloud changes, AI proposals, agent pushes to a registry, and runbook fixes |
| Needs | Nothing beyond the server |
| Design | [Note 032](../design/032-approval-rules.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Read the rules | Developer | So people know who will review their changes |
| Create, change or delete rules | Administrator | Audited as `approval.rules` |
| Approve a change | Administrator | Not the person who proposed it; the rules also apply |

## How it works

A rule matches a change by:

- **Account** (for example Primary). None ticked means every account.
- **Made by**: the assistant (AI-drafted changes), or people in the console. None ticked means both.
- **Only high-risk changes** (such as deleting a stack), if ticked.

and then asks for:

- **Needs two approvals**: the first approval is recorded and the change keeps waiting ("Approve (1 of 2)"); it
  runs only when a second, different administrator approves the same exact change.
- **Who may approve**: the administrators you tick; everyone else's approval is refused. None ticked means any
  administrator who did not propose the change.

When several rules match, the strictest wins: the most approvals any rule asks for, and only people allowed by every
rule that names approvers. With no rules, nothing changes. The Approvals page shows on each waiting change how many
approvals it needs, who already approved, and who may approve next.

## Add a rule

1. Open **Security → Approval rules** and choose **Add a rule**.
2. Name it, and choose one approval or two under **Needs**.
3. Tick the accounts, who made the change, and high-risk only, as needed.
4. Tick who may approve, or leave it empty for any administrator.
5. Choose **Save rules**.

![An approval rule asking for two approvals on the Primary account for the assistant's changes]({{ '/console/approval-rules.png' | relative_url }})

## Security model

- A person can never approve their own proposal, whatever the rules say.
- Rules only add requirements. They cannot let a non-administrator approve or skip a check the approval runs.
- A rule must name active administrators, and two approvals need at least two people who may approve.
- If the rules cannot be read, nothing is approved until they can.
- Every change to the rules is in the [audit log](audit-log.md).

## Limits and verification

{: .warning }
**Checked by tests only.** The rules are covered by tests, including two approvals, named approvers and the strictest
rule winning. Two real administrators approving one change in two browsers was not checked; the tests use two accounts.
Environment requests have their own self-service rules and are not covered by approval rules.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| An approval is refused, naming a rule | The rule says only certain people may approve, and you are not one of them. |
| An approval is refused with "you already approved this" | The change needs approvals from different administrators. |
| Nobody can approve a change | Two matching rules name approver lists that share nobody. The message says an administrator must fix the rules. |
| The rules page cannot be saved | A named approver is not an active administrator, or two approvals are asked with fewer than two possible approvers. |

## Related

- [People and roles](people-and-roles.md)
- [Policy packs](policy-packs.md)
- [Audit log and retention](audit-log.md)
- [Runbooks](runbooks.md)
- [API: approval rules](../api/reference.md)
