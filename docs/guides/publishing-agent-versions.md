---
title: Publishing agent versions
parent: Guides
nav_order: 46
---

# Publishing agent versions

By default, saving an agent makes the new version current at once. A **gate** changes that: you name an
[eval suite](evals.md) a new version must pass, and from then on a saved version is a **candidate** until it passes and is
published.

Use it for agents that workflows, teams and schedules rely on, so a change cannot reach them untested.

## At a glance

| | |
| --- | --- |
| Where in the console | An agent's page, under **Publishing** (buttons **Publish v<n>** and **Run the suite, publish if it passes**) |
| Needs | An [agent](agents.md) and an eval suite for it |
| Design note | [050](../design/050-evals.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Set or clear the gate | Administrator | The suite must be one of this agent's |
| Publish a version, or run the suite and publish if it passes | Administrator | |
| Roll back | Administrator | Not gated |
| Run the suite | Developer | See [Evals](evals.md) |

## How it works

1. On the agent's page, under **Publishing**, pick the suite a new version must pass.
2. From then on, saving makes a **candidate** (shown as such under Versions) while the current version keeps serving
   workflows, teams and schedules. People can still try the candidate.
3. **Publish** makes a candidate current once a run of the suite has passed on it since the suite last changed. The run
   must be on that version, with its own model, with every case passed.
4. **Run the suite, publish if it passes** does both, and leaves the version a candidate if any case fails.
5. **Rolling back is not gated**, so a known good version can always be restored.

![Publishing: v4 is a candidate and v3 stays current]({{ '/console/publishing-gate.png' | relative_url }})

Without a gate, a saved version is published at once.

## Security model

- A version cannot become current without a passing run of the gate suite on that exact version.
- A change to the suite after a pass means the version needs a new run.
- Setting the gate and publishing are audited (`ai.agent.gate`, `ai.agent.publish`).
- A rollback is deliberately not gated.

## Limits and verification

{: .warning }
**Checked in a browser against a stand-in model**, not a real model ([note 050](../design/050-evals.md)).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| Publish is refused (409) | `vN has no passing run of <suite> since the suite last changed; run it (or publish when it passes)`. |
| Publish is refused: "the gate suite ... no longer exists" | The suite was deleted; choose another, or turn the gate off. |
| My new version is not used by a workflow | It is a candidate; publish it. |
| The suite cannot be chosen | "suite: not one of this agent's suites", or "suite: it has no cases yet, so nothing could pass it". |
| **Run the suite, publish if it passes** left a candidate | At least one case failed. |

## Related

- [Evals](evals.md)
- [Agents](agents.md)
- [Versioning](versioning.md)
