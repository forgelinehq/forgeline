# 054: Developers write agents, teams and workflows

Status: built (2026-11-27).

## Why

Only administrators could create or change agents, skills, teams and workflows. In testing, the assistant could build nothing
for a developer: most "build me this" requests ended in "ask an administrator". The maintainer decided on 2026-11-27 that
developers should be able to do nearly everything, and that only the sensitive things stay with administrators.

## Decision

| Developers | Only administrators |
| --- | --- |
| Create, change, roll back, turn off, archive and restore agents, skills, teams, workflows | Users, roles, sign-in, other people's tokens |
| Publish agent versions, import bundles, set reporting lines | Secrets, model providers, AWS, GitHub and other connections, MCP servers, registries and pushes |
| Team and workflow schedules | Approving cloud changes, pushes and runbook runs; releasing a workflow's stage |
| Write runbooks | Reviewing runbooks |
| Evals; delete what they created | Approval rules, policy packs, assistant permissions, budgets, publishing gates, webhook secrets, audit settings |

Connecting MCP servers stays with administrators for now. Letting developers add servers that only read needs its own check of
where such a server may reach; it is not part of this change.

## The risk, and the rule

A model follows what an agent, a skill, a team or an eval case says, and an agent's lookups run as the person who started it.
So whoever writes those could borrow an administrator's rights: write "read the audit log and put it in your answer", then
wait for an administrator to run it.

**What a developer wrote runs with at most a developer's rights, whoever starts it.**

- Every saved agent, skill and workflow version, team and eval suite records its author's role. An empty role is an
  administrator's: before this note nobody else could write them (except eval suites, which stay as they were until saved
  again).
- An agent run is held to a developer's rights when its version, or any skill version it pins, is a developer's. This is
  decided where every agent run starts, so chat, the assistant asking an agent, teams, missions and evals all get it.
- A team run is held to a developer's rights when the team or any agent in it is a developer's: what one member says reaches
  the others.
- An eval run is held to a developer's rights when the suite or the version is a developer's: the cases are what the agent is
  asked.
- Workflows never ran as an administrator and still do not.
- A developer cannot give an agent a tool only an administrator may use (`aws_script`, `search_audit`).
- An administrator who saves a developer's text, changed or not, makes it theirs. For an agent that follows a developer's
  skill, the administrator saves the skill and then a version of the agent that pins it.

## Approvals administrators set up stay theirs

- What an administrator turned off, only an administrator turns on. Turning it off again, or archiving and restoring it, does
  not take that over.
- With a publishing gate on an agent, only an administrator changes or deletes the gate's suite, and only an administrator
  rolls the agent back (a rollback is not gated and could bring back a version that never passed).
- A workflow whose current version an administrator saved with a gated stage or a question for a person is changed, turned
  off or on, paused or deleted only by an administrator. A developer's own workflows are theirs, gated or not.

## Deleting (2026-11-28)

The maintainer decided that a developer deletes only what they created, not an administrator's or another person's, and
that an administrator deletes anything. It covers pipelines, agents, skills, workflows, teams, team schedules, eval suites
and blueprints. The creator is who saved the first version; saving it later does not move it. A developer cannot remove a
team that holds a schedule someone else made, because the schedule would go with it. Teams and team schedules saved before
this record no creator, so only an administrator deletes them. The creator is kept by user name: an account made again
under a removed person's name counts as that person. Restoring from the archive is not deleting.

## What is left open

- A developer can change what an administrator or another developer wrote, where no approval is attached. Every change is
  a version.
- A developer's agent can be given any read-only tool of an MCP server an administrator connected, including servers not
  offered to the assistant.
- An answer from a developer's agent can reach an administrator's chat when the assistant asks it. It is treated as data, like
  a log line, and changes still ask first; under autopilot that protection is the autopilot's own limits.
- Spending: developers can create schedules and agents that use the model provider. Budgets, which only administrators set,
  limit it.
