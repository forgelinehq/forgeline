---
title: Pipeline editor and runs
parent: Guides
nav_order: 20
---

# Pipeline editor and runs

This page covers the console side of pipelines: the list of pipelines, the editor with its live graph, versions, and the
run page with its logs and its explanation of a failure. For the pipeline file itself (steps, `needs`, permissions,
builds) see [Write pipelines](pipelines.md).

Use the editor to write or change a pipeline with the server checking it as you type, and the run page to follow a run
and find out why it failed.

## At a glance

| | |
| --- | --- |
| Where in the console | **Delivery → Pipelines** |
| Needs | Docker on the server; a pipeline file ([Write pipelines](pipelines.md)) |
| Design notes | [Note 001](../design/001-execution-engine.md), [note 006](../design/006-console.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See pipelines, runs and step logs | Viewer | Secrets appear as `***` in logs |
| Check a pipeline file | Viewer | Saves nothing |
| Create a pipeline, save a version, run, cancel, restore a version | Developer | A save refused with 409 means someone saved after the version you started from |
| Delete a pipeline | Administrator | Deletes its versions too |

## How it works

1. **Delivery → Pipelines** lists every pipeline with its last run, a sparkline of recent runs, its success rate and
   its average time.
2. Opening a pipeline shows a YAML editor that the server checks as you type, beside a live graph of its steps. The graph
   says **Valid**, or names the problem with its line.
3. Every save is a version. **History** lets you view an older version or restore it, which makes it the newest.
4. **Run** starts the latest version. The run page draws the steps as a graph coloured by status, with live logs.

![A pipeline: the YAML editor beside its step graph, with Add step, History, Save and Run]({{ '/console/pipeline-editor.png' | relative_url }})

## Edit a pipeline

1. Open the pipeline, or choose **New pipeline** on Home.
2. Edit the YAML, or choose **Add step** to build a step from a form: its needs, network, secrets, time limit, or a Claude
   Code agent.
3. Choose **Save**. It stays off until the file is valid.
4. To try an older version, choose **History**, view it, and restore it if you want it back.

## Follow a run

1. Choose **Run**, or open a run from the list.
2. The run page shows who started it, the trigger, when it was created, how long it took and its source commit if it ran
   against a repository.
3. Steps are boxes on a graph, coloured by status; dots move along the lines into a step while it runs. Pick a step to
   read its output. **Follow** keeps the log scrolling.
4. A running run can be cancelled.

## Find out why a run failed

A failed run shows **What went wrong** above the graph. It names the step that failed first and its exit code, lists
likely causes and the steps that did not run, and shows the end of its output. **Show the step** jumps to that step's log,
and **Ask the assistant** opens the assistant with the failure as context.

![A failed run: what went wrong, the run's details and the step graph]({{ '/console/pipeline-run-failed.png' | relative_url }})

## Security model

- **Logs mask secrets.** A step's output shows `***` where it printed a secret it was given.
- **Steps get only what they list.** Network and secrets come from the step's `permissions`; see
  [Write pipelines](pipelines.md) and [Secrets and memory](secrets-and-memory.md).
- **Pull-request runs are untrusted.** They get no secrets; see [People and roles](people-and-roles.md#security-model).
- **Audited.** Pipeline saves and run starts are in the [audit log](audit-log.md).

## Limits and verification

The pipeline pages were checked in a browser against demo pipelines. The server's own tests cover validation, runs and the
failure explanation. This page makes no claim beyond that; the real-service checks for pipelines that build images or
start GitHub workflows are on [Write pipelines](pipelines.md) and [GitHub Actions](github-actions.md).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| **Save** is off | The YAML is not valid; the graph panel names the problem. |
| A save is refused with a conflict | Someone saved a newer version after you opened it. Reload and apply your change again. |
| **Run** fails to start with a source | The branch is no longer on the GitHub allow list, or GitHub could not give the newest commit. |
| A run's step shows `***` | A secret it was given was printed; that is the masking. |
| The run says a step failed with exit code 1 | The command reported failure; read the end of the output in **What went wrong**. |

## Related

- [Write pipelines](pipelines.md)
- [GitHub Actions](github-actions.md)
- [Secrets and memory](secrets-and-memory.md)
- [Console basics](console-basics.md)
