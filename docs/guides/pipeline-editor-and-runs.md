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
| Create a pipeline, save a version, run, cancel a run, restore a version | Developer | A save is refused if someone saved after the version you started from |
| Open a run's trace, ask the assistant about a failure | Developer | |
| Delete a pipeline | Developer | Deletes its versions too; past runs stay in the run list |

## How it works

1. **Delivery → Pipelines** lists every saved pipeline with its number of steps, its version, its latest run and who
   last changed it. Home has a second table with each pipeline's recent runs, success rate and average time.
2. Opening a pipeline shows a YAML editor that the server checks as you type (after a short pause), beside a live graph
   of its steps. The graph panel says **Valid**, or becomes a **Problems** list, each with its path such as
   `steps[1].needs[0]`. Valid pipelines may also show **Suggestions** (a step that holds secrets and has network access, or
   an image without a pinned tag) and a warning for secrets that are requested but not set.
3. Every save is a version, and the last 20 are kept. **History** lets you view an older version or restore it, which
   makes it the newest. The name inside the YAML is the pipeline's name and cannot be changed on a saved pipeline; create
   a new one to rename.
4. **Run** starts the latest saved version. If you have unsaved changes the button reads **Save & run** (**Create & run**
   for a new pipeline). The run page draws the steps as a graph coloured by status, with live logs.

![A pipeline: the YAML editor beside its step graph, with Add step, History, Save and Run]({{ '/console/pipeline-editor.png' | relative_url }})

## Edit a pipeline

1. Open the pipeline, or choose **New pipeline** here or on Home. A new pipeline starts from a two-step example.
2. Edit the YAML, or choose **Add step** to build a step from a form. The kinds are **Script**, **Claude Code agent**,
   **GitHub Actions workflow** and **Build an image**; the form also sets what it runs after, network access, a time
   limit and the secrets it may read. The step is added at the end of the file. (An `eval` step is written by hand.)
3. Add a description of up to 300 characters, and, if GitHub is connected, a **Source repository** and **Branch**
   (see [Write pipelines](pipelines.md#run-against-a-repository)).
4. Choose **Save** (**Create** for a new pipeline). It stays off until the file is valid and changed.
5. To try an older version, choose **History**, view it, and restore it if you want it back.
6. If someone saved after you opened the pipeline, a **This pipeline changed** dialog offers **Keep editing**, **Load
   their version** (your edits stay on screen until you do) or **Overwrite**.
7. Developers and administrators have a trash button that deletes the pipeline after they type its name.

## Follow a run

1. Choose **Run**, or open a run from the list.
2. The run page shows who started it, the trigger, when it was created, how long it took and its source commit if it ran
   against a repository.
3. Steps are boxes on a graph, coloured by status; dots move along the lines into a step while it runs. Pick a step to
   read its output. **Follow** keeps the log scrolling.
4. A running run can be cancelled with **Cancel** (a confirmation dialog first). A finished run has **Run again**, and
   developers also get a **Trace** button.

## Find out why a run failed

A failed run shows **What went wrong** above the graph. It names the step that failed first and its exit code, lists
likely causes and the steps that did not run, and shows the end of its output (under **End of the output**). The
causes come from fixed rules, for example "Exit code 127 means the shell could not find the command" or "The step hit its
time limit". If none applies it says "The command ran and reported failure. Read the end of the output." A run that fails before
any step does says "The run failed before any step did", with the reason. **Show the step** jumps to that step's log,
and **Ask the assistant** opens the assistant with the failure as context.

![A failed run: what went wrong, the run's details and the step graph]({{ '/console/pipeline-run-failed.png' | relative_url }})

## Security model

- **Logs mask secrets.** A step's output shows `***` where it printed a secret it was given.
- **Steps get only what they list.** Network and secrets come from the step's `permissions`; see
  [Write pipelines](pipelines.md) and [Secrets and memory](secrets-and-memory.md).
- **Pull-request runs are untrusted.** They get no secrets, and the run page says "untrusted: no secrets"; see
  [Write pipelines](pipelines.md#secrets).
- **Audited.** Pipeline saves and run starts are in the [audit log](audit-log.md).

## Limits and verification

The server's own tests cover validation, runs and the failure explanation. The editor and run page labels were compared
with `editor.js`, `run.js` and `console_pipelines.go`.

{: .warning }
**Run in a browser on 2026-11-27**, on the UAT instance (main at `487f373`), as a developer and a viewer: **New pipeline** with the live check (an unknown `needs` showed `steps[1].needs[0]  unknown step "x"` and kept **Create** off), **Create & run**, the run page and its output, a failed run with **What went wrong** (exit code 127, the skipped step, the end of the output), **History** with **Restore** (it made version 4), **Cancel** with its confirmation dialog, the **Add step** form, and the viewer's page (no Save, Run or Add step; only **History**). Through the API: a rename, a duplicate name, a stale save (409), restoring a version that is no longer kept, and the 403 for a viewer. The real-service checks for pipelines that build images or start GitHub workflows are on [Write pipelines](pipelines.md) and [GitHub Actions](github-actions.md).
**Not checked.** The **This pipeline changed** dialog in the browser (the 409 behind it was checked), a **Source repository**, **Follow** with a long log, **Ask the assistant**, the administrator's delete button, and the 1 MiB limit.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| **Save** is off | The YAML is not valid (the **Problems** panel names it), or nothing has changed. |
| "the name in the YAML is ... but this pipeline is ...; to rename, create a new pipeline" | You changed `name:` in a saved pipeline. |
| "a pipeline named ... already exists" | A new pipeline's name is taken. |
| "this pipeline changed (now version N, by ...) since you opened it" | Someone saved a newer version after you opened it. Choose **Load their version** and apply your change again, or **Overwrite**. |
| "version N is not kept (the last 20 are)" | Only the 20 newest versions can be restored. |
| "pipeline is larger than 1 MiB" | The file is over the limit. |
| **Run** fails to start with a source | The branch is no longer on the GitHub allow list, or GitHub could not give the newest commit. |
| The **Source repository** box is missing | GitHub has no repositories on its allow list, and this pipeline has no source. See [Connect GitHub](connect-github.md). |
| A run's step shows `***` | A secret it was given was printed; that is the masking. |
| The run says a step failed with exit code 1 | The command reported failure; read the end of the output in **What went wrong**. |

## Related

- [Write pipelines](pipelines.md)
- [GitHub Actions](github-actions.md)
- [Secrets and memory](secrets-and-memory.md)
- [Versioning](versioning.md)
- [Console basics](console-basics.md)
