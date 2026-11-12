---
title: Connect AWS
parent: Guides
nav_order: 5
---

# Connect AWS

Go to **Watch → Infrastructure → AWS account → Connect**. Two tabs:

## Sign in with AWS (IAM Identity Center)

1. Enter your **access portal URL** (like `https://d-xxxxxxxxxx.awsapps.com/start`, from the IAM Identity Center page) and the region of your Identity Center.
2. Forgeline shows a short **code** and a link. Open the link in your own browser and approve.
3. The dialog continues by itself. Pick an **account** and a **role**, and the region Forgeline should work in.
4. **Test and connect.**

![Code]({{ '/console/23-aws-signin-code.png' | relative_url }})
![Choose]({{ '/console/24-aws-signin-choose.png' | relative_url }})

No AWS CLI is needed for the sign-in. Forgeline keeps the sign-in token sealed in its vault and
the role credentials only in memory, and passes them to the `aws` command as environment variables.

{: .important }
Pick a **read-only role** if you only want to look. Changes always need a second administrator's approval, but they run with whichever role you picked.

## Server profile
Uses a named profile of the AWS CLI on the machine that runs the server. Use it when the server
already has credentials (for example an instance role). Sign in on the server with `aws sso login`.

## Looking and changing

- **Stacks, instances, spend.** Spend loads only when you ask, because AWS bills each Cost Explorer request.
- **Cloud changes.** A developer or admin proposes a change (deploy a template, or start, stop or reboot an instance) with a reason. Forgeline previews it as a CloudFormation change set. A **different** administrator approves or rejects. Approval runs it and writes the audit log.

![Preview]({{ '/console/23-deploy-preview.png' | relative_url }})
![Done]({{ '/console/25-change-succeeded.png' | relative_url }})

**Verified:** reading a real account, the real device-code sign-in and the real credential step, and rejecting a change on a real account. Executing an approved change was run on a real account: a deploy and a delete of the demo stack, each approved by a second actor in the browser. Design: [note 009](https://github.com/forgelinehq/forgeline/blob/main/docs/design/009-aws-signin.md).

## What you see once connected

The **AWS account** tab shows the account's resources as a map, grouped by VPC, with a list of what needs a look. See the [Console tour]({{ '/guides/console-tour/' | relative_url }}#infrastructure). It is read-only and uses the same connection. Stack boxes and lines on the map use two more read permissions, `cloudformation:ListStackResources` and `lambda:ListEventSourceMappings`, which `ReadOnlyAccess` includes; without them the map says what is missing. Watch › Map shows the same map on its own page.

## Building blueprints

Blueprints (Build › Design) are built with OpenTofu, which Forgeline downloads on first use and checks against the
release's published checksum (set `FORGELINE_TOFU` to use your own). Building needs the AWS permissions for what a
blueprint makes. Tables, buckets, queues and log groups are covered by `PowerUserAccess`. An app block also makes two
IAM roles, which `PowerUserAccess` does not allow; to build apps, add a policy that allows the IAM role actions listed
in [design note 035](https://github.com/forgelinehq/forgeline/blob/main/docs/design/035-blueprints.md) on
`arn:aws:iam::<account>:role/fl-*` only, so Forgeline cannot change any other role.

The policy below is that permission, ready to attach to the role Forgeline builds with. It lets Forgeline manage only
roles named `fl-*`, attach only the ECS task execution policy to them, and hand them only to ECS tasks and Bedrock
AgentCore, so it cannot give any role more than a blueprint asks for:

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
      "Sid": "OnlyTheEcsExecutionPolicy",
      "Effect": "Allow",
      "Action": ["iam:AttachRolePolicy", "iam:DetachRolePolicy"],
      "Resource": "arn:aws:iam::*:role/fl-*",
      "Condition": { "ArnEquals": { "iam:PolicyARN": "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy" } }
    },
    {
      "Sid": "PassOnlyToEcsAndAgentCore",
      "Effect": "Allow",
      "Action": "iam:PassRole",
      "Resource": "arn:aws:iam::*:role/fl-*",
      "Condition": { "StringEquals": { "iam:PassedToService": ["ecs-tasks.amazonaws.com", "bedrock-agentcore.amazonaws.com"] } }
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
