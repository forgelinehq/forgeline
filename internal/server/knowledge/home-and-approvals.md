---
title: Home and approvals
parent: Guides
nav_order: 12
---

# Home and approvals

**Home** is the first page after sign-in. It shows what needs you: changes waiting for your approval, failed pipeline
runs, serious security findings and connections that need attention, with your pipelines below. **Approvals** lists the
changes the assistant and agents drafted, plus runbook fixes and agent pushes, so one person can read each change and
decide. Cloud changes made from the console are approved under **Operations → Infrastructure → Cloud changes**.

Use Home to start the day, and Approvals whenever you are asked to review something. Nothing a person, an agent or a
workflow proposes changes your cloud until a different administrator approves it.

## At a glance

| | |
| --- | --- |
| Where in the console | **Home → Today** and **Home → Approvals**; the **Approvals** button at the top right of every page |
| Needs | Nothing for Home. The **Spend** tile needs AWS connected |
| Design note | [Note 029](../design/029-console-spaces-and-home.md), [note 032](../design/032-approval-rules.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See Home, the Approvals list and a proposal | Viewer | A viewer sees no **Ask** box, Spend or AI team tile, and no agent pushes or runbook fixes |
| Use the **Ask or tell Forgeline** box, see the setup card | Developer | |
| Skip a setup step | Administrator | |
| Propose a change (a cloud change, or through the assistant) | Developer | |
| Withdraw a proposal | Developer | Only the person who proposed it, or an administrator |
| Approve or reject a proposal or cloud change | Administrator | Never the person who proposed it; the [approval rules](approval-rules.md) can ask for more approvals or name who may approve |

## How it works

1. A person, the assistant, a mission or a workflow files a change: an AI proposal (the exact AWS calls an agent
   drafted) or a cloud change (a stack, a blueprint, an environment).
2. An AI proposal appears under **Approvals** with who proposed it, the reason, the calls it will make, the exact
   script and its hash. One drafted by the assistant says so; one from a mission says "from a mission". A cloud change
   appears under **Infrastructure → Cloud changes**. Both show how many approvals the approval rules need.
3. A different administrator reads the exact change and approves or rejects it. Approving runs what was shown.
4. Recently decided changes move to **Recent changes** on Home, and are recorded in the [audit log](audit-log.md).
5. A proposal nobody decides within 24 hours shows as expired and cannot be approved.

## Read Home

Top to bottom:

- **Ask or tell Forgeline** (developers and up) opens the assistant with your question.
- **Set up Forgeline** appears until every first-run step is done or skipped (developers and up). The steps are: create
  the first administrator, bring in your team, connect GitHub, connect a cloud account, turn on the assistant, choose
  what the AI may touch, and make your first pipeline or blueprint. Each has a button to the page that does it, and a
  step ticks itself when it is done. An administrator can **Skip** one. **Settings → Setup** shows the list at any time.
- A greeting says how many things need you, and tiles count your pipelines (**Apps and pipelines**), your AWS **Spend**,
  **Your AI team** and **Security** findings. Spend loads only when you press **Load from AWS**, because AWS bills each
  look at Cost Explorer.
- **Needs you** lists what is waiting, most urgent first, six at most, then "and N more: see Approvals and Guard".
- **Start something** offers next steps: build and test a GitHub repository with the assistant, write a pipeline, hire an
  agent from the library. A viewer sees a note to ask an administrator for the developer role.
- **Recent changes** lists the last four proposals and cloud changes that were approved, rejected, applied, failed,
  canceled or expired.
- A strip shows the Docker host, the server and the disk. The **Pipelines** table and **Recent runs** follow; see
  [Pipelines](pipelines.md).

![Home with the greeting, tiles, Needs you and Recent changes]({{ '/console/home-today.png' | relative_url }})

| Needs you shows | When |
| --- | --- |
| **Approve: ...** (a **Review** button) | An administrator looks at a change someone else proposed |
| **Waiting for approval: ...** | A change you proposed, or you are not an administrator |
| **... failed** | A pipeline's latest run failed |
| A finding | A critical or high finding is open |
| **... needs attention** | A connection needs attention (developers and up) |

## Review and decide

1. Open **Home → Approvals**, or the **Approvals** button at the top right.
2. Open a change. Read the reason, the calls, the exact script and its hash, and the number of approvals it needs.
3. Add a note if you want, then choose **Approve and apply** or **Reject**. A confirmation dialog repeats what will run.
   If the rules ask for two approvals, the first button reads "Approve (1 of 2)": your approval is recorded and the
   change keeps waiting for a second, different administrator.
4. To take back your own proposal, choose **Withdraw this proposal**.

![The Approvals list]({{ '/console/home-approvals.png' | relative_url }})

Below the list, **Runbook fixes** and **Agent pushes** appear when there are any. They have their own pages: a
[runbook](runbooks.md) fix, and an agent push to a registry (see [Assistant](assistant.md)).

## Security model

- **A different person.** The Approve button is disabled for your own proposal, with "You proposed this, so someone
  else must approve it", and the server refuses it too. The same goes for approving twice yourself.
- **Exact text.** An AI proposal's approve call carries the hash of the script you read.
- **Rules can tighten it.** [Approval rules](approval-rules.md) add a second approval or restrict who may approve, and
  the strictest matching rule wins.
- **Audited.** Approvals, rejections and cancellations are in the audit log.

## Limits and verification

Who is asked to approve, which runs, findings and connections are listed, and their order were tested under Node;
removing the "not your own proposal" rule and the "administrators only" rule each made the tests fail
([note 029](../design/029-console-spaces-and-home.md), "What was checked").

{: .warning }
**Not checked.** A screen reader on the Needs you list. The Home picture predates the rename of "marketplace" to
"library" in **Start something**. This page was compared with `overview.js`, `inbox.js`, `proposals.js` and the route
table; it was not run again in a browser.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| A change you proposed shows no active Approve button | A different administrator must approve it. |
| A change you proposed is under **Waiting for approval** | Expected: only others' changes show **Approve** in Needs you. |
| **Spend** shows a dash | It loads only when you press **Load from AWS**. |
| Security shows "Guard has not scanned yet" | No findings scan has finished. |
| "Only ... may approve this change." | An [approval rule](approval-rules.md) names who may approve. |
| "You approved this. It needs another administrator." | The rules need two approvals and yours is already recorded. |
| A proposal shows as expired | Nobody decided it within 24 hours. Ask for it again. |
| A cloud change is not on **Approvals** | It is under **Infrastructure → Cloud changes**. |

## Related

- [Console basics](console-basics.md)
- [Approval rules](approval-rules.md)
- [Cloud canvas](cloud-canvas.md)
- [Runbooks](runbooks.md)
- [Audit log](audit-log.md)
