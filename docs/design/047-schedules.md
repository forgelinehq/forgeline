# 047: Schedules for workflows and teams

Status: **built 2026-11-19**, on the maintainer's request ("I also strongly want to add scheduler options to workflow
or team executions"). The decisions below follow the rules unattended workflows already had (note 024); say if any
should change.

## Question

A workflow could run every 15 minutes to every week, from a short list, in its settings drawer. People asked for the
usual kinds of time ("weekdays at nine", "the first of the month"), in their own time zone, for teams as well as
workflows, and for one place to see and pause everything that runs by itself.

## What is built

- **One schedule shape** (`internal/schedule`): every N minutes (15 to 10080), or a five-field cron expression
  (minute hour day-of-month month weekday; lists, ranges, steps, names) in an IANA time zone. The standard library
  only: a small parser instead of a dependency. The zones database is compiled in (`time/tzdata`), so a container
  without `/usr/share/zoneinfo` still knows Europe/Berlin. Times follow the zone's daylight saving.
- **At most every 15 minutes**, as before. For cron this is exact: the shortest gap between the minutes it runs at,
  around the hour, must be at least 15.
- **Missed times are not run late.** A time is run if the server sees it within an hour (it was down, or the tick was
  late); older ones are skipped. Resuming, or changing the time, counts from then. A run never queues behind another:
  a workflow or team still working skips that time, and a team schedule says so ("Skipped: … is already working").
- **Workflows** keep their schedule in the version (`every_minutes`, or `cron` and `tz`), so it is in the history
  and the hash. Pausing does not need a new version: it is kept beside the workflow and audited.
- **Teams** get schedules of their own: a goal, a time, and the role the run works with. Up to 10 per team.
- **Run as viewer or developer, never administrator.** Nobody watches a scheduled run, as with workflows: a viewer's
  run only reads; a developer's may propose changes, which a different administrator approves. The run records the
  role (`run_as`) and who started it (`schedule:<id>`). **Run now** in the list runs as the person who presses it.
- **Who may do what**: developers see the list and preview times; administrators create, change, pause and delete.
  Every change is in the audit log (`ai.schedule.*`), and each scheduled run as before (`ai.workflow.run`,
  `ai.team.run` "by schedule").
- **The console**: one schedule picker everywhere (presets "every few hours", "every day at", "weekdays at", "every
  week on", "every month on day", or a custom cron), with the zone and the next three runs in the viewer's own time,
  worked out by the server so what is saved is what was shown. **Team › Schedules** lists every schedule, soonest
  first, with pause, resume, change and run now. A team's page has "On a schedule".

## Not done

- Calendars (skip holidays), "the last day of the month", and running more often than every 15 minutes.
- A schedule that hands the run some input, other than a team's fixed goal.
- Notifications when a scheduled run fails; the run list and the audit log show it.
