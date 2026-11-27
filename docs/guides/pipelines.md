---
title: Write pipelines
parent: Guides
nav_order: 2
---

# Write pipelines

A pipeline is a list of steps. Each step is a container image plus one thing to do. Steps run in containers, in
order of their `needs`, and the run page shows each step's output.

Use a pipeline to build, test and publish your own code, to start existing GitHub workflows, to build container images,
or to gate a change to an agent behind its tests. Run `forgeline schema` for the exact JSON Schema; most editors can
use it for completion.

## At a glance

| | |
| --- | --- |
| Where in the console | **Delivery → Pipelines**; **Settings → Connections → Secrets** and **Image builds**; **Settings → AI → Step container limits** |
| Needs | Docker on the server (or on your laptop for `forgeline run`) |
| Design | [Note 001](../design/001-execution-engine.md), [note 043](../design/043-native-image-builds.md), [note 050](../design/050-evals.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See pipelines and runs | Viewer | |
| Validate a pipeline file | Viewer | Saves nothing |
| Save a pipeline or a new version, run it, cancel a run, restore an old version | Developer | |
| Delete a pipeline | Administrator | |
| List secret names | Developer | Never values |
| Create, replace or delete a secret | Administrator | |
| Choose the step container baseline | Administrator | **Settings → Step container limits**; developers can read it |
| Add push rules for built images | Administrator | **Settings → Image builds** |

## How it works

1. You save a pipeline in the console, or a repository holds a `forgeline.yaml` that GitHub starts (see
   [Connect GitHub](connect-github.md)), or you run a file with `forgeline run`.
2. Forgeline checks the file, listing every problem at once with its location (for example
   `steps[2].needs[0]: unknown step "x"`), and draws the step graph. Steps with no dependency between them run in
   parallel.
3. Each step runs in its own container with only the secrets it asks for and, by default, network access. A step whose
   `inputs` did not change since the last good run is served from the cache; a step with no `inputs` is never cached.
4. The run page shows each step's output (secrets appear as `***`) and, for a failed run, which step failed first.

## Write a pipeline

```yaml
version: v1alpha1
name: build-and-test
steps:
  - id: build
    image: golang:1.27
    run: go build ./...
    inputs: [go.mod, go.sum, "**/*.go"]

  - id: test
    image: golang:1.27
    run: go test ./...
    needs: [build]

  - id: publish
    image: alpine:3.24
    run: ./publish.sh
    needs: [test]
    timeout: 5m
    permissions:
      network: egress
      secrets: [REGISTRY_TOKEN]
```

### Step fields

| Field | Meaning |
| --- | --- |
| `id` | Unique name inside the pipeline: lowercase letters, digits and hyphens, starting with a letter, up to 63 characters (pipeline names follow the same rule). |
| `image` | Container image, with no spaces. Required for `run`, `command` and `agent` steps. |
| `run` / `command` / `agent` / `workflow` / `build` / `eval` | **Exactly one.** `run` is a shell script, `command` an argument list with no shell, `agent` a coding agent (see [Agent steps](agent-steps.md)), `workflow` starts a [GitHub Actions workflow](github-actions.md) and waits for it, `build` builds a container image (see below), `eval` runs an agent's test suite (see below). `workflow`, `build` and `eval` steps have no image or permissions. |
| `needs` | Steps that must succeed first. Steps with no dependency between them run in parallel. |
| `inputs` | Files the step reads. If none changed since the last good run, the step is served from the cache (`--no-cache` turns that off). |
| `env`, `workdir`, `timeout` | Environment variables, an absolute working directory (default `/workspace`), and a positive time limit such as `10m`. |
| `permissions.network` | `none` cuts the step off from every network; `egress` allows outbound access. Empty means `egress`. |
| `permissions.secrets` | Names of secrets this step may read. A step gets nothing it does not list. A name cannot also be in `env`. |
| `permissions.capabilities` | Linux capabilities this step needs, such as `CHOWN` or `DAC_OVERRIDE` (Docker's default set only). See below. |

### Linux capabilities

Every step container drops **all** Linux capabilities, then gets back a baseline. An administrator chooses the baseline under
**Settings → Step container limits** (`forgeline run` has `--capabilities`): **relaxed** (the default) keeps `CHOWN`, `DAC_OVERRIDE`,
`FOWNER`, `FSETID`, `KILL`, `NET_BIND_SERVICE`, `SETGID` and `SETUID` and drops raw network sockets, device nodes, file
capabilities, capability changes and `chroot`; **strict** keeps nothing. A step lists more in `permissions.capabilities`, and each
time a step lists one the mode did not already give, the audit log records it (`run.capability.grant`).

If a step fails with `Operation not permitted` or `Permission denied` after you choose strict, add the capability it needs to that
step. Measured on eleven common steps ([the test note](../testing/2026-11-26-step-capabilities.md)): in strict, writing into the
workspace as root needs `DAC_OVERRIDE`, `chown` needs `CHOWN`, and `apt-get install` needs `CHOWN`, `SETUID`, `SETGID`,
`DAC_OVERRIDE` and `FOWNER`. Package installs through `apk`, `npm`, `pip` and `go build` needed nothing.

## Run against a repository

A pipeline saved in the console can name a **source**: a repository and branch on the GitHub allow list (**Settings →
Connections → GitHub**). Set it in the editor under **Source repository** and **Branch**. Every run then downloads that
branch's newest commit into its workspace before the first step, as a run GitHub starts does, and the run page shows
which commit it ran. The allow list is read again at each run: a branch taken off it stops being downloaded.

![A pipeline with a source repository and branch]({{ '/console/pipelines.png' | relative_url }})

Such a run is still a console run: the pipeline comes from the console, it may use secrets, and a `build` step builds
without pushing (only runs GitHub starts for a push may push).

## Build an image

A `build` step builds a container image from the run's files with BuildKit, through the server's Docker (or your
laptop's, with `forgeline run`), and can push it to Amazon ECR:

```yaml
  - id: image
    build:
      context: app                # a directory of the repository; empty means the root
      dockerfile: Dockerfile      # relative to the context
      platform: linux/amd64       # optional; linux/amd64, linux/arm64 or linux/arm/v7
      args: {VERSION: "1.4"}      # at most 25 build arguments of up to 1000 bytes; never secrets, they stay in the image's history
      network: none               # optional: egress (the default) or none, for the Dockerfile's RUN lines
      push: 123456789012.dkr.ecr.ap-south-1.amazonaws.com/web   # an ECR repository, no tag
      tags: [latest]              # optional, at most 5; the default is the first 12 characters of the commit
    timeout: 20m                  # optional; with no timeout the build is limited to 30 minutes
```

The Dockerfile's `RUN` lines get no secrets, no files from the server and no extra privileges. **Any run may build.
Pushing is trusted only for a run GitHub started for a push** (not a pull request, not a console run) to a branch that
an administrator's push rule under **Settings → Image builds** allows, to exactly the rule's repository. For each push
Forgeline asks the rule's AWS account for a short-lived ECR token, which never enters a build container or a log, and
builds without reusing cached layers. A pull request or a console run builds the same step without pushing (the log
says "building without pushing: ..."), so pull requests check that the image builds. A GitHub push to a branch with no
matching rule fails with `push refused: no push rule lets owner/repo branch "x" push to ...`. Every build and push is
in the audit log. Pushing does not deploy: a deploy still goes through an approval. `forgeline run` builds and never
pushes. See [design note 043](../design/043-native-image-builds.md).

On a machine with an ARM processor (a Raspberry Pi, say), building for `linux/amd64` needs QEMU emulation installed in
Docker (`docker run --privileged --rm tonistiigi/binfmt --install amd64`, run once by whoever manages that machine).

## Test an agent

An `eval` step runs a suite from **Agents → Evals** and fails unless every case passes, so the pipeline that ships a change
to an agent can test it first:

```yaml
  - id: gate
    eval:
      suite: triage-regression   # the suite's id, shown in its address in Agents → Evals
      version: 3                 # optional: the agent version to test; empty means its current version
      model: openai/gpt-5        # optional: test on another model as provider/model
  - id: deploy
    needs: [gate]
    image: alpine:3.20
    run: ./deploy.sh
```

The step runs on the server, as the run, with the same safety as a run started in the console: nothing the agent does is
carried out, and every attempt is a trace that counts against the agent's budget. The log lists each case as `pass` or
`FAIL`; a failure names the failing cases and stops every step that needs the gate. **A run started by GitHub cannot run
an eval**, because it spends model tokens and its pipeline came from a push or a pull request. `forgeline run` on a laptop
cannot either: the agents live on the server.

## Secrets

An administrator adds one under **Settings → Connections → Secrets**: a name of letters, digits and underscores (not
starting with a digit), and a value of 8 characters to 16 KiB with no newline. Request it with
`permissions.secrets`; it arrives as an environment variable of the same name and is masked as `***` in stored output.
A secret set by the server's environment (`FORGELINE_SECRET_<NAME>`) cannot be changed in the console.


**How much a run is trusted follows from how it started**, and a pipeline file cannot claim more:

- A run started by a signed-in developer or administrator, in the console or through the API, is **trusted** and gets the secrets its steps ask for.
- A run for a **pull request** is **untrusted**: its pipeline file and code come from whoever opened it, so it gets **no secrets**, whatever the settings say. A step that asks for one fails, naming the secret and the reason ("this run was started by GitHub for a pull request, which is untrusted and receives no secrets"), and the run page says "untrusted: no secrets".
- A run for a **push** to an allowed branch gets secrets **only if an administrator turned on "Secrets for pushes" for that repository** (**Settings → Connections → GitHub**, on the repository's row). It is off by default: whoever can push to an allowed branch can write the pipeline file, so this is a decision about trusting them.

Every secret a step is given, and every refusal, is in the audit log with the run's trust.

## From the command line

```sh
forgeline run pipeline.yaml                                # locally in Docker
forgeline submit --server URL --wait pipeline.yaml         # to a server (uses FORGELINE_TOKEN)
```

## Security model

- **Nothing by default.** A step gets no secrets and only the network it asks for. `permissions.network: none` cuts it
  off from every network.
- **Trust follows from how the run started.** A pull-request run gets no secrets; a push run gets them only where an
  administrator allowed it for that repository; a signed-in developer's or administrator's run is trusted.
- **Capabilities dropped.** Every step container drops all Linux capabilities and gets back only the baseline. Each
  grant beyond the baseline is in the audit log as `run.capability.grant`.
- **Builds push only for pushes.** Only a run GitHub started for a push to a branch with a matching push rule may push an
  image, and only to that rule's repository. The Dockerfile's `RUN` lines get no secrets.
- **No GitHub-started runs for workflow or eval steps.** The pipeline in a push or pull request is not trusted to
  start more work or spend model tokens.
- **Audited.** Every secret a step is given, every refusal, every build and push, and every capability grant is in the
  audit log.

## Limits and verification

- `workflow`, `build` and `eval` steps have no image or permissions.
- A `build` step with no `timeout` is limited to 30 minutes. `forgeline run` builds and never pushes.
- `forgeline run` cannot run `workflow` or `eval` steps: the connection and the agents live on the server.
- The capabilities needed by common steps were measured on eleven steps (see the test note linked under
  *Linux capabilities*). Steps not in that list may need more in strict mode.

{: .warning }
**Run on the UAT instance on 2026-11-27** (main at `487f373`), as a developer and a viewer, in the console and through the API: a two-step pipeline written in the editor, checked, created and run; a failing run; validation messages for eight kinds of mistake (upper-case id, `run` with `command`, a secret also in `env`, a `workflow` step with an image, several problems at once, a missing secret and a mutable tag, a relative `workdir`, a capability that cannot be granted); a step with `network: none` could not reach the internet; a secret printed by a step showed as `***`; viewers were refused saving, running, restoring, deleting and listing secrets. Field names, limits, trust rules and the build push rules were also compared with `internal/pipeline` and `internal/server`.
**Not checked.** Runs started by GitHub (the pull-request and push trust rules), a source repository, `build` steps and the ECR push, `eval` steps, the QEMU note, strict capabilities and `forgeline run`.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| A step fails with `Operation not permitted` or `Permission denied` after you choose strict | The step needs a capability. Add it under `permissions.capabilities`. |
| A step that asks for a secret fails and the run page says "untrusted: no secrets" | The run is for a pull request. Pull-request runs never get secrets. |
| A push run gets no secrets | **Secrets for pushes** is off for that repository (default). |
| A `build` step fails with `push refused: no push rule lets ...` | The run is a GitHub push, but no rule under **Settings → Image builds** matches the repository, branch and target. |
| A `build` step succeeds but nothing was pushed | The run was not started by a GitHub push (a console run or a pull request builds without pushing). |
| A `workflow` or `eval` step fails on a GitHub-started run | Refused on purpose: "runs started by GitHub may not start GitHub workflows" or "...may not run evals". |
| Building for `linux/amd64` fails on an ARM machine | QEMU emulation is not installed in Docker (see *Build an image*). |
| A run with a source repository will not start | The branch is no longer on the GitHub allow list; the allow list is read at each run. |
| `forgeline submit` says to set `--server` and `FORGELINE_TOKEN` | It needs both (or `FORGELINE_SERVER`). |
| A step is skipped from the cache when you wanted a fresh run | None of its `inputs` changed. Use `--no-cache`. |

## Related

- [Connect GitHub](connect-github.md)
- [GitHub Actions](github-actions.md)
- [Agent steps](agent-steps.md)
- [Pipeline editor and runs](pipeline-editor-and-runs.md)
- [Versioning](versioning.md)
- [People and roles](people-and-roles.md)
- [API: pipelines and runs](../api/reference.md)
