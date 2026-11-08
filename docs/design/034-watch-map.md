# 034: Watch map: stacks and the links AWS states

Status: **Built (2026-11-08).** Frame 6 of the confirmed screen frames (note 029). Builds on note 023 (the
AWS map from the inventory, and alarms).

## Question

The approved Watch frame shows the cloud as a map: resources inside the stack that made them, lines for
what uses what, and a side panel that says what is wrong and what to do. Note 023 drew no lines because
`describe` and `list` calls do not say what talks to what. Which links can we draw without guessing?

## Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| Page | Watch › **Map** (`#/map`), the Watch space's first tab. The same map is still the AWS tab of Infrastructure | The frame puts the map first; old links keep working |
| Accounts | A switch at the top lists the accounts (note 017). The choice is shared with Infrastructure › AWS | One click between accounts, as in the frame |
| Stacks | `cloudformation list-stack-resources` for each stack the inventory found (at most 25 per account; the rest are counted). A listed resource whose type and physical id match a part sits inside its stack's dashed box, not inside its VPC | CloudFormation states exactly what a stack made. A VPC a stack made stays a box of its own |
| What is matched | Type to service: DynamoDB tables, Lambda functions, S3 buckets, EC2 instances, volumes, security groups, ECR repositories, RDS databases, SQS queues, SNS topics, log groups, load balancers, ECS clusters and services. The physical id must equal the resource's id, name or ARN. Parts of other types (roles, policies, alarms) are counted under the box | No match by similar names or tags |
| Deleted and kept parts | A part CloudFormation deleted is left out. A part it kept (`DELETE_SKIPPED`) is shown in the box with a note that the stack no longer manages it | A rolled-back stack often leaves tables behind; people need to see them and know why |
| Lines | Only Lambda event source mappings (`lambda list-event-source-mappings`): a function and the queue or table stream it reads. A source the map does not list (a Kinesis stream) is named by its ARN in the panel and gets no line. A disabled mapping is dashed | It is the one "uses" link AWS states in a single read call. Load balancer targets, VPC endpoints and IAM are later, each from its own stated source |
| Drawing | An SVG under the tiles, drawn after layout and again on resize. Curves leave each tile on its left edge so they never cross a name. The selected resource's lines are highlighted. No moving dots: traffic is not measured | The frame's "dots on lines that carry live traffic" would need metrics (note 023 step 4) |
| Side panel | Made by which stack; Uses and Used by (each a link to that resource); for a stack, what it made by type and what its state means in plain words (a rolled-back stack cannot be updated, only deleted and deployed again). A flagged resource lists why, and its button reads **Ask the assistant to fix it** | The button fills in the question; the person sends it, and any fix is a proposal someone approves |
| Not built from the frame | **Open logs** (note 023 step 3 is not built), **Redesign it in Build** (the design canvas, frame 4), cost per resource | Each needs its own feature first |
| API | `GET /v1/infra/aws/links?account=` (viewer), cached for a minute like the inventory. Read-only. A refused part names the permission | Same shape as alarms |

## IAM

Two read permissions: `cloudformation:ListStackResources` and `lambda:ListEventSourceMappings`. Both are in the
AWS managed `ReadOnlyAccess` and `PowerUserAccess` policies. Without them the map still works, without stack
boxes or lines, and says what is missing.

## Tested

Server (fake CLI): the deleted stack is not read, deleted parts are left out, a refused stack and a refused
mapping list name the permission, function and source ARNs are read correctly (aliases, table streams, layer
ARNs and non-table DynamoDB ARNs), at most 25 stacks, a second look within the minute costs no AWS call, only
read calls are made. Node: parts are placed by exact type and id, never a VPC; a stack part sits in its stack
and not its VPC; the stack is its box and a stack not read stays a tile; a search hides an empty stack; uses
and used by from both sides; lines only between listed resources; the stack state words. Each rule was removed
in turn and a test failed (14 of 14). Browser: the fake AWS account (a rolled-back stack with a function
reading a queue).
