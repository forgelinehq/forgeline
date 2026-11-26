---
title: Home and approvals
parent: Guides
nav_order: 12
---

# Home and approvals

**Home** is the first page after sign-in. It shows what needs you: changes waiting for your approval, failed pipeline
runs, open security findings and connections that need attention. **Approvals** lists every change waiting for a
decision, from AI proposals to cloud changes, so one person can read each change and decide.

Use Home to start the day, and Approvals whenever you are asked to review something. Nothing a person, an agent or a
workflow proposes changes your cloud until a different administrator approves it here.

## At a glance

| | |
| --- | --- |
| Where in the console | **Home → Today** and **Home → Approvals**; the **Approvals** button at the top right of every page |
| Needs | Nothing for Home. Cloud tiles fill in once AWS is connected |
| Design note | [Note 029](../design/029-console-spaces-and-home.md), [note 032](../design/032-approval-rules.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See Home and the Approvals list | Viewer | |
| Ask or tell Forgeline (the assistant box) | Developer | |
| Skip a setup step | Administrator | |
| Propose a change | Developer | You can cancel your own proposal while it waits |
| Approve or reject an AI proposal or a cloud change | Administrator | Never the person who proposed it; the [approval rules](approval-rules.md) can ask for two approvals or name who may approve |

## How it works

1. A person, the assistant, a mission or a workflow files a change: an AI proposal (the exact AWS calls an agent
   drafted) or a cloud change (a stack, a blueprint, an environment).
2. The change appears under **Approvals** with who asked, why, the preview, and how many approvals the approval rules
   need. A change filed by a mission says which one.
3. A different administrator reads the exact change and approves or rejects it. Approving runs exactly what was
   previewed.
4. The result moves to **Recent changes** on Home and is recorded in the [audit log](audit-log.md).

## Read Home

Top to bottom:

- **Set up Forgeline** appears until a new server is set up. It lists the first-run steps (your team, GitHub, a cloud
  account, the assistant, what the AI may touch, a first pipeline or blueprint), each with a button to the page that
  does it. A step ticks itself when it is done; an administrator can skip one. **Settings › Setup** shows the list at
  any time.
- **Ask or tell Forgeline** opens the assistant with your question (developers and administrators).
- A greeting says how much needs you, and four tiles count your pipelines, your AWS **spend**, your **AI team** and
  **security** findings. Spend loads only when you press **Load from AWS**, because AWS bills each look.
- **Needs you** lists what is waiting for you, most urgent first (see the table below).
- **Start something** offers next steps: build and test a GitHub repository with the assistant, write a pipeline, hire
  an agent.
- **Recent changes** lists the last changes approved, rejected or applied.
- A strip at the bottom shows the Docker host, the server and the disk.

![Home with the greeting, tiles, Needs you and Recent changes]({{ '/console/home-today.png' | relative_url }})

| Needs you shows | When |
| --- | --- |
| Changes you can approve | Someone else proposed them (a different administrator must approve) |
| Pipelines that failed | Their last run failed |
| Security findings | Critical and high findings that are open |
| Connections | A connection needs attention |

## Review and decide

1. Open **Home → Approvals**, or the **Approvals** button at the top right.
2. Open a change. Read the exact text or preview, the risk, the reason and the number of approvals it needs.
3. Choose **Approve** or **Reject**, with a note if you want. If the rules ask for two approvals, the first is recorded
   ("Approve (1 of 2)") and the change keeps waiting for a second, different administrator.

![The Approvals list]({{ '/console/home-approvals.png' | relative_url }})

Approval pages for other kinds of change have their own guides: a [runbook](runbooks.md) fix, and an agent push to a
registry under **Home › Approvals › Agent pushes** (see [Assistant](assistant.md)).

## Security model

- **A different person.** An administrator never sees an Approve button for their own proposal, and the server refuses
  it too. Removing that rule made tests fail (design note 029, "What was checked").
- **Exact text.** An approval is tied to what you read. For AI proposals the approve call carries the hash of the text.
- **Rules can tighten it.** [Approval rules](approval-rules.md) add a second approval or restrict
  who may approve, and the strictest matching rule wins.
- **Audited.** Approvals, rejections and cancellations are in the audit log.

## Limits and verification

Who is asked to approve, which runs, findings and connections are listed, and their order were tested under Node;
removing the "not your own proposal" rule and the "administrators only" rule each made the tests fail. The page was
checked in a browser on demo data. A screen reader on the Needs you list was not checked
([note 029](../design/029-console-spaces-and-home.md)).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| A change you proposed shows no Approve button | A different administrator must approve it. |
| A change is not in **Needs you** | You proposed it, or you are not an administrator. |
| **Spend** shows a dash | It loads only when you press **Load from AWS**. |
| Security shows "Guard has not scanned yet" | No findings scan has run. |
| An approval is refused | You proposed the change, or the rules do not name you as an approver. |

## Related

- [Console basics](console-basics.md)
- [Approval rules](approval-rules.md)
- [Cloud canvas](cloud-canvas.md)
- [Runbooks](runbooks.md)
- [Audit log](audit-log.md)
