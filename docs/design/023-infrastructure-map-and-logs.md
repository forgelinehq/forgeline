# 023: Infrastructure map and logs

Status: **Order of work steps 1 (the map) and 2 (alarms) are built; logs and metrics are design only.** Date: 2026-11-01.
Builds on notes 006 (console), 015 (AWS access for the assistant), 017 (more than one account) and
022 (console redesign, step 2). Facts about the code are from `origin/main` at `f927cad`.

## Question

The Infrastructure page lists what is in an AWS account but cannot show how it is doing or what it
is saying. People want to open a service, see whether it is healthy, and read its recent logs
without leaving the console. How do we add that without reading more of an account than we can
defend?

## What exists today

- `GET /v1/infra/aws/resources?account=` already returns every resource per service (id, name,
  state, ARN, labelled fields, tags), cached for 60 seconds, at most 200 per service, with an error
  per service when it could not be listed. A map can be built from this with **no new API**.
- Forgeline's own step logs stream to the browser over server-sent events (`stream.go`, `lib/logs.js`)
  and are masked by exact match against known secret values (`internal/redact`).
- Note 015 deliberately refuses `call_aws` operations that read **log events and query results**.
  So reading CloudWatch log content is a new capability, not a small extension.
- Cloud changes only happen through proposal and approval (note 006). Nothing here changes that;
  this note is read-only.

## Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| Shape of the map | **Grouped, not a dependency graph.** Resources are shown by category and service with their state. No lines are drawn between them in the first version | `describe` and `list` calls do not say what talks to what. A graph with guessed lines would be wrong in ways nobody could see. Lines are added later only where AWS states the link (a load balancer's target group to an ECS service; a Lambda event source to its queue) |
| Health | From CloudWatch alarms (`describe-alarms`, a read operation) matched to resources by their dimensions. A resource with no alarm shows "no alarms", never "healthy" | We should not claim health we did not measure |
| Metrics | Separate, later (`get-metric-data`). Sparklines need it | Keeps the first version small |
| Logs endpoint | New `GET /v1/infra/aws/logs?account=&group=&since=&filter=&limit=&after=`. The server builds the `aws logs filter-log-events` call itself from validated values. No free-form command | The model never writes the command, so the rules in note 015 about command text do not apply; the rules below do |
| Live tail | The browser repeats the request with the `after` cursor every 3 seconds while the log panel is visible, and stops when the tab is hidden or the person leaves the page | No new streaming API, and it works behind any proxy |
| **Which log groups can be read** | **An allowlist per account, set by an administrator under Integrations. The default is empty: no logs.** An administrator picks groups from `describe-log-groups` (names only) | Logs can hold personal data and secrets that no masking will catch. The allowlist is the boundary; masking is not |
| Who can read | Developers and administrators, only on allowlisted groups. Viewers cannot. **Needs a decision**, see below | |
| Masking | Credential-shaped text is replaced with `***` on the server before it leaves: AWS access keys, private key blocks, GitHub and Slack tokens, `Authorization` header values, JWTs. The panel says it is best effort and lists what is masked | Same honest limit as note 005 T3: it limits accidents, it is not a boundary |
| Limits | 30 seconds per call; at most 500 events per call (default 200); each message cut at 4 KB; whole response cut at 256 KB; window at most 24 hours (default 15 minutes); filter pattern at most 200 characters; 30 reads a minute per person | Bounds cost and memory |
| Audit | Every read records who, account, group, window, the length of the filter and the number of events. Never the text. Allowlist changes are recorded | Matches how the assistant is audited (note 008) |
| The assistant | **Not wired.** `call_aws` still refuses log events. The "Ask agent" button sends no log text | Log lines are untrusted input. Whether the assistant may read them is its own decision (see note 024) |

## Decisions made (2026-11-01)

1. **Who can read logs:** developers and administrators, only on groups an administrator allowlisted.
   (The stricter option was administrators only, like `call_aws`.)
2. **Where the allowlist lives:** on the AWS account record (note 017), so each account has its own list.

## IAM

The console's AWS role needs `logs:DescribeLogGroups`, `logs:FilterLogEvents` and
`cloudwatch:DescribeAlarms`. **Unverified:** whether the read-only role created by note 009 already
includes them. Check the current AWS documentation and the role before building. If a permission is
missing, the panel must say which one, per group, instead of showing an empty list.

## Order of work (each its own pull request)

1. **Map from the inventory (built).** A Map view is the default in the AWS explorer, with the old list
   as a switch. Resources sit inside the VPC their own `VPC` field names (a placeholder container when that
   VPC is not in the VPC list); the rest are grouped by category and labelled as not placed. A strip lists
   what needs a look, from state and fields only: failing or changing state, a security group open to the
   internet, an unattached or unencrypted volume, a publicly accessible or unencrypted database. A stopped
   instance is not flagged. A side panel shows the resource's detail. Kubernetes keeps the list. No new
   endpoint. **Not done:** links to Guard findings for a resource (Guard's findings are not keyed by
   resource in the inventory; a later change).
2. **Alarms and health (built).** `GET /v1/infra/aws/alarms` makes one `cloudwatch describe-alarms` call (viewer
   role, at most 500 alarms, ALARM first). The console matches an alarm to a resource only when the alarm's
   namespace and its own dimensions name it: EC2 `InstanceId`, EBS `VolumeId`, RDS `DBInstanceIdentifier`, Lambda
   `FunctionName`, DynamoDB `TableName`, SQS `QueueName`, S3 `BucketName`, load balancers by the ARN suffix, ECS
   services by cluster and service name, ECS clusters by cluster name. An alarm in ALARM flags the resource;
   "no data" and OK flag nothing. A resource with no alarm shows "no alarm names this resource", never
   "healthy". If the role cannot call `cloudwatch:DescribeAlarms` the console says so and the map still works.
   **Not done:** composite alarms (the default call returns metric alarms only), alarms on metric math
   expressions (no dimensions, so they match nothing), and a list of all alarms apart from the resources.
3. **Logs.** The endpoint, the allowlist, masking, audit and the log panel.
4. **Metrics.**

## Tests to write with step 3

Go, with the race detector, using a fake CLI (extend `scripts/dev/fake-aws` with `logs` and
`cloudwatch`): arguments are built only from validated values; a group not on the allowlist is
refused for everyone; a viewer is refused; each limit; each masking pattern; the audit entry holds no
log text; the cursor does not repeat or skip events. Then remove each rule in turn and confirm a test
fails, as the repository requires.

## What was checked, and what was not

Checked: the facts in "What exists today", by reading the code. Nothing is built and no AWS call was
made.

Not checked: CloudWatch's paging and size behaviour for `filter-log-events`, the IAM question above,
and how the pattern masking behaves on real log lines. Verify these before step 3.

## Not in this note

Kubernetes pod logs, Logs Insights queries (note 015 refuses query results; it needs its own
decision), searching across groups, and alerting.
