---
title: Endpoints
parent: API reference
nav_order: 1
---

# Endpoints

Role = the minimum role. A test (`internal/server/apidocs_test.go`) fails if a route is missing from this page or has a different role, so the lists stay true to the code. *public* needs no credentials.

## Health and sign-in

| Method and path | Role | Purpose |
| --- | --- | --- |
| `GET /healthz` | public | Liveness. |
| `GET /v1/auth/state` | public | Is setup needed, who am I, and `sso` (`enabled`, `name`): whether the sign-in page offers single sign-on. |
| `POST /v1/auth/setup` | public (needs server token) | Create the first admin. Body: `token`, `username`, `name`, `password`. |
| `POST /v1/auth/login` | public | Body: `username`, `password`. Sets the session cookie. Throttled. |
| `GET /v1/auth/oidc/start` | public | Starts single sign-on (design note 031): sends the browser to the provider with PKCE, a single-use `state` (10 minutes, bound to a `SameSite=Lax` cookie) and a `nonce`. When single sign-on is off it goes back to the sign-in page with `#sso-error=off`. |
| `GET /v1/auth/oidc/callback` | public | Where the provider sends the browser back. Checks the state and its cookie, exchanges the code, verifies the ID token (signature, issuer, audience, expiry, nonce), applies the rules (new people, domains, roles from groups, never demoting the last administrator) and sets the session cookie, then goes to `/#/`. A refusal goes to the sign-in page with `#sso-error=` `state`, `token`, `refused`, `provider` or `off`; the audit log has the full reason (`login.sso.failed`). A person is matched by issuer and subject, never by email. |
| `GET /v1/settings/sso` | admin | Single sign-on settings: `enabled`, `name` (button text), `issuer`, `client_id`, `address`, `groups_claim`, `mappings` (group and role), `allow_new`, `default_role`, `domains`, plus `has_secret`, `redirect_url` (to register at the provider) and `ready`. The client secret is never returned. |
| `PUT /v1/settings/sso` | admin | The same fields, plus `client_secret` (write-only; empty keeps the stored one, sealed in the vault). The issuer and address must be `https`, or `http` on a loopback host. Turning it on needs the issuer, client ID, address and a stored secret. Audited as `sso.update`. |
| `POST /v1/settings/sso/test` | admin | Body: `issuer`. Reads the provider's discovery document and says what it found (`ok`, `authorization_endpoint`, `token_endpoint`, `pkce`). Changes nothing. |
| `GET /v1/settings/approvals` | developer | The approval rules (design note 032): each with `name`, `accounts`, `kinds` (`ai`, `cloud`), `high_risk`, `approvals` (1 or 2) and `approvers`. No rules means one approval from an administrator who did not propose the change. |
| `PUT /v1/settings/approvals` | admin | Body: `rules`. Validated (known accounts, active administrators, two approvals need two possible approvers). Every matching rule applies and the strictest wins. Waiting changes show `approval` (`needed`, `given`, `approvers`, `rules`). Audited as `approval.rules`. |
| `POST /v1/auth/logout` | viewer | Ends the session. |
| `POST /v1/me/password` | viewer | Body: `current`, `new` (at least 12 characters). |
| `GET /v1/tokens` | viewer | Your personal API tokens (never the secrets); `?all=1` for everyone's, administrators only. |
| `POST /v1/tokens` | viewer | Body: `name`, `role` (at most your own; default yours), `days` (1 to 365, default 90). Needs a browser session, not a token. Returns the secret once. At most 10 each. |
| `DELETE /v1/tokens/{id}` | viewer | Revoke your token (an administrator can revoke any). |
| `POST /mcp` | public | The MCP endpoint (it checks the caller itself) (streamable HTTP, one JSON-RPC message per request). Only a personal API token works here (not the server token, not a session), so it answers 401 to anything else. See note 020. |

## Runs

| Method and path | Role | Purpose |
| --- | --- | --- |
| `POST /v1/runs` | developer | Submit a pipeline. `multipart/form-data`: field `pipeline` (YAML) and optional `source` (an archive of the files the steps read). |
| `GET /v1/runs` | viewer | List runs, newest first. |
| `GET /v1/runs/{id}` | viewer | One run with its steps and their status. |
| `GET /v1/runs/{id}/failure` | viewer | For a failed run: the step that failed first, exit code, likely causes, steps that did not run, and the end of its output. |
| `POST /v1/runs/{id}/cancel` | developer | Stop a run. |
| `GET /v1/runs/{id}/steps/{step}/logs` | viewer | A step's output. Send `Accept: text/event-stream` to follow it live. Secrets appear as `***`. |

A run's `status` is one of `queued`, `running`, `succeeded`, `failed`, `canceled`. A step's is one of `pending`, `running`, `succeeded`, `cached`, `failed`, `skipped`, `canceled`.

## Pipelines

| Method and path | Role | Purpose |
| --- | --- | --- |
| `GET /v1/pipelines` | viewer | Saved pipelines with latest status. |
| `POST /v1/pipelines/validate` | viewer | Body: `yaml`. Returns problems with line numbers and the step graph. Saves nothing. |
| `POST /v1/pipelines` | developer | Save a new pipeline. Body: `yaml`, `description`. The name comes from the YAML. |
| `GET /v1/pipelines/{id}` | viewer | The pipeline and its versions. |
| `PUT /v1/pipelines/{id}` | developer | New version. Body: `yaml`, `description`, `base_version`. `409` if someone saved after `base_version`. |
| `POST /v1/pipelines/{id}/run` | developer | Run the latest version. May use secrets. |
| `POST /v1/pipelines/{id}/versions/{version}/restore` | developer | Make an old version the newest. |
| `DELETE /v1/pipelines/{id}` | admin | Delete the pipeline and its versions. |
| `GET /v1/overview` | viewer | The pipeline and run figures on the Home page (its Needs you list also reads proposals, cloud changes, Guard findings and integrations). |

## Secrets

| Method and path | Role | Purpose |
| --- | --- | --- |
| `GET /v1/secrets` | developer | Names, descriptions, who changed them and when. Never values. |
| `PUT /v1/secrets/{name}` | admin | Body: `value` (at least 8 characters), `description`. Name matches `[A-Za-z_][A-Za-z0-9_]*`. |
| `DELETE /v1/secrets/{name}` | admin | Remove. |

## People and audit

| Method and path | Role | Purpose |
| --- | --- | --- |
| `GET /v1/users` | admin | List. |
| `POST /v1/users` | admin | Body: `username`, `name`, `password`, `role`. |
| `PATCH /v1/users/{id}` | admin | Any of `name`, `role`, `disabled`, `password`. The last admin cannot be removed or demoted. |
| `DELETE /v1/users/{id}` | admin | Remove. |
| `GET /v1/audit` | admin | The audit log, newest first. Filters by query string: `actor`, `action` (prefix), `denied=1`, `from` and `to` (dates, `to` included), `limit` (1 to 1000). |
| `GET /v1/audit/export` | admin | The whole audit log, or the part the same filters select, oldest first, as a download: `format=csv` (default; cells that would start a spreadsheet formula are prefixed with `'`) or `format=jsonl`. At most 200,000 events per export. Each export is audited as `audit.export` before the file is sent (design note 033). |
| `GET /v1/settings/audit` | admin | How long the audit log is kept: `retention_days` (0 keeps it forever, the default), `count`, `oldest`, `last_prune`, `last_pruned`. |
| `PUT /v1/settings/audit` | admin | Body: `retention_days`, 0 or 30 to 3650. Events older than the period are deleted once an hour; each deletion is audited as `audit.pruned` and each change as `audit.retention`. |

## GitHub

| Method and path | Role | Purpose |
| --- | --- | --- |
| `GET /v1/integrations/github` | developer | Current settings (no secrets), last deliveries. |
| `PUT /v1/integrations/github` | admin | Body: `enabled`, `api_base`, `public_url`, `pipeline_file`, `rules` (`owner/repo` plus branches), `token` (write-only). |
| `DELETE /v1/integrations/github` | admin | Disconnect and delete stored keys. |
| `POST /v1/integrations/github/test` | admin | Check the connection. |
| `POST /v1/integrations/github/app/start` | admin | Begin creating the App. Body: `public_url`, `name`, `owner`, `api_base`. |
| `GET /v1/integrations/github/app/launch` | admin | The page that sends you to GitHub. |
| `GET /v1/integrations/github/app/callback` | public | GitHub returns here with `code` and `state`. Bound to the admin's setup by a one-time state. |
| `GET /v1/integrations/github/app/installations` | admin | Installations of the App. |
| `GET /v1/integrations/github/app/repos` | admin | Repositories of an installation. |
| `POST /v1/integrations/github/app/install` | admin | Body: `installation_id`, `rules`, `public_url`. |
| `POST /webhooks/github` | public (signed) | Where GitHub sends events. Checked with the webhook secret; a bad signature gets `401`. |

## AWS

| Method and path | Role | Purpose |
| --- | --- | --- |
| `GET /v1/integrations` | developer | Every system Forgeline connects to, for the integrations hub (note 025): built ones with a status (`connected`, `attention`, `off`), logins found on this server by name only (`detected`, such as an AWS profile), and planned ones (`next`, `later`, with a build order and what they will do). Each has a plain `summary`, a `problem` that says what to do when it needs attention, `warnings`, who enforces read-only (`read_only_by`: `provider` or `forgeline`), and the last test. Status comes from stored settings; only the Docker host runs a local command. No secret is ever included. |
| `POST /v1/integrations/{id}/checks` | admin | Runs a read-only test of one integration (`github`, `aws`, `aws-mcp`, `docker`, `kubernetes`, `claude`) and returns its checks, each with a result (`ok`, `warn`, `fail`), a detail, a fix and a time. It creates and changes nothing (`changed` is always false), is audited, and is remembered for the hub. The Claude test sends one short question. Planned systems have no test (404). |
| `GET /v1/integrations/{id}/activity` | admin | The newest 30 audit entries that belong to one integration (`github`, `aws`, `aws-mcp`, `gcp`, `azure`, `claude`, `kubernetes`, `docker`), for the Activity tab: time, who, what, detail, and whether it was allowed (refusals are included). The match is exact, so AWS and the AWS MCP Server never show each other's entries. No address is included. |
| `GET /v1/integrations/{id}/policy` | developer | The access policy of an integration that has tools (`aws`, `aws-mcp`, `kubernetes`, `gcp`, `azure`; others are 404): each tool with its kind (`read` or `write`), level (`allow`, `ask` or `block`), the lowest role that may use it, and what it does, plus, for `aws`, `services` (each switch with `id`, `label`, `tech`, `on` and `level`: `off`, `read` or `change`; see the next row but one), plus the **effective access** of every agent (`allowed`, `blocked` by the connection, `needs_person` when the tool needs approval and an agent run has nobody to ask, or `not_listed` in the agent's tool list) with the reason. `readable` is false when the stored policy cannot be read; every tool is then refused. |
| `PUT /v1/integrations/{id}/policy` | admin | Body: `tools`, a map of tool name to `allow`, `ask` or `block`, for this integration's tools; the ones left out go back to allowed. A tool that asks for a change (`propose_aws_change`, `propose_cloud_change`) cannot be set to `allow`: a change is always a proposal that a different administrator approves, but it can be blocked. Unknown tools, another integration's tools and any other level are refused with 400. A policy that cannot be read is not overwritten (409). Audited with before and after. `ask` (a read tool that needs a person to approve each call) is accepted for read tools; for a tool that asks for a change it is accepted and changes nothing, because that is already how it works. A blocked tool is refused for everyone, including administrators and agents that list it, and is not shown to the model or offered over MCP. |
| `PUT /v1/integrations/{id}/services` | admin | AWS only (others are 404). Body: `off`, the AWS service switches turned off for the AI (`s3`, `ecs`, `ecr`, `ec2`, `lambda`, `rds`, `dynamodb`, `elb`, `sqs`, `sns`, `cloudformation`, `cloudwatch`, `ce`, and `other` for every service not listed), and `read_only`, the ones the AI may read but never propose a change to (Look only). Both lists replace what was stored; empty lists put everything back to change, with approval. An unknown switch, or one in both lists, is 400; an unreadable policy 409. A look-only service is refused in `propose_aws_change` and `propose_cloud_change`, and an AI proposal for it is refused at approval (409); reads work. An off service is refused in `call_aws`, `aws_script`, `aws_summary` and both proposal tools, left out of `aws_resources` and `guard_findings`, and an AI proposal for it is refused at approval (409). People still see everything in the explorer. The switches show under `services` in `GET /v1/integrations/aws/policy`. Audited as `policy.services`. |
| `GET /v1/integrations/gcp` | developer | The Google Cloud connection: whether it is on and its scope. Before it is connected, `detected_project` offers the server's gcloud default project (a name, never a credential). |
| `PUT /v1/integrations/gcp` | admin | Body: `enabled`, `scope` (a project id, `projects/…`, `folders/…` or `organizations/…`). Turning it on first proves the server's gcloud login can search the scope and refuses with what to fix (no login, no permission, API not enabled, unknown scope); nothing is saved on a refusal. Read-only: the only commands run are `asset search-all-resources`, `auth list` and `config get-value`. |
| `GET /v1/infra/gcp/resources` | viewer | Everything in the connected scope by service (Compute Engine, Cloud Run, GKE, Cloud Storage, Cloud SQL, Vertex AI and more; unknown types under `other`), at most 200 per service with the true count, cached 60 s; `?refresh=1` skips the cache. |
| `GET /v1/integrations/azure` | developer | The Azure connection: whether it is on and which subscriptions are chosen (none chosen means every subscription the login can read). Before it is connected, an administrator also gets `available`, the subscriptions the login can see (name and id). |
| `PUT /v1/integrations/azure` | admin | Body: `enabled`, `subscriptions` (subscription ids, at most 20; each must look like a GUID). Turning it on first proves the server's az login can run a Resource Graph query and refuses with what to fix (no login, no permission, the Resource Graph extension missing, unknown subscription); nothing is saved on a refusal. Read-only: the only commands run are `graph query` (with one fixed query), `account list` and `account show`. |
| `GET /v1/infra/azure/resources` | viewer | Everything in the chosen subscriptions by service (virtual machines, App Service, AKS, storage, SQL, networks, Azure OpenAI and AI Foundry under AI, and more; unknown types under `other`), read in pages of 1000 up to 3000 resources (`notice` says when the limit was reached), at most 200 shown per service with the count, cached 60 s; `?refresh=1` skips the cache. |
| `GET /v1/integrations/aws` | developer | Settings and identity. |
| `PUT /v1/integrations/aws` | admin | Use a server profile. Body: `enabled`, `profile`, `region`. The name `forgeline-sso` is reserved. |
| `GET /v1/integrations/aws/mcp` | admin | Whether the AWS MCP Server is on, the tools it offers, and the proxy and endpoint used. |
| `PUT /v1/integrations/aws/mcp` | admin | Body: `enabled`. Turning it on starts the signing proxy and checks the server offers `aws___run_script`. |
| `POST /v1/integrations/aws/sso/start` | admin | Body: `start_url`, `sso_region`. Returns `login_id`, the `user_code` and the link to approve. |
| `POST /v1/integrations/aws/sso/poll` | admin | Body: `login_id`. Never blocks; returns `status`: `pending`, `expired`, `denied`, or `authorized` once approved. |
| `GET /v1/integrations/aws/sso/accounts` | admin | Accounts you may use. |
| `GET /v1/integrations/aws/sso/roles` | admin | Roles in an account (`?account_id=`). |
| `POST /v1/integrations/aws/sso/connect` | admin | Body: `account_id`, `account_name`, `role_name`, `region`. Tests the credentials before keeping them. |
| `GET /v1/integrations/aws/sso` | developer | Whether you are signed in, and until when. |
| `DELETE /v1/integrations/aws/sso` | admin | Sign out and delete the stored token. |
| `GET /v1/ai/workflows` | developer | Workflows: stages, steps, schedule, run-as role. |
| `GET /v1/ai/workflows/{id}` | developer | One workflow with every version. |
| `POST /v1/ai/workflows` | admin | Create a workflow (version 1): stages of agent steps, optional gate per stage, optional schedule, run-as viewer or developer. |
| `POST /v1/ai/workflows/{id}/versions` | admin | Save a new version; agent versions are pinned. |
| `POST /v1/ai/workflows/{id}/rollback` | admin | Copy an old version forward as the current one. |
| `POST /v1/ai/workflows/{id}/disable` | admin | Stop it running, including its schedule. |
| `POST /v1/ai/workflows/{id}/enable` | admin | Turn it back on. |
| `POST /v1/ai/workflows/{id}/run` | developer | Start a run now (one at a time per workflow). |
| `GET /v1/ai/workflow-runs` | developer | Recent runs (`?workflow=`). |
| `GET /v1/ai/workflow-runs/{id}` | developer | One run with each step's output. |
| `POST /v1/ai/workflow-runs/{id}/release` | admin | Let a run waiting at a gate start its next stage. |
| `POST /v1/ai/workflow-runs/{id}/cancel` | developer | Cancel a run (its starter, or an administrator). |
| `GET /v1/envs` | developer | Environments (temporary stacks) with owner, status, expiry and cost estimate. |
| `GET /v1/envs/templates` | developer | The environment templates (developer box, QA sandbox, Lambda sandbox, event pipeline, workflow sandbox) with the options a person can set. |
| `GET /v1/envs/policy` | developer | The self-service policy. |
| `PUT /v1/envs/policy` | admin | Set the self-service policy: templates, time and cost limits, environments per person. |
| `POST /v1/envs/estimate` | developer | The cost of a request and whether the policy lets it start without approval. |
| `POST /v1/envs` | developer | Ask for an environment. Within the self-service policy it starts at once; otherwise an administrator approves the change as for any cloud change. |
| `GET /v1/envs/{id}` | developer | One environment. |
| `POST /v1/envs/{id}/extend` | developer | Extend it (its owner or an administrator), up to the longest life the policy allows. |
| `POST /v1/envs/{id}/destroy` | developer | Delete it now (its owner or an administrator). |
| `GET /v1/k8s/clusters` | viewer | Registered Kubernetes clusters (never their tokens). |
| `POST /v1/k8s/clusters` | admin | Register a cluster (`eks` through a connected AWS account, or `token`); proves it with a read before saving. |
| `DELETE /v1/k8s/clusters/{id}` | admin | Remove a cluster and its stored token. Changes nothing in the cluster. |
| `GET /v1/k8s/clusters/{id}/resources` | viewer | Nodes, workloads, pods, services, namespaces and warning events, read-only; `?refresh=1` skips the 60 s cache. |
| `GET /v1/guard/findings` | viewer | Guard findings (`?status=open|resolved`, `?account=`, `?severity=`) with severity counts and any accepted exception. |
| `GET /v1/guard/scans` | viewer | Recent scans and the automatic scan interval. |
| `POST /v1/guard/scan` | developer | Start a scan of every account, cluster and the pipelines (or one `account`, a cluster as `k8s:<id>`, or `pipelines`); read-only; one at a time. |
| `PUT /v1/guard/settings` | admin | Automatic scans every 6, 12, 24 or 168 hours, or 0 for off. |
| `POST /v1/guard/findings/{id}/accept` | admin | Accept a finding for 1 to 90 days with a reason; it reopens when that ends. |
| `DELETE /v1/guard/findings/{id}/accept` | admin | End an acceptance early. |
| `GET /v1/ai/tools` | developer | The tools an agent can be given, with the role each needs. |
| `GET /v1/ai/skills` | developer | Skills (name, description, current version). |
| `GET /v1/ai/skills/{id}` | developer | One skill with every version. |
| `POST /v1/ai/skills` | admin | Create a skill (version 1). |
| `POST /v1/ai/skills/{id}/versions` | admin | Save a new version; history is append-only and an unchanged save is refused. |
| `POST /v1/ai/skills/{id}/rollback` | admin | Make an old version current by copying it forward as a new version. |
| `DELETE /v1/ai/skills/{id}` | admin | Remove a skill no agent version uses. |
| `GET /v1/ai/agents` | developer | Agents with current version, tier and tools. |
| `GET /v1/ai/agents/{id}` | developer | One agent with every version. |
| `POST /v1/ai/agents` | admin | Create an agent (version 1): instructions, pinned skills, tool allowlist, tier T0 or T1. |
| `POST /v1/ai/agents/{id}/versions` | admin | Save a new version; skill pins are made exact. |
| `POST /v1/ai/agents/{id}/rollback` | admin | Copy an old version forward as the current one. |
| `POST /v1/ai/agents/{id}/disable` | admin | Turn an agent off (it cannot run or be listed). |
| `POST /v1/ai/agents/{id}/enable` | admin | Turn it back on. |
| `PUT /v1/ai/agents/{id}/reports-to` | admin | Body: `reports_to`, another agent's id, or empty to report to people. A line that would make a loop is refused. Describes the team; it gives no agent power over another. Audited as `ai.agent.team`. |
| `GET /v1/ai/team` | developer | The team for the org chart: each agent with `reports_to`, `mode` (`advises` or `proposes`), `state` (`working`, `waiting` at a workflow gate, `scheduled` with `next_at` and `every_minutes`, `idle`, `off`), `doing` and `run_id`, `workflows`, `steps_week`, `failed_week`, `last_at`; `mission` (the workflow run in progress, else the latest, with its stages); `schedules_stopped` when the assistant is off. |
| `GET /v1/blueprints` | developer | Blueprints (note 035), newest change first: name, account, region, number of blocks and of warnings, who changed it and when. |
| `POST /v1/blueprints` | developer | Save a new blueprint: `name`, `description`, `target` (`cloud` `aws`, `account`, `region`), `blocks`, `links`. Refused with a message that says where (`links[0]: ...`) when it is not valid, or when the account is not connected. Audited. Nothing is built. |
| `POST /v1/blueprints/check` | developer | Check a blueprint that is not saved: `error` when it is not valid, else the plain-words `checks`. |
| `GET /v1/blueprints/{id}` | developer | One blueprint with its `revision`, who made and changed it, and its `checks` (`ok` or `warn`). |
| `PUT /v1/blueprints/{id}` | developer | Replace a blueprint. Send the `revision` you started from; 409 if someone saved it since. Audited. |
| `DELETE /v1/blueprints/{id}` | developer | Delete a draft; only its author or an administrator. Audited. Nothing in the cloud changes. |
| `GET /v1/blueprints/{id}/code` | developer | The OpenTofu configuration the blueprint generates (`main.tf.json`, AWS provider `~> 6.72`). |
| `POST /v1/blueprints/{id}/plan` | developer | Preview: run `tofu plan` on the saved blueprint in the background (reads only). 202 with the preview, `status` `planning`; 409 while another preview or build of it runs. Audited. |
| `GET /v1/blueprints/{id}/plan` | developer | The latest preview, or `null`: `status` (`planning`, `ready`, `failed`), `error`, `revision`, `aws_account`, `add`, `change`, `remove`, and `changes` (`address`, `type`, `block`, `action` `add`/`change`/`replace`/`remove`). The plan file itself is never returned. |
| `POST /v1/blueprints/{id}/propose` | developer | Body `reason`. Files the latest preview as a `blueprint-apply` cloud change for a different administrator to approve; refused unless the preview is ready, of the saved blueprint, under an hour old, from the current state and changing something. On approval exactly that plan is applied. |
| `GET /v1/blueprints/{id}/built` | developer | Whether it has been built: `built`, and if so `aws_account`, `region`, `applies`, `updated_at`, `updated_by`. |
| `GET /v1/plugins` | developer | Whether plugins are on, and the installed plugins with their tools, network and host grants and secret names (never values). |
| `PUT /v1/plugins/settings` | admin | Turn plugins on or off. Turning on checks that Docker answers. |
| `GET /v1/market/packages` | developer | The catalogue: each package with its skills, agents, workflows, environment templates, tools, tiers, sha256, source (`built-in`, `local` or `git`), signer, and what is installed; `git` describes the signed git source (url, ref, trusted key ids, last commit and fetch, error). |
| `POST /v1/market/refresh` | admin | Fetch the signed git source now. |
| `DELETE /v1/market/seen/{id}` | admin | Make the server forget which signed versions of a package it has seen from git, so an older one is accepted again (for a version signed by mistake or a replaced key). Audited. |
| `POST /v1/market/install` | admin | Install or upgrade a package. Needs the sha256 that was reviewed; an upgrade never raises the tier. `grants` gives each plugin its network (`network`, or `hosts`: a subset of the hosts the plugin asked for) and secrets; nothing is granted by default. |
| `DELETE /v1/market/install/{id}` | admin | Forget an install and disable its agents. Skills and agents stay with their history. |
| `POST /v1/ai/agents/{id}/chat` | developer | Talk to an agent (`version` optional). It uses only its own tools, as the asker. |
| `GET /v1/aws/accounts` | viewer | The connected AWS accounts: the primary one and any added. |
| `POST /v1/aws/accounts` | admin | Add a read-only account by profile and region; proves it with `sts get-caller-identity`, refuses duplicates. |
| `PATCH /v1/aws/accounts/{id}` | admin | Body: `writes` (true or false). Allows or stops change proposals for an added account (off by default); turning it off cancels the proposals waiting for it. |
| `DELETE /v1/aws/accounts/{id}` | admin | Remove an added account and cancel the proposals waiting for it. Changes nothing in AWS. |
| `GET /v1/infra/aws` | viewer | Identity, stacks, instances. `?account=<id>` reads an added account. |
| `GET /v1/infra/aws/resources` | viewer | Every resource in the account by service (ECS, EC2, S3, Lambda, RDS and more), read-only; `?account=<id>` picks the account; `?refresh=1` skips the 60 s cache. |
| `GET /v1/infra/aws/alarms` | viewer | The account's CloudWatch metric alarms (name, state, reason, namespace, dimensions), alarms in ALARM first, at most 500; read-only, one `describe-alarms` call. `?account=<id>` picks the account. If the role may not read them, `error` names the missing permission (`cloudwatch:DescribeAlarms`). |
| `GET /v1/infra/aws/links` | viewer | What AWS states connects the map's resources (note 034): for each CloudFormation stack the inventory found (at most 25; `stacks_skipped` counts the rest), the resources it made from `list-stack-resources` (type, logical and physical id, status; deleted parts left out), and each Lambda event source mapping (a function and the queue, table stream or stream it reads, with its state). Read-only, cached for a minute like the inventory; `?account=<id>` picks the account, `?refresh=1` reads again. A refused part names the missing permission (`cloudformation:ListStackResources`, `lambda:ListEventSourceMappings`). |
| `GET /v1/infra/aws/cost` | developer | Spend for 30 days (each call is billed by AWS). |
| `GET /v1/infra/runtime` | viewer | The Forgeline server, Docker host, running steps, disk. |

## Cloud changes

| Method and path | Role | Purpose |
| --- | --- | --- |
| `GET /v1/cloud/catalog` | developer | Templates that can be deployed, and the instance actions. |
| `POST /v1/cloud/changes` | developer | Propose. Body: `kind` (`stack-deploy`, `stack-delete` or `instance-action`), `reason`, and `template`, `stack`, `params` or `instance_id`, `action` (`start`, `stop`, `reboot`). |
| `GET /v1/cloud/changes` | viewer | List. |
| `GET /v1/cloud/changes/{id}` | viewer | One change, including the preview of exactly what would happen. |
| `POST /v1/cloud/changes/{id}/approve` | admin | Body: `note`. Not allowed for the person who proposed it. Runs the change. |
| `POST /v1/cloud/changes/{id}/reject` | admin | Body: `note`. |
| `POST /v1/cloud/changes/{id}/cancel` | developer | Withdraw your own proposal. |

## GitHub Actions

Only repositories on the GitHub allow list. A workflow can only be started on a branch its rule allows.

| Method and path | Role | Purpose |
| --- | --- | --- |
| `GET /v1/actions/repos` | developer | The repositories you may use, and their branches. |
| `GET /v1/actions/workflows` | developer | `?repo=owner/name`. Workflows of the repository. |
| `GET /v1/actions/runs` | developer | `?repo=`, optional `workflow=` (number or `ci.yml`) and `status=`. Latest 30 runs. Each has `fork: true` when its code came from another repository. |
| `GET /v1/actions/runs/{id}` | developer | `?repo=`. The run and its jobs with steps. |
| `GET /v1/actions/jobs/{id}/logs` | developer | `?repo=`. The last 256 KB of the job's log as plain text. |
| `POST /v1/actions/dispatch` | developer | Body: `repo`, `workflow`, `ref` (an allowed branch), `inputs` (up to 25 strings). Needs `workflow_dispatch` in the workflow file. |
| `POST /v1/actions/runs/{id}/cancel` | developer | Body: `repo`. |
| `POST /v1/actions/runs/{id}/rerun` | developer | Body: `repo`, `failed_only`. |

## Assistant

| Method and path | Role | Purpose |
| --- | --- | --- |
| `GET /v1/assistant` | developer | Whether it is on, and which backend. |
| `PUT /v1/integrations/assistant` | admin | Body: `backend` (`off`, `api`, `claude-token`, `cli`), `model`, `api_base`, `api_key` and `claude_token` (both write-only; the token is stored as the secret `CLAUDE_CODE_OAUTH_TOKEN`). |
| `POST /v1/integrations/assistant/test` | admin | Ask it something small to check the setup. |
| `GET /v1/ai/proposals` | viewer | Changes the assistant drafted, newest first, with their status. |
| `GET /v1/ai/proposals/{id}` | viewer | One proposal: the exact script, the AWS calls it makes, risk, and (after approval) what ran. |
| `POST /v1/ai/proposals/{id}/approve` | admin | Body: `hash` (of the text you read), `note`. Not allowed for the person who proposed it. Applies the approved script through the AWS MCP Server and returns 202; poll the proposal. |
| `POST /v1/ai/proposals/{id}/reject` | admin | Rejects a waiting proposal. |
| `POST /v1/ai/proposals/{id}/cancel` | developer | The proposer or an administrator withdraws a waiting proposal. |
| `POST /v1/assistant/chat` | developer | Body: `message`, `history`, `context` (the page: run, pipeline, infra or overview). Returns `reply`, the read-only `lookups` it ran any `proposals` it filed and any `environments` it asked for (requested, never started: a different administrator approves). 20 a minute per person. |
| `POST /v1/assistant/runs` | developer | The same body, checks, limits and audit entries as `/v1/assistant/chat`, answered as server-sent events that follow the [AG-UI](https://docs.ag-ui.com/) vocabulary: `RUN_STARTED`, `CUSTOM forgeline.context`, then for each lookup `TOOL_CALL_START`, `TOOL_CALL_ARGS`, `TOOL_CALL_END`, `TOOL_CALL_RESULT` (a result cut at 2 KB, or the reason it was refused) and `CUSTOM forgeline.tool_status`; `CUSTOM forgeline.proposal` or `forgeline.environment` when one was filed; `TEXT_MESSAGE_START/CONTENT/END` with the whole answer in one piece; and `RUN_FINISHED` or `RUN_ERROR`. Checks that fail first (rate limit, bad body, assistant off) are ordinary JSON errors. Closing the connection cancels the model call. |
| `POST /v1/assistant/approvals/{id}` | developer | Answer a question the assistant stream is waiting on: body `{"decision":"approve"}` or `{"decision":"deny"}`. When the access policy says a read tool needs approval, `/v1/assistant/runs` sends `CUSTOM forgeline.approval` (`id`, `tool`, `integration`, `args`, `expiresInSeconds`) before the tool, waits, and sends `CUSTOM forgeline.approval_decided` (`approve`, `deny` or `expired`). Only the person who asked can answer, once; anything else is 404. No answer in two minutes is a denial. Both outcomes are audited as `policy.approval` against the integration. |
| `GET /v1/assistant/conversations` | developer | Your saved conversations, newest first: `id`, `title`, `updated`, `messages`, and `refs` (the proposals and environment requests the chat filed). Only your own; the shared server token has none. Note 027. |
| `GET /v1/assistant/conversations/{id}` | developer | One of your conversations with its messages (questions, answers, and the names of the lookups used; never a lookup result). Someone else's, an administrator's included, is 404. |
| `DELETE /v1/assistant/conversations/{id}` | developer | Delete one of your conversations. Audited as `chat.delete`. |
| `DELETE /v1/assistant/conversations` | developer | Delete all of your conversations. Audited as `chat.delete_all`. |
| `GET /v1/assistant/memory` | developer | The notes you can see: `mine` (about you, active or pending your confirmation) and `system` (active ones, ones you suggested, and every pending one for an administrator), with `enabled` and `admin`. |
| `POST /v1/assistant/memory` | developer | Add a note, body `{"text":"...","about":"me|system"}`. A note about you is active at once; a developer's note about the system waits for an administrator. Up to 300 characters; secrets and markup are refused (400). 409 when memory is off. |
| `PUT /v1/assistant/memory/{id}` | developer | Change the text of your own note, or any system note as an administrator. Anyone else gets 404. |
| `POST /v1/assistant/memory/{id}/approve` | developer | Confirm your own pending note, or approve a pending system note as an administrator. Anyone else gets 404. |
| `POST /v1/assistant/actions/{id}/confirm` | developer | Do what the assistant prepared (save a pipeline, start a run), as you. Only the person it was prepared for; anyone else gets 404. Once only (409 after), and not after 30 minutes. Audited. |
| `POST /v1/assistant/actions/{id}/dismiss` | developer | Discard a prepared action. Same ownership rule. |
| `DELETE /v1/assistant/memory/{id}` | developer | Delete your own note; an administrator deletes system notes; you can withdraw a system note you suggested while it is pending. |
| `DELETE /v1/assistant/memory` | developer | Forget every note about you. Audited as `memory.me.forget_all`. |
| `PUT /v1/assistant/memory/settings` | admin | Turn memory on or off, body `{"enabled":false}`. Audited as `memory.settings`. |

## The pipeline schema

`forgeline schema` prints the JSON Schema; the same file is in the repository at
[`schema/pipeline.v1alpha1.schema.json`](https://github.com/forgelinehq/forgeline/blob/main/schema/pipeline.v1alpha1.schema.json).
