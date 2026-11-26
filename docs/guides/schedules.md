---
title: Schedules
parent: Guides
nav_order: 41
---

# Schedules

**Schedules** lists everything that runs by itself: workflows on their schedule and goals given to teams at set times,
soonest first. It shows the next run in your time, how the last one went, and who it runs as.

Use it to see what will run when, pause something, or run it now.

## At a glance

| | |
| --- | --- |
| Where in the console | **Workflows → Schedules**; a team's page under **On a schedule**; a workflow's own schedule is set in its canvas **Settings → Trigger** |
| Needs | A workflow with a schedule, or a team |
| Design note | [047](../design/047-schedules.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See schedules | Developer | |
| Preview the next times | Developer | Saves nothing |
| Pause, Resume, Change, Run now | Administrator | The buttons are shown to administrators; **Run now** runs as you |
| Add a schedule to a team | Administrator | At most 10 per team; runs as a viewer or a developer, never an administrator |

## How it works

A schedule is one of:

- every few minutes or hours (15 minutes to 10,080, one week; nothing runs more often than every 15 minutes),
- every day, weekdays, a day of the week or of the month at a time,
- a cron expression,

in a time zone you pick (empty is UTC). The picker shows the next three runs before you save.

![Workflows, Schedules]({{ '/console/schedules.png' | relative_url }})

Rules for missed and overlapping times:

| Case | What happens |
| --- | --- |
| A time missed by less than an hour (the server was down or busy) | Runs once, late |
| A time missed by more than an hour | Skipped, not run late |
| A workflow or team still working at the next time | That time is skipped |
| A paused workflow schedule | The workflow still runs by hand, from its webhook and after failed runs |
| A resumed cron schedule | Waits for its next time instead of running the missed ones |

## Schedule a team

1. Open the team, and under **On a schedule** add a goal.
2. Pick the time, the time zone and the role it runs as (viewer or developer).
3. Save.

## Security model

- A team's schedule never runs as an administrator.
- **Run now** runs as you.
- Pause, resume, change, create and delete are audited (for example `ai.schedule.pause`).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| Nothing ran at the set time | The server was down for more than an hour, or the workflow or team was still working. |
| A schedule shows why it is **off** | The workflow is turned off ("the workflow is turned off"), or no model is set up ("no model is set up, so nothing runs by itself"). |
| I cannot pause or change one | Only administrators see the buttons. |
| A team gets "a team has at most 10 schedules" | The limit is 10 schedules a team. |
| "this workflow has no schedule; set one in its settings" | Pause or resume was used on a workflow that has no schedule. |

## Related

- [Workflows](workflows.md)
- [Teams](teams.md)
- [Alarm intake](alarm-intake.md)
