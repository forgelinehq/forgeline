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
| `run` / `command` / `agent` / `workflow` / `build` | **Exactly one.** `run` is a shell script, `command` an argument list with no shell, `agent` a coding agent, `workflow` starts a [GitHub Actions workflow]({{ '/guides/github-actions/' | relative_url }}) and waits for it, `build` builds a container image (see below). `workflow` and `build` steps have no image or permissions. |
| `needs` | Steps that must succeed first. Steps with no dependency between them run in parallel. |
| `inputs` | Files the step reads. If none changed since the last good run, the step is served from the cache (`--no-cache` turns that off). |
| `env`, `workdir`, `timeout` | Environment, working directory, and a time limit such as `10m`. |
| `permissions.network` | `none` cuts the step off from every network; `egress` allows outbound access. |
| `permissions.secrets` | Names of secrets this step may read. A step gets nothing it does not list. |

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
builds without reusing cached layers. Every build and push is in the audit log, with the image's digest. Pushing does
not deploy: a deploy still goes through an approval. `forgeline run` builds and never pushes. See
[design note 043](https://github.com/forgelinehq/forgeline/blob/main/docs/design/043-native-image-builds.md).

![Adding a build step in the editor]({{ '/console/137-add-build-step.png' | relative_url }})

On a machine with an ARM processor (a Raspberry Pi, say), building for `linux/amd64` needs QEMU emulation installed in
Docker (`docker run --privileged --rm tonistiigi/binfmt --install amd64`, run once by whoever manages that machine).

## Secrets
Add one under **Secrets** (name in capitals, value of at least 8 characters). Request it with
`permissions.secrets`; it arrives as an environment variable of the same name and is masked as
`***` in stored output.

![A secret used with no network]({{ '/console/32-vault-secret-run.png' | relative_url }})

Runs **started by GitHub get no secrets** yet; runs started by a signed-in developer or admin do.

## From the command line

```sh
forgeline run pipeline.yaml                                # locally in Docker
forgeline submit --server URL --wait pipeline.yaml         # to a server (uses FORGELINE_TOKEN)
```
