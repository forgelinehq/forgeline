# 035: Blueprints and the design canvas

Status: **Slice 1 (the model, its checks and the OpenTofu it generates) is built.** Date: 2026-11-08.
Frame 4 of the confirmed screen frames (note 029). Decided by the maintainer on 2026-11-08: **OpenTofu**.

## Question

People who are not infrastructure specialists should be able to say what they want ("redo my interviewer
stack, keep it cheap"), see it drawn as blocks, change it, preview exactly what AWS will do, and send it for
approval. What is a blueprint, how does it become infrastructure, and how do we keep "a different person
approves every change" true?

## Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| What a blueprint is | A typed document: a name, a target (cloud, account, region), **blocks** and **links**. Go types in `internal/blueprint` are the source of truth | Principle 1. The canvas, the assistant and the API all edit the same document |
| Blocks (first set) | `frontdoor` (a public load balancer, HTTP or HTTPS), `service` (containers on ECS Fargate), `table` (DynamoDB), `bucket` (S3), `queue` (SQS), `logs` (CloudWatch log group), `existing` (a table, bucket or queue that already exists; drawn dashed and never created or changed) | What the frame needs and what most small apps need. Functions, databases and networks come later |
| Links | A front door sends traffic to one service; a service uses a table, bucket, queue or existing resource (`read` or `readwrite`) or writes its logs to a logs block. Nothing else is a valid link | A link is configuration, not decoration: it opens the port, grants exactly those IAM actions, and passes the resource's name to the containers |
| Safe by default, not as options | Buckets block all public access, are encrypted and owned by the account; tables use on-demand billing, point-in-time recovery and encryption; queues are encrypted; a service accepts traffic only from its front door; only a front door is reachable from the internet; tables and buckets carry `prevent_destroy` and deletion protection, so removing one from a blueprint fails the plan instead of deleting data | People who do not know these settings should still get them |
| Checks | Each blueprint is summarised in plain words (`ok` or `warn`): what is open to the internet, whether data is kept, how long logs are kept, plain HTTP, a service nothing can reach, one region only, files without old versions | The frame's check list; computed from the document, never from a model |
| Code | **OpenTofu** (MPL-2.0), generated as `main.tf.json` with the `hashicorp/aws` provider pinned to `~> 6.72`. JSON, not HCL, so the generator is `encoding/json` with no new dependency, and the output is deterministic. Every resource is tagged `forgeline:blueprint` | The maintainer chose OpenTofu: one tool for AWS now and Google Cloud and Azure later |
| Text people type | Names and environment values are escaped (`${` and `%{`) so nothing a person types is evaluated by OpenTofu. Environment values are not secrets; secrets come later from Forgeline's secret store | A blueprint must not become a way to run expressions |
| Network | The account's default VPC and its subnets, read at plan time. Tasks get a public address only to pull images; inbound traffic still comes only from the front door | No NAT gateway cost for small apps. A chosen VPC is later |
| Preview and apply (slice 3) | `tofu plan -out` in a fresh directory; the plan (`tofu show -json`) is drawn on the canvas as add, change and remove, and is what the approver sees. Approval applies **that saved plan** and nothing else; a plan made stale by another change fails | What is approved is what runs |
| State (slice 3) | Kept by Forgeline, encrypted with its vault, one state per blueprint and target, with a lock while a plan or apply runs | No bucket to create before the first deploy; Forgeline is the only writer |
| The binary (slice 3) | A pinned `tofu` release downloaded and checked against its published SHA-256 sums, like the Tailwind CLI; providers are cached in the data directory | Same supply-chain rule as the rest of the build |
| Who may do what | Developers draft and preview; applying is a cloud change approved by a different administrator under the approval rules (note 032). The AI service switches (note 030) apply to what the assistant drafts | No automatic apply anywhere (note 030) |

## Slices

1. **Model, checks, code (this slice).** `internal/blueprint`: types, validation with messages that say where
   (`blocks[2].port: ...`), checks, and `main.tf.json` generation. API to save drafts and read the generated
   code. Build › Blueprints lists them. No AWS call, no `tofu` run.
2. **Canvas.** Build › Design: blocks laid out with our own SVG layout, add and edit blocks and links, the
   checks beside it, the Code view.
3. **Preview and approval.** The `tofu` runner, state, `Preview changes` drawn on the canvas, `Send for approval`
   as a cloud change, apply of the saved plan.
4. **The assistant drafts.** "Change something…" beside the canvas edits the document through a tool, with the
   same validation; reading an existing stack (for example a rolled-back one from the Watch map) as a start.

## Not decided yet

- Cost estimates per block (prices differ per region; the frame's "about $31/month" needs a real price source).
- Secrets for services, custom VPCs, autoscaling, more block kinds, Kubernetes and the other clouds.

## Slice 1: tested

Go: every validation rule with the place it names, the checks, the generated code (escaping, data kept,
public access blocked, only the front door open, least-privilege access per link, the container's settings,
HTTPS redirect, name lengths, the same code every time). The generated code was checked with OpenTofu 1.13.9
(`tofu init` and `tofu validate` with the AWS provider) for HTTP and HTTPS front doors; set `FORGELINE_TOFU` to
run that test. API: roles, a message that says where, an unknown account, revisions, author-only delete, audit.
Node: the words for blocks and links, and every starting point is valid by the server's rules. 13 of 14 rules
fail a test when removed; the 14th (escaping the image) cannot fail, since an image name may not contain `$` or `{`.
