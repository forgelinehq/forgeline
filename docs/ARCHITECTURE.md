# Architecture

How Forgeline is built today (2026-11-08). The reasons behind each part are in the design notes; what comes next is in
the [roadmap](ROADMAP.md).

## One server, one binary

Forgeline is a single Go program, `forgeline server`. It serves the API, the web console (plain JavaScript embedded in
the binary, no build step for users) and runs the work. It needs nothing else to start: state can live in memory, in a
file, or in PostgreSQL for anything you keep.

| Part | Where | What it does |
| --- | --- | --- |
| Pipeline model | `internal/pipeline` | The Go types are the source of truth; the JSON Schema and the YAML form are generated from them, and every error says where (`steps[2].needs[0]: …`) |
| Scheduler and executor | `internal/scheduler`, `internal/executor`, `internal/cache` | Steps run as containers on the Docker host, independent steps in parallel, unchanged ones from a cache |
| Run store | `internal/store` | Runs and logs, in memory or PostgreSQL |
| Console store | `internal/docstore` | Users, pipelines, agents, proposals, blueprints, missions… as documents in a file or PostgreSQL |
| Secrets | `internal/vault`, `internal/secrets`, `internal/redact` | Encrypted at rest with the server's key, write-only, masked in logs by exact match |
| Sign-in | `internal/authn` | Local users, roles (viewer, developer, administrator), API tokens, and single sign-on with OpenID Connect |
| API and console | `internal/server` | The HTTP API, the console's pages, the assistant, agents, workflows, missions, approvals and the audit log |
| Blueprints | `internal/blueprint` | The blueprint model, its checks, and the OpenTofu code it generates |

## The AI

The assistant and agents call a model through one function, so every call is counted against an agent's budget and a
mission's budget. The model can be Claude by API key, the `claude` program on the server, or a Claude Code token. The
model never gets cloud credentials and never runs anything itself: it asks the server for **lookups** (read-only, each
checked against the person's role, the per-service switches and the access policy), and anything that would change
something becomes a **proposal**. Everything read from logs, repositories and cloud accounts reaches the model as data
marked untrusted.

## Changing the cloud

Nothing changes a cloud account without a person. There are four kinds of change, all through the same approval path
(a different administrator approves, under the approval rules, and everything is audited):

| Change | Preview | Applied by |
| --- | --- | --- |
| A stack from a template | A CloudFormation change set | Forgeline executes that change set |
| The AWS calls an agent drafted | The exact calls | Forgeline runs them through the AWS MCP Server |
| An environment | A change set, a time to live and a cost estimate | Forgeline, and removes it when its time is up |
| A blueprint | `tofu plan` | Forgeline applies exactly that saved plan; the state is kept encrypted |

Cloud access uses the server's own AWS login (IAM Identity Center, a profile, or IAM Roles Anywhere), the `gcloud`
and `az` logins for read-only views, and a kubeconfig for Kubernetes.

## Running it

One server on a laptop, a small machine (it runs on a Raspberry Pi), or a VM. Running several servers against one
database has not been tested yet.
