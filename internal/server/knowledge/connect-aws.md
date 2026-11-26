---
title: Connect AWS
parent: Guides
nav_order: 5
---

# Connect AWS

Forgeline connects to an AWS account to look at it (stacks, instances, resources, spend) and to change it through
proposals that a different administrator approves. You can sign in with AWS IAM Identity Center from the console, or use
a named profile of the AWS CLI on the server. One connection is the primary account; an administrator can add more
accounts by profile, which are read-only until they allow changes for them.

Connect AWS before you use [runbooks](runbooks.md), [alarm intake](alarm-intake.md), the AWS map, cloud changes or
blueprints.

## At a glance

| | |
| --- | --- |
| Where in the console | **Operations → Infrastructure → AWS account → Connect** (**Change connection** once connected) |
| Needs | An AWS account with IAM Identity Center (for the sign-in), or an AWS CLI profile on the server |
| Design | [Note 009](../design/009-aws-signin.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See the account's stacks, instances and resources | Viewer | Read-only |
| See spend | Developer | Each request is billed by AWS, so it loads only when you ask |
| Connect, change or disconnect the connection, use a server profile | Administrator | |
| Add, remove or **Allow changes** for an extra account | Administrator | An added account is read-only until allowed |
| Turn the AWS MCP Server on or off | Administrator | Shown on the primary account only |
| Propose a change (deploy the demo, propose a stack deletion, start, stop or reboot an instance) | Developer | With a reason; for an added account only after **Allow changes** |
| Approve or reject a change | Administrator | Not the person who proposed it |
| Cancel a proposal | Developer | Your own; an administrator can cancel any |

## How it works

1. An administrator connects, by signing in with AWS or by naming a server profile.
2. Forgeline keeps the sign-in token sealed in its vault and the role credentials only in memory, and passes them to the
   `aws` command as environment variables.
3. Looking is read-only. To change something, a developer or administrator proposes the change with a reason, and
   Forgeline previews it as a CloudFormation change set.
4. A **different** administrator approves or rejects, under **Infrastructure → Cloud changes** (see
   [Home and approvals](home-and-approvals.md)). Approval runs it and writes the audit log. A change nobody decides
   within 24 hours expires.

## Sign in with AWS (IAM Identity Center)

1. Choose **Connect**, then the **Sign in with AWS** tab. Enter your **AWS access portal URL** (like
   `https://d-xxxxxxxxxx.awsapps.com/start`, from IAM Identity Center → Settings) and the **Identity Center region**.
2. Forgeline shows a short **code** and a link. Open the link in your own browser and approve.
3. The dialog continues by itself. Pick an **Account** and a **Role**, and the **Region** Forgeline should look at and
   deploy into.
4. Choose **Test and connect**.

No AWS CLI is needed for the sign-in.

{: .important }
Pick a **read-only role** if you only want to look. Changes always need a second administrator's approval, but they run
with whichever role you picked.

## Use a server profile

This uses a named profile of the AWS CLI on the machine that runs the server. Use it when the server already has
credentials (for example an instance role). Sign in on the server with `aws sso login --profile <name>`. In **Connect**,
choose the **Server profile** tab, enter the **Profile** (leave it empty to use the server's own role) and the
**Region**, then **Test and save**. **Disconnect** removes the connection.

To look at more accounts, choose **Add account**, give a **Name**, **Environment**, **Profile** and **Region**. Forgeline
checks the profile before it saves. An added account is read-only: the explorer, stacks, spend and the assistant can look
at it, and an administrator must choose **Allow changes** before people can propose changes for it. **Remove** stops
showing it and cancels its waiting proposals; nothing in AWS changes.

## Look and change

- **Stacks, instances, spend.** The tab lists CloudFormation stacks and EC2 instances. Spend loads only when you choose
  **Load last 30 days**, because AWS bills each Cost Explorer request, and needs Cost Explorer enabled and
  `ce:GetCostAndUsage` allowed.
- **Cloud changes.** Developers can use **Deploy the Forgeline demo**, **Propose deletion** on a stack, and **Start**,
  **Reboot** or **Stop** on an instance. Each asks for a reason. The preview shows exactly what would be added, changed
  or replaced. A different administrator approves or rejects it. Blueprints are proposed from the
  [cloud canvas](cloud-canvas.md).

![A proposed deploy: the reason, the parameters and exactly what would change]({{ '/console/connect-aws.png' | relative_url }})

Once connected, the **AWS account** tab also has an explorer of the account's resources, and the connected account feeds
the map. See the [Infrastructure map and services](infrastructure-map-and-services.md). It is read-only and uses the same
connection. Stack
boxes and lines on the map use two more read permissions, `cloudformation:ListStackResources` and
`lambda:ListEventSourceMappings`, which `ReadOnlyAccess` includes; without them the map says what is missing. Cloud → Map
shows the same map on its own page.

## Permissions for blueprints

### Any other AWS resource

Forgeline's own blocks and templates cover common needs. For anything else, an administrator allows resource types
under **Security → Resource types** (for example `aws_sns_topic` or `aws_route53_record`), per AWS account. On the
canvas, **Resource** then offers those types, with a form made from the AWS provider's own schema: required settings
first, the rest folded away. Settings that hold secrets are never offered (use a secret block), nor what AWS sets
itself. A resource's link fills one of its settings from another block, for example a subscription's `topic_arn` from
a topic's `arn`, or its `endpoint` from a queue's `arn`.

Forgeline chooses none of a resource's settings, so read its previews: the guardrails and any policy packs check them,
and a different administrator approves them, as for everything else. The role Forgeline builds with needs permission
for those types too (`PowerUserAccess` covers most, but not IAM).

### Permissions to build

Blueprints (Cloud → Canvas) are built with OpenTofu, which Forgeline downloads on first use and checks against the
release's published checksum (set `FORGELINE_TOFU` to use your own). Building needs the AWS permissions for what a
blueprint makes. Tables, buckets, queues and log groups are covered by `PowerUserAccess`. An app block also makes two
IAM roles, which `PowerUserAccess` does not allow; to build apps, add a policy that allows the IAM role actions listed
in [design note 035](../design/035-blueprints.md) on
`arn:aws:iam::<account>:role/fl-*` only, so Forgeline cannot change any other role.

The policy below is that permission, ready to attach to the role Forgeline builds with. It lets Forgeline manage only
roles named `fl-*`, attach to them only the AWS managed policies blueprints use (the ECS task execution policy, and
for the EKS template the four EKS ones), and hand them only to the services blueprints use (ECS tasks, Bedrock
AgentCore, and for the templates Lambda, Step Functions, Firehose and EKS), so it cannot give any role more than a
blueprint asks for. The last statement only reads one of AWS's own roles: EKS refuses to make a node group unless the
caller may look that role up (both forms are listed because IAM checks a role that does not exist yet by its bare
name). Set it once: it covers every block and template Forgeline offers, and a test keeps this list in step
with them. If an apply ever fails with `iam:PassRole`, Forgeline says which services the list must hold:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ManageBlueprintRoles",
      "Effect": "Allow",
      "Action": [
        "iam:CreateRole", "iam:DeleteRole", "iam:GetRole", "iam:UpdateRole", "iam:UpdateAssumeRolePolicy",
        "iam:TagRole", "iam:UntagRole", "iam:ListRoleTags",
        "iam:PutRolePolicy", "iam:GetRolePolicy", "iam:DeleteRolePolicy", "iam:ListRolePolicies",
        "iam:ListAttachedRolePolicies", "iam:ListInstanceProfilesForRole"
      ],
      "Resource": "arn:aws:iam::*:role/fl-*"
    },
    {
      "Sid": "OnlyListedAwsPolicies",
      "Effect": "Allow",
      "Action": ["iam:AttachRolePolicy", "iam:DetachRolePolicy"],
      "Resource": "arn:aws:iam::*:role/fl-*",
      "Condition": { "ArnEquals": { "iam:PolicyARN": [
        "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryPullOnly", "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy",
        "arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy", "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
        "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
      ] } }
    },
    {
      "Sid": "PassOnlyToBlueprintServices",
      "Effect": "Allow",
      "Action": "iam:PassRole",
      "Resource": "arn:aws:iam::*:role/fl-*",
      "Condition": { "StringEquals": { "iam:PassedToService": [
        "bedrock-agentcore.amazonaws.com", "ecs-tasks.amazonaws.com", "eks.amazonaws.com", "firehose.amazonaws.com",
        "lambda.amazonaws.com", "states.amazonaws.com"
      ] } }
    },
    {
      "Sid": "ReadTheEksNodeGroupServiceRole",
      "Effect": "Allow",
      "Action": "iam:GetRole",
      "Resource": [
        "arn:aws:iam::*:role/AWSServiceRoleForAmazonEKSNodegroup",
        "arn:aws:iam::*:role/aws-service-role/eks-nodegroup.amazonaws.com/AWSServiceRoleForAmazonEKSNodegroup"
      ]
    }
  ]
}
```

For example, as an inline policy on the role (run it yourself, with an administrator's credentials):

```sh
aws iam put-role-policy --role-name <your-forgeline-role> --policy-name forgeline-blueprint-roles \
  --policy-document file://forgeline-blueprint-roles.json
```

An app or agent with a **source repository** also needs GitHub's OIDC provider in the account, once, so GitHub Actions can
push images without stored keys:

```sh
aws iam create-open-id-connect-provider --url https://token.actions.githubusercontent.com --client-id-list sts.amazonaws.com
```

Without it, building such a blueprint fails when IAM refuses the push role, and the error says the provider does not exist.

## Security model

- **Two people.** A change needs an administrator who did not propose it. [Approval rules](approval-rules.md) can ask
  for more.
- **Whatever the role allows.** An approved change runs with the role you picked, so a read-only role cannot change
  anything.
- **Extra accounts are read-only** until an administrator allows changes for them.
- **Credentials.** The sign-in token is sealed in the vault; role credentials stay in memory.
- **Audited.** Every proposal, approval and rejection is in the [audit log](audit-log.md).

## Limits and verification

{: .warning }
**Checked against the code for this review:** the console labels, roles and the permission names in the troubleshooting
table. The IAM policy below is checked by a test against the blueprint blocks and templates. The account-adding flow and
the AWS MCP Server card were not run. **Recorded earlier:** reading a real account, the real device-code sign-in and the real credential step, and rejecting a change
on a real account. Executing an approved change was run on a real account: a deploy and a delete of the demo stack, each
approved by a second actor in the browser.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| The map says what is missing | The role lacks `cloudformation:ListStackResources` or `lambda:ListEventSourceMappings`. |
| An approval is refused | You proposed the change, or an [approval rule](approval-rules.md) names other approvers. |
| Propose buttons are missing | You are a viewer, or the account selected is an added account that has not been allowed changes. |
| Spend shows "Cost data is not available" | Cost Explorer is not enabled on the account, or the role may not call `ce:GetCostAndUsage`. |
| "Not reachable" next to the account | Forgeline could not read the account's identity; the error is shown on the page. Choose **Change connection** and sign in again. |
| "The code expired before it was approved. Start again." | You did not approve the sign-in code in time. |
| A blueprint apply fails with `iam:PassRole` | Forgeline says which services the policy in *Permissions to build* must hold. |
| Building an app or agent with a source repository fails because the OIDC provider does not exist | Create GitHub's OIDC provider in the account once (see *Permissions to build*). |

## Related

- [Runbooks](runbooks.md)
- [Alarm intake](alarm-intake.md)
- [Approval rules](approval-rules.md)
- [Cloud canvas](cloud-canvas.md)
- [Policy packs](policy-packs.md)
- [People and roles](people-and-roles.md)
- [API: AWS](../api/reference.md)
