# 040: Any resource, any cloud: templates, a generic block, guardrails and a library

Status: **Step 1 built (2026-11-09): built-in guardrails on every blueprint preview (`internal/guardrail`, rule set `builtin-1`).** Extends notes 035 and 039. Builds in the order under "Plan".

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

### 3. Repositories linked to what runs them

A service, function or agent block can take a **source**: a GitHub repository (from the repositories the GitHub App may
read), a path and a branch. Forgeline then:

- suggests a pipeline (build, test, push the image to a registry the blueprint makes, deploy by updating the block's
  image through a proposal), using the read-only repository lookups the assistant already has;
- suggests which repository fits a block by reading Dockerfiles and manifests, and says why;
- never pushes to the repository; changes to it are pull requests a person merges.

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

### 5. The generic block: any resource of an allowed provider

For what no template covers, an administrator can allow **resource types** (per cloud account): for example
`aws_sns_topic` or `aws_cloudfront_distribution`. A **resource** block then takes any allowed type, and its form is
generated from the provider schema (required fields first, sensitive fields refused: a value that must be secret comes
from a secret block). Its links are references to other blocks' attributes, chosen from a list. Guardrails apply as
everywhere. It is drawn with the type's icon and named in plain words from the schema's description. This is the
"anyone can create any kind of resource" path, with the administrator deciding how wide "any" is.

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

## Plan

AWS first: steps 1 to 8 are AWS only. In order, each step its own design-note addendum or pull request, tested on the Pi:

1. Guardrails on every plan: built-in rules, the deny/warn display, audit (layer 4, first part).
2. The template format, and the AgentCore agent template.
3. Lambda, Step Functions and ECS cluster templates.
4. Repositories linked to blocks, with suggested pipelines (layer 3).
5. EKS with namespaces (two-stage apply), data pipeline and code pipeline templates.
6. Policy packs in containers: Conftest, Checkov or Trivy (layer 4, second part).
7. The generic resource block from provider schemas, with the administrator's allow list (layer 5), for AWS.
8. The library for templates, skills, tools, agents and guardrail packs (layer 7).
9. **Only after AWS is complete** (maintainer, 2026-11-08): Google Cloud, then Azure, then RunPod and others (layer 6),
   each with its first blocks, templates and guardrails.

## Not decided

- Whether Conftest, Checkov or Trivy is the default pack (Checkov has the most Terraform rules; Trivy also scans images,
  which pipelines already need). Decide in step 6 with real plans.
- How far Watch goes on Azure and Google Cloud.
- Signing library items (Sigstore) or checksums only.
