---
title: Agent graphs checked, 2026-11-17
---

# Agent graphs with the real model, 2026-11-17

The real check of agent graphs (#151, [note 044](../design/044-agent-graphs.md) parts 1 and 2) on the maintainer's
Raspberry Pi Forgeline, after the updater deployed it, with the Pi's own Claude connection. The run was started by
`claude-agent` (a developer); the agents and the workflow were made, and the question answered, with the server token,
as the maintainer allows for Claude's own tests.

## The graph

Two read-only T0 agents (a triager and an explainer), and a graph of 12 nodes:

1. **start**, then two **lookups** at once: `list_runs` (the last 10 runs) and `morning_check`;
2. a **merge** of the two;
3. the **triage agent**, reading both as data and asked to write `verdict` and `failed`;
4. a **rule decision** on `verdict`: `fine` to an end, `else` to a **question** (`dig` or `stop`);
5. after `dig`, an **agent decision** (the explainer chooses `explain` or `else`), the **explainer agent**, and an end.

## What happened

- **First try:** the `list_runs` lookup failed at once, with "the arguments are not valid JSON for this tool". I had
  given `limit` as text. That is a usability gap, fixed in the same pull request as this page: the error now says what
  the tool takes, and the canvas shows it beside the arguments.
- **Second run, 8 seconds:** both lookups and the merge ran. Claude answered and wrote `verdict: healthy` and
  `failed: 0`, nothing else. The rule took `fine` and the run ended as done in 7 steps.
- **Third run,** with the rule changed so it never matches:
  - the run stopped at the question after 7 steps, waiting;
  - an answer from `claude-agent` (a developer) was refused with 403;
  - the administrator answered `dig`;
  - Claude, as the explainer, chose `explain` on its own;
  - the explainer wrote three sentences and the run ended as done in 10 steps.
  - The audit log has `ai.workflow.run`, `ai.workflow.step` for every agent and `ai.workflow.answer`.
- The test agents and workflows were turned off afterwards.

## Not checked

- The canvas run view against these real runs in a browser on the Pi (checked locally on a saved graph).
- A graph whose agent files a proposal (a T1 agent); the path is the same as for stage workflows.
- A scheduled graph run.
