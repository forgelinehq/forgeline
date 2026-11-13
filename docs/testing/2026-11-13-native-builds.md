---
title: Native builds checked, 2026-11-13
---

# Native image builds against real ECR, 2026-11-13

The check [note 043](../design/043-native-image-builds.md) asks for, on the maintainer's Raspberry Pi (aarch64,
Docker 29.9.2, BuildKit 0.34) with the merged build (#125, then #126). Account numbers are masked.

## How

GitHub cannot reach the Pi's Forgeline (the tunnel has no route to it), so with the maintainer's choice a **temporary
second Forgeline** ran on the Pi on a loopback port, with its own data directory and GitHub triggers for `acme/web:main`.
A small stand-in played GitHub: it served the repository's tarball (a `forgeline.yaml` with a build step and
`app/Dockerfile`) and took commit statuses; webhooks were signed with the temporary server's own secret. AWS was real:
the Pi's `forgeline-default` profile (IAM Roles Anywhere). The test repository `forgeline-build-check` was created through
a proposal filed by the `claude-agent` user's assistant and approved with the server token, after reading its script
(one `CreateRepository` call).

## Results

| Event | Run | What happened |
| --- | --- | --- |
| Push to `main` (commit `1111…`) | succeeded | Built from `app` (the build argument arrived), pushed to the test repository tagged with the commit; ECR shows the same digest Forgeline logged and audited. |
| Pull request #7 (#125) | failed | Push refused before building: "only a push to a branch may push images; this run is for refs/pull/7/head". Correctly not pushed, but a pull request should still build: fixed in #126. |
| Pull request #8 (#126) | succeeded | Built without pushing; the log and the audit say `not pushed (built only)` with the reason. No credential was asked for. |
| Push to `main` (commit `4444…`, #126) | succeeded | Pushed; ECR then held exactly the two images from the two pushes, nothing from either pull request. |

No `forgeline-build-*` client configuration was left in `/tmp`, and the stand-in's log of requests held no token.

## Cleaned up

The temporary server and stand-in stopped; the test ECR repository deleted with its images (directly with the Pi's
profile, since the assistant's change tool does not delete); the local image tags removed; the test folder with the
temporary token, webhook secret and session cookie removed. A few MB of BuildKit cache from these builds remain in the
Pi's Docker, shared with other projects' builds.

## Open

- GitHub-started runs on the Pi need a public route for `/webhooks/github` (a tunnel change, the maintainer's call).
- Source checkout for console and scheduled runs (note 043, slice 2).
