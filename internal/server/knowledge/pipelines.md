---
title: Write pipelines
parent: Guides
nav_order: 2
---

# Write pipelines

A pipeline is a list of steps. Each step is a container image plus one thing to do. Run
`forgeline schema` for the exact JSON Schema; most editors can use it for completion.

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

## Step fields

| Field | Meaning |
| --- | --- |
| `id` | Unique name inside the pipeline. |
| `image` | Container image. Required. |
| `run` / `command` / `agent` / `workflow` / `build` / `eval` | **Exactly one.** `run` is a shell script, `command` an argument list with no shell, `agent` a coding agent, `workflow` starts a [GitHub Actions workflow]({{ '/guides/github-actions/' | relative_url }}) and waits for it, `build` builds a container image (see below), `eval` runs an agent's test suite (see below). `workflow`, `build` and `eval` steps have no image or permissions. |
| `needs` | Steps that must succeed first. Steps with no dependency between them run in parallel. |
| `inputs` | Files the step reads. If none changed since the last good run, the step is served from the cache (`--no-cache` turns that off). |
| `env`, `workdir`, `timeout` | Environment, working directory, and a time limit such as `10m`. |
| `permissions.network` | `none` cuts the step off from every network; `egress` allows outbound access. |
| `permissions.secrets` | Names of secrets this step may read. A step gets nothing it does not list. |
| `permissions.capabilities` | Linux capabilities this step needs, such as `CHOWN` or `DAC_OVERRIDE` (Docker's default set only). See below. |

### Linux capabilities

Every step container drops **all** Linux capabilities, then gets back a baseline. An administrator chooses the baseline under
Settings › Step container limits (`forgeline run` has `--capabilities`): **relaxed** (the default) keeps `CHOWN`, `DAC_OVERRIDE`,
`FOWNER`, `FSETID`, `KILL`, `NET_BIND_SERVICE`, `SETGID` and `SETUID` and drops raw network sockets, device nodes, file
capabilities, capability changes and `chroot`; **strict** keeps nothing. A step lists more in `permissions.capabilities`, and each
time a step lists one the mode did not already give, the audit log records it (`run.capability.grant`).

If a step fails with `Operation not permitted` or `Permission denied` after you choose strict, add the capability it needs to that
step. Measured on eleven common steps ([the test note](../testing/2026-11-26-step-capabilities.md)): in strict, writing into the
workspace as root needs `DAC_OVERRIDE`, `chown` needs `CHOWN`, and `apt-get install` needs `CHOWN`, `SETUID`, `SETGID`,
`DAC_OVERRIDE` and `FOWNER`. Package installs through `apk`, `npm`, `pip` and `go build` needed nothing.

## Run against a repository

A pipeline saved in the console can name a **source**: a repository and branch on the GitHub allow list (Settings ›
Connections › GitHub). Set it in the editor under **Source repository** and **Branch**. Every run then downloads that
branch's newest commit into its workspace before the first step, as a run GitHub starts does, and the run page shows
which commit it ran. The allow list is read again at each run: a branch taken off it stops being downloaded.

![A pipeline with a source repository]({{ '/console/138-pipeline-source.png' | relative_url }})

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
      args: {VERSION: "1.4"}      # build arguments; never secrets, they stay in the image's history
      network: none               # optional: the Dockerfile's RUN lines get no network
      push: 123456789012.dkr.ecr.ap-south-1.amazonaws.com/web
      tags: [latest]              # optional; the default is the first 12 characters of the commit
    timeout: 20m                  # optional; the default is 30 minutes
```

The Dockerfile's `RUN` lines get no secrets, no files from the server and no extra privileges. **Any run may build.
Pushing is trusted only for a run GitHub started for a push** (not a pull request, not a console run) to a branch that
an administrator's push rule under **Settings › Image builds** allows, to exactly the rule's repository. For each push
Forgeline asks the rule's AWS account for a short-lived ECR token, which never enters a build container or a log, and
builds without reusing cached layers. A pull request or a console run builds the same step without pushing, so pull
requests check that the image builds; a push to a branch without a matching rule fails. Every build and push is in the audit log, with the image's digest. Pushing does
not deploy: a deploy still goes through an approval. `forgeline run` builds and never pushes. See
[design note 043](https://github.com/forgelinehq/forgeline/blob/main/docs/design/043-native-image-builds.md).

![Adding a build step in the editor]({{ '/console/137-add-build-step.png' | relative_url }})

On a machine with an ARM processor (a Raspberry Pi, say), building for `linux/amd64` needs QEMU emulation installed in
Docker (`docker run --privileged --rm tonistiigi/binfmt --install amd64`, run once by whoever manages that machine).

## Test an agent

An `eval` step runs a suite from **Agents › Evals** and fails unless every case passes, so the pipeline that ships a change
to an agent can test it first:

```yaml
  - id: gate
    eval:
      suite: triage-regression   # the suite's id, shown in its address in Agents › Evals
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
Add one under **Secrets** (name in capitals, value of at least 8 characters). Request it with
`permissions.secrets`; it arrives as an environment variable of the same name and is masked as
`***` in stored output.

![A secret used with no network]({{ '/console/32-vault-secret-run.png' | relative_url }})

**How much a run is trusted follows from how it started**, and a pipeline file cannot claim more:

- A run started by a signed-in developer or administrator, in the console or through the API, is **trusted** and gets the secrets its steps ask for.
- A run for a **pull request** is **untrusted**: its pipeline file and code come from whoever opened it, so it gets **no secrets**, whatever the settings say. A step that asks for one fails, naming the secret and the reason, and the run page says "untrusted: no secrets".
- A run for a **push** to an allowed branch gets secrets **only if an administrator turned on "Secrets for pushes" for that repository** (Settings › GitHub). It is off by default: whoever can push to an allowed branch can write the pipeline file, so this is a decision about trusting them.

Every secret a step is given, and every refusal, is in the audit log with the run's trust.

## From the command line

```sh
forgeline run pipeline.yaml                                # locally in Docker
forgeline submit --server URL --wait pipeline.yaml         # to a server (uses FORGELINE_TOKEN)
```
