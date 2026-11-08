# 035: Blueprints and the design canvas

Status: **Slices 1 (the model, its checks and the OpenTofu it generates), 2 (the canvas) 3 (preview, approval and apply) and 4 (the assistant beside the canvas) are built.** Date: 2026-11-08.
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

## Slice 2: the canvas (built)

| Decision | Choice |
| --- | --- |
| Page | Build › Design (`#/design/{id}`), the Build space's first tab; `#/design` opens the blueprint changed last |
| Drawing | SVG only: a block is a group placed with a `transform` attribute, so the CSP needs no inline styles. It scales down to fit its panel, but not below a readable size (then it scrolls) |
| Editing | Add from the toolbar (each kind starts valid); drag or arrow keys to move; the panel on the right edits the picked block's fields and its links. Only links the server would accept are offered (`ui/lib/canvas.js` mirrors the Go rules; the server stays the authority) |
| Checking | Each change is sent to `POST /v1/blueprints/check`; the answer's `blocks[2]` and `links[0]` are shown as the names on the canvas. Save is off while there is a problem. Saving sends the revision it started from |
| Layout | A blueprint with no positions is laid out in three columns: front doors, apps, then what apps use |

Tested under Node: building the small web app only with the canvas's operations (and the server accepts the result),
the links offered at each step, ids and names of added blocks, removing blocks and links, moving, layout, the
messages, paths and bounds. Every kind's starting block is valid by the Go rules. Browser: drag, add, link, a
problem named by its block with Save off, fix, save, reload; light and dark; no sideways scroll on a phone.

## Slice 3: preview, approval and apply (built)

| Decision | Choice |
| --- | --- |
| The binary | OpenTofu 1.13.9, downloaded on first use into `<data-dir>/tools/tofu-1.13.9/` and refused unless the zip's SHA-256 matches the release's published sum (pinned for Linux and macOS, amd64 and arm64). `FORGELINE_TOFU` names another binary (`scripts/dev/fake-tofu` for demos). Providers are cached in `<data-dir>/tools/tofu-plugins` (the AWS provider takes about 700 MB) |
| What OpenTofu sees | A fixed environment: `PATH`, `HOME`, the AWS config file variables, CA bundles and proxies, the target's `AWS_PROFILE` and `AWS_REGION`, and its own settings (`TF_IN_AUTOMATION`, an empty CLI config, the provider cache). Never the server's token or other variables. Each run gets a private directory, removed afterwards (it holds the state in the clear) |
| Preview | `POST /v1/blueprints/{id}/plan` (developer) runs `tofu init` and `tofu plan -out` in the background against the saved blueprint, reading only. `tofu show -json` gives the resources to add, change, replace or remove, each tied to its block. The plan file is sealed with the vault; the API never returns it. The canvas marks each block (+ add, ~ change, replace, remove) and lists every resource |
| Send for approval | `POST /v1/blueprints/{id}/propose` with a reason files a `blueprint-apply` cloud change. Refused unless the preview is finished, of the blueprint as saved now, under an hour old, made from the state as it is now, and changes something. The change carries a frozen copy of the plan; its preview rows are the plan's resources |
| Approval | The usual rules: a different administrator approves, and the approval rules (note 032) count a plan that removes or replaces anything as high risk |
| Apply | On approval Forgeline restores the code, the provider lock file and the state, runs `tofu init -lockfile=readonly` (the providers the plan was made with) and `tofu apply` of the saved plan file. It refuses if the blueprint was built again since the preview. The state is sealed and saved after every apply, failed ones too, since a failed apply may have made things. The result lists the outputs (the front door's address) |
| Built blueprints | A built blueprint cannot be deleted as a draft (its state would be lost) or moved to another account or region. Removing what a blueprint built comes later |
| Permissions | Building needs the AWS permissions for what the blueprint makes. An app block makes two IAM roles (`fl-…-exec` and `fl-…-task`), which `PowerUserAccess` does not allow: give the role `iam:CreateRole`, `iam:DeleteRole`, `iam:GetRole`, `iam:TagRole`, `iam:PutRolePolicy`, `iam:GetRolePolicy`, `iam:DeleteRolePolicy`, `iam:AttachRolePolicy`, `iam:DetachRolePolicy`, `iam:ListRolePolicies`, `iam:ListAttachedRolePolicies`, `iam:ListInstanceProfilesForRole` and `iam:PassRole` on `arn:aws:iam::<account>:role/fl-*` only. Tables, buckets, queues and logs need no IAM |

Tested with a stand-in OpenTofu (Go, race detector): preview, the environment OpenTofu sees (a server variable
planted for the test never reaches it), the work directory removed, the plan sealed and never returned, a stale
preview refused (edited, older state, nothing to do), propose, the proposer cannot approve, apply of the saved plan
with the planned providers, the state sealed, a second proposal from the same preview refused at apply, a built
blueprint cannot be deleted or moved, removing a block is high risk, a failed apply keeps its state and the next
preview starts from it, and a download with the wrong checksum is refused. 15 rules removed one at a time, each
fails a test. Browser with `fake-aws` and `fake-tofu`: preview, stale after an edit, send, a second administrator
approves, the apply succeeds, the canvas shows Built and the next preview changes nothing.

## Slice 4: the assistant beside the canvas (built)

| Decision | Choice |
| --- | --- |
| Where | A panel on Build › Design. A person describes a change; the answer is a few sentences and, when it edits, a suggested draft with its changes listed in words (`blueprint.Diff`). **Use this** puts it on the canvas unsaved; the person saves, previews and sends it for approval as usual |
| What the model gets | Its own instructions only (the block kinds, their fields and the link rules), the draft as untrusted data, and the services it may not use. No lookups and no tools: it cannot read the account or act |
| Checks on its answer | The draft is checked by the same rules as a save; if it is not valid the model is told why once and may send it again. The account and region are always the saved blueprint's. Blocks keep their places; new ones are placed in their column. A draft that adds, changes or removes a block on a service the AI service switches (note 030) turn off or set to look only is not offered, and the reason is shown |
| Never | The assistant does not save, preview or propose. Each question is audited (`blueprint.assist`, with lengths, not text) |
| API | `POST /v1/blueprints/{id}/assist` (developer): `message`, `draft` (what is on screen), `history`. Answers `reply`, `draft` (or none), `changes`, `checks`, `problem` |

Tested with a scripted model: a draft with a queue comes back checked, with its changes in words, the target kept,
places kept and nothing saved; the instructions carry the draft as data and no lookups; an invalid draft gets one
retry and a second failure gives a reason and no draft; a plain question gets no draft and no retry; a look-only
service blocks an added queue, a turned-off service blocks removing a table, and the model is told; with the
assistant off it says so. 8 rules removed one at a time, each fails a test. Browser with a stand-in `claude`:
ask, see the suggested changes, use it, the new block is placed and the checks pass.

## Built by Forgeline (built 2026-11-08)

Build › **Built** (`#/built`, `GET /v1/built`) lists what Forgeline created and keeps: each blueprint with a saved state
(its resources read from the sealed state, with only the address, type, name, id and ARN leaving the server; each resource
tied to its block by its OpenTofu name), stacks deployed by an approved change and not deleted by a later one, and
environments that are up. Each blueprint is drawn as an interactive diagram (its blocks where they sit on the canvas, the
flow along its links animated); picking a block lists its resources. **Check against AWS** (`?check=1`) looks each
resource up in the account's inventory: present, missing (deleted outside Forgeline), or not checked (a kind the inventory
does not list, such as a role, or a state that names no id). Tested: a state attribute that is not a name, id or ARN never
reaches the API, data sources are left out, deleted and failed stacks are not listed, finished environments are not
listed, and the check marks present, missing and unchecked.
