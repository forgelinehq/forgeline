---
title: Infrastructure map and services
parent: Guides
nav_order: 15
---

# Infrastructure map and services

These pages show what is running in your cloud accounts, read-only. The **Map** draws your AWS resources inside the
CloudFormation stack that made them and the VPC they sit in, and flags what needs a look. **Services** shows one tile per
AWS service. **Infrastructure** covers the Forgeline server itself, each connected cloud and the cloud changes.
**Kubernetes** lists clusters and what runs in them.

Use them to answer "what is running, and is anything wrong" without opening the AWS console. They never change anything.
To change something, draw it on the [cloud canvas](cloud-canvas.md) or ask the assistant to propose it.

## At a glance

| | |
| --- | --- |
| Where in the console | **Cloud → Map**, **Operations → Services**, **Operations → Infrastructure**, **Operations → Kubernetes** |
| Needs | A [connected cloud account](connect-aws.md) ([Google Cloud](connect-gcp.md), [Azure](connect-azure.md)); a registered cluster for Kubernetes |
| Design notes | [023 infrastructure map and logs](../design/023-infrastructure-map-and-logs.md), [034 the watch map](../design/034-watch-map.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See the Map, Services, Infrastructure, Kubernetes | Viewer | |
| See AWS spend (**Load from AWS** on **Home → Today**, **Load last 30 days** on **Infrastructure → AWS account**) | Developer | Each look is a Cost Explorer request that AWS bills, about a cent |
| Ask the assistant about a resource | Developer | |
| Register or remove a Kubernetes cluster | Administrator | Registering proves the cluster with a read first; removing changes nothing in the cluster |
| Connect or change an account | Administrator | See [Connect AWS](connect-aws.md) |

## How it works

1. Forgeline reads each connected account with read calls only, caches the answer for 60 seconds, and draws it.
   **Refresh** reads again.
2. The Map groups resources inside the CloudFormation stack that made them, then the VPC they are in, then the rest by
   kind.
3. A line joins a Lambda function to the queue or table stream it reads, with moving dots when Lambda reports the link as
   enabled, in the direction data goes. Traffic itself is not measured, and nothing is linked by similar names.
4. A strip lists what **needs a look**: a failing state, a security group open to the internet, an unencrypted volume,
   an alarm in alarm.

## Use the map

1. Open **Cloud → Map**. A switch at the top changes account; **Map** and **List** switch the view, and the search box
   filters by name, id or tag.
2. Pick anything to see which stack made it, what it uses and what uses it.
3. Open a flagged resource and, as a developer, choose **Ask the assistant to fix it** (**Ask the assistant about this**
   when nothing is flagged). The assistant drafts a proposal; nothing changes until a different administrator approves
   it (see [Home and approvals](home-and-approvals.md)).

![The map: a rolled-back stack, the resources that need a look, and a side panel]({{ '/console/infrastructure-map.png' | relative_url }})

## Browse services

**Operations → Services** shows one tile per AWS service (EC2, Lambda, S3, RDS, queues, stacks and so on).

1. The page draws at once, and each tile is read on its own, a few at a time, so the first ones fill in while the rest
   are still being read.
2. A service the role may not list says so on its tile.
3. Open a tile to see everything of that kind in the account on its own page, with search, details and alarms, read with
   that service's calls only. The Map's list view links to the same pages ("Open its page").

![The Services page: tiles by category, one showing "not listed"]({{ '/console/infrastructure-services.png' | relative_url }})

## Infrastructure and Kubernetes

**Operations → Infrastructure** has tabs for:

| Tab | Shows |
| --- | --- |
| Platform | The Forgeline server, the Docker host, running steps and the disk |
| AWS account, Google Cloud, Azure | What is in each connected cloud, by service (AWS keeps 200 resources per service with the true count; Azure reads at most 3,000 resources in all, from at most 20 subscriptions; the Google Cloud limit was not checked) |
| Cloud changes | Stacks, blueprints and environments proposed, approved and applied |

**Operations → Kubernetes** lists the registered clusters and their nodes, workloads, pods, services, namespaces and
warning events, read-only. A cluster is registered through a connected AWS account (EKS) or with a token, which is
stored and never shown again. The findings of Forgeline's checks are under **Security → Findings**.

## Permissions in AWS

The Map's stack boxes and lines use two more read permissions on top of the inventory: `cloudformation:ListStackResources`
and `lambda:ListEventSourceMappings`. Alarms need `cloudwatch:DescribeAlarms`. Both stack permissions are in the AWS
managed `ReadOnlyAccess` and `PowerUserAccess` policies. Without them the map still works, without stack boxes or lines,
and says which permission is missing.

## Security model

- **Read-only.** Only read calls are made. A refused part names the missing permission rather than guessing.
- **No guessed links.** A line appears only where AWS states the link, never from similar names.
- **Bounded.** The map reads at most 25 stacks (`stacks_skipped` counts the rest), alarm lists stop at 500, each service
  list keeps 200 resources (the true count is still shown), and the inventory is cached for 60 seconds so repeat looks
  cost no AWS call.
- **Secrets stay out.** A Kubernetes token is never returned by the API.
- **The AI is switched separately.** What the assistant may read in AWS follows the service switches under
  [Connections and AI access](connections-and-ai-access.md); people always see everything their role may.

## Limits and verification

{: .warning }
**Tested with fakes, and on 2026-11-27 read on a real account.** The Map and its links were checked against the fake AWS CLI (a rolled-back stack with a function reading a queue) and in a browser on demo data, with 14 of 14 rules failing a test when removed ([note 034](../design/034-watch-map.md)). On the UAT instance (main at `487f373`, a real primary account and an added account, as a developer and a viewer) these were read in a browser: the **Map** (a rolled-back stack with its six tables in a box, a stack that is only in review, the VPC box, "2 resources need a look", the account switch, the **Map** and **List** views, the search box, the side panel with **Ask the assistant to fix it**), **Services** (tiles by category with counts, an S3 page with its four buckets, the account switch), **Infrastructure** (Platform and AWS account) and **Kubernetes** (no cluster registered: "No clusters yet"). Nothing was changed. **Not checked.** A tile that says "not listed", the cache refresh timing, a stack box with its lines (no function read a queue in that account), alarms (none existed), a registered Kubernetes cluster, Google Cloud and Azure, and the limits (25 stacks, 200 resources, 3,000 for Azure). Reading a very large account is not recorded in a test note.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| A tile says "not listed" | The role may not list that service, or the account does not support the call. |
| The map has no stack boxes or lines | `cloudformation:ListStackResources` or `lambda:ListEventSourceMappings` is missing. |
| Alarms do not show | `cloudwatch:DescribeAlarms` is missing. |
| The page looks a minute old | Answers are cached for 60 seconds; choose **Refresh**. |
| A stack shows as a tile, not a box | Its resources could not be read, or more than 25 stacks exist (the rest are counted, not read). |
| A Kubernetes cluster cannot be added | A name is needed (and must be new), the environment must be production, staging, development or sandbox, and at most 30 clusters can exist. An EKS cluster also needs its name, region and a connected AWS account. |

## Related

- [Connect AWS](connect-aws.md), [Connect Google Cloud](connect-gcp.md), [Connect Azure](connect-azure.md)
- [Cloud canvas](cloud-canvas.md)
- [Blueprints and environments](blueprints-and-environments.md)
- [Connections and AI access](connections-and-ai-access.md)
