---
title: GitHub Actions
parent: Guides
nav_order: 4
---

# GitHub Actions

Use **Actions** in the console to start, watch and re-run your GitHub workflows without leaving Forgeline. Forgeline
only talks to GitHub's API; GitHub runs the jobs. A pipeline step can also start a workflow and wait for it.

Use it when you already have GitHub workflows and want one place to see them next to your Forgeline pipelines.

## At a glance

| | |
| --- | --- |
| Where in the console | **Ship → GitHub Actions**; **Add step → Kind → GitHub Actions workflow** in the pipeline editor |
| Needs | [GitHub connected](connect-github.md) with the **Actions: read and write** permission; `on: workflow_dispatch` in the workflow file to start one |
| Design | [Note 011](../design/011-github-actions.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See workflows, runs, jobs and logs | Developer | Only repositories on the allow list |
| Run a workflow, cancel, re-run all jobs or failed jobs | Developer | Only on a branch the allow list's rule allows; 20 writes a minute per person |
| Use a `workflow` step in a pipeline | Developer | Not in runs GitHub started |

## How it works

The page lists a repository's workflows and its latest 30 runs. Click a workflow to filter. Runs that are still going
refresh every 10 seconds. Open a run to see its jobs and steps and the end of a job's log.

![The Actions page: workflows on the left, runs on the right, a fork's run tagged]({{ '/console/github-actions.png' | relative_url }})

## Before you start

1. [Connect GitHub](connect-github.md). The page lists only the repositories on the allow list.
2. Give the GitHub App the **Actions: read and write** permission. Open the App's page on GitHub and approve the new
   permission (GitHub asks the App's owner). With a token instead of an App, the token needs the `repo` scope (classic)
   or the Actions permission (fine-grained).
3. For **Run**, the workflow file needs `on: workflow_dispatch`.

If the permission is missing you see: *the GitHub App does not have the Actions permission yet*.

## Run and watch a workflow

1. Open **Ship → GitHub Actions** and pick a repository.
2. Choose **Run** next to a workflow, pick an allowed branch and fill in any inputs.
3. Open the run to read a job's log, **cancel**, **re-run all jobs**, or **re-run failed jobs**.

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
- It succeeds only if the workflow's conclusion is `success`. If you cancel the Forgeline run, or the step times out,
  the GitHub run is canceled as well.
- The repository and branch are checked when the step runs, against the allow list. A refusal is in the audit log.
- **Runs started by GitHub cannot use it**, because the pipeline in a push or pull request is not trusted to start more
  work.
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
- A workflow can take at most 25 inputs.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| "the GitHub App does not have the Actions permission yet" | Approve the **Actions: read and write** permission on the App's page on GitHub. |
| A repository is missing from the page | It is not on the allow list. |
| **Run** is refused for a branch | The branch is not allowed by the repository's rule. |
| **Run** fails | The workflow file lacks `on: workflow_dispatch`. |
| A pipeline with a `workflow` step fails when GitHub starts it | Runs started by GitHub cannot use that step. |

## Related

- [Connect GitHub](connect-github.md)
- [Write pipelines](pipelines.md)
- [API: GitHub Actions](../api/reference.md)
