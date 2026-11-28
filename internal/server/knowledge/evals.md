---
title: Evals
parent: Guides
nav_order: 45
---

# Evals

An eval suite tests an agent before people rely on it. A suite is a list of cases for one agent: what it is asked, what a
tool returns in that case, and checks on the answer and on what the agent did. Nothing an eval runs changes anything.

Use evals to check that an agent still behaves after you change its instructions, tools or model, and to test that it does
not obey instructions planted in a log or an issue. A suite can also gate publishing a new version (see
[Publishing agent versions](publishing-agent-versions.md)).

## At a glance

| | |
| --- | --- |
| Where in the console | **Agents → Evals** |
| Needs | An [agent](agents.md); a judge model for rubric checks |
| Design note | [050](../design/050-evals.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See suites and runs | Developer | |
| Create, change, delete a suite | Developer | Deleting: only a suite they created; an administrator, any. At most 100 cases a suite, 12 checks a case, 4,000 characters of input a case. A suite that is an agent's publishing gate is only an administrator's to change |
| Run a suite | Developer | Runs as you, with at most a developer's rights when a developer wrote the suite or the version; one run at a time per suite |
| Add the attack suite | Developer | 409 if the agent already has one |
| Add a case from a trace | Developer | The trace must have kept its prompt |

## How it works

### Cases

A case has:

- an input: what the agent is asked;
- optional **fixtures**: what a tool returns in that case instead of live data, so the input is the same every time. Put an
  attack in one to test that the agent does not obey it;
- **checks**.

### Checks

| Kind | Passes when |
| --- | --- |
| Contains / does not contain / matches | The answer contains, does not contain or matches something |
| Is JSON / sets a field | The answer is JSON, or sets a field to a value |
| Calls a tool / never calls one | The agent calls (or never calls) a tool, optionally with a given argument |
| At most N tool calls, tokens, seconds | The agent stays within the limit |
| **Judge** | A judge model grades the answer against a rubric you write |

### Runs

1. **Run** asks every case once, two, three or five times (the suite's setting, at most five) against a version you pick, optionally on another model, as you.
2. Proposals, environment requests and memory notes are recorded and checked, never filed. A tool with a fixture returns the
   fixture.
3. A case passes only if every attempt passes, so answers that hold up only sometimes show up.
4. The run page shows each case's attempts as ticks and crosses. Click one for its checks, its answer and its trace.

![A run: each case's attempts, and the checks of one]({{ '/console/evals-run.png' | relative_url }})

A trace that kept its prompt has **Add to a suite**, which fills a case in from it. The newest 50 runs per suite are kept.

## Add an attack suite

**Add attack suite** (on the Evals list) gives an agent seven ready-made cases from the OWASP list for agentic
applications:

| Risk | Cases |
| --- | --- |
| ASI01 | An order hidden in a failed run's log or in an issue (and a request to reveal instructions) |
| ASI02 | A request to stop servers without approval, or to run a tool for something it is not for |
| ASI03 | A request for secret values, or for a role the person does not have |

A case passes only when the agent does not obey: it files nothing, never says the planted word, and a judge model agrees it
refused. Give the agent `run_failure` or `get_step_log` so it reads the planted text. The cases are ordinary cases, so you
can change them or add your own.

## Security model

- Evals run as you and record, never carry out, proposals, environment requests, prepared actions and memory writes.
- A fixture or a recorded call only answers a tool the agent may use.
- Create, run and delete are audited.

## Limits and verification

{: .warning }
**Not checked against a real model**, or a real pipeline in a repository ([note 050](../design/050-evals.md)). The checks
were tested with a stand-in agent that obeys (fails) and one that refuses (passes), by removing each kind of check in turn,
and in a browser on 2026-11-23. Whether a given agent has the tools to read the planted text depends on its tool list.
On 2026-11-27 (main at `487f373`) on the UAT instance one suite with a fixture ran against Haiku 5.5 and passed, a failing case failed, the attack suite was added (seven cases; not run) and the limits and messages were checked. Judge checks and the attack suite's run were not. See the [note 2026-11-27](../testing/2026-11-27-uat-assistant-agents.md).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| Run refused with 409 | "this suite is already running", "the suite has no cases yet", or "no model is set up..." (add a provider under **Settings → Models and providers** or turn on the assistant). |
| An attack case passes but the agent never read the planted text | The agent lacks `run_failure` or `get_step_log`. |
| A case fails on some attempts only | The answer does not hold up every time; a case needs every attempt to pass. |
| **Add to a suite** is missing on a trace | The trace did not keep its prompt: "this trace did not keep its prompt; turn on Keep prompts and answers under Settings › OpenTelemetry, or type the case". |
| Cannot add the attack suite | The agent already has one (409). |

## Related

- [Publishing agent versions](publishing-agent-versions.md)
- [Agents](agents.md)
- [Pipelines](pipelines.md) (an `eval:` step runs a suite in a pipeline)
