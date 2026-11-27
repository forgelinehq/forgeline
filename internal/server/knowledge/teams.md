---
title: Teams
parent: Guides
nav_order: 40
---

# Teams

A team is a **lead** and up to eight **members**, all [agents](agents.md). You give the team a goal in your own words; the
lead splits the work, the members look into it at the same time, and the lead writes one answer.

Use a team for a question that needs several viewpoints: "why did the last deploys fail, and is anything wrong
underneath?"

## At a glance

| | |
| --- | --- |
| Where in the console | **Agents → Teams** |
| Needs | At least two agents (a lead and one member) |
| Design note | [045](../design/045-agent-studio.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See teams and their runs | Developer | |
| Give a team a goal | Developer | 2000 characters at most; one run at a time per team |
| Create, change or remove a team | Administrator | A team has 1 to 8 members besides the lead; at most 50 teams; removal is refused while it is working |
| Schedule a goal | Administrator | See [Schedules](schedules.md) |

## How it works

1. **The lead plans.** It gives each member who can help one task, and says in a sentence how it split the work. It may
   only hand work to its own members, once each; anything else in its plan is dropped.
2. **The members work at the same time** (three at once), each with its own model, skills, tools and monthly budget, and
   as the person who gave the goal, so no agent can do more than you could.
3. **The lead writes the answer** from what the members found. Their answers reach it as data, never as instructions, so a
   member that read something hostile in a log cannot steer the lead.

The run's page shows the plan, a card per member with its task, what it looked up and what it found, and the team's
answer, live. A change a member proposes goes to **Approvals** like any other.

## Make a team

1. As an administrator, choose **New team** (developers do not see the button).
2. In the side panel give it a name, a purpose (the lead reads it), the lead, and the members, picked from a searchable
   list of agents. An agent can be in several teams.
3. Choose **Make the team**, open it, type a goal and choose **Give the goal**. (On the **Teams** page, **Give a goal** on a
   team's card opens the same page.)

The library's **Incident triage** and **Platform review** packages install as teams. Below the teams, every agent is listed
with what it is doing now (working, waiting for your OK, asleep until its next run, waiting to be asked, or off), which
teams it is in, and what it used this month.

## Security model

- Members work as the person who gave the goal.
- Member answers reach the lead as data, never instructions.
- A lead can only hand work to its own members.
- Every agent call is audited and appears in the model call log as *agent in team*.
- A change a member proposes needs approval like any other.

## Limits and verification

A goal is refused before anything is spent when the lead or a member is turned off. A team keeps its last 50 runs. Removing a
team keeps its agents and past runs. Past runs are kept under the team's id, so a new team made with the same name shows
them again.

{: .warning }
**Checked through the console on 2026-11-27**, on the UAT instance at commit `487f373`, with the real model
(`claude-haiku-5-5`): an administrator made a team of a lead and two members in the side panel, a developer gave it a goal
in the page, and the run showed the plan, a card per member and the answer. The audit log showed one `ai.team.step` per
agent call as the developer, and the model call log showed `<agent> in <team>`. A developer got 403 on creating a team or
adding a schedule, a viewer got 403 on giving a goal, and a goal of 2001 characters was refused with the message below. Not
run: a second goal while the team was working, removing a team while it was working, and a lead that names an agent outside
its team (it needs a hostile model). See the [test note](../testing/2026-11-27-uat-workflows-teams.md).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| The goal is refused at once with 409 | An agent is turned off ("<agent> is turned off; turn it on or change the team"), no longer exists, or has no current version. |
| "<team> is already working on a goal" | A team runs one goal at a time. |
| "goal: say what the team should do, in at most 2000 characters" | The goal is empty or too long. |
| A member did no work | The lead picked only some members, or the plan named an agent that is not a member and was dropped. |
| An agent stopped mid-run | It used its monthly [budget](ai-use-and-budgets.md). |

## Related

- [Schedules](schedules.md)
- [Agents](agents.md)
- [Missions](missions.md)
- [Workflows](workflows.md)
