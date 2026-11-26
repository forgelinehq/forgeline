---
title: GitHub Actions
parent: Guides
nav_order: 4
---

# GitHub Actions

Use **GitHub Actions** in the console to start, watch and re-run your GitHub workflows without leaving Forgeline. Forgeline
only talks to GitHub's API; GitHub runs the jobs. A pipeline step can also start a workflow and wait for it.

Use it when you already have GitHub workflows and want one place to see them next to your Forgeline pipelines.

## At a glance

| | |
| --- | --- |
| Where in the console | **Delivery → GitHub Actions**; **Add step → Kind → GitHub Actions workflow** in the pipeline editor |
| Needs | [GitHub connected](connect-github.md) with the **Actions: read and write** permission; `on: workflow_dispatch` in the workflow file to start one |
| Design | [Note 011](../design/011-github-actions.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See workflows, runs, jobs and logs | Developer | Only repositories on the allow list |
| Run a workflow | Developer | Only on a branch the allow list's rule allows; 20 writes a minute per person |
| Cancel a run, re-run all jobs, re-run failed jobs | Developer | 20 writes a minute per person (shared with Run) |
| Use a `workflow` step in a pipeline | Developer | Not in runs GitHub started; at most 20 workflow steps start a minute across the server |

## How it works

Pick a repository from the list at the top. The page lists its workflows on the left and its latest 30 runs (status, run, branch, event, who, when) on the right. Click a workflow to filter. While any run is unfinished the page
refreshes every 10 seconds. Open a run to see its jobs and steps and the end of a job's log.

![The Actions page: workflows on the left, runs on the right, a fork's run tagged]({{ '/console/github-actions.png' | relative_url }})

## Before you start

1. [Connect GitHub](connect-github.md). The page lists only the repositories on the allow list.
2. Give the GitHub App the **Actions: read and write** permission. Open the App's page on GitHub and approve the new
   permission (GitHub asks the App's owner). With a token instead of an App, the token needs the `repo` scope (classic)
   or the Actions permission (fine-grained).
3. For **Run**, the workflow file needs `on: workflow_dispatch`.

If the permission is missing you see: *GitHub refused: the GitHub App does not have the Actions permission yet. Approve it on the App's page, then try again*.

## Run and watch a workflow

1. Open **Delivery → GitHub Actions** and pick a repository.
2. Choose **Run** next to a workflow, pick an allowed branch (only branches named exactly in the rule are suggested) and add any inputs, then choose **Run workflow**. The run appears in the list after a few seconds.
3. Open the run to read a job's log, **Cancel run** (while it runs), **Re-run all jobs**, or **Re-run failed jobs** (after a failure).

## Start a workflow from a pipeline

A step can start a workflow and wait for it, so a Forgeline pipeline can include your existing GitHub jobs:

```yaml
steps:
  - id: prepare
    image: alpine:3.24
    run: echo preparing
  - id: ci
    needs: [prepare]
    timeout: 30m            # how long to wait
    workflow:
      repo: acme/widgets
      workflow: ci.yml
      ref: main
      inputs: {environment: staging}
      # detach: true        # do not wait
```

In the editor use **Add step → Kind → GitHub Actions workflow**.

- It runs on the server, with the server's GitHub connection. No GitHub credential ever enters a container, and the step
  takes no image, script, environment or permissions.
- It succeeds only if the workflow's conclusion is `success`; otherwise it fails with `workflow finished with conclusion "..."`. If you cancel the Forgeline run, or the step times out,
  the GitHub run is canceled as well. With `detach: true` the step succeeds as soon as GitHub accepts the request.
- The repository and branch are checked when the step runs, against the allow list. A refusal is in the audit log.
- **Runs started by GitHub cannot use it**: "runs started by GitHub may not start GitHub workflows".
- `forgeline run` on your laptop cannot run it.

## Security model

- Only repositories on the allow list, and only branches their rule allows. Starting a workflow runs that branch's
  workflow file, so a branch you would not trust to push to is refused.
- Developers and administrators only. Every start, cancel, re-run and refusal is in the audit log, and writes are limited
  to 20 a minute per person.
- Logs and run names come from GitHub, so they are shown as plain text. The GitHub token is never sent to the address a
  log is downloaded from.
- A run whose code comes from a **fork** is tagged and carries a warning.

## Limits and verification

- **Runners on your own machines are not built yet.** The plan is single-use runners, started only for private
  repositories you list, with no Forgeline secrets and the credential kept in memory only. See
  [design note 011](../design/011-github-actions.md).
- The page shows the latest 30 runs, and the last 256 KB of a job's log.
- A workflow can take at most 25 inputs, each value up to 1000 bytes.

{: .warning }
**Not run for this review.** The labels, limits and messages were compared with `actions.js`, `console_actions.go` and
`console_workflow_step.go`; the real-GitHub behaviour (permissions, dispatch, fork tag) was not re-tested. The "Actions
read and write" permission list and the classic `repo` scope are not checked in the code.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| "the GitHub App does not have the Actions permission yet" | Approve the **Actions: read and write** permission on the App's page on GitHub. |
| A repository is missing from the page | It is not on the allow list. |
| **Run** is refused for a branch | The branch is not allowed by the repository's rule. |
| **Run** fails with "GitHub refused: ..." | GitHub's own message is shown; the usual cause is a workflow file without `on: workflow_dispatch`. |
| "too many actions; wait a minute" | More than 20 writes in a minute from you. |
| "that repository is not on the GitHub allow list" | Add it under **Settings → Connections → GitHub**. |
| "GitHub rejected the stored credentials; reconnect GitHub" | The token or App credential is no longer valid. |
| A pipeline with a `workflow` step fails when GitHub starts it | Runs started by GitHub cannot use that step. |

## Related

- [Connect GitHub](connect-github.md)
- [Write pipelines](pipelines.md)
- [Pipeline editor and runs](pipeline-editor-and-runs.md)
- [API: GitHub Actions](../api/reference.md)
