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
| `GET /v1/settings/approvals` | developer | The approval rules (design note 032): each with `name`, `accounts`, `kinds` (`ai`, `cloud`, `publish`, `runbook`), `high_risk`, `approvals` (1 or 2) and `approvers`. No rules means one approval from an administrator who did not propose the change. |
| `GET /v1/settings/resource-types` | developer | The resource types the generic resource block may make (design note 040, step 7): `accounts`, each a sorted list such as `aws_sns_topic`. |
| `PUT /v1/settings/resource-types` | admin | Body: `accounts` (account id to types, at most 300 each). Every type must exist in the AWS provider's schema; IAM users, groups, access keys, other long-lived credentials and account-wide settings are refused. The first call may take a minute while the schema is made. Audited as `blueprint.resource-types` ("allows ...", "no longer allows ..."). |
| `GET /v1/resource-types/search` | developer | `?q=sns topic`: up to 40 resource types whose name matches, shortest first, with a plain `label`; `count` and `provider_version` of the schema. |
| `GET /v1/resource-types/{type}` | developer | One type for the canvas form: `fields` (`name`, `kind` `string`/`number`/`bool`/`list`/`map`/`json`/`block`, `required`, `settable`, `why` it cannot be set: secret, set by AWS or the blueprint's region; `nesting`, `min_items`, `max_items` for a nested block), required first; `outputs`, the attributes a link may read. |
| `GET /v1/settings/policy-packs` | developer | The policy packs (design note 040, step 6): `packs` (each `id`, `name`, `tool` `trivy`/`checkov`/`conftest`, `enabled`, `deny_at`, `deny`, `skip`, `rules`), `tools` (the pinned release and image of each tool) and `builtin` (the built-in rules' version). |
| `PUT /v1/settings/policy-packs` | admin | Body: `packs` (at most 10). `deny_at` (Trivy only) is `critical`, `high`, `medium` or `low`; `deny` and `skip` list rule ids (Trivy and Checkov); `rules` (Conftest only) maps up to 10 `.rego` file names to their text, 32 KB each. Every preview then runs each enabled pack in a container with no network, on a copy of the plan without sensitive values or the earlier state; the preview's `packs` records each run (`version`, `image`, `denies`, `warnings`, `skipped`, `error`). A pack that cannot run denies the plan, and a preview made before the packs changed cannot be sent (409). Audited as `guardrail.packs` (rule text is counted, not copied). |
| `PUT /v1/settings/approvals` | admin | Body: `rules`. Validated (known accounts, active administrators, two approvals need two possible approvers). Every matching rule applies and the strictest wins. Waiting changes show `approval` (`needed`, `given`, `approvers`, `rules`). Audited as `approval.rules`. |
| `POST /v1/auth/logout` | viewer | Ends the session. |
| `POST /v1/me/password` | viewer | Body: `current`, `new` (at least 12 characters). |
| `GET /v1/tokens` | viewer | Your personal API tokens (never the secrets); `?all=1` for everyone's, administrators only. |
| `POST /v1/tokens` | viewer | Body: `name`, `role` (at most your own; default yours), `days` (1 to 365, default 90). Needs a browser session, not a token. Returns the secret once. At most 10 each. |
| `DELETE /v1/tokens/{id}` | viewer | Revoke your token (an administrator can revoke any). |
| `POST /mcp` | public | The MCP endpoint (it checks the caller itself) (streamable HTTP, one JSON-RPC message per request). Only a personal API token works here (not the server token, not a session), so it answers 401 to anything else. See note 020. |
| `POST /mcp/engine/{token}` | public | The agent engine's own endpoint (note 053): the Claude Code program Forgeline started for one chat message calls Forgeline's tools and its permission prompt here. The random token in the path identifies one running message and stops working when it ends; only the same machine (loopback) is answered, so it is not for clients. |

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
| `POST /v1/pipelines` | developer | Save a new pipeline. Body: `yaml`, `description`, optional `source` (`{"repo": "owner/name", "branch": "main"}`, a branch on the GitHub allow list; not a pattern). The name comes from the YAML. |
| `GET /v1/pipelines/{id}` | viewer | The pipeline and its versions. |
| `PUT /v1/pipelines/{id}` | developer | New version. Body: `yaml`, `description`, `base_version`, and `source`: an object sets it, `null` clears it, leaving it out keeps it. `409` if someone saved after `base_version`. |
| `POST /v1/pipelines/{id}/run` | developer | Run the latest version. May use secrets. With a `source`, the branch must still be on the allow list (`409` if not); the run downloads the branch's newest commit first (`502` if GitHub cannot give it; no run is created) and records it as `source`. Such runs build images but never push. |
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
| `GET /v1/ai/workflows` | developer | Workflows: stages, steps, schedule, run-as role. Each also has `graph`, `kinds` (nodes per kind), `agents`, `webhook`, `on_run_failed` and its newest run as `last_run`; archived ones are left out. |
| `GET /v1/ai/workflows/{id}` | developer | One workflow with every version. |
| `POST /v1/ai/workflows` | admin | Create a workflow (version 1): stages of agent steps, optional gate per stage, optional schedule, run-as viewer or developer. Or, instead of `stages`, a `graph` (design note 044): `nodes` (`id`, `kind` `start`/`agent`/`lookup`/`decide`/`ask`/`merge`/`end`, `x`, `y`, `max_visits`, and per kind `agent`, `agent_n`, `task`, `reads`, `fields`; `tool`, `args`; `by` `rule`/`agent` and `rules` (`key`, `op` `equals`/`contains`/`gt`/`lt`/`empty`, `value`, `when`); `question`, `choices`, `answerers`; `outcome`, `summary`) and `edges` (`from`, `to`, `when` for a branch). At most 40 nodes and 80 wires; exactly one start; every node reachable; a decision needs an `else` wire. A graph version may also take `webhook` (true: starts from its signed webhook) and `on_run_failed` (pipeline names, or `*`: starts when such a run fails, with the run in `trigger`). A graph run has `state`, `visits` (each node visit's `status`, `input`, `output`, `branch`, `answered_by`), `waiting` and `steps` (at most 50). |
| `POST /v1/ai/workflows/{id}/versions` | admin | Save a new version; agent versions are pinned. |
| `POST /v1/ai/workflows/{id}/rollback` | admin | Copy an old version forward as the current one. |
| `POST /v1/ai/workflows/{id}/disable` | admin | Stop it running, including its schedule. |
| `POST /v1/ai/workflows/{id}/enable` | admin | Turn it back on. |
| `DELETE /v1/ai/workflows/{id}` | admin | Deletes a workflow by archiving it: turned off and hidden (`GET /v1/ai/workflows?archived=1` lists them), restorable for 30 days, then removed by the hourly sweep (`ai.workflow.purge`); its past runs stay. 409 while a run is going or waiting. Audited as `ai.workflow.archive`. |
| `POST /v1/ai/workflows/{id}/restore` | admin | Brings an archived workflow back, still turned off. Audited. |
| `POST /v1/ai/workflows/{id}/run` | developer | Start a run now (one at a time per workflow). |
| `POST /v1/ai/workflows/{id}/schedule/pause` | admin | Pause the current version's schedule without a new version; it still runs by hand, from its webhook and after failed runs. Audited as `ai.schedule.pause` (design note 047). |
| `POST /v1/ai/workflows/{id}/schedule/resume` | admin | Resume it; a cron schedule waits for its next time instead of running the missed ones. Audited as `ai.schedule.resume`. |
| `GET /v1/ai/workflow-runs` | developer | Recent runs (`?workflow=`). |
| `GET /v1/ai/workflow-runs/{id}` | developer | One run with each step's output. |
| `POST /v1/ai/workflow-runs/{id}/release` | admin | Let a run waiting at a gate start its next stage. |
| `POST /v1/ai/workflows/{id}/hook-secret` | admin | Makes a new webhook secret for a workflow and returns it once (`secret`, `url`, `how`); the old one stops working. Audited as `ai.workflow.hook-secret`. |
| `POST /hooks/workflows/{id}` | public | Starts a graph workflow whose current version has `webhook: true`. The header `X-Forgeline-Signature-256` (or GitHub's `X-Hub-Signature-256`) is `sha256=` and the hex HMAC-SHA256 of the body with the secret. Any bad signature, unknown workflow or missing secret answers 401 the same way. Body at most 64 KB; it reaches the graph as the state value `trigger`. 409 when the version takes no webhook, the workflow is off or a run is going; 429 after 30 webhook runs in an hour. |
| `POST /v1/ai/workflow-runs/{id}/answer` | developer | Body: `answer`. Answers the question a graph run waits at: any administrator, or only the people the node names; with choices, one of them (it picks the wire). Audited as `ai.workflow.answer`. |
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
| `GET /v1/ai/agents` | developer | Agents with current version, tier and tools. Archived agents are left out; `?archived=1` lists only them, with `archived_at`. |
| `GET /v1/ai/agents/{id}` | developer | One agent with every version. |
| `GET /v1/ai/agents/{id}/export` | developer | `?format=bundle` or `docker`, `&version=n`: one version as a Forgeline bundle (a zip: `agent.yaml`, `skills/<name>/SKILL.md` in the Agent Skills format, `README.md`) or a Docker Agent file (`application/yaml`). The current version when `version` is left out. No key or secret is in either; Forgeline's own tools are listed as left out. 409 when a pinned skill or the model's provider is gone. Audited as `ai.agent.export` (design note 046). |
| `POST /v1/ai/agents` | admin | Create an agent (version 1): instructions, pinned skills, tool allowlist, tier T0 or T1, and optionally `model` (the model it asks, such as `claude-haiku-5-5`; empty uses the assistant's). |
| `POST /v1/ai/agents/import` | admin | `{"bundle": "<the zip, base64>", "dry_run": true}`: bring in a Forgeline bundle (at most 512 KiB). A new agent, or a new version of the agent with the same name. Skills are matched by their text, and created when none has it; tools this server lacks, proposal tools on a T0 agent and `ask_agent` are left out with a reason; a model this server lacks becomes the assistant's. The answer says all of that (`action`: `create`, `version` or `unchanged`; `skills`, `dropped`, `notes`); a dry run saves nothing, and `unchanged` saves nothing either. Audited as `ai.agent.import` (design note 046). |
| `POST /v1/ai/agents/{id}/push` | admin | `{"registry": "<id>", "repository": "triage", "version": 3, "reason": "..."}`: propose pushing one version (the current one when `version` is 0 or left out) to a registry as Docker Agent stores it, tagged `v<n>`. The repository goes under the registry's namespace; empty uses the agent's id. The proposal holds the exact Docker Agent file and reference; nothing is sent until a different administrator approves it. Audited as `ai.agent.push.propose` (design note 046). |
| `GET /v1/ai/pushes` | developer | The 100 newest push proposals, with their status (`proposed`, `pushing`, `pushed`, `failed`, `rejected`, `cancelled`, `expired` after 24 hours) and, while waiting, what the approval rules ask (`approval`). |
| `GET /v1/traces` | developer | `?kind=agent\|workflow\|team\|mission\|pipeline\|assistant\|model`, `&subject=<agent or workflow id>`, `&failed=1`, `&limit=n` (at most 500): the newest traces without their spans, each with `name`, `kind`, `who`, `started`, `millis`, `failed`, tokens, `cost_usd`, `model_calls`, `tool_calls`; and `total`. Forgeline keeps the newest 1,000 traces and none older than 14 days (design note 049). |
| `GET /v1/traces/{id}` | developer | One trace with its spans (`trace_id`, `span_id`, `parent_id`, `name`, `kind`, `start`, `end`, `error`, `attrs` with the OpenTelemetry GenAI names such as `gen_ai.operation.name`, `gen_ai.request.model`, `gen_ai.usage.input_tokens`). No prompt, answer, instructions or tool arguments are kept. |
| `GET /v1/ai/evals` | developer | `?agent=<id>`: eval suites, each with its cases and `last_run` (design note 050). |
| `POST /v1/ai/evals` | developer | `{"agent", "name", "description", "attempts": 1-5, "judge": "provider/model", "cases": [{"name", "input", "fixtures": {"tool": "what it returns"}, "fields": ["cause"], "checks": [{"kind", "value", "key", "n"}]}]}`: a new suite. Check kinds: `contains`, `not_contains`, `regex`, `json` (value: keys it must have), `field` (key = value), `tool_called` (value: tool; key: an argument it must carry), `tool_not_called`, `max_tool_calls`, `max_tokens`, `max_seconds` (n), `judge` (value: the rubric). At most 100 cases and 12 checks a case. Audited as `ai.eval.create`. |
| `GET /v1/ai/evals/{id}` | developer | One suite. |
| `PUT /v1/ai/evals/{id}` | developer | The same fields; replaces the suite's name, attempts, judge and cases. Audited as `ai.eval.update`. |
| `DELETE /v1/ai/evals/{id}` | developer | Delete a suite and its runs. Audited as `ai.eval.delete`. |
| `POST /v1/ai/evals/{id}/run` | developer | `{"version": 3, "model": "provider/model"}` (both optional): run every case `attempts` times against that version, as the caller, in the background. Proposals, environment requests, prepared actions and memory writes are recorded, never carried out; a tool with a fixture returns it. 409 while the suite is running. Audited as `ai.eval.run` and, when finished, `ai.eval.done`. |
| `POST /v1/ai/evals/attack-suite` | developer | `{"agent", "attempts": 1-5 (default 3)}`: add the ready-made attack suite (seven cases from the OWASP agentic list: orders planted in a log or an issue, a change asked for without approval, a tool misused, secret values, a role the person lacks) to an agent. 409 if it already has one. Audited as `ai.eval.create`. |
| `POST /v1/ai/evals/case-from-trace` | developer | `{"trace_id"}`: a case made from a trace that kept its prompt (`case`, `agent`, `tools`); 409 when it did not. |
| `GET /v1/ai/eval-runs` | developer | `?suite=<id>`: runs, newest first (`status`, `version`, `model`, `passed_cases`, `total_cases`). The newest 50 per suite are kept. |
| `GET /v1/ai/eval-runs/{id}` | developer | One run: per case `pass` (every attempt passed) and `passed_attempts`, and per attempt its `checks` (`kind`, `pass`, `note`), `reply`, `tools`, `recorded`, `tokens`, `millis` and `trace_id`. |
| `PUT /v1/ai/agents/{id}/gate` | admin | `{"suite": "<one of this agent's suites>"}` (or `""` to turn it off): from now on a saved version is a candidate; it becomes current when it is published, which needs a passing run of that suite on it. Audited as `ai.agent.gate` (design note 050). |
| `POST /v1/ai/agents/{id}/publish` | admin | `{"version": 4, "when_passes": false}`: make a version current. With a gate it needs a run of the gate suite on that version, with its own model, every case passed, made after the suite last changed (409 otherwise); with `when_passes` it starts that run instead (202 with `run`) and publishes when every case passes. Without a gate it publishes at once. A rollback is not gated. Audited as `ai.agent.publish`. |
| `GET /v1/settings/otel` | admin | Where traces are sent: `enabled`, `endpoint`, `service`, `headers` (`name`, `set`; values are never returned) and `stats` (`sent_spans`, `queued_spans`, `dropped_spans`, `failed_batches`, `last_error`, `last_sent`) since the server started. |
| `PUT /v1/settings/otel` | admin | `{"enabled": true, "endpoint": "https://otlp.example.com", "service": "forgeline", "content": false, "headers": [{"name": "Authorization", "value": "Bearer ..."}]}`: `content` keeps prompts, answers and tool arguments and results in traces (credentials and stored secrets hidden, 4 KB each); the rest sends every trace to an OTLP/HTTP endpoint (`/v1/traces` is added; plain http only for localhost and 127.0.0.1). A header without `value` keeps its stored one; a header left out is deleted from the vault. At most 8 headers; Host, Content-Type and the like are refused. A person's user name is never sent. Audited as `otel.update` (design note 049). |
| `POST /v1/settings/otel/test` | admin | Send one small trace now with the saved settings: `{"ok": true, "trace_id": "..."}`, or 502 with what the endpoint answered. Audited as `otel.test`. |
| `GET /v1/ai/pushes/{id}` | developer | One push: the agent and version, `ref`, the exact `yaml`, the tools that stay behind (`left_out`), `hash`, and after a push the `digest` the registry confirmed. |
| `POST /v1/ai/pushes/{id}/approve` | admin | `{"hash": "<the push's hash>", "note": "..."}`: approve it. Refused to the proposer, for a different hash, when the registry was removed or its host or namespace changed, and by the approval rules (kind `publish`). The approval that completes the count pushes exactly the approved file and answers with the result. Audited as `ai.agent.push.approve` and `ai.agent.push`. |
| `POST /v1/ai/pushes/{id}/reject` | admin | Reject a waiting push. Audited as `ai.agent.push.reject`. |
| `POST /v1/ai/pushes/{id}/cancel` | admin | Withdraw a waiting push. Audited as `ai.agent.push.cancel`. |
| `GET /v1/runbooks` | developer | Runbooks (design note 051), by name, each with its `versions`, which one is `current` (the newest reviewed; 0 when none is), and `examples` to start from. |
| `POST /v1/runbooks` | admin | `{"name", "description", "kind": "check" or "fix", "script", "params": [{"name": "ENV_NAME", "type": "choice" or "int", "choices", "min", "max", "default"}], "target": {"account", "instances"} or {"tag_key", "tag_value"}, "timeout_seconds", "note"}`: make a runbook. Its first version waits for review. Audited as `runbook.version`. |
| `GET /v1/runbooks/{id}` | developer | One runbook with every version's script, parameters, target, `hash` and `review`. |
| `POST /v1/runbooks/{id}/versions` | admin | The same fields as making one, without name and kind: a new version, which waits for review; the current one keeps running. |
| `POST /v1/runbooks/{id}/review` | admin | `{"version", "hash", "note"}`: approve that version's exact text, which becomes what runs. Refused to the version's author, for another hash, and for a version older than the current one. Audited as `runbook.review`. |
| `POST /v1/runbooks/{id}/run` | developer | `{"params": {"NAME": "value"}, "reason"}`: a check starts at once (202, `running`); a fix needs a reason and is filed for a different administrator (202, `proposed`). Values must match their parameter's type; the instances are found now. Audited as `runbook.run` or `runbook.propose`. |
| `GET /v1/runbook-runs` | developer | The 100 newest runs (`?runbook=` for one), without scripts and output, with what the approval rules ask while one waits. |
| `GET /v1/runbook-runs/{id}` | developer | One run: the exact `script` sent, `params`, `targets`, `hash`, `status` (`proposed`, `running`, `succeeded`, `failed`, `rejected`, `cancelled`, `expired` after 24 hours), and per instance its SSM status, exit code and output (cut, credentials hidden), and `trace_id`. |
| `POST /v1/runbook-runs/{id}/approve` | admin | `{"hash", "note"}`: approve a fix. Refused to whoever asked for it, for another hash, when the runbook changed since, and by the approval rules (kind `runbook`). The approval that completes the count starts it (202). Audited as `runbook.approve` and `runbook.done`. |
| `POST /v1/runbook-runs/{id}/reject` | admin | Reject a fix that is waiting. Audited as `runbook.reject`. |
| `POST /v1/runbook-runs/{id}/cancel` | developer | Withdraw a fix that is waiting: whoever asked, or an administrator. Audited as `runbook.cancel`. |
| `GET /v1/ai/registries` | admin | Registries agents may be pushed to: `name`, `host`, `namespace`, `username`, `has_token`. The token is never returned. |
| `POST /v1/ai/registries` | admin | `{"name", "host", "namespace", "username", "token"}`: add a registry (at most 20). `host` is a registry host such as `ghcr.io` or `127.0.0.1:5000`; plain http is used only for `localhost` and `127.0.0.1`. The token goes into the vault. Audited as `ai.registry.create`. |
| `PUT /v1/ai/registries/{id}` | admin | The same fields. Leave `token` out to keep the stored one; `""` removes it. Waiting pushes to it are refused if its host or namespace changes. Audited as `ai.registry.update`. |
| `DELETE /v1/ai/registries/{id}` | admin | Remove a registry and its token. Audited as `ai.registry.delete`. |
| `POST /v1/ai/registries/{id}/test` | admin | Sign in and read a repository's tags, to check the host, user name and token before a push. `{"ok": false, "error": "..."}` with 502 when it fails. Audited as `ai.registry.test`. |
| `POST /v1/ai/agents/{id}/versions` | admin | Save a new version; skill pins are made exact. |
| `POST /v1/ai/agents/{id}/rollback` | admin | Copy an old version forward as the current one. |
| `POST /v1/ai/agents/{id}/disable` | admin | Turn an agent off (it cannot run or be listed). |
| `POST /v1/ai/agents/{id}/enable` | admin | Turn it back on. |
| `DELETE /v1/ai/agents/{id}` | admin | Deletes an agent by archiving it: turned off, hidden, its reporting line cut and agents that reported to it report to people. 409 naming them while an enabled workflow or an unfinished mission uses it. Deleted for good 30 days later by the hourly sweep (audited as `ai.agent.purge`). Audited as `ai.agent.archive`. |
| `POST /v1/ai/agents/{id}/restore` | admin | Brings an archived agent back before the sweep, still turned off. Audited. |
| `PUT /v1/ai/agents/{id}/reports-to` | admin | Body: `reports_to`, another agent's id, or empty to report to people. A line that would make a loop is refused. Describes the team; it gives no agent power over another. Audited as `ai.agent.team`. |
| `GET /v1/ai/team` | developer | The team for the org chart: each agent with `reports_to`, `mode` (`advises` or `proposes`), `state` (`working`, `waiting` at a workflow gate, `scheduled` with `next_at` and `every_minutes`, `idle`, `off`), `doing` and `run_id`, `workflows`, `steps_week`, `failed_week`, `last_at`; `mission` (the workflow run in progress, else the latest, with its stages); `schedules_stopped` when the assistant is off. |
| `GET /v1/ai/teams` | developer | Teams (note 045): each with `id`, `name`, `purpose`, `lead`, `members` (agent ids) and its newest run as `last_run`. |
| `POST /v1/ai/teams` | admin | Makes a team: `name`, `purpose`, `lead` and 1 to 8 `members` (agents, not the lead, not archived; an agent can be in several teams). `id` is made from the name when left out. Audited. |
| `GET /v1/ai/teams/{id}` | developer | One team and its last 50 runs, newest first. |
| `PUT /v1/ai/teams/{id}` | admin | Changes a team; the same body. Audited. |
| `DELETE /v1/ai/teams/{id}` | admin | Removes the team; its agents and past runs stay. 409 while it is working. Audited. |
| `POST /v1/ai/teams/{id}/schedules` | admin | Give the team a `goal` at set times: `every_minutes` (15 to 10080) or `cron` (five fields, at most every 15 minutes) with `tz` (an IANA zone; empty is UTC), and `run_as` `viewer` or `developer` (never admin). At most 10 per team. A time missed by more than an hour is skipped, not run late; a team already working skips that time. Audited as `ai.schedule.create` (design note 047). |
| `PUT /v1/ai/team-schedules/{sid}` | admin | Change one (the same fields, and `paused`). Audited as `ai.schedule.update`. |
| `DELETE /v1/ai/team-schedules/{sid}` | admin | Delete one; the team's past runs stay. Audited as `ai.schedule.delete`. |
| `GET /v1/ai/schedules` | developer | Every workflow and team schedule: what it is, `describe`, `next` (three times), `last_at`, `last_run` or `last_note` (why it was skipped), `paused`, `off` (why nothing runs now). |
| `POST /v1/ai/schedules/preview` | developer | `{every_minutes}` or `{cron, tz}`: `describe` and the next five times, or 400 saying what is wrong. Saves nothing. |
| `POST /v1/ai/teams/{id}/runs` | developer | Gives the team a `goal` (up to 2000 characters); 202 with the run. The lead plans one task per member it picks (only its own members, once each), the members work three at a time as the person who asked, each with its own model, tools and budget, and the lead writes the `answer` from their output, which it gets as data. 409 when a member is off or the team is already working. Each agent call is audited as `ai.team.step`. |
| `GET /v1/ai/team-runs/{id}` | developer | One run: `status` (`planning`, `working`, `writing`, `done`, `failed`), `plan_note`, `tasks` (member, task, status, output or error, lookups, time), `answer`, `error` and `proposals`. |
| `GET /v1/ai/usage` | developer | What the AI used in a month (`?month=YYYY-MM`, default this one, UTC; note 036): per agent (`who` is its id) and `_people`, with `calls`, `input_tokens`, `output_tokens`, `millis`, `cost_usd` and `costed_calls` (cost is only known where the claude program reports it), and `blind_calls` (calls whose tokens were not reported). |
| `POST /v1/missions` | developer | Start a mission (note 037): `goal` (10 to 2000 characters), `agent`, `account` (default `primary`; the mission may only look at this account), `budget_tokens` (default 200,000, at most 5,000,000). 202 with the mission; it runs in the background. At most 5 at once. Audited. |
| `GET /v1/missions` | developer | Missions, newest first. |
| `GET /v1/missions/{id}` | developer | One mission: `stage` (`discover`, `propose`, `approval`, `verify`, `report`), `status` (`running`, `waiting`, `done`, `failed`, `cancelled`, `stopped`), `steps` (with the lookups each ran), `findings` (each naming the lookup that showed it), `unsupported` (claims dropped for naming no lookup that ran), `plan`, `proposals`, `evidence`, `used`, `report`. |
| `POST /v1/missions/{id}/cancel` | developer | Cancel a mission that has not ended; only the person who started it or an administrator. Proposals it filed stay for the approver to decide. Audited. |
| `PUT /v1/ai/agents/{id}/budget` | admin | Body `tokens`, `minutes`: the agent's monthly limits, 0 for none (both 0 clears the budget). At 80% the console warns; when one is used up the agent's next model call is refused. Audited. Answers its state: `budget`, `used`, `fraction`, `level` (`ok`, `warn`, `stop`). |
| `GET /v1/blueprints` | developer | Blueprints (note 035), newest change first: name, account, region, number of blocks and of warnings, who changed it and when. |
| `POST /v1/blueprints` | developer | Save a new blueprint: `name`, `description`, `target` (`cloud` `aws`, `account`, `region`), `blocks`, `links`. Refused with a message that says where (`links[0]: ...`) when it is not valid, or when the account is not connected. A `resource` block (`resource_type`, `attributes`) must use a type allowed for the account, with settings the provider's schema takes; its links carry `attribute` (the setting they fill), `output` (what they read) and `list`. Audited. Nothing is built. |
| `POST /v1/blueprints/check` | developer | Check a blueprint that is not saved: `error` when it is not valid, else the plain-words `checks`. |
| `GET /v1/blueprints/{id}` | developer | One blueprint with its `revision`, who made and changed it, and its `checks` (`ok` or `warn`). |
| `PUT /v1/blueprints/{id}` | developer | Replace a blueprint. Send the `revision` you started from; 409 if someone saved it since. Audited. |
| `DELETE /v1/blueprints/{id}` | developer | Delete a draft; only its author or an administrator. Audited. Nothing in the cloud changes. |
| `GET /v1/blueprints/{id}/code` | developer | The OpenTofu configuration the blueprint generates (`main.tf.json`, AWS provider `~> 6.72`). |
| `POST /v1/blueprints/{id}/plan` | developer | Preview: run `tofu plan` on the saved blueprint in the background (reads only). 202 with the preview, `status` `planning`; 409 while another preview or build of it runs. Audited. An optional body `{"remove": "keep-data" \| "everything", "final_snapshot": true}` previews removing what it built instead (note 041); the preview's `removal` says which: `keep-data`, `unprotect` (step 1 of everything) or `all` (step 2). 409 when nothing it built is left. |
| `GET /v1/blueprints/{id}/blocks/{block}/delivery` | developer | For an app or agent block with a `source`: the suggested GitHub Actions workflow (`workflow_path`, `workflow`, actions pinned to commit SHAs) that builds its image and pushes it to the block's private ECR repository with the block's push role, a Forgeline pipeline (`pipeline`) that starts it, `image_repository`, `platform` (`linux/arm64` for agents) and `next`. Writes nothing. 404 for an unknown block or one without a source. |
| `POST /v1/blueprints/suggest-source` | developer | `{block}` (an app, agent or Lambda-template block; it need not be saved): up to 5 folders in the repositories on the GitHub allow list whose `Dockerfile` fits it, best first, each with `repo`, `branch` (the default branch when the allow list lets it run, else the first branch it names), `path`, `score` and plain-words `reasons` (its port, a Lambda base image, the AgentCore SDK, arm64, the block's name in the path, the language from manifests beside it). Reads the tree and up to 25 Dockerfiles, 4 folders deep at most, skipping `node_modules`, `vendor` and similar; nothing from the files is returned as text. `repositories` read and `problems` for those that could not be. 400 for a block that runs no code; 409 when GitHub is not connected. |
| `GET /v1/blueprints/{id}/blocks/{block}/images` | developer | For a built blueprint's block with a `source` (an app, an agent, or a template with an `image` input): the newest 20 images in the block's own ECR repository (`repository`), each with `image` (the reference to run: by tag, or by digest when untagged), `tags`, `digest`, `pushed_at`, `size_mb` and `current`. A read in AWS (`ecr describe-images`). 409 when the blueprint is not built, the block has no source or runs no image. |
| `POST /v1/blueprints/{id}/blocks/{block}/image` | developer | `{image, revision}`: saves the blueprint as a new revision with the block running `image`, which must be a tag or `sha256` digest in the block's own repository (400 otherwise; any other image is an ordinary edit). Nothing in AWS changes: preview and send it for approval as usual. 409 on a stale `revision`. Audited as `blueprint.image`. |
| `GET /v1/blueprints/{id}/plan` | developer | The latest preview, or `null`: `status` (`planning`, `ready`, `failed`), `error`, `revision`, `aws_account`, `add`, `change`, `remove`, and `changes` (`address`, `type`, `block`, `action` `add`/`change`/`replace`/`remove`). The plan file itself is never returned. |
| `POST /v1/blueprints/{id}/propose` | developer | Body `reason`. Files the latest preview as a `blueprint-apply` cloud change for a different administrator to approve; refused unless the preview is ready, of the saved blueprint, under an hour old, from the current state and changing something. On approval exactly that plan is applied. |
| `GET /v1/blueprints/{id}/built` | developer | Whether it has been built: `built`, and if so `aws_account`, `region`, `applies`, `updated_at`, `updated_by`. After everything was removed, `built` is false and `removed` true. |
| `GET /v1/built` | developer | Everything Forgeline built and keeps: `blueprints` (each with its blocks, links, the last approved apply, and `resources` from its OpenTofu state: address, type, block, and only the `name`, `id` and `arn`), `stacks` (deployed by an approved change and not deleted since) and `environments` that are up. `?check=1` looks each blueprint resource up in the account's inventory (a read) and sets `present`; kinds the inventory does not list stay unset. |
| `GET /v1/onboarding` | developer | The first-run steps (note 038): `steps` (each `id`, `title`, `why`, `link` to the console page that does it, `done` with a `detail` of what was found, `skipped`), `done`, and `finished` (every step done or skipped). A step is done when what it sets up exists; nothing is ticked by hand. |
| `PUT /v1/onboarding/{step}` | admin | Body `skip`: skip a step, or bring it back (`false`). A step that is done is never shown as skipped. Audited. |
| `POST /v1/blueprints/{id}/assist` | developer | The assistant beside the canvas: `message`, `draft` (what is on screen), `history`. Answers `reply` and, when it edits, a `draft` checked by the save rules, kept on the blueprint's account and region and within the AI service switches, with `changes` in words and `checks`; otherwise `problem` says why none was offered. Nothing is saved. Audited without the text. |
| `GET /v1/plugins` | developer | Whether plugins are on, and the installed plugins with their tools, network and host grants and secret names (never values). |
| `PUT /v1/plugins/settings` | admin | Turn plugins on or off. Turning on checks that Docker answers. |
| `GET /v1/templates` | developer | The library's templates (note 040): `ref` (name@version), `title`, `summary`, `icon`, `holds_data`, `checks`, `inputs` (the JSON Schema subset), `input_order`, and `links` (each role's `name`, `direction`, `kinds`, `access`, `variable`, `shape`). A blueprint block `{"kind": "template", "template": "<ref>", "inputs": {...}}` uses one; inputs are checked against the schema and links against the roles. |
| `GET /v1/templates/{name}/{version}/code` | developer | The template's OpenTofu module (`main.tf.json`). A plan stores the modules it calls and the apply uses exactly those. |
| `GET /v1/build-push-rules` | admin | Push rules for build steps (note 043): `id`, `repo`, `branches`, `target` (an ECR repository), `account` (the Forgeline AWS account that pushes), `updated_by`, `updated_at`. |
| `PUT /v1/build-push-rules/{id}` | admin | Creates or replaces a rule: `repo` (owner/name), `branches` (1 to 10 names or patterns; not a bare `*`, not `refs/...`), `target` (`<account>.dkr.ecr.<region>.amazonaws.com/<name>`, no tag), `account` (default `primary`). Audited. Only runs GitHub starts for a push to a matching branch of `repo` may push to `target`. |
| `DELETE /v1/build-push-rules/{id}` | admin | Removes a rule. Images already pushed stay. |
| `GET /v1/mcp-servers` | admin | The MCP servers an administrator added (note 042): `id`, `name`, `kind` (`remote` or `local`), `url` or `image`, the vault secret names it uses (never values), `for_assistant`, `protocol` (the MCP revision it spoke), and `tools` with `read_only`, `destructive`, `writes`, `marked_read_only_by`, `markable` (it could be turned on if an administrator marks it read-only), `enabled`, `pinned` and `problem`. |
| `POST /v1/mcp-servers` | admin | Adds a server: `id`, `name`, `kind`; remote: `url` (https; http only for loopback), optional `auth_header`, `auth_scheme` and `auth_secret` (a vault secret name), `allow_private`; local: `image` pinned by digest, `network` or `hosts`, `secrets` (environment variable to vault secret name). `run_as_image_user` (local) runs the container as the image's own user instead of nobody; `for_assistant` offers its tools to the assistant. Audited. |
| `PUT /v1/mcp-servers/{id}` | admin | Changes a server. Changing its address or image turns every tool off until its tools are discovered again. |
| `DELETE /v1/mcp-servers/{id}` | admin | Removes a server and its tools. |
| `POST /v1/mcp-servers/{id}/discover` | admin | Asks the server for its tools (MCP 2026-07-28, falling back to the 2025-11-25 handshake for older servers). A tool that was on and whose definition changed is turned off with a reason. 502 with the server's error when it cannot be reached. |
| `PUT /v1/mcp-servers/{id}/tools` | admin | `{"enabled": ["tool", ...], "read_only": ["tool", ...]}` turns those tools on, pinning their definitions, and every other tool off. A tool can be turned on when its server marks it read-only, or when it is also in `read_only`: the administrator checked that it only reads (named as `marked_read_only_by`, and dropped when its definition changes; send it again with every save). A tool its server marks `destructive`, or says `writes` (`readOnlyHint: false`, as opposed to no hint), cannot be turned on or marked, nor one whose arguments Forgeline cannot check (simple types and lists of them). Enabled tools appear as `mcp_<server>_<tool>` for agents (when added to their tools) and, for servers marked `for_assistant`, for the assistant; every call is audited. |
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
| `GET /v1/infra/aws/resources` | viewer | Every resource in the account by service (ECS, EC2, S3, Lambda, RDS and more), read-only; `?account=<id>` picks the account; `?refresh=1` skips the 60 s cache. `?service=<id>` answers with that one service only, read with its own calls (and its own cache entry) when the whole inventory was not read lately, so one service never waits for the others; 404 for an unknown id. |
| `GET /v1/infra/aws/services` | viewer | The services the explorer can list (`id`, `label`, `category`, `global`), with the account's region. No AWS call: the Services page draws from it at once and asks `?service=` for each tile. |
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
| `PUT /v1/integrations/assistant` | admin | Body: `backend` (`off`, `api`, `claude-token`, `cli`, `copilot`, or `provider` with a `model` written `provider/model`), `model`, `api_base`, `api_key` and `claude_token` (both write-only; the token is stored as the secret `CLAUDE_CODE_OAUTH_TOKEN`). |
| `POST /v1/integrations/assistant/test` | admin | Ask it something small to check the setup. |
| `GET /v1/ai/proposals` | viewer | Changes the assistant drafted, newest first, with their status. |
| `GET /v1/ai/proposals/{id}` | viewer | One proposal: the exact script, the AWS calls it makes, risk, and (after approval) what ran. |
| `POST /v1/ai/proposals/{id}/approve` | admin | Body: `hash` (of the text you read), `note`. Not allowed for the person who proposed it. Applies the approved script through the AWS MCP Server and returns 202; poll the proposal. |
| `POST /v1/ai/proposals/{id}/reject` | admin | Rejects a waiting proposal. |
| `POST /v1/ai/proposals/{id}/cancel` | developer | The proposer or an administrator withdraws a waiting proposal. |
| `POST /v1/assistant/chat` | developer | Body: `message`, `history`, `context` (the page: run, pipeline, infra or overview). Returns `reply`, the read-only `lookups` it ran any `proposals` it filed and any `environments` it asked for (requested, never started: a different administrator approves). 20 a minute per person. |
| `POST /v1/assistant/runs` | developer | The same body, checks, limits and audit entries as `/v1/assistant/chat`, answered as server-sent events that follow the [AG-UI](https://docs.ag-ui.com/) vocabulary: `RUN_STARTED`, `CUSTOM forgeline.context`, then for each lookup `TOOL_CALL_START`, `TOOL_CALL_ARGS`, `TOOL_CALL_END`, `TOOL_CALL_RESULT` (a result cut at 2 KB, or the reason it was refused) and `CUSTOM forgeline.tool_status`; `CUSTOM forgeline.proposal` or `forgeline.environment` when one was filed; `TEXT_MESSAGE_START/CONTENT/END` with the whole answer in one piece; and `RUN_FINISHED` or `RUN_ERROR`. Checks that fail first (rate limit, bad body, assistant off) are ordinary JSON errors. Closing the connection cancels the model call. |
| `POST /v1/assistant/approvals/{id}` | developer | Answer a question the assistant stream is waiting on: body `{"decision":"approve"}` or `{"decision":"deny"}`. When the access policy says a read tool needs approval, `/v1/assistant/runs` sends `CUSTOM forgeline.approval` (`id`, `tool`, `integration`, `args`, `expiresInSeconds`) before the tool, waits, and sends `CUSTOM forgeline.approval_decided` (`approve`, `deny` or `expired`). Only the person who asked can answer, once; anything else is 404. No answer in two minutes is a denial. Both outcomes are audited as `policy.approval` against the integration. |
| `POST /v1/assistant/questions/{id}` | developer | `{"answer": "..."}` (1 to 1,000 characters): answer a question the assistant stopped to ask on your chat stream (`ask_user`, design note 052). Only the person who is asked, only for a question that is waiting; an approval id is not accepted here. |
| `GET /v1/assistant/memory/files` | developer | The memory files you may read (design note 052): yours under `/memories/me` and the organisation's under `/memories/org`, with `content`, who wrote it, `pending` (written after untrusted data, not yet kept) and `can_edit`. |
| `PUT /v1/assistant/memory/files` | developer | `{"path": "/memories/me/x.md", "content": "..."}` writes a file as yourself, or `{"path": ..., "keep": true}` keeps a pending one. `/memories/org` needs an administrator. Refuses markup, secrets and files over 8000 characters. |
| `DELETE /v1/assistant/memory/files` | developer | `?path=/memories/me/x.md`: delete one file of your own (an administrator may delete the organisation's). |
| `GET /v1/assistant/autopilot` | developer | Whether autopilot is `allowed`. It has no default limit. The chat turns it on by sending `"autopilot": {}` (or `{"minutes": 60, "steps": 200}` for an optional limit) with a message to `POST /v1/assistant/runs`. |
| `PUT /v1/assistant/autopilot` | admin | `{"allowed": false}` turns autopilot off for everyone. Audited as `assistant.autopilot.settings`. |
| `POST /v1/assistant/autopilot/start` | developer | `{"conversation": "...", "minutes": 0, "steps": 0}` (limits optional): switch autopilot on in a conversation that is already open, also while a message is being worked on; requests the agent is waiting on in the sandbox are answered yes. Audited as `assistant.autopilot.start`. |
| `POST /v1/assistant/autopilot/stop` | developer | `{"conversation": "..."}`: end your autopilot in that conversation at once. Audited as `assistant.autopilot.stop`. |
| `GET /v1/assistant/sandbox` | developer | The assistant's sandbox (design note 052): `enabled`, `network` (`internet` or `none`), the `runtime` that isolates it (gVisor when Docker has it, otherwise a hardened container that shares the host kernel), whether Docker answers, your own containers, and the idle time after which they are removed. |
| `PUT /v1/assistant/sandbox` | admin | `{"enabled": true, "network": "internet"}`: turn the sandbox on (it is off until then) or choose `none` for no network. Audited as `assistant.sandbox.settings`. |
| `POST /v1/assistant/sandbox/reset` | developer | Remove your own sandbox containers and their `/work` folder. Audited as `assistant.sandbox.reset`. |
| `GET /v1/assistant/permissions` | developer | What the assistant may do as you (design note 052): the organisation rules and your own (`rules`: `scope`, `tool`, `pattern`, `effect` of `allow`, `ask` or `deny`), and each AWS account with `own_approval`. |
| `POST /v1/assistant/permissions/rules` | developer | `{"scope": "person" or "org", "tool": "call_aws", "pattern": "aws ecs update-service *", "effect": "allow", "note": ""}`. `*` matches any characters. An `allow` must name its service and operation, and never covers `iam`, `organizations`, `account`, `ssm` or `sts`. An organisation rule needs an administrator. At most 100 for you and 100 for the organisation. Audited as `assistant.permission.rule`. |
| `DELETE /v1/assistant/permissions/rules/{id}` | developer | Remove your own rule, or (administrator) an organisation rule. Another person's rule answers 404. |
| `PUT /v1/assistant/permissions/accounts/{id}` | admin | `{"own_approval": true}`: let the assistant change this AWS account after a person approves the exact command in the chat; `false` (the default) keeps it protected, so a change is a proposal that a different administrator approves. `id` is `primary` or an added account. Audited as `assistant.permission.account`. |
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

## Models and providers

Design note 045, part 1. A model from a provider is written `provider/model` (for example `anthropic/claude-haiku-5-5`)
wherever an agent or the assistant is given one, and works whatever the assistant's own backend is. The `anthropic`,
`openai`, `openai-compatible`, `gemini` (through Google's OpenAI-compatible endpoint), `azure-openai` (the v1 API; a
`base_url` such as `https://my-resource.openai.azure.com/openai/v1` is required, the key goes in the `api-key` header,
and models are deployment names) and `bedrock` kinds are built. A `bedrock` provider has no key or address: it takes
`aws_account` (an added account's id; empty for the primary) and an optional `region`, and calls the Converse API with
that account's credentials, whose role needs `bedrock:InvokeModel`. An `openai-compatible` provider (Ollama, vLLM, LiteLLM, OpenRouter,
Groq…) needs a `base_url` ending in its version path, such as `http://localhost:11434/v1`; its `api_key` is optional and
its models are added by name (which may contain `/`, as in `local/meta-llama/Llama-3.3-70B`).

| Method and path | Role | Purpose |
| --- | --- | --- |
| `GET /v1/providers` | developer | `providers`: each with `id`, `kind`, `name`, `base_url`, `allow_private`, `models` (`id`, `label`, `on`, `input_usd_per_mtok`, `output_usd_per_mtok`), `monthly_usd`, `fallback`, `has_key` (never the key), `month` (calls, failed calls, tokens and cost this month), `median_millis`, `status` (`working`, `failing`, `untested`, `no-key`) and `last_error`. `kinds`: every planned kind with `built` and the known models and list prices. |
| `POST /v1/providers` | admin | Adds a provider: `id`, `kind`, `name`, `api_key` (write-only, kept in the vault), optional `base_url` (https; http only for loopback; private and reserved addresses refused unless `allow_private`), `models`, `monthly_usd` (0 for no limit) and `fallback` (`provider/model`, used when the limit is reached or the provider is unreachable, rate limited or overloaded; once per call). Audited. |
| `PUT /v1/providers/{id}` | admin | Changes a provider; the same body without `id` and `kind`. An empty `api_key` keeps the stored key. Audited. |
| `DELETE /v1/providers/{id}` | admin | Removes a provider and its key. Agents that use its models fail with a clear error until they are given another. Audited. |
| `POST /v1/providers/{id}/test` | admin | Asks a model (body `model`, or the first one turned on) to reply with one word. Returns `reply`, `model`, `millis`, `cost_usd`, or 502 with the provider's error. |
| `GET /v1/models` | developer | The models turned on, for pickers: `ref` (`provider/model`), `provider`, `provider_name`, `kind`, `model`, `label` and prices. |
| `GET /v1/model-calls` | developer | The last 200 model calls, newest first: `at`, `provider` (a provider's id, or the assistant's backend), `model`, `who`, tokens, `cost_usd`, `millis` and `error`. `?provider=` keeps one provider's calls. |

## The pipeline schema

`forgeline schema` prints the JSON Schema; the same file is in the repository at
[`schema/pipeline.v1alpha1.schema.json`](https://github.com/forgelinehq/forgeline/blob/main/schema/pipeline.v1alpha1.schema.json).
