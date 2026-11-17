---
title: Policy packs checked, 2026-11-17
---

# Policy packs on the Raspberry Pi, 2026-11-17

The real check of policy packs (#147, [note 040](../design/040-any-resource-any-cloud.md) step 6) on the maintainer's
Raspberry Pi Forgeline (arm64), after the updater deployed it, with OpenTofu 1.13.9 and the Pi's AWS role. Blueprints
were filed by `claude-agent`; the packs were set and the changes approved with the server token, as the maintainer
allows for Claude's own tests. Account numbers are masked.

## Choosing the default

Before building, the three tools were run by hand on real plans made on the Pi (a 57-resource web stack and the EKS
cluster), each in a container with no network, a read-only root, no capabilities and a non-root user:

| | Conftest 0.71.1 | Trivy 0.76.2 | Checkov 3.4.5 |
|---|---|---|---|
| Image (arm64) | 91 MB | 186 MB | 579 MB |
| Time on the web stack, 256 MB to 1 GB | 0.7 to 0.8 s | 1.8 to 2.4 s | 6.0 to 9.1 s |
| Findings, web stack / EKS | own rules | 11 / 7 | 29 / 6 |
| Severities offline | n/a | low to critical | none |

Each tool reported the same on the cut-down copy of the plan (no sensitive values, no earlier state) as on the full
plan, except Trivy on the web stack (11 against 12 findings).

## Steps

1. Packs set: Trivy denying from **high**, Checkov (warnings only), and a Conftest pack with two rules (log groups keep
   logs at least 30 days: deny; a queue without an `owner` tag: warn).
2. A web stack (front door, app, table), previewed only: 18 to add, in 36 seconds. Trivy denied twice on the load
   balancer (`AWS-0052` invalid headers, `AWS-0053` exposed to the internet) and warned three times; Checkov warned 15
   times; the built-in `public-web` warning stayed. Send for approval: **409**, naming `aws_lb.web`.
3. One queue: previewed with Conftest's warning (`a queue should name its owner in a tag`, on `aws_sqs_queue.jobs`).
   Then the packs were changed (Trivy to warnings only), and Send for approval of the earlier preview: **409** "the
   policy packs changed since the preview; preview again".
4. Previewed again (48 seconds), sent, approved: 1 added. The approver's note named each pack, its tool version and its
   warnings.
5. Removed (one approved step, the packs checked the removal too), both blueprints deleted, the packs put back as they
   were (none). The audit log has each change of the packs as `guardrail.packs`, with the Rego counted (1 file, 436
   bytes) and not copied.
6. The pack images Forgeline pulled were removed from the Pi afterwards.

## What it showed

- Trivy's "high" includes `AWS-0053`, a load balancer open to the internet, which is what a front door is. An
  organization that denies from high must skip `AWS-0053` (the guide says so); "critical" does not deny a front door.
- A preview with three packs takes about 10 to 15 seconds longer on the Pi, mostly Checkov.

## Not checked

- A pack whose image cannot be pulled on the Pi (no registry access); the tests cover a pack that cannot start.
- Checkov on amd64 servers other than this machine; Conftest rules over 32 KB.
