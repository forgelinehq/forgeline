# 036: AI use and budgets

Status: **Built (2026-11-08).** Task p4-06 of the redesign plan; the budget bar the Team frame (note 029, frame 5)
left out because nothing was measured.

## Question

An enterprise needs to know what its AI agents use and to cap it, so a looping agent cannot run up a bill. What can
Forgeline measure honestly, and where does a limit bite?

## Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| Where use is counted | In `askModel`, which every model call goes through (the assistant, agents, workflow steps, the canvas assistant) | One place, so nothing is missed |
| What is counted | Calls; input tokens (including cache reads and writes) and output tokens, as the backend reports them; the time the call took; the cost in US dollars when the `claude` program reports it (`total_cost_usd`). The Anthropic API reports tokens, not cost, so cost is shown only for the calls that had one ("$1.50 for 2 of 3 calls") | Nothing is estimated from a price list we would have to keep current |
| The claude program | Asked for `--output-format json`, which carries the reply, the tokens and the cost. Output that is not that JSON (an older program, a stand-in) is still taken as the reply; the call is counted with its tokens unknown, and the page says the totals are low by those calls. A result marked `is_error` is an error | Works with any version |
| Who it counts against | The running agent (its id, from the agent scope), or one total for people's own questions | Budgets are per agent |
| Period | Calendar month, UTC. Stored as one document per agent per month (`aiusage`, id `agent|YYYY-MM`) | Small, and a month is how bills work |
| Budgets | Per agent, set by administrators (`PUT /v1/ai/agents/{id}/budget`): tokens and/or minutes a month; zero for no limit. Kept on the agent, not in its versions. Audited | Tokens bound cost; minutes bound a backend that reports no tokens |
| Warn and stop | 80% of either limit warns (Team card, AI use page, audit `ai.agent.budget.warn`). At 100% the agent's next model call is refused before it is sent, with a message naming the agent and the limit; a workflow step fails with it. Crossing is audited (`ai.agent.budget.stop`). A call that starts under the limit finishes, so an agent can end a little over | The limit is checked before each call; we cannot know a call's size in advance |
| Where people see it | Settings › **AI use and budgets** (`#/ai-use`, developers read, administrators set budgets): per agent and for people, this month or one of the last six. Team cards show the budget bar, or the month's tokens when there is no budget; an agent that used its budget says **Stopped** | |
| Not counted | Cloud spend an agent causes (for example a change it proposed and someone approved). Forgeline cannot attribute AWS costs to an agent yet | Said on the page |

## Tested

Go: a call counts against the agent that made it (input with cache, output, time), people separately, an idle
agent has none; budgets are for administrators, range-checked, audited, kept across versions, shown on the team;
the call that crosses the limit finishes and the next is refused without reaching the model, with its message;
other agents are not affected; crossing 80% and 100% is audited; raising the budget lets it run; minutes count on
their own; zero clears it; the claude program's JSON (tokens with cache, cost), its errors and plain output. 10
rules removed one at a time, each fails a test. Node: the words (tokens, minutes, cost known, partly known and not,
the budget line, months). Browser with a stand-in claude: the Team card's bar and Stopped, the AI use page, setting
a budget.
