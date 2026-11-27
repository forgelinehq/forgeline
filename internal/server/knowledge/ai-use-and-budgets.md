---
title: AI use and budgets
parent: Guides
nav_order: 47
---

# AI use and budgets

**Usage and budgets** shows what each agent used this month (tokens, model time and calls, and the cost where the `claude`
program reports it) and what people used asking the assistant themselves. An administrator can give an agent a monthly
budget so a looping agent cannot run up a bill.

Use it to see where model use goes and to cap it.

## At a glance

| | |
| --- | --- |
| Where in the console | **Agents → Usage and budgets** (a month picker shows this month and the five before it) |
| Needs | Nothing beyond agents that have run |
| Design note | [036](../design/036-ai-use-and-budgets.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See usage | Developer | This month or one of the five before it |
| Set or clear a budget | Administrator | Tokens (0 to 1,000,000,000), minutes (0 to 100,000), or both; 0 for none |

## How it works

1. Every model call is counted where it is made: the assistant, agents, workflow steps and the canvas assistant. The count
   is calls, input and output tokens (including cache), the time the call took, and the cost in US dollars when the
   `claude` program reports it. A call to a [provider's model](models-and-providers.md) is priced from the prices set on
   that provider (dollars per million tokens), so it has a cost. A call whose cost nobody reports has none, so the page
   shows "$1.50 for 2 of 3 calls". A cost under one cent shows with four decimals (for example `$0.0013`); the call log on Models and providers
   shows each call's cost.
2. Use counts against the running agent, or one total for people's own questions. The period is the calendar month, in UTC.
3. At **80%** of either limit the agent's card and the page warn (and the crossing is audited).
4. At 100% the agent's next model call is refused before it is sent. Its row on this page says "stopped until next month or a
   higher budget", its card on **Agents → Teams** says **Stopped: its monthly budget is used up**, and a workflow step it was
   running fails with a message saying why. A call that starts under the limit finishes, so an agent can end a little over.
5. Raise the budget, or wait for the next month (UTC), to let it run again. A mission also has its own token budget; see [Missions](missions.md).

![AI use and budgets]({{ '/console/ai-use-and-budgets.png' | relative_url }})

## Set a budget

1. On **Usage and budgets** choose **Set budget** next to an agent.
2. Enter tokens, minutes, or both (limits: "tokens: 0 (no limit) to 1000000000", "minutes: 0 (no limit) to 100000"). Minutes bound a backend that reports no tokens. Both 0 clears the budget.
3. Save. The budget is kept on the agent, not in its versions.

## Security model

- The limit is checked before each call, so a stopped agent cannot spend more.
- Every budget change and every crossing is in the audit log (`ai.agent.budget.warn`, `ai.agent.budget.stop`).

## Limits and verification

{: .warning }
**Run on the UAT instance, in part.** On 2026-11-27 (main at `487f373`) on the UAT instance a one-token budget on a test agent refused the next call with the message quoted below, and the usage page showed the agent as stopped. The 80% warning and a stopped workflow step were not run. See the [note 2026-11-27](../testing/2026-11-27-uat-assistant-agents.md).

Cloud spend an agent causes (for example a change it proposed and someone approved) is **not counted** here yet: Forgeline
cannot attribute AWS costs to an agent. Output that is not the `claude` program's JSON (an older program, a stand-in) is
counted with its tokens unknown, and the page says the totals are low by those calls.

Design note 036 records the tests: ten rules were removed one at a time and each failed a test, and the page was checked in a
browser with a stand-in `claude`.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| An agent says **Stopped** | It used its monthly budget; raise it or wait for the next month (UTC). The error reads `over budget: <agent> has used its budget for YYYY-MM (<used> of <limit> tokens); an administrator can raise it under Agents → Usage and budgets`. |
| A workflow step failed with a budget message | The agent it runs hit its budget. |
| `POST /v1/ai/agents/{id}/chat` answers `429` with `over budget` | The agent used its monthly budget (a failure of the model behind it is `502`). |
| Cost is missing for some calls | Only calls where the `claude` program reports cost have one. |
| Totals look low | Some calls reported no tokens (shown as such on the page). |
| I cannot set a budget | Only administrators can. |

## Related

- [Agents](agents.md)
- [Missions](missions.md) (each mission also has its own token budget)
- [Assistant](assistant.md)
