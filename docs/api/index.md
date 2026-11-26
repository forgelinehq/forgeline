---
title: API reference
nav_order: 4
has_children: true
permalink: /api/
---

# API

Everything the console does goes through this REST API, so anything you can click you can script.
Bodies are JSON unless noted. Times are RFC 3339 UTC. Every route, its minimum role, its fields and
its limits are in the [endpoint reference](reference.md).

{: .note }
Forgeline is pre-alpha. A test keeps the list of routes and roles in the reference true to the code; the field lists
are written by hand from the handlers and can lag. The examples on this page were captured from a running server
(ids, times and tokens differ on yours).

## Authentication

A `/v1` request is authenticated in one of two ways. `POST /mcp` accepts neither: it takes a personal API token only, see below.

**Server token** (scripts, CI, the CLI). Send `Authorization: Bearer <token>`. The token is the one the server was
started with (`FORGELINE_TOKEN`; on a loopback address without it, Forgeline makes one and prints it once). It counts as
an administrator named `token`. The comparison is constant time; any other bearer value, including a personal API token,
is `401` on the `/v1` endpoints.

```sh
export FL=http://127.0.0.1:8080 TOKEN=...   # the server token
curl -s -H "Authorization: Bearer $TOKEN" $FL/v1/runs
```

**Session cookie** (the console). `POST /v1/auth/login` sets `forgeline_session`: 12 hours, `HttpOnly`,
`SameSite=Strict`, and `Secure` when the request came over HTTPS. A request that changes state (anything but `GET`,
`HEAD`, `OPTIONS`) and uses a cookie must also send `X-Forgeline-Request: 1` (else `403` "missing X-Forgeline-Request
header"), and if it carries an `Origin` header it must match the host (else `403` "cross-origin request refused").
Bearer requests need neither.

```sh
curl -s -c jar -H 'Content-Type: application/json' -H 'X-Forgeline-Request: 1' \
  -d '{"username":"dana","password":"..."}' $FL/v1/auth/login
```

A few things need a person and not a token: `POST /v1/tokens` (a token cannot make tokens) and `POST /v1/me/password`
(the server token has no password). The first administrator is made with `POST /v1/auth/setup`, which needs the server
token in its body.

Failed sign-ins are throttled: after 5 failures for one name or one address the next attempts get `429` with a
`Retry-After` header (30 seconds, doubling up to 15 minutes).

### Personal API tokens and MCP

A person can make personal API tokens in the console (Account, API tokens) or with `POST /v1/tokens` from a browser
session: `flt_...`, shown once, up to 10 per person, 1 to 365 days (default 90), with a role no higher than the person's.
The token's effective role is the lower of its own and its owner's current role; disabling or deleting the owner stops it.
Tokens are stored as a hash.

A personal token works on one endpoint, `POST /mcp`, which is Forgeline as an MCP server (stateless streamable HTTP,
one JSON-RPC 2.0 message per request; protocol revisions 2025-06-18, 2025-03-26 and 2024-11-05). It exposes the
read-only lookups the assistant has, plus propose-only tools; nothing it offers applies or approves a change. It
allows 60 calls a minute and 4 at once per token; over that, a call returns a tool result with `isError: true`.
A bad or missing token is `401`, and design note 020 has the rest.

```sh
curl -s -H "Authorization: Bearer $FLT_TOKEN" -H 'Content-Type: application/json' $FL/mcp \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}'
```

```json
{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"tools":{"listChanged":false}},"instructions":"Forgeline: read what runs and what Guard found, and propose changes. A proposal runs only after a different administrator approves it in the console.","protocolVersion":"2025-06-18","serverInfo":{"name":"forgeline","version":"1"}}}
```

`tools/list` shows the tools this token's role and the server's connections allow.

## Roles

Each endpoint needs a minimum role: **viewer** < **developer** < **admin**. The [reference](reference.md) gives it for each
route. A role that is too low gets `403` and the refusal is written to the audit log (`access.denied`):

```json
{"error":"your role (developer) cannot do this; admin is required"}
```

## Requests

- An endpoint that reads a JSON body needs `Content-Type: application/json` (else `415` "send Content-Type:
  application/json"), accepts at most 1 MiB (`413`), and rejects a field it does not know
  (`400` `invalid JSON: json: unknown field "yam"`). Send `{}` when there is nothing to say; an empty body is
  invalid JSON. Pipeline submission (`POST /v1/runs`) is `multipart/form-data` instead.
- Secrets are write-only everywhere: no response contains a secret value, an API key or a password hash.

## Errors

Failures return a status code and `{"error": "what went wrong, in words you can act on"}`. A pipeline that does not
validate adds `problems`, each with the `path` of the field and a `msg`.

| Code | Meaning |
| --- | --- |
| 400 | The body or a value is not acceptable. The message says which. |
| 401 | No or wrong credentials (`sign in required`, with `WWW-Authenticate: Bearer`). |
| 403 | Signed in, but the role is too low, the request needs a browser session, or the CSRF checks above failed. |
| 404 | No such object. Someone else's private object (a conversation, a token) is `404`, not `403`. |
| 409 | The request conflicts with the current state: a pipeline saved by someone else since you opened it, a change that is no longer waiting, a limit reached, an integration that is not connected. |
| 413 | The body, a pipeline or an upload is larger than the limit. |
| 415 | The body is not `application/json`. |
| 422 | A connection could not be proved (an AWS profile, a cluster); the message says what to fix. |
| 429 | Too many attempts or questions, see Rate limits. |
| 502 | A system Forgeline called (GitHub, AWS, a model provider, a registry) refused or could not be reached. The message carries its answer. |
| 503 | The server is busy with a limited resource (for example too many source downloads at once); try again. |

`internal error: ...` with `500` is a server fault; the server log has the cause.

## Rate limits

| What | Limit | Answer |
| --- | --- | --- |
| Sign-in and first-admin setup | 5 failures, then 30 s doubling to 15 min, per name and per address | `429`, `Retry-After` |
| Assistant chat, blueprint assistant, agent chat | 20 a minute per person | `429` "slow down: the assistant allows 20 questions a minute" |
| GitHub Actions dispatch, cancel, rerun | 20 a minute per person | `429` "too many actions; wait a minute" |
| Webhook-started workflows | 30 runs an hour per workflow | `429` |
| `POST /mcp` | 60 calls a minute and 4 at once per token | a tool result with `isError: true` |

Reads of the cloud are cached for 60 seconds (`?refresh=1` skips the cache on the inventory endpoints), so polling them
faster gains nothing.

## Common patterns

**Lists** answer as an object with a named array: `{"runs": [...]}`, `{"pipelines": [...]}`, `{"events": [...]}`.
There is no shared pagination. Runs use a cursor: `GET /v1/runs?limit=50` returns `next_before` when the page is full;
pass it as `before` for the older page (`limit` is at most 200). The audit log does the same with `next` and `before`
(`limit` up to 1000). Most other lists return everything or a fixed newest-N (the reference says which).

**Archiving.** `DELETE` on an agent or a workflow archives it: turned off and hidden, and restorable for 30 days with
`POST .../restore`, after which an hourly sweep removes it. `GET /v1/ai/agents?archived=1` and
`GET /v1/ai/workflows?archived=1` list the archived ones. Pipelines, secrets, users and tokens are deleted at once
(`204`).

**Jobs that answer `202`.** A `202` means the work was accepted and runs in the background. The body is the object in its
first state and its `id` is what you poll with the matching `GET` until the status is final:

| Start | Poll | Moving through | Final |
| --- | --- | --- | --- |
| `POST /v1/runs`, `POST /v1/pipelines/{id}/run` | `GET /v1/runs/{id}` | `queued`, `running` | `succeeded`, `failed`, `canceled` |
| `POST /v1/cloud/changes` | `GET /v1/cloud/changes/{id}` | `preparing` | `proposed` (waits for a person), `failed` |
| `POST /v1/cloud/changes/{id}/approve` | the same | `executing` | `succeeded`, `failed` |
| `POST /v1/guard/scan` | `GET /v1/guard/scans` | `running` | `done`, `failed` |
| `POST /v1/missions` | `GET /v1/missions/{id}` | `running`, `waiting` | `done`, `failed`, `cancelled`, `stopped` |
| `POST /v1/ai/workflows/{id}/run` | `GET /v1/ai/workflow-runs/{id}` | see the reference | |
| `POST /v1/ai/teams/{id}/runs` | `GET /v1/ai/team-runs/{id}` | `planning`, `working`, `writing` | `done`, `failed` |
| `POST /v1/runbooks/{id}/run` | `GET /v1/runbook-runs/{id}` | `proposed`, `running` | `succeeded`, `failed`, `rejected`, `cancelled`, `expired` |
| `POST /v1/blueprints/{id}/plan` | `GET /v1/blueprints/{id}/plan` | `planning` | `ready`, `failed` |

The `Location` header on a run points at its `GET`. Step logs can be followed live instead of polled: send
`Accept: text/event-stream` to `GET /v1/runs/{id}/steps/{step}/logs`.

**Approvals.** A change to production is never applied by the request that asks for it. It is filed as a proposal
(`/v1/cloud/changes`, `/v1/ai/proposals`, `/v1/runbook-runs`, `/v1/ai/pushes`) and applied when a different
administrator approves, or as many as the approval rules need. The approver cannot be the proposer (`403`). For AI proposals, runbook fixes and agent pushes
the approval also carries the hash of the text the approver read, and a changed text is refused.

**Audit.** Writes and refusals are recorded. Read them with `GET /v1/audit` (administrators): `actor`, `action`
(prefix), `denied=1`, `from`, `to`, `limit`. For example `GET /v1/audit?action=access.denied&limit=2` lists the last two
refusals.

## Quick tour

```sh
H="Authorization: Bearer $TOKEN"; J='Content-Type: application/json'

# Check a pipeline without saving it
curl -s -H "$H" -H "$J" $FL/v1/pipelines/validate \
  -d '{"yaml":"version: v1alpha1\nname: demo\nsteps:\n  - id: a\n    image: alpine:3.24\n    run: echo hi\n"}'

# Save it
curl -s -H "$H" -H "$J" $FL/v1/pipelines \
  -d '{"yaml":"version: v1alpha1\nname: demo\nsteps:\n  - id: a\n    image: alpine:3.24\n    run: echo hi\n","description":"first"}'

# Run it and read a step's log
curl -s -X POST -H "$H" $FL/v1/pipelines/demo/run
curl -s -H "$H" $FL/v1/runs/RUN_ID/steps/a/logs

# Add a secret (write-only)
curl -s -X PUT -H "$H" -H "$J" $FL/v1/secrets/REGISTRY_TOKEN \
  -d '{"value":"s3cret-value","description":"registry push"}'
```

## Worked examples

### Save a pipeline, run it, read the result

Validate first. A bad pipeline is still `200`, with `valid: false` and the problems:

```sh
curl -s -H "$H" -H "$J" $FL/v1/pipelines/validate -d '{"yaml":"version: v1alpha1\nname: demo\nsteps:\n  - id: a\n    run: echo hi\n"}'
```

```json
{"valid":false,"problems":[{"path":"steps[0].image","msg":"is required and must not contain whitespace, got \"\""}],"missing_secrets":[],"warnings":[]}
```

With a valid pipeline the answer lists the graph (`"valid":true,"name":"demo","problems":[],"graph":{"steps":[...],"layers":[["a"]]}`).
Save it (`201`):

```sh
curl -s -H "$H" -H "$J" $FL/v1/pipelines -d '{"yaml":"version: v1alpha1\nname: demo\nsteps:\n  - id: a\n    image: alpine:3.24\n    run: echo hi\n","description":"first"}'
```

```json
{"id":"demo","name":"demo","description":"first","yaml":"version: v1alpha1\nname: demo\nsteps:\n  - id: a\n    image: alpine:3.24\n    run: echo hi\n","version":1,"created_at":"2026-11-26T15:21:29.887873302Z","created_by":"token","updated_at":"2026-11-26T15:21:29.887873302Z","updated_by":"token"}
```

Run it. The answer is `202` and the run is only `queued`:

```sh
curl -si -X POST -H "$H" $FL/v1/pipelines/demo/run
```

```
HTTP/1.1 202 Accepted
Content-Type: application/json
Location: /v1/runs/2bff87ff5e2e4e85aed0

{"id":"2bff87ff5e2e4e85aed0","pipeline":"demo","status":"queued","trigger":"ui","trust":"trusted","triggered_by":"token","pipeline_id":"demo","pipeline_version":1,"created_at":"2026-11-26T15:21:43.002464518Z","steps":[{"id":"a","status":"pending"}]}
```

Poll the run until `status` is `succeeded`, `failed` or `canceled`, then read the step's log (plain text, secrets shown
as `***`):

```sh
curl -s -H "$H" $FL/v1/runs/2bff87ff5e2e4e85aed0
curl -s -H "$H" $FL/v1/runs/2bff87ff5e2e4e85aed0/steps/a/logs
```

```json
{"id":"2bff87ff5e2e4e85aed0","pipeline":"demo","status":"succeeded","trigger":"ui","trust":"trusted","triggered_by":"token","pipeline_id":"demo","pipeline_version":1,"created_at":"2026-11-26T15:21:43.002464518Z","started_at":"2026-11-26T15:21:43.002777657Z","finished_at":"2026-11-26T15:21:44.757067455Z","steps":[{"id":"a","status":"succeeded","started_at":"2026-11-26T15:21:43.004190855Z","finished_at":"2026-11-26T15:21:44.757058498Z"}],"trace_id":"fb17e3130cd27503f35e7d9f35a27bf6"}
```

```
hi
```

Saving again with `base_version` set to a version that is no longer current is refused with `409`, and the body carries
the pipeline as it is now in `current`, so a script can merge and retry:

```json
{"current":{"id":"demo","name":"demo","version":1,"...":"..."},"error":"this pipeline changed (now version 1, by token) since you opened it"}
```

### Find out why a run failed

For a failed run, `GET /v1/runs/{id}/failure` names the first step that failed, its exit code, likely causes and the end
of its output, so a script does not have to parse the log:

```sh
curl -s -H "$H" $FL/v1/runs/RUN_ID/failure
```

```json
{"failed":true,"step":"t","headline":"Step t failed with exit code 127","exit_code":127,"causes":["Exit code 127 means the shell could not find the command. Use an image that has the tool."],"skipped":[],"tail":"sh: nosuchcmd: not found"}
```

To list that pipeline's recent runs, 1 at a time: `GET /v1/runs?pipeline=broken&limit=1` returns `{"runs": [...], "next_before": "<id>"}`.
The cursor is present because the page was full, not because older runs are known to exist.

### Create an API token from a session and use it

Tokens are made from a browser session, so log in with a cookie first. The reply shows the secret once:

```sh
curl -s -b jar -H 'Content-Type: application/json' -H 'X-Forgeline-Request: 1' $FL/v1/tokens \
  -d '{"name":"ci","role":"developer","days":30}'
```

```json
{"id":"<sha-256 of the token>","user_id":"dana","owner":"dana","name":"ci","role":"developer","created_at":"2026-11-26T15:23:04.440768896Z","expires_at":"2026-12-26T15:23:04.440768896Z","token":"flt_<shown once>"}
```

That token is for `/mcp` (above), not for `/v1`: used as a bearer token on `GET /v1/runs` it answers
`401 {"error":"sign in required"}`. A token or the server token trying to make a token gets
`403 {"error":"sign in to the console as a person to create a token"}`.
