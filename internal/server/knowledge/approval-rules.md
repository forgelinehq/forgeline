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
| Where in the console | **Security → Approval rules**; waiting changes under **Home → Approvals** |
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

- **For changes to** an account (for example Primary). None ticked means every account. A rule that names an account
  never matches an agent push to a registry, because a push is not made in an AWS account.
- **Made by**: **The assistant (AI-drafted changes)**, **People in the console**, **Pushing an agent to a registry** or
  **Runbook fixes on servers**. None ticked means all four.
- **Only high-risk changes** (such as deleting a stack), if ticked.

and then asks for:

- **Needs**: **One approval** or **Two approvals**. With two, the first approval is recorded and the change keeps waiting ("Approve (1 of 2)"); it
  runs only when a second, different administrator approves the same exact change.
- **Who may approve**: the administrators you tick; everyone else's approval is refused. None ticked means any
  administrator who did not propose the change.

Worked example: a rule "Production needs two" with account Production, **Needs** two approvals, and `ada` and `dana`
ticked under **Who may approve**. A change to Production then waits for both `ada` and `dana`; if `ada` proposed it, only
`dana` is left and the rule cannot be met, so someone must change the rule.

You can keep at most 20 rules. When several rules match, the strictest wins: the most approvals any rule asks for, and only people allowed by every
rule that names approvers. With no rules, nothing changes. The Approvals page shows on each waiting change how many
approvals it needs, who already approved, and who may approve next.

## Add a rule

1. Open **Security → Approval rules** and choose **Add a rule**.
2. Name it, and choose one approval or two under **Needs**.
3. Tick the accounts, **Made by**, and **Only changes rated high risk (for example deleting a stack)**, as needed.
4. Tick who may approve, or leave it empty for any administrator.
5. Choose **Save rules**.

![An approval rule asking for two approvals on the Primary account for the assistant's changes]({{ '/console/approval-rules.png' | relative_url }})

## Security model

- A person can never approve their own proposal, whatever the rules say.
- Rules only add requirements. They cannot let a non-administrator approve or skip a check the approval runs.
- A rule must name active administrators (`approvers[0]: "x" is not an active administrator`), and two approvals need at
  least two people who may approve (`two approvals need at least two people who may approve`).
- If the rules cannot be read, nothing is approved until they can.
- Every change to the rules is in the [audit log](audit-log.md).

## Limits and verification

{: .warning }
**Checked by tests only.** On 2026-11-27 (commit `487f373`) the page and the API were read as a developer on the UAT instance, which has no
rules: the empty page, the refusal of a write and a one-approval change were seen, but no rule was added or met
([note](../testing/2026-11-27-uat-cloud-security.md)). The rules are covered by tests, including two approvals, named approvers and the strictest
rule winning. Two real administrators approving one change in two browsers was not checked; the tests use two accounts.
An environment that starts under the [self-service policy](blueprints-and-environments.md#environments) is approved by the policy (`policy:self-service`), not by a person, and approval rules do not count it. Environment requests outside the policy are ordinary cloud changes and follow the rules.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| `the approval rule (Production needs two) says only ada, dana may approve this change` | The rule names approvers and you are not one of them. |
| `you already approved this; it needs 2 approvals from different administrators` | The change needs approvals from different administrators. |
| `the approval rules (...) leave nobody who may approve this change; an administrator must fix the rules` | Two matching rules name approver lists that share nobody. |
| `the approval rules could not be read, so nothing is approved until they can` | The stored rules are unreadable; fix or remove them. |
| The rules page cannot be saved | A named approver is not an active administrator, two approvals are asked with fewer than two possible approvers, a rule has no name (up to 60 characters), or there are more than 20 rules. |

## Related

- [People and roles](people-and-roles.md)
- [Policy packs](policy-packs.md)
- [Audit log and retention](audit-log.md)
- [Runbooks](runbooks.md)
- [API: approval rules](../api/reference.md)
