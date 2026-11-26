---
title: Blueprints and environments
parent: Guides
nav_order: 14
---

# Blueprints and environments

A **blueprint** is a saved drawing of what should exist in one AWS account, with the OpenTofu code it generates and a
plain-words summary of what it means. **Resources** (the Built page) lists everything Forgeline created from blueprints
and approved changes, and checks it against AWS. **Environments** are temporary stacks from templates, with an owner, a
time to live and a cost estimate, removed when their time is up.

Use blueprints for infrastructure that should stay, and environments for a developer box or a test sandbox that should
disappear by itself. You draw and change blueprints on the [cloud canvas](cloud-canvas.md); this page covers the list,
the checks, the code and what exists afterwards.

## At a glance

| | |
| --- | --- |
| Where in the console | **Cloud → Blueprints**, **Cloud → Resources**, **Cloud → Environments**; guardrails under **Security → Policy packs** and **Security → Resource types** |
| Needs | An [AWS connection](connect-aws.md). Policy packs need Docker on the server |
| Design notes | [035 blueprints](../design/035-blueprints.md), [041 removal](../design/041-removing-what-a-blueprint-built.md), [017 platform roadmap](../design/017-platform-roadmap.md) (environments) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| List, open, create and edit blueprints | Developer | A save refused with 409 means someone saved it since you opened it |
| Delete a blueprint | Developer | Only its author or an administrator |
| Read the generated OpenTofu code | Developer | |
| See Resources and run **Check against AWS** | Developer | A read only |
| Ask for an environment | Developer | Starts at once only if the self-service policy allows; otherwise an administrator approves |
| Extend or delete an environment | Developer | Its owner or an administrator |
| Set the environment self-service policy | Administrator | |
| Add policy packs, allow resource types | Administrator | Developers can read them |

## How it works

1. **New blueprint** starts from **Empty**, **Small web app** or **Queue worker**. You edit it on the canvas.
2. The blueprint page says in plain words what it means: what is open to the internet, whether data is kept, how long
   logs are kept. It also shows the OpenTofu code it generates (`main.tf.json`, the AWS provider pinned to `~> 6.72`).
3. A preview runs the code through `tofu plan`. The [guardrails](#guardrails) check the result.
4. After a different administrator approves, the plan is applied and the blueprint appears under **Resources**.

![A blueprint: its blocks, how they connect, what it means, and the generated code below]({{ '/console/blueprints-blueprint.png' | relative_url }})

### What the safe settings are

These are not options on any block:

| Block | Always |
| --- | --- |
| Files (S3) | Private and encrypted; kept if you remove the blueprint |
| Tables | Kept if you remove the blueprint |
| Anything reachable from the internet | Only a front door |
| Databases | Private, encrypted and protected from deletion. AWS creates their passwords and keeps them in Secrets Manager; an app gets the password when it starts (as `DB_PASSWORD` for a database block called `db`) and Forgeline never sees it |
| Secret blocks | Created empty. You set the value in AWS, and only the apps linked to it can read it |

## Guardrails

Guardrails check every preview before it can be sent for approval.

| Line | Meaning | Examples |
| --- | --- | --- |
| Red: **deny** | **Send for approval** stays off until the blueprint changes. The attempt is audited | A database open to the internet, an unencrypted disk, an IAM user, a policy that allows every action |
| Amber: **warning** | Stops nothing; the approver reads it next to the changes | A web port open to the internet on a front door, deleting a table |

An administrator can add more rules under **Security → Policy packs** (Trivy, Checkov, or your own Conftest rules). Their
findings appear in the same list, and the line under it says which rule sets checked the preview. See
[Policy packs](policy-packs.md).

For anything the blocks and templates do not cover, a **Resource** block can make any AWS resource type an administrator
allowed under **Security → Resource types**.

## See what Forgeline built

**Cloud → Resources** (the page is titled **Built by Forgeline**) lists everything Forgeline created and keeps in your
cloud accounts:

- each blueprint that was built, drawn as an interactive diagram (its blocks as on the canvas, with moving dots showing
  which way things flow);
- stacks deployed through approved changes;
- environments that are up.

1. Pick a block to see the AWS resources it made, by name, id and ARN. Nothing else from the saved state leaves the
   server.
2. Choose **Check against AWS**. Forgeline looks each resource up in the account and marks it as still there or
   missing, so you notice something deleted outside Forgeline. A resource of a kind the inventory does not list is
   marked **not checked**.

![A built blueprint as a diagram, with the resources of the selected block]({{ '/console/blueprints-built.png' | relative_url }})

## Environments

An environment is a temporary CloudFormation stack made from a template. The templates are Developer box, QA sandbox,
Lambda sandbox, Event pipeline and Workflow sandbox.

1. Choose **New environment**, pick a template and its options, a purpose and a time to live. Forgeline shows an
   estimate of the monthly cost and whether the policy lets it start without approval.
2. If the request fits the **self-service policy**, it starts at once. Otherwise it is a cloud change that a different
   administrator approves, like any other.
3. Forgeline deletes the environment when its time is up. Its owner or an administrator can **Extend** it, up to the
   longest life the policy allows, or **Delete** it now.

An administrator sets the **Self-service policy**: which templates may start on their own, the longest request, the
longest life including extensions, how many active environments one person may have, and the largest monthly estimate
that skips approval. Self-service is off until an administrator turns it on. The defaults are 24 hours for a request,
168 hours for a lifetime, 2 active per person and $40 a month.

## Security model

- **Same approval as everything else.** A build, a removal and an environment outside the self-service policy are
  cloud changes approved by a different administrator, who sees the exact preview.
- **Data survives by default.** Tables, buckets, databases and secrets are kept when you remove a blueprint's other
  parts, and protected from deletion until a second approval.
- **Secrets stay in AWS.** Database passwords are created and held by AWS; a secret block's value is never in Forgeline.
- **State is sealed.** OpenTofu state is encrypted with Forgeline's vault, one state per blueprint and target, with a
  lock while a plan or apply runs.
- **Only names leave.** Resources lists name, id and ARN of what was made, nothing else from the state.
- **Audited.** Saves, deletes, previews and approvals are in the [audit log](audit-log.md).

## Limits and verification

{: .warning }
**Checked on real AWS in part.** The 2026-11-08 test blueprint and a round trip with a queue, a protected table, a
protected bucket and logs were built and removed on a real account, each step approved by a second principal
([note 041](../design/041-removing-what-a-blueprint-built.md)). The generated code passes `tofu validate` (OpenTofu
1.13.9, AWS provider 6.72). **Not checked:** removing a real RDS database, and cost estimates per block (there is no
real price source yet, [note 035](../design/035-blueprints.md)). Policy packs were run on real plans
([2026-11-17 test note](../testing/2026-11-17-policy-packs.md)).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| **Send for approval** stays off | A guardrail says **deny**, or the preview is not current. Read the red line and preview again. |
| Saving is refused with a conflict | Someone saved the blueprint after you opened it. Reload and apply your change again. |
| A policy pack stops the plan | The pack could not run (it needs Docker); a pack that cannot run never counts as a pass. |
| **Check against AWS** marks a resource missing | It was deleted outside Forgeline, or the role cannot read that service. |
| A new environment waits for approval | The template, length or cost is outside the self-service policy, or self-service is off. |
| Delete is refused on a blueprint | Something it built is still there; remove it first. |

## Related

- [Cloud canvas](cloud-canvas.md)
- [Infrastructure map and services](infrastructure-map-and-services.md)
- [Connect AWS](connect-aws.md)
- [People and roles](people-and-roles.md)
