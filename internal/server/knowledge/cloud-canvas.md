---
title: Cloud canvas
parent: Guides
nav_order: 13
---

# Cloud canvas

The canvas is where you draw what should exist in an AWS account: a front door, apps on ECS, tables, file buckets,
queues, logs, databases, caches and secrets, joined by links. Forgeline turns the drawing into OpenTofu code, shows
what building it would do, and builds exactly that preview only after a different administrator approves it.

Use the canvas to create or change infrastructure without writing OpenTofu. A drawing is saved as a
[blueprint](blueprints-and-environments.md); nothing in AWS changes until a preview is approved.

## At a glance

| | |
| --- | --- |
| Where in the console | **Cloud → Canvas** (and **Open on the canvas** from a blueprint) |
| Needs | An [AWS connection](connect-aws.md); Forgeline downloads a pinned, checksummed OpenTofu itself. Building from GitHub also needs a [GitHub connection](connect-github.md) |
| Design notes | [035 blueprints](../design/035-blueprints.md), [039 more blocks](../design/039-more-blueprint-blocks.md), [040 any resource](../design/040-any-resource-any-cloud.md), [041 removal](../design/041-removing-what-a-blueprint-built.md), [043 image builds](../design/043-native-image-builds.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| Draw, save and check a blueprint | Developer | Saving needs the AWS account to be connected. The server keeps at most 200 blueprints |
| Preview changes | Developer | Reads only; refused while another preview or build of it runs (`this blueprint is busy with ...; try again when it ends`) |
| Send for approval | Developer | The preview must be finished, of the saved blueprint, under an hour old, from the current state, and change something |
| Approve a build or a removal | Administrator | Not whoever sent it; the [approval rules](approval-rules.md) count removals and replacements as high risk |
| Delete a blueprint | Developer | Only its author or an administrator, and only once nothing it built is left (the server answers `this blueprint has built things that still exist; ... Remove what it built first`) |
| Allow a build push rule ("Allow this push") | Administrator | |
| Suggest a repository, show the GitHub workflow, list images, use an image | Developer | |
| Ask the assistant to draft a change | Developer | 20 questions a minute |

## How it works

1. Open **Cloud → Canvas** and start a blueprint, or open one. Add blocks from the toolbar, drag them into place
   (arrow keys move the one in focus), and pick one to change it in the panel on the right.
2. Under **Links**, join blocks. Only links that make sense are offered. Every change is checked as you go, and **Save**
   stays off until a problem is fixed.
3. Choose **Preview changes**. Forgeline asks OpenTofu what building the saved blueprint would do. Each block says
   whether it would be added, changed, replaced or removed.
4. Choose **Send for approval** and give a reason of 5 to 500 characters (`say why in 5 to 500 characters; the approver reads
   it`). This files the preview as a cloud change. Until it is approved the person who sent it (or an administrator) can withdraw it with **Withdraw this proposal** on its page. The server refuses it when the
   preview is not ready (`preview the changes first`), is for an older version of the blueprint (`the blueprint changed
   after the preview; preview again`), is over an hour old (`the preview is more than an hour old; preview again`),
   changes nothing (`the preview changes nothing; there is nothing to approve`) or breaks a guardrail.
5. A different administrator approves in **Home → Approvals**. Forgeline applies exactly the approved plan, then the
   canvas shows **Built**.

![The canvas with its toolbar, blocks, and the Preview changes and Send for approval buttons]({{ '/console/cloud-canvas.png' | relative_url }})

### What the lines mean

| On the canvas | Meaning |
| --- | --- |
| Orange line | Traffic from the internet |
| Grey line | An app using data |
| Dashed line | Logs |
| Dashed block | Something that already exists: used, never changed |
| Moving dots | Which way things flow: the internet into a front door, traffic to the app, data back from what it reads, logs out |

The dots show what the blueprint configures, not measured traffic.

### Blocks

An app can call another app, connect to a database or a cache, read a secret, and scale between a fewest and a most
number of tasks. A front door can send paths such as `/api/*` to a second app. For anything the blocks and templates do
not cover, a **Resource** block can make any AWS resource type an administrator allowed under **Security → Resource
types**, with a form made from the provider's schema (see [Connect AWS](connect-aws.md#any-other-aws-resource)).

An app can run on **Fargate Spot** (**Where it runs**). It is much cheaper, but AWS may stop a task with two minutes'
warning and it is started again, so it suits workers and anything that can restart. "One regular task, the rest on
Spot" keeps one task on regular Fargate. The checks say which apps use Spot.

An **Agent** block runs your agent on Amazon Bedrock AgentCore from its container image in ECR (built for arm64,
answering on port 8080). Link it to the tables, files, queues and secrets it may use: its role gets exactly those and
Bedrock models, plus AWS's documented runtime permissions, nothing else. It is reached through AWS's API, never a
public port. That is the design: tests check the generated code, but a real AgentCore build has not been run (see the notice at the end).

## Use the assistant beside the canvas

The **Assistant** panel takes a change in your own words ("add a place to keep interview recordings") and answers with
what it would change, as sentences.

1. Describe the change.
2. Choose **Use this** to put its draft on the canvas, unsaved.
3. Save, preview and send for approval as usual.

It cannot read your account or do anything itself, and it will not touch a service an administrator turned off or set
to look only for the AI.

## Templates

**Templates** are reviewed, versioned solutions from Forgeline's library, added like any block (the dashed buttons at
the end of the Add bar). A template's settings form is drawn from its inputs, and the server checks every save against
the same rules. **Read its code** shows the OpenTofu module it builds. A plan keeps the template's code, so the
approved apply builds exactly what was previewed.

| Template | What it makes |
| --- | --- |
| Lambda function | A container image from ECR on AWS Lambda, run on a schedule, by messages on a linked queue, or on demand, with its own log group and a role that reaches only what it is linked to |
| Step Functions workflow | Runs the Lambda functions linked to it one after another in the order you link them, passing each result on and retrying a step that fails. Start it from the AWS console or the API |
| EKS cluster with namespaces | Built in two approved steps: the first preview makes the cluster and its nodes; once they exist, the next adds the namespaces, each with a quota and a network policy that admits traffic only from the same namespace. It costs about $0.10 an hour plus the nodes |
| Data pipeline | Takes records that linked apps send, optionally reshapes them with a linked Lambda function, and stores them as compressed JSON in the one bucket it is linked to, by day |

For a Data pipeline, each app that "sends to" it gets the stream's name as `<BLOCK>_STREAM` and may only put records
into it. Name the record's fields and types under **Columns** (`event=string`, `at=timestamp`), and query them with
Athena in the pipeline's workgroup, from the table `<database>.records`; no crawler runs. The data lives in the bucket
block, so removing what the blueprint built keeps it unless you choose to remove everything.

Right after a Step Functions workflow is first built, AWS can take up to a minute to let its new role start the
functions, so a run started at once may be refused. Start it again a minute later.

## Build an app's image from GitHub

An app or an agent can name where its **Code** lives: a GitHub repository, a branch and a folder.

1. Choose **Suggest a repository…**. Forgeline reads the Dockerfiles in the repositories on the GitHub allow list and
   lists the folders that fit the block, with why. Choose **Use** to fill them in.
2. Save. The blueprint now makes a private image repository for the block, and a role that only that branch can use,
   from GitHub Actions, to push images. GitHub's OIDC provider must be set up in the AWS account once.
3. Choose **Build from GitHub…**. Pick one of two ways to build:
   - **GitHub Actions**: Forgeline shows the workflow to add to the repository (its actions pinned to commit SHAs) and a
     Forgeline pipeline that starts it (save it as a pipeline under **Delivery → Pipelines**).
   - **Built by Forgeline**: a `forgeline.yaml` for the repository with a `build` step that pushes to the block's image
     repository, and the push rule that allows it. An administrator chooses **Allow this push** once the blueprint has
     been previewed, since the rule needs the AWS account (until then the dialog says `Preview the blueprint first: the push rule needs the AWS account.`; a developer sees `an administrator allows it`). Forgeline's own AWS connection pushes, only for runs GitHub
     starts for a push to that branch; pull requests build without pushing. See
     [Build an image](pipelines.md#build-an-image).

Forgeline never pushes to your repository, and running a new image is a blueprint change you preview and send for
approval.

## Run a new image

Once the blueprint is built:

1. Choose **Run a new image…** on the block. It lists the images pushed to the block's repository, newest first, and
   marks the one running now.
2. Choose **Use and preview**. This saves the blueprint with that image and starts the preview.
3. Send it for approval as usual.

Only images from the block's own repository are offered; any other image is set in the block's fields as before.

![The Run a new image dialog listing one pushed image]({{ '/console/cloud-canvas-run-new-image.png' | relative_url }})

## Remove what a blueprint built

**Remove…** appears once a blueprint is built. It makes a preview too, approved by someone else and applied exactly.

| Choice | What it removes | Approvals |
| --- | --- | --- |
| **Keep the data** | Apps, front doors, queues, caches and logs. Tables, buckets, databases and secrets stay, still protected | One |
| **Remove everything, data included** | Everything, in two steps when something is protected | Two: first deletion protection is turned off and the rest removed, then the data is deleted |

A final snapshot of each database is kept unless you untick it; it costs a little storage until someone deletes it in
AWS. Afterwards the canvas shows **Removed**, and the blueprint can be built again.

## Security model

- **Preview, then exactly that.** Approval applies the saved plan and nothing else. A plan made stale by another change,
  or older than an hour, cannot be sent.
- **A different administrator.** Whoever sends a change cannot approve it.
- **Safe settings are not options.** Files are private and encrypted, tables and buckets keep their data if you remove
  them, and only a front door is reachable from the internet. See [Blueprints](blueprints-and-environments.md#guardrails).
- **Least privilege per link.** A block's role gets only what it is linked to. An agent block never gets a public port.
- **Nothing pushes for you.** Forgeline never writes to your repository. A pull request's build never pushes an image,
  and a push rule limits which repository and branch may push where.
- **No second path.** Removal uses the same plan, guardrails and approval as a build, not `tofu destroy`.
- **Audited.** Saves, previews, proposals, image changes and approvals are in the [audit log](audit-log.md).

## Limits and verification

{: .warning }
**Checked on real AWS, not everything.** The Lambda template and the Step Functions template, the Data pipeline and the
EKS namespaces were each built, used and removed through approvals on a real account
([Lambda](../testing/2026-11-15-lambda-template.md), [Step Functions](../testing/2026-11-16-step-functions-template.md),
[Data pipeline](../testing/2026-11-16-data-pipeline.md), [EKS](../testing/2026-11-16-eks-namespaces.md)). Running a new
image was checked on real AWS ([2026-11-16](../testing/2026-11-16-run-new-image.md)), and so was a native build pushing to
real ECR ([2026-11-13](../testing/2026-11-13-native-builds.md)). Removal was checked on real AWS with a queue, a protected
table, a protected bucket and a log group, each step approved by a second principal. **Not checked:** removing a real
database (RDS), with or without a final snapshot, because building one costs money. The AgentCore agent template needs a
raised AgentCore quota on the account and its real build is still to do ([note 040](../design/040-any-resource-any-cloud.md)).
The generated OpenTofu was validated with OpenTofu 1.13.9 and AWS provider 6.72 in the tests.
On 2026-11-27 (commit `487f373`) a queue, a table and a log group were built and then removed through the API as a developer, each change approved by
the server token, and a stale preview and a missing preview were refused with the texts above ([note](../testing/2026-11-27-uat-cloud-security.md)).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| **Save** is off | A check found a problem; the canvas names it. |
| **Send for approval** is off or refused (texts under How it works) | The preview is not finished, is older than an hour, is not of the saved blueprint, changes nothing, or a guardrail says **deny**. Preview again. |
| Two changes wait for the same preview (**Send for approval** pressed twice) | Each press files one. Approve one; the other then fails with "built again" because the preview is stale. Withdraw the extra with **Withdraw this proposal**. |
| The preview says planning for a long time | Another preview or build of the same blueprint is running. |
| **Suggest a repository** finds nothing | GitHub is not connected, or the repository is not on the GitHub allow list. |
| **Run a new image** is empty | The block's repository has no pushed images yet, or the blueprint is not built. |
| A Step Functions run is refused right after the first build | AWS has not yet let the new role start the functions; try again a minute later. |
| **Allow this push** is missing | It needs an administrator, and the blueprint must have been previewed first. |

## Related

- [Blueprints and environments](blueprints-and-environments.md)
- [Home and approvals](home-and-approvals.md)
- [Connect AWS](connect-aws.md)
- [Write pipelines](pipelines.md)
- [People and roles](people-and-roles.md)
