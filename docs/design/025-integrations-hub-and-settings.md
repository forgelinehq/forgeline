# 025: Integrations hub, settings area, and the next clouds

Status: **Decided 2026-11-02. Slice S1 (the hub) is being built; the rest is design only.** Builds on notes
006 (console), 009/010 (AWS and GitHub sign-in), 015/016 (AWS access for the assistant), 017 (accounts,
agents, environments, marketplace), 018/019 (plugins and signed packages), 022 (redesign), 023 and 024.
Facts about the code are from `origin/main` at `0c143e6`. The visual plan with a clickable prototype was
reviewed before this note; it is a design aid and is not part of the repository.

## Question

Connections live in four places (AWS and Docker under Infrastructure, GitHub and Claude on a page the
sidebar files under "Cloud", Kubernetes on its own page, plugins in the Marketplace). An administrator cannot
see what is connected, what is broken, or what each connection lets the AI do. We also want GCP, Azure and
AI-ready providers (cloud model services, GPU clouds), and more AI features that need a stronger notion of
"what may this agent do with this connection". How do we structure this so that adding a system is a small
change and the safety rules stay the same?

## Decisions (approved by the maintainer, 2026-11-02)

| Decision | Choice |
| --- | --- |
| Model | Four layers for every integration: **catalog** (what can connect), **connections** (what is connected, with a status and a time), **policy** (what each tool may do) and **activity** (what happened, including refusals) |
| Where | A **Settings** area behind a gear in the sidebar (Workspace, Connections, AI, Security, Developer). Integrations is its hub. People, Audit log and Secrets move into it. Proposals is renamed Approvals. ⌘K searches pages, settings and integrations. Old addresses redirect |
| How a person connects | **A popup** over the hub, the same for every system: find or choose, sign in, test, scope, done. A connected system opens the same popup with Overview (and a Test button), Access, Activity and Settings. The popup has its own address |
| The test | Runs the integration's declared checks one at a time, each with a result, a time and a fix. It only reads: it never creates, launches or changes anything, and says so. A warning does not block connecting. The same checks run on a timer, so "needs attention" is real |
| Statuses | Connected, Needs attention (with what to do and when it was checked), Detected here (found on the server, not connected), Coming soon (next or later). A planned system has no Connect button. A broken connection is kept, not deleted, and names what depends on it |
| Detection | Forgeline may list existing logins on the server by **name only**, never a credential: AWS profiles, kubeconfig contexts, the Docker socket. An administrator confirms each one |
| Policy | Per tool: **Allow, Needs approval, Blocked**, with a ceiling per connection and an agent's own grant; **the stricter wins**, and a view shows the effective access and which rule decided. A write tool can **never** be Allow: a change stays a proposal that a different administrator approves. New tools start at Needs approval, reads at Allow |
| Trust levels | Built in, Verified (signed manifest, note 019), Community (blocked until an administrator allows it). Community is built last |
| Read-only | Each integration states **who enforces read-only**: the provider (a role) or Forgeline alone. Where a provider offers no read-only keys (Lambda and RunPod, per their docs), Forgeline calls only an allow-listed set of read endpoints, stores the key write-only, warns in the test and recommends an expiry |
| Order | Hub and Settings, then policy, then the provider interface, then **GCP**, then **Azure**, then **AI infrastructure**, then Datadog, then the rest |
| AI infrastructure first | (a) show what you have: instances, GPU types, spend, idle GPUs; (b) supply models to agents, through the cloud connections and a generic OpenAI-compatible endpoint. Launching GPUs through proposals with cost caps comes later; deploying to inference platforms later still |
| First GPU clouds | Lambda, then Nebius, then CoreWeave as a preset on the Kubernetes integration. RunPod targets REST v2 only |
| First AI features | Approver copilot, AI inbox, idle GPU guard, agent activity and controls (each gets its own note) |

## The manifest

An integration describes itself, so the hub, the wizard, the policy and the health check need no new screen
per system. Fields that exist from S1 are marked **now**; the rest arrive with the slice named.

| Field | Meaning | When |
| --- | --- | --- |
| `id`, `name`, `category`, `tier` | Identity and trust level (built-in, verified, community) | now |
| `status` inputs | How its state is read without a network call (settings, stored last test) | now |
| `checks` | The read-only checks the test runs, in order | now |
| `read_only_enforced_by` | `provider` or `forgeline` | now (shown), enforced with S3 |
| `auth` | Methods, which secrets, whether an expiry is recommended | S1 wizard |
| `tools` with `risk` read or write (and `cost`) | What agents can call; drives the policy | S3 |
| `api.version`, `api.sunset` | So the hub can warn before a provider retires a version (RunPod REST v1 ends 2026-11-15) | S4 |
| `network` | Addresses the connector may reach | plugins, S7+ |
| `feeds` | Which features use it | later |

Compatible in spirit with the MCP registry's `server.json` (packages for local servers, remotes for hosted
ones, secret headers), so a registry entry can later become a Community catalog item. That compatibility is
not verified against the schema.

## What other platforms do (checked in their own docs unless noted)

- **Datadog:** statuses Detected, Installed, Available and Missing Data; autodetection; a Configure tab; a
  separate "Integrations Manage" permission; officially supported integrations kept apart from community
  ones. The page layout is not described in the docs read.
- **Anthropic (Claude connectors):** per role a connector is Always allow, Needs approval, Blocked or
  Custom with per-tool levels; the organization policy is a ceiling and the stricter setting wins; a view
  shows effective access and which role granted it; new roles default to Needs approval (the page also calls
  defaults permissive, and does not mention auditing changes).
- **Google Workspace:** apps are Trusted, Limited or Blocked; a block beats an allow-list; a list of apps
  people used that nobody configured.
- **Connection platforms (from search summaries):** keep the record when a credential breaks, store a status
  with a time, retry refresh then mark inactive, offer a "needs attention" filter and one-click reconnect.

## AI-ready providers (what the docs say; see the plan page for sources)

- **Lambda:** Bearer key, optional expiry, no scoped or read-only keys mentioned; read endpoints for
  instances, instance types, filesystems and firewall rules; launch and terminate are POST calls; about one
  request per second and one launch per 12 seconds. No billing endpoint seen.
- **RunPod:** API key; **REST v1 retires 2026-11-15**, v2 exists; categories include pods, serverless
  endpoints, volumes, templates and billing. Key scopes and rate limits not stated on the page read.
- **CoreWeave:** Kubernetes (CKS) with RBAC; IAM is default-deny with Viewer roles. How a program gets a
  token and any billing API were not found.
- **Nebius:** CLI `nebius`, REST `api.nebius.cloud` with a Bearer token, SDKs, service accounts; usage
  export (CSV, FOCUS). A viewer role, a usage API and a model service were not confirmed.
- **Modal:** workspace API tokens; web endpoints are unauthenticated by default. No scoped tokens or usage
  API found.
- **Not read:** Crusoe, Vast.ai, Fluidstack, Together, Fireworks, Groq, Baseten, Replicate, Hugging Face,
  Cloudflare, OpenRouter, LiteLLM. Prices and rankings in vendor roundups disagree.

## Order of work (each its own pull request)

1. **S1 Hub.** `GET /v1/integrations` lists every integration with status, a plain sentence, what to do
   next, when it was last tested, and who enforces read-only; includes planned systems and detected logins.
   `POST /v1/integrations/{id}/test` runs the checks (read-only). The console gets the hub page with
   cards, filters and the popup (Overview with Test, and a link into the existing settings form). Built in
   slices: this note; the summary and test endpoints; the hub page; moving the existing forms into the popup.
2. **S2 Settings area:** gear, left menu, ⌘K, redirects, People/Audit/Secrets moved, Proposals renamed.
3. **S3 Access policy:** the per-tool model above, enforced on the server where lookups run, with the
   effective access view. Its own note before code; every rule removed in turn to confirm a test fails.
4. **S4 Provider interface and one resource model:** AWS ported with its tests unchanged; the explorer gets a
   provider switch.
5. **S5 Google Cloud** (read-only; Cloud Asset Inventory) then **S6 Azure** (read-only; Resource Graph),
   each with its AI services in the explorer (Vertex AI, Foundry).
6. **S7 Models:** cloud model services through the cloud connection, plus a generic OpenAI-compatible
   endpoint with per-agent budget.
7. **S8 GPU clouds, read-only:** Lambda, Nebius, CoreWeave preset, RunPod v2; the idle GPU guard.
8. **S9 Datadog**, Slack and Teams; **S10** GPU launches through proposals with cost caps (own note);
   community catalog, registries, GitLab.

## Rules that do not change

- Anything that changes the outside world is a proposal that a different administrator approves (note 006).
  Nothing in this note adds a way to write. A GPU launch, when it comes, is a proposal with a cost cap.
- Secrets are write-only and never reach an agent. The hub never shows or logs one.
- The console builds markup from text nodes only (`ui/lib/dom.js`); no `innerHTML`, no inline styles.

## What was checked, and what was not

Checked: the code facts above by reading it; the competitor and provider facts in their own documentation
where marked; the visual plan in a browser against example data.

Not checked: anything about a real GCP, Azure, Lambda, Nebius or Datadog account; the MCP registry schema
compatibility; the providers listed under "Not read"; paging of Cloud Asset Inventory and Resource Graph at
large sizes; whether Google's managed MCP servers are generally available.

## Not in this note

The per-tool policy details (its own note), the provider interface's Go types (S4), GPU launch economics
and cost caps (S10), writes to GCP and Azure, identity federation (Forgeline issuing tokens), and the
community catalog's review process.
