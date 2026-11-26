---
title: Policy packs
parent: Guides
nav_order: 7
---

# Policy packs

Every blueprint preview is checked by Forgeline's built-in guardrails. A policy pack adds more rules: Trivy for known
misconfigurations, Checkov for a wider set, or your own rules written for Conftest in Rego. A rule that denies stops
the plan from being sent for approval; a rule that warns is shown to the approver.

Use packs when your organization has standards the built-in guardrails do not know, such as a minimum log retention.

## At a glance

| | |
| --- | --- |
| Where in the console | **Security → Policy packs** |
| Needs | Docker on the server |
| Design | [Note 040](../design/040-any-resource-any-cloud.md) (step 6) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Read the packs | Developer | |
| Change the packs | Administrator | At most 10 packs; audited |

## How it works

1. You preview a blueprint. Forgeline makes the plan with OpenTofu.
2. Each enabled pack runs in a container on a copy of the plan without its sensitive values.
3. A finding that your pack marks as deny stops the plan from being sent for approval. Everything else is a warning.
4. The preview and the approval say which pack and tool version checked the plan.

### The three tools

- **Trivy**: known misconfigurations, each rated low to critical. Choose **Deny from** (for example Critical) and a
  plan with such a finding cannot be sent for approval; everything below is a warning the approver reads.
- **Checkov**: a wider set. It does not rate its findings when it runs without a network, so each is a warning unless
  you list the rule under **Rules that always deny**.
- **Conftest**: your own rules in Rego. A `deny` rule stops the plan; a `warn` rule is a warning. The plan is the
  input, as OpenTofu prints it. A rule can return `{"msg": ..., "address": ...}` to name the resource:

  ```rego
  package main

  import rego.v1

  deny contains {"msg": msg, "address": rc.address} if {
  	some rc in input.resource_changes
  	rc.type == "aws_cloudwatch_log_group"
  	rc.change.after.retention_in_days < 30
  	msg := "logs must be kept at least 30 days"
  }
  ```

## Add a pack

1. Open **Security → Policy packs** and add a pack for one of the three tools.
2. For Trivy choose **Deny from**. For Trivy and Checkov, list rules under **Rules to skip** (the preview counts what was
   skipped) and **Rules that always deny**. For Conftest, add your `.rego` files.
3. Save, then make the preview again: a preview made before the packs changed must be made again before it can be sent.

Trivy and Checkov flag some choices Forgeline's blocks make on purpose, such as a load balancer open to the internet.
Trivy rates a load balancer open to the internet (`AWS-0053`) as high, so with **Deny from: High** skip that rule or no
front door can be built. Read the first previews, then list what your organization accepts under **Rules to skip** and
what it never accepts under **Rules that always deny**.

## Security model

- Each pack runs in a container with no network, a read-only file system and no capabilities, on a copy of the plan
  without its sensitive values.
- A pack that cannot run stops the plan; it never counts as a pass.
- Every change to the packs is in the [audit log](audit-log.md).

## Limits and verification

{: .note }
The three tools were run on real plans on a Raspberry Pi Forgeline with OpenTofu and a real AWS role on 2026-11-17 (see
[the test note](../testing/2026-11-17-policy-packs.md)).

A Conftest pack holds at most 10 `.rego` files of 32 KB each, and Forgeline keeps at most 10 packs.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| A preview cannot be sent for approval | A pack's deny rule matched. The preview names the rule. |
| The plan is stopped and no finding is named | A pack could not run (for example Docker is not reachable). It never counts as a pass. |
| A load balancer open to the internet is denied | Trivy rates `AWS-0053` as high; skip it or raise **Deny from**. |
| A changed pack has no effect on an old preview | Make the preview again. |

## Related

- [Approval rules](approval-rules.md)
- [Connect AWS](connect-aws.md)
- [People and roles](people-and-roles.md)
- [API: policy packs](../api/reference.md)
