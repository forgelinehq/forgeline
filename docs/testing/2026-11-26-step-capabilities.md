---
title: Step container capabilities, measured 2026-11-26
---

# Dropping Linux capabilities in step containers: what breaks

Before choosing a default ([note 005](../design/005-threat-model.md), T2, decision 2) eleven typical steps were run with real
containers on the development machine (Docker, amd64), once with `forgeline run --capabilities relaxed` and once with
`--capabilities strict`. Every step runs with `--cap-drop ALL`; relaxed adds back `CHOWN, DAC_OVERRIDE, FOWNER, FSETID, KILL,
NET_BIND_SERVICE, SETGID, SETUID` (Docker's default set without raw sockets, device nodes, file capabilities, capability
changes and chroot); strict adds back nothing.

| Step | Relaxed | Strict |
|---|---|---|
| Write a file into the workspace as root | pass | **fail** (`Permission denied`) |
| `apk add curl` (alpine) | pass | pass |
| `apt-get install jq` (ubuntu) | pass | **fail** (`setgroups`, `seteuid`, `chown to _apt` not permitted) |
| `go build` (golang) | pass | pass |
| `npm install` (node) | pass | pass |
| `pip install` (python) | pass | pass |
| `chown 1000:1000 file` | pass | **fail** |
| Listen on port 80 | pass | pass (Docker lets any process bind low ports) |
| `ping` (alpine, busybox) | pass | pass (unprivileged ICMP) |
| `wget https://…` | pass | pass |
| `git clone` (alpine/git) | pass | pass |

Strict fixes, one line in the step's `permissions.capabilities`:

| Step | Smallest set that worked |
|---|---|
| Write into the workspace | `DAC_OVERRIDE` (root does not own the folder; the host user does) |
| `chown` | `DAC_OVERRIDE`, `CHOWN` |
| `apt-get install` | `CHOWN, SETUID, SETGID` was **not** enough; `CHOWN, SETUID, SETGID, DAC_OVERRIDE, FOWNER` worked |

## What it means

- Almost every real step writes into the workspace as root, so **strict needs `DAC_OVERRIDE` on most steps**. That is why relaxed
  is the default for now. Strict is a good setting for pipelines whose steps run as a non-root user or write only to `/tmp`.
- Relaxed still drops the capabilities nothing above needed. Dropped-capability tests also run in CI against a real Docker
  (`TestDockerCapabilitiesAreDropped`: the effective set is empty with none given back, a root write into a host-owned folder fails
  without `DAC_OVERRIDE`, and a grant or the relaxed baseline fixes it).
- Not measured: Docker-in-Docker, steps that need `NET_ADMIN` or `SYS_ADMIN` (those cannot be granted from a pipeline file), arm64
  (the Raspberry Pi), and pipelines on the Pi or other servers.
