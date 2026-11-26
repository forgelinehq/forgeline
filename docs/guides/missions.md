---
title: Missions
parent: Guides
nav_order: 44
---

# Missions

A mission gives one agent a goal in your own words, for example "check the payments service for anything fragile and fix
what you can", and one AWS account to look at. It goes through five stages, drawn live on its page, and a person approves
every change.

Use a mission when you want an agent to investigate an account, propose fixes, and then check that the fixes worked, with
a record of what it actually looked at.

## At a glance

| | |
| --- | --- |
| Where in the console | **Operations → Missions** |
| Needs | An [agent](agents.md) and an [AWS connection](connect-aws.md) |
| Design notes | [014](../design/014-agentic-missions.md), [037](../design/037-missions.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Start a mission | Developer | At most 5 running at once; the agent runs with your role |
| See missions | Developer | |
| Cancel a mission | Developer | Only the one who started it, or an administrator |
| Approve or reject a proposal it filed | Administrator | A different administrator from the proposer; the approver sees which mission filed it |

## How it works

| Stage | What happens |
| --- | --- |
| **1. Discover** | The agent only reads |
| **2. Propose** | An agent allowed to propose files change proposals for what it can fix; one that only advises recommends |
| **3. Your OK** | A different administrator approves or rejects each proposal |
| **4. Verify** | If anything was applied, the agent looks again and says whether each finding is fixed |
| **5. Report** | Written by Forgeline from the record, not by the model |

Two rules keep a mission honest:

1. Every finding must name the lookup that showed it. The server keeps only findings whose lookup the agent really ran; the
   rest are left out and counted on the page.
2. The agent may only look at the account you chose. A lookup for any other account is refused by the server.

![A finished mission]({{ '/console/missions-done.png' | relative_url }})

## Start a mission

1. Open **Operations → Missions** and start one.
2. Write the goal (10 to 2000 characters), pick the agent and the AWS account.
3. Optionally set a token budget. The default is 200,000 and the most is 5,000,000.
4. Watch the stages. A mission waiting for approval shows its proposals; a different administrator decides them under
   **Home → Approvals**.

A mission stops before its next model call once its budget is used. The agent's own monthly
[budget](ai-use-and-budgets.md) also applies.

## Security model

- Reads are scoped to one account, checked on the server, not in the prompt.
- The model never applies a change: proposals go through the normal approval path with a different administrator.
- Findings without a lookup that ran are dropped and counted.
- The report is written by Forgeline, not the model.
- Start, each stage and cancel are audited.

## Limits and verification

A mission shows at most 20 findings. It waits at most 7 days for its proposals, then reports with what was decided. A
mission that was running a stage when the server stopped is marked failed ("interrupted by a restart"); one waiting for
approval keeps waiting.

{: .warning }
**Checked with stand-ins.** The mission runner was tested with a scripted model and the AWS stand-ins, and in a browser
with a scripted `claude` and `fake-aws` ([note 037](../design/037-missions.md)). This page does not claim a real-model
run.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| A lookup is refused | It names an account other than the mission's. |
| A finding is counted as left out | It named no lookup the agent ran. |
| The mission stopped | It used its token budget, or the agent used its monthly budget. |
| The mission waits | A proposal is waiting for a different administrator. |
| The mission failed after a restart | It was running a stage when the server stopped. |
| Cannot start another | 5 missions are already running. |

## Related

- [Agents](agents.md)
- [Teams](teams.md)
- [AI use and budgets](ai-use-and-budgets.md)
- [People and security](people-and-security.md)
