# 040: Any resource, any cloud: templates, a generic block, guardrails and a library

Status: **Steps 3 and 4 built (2026-11-16); see below.** **Step 1 built (2026-11-09): built-in guardrails on every blueprint preview (`internal/guardrail`, rule set `builtin-1`).**
**Step 2, first part, built (2026-11-12): the AgentCore agent as a block** (an `agent` block, not yet a template in the
template format). It runs an ECR image on Bedrock AgentCore Runtime with the execution role AWS documents, scoped to the
blueprint's region and account, its own image and its own workload identity, never `GetWorkloadAccessTokenForUserId`, plus
the tables, buckets, queues and secrets it is linked to. Guardrails `builtin-2` allow `Resource: "*"` only for statements
whose every action IAM cannot scope (ECR token, X-Ray writes, CloudWatch metrics). Still to do in step 2: the template
format itself, AgentCore memory and gateway, and a real build (the Pi's role cannot create IAM roles until the `role/fl-*`
policy is added).
**Step 4, first part, built (2026-11-12): a source repository on app and agent blocks.** The blueprint then makes a
private ECR repository (immutable tags, scanned, the 30 newest images kept) and a push role that only GitHub Actions in
that repository and branch can take through GitHub's OIDC provider, able to push to that one repository. Forgeline
suggests the workflow (actions pinned to commit SHAs; arm64 for agents) for a person to add by pull request, and a
Forgeline pipeline that starts it; running a new image stays a previewed, approved blueprint change. **Built 2026-11-16: moving a block to a newly pushed
image in one step** ("Run a new image…": the images in the block's own repository, newest first; picking one saves the
blueprint with it and starts the preview; only that repository's images are accepted; checked on real AWS,
[2026-11-16](../testing/2026-11-16-run-new-image.md)). **Built 2026-11-16: which repository fits a block** (see
"Suggesting a repository" under layer 3). Extends notes 035 and 039. Builds in the order under "Plan".

## Question

Hand-made blocks (note 039) cover a web app. People also want to deploy an agent to Bedrock AgentCore with what it
needs, an EKS cluster with several namespaces, Lambda and Step Functions, ECS clusters with many services, data
pipelines, code pipelines connected to their repositories, and the same on Azure, Google Cloud, RunPod and others. A
hand-made block per resource does not scale: AWS alone has well over a thousand resource types. How does Forgeline
reach "anything" without giving up what makes it safe (plain words, a preview, a second person approving, nothing
secret in Forgeline)?

## What exists to build on

- **Provider schemas.** `tofu providers schema -json` describes every resource of every provider: each attribute, its
  type, whether it is required, computed or sensitive. A form, a validator and the assistant's instructions can be
  generated from it rather than written by hand. ([OpenTofu docs](https://opentofu.org/docs/cli/commands/providers/schema))
- **Providers for every target.** The AWS provider (Forgeline already pins `~> 6.72`) has AgentCore
  (`aws_bedrockagentcore_agent_runtime` and about twenty more), EKS, Lambda, Step Functions, Glue, CodePipeline. AzureRM,
  Google and RunPod (`runpod/runpod` 2.x: pods, serverless endpoints) providers exist too.
  ([AgentCore resource](https://registry.terraform.io/providers/hashicorp/aws/6.38.0/docs/resources/bedrockagentcore_agent_runtime),
  [AgentCore with Terraform](https://dhrimov.dev/blog/deploy-bedrock-agentcore-terraform), [RunPod provider](https://docs.runpod.io/integrations/terraform/overview))
- **AWS Cloud Control API.** One create/read/update/delete/list interface, with a JSON schema, for every resource type
  in the CloudFormation registry. Useful for **reading** anything (the Watch map, the Built check, adopting what
  exists), not as a second way to change things: changes stay in OpenTofu plans. ([supported types](https://docs.aws.amazon.com/cloudcontrolapi/latest/userguide/supported-resources.html))
- **Policy as code on the plan.** The common pattern is to turn the plan into JSON and test it before apply: OPA and
  Conftest for an organization's own rules (Rego), Checkov or Trivy for broad sets of known misconfigurations.
  Plan-time checks are the gate that is hard to skip. ([OPA with OpenTofu](https://oneuptime.com/blog/post/2026-03-20-open-policy-agent-opentofu/markdown),
  [IaC security tools](https://env0.com/blog/top-infrastructure-as-code-security-tools), [Checkov with OpenTofu](https://oneuptime.com/blog/post/2026-03-20-checkov-policy-scanning-opentofu/markdown))

## Decisions

Four layers. Each wider layer is used only where the narrower one does not reach, and **every layer ends in the same
OpenTofu plan, the same guardrails and the same approval.**

### 1. Blocks (what exists today)

Hand-made, plain words, safe settings that are not options. They stay the first thing people see and grow slowly.

### 2. Templates: whole solutions as one block

A **template** is a reviewed, versioned bundle:

| Part | What it is |
| --- | --- |
| `template.json` | Name, plain-words summary, the cloud and providers (with pinned versions and checksums), and its **inputs as JSON Schema**, from which the canvas draws the form |
| `main.tf.json` (or `.tf`) | The OpenTofu code, taking only those inputs; no `local-exec`, no external data sources, no providers other than those declared |
| `checks.json` | The plain-words checks it adds ("the agent can call only these tools"), shown like a block's |
| `diagram.json` | How its resources draw: which resource is the node, what links it offers (an AgentCore agent offers "calls a tool", "reads a secret") |
| `guardrails` | Rules the template promises to pass (layer 4), tested in its CI |
| tests | Real `tofu validate` and a plan against fakes, like `TestTofuValidates` |

On the canvas a template is one block (or a group box that opens) with links to ordinary blocks, so "an AgentCore agent
that reads this table and this secret" is drawn the same way as an app.

First-party templates, in this order, each its own pull request with a real preview on the Pi:

1. **AgentCore agent**: runtime from a container image in ECR or from code, its IAM role scoped to what it is linked to,
   an endpoint, a gateway to linked tools, memory if asked, logs.
2. **Lambda function**: from a repository or an image, with triggers (queue, schedule, front door path, bucket event).
3. **Step Functions workflow**: drawn from linked Lambda functions and services; the definition is generated from the
   links and editable as JSON.
4. **ECS cluster with many services**: what note 039 does, plus a shared cluster block, capacity providers and service
   connect.
5. **EKS cluster with namespaces**: the cluster and node groups in one apply; namespaces, quotas and network policies
   in a **second stage** that uses the Kubernetes provider against the new cluster (two stages in one approval, applied
   in order).
6. **Data pipeline**: Kinesis or SQS in, Glue or Lambda transform, S3 out, Athena tables and a Glue catalog.
7. **Code pipeline**: CodeBuild and CodePipeline, or (preferred) a Forgeline pipeline that builds an image and updates
   a service (layer 3).

#### The template format (decided 2026-11-14)

A template is a directory `<name>/<version>/` with three files. First-party templates are compiled into Forgeline
(`internal/template/library`); the library (layer 7) adds reviewed, checksummed third-party ones later.

- **`template.json`**: `name`, `version` (semver), `title`, `summary`, `cloud`, `providers` (each must equal the
  version Forgeline pins: a template cannot bring its own provider), `holds_data` (whether removing it loses data), the
  plain-words `checks` it adds (with `{input}` placeholders), and `inputs`, a JSON Schema subset the canvas draws as a
  form and Forgeline checks before any code is generated: `string` (`pattern`, `maxLength`, `enum`), `integer` and
  `number` (`minimum`, `maximum`, `enum`), `boolean`, a map of strings (`additionalProperties` a string schema,
  `maxProperties`, `propertyNames.pattern`), `default`, `title`, `description`, `required`. Any other keyword makes the
  template invalid, so the form never leaves out a rule the code relies on.
- **`diagram.json`**: how the block draws (`icon`) and the **link roles** it offers. Each role names its direction
  (out: the template uses the other block; in: the other block drives it), the block kinds it accepts, whether it takes
  read or read-write access, and the module variable that receives the linked blocks. The variable's shape is fixed by
  the role (`data`: id, kind, ARN, value, environment name, access; `secret`: id, ARN, environment name; `source`: id,
  ARN), so the generator, not the template, says what a link points at, and the reviewed template code decides which
  IAM actions it grants.
- **`main.tf.json`**: an OpenTofu module. Its variables are exactly the inputs, the link variables and `name` and
  `tags`. It may not declare providers or backends, call other modules, use provisioners, use resources or data sources
  of a provider it does not declare, or read secret values (`aws_secretsmanager_secret_version`, `aws_ssm_parameter`
  and similar). These are checked when the template loads, and tested by breaking them.

The blueprint calls a template block as a module (`module.<block>`). The module files are stored with the plan, next to
`main.tf.json`, so an approved plan applies exactly the code it was planned with. Every plan still goes through the
built-in guardrails.

Built 2026-11-14: the format and checks (`internal/template`), template blocks in blueprints (module calls, links by
role, module files stored with the plan), `GET /v1/templates`, and the canvas (a button per template, a form drawn
from its inputs, links by role). Checked on real AWS ([2026-11-15](../testing/2026-11-15-lambda-template.md)): a queue
triggers the Lambda template, which writes to its linked table; built, used and removed through approvals. The Step
Functions workflow template too ([2026-11-16](../testing/2026-11-16-step-functions-template.md)): it ran two linked
functions in order.

**Links between templates (decided 2026-11-15).** A link role may accept another template's blocks, named
`template:<name>`, with the shape `target`: the generator passes each linked block's id and the ARN from the output
the role names (`output`), read from that template's module (`module.<block>.<output>`). The role's template must be in
the library and declare that output; the check refuses anything else. Template blocks still never link to each other
otherwise.

The second template is **Step Functions workflow** (`aws-step-functions-workflow`): it runs the Lambda function blocks
it is linked to, in the order of the links, each step retried with backoff. Its definition is generated, not typed:
taking a person's own Amazon States Language with the functions' ARNs filled in would need OpenTofu's `templatestring`,
which can also call functions such as `file()` on the server while planning. A schedule and execution logs wait too:
both need a role passed to another service, and Step Functions' log delivery needs actions IAM only allows on `*`.

**ECS cluster (decided 2026-11-16, maintainer).** Blueprints already run all app blocks on one shared cluster, so the
"ECS cluster with many services" template became a setting on app blocks instead: Fargate Spot, alone or behind one
regular task. Service Connect is left out: apps already call each other by name through the blueprint's private DNS
namespace, and Service Connect would add a proxy to every task mainly for metrics and retries. Built and checked on real
AWS (#137, [2026-11-16](../testing/2026-11-16-step-functions-template.md#fargate-spot-137-same-day)).

**The data pipeline template and writer links (step 5, first part; decided 2026-11-16).** `aws-data-pipeline` takes
records apps send it, through Amazon Data Firehose (optionally passed through a linked Lambda function template that
reshapes them), and stores them as JSON in a **linked bucket block**, under `<name>/year=…/month=…/day=…/`. A Glue
catalog table over that prefix (columns from an input, partitions found by projection, so no crawler runs) and an
Athena workgroup (results in the same bucket, a limit on the data one query may scan) make it queryable. Choices:

- **The data lives in an ordinary bucket block, not in the template.** Removal keeps data by block kind (note 041): a
  bucket block is protected and kept by "keep the data", while a template's own resources cannot carry
  `prevent_destroy` per blueprint. So a template that would hold data links to the block that holds it.
- **Link roles may say how many links they need** (`min`, `max`): the pipeline needs exactly one bucket and at most one
  transform. Validation refuses a blueprint with fewer or more.
- **Writer links: an app sends data into a template.** A role with the new shape `writer` (direction in, kinds
  `service`) names the actions an app gets (`actions`, no wildcards, no IAM, STS, KMS or Organizations actions), the
  template output holding the ARN they apply to (`output`), and the output whose value the app gets as an environment
  variable (`env_output`, named `<BLOCK>_<env_suffix>`, such as `EVENTS_STREAM`). The output named in `output` must be
  the `.arn` of a resource the template itself declares, checked when the template loads, so a template cannot hand an
  app rights on `*` or on something outside itself. The generator adds exactly that statement to the app's task role.
  This is a trust-model change: a reviewed template now decides a few actions in an app's role, always on its own
  resource. It is mutation-tested.
- **Firehose needs a role passed to it**, so `firehose.amazonaws.com` joins the setup guide's PassRole list (an IAM
  change on existing installations, which the error hint names).

Built (#143) and checked on real AWS ([2026-11-16](../testing/2026-11-16-data-pipeline.md)): an app's own role sent records
and was refused an action the template does not grant; Athena returned them by day; everything was removed.

**EKS with namespaces (step 5, second part; decided 2026-11-16).** `aws-eks-cluster` makes an EKS cluster in the
default VPC (its public subnets, so no NAT gateway to pay for), a managed node group, and in each namespace a person
names: a resource quota, default container requests and a network policy that lets pods take traffic only from their
own namespace (the VPC CNI's network policy agent enforces it). People reach the cluster with AWS IAM only: an optional
administrator role, and per namespace an optional role that may edit only that namespace (EKS access entries).
Choices:

- **Two approved steps, not one.** The namespaces are Kubernetes objects: OpenTofu creates them through the Kubernetes
  provider, which needs the cluster's address and certificate. Before the cluster exists, a plan cannot contain them,
  so "two stages in one approval" (above) would have applied a second plan nobody saw. Instead, the first preview
  builds the cluster and nodes. Once they exist, the next preview adds the namespaces, and it is approved like any
  other. This is how removing everything already works when something is protected (note 041). The template's checks say so.
- **The template format learns a second stage**: `template.json` may name its cluster (`"kubernetes": {"cluster":
  "aws_eks_cluster.this"}`) and then may use the pinned `hashicorp/kubernetes` provider. Forgeline, not the template,
  configures that provider, one per template block (`provider "kubernetes" { alias = "<block>" }`). Until the cluster is
  in the blueprint's state, the provider is empty and the module gets `cluster_ready = false`. Once it is there, the
  provider reads the cluster by name (a data source) and signs in with `aws eks get-token` when the plan applies.
  A token fetched while planning would have expired by the time someone approves. When the template loads, Forgeline
  checks: the cluster is named `var.name` (so Forgeline knows it); every Kubernetes resource is created only when
  `var.cluster_ready` is true and depends on the cluster (so removal deletes the namespaces before the cluster);
  Kubernetes secrets cannot be read.
- **Removal keeps working.** When a removal drops the block, Forgeline still configures that provider from the
  cluster's name while the cluster is in the state. OpenTofu can then delete the namespaces first.
- **AWS managed policies a template attaches are listed**, like the services it passes roles to: `attaches_policies`
  names exact `arn:aws:iam::aws:policy/…` ARNs. The template may attach no others, and never one of the broad ones. The
  setup guide's attach statement lists them all, and a test keeps the two in step. EKS needs four:
  `AmazonEKSClusterPolicy`, `AmazonEKSWorkerNodePolicy`, `AmazonEKS_CNI_Policy` and
  `AmazonEC2ContainerRegistryPullOnly`. Copying them into inline policies would leave stale copies when AWS updates
  them.
- **Cost guard**: the cluster stays on standard support (`upgrade_policy`). Extended support costs six times as much
  an hour, and a cluster left behind on an old version would switch to it silently. The check text names the price.
- **The code pipeline template is not built.** Step 4 already builds an app's image from its repository with GitHub
  Actions and lets a person run a new image through preview and approval. A CodePipeline template would be a second
  way to do the same thing.

Built (#145) and checked on real AWS ([2026-11-16](../testing/2026-11-16-eks-namespaces.md)): the cluster, then its
namespaces in a second approved preview; a pod in another namespace could not reach a pod that one in the same namespace
could; everything was removed. The check found one more permission the builder role needs (reading AWS's EKS node group
service role), now in the setup guide.

The first template is **AWS Lambda function** (`aws-lambda-function`), chosen by the maintainer because it can be built
on real AWS now: a container image from ECR, its own log group and role, a schedule, queues that trigger it, and the
tables, buckets, queues and secrets it uses. The AgentCore agent template follows once the account's AgentCore quota is
raised.

### 3. Repositories linked to what runs them

A service, function or agent block can take a **source**: a GitHub repository (from the repositories the GitHub App may
read), a path and a branch. Forgeline then:

- suggests a pipeline (build, test, push the image to a registry the blueprint makes, deploy by updating the block's
  image through a proposal), using the read-only repository lookups the assistant already has;
- suggests which repository fits a block by reading Dockerfiles and manifests, and says why;
- never pushes to the repository; changes to it are pull requests a person merges.

**Suggesting a repository (decided 2026-11-16).** No model reads the repositories: Forgeline parses them. For each
repository on the GitHub allow list it reads the file tree of the default branch (or, when the allow list does not let
that branch run, the first branch the list names outright) and up to 5 `Dockerfile`s per repository, 25 in all, at most
4 folders deep, skipping `node_modules`, `vendor`, `testdata`, `third_party`. From each it takes facts only: the ports
it exposes, whether the final stage starts from an AWS Lambda base image, whether it builds for arm64, whether it
installs the Bedrock AgentCore or Strands Agents SDK, and which manifests sit beside it (the language). A folder is scored
against the block: an app wants its port and not a Lambda image; an agent wants the SDK, port 8080 and arm64; a Lambda
function wants a Lambda base image; words of the block's name in the repository name or path add to it. Each suggestion
says why in fixed sentences; the repository's own text is never returned, so a hostile Dockerfile cannot put words in
the console. Using one fills in the block's Code fields, and nothing is saved until the person saves. Only files named
`Dockerfile` count, because the suggested build uses the folder as its context with the default file name.

### 4. Guardrails on every plan

Before any plan can be sent for approval, Forgeline turns it into JSON (`tofu show -json plan.bin`) and tests it:

- **Built-in rules** (Go, always on, mutation-tested): nothing open to the internet except what a front-door-like block
  declares; storage and databases encrypted; no `*` actions or resources in IAM policies; no IAM users or access keys;
  no deleting what holds data without the high-risk approval (note 032); only allowed regions, providers and resource
  types.
- **Policy packs** an organization adds: Rego run by Conftest, and Checkov or Trivy rule sets. They run **as containers**
  (principle 3), with no network, on the plan JSON only (never the state, never a secret).
- Results show on the preview: **deny** stops Send for approval; **warn** is shown to the approver, who must read it.
- The approver sees which pack version passed, and it is recorded in the audit log.

**Policy packs, as built (step 6).** The three tools were run on real plans first (a 57-resource web stack and the EKS
cluster, planned on the maintainer's Raspberry Pi), each as a container with no network, a read-only root, no
capabilities and 256 MB:

| | Conftest 0.71.1 | Trivy 0.76.2 | Checkov 3.4.5 |
|---|---|---|---|
| Image (arm64) | 91 MB | 186 MB | 579 MB |
| Time on the web stack | under 1 s | about 2 s | 6 to 9 s |
| Rules it brings | none (your own Rego) | yes, each rated low to critical | yes, more of them, **not rated offline** |
| Findings, web stack / EKS | (own rules) | 11 / 7 | 29 / 6 |
| Names the plan's resource | whatever the rule returns | not inside a module (`type.name_<hash>`) | yes |

So the choice the note left open is: **no pack is on by default; Trivy is the pack to turn on for known
misconfigurations; Conftest is for an organization's own rules; Checkov is offered for those who want its wider set.**
Trivy rates its findings, so an administrator can say "deny from critical" in one setting; it is small and quick, and
pipelines already use it for images. Checkov's ratings come only from its vendor's service, which a container with no
network cannot reach, so each of its findings is a warning unless the administrator lists the rule as one that denies.
Nothing is on by default because both tools flag choices Forgeline's own blocks make on purpose (a public load
balancer, logs kept less than a year, AWS-owned encryption keys): an organization should decide which of those it
denies or skips, and both lists are settings of a pack.

- **Settings › Policy packs** (administrators change, developers read). A pack has a name, a tool, on or off, and:
  for Trivy a severity from which it denies; for Trivy and Checkov, rule ids that always deny and rule ids to skip; for
  Conftest, up to 10 Rego files (a `deny` rule denies, a `warn` rule warns, and a rule may return `{msg, address}` to
  name the resource). Changes are audited; Rego text is counted in the audit entry, not copied.
- **What a pack is given.** `tofu show -json` prints sensitive values in plain text and carries the whole earlier
  state. The pack gets a copy with only the planned values, the resource changes and the configuration, in which every
  value OpenTofu marks sensitive is replaced; variables, output changes and the earlier state are left out. Checkov and
  Conftest report the same on the copy as on the full plan; Trivy differed by one finding on the web stack.
- **How a pack runs.** `docker run` with the image named by digest (the same bytes on amd64 and arm64), `--network
  none`, `--read-only` with a small `/tmp`, `--cap-drop ALL`, `no-new-privileges`, the server's own user, 1 GB of
  memory, 256 processes, four minutes, and the copy of the plan mounted read-only. Each tool is told not to fetch rule
  updates. Docker is the only way to run a pack; there is no in-process fallback (principle 3).
- **Never a silent pass.** A pack that cannot start, runs out of time, or prints something Forgeline cannot read is a
  **deny** named `pack-failed`, with the tool's last words. A preview records when the packs were last changed, and
  Send for approval refuses a preview checked by other packs than today's.
- **What people see.** Findings join the built-in ones (`trivy:AWS-0040`, `checkov:CKV_AWS_58`, `conftest:<pack>`),
  denies first; under them, "Checked by built-in rules builtin-2; Broad (trivy 0.76.2, 1 skipped)". The approver's note
  names each pack, tool version and warning count. Trivy's module names are matched back to the plan's addresses when
  exactly one fits. A pack shows at most 100 findings and counts the rest.

Not built: choosing another registry for the tool images (an organization that cannot reach Docker Hub must mirror
them under the same digests); packs per cloud account; Trivy's or Checkov's own custom rule formats; packs from the
library (step 8).

### 5. The generic block: any resource of an allowed provider

For what no template covers, an administrator can allow **resource types** (per cloud account): for example
`aws_sns_topic` or `aws_cloudfront_distribution`. A **resource** block then takes any allowed type, and its form is
generated from the provider schema (required fields first, sensitive fields refused: a value that must be secret comes
from a secret block). Its links are references to other blocks' attributes, chosen from a list. Guardrails apply as
everywhere. It is drawn with the type's icon and named in plain words from the schema's description. This is the
"anyone can create any kind of resource" path, with the administrator deciding how wide "any" is.

**As built (step 7).**

- **The schema.** `tofu providers schema -json` with the provider blueprints pin (`hashicorp/aws ~> 6.72`) prints
  17 MB for 1,766 resource types, in about 6 seconds and 570 MB of memory. Forgeline makes it the first time someone
  needs it, keeps a cut-down copy in the data directory (types, settings and nested blocks, with which are required,
  computed, sensitive or write-only), and makes it again after 30 days or when the pin changes. No AWS credentials
  are used. The AWS provider gives no descriptions (6 of 1,766 types have one), so a type is named from its own words
  ("aws_sns_topic" is "SNS topic") and drawn with one icon.
- **The allow list.** Settings › Resource types, per AWS account; administrators change it, developers read it, every
  change is audited as "allows ... / no longer allows ...". Some types can never be allowed: IAM users, groups, access
  keys and other long-lived credentials (the guardrails deny them anyway), and `aws_organizations_*`,
  `aws_account_*` and `aws_iam_account_*`.
- **The block.** `kind: resource`, `resource_type`, `attributes`. Every setting is checked against the schema when the
  blueprint is saved, checked or previewed: it exists, it may be set (not computed only), it is never sensitive or
  write-only (a secret belongs in a secret block), its value has the type's shape, nested blocks are within their
  counts, and every required setting is set or linked. `region` cannot be set: a blueprint builds in its target
  region. Everything typed is escaped, keys included, so no value becomes an OpenTofu expression. A resource named
  like a part another block makes (`aws_ecs_cluster.main`) is refused, not merged.
- **Links.** A link from a resource block fills one of its settings that takes text (or adds to a list of text) with
  an attribute of the other block: any attribute of another resource block, or `arn`, `name`, `url` or `dns_name` of
  a queue, table, files, logs, secret, front door or existing block. Nothing links *to* a resource block yet: an app
  that publishes to a topic would need IAM permissions Forgeline cannot derive from an arbitrary type.
- **What stays the same.** The guardrails and policy packs check the plan, a different administrator approves it, and
  removal works as for other blocks: keep-data keeps a resource whose type holds data (the guardrails' list), and step
  1 of removing everything turns off the protection of a bucket, table or database made this way.
- A build preview is refused once a type is taken off the allow list; a removal preview never is.

Not built: links into a resource block; icons per service; resources of other providers; the assistant drafting
resource blocks (it may, but only with types already allowed, and the same checks apply).

### 6. Clouds: a connection per cloud, the same model

`target` becomes `{cloud, connection, region}`. A **cloud connection** says how OpenTofu gets credentials without
Forgeline storing long-lived keys where the cloud allows it:

| Cloud | How it signs in | Notes |
| --- | --- | --- |
| AWS | profile or IAM Roles Anywhere (today) | as now |
| Azure | workload identity federation (OIDC) or a managed identity | subscription and resource group per connection |
| Google Cloud | workload identity federation | project per connection |
| RunPod, others with API keys | a key in the vault (write-only), passed to that plan's environment only | marked "long-lived key" on the connection |

Each cloud gets its own small set of blocks and templates (Azure container apps, Google Cloud Run, RunPod serverless
endpoint and pod), its own built-in guardrails, and Watch support through that cloud's read APIs later.

### 7. The library: templates, skills, tools, agents and guardrail packs

One place (**Team › Library**, growing out of today's Marketplace) lists what can be installed:

| Kind | What it is |
| --- | --- |
| Template | layer 2 |
| Guardrail pack | layer 4 |
| Skill | instructions an agent or the assistant follows for one kind of job ("set up an AgentCore agent") |
| Tool | an MCP server or a container tool an agent may be granted |
| Agent | a ready agent definition with its skills, tools and scopes |

Each item is versioned, checksummed, shows who published and who reviewed it, and is installed by an administrator
(audited). First-party items live in this repository; third-party ones are pinned to a commit and never auto-update.
The canvas assistant chooses from installed templates and blocks; it may not write raw OpenTofu.

**The library, as built (step 8).** Team › Library is the marketplace grown: one page lists packages of agents and
skills, workflows, tools, guardrail packs and environment templates, and the reviewed OpenTofu templates the canvas
offers (built in, nothing to install), with a filter for each kind. Everything the marketplace already did holds:
versions, the content hash bound to the install, signed git sources (note 019), tiers that an update cannot raise, and
audited installs. A package can now also hold:

- **Guardrail packs** (Trivy, Checkov or Conftest, as in step 6), installed **turned off**: an administrator reads what
  they would deny and turns each on under Settings › Policy packs. An update keeps on or off as the administrator set it.
- **MCP servers** (remote or a local container image pinned by digest), installed **with no token and none of their
  tools turned on**; the administrator adds the token, lists the tools and turns on the ones wanted, as for any MCP
  server (note 042). An update that moves a server (new address or image) forgets its tools, so they are checked again.
  A package cannot point at this machine or allow private networks.
- **Who reviewed it** (`reviewed_by`), shown beside the publisher; empty is shown as "nobody named".

A package cannot take over a pack or MCP server someone made by hand (same id), and that is checked before anything is
written. Uninstalling removes the packs and MCP servers it added, turns off its agents and workflows, and keeps the
skills and agents with their history.

**What ships, and why.** The content follows what teams report using agents for in operations in 2026: incident
investigation and triage, runbooks turned into skills, drift detection, reliability and upgrade work, with a person
approving every write ([Dynatrace](https://cdn.dm.dynatrace.com/assets/documents/reports/bae22697-agentic-ai-report-2026.pdf),
[DuploCloud](https://duplocloud.com/blog/what-using-ai-agents-in-devops-looks-like-in-2026/),
[DevOps.com on skills](https://devops.com/claude-introduces-agent-skills-for-custom-ai-workflows/)), and the MCP servers
that recur in 2026 lists for platform teams: GitHub, the cloud provider and Terraform
([Claude Market](https://claudemarket.ai/blog/best-devops-mcp-servers), [MCPBundles](https://mcpbundles.com/blog/best-mcp-servers-devops)),
read-only first. Most of these surveys come from vendors; they agree on the use cases more than on the numbers.
New first-party packages:

| Package | Kind | What it gives |
| --- | --- | --- |
| Incident response | skills, agent | runbooks for the first minutes, a rollback, the on-call handover and a blameless postmortem; an incident scribe that builds the timeline (reads only) |
| Drift watch | skill, agent | compares what each blueprint built with AWS now, using a new read-only lookup `blueprints_built` (each resource's address and ARN or id from the state, nothing else) |
| Upgrade planner | skill, agent | Lambda runtimes, EKS, RDS and ElastiCache versions near the end of support, with dates quoted from AWS documentation |
| Known misconfigurations | guardrail pack | Trivy, denying critical findings (skips AWS-0040 and AWS-0041: the EKS template's API is public and signed in with IAM) |
| Cost guard | guardrail pack | Conftest warnings: EKS clusters, NAT gateways, two-zone databases, large sizes, provisioned tables, logs kept for ever |
| Data protection | guardrail pack | Conftest denies on making: tables without point-in-time recovery, under 7 days of database backups, secrets deleted at once, logs kept under 30 days (the EKS template's default of 14 included) |
| GitHub (read only) | tool | GitHub's remote MCP server at its read-only address, with a token the administrator gives |
| AWS Knowledge | tool | AWS's public documentation MCP server; no token |
| Terraform Registry | tool | HashiCorp's MCP server (1.4.1, MPL-2.0) as a local container that may reach only registry.terraform.io |

Checked: the Conftest rules in the real container on a plan that breaks each rule (and a removal, which breaks none);
discovery against the real AWS Knowledge server (5 read-only tools) and the Terraform Registry container (9 read-only
tools); the GitHub server answers 401 until a token is added. Not checked: the GitHub server with a token, and agents
using these tools in a real conversation.

## Plan

AWS first: steps 1 to 8 are AWS only. In order, each step its own design-note addendum or pull request, tested on the Pi:

1. Guardrails on every plan: built-in rules, the deny/warn display, audit (layer 4, first part).
2. The template format, and the AgentCore agent template.
3. Lambda, Step Functions and ECS cluster templates.
4. Repositories linked to blocks, with suggested pipelines (layer 3).
5. EKS with namespaces (two approved steps) and the data pipeline template (built; the code pipeline is covered by step 4).
6. Policy packs in containers: Conftest, Checkov or Trivy (layer 4, second part). **Built** (#147) and checked on the Pi
   ([2026-11-17](../testing/2026-11-17-policy-packs.md)); see "Policy packs, as built".
7. The generic resource block from provider schemas, with the administrator's allow list (layer 5), for AWS. **Built**
   (#149) and checked on the Pi ([2026-11-17](../testing/2026-11-17-resource-block.md)); see "As built (step 7)".
8. The library for templates, skills, tools, agents and guardrail packs (layer 7). **Built**; see "The library, as built".
9. **Only after AWS is complete** (maintainer, 2026-11-08): Google Cloud, then Azure, then RunPod and others (layer 6),
   each with its first blocks, templates and guardrails.

## Not decided

- How far Watch goes on Azure and Google Cloud.
- Signing library items (Sigstore) or checksums only.
