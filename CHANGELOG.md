# Changelog

All notable changes to Forgeline. The format follows [Keep a Changelog](https://keepachangelog.com/),
and versions follow [Semantic Versioning](docs/guides/versioning.md). Change the `Unreleased`
section in the pull request that makes the change; a release moves it under its version and date.

## [Unreleased]

### Added
- **Guardrails on every preview** (design note 040, step 1). Each blueprint preview is checked before it can be sent
  for approval. It is denied for: anything but web ports open to the internet; public or unencrypted databases; caches
  without encryption; unencrypted disks; buckets that could be made public; IAM users or access keys;
  AdministratorAccess-style policies; IAM policies allowing every action or every resource. It warns for: web ports on
  the internet, deleting what holds data, `service:*` actions, and policies only known at apply. A deny switches off
  Send for approval and is audited; the approver sees the warnings and which rule set passed.
- **More blueprint blocks** (design note 039). Databases (RDS MySQL or Postgres: private, encrypted, protected from
  deletion, with backups; AWS creates the password and keeps it in Secrets Manager, and apps get it at start without
  Forgeline ever seeing it), Redis caches (ElastiCache, encrypted), and secrets created empty for a person to fill in AWS.
  An app can call another app privately by name, scale between a fewest and a most number of tasks on CPU, and a front
  door can send paths such as `/api/*` to other apps. Existing databases, caches and secrets can be used as they are.
  The canvas assistant knows all of them.
- **Set up Forgeline** (design note 038, step 1). A new server's Home lists the first-run steps (your team, GitHub, a cloud
  account, the assistant, what the AI may touch, a first pipeline or blueprint), each linking to the page that does it and
  ticking itself when done; administrators can skip one. Settings › Setup shows them any time. `forgeline onboard` shows
  the same list in a terminal and asks what a terminal can answer (the assistant, an AWS profile and region), never a key.
- **Built by Forgeline** (Build › Built): everything Forgeline created and keeps in your cloud accounts in one place. Each
  built blueprint is an interactive diagram whose blocks list the AWS resources they made (name, id, ARN only; the state
  stays sealed), plus stacks from approved changes and environments that are up. **Check against AWS** marks each resource
  as still there or missing.
- **Moving diagrams.** Dots move along lines to show which way things flow: into a pipeline step while it runs, along a
  blueprint's links on the canvas and on the Built page (the internet into a front door, data to and from an app, logs
  out), and along enabled Lambda event sources on the Watch map. The stage a mission or workflow is working on shows a
  moving bar, and one waiting for you a pulsing dot. They show configuration and progress, not measured traffic, and stop
  for people who ask their system for less motion.
- **Docs pages like docs.openclaw.ai's:** breadcrumbs and the section above each title, and a **Copy page** menu (copy the
  page as Markdown, view it as Markdown, or open it in ChatGPT or Claude to ask about it). Every page has a plain Markdown
  copy next to it, and `llms.txt` lists them for AI tools.
- **A new documentation site** in the style of docs.openclaw.ai: Starlight (Astro, MIT) on GitHub Pages, with a home page
  of cards, a grouped sidebar (Start here, Guides, Reference, Project, Design notes), search on every page, dark and light
  themes in the console's colours and fonts, and an edit link on each page. Pages are still written in `docs/`; the design
  notes, the roadmap and the architecture are now real pages instead of raw files. The Pages workflow pins its actions by
  commit SHA.
- **Missions board and live view** (design note 037, part 2). Team › Missions: start a mission, see missions by where
  they stand, and follow one live, stage by stage, with its findings, proposals, evidence and report.
- **Missions, part 1** (design note 037). `POST /v1/missions` gives one agent a goal and one AWS account. It discovers (read
  only), proposes fixes (change proposals a different administrator approves; an advisory agent only recommends), waits for
  the decisions, verifies after anything was applied, and Forgeline writes the report from the record. Findings and
  evidence must name a lookup the agent really ran, or they are dropped and counted. Lookups outside the mission's account
  are refused on the server. Each mission has a token budget. Approvers see which mission filed a proposal.
- **AI use and budgets** (design note 036). Every model call is counted against the agent that made it (or people): tokens
  as the backend reports them, model time, and cost where the claude program reports it (it is now asked for JSON output).
  Administrators set monthly budgets in tokens and minutes per agent; at 80% the console warns, and once used up the
  agent stops before its next model call. Settings › AI use and budgets lists it all; Team cards show the bar.
- **The assistant beside the design canvas** (design note 035, slice 4). Describe a change in your own words; the assistant
  suggests an edited draft, checked by the same rules as a save (with one retry), kept on the blueprint's account and region
  and within the AI service switches, with its changes listed as sentences. Use this puts it on the canvas unsaved; the
  assistant never saves, previews or proposes. It gets no lookups or tools.
- **Preview and build blueprints** (design note 035, slice 3). Preview changes runs OpenTofu 1.13.9 (downloaded once and
  checked against its published checksum) to plan the saved blueprint, reading only; each block on the canvas is marked add,
  change, replace or remove. Send for approval files a cloud change; a different administrator approves under the approval
  rules (a removal is high risk), and exactly the previewed plan is applied with the providers it was planned with. Stale
  previews are refused. The plan and the state are sealed by the vault, the state is kept after failed applies too, and
  OpenTofu never sees the server's own environment. `scripts/dev/fake-tofu` lets you try it without a cloud.
- **The design canvas** (design note 035, slice 2). Build › Design opens a blueprint as blocks you drag into place, add from
  a toolbar and change in a panel beside them, including the links between them; only links the server would accept are
  offered. Each change is checked as you type, with problems named by block, and Save keeps it unless someone saved first.
- **Blueprints** (design note 035). Build › Blueprints keeps designs of what should exist in an AWS account: a front door,
  apps on ECS Fargate, DynamoDB tables, S3 buckets, SQS queues, log groups, and existing resources that are used but never
  changed, joined by links that open exactly the port and grant exactly the access they say. Each blueprint is checked in
  plain words and generates OpenTofu code (`main.tf.json`, AWS provider `~> 6.72`) with safe settings that are not options:
  private encrypted buckets, tables and buckets protected from deletion, only the front door reachable from the internet.
  Typed text is escaped so OpenTofu never evaluates it. Drafts only: nothing is built yet. New `/v1/blueprints` API.
- **Watch › Map** (design note 034, frame 6). The AWS map gets its own page, first in Watch, with a switch between accounts.
  Resources a CloudFormation stack made sit inside that stack's dashed box (from `list-stack-resources`, matched by exact
  type and id; parts the stack kept after a rollback are marked as no longer managed). A line joins a Lambda function to the
  queue or table stream it reads (from `list-event-source-mappings`); nothing is linked by name. Picking a resource shows
  which stack made it, what it uses and what uses it; a stack's state is explained in plain words, and a flagged resource
  offers **Ask the assistant to fix it**. New read-only `GET /v1/infra/aws/links`.
- **Morning check, and the assistant can read your repositories** (design note 028). A new `morning_check` lookup gives one
  look at what needs a person: the Docker host, failed runs in the last 24 hours, change proposals waiting and the worst open
  Guard findings. New `github_repos` and `github_files` lookups let the assistant see which repositories an administrator
  allowed and read a directory or a file in them (default branch, 12 KB at most, secret-looking files such as `.env`, keys
  and `.pem` never read, known secret shapes hidden), so it can write a pipeline that fits the project and offer it as a card.
- **The assistant can act inside Forgeline, with your confirmation** (design note 028). A new `prepare_action` tool saves a
  pipeline or starts a run, but only by filing a card: it lists the exact effects (written by the server from the file, so
  the model cannot soften them: images, network, secrets), shows the file, and does nothing until you press Confirm. Confirm
  runs it as you, through the console's own checks, once, within 30 minutes, and audits it; only the person it was prepared for
  can confirm (an administrator gets 404). Cloud changes are not in it and stay proposals approved by a different administrator.
  The assistant is also told about the setup it runs in: what is connected, recent runs, waiting proposals and the Docker host.
- **Assistant memory and conversation history** (design note 027). The assistant keeps short notes about you and about the
  system, and uses the ones that match your question: it ranks by words in common, then by how recently and how often a
  note helped, puts at most about 1.5 KB in the prompt as background (never as instructions), and drops notes nobody used for
  180 days. The assistant can save a note with a new `remember` tool, but only for something you said yourself in a turn
  where it has read nothing you did not type; after a lookup or with a run, pipeline or infrastructure page attached, the note
  waits for you to keep it, and a note about the system always waits for an administrator. Secrets, markup and anything over
  300 characters are refused. Every conversation is now saved for the person who had it (questions, answers, which lookups
  ran and what was filed, never lookup results, secrets hidden), private even from administrators, up to 100 conversations
  and 90 days. A new Memory page under Settings lists, edits and deletes notes and conversations, and an administrator
  confirms system notes and can turn memory off; the assistant panel has a History button to reopen a conversation. Agents,
  MCP clients and plain chat requests neither read nor write memory. API: `/v1/assistant/conversations` and
  `/v1/assistant/memory`.
- **Needs approval for read tools, and Google Cloud and Azure lookups for the assistant** (design note 026, slice 2). The
  access policy has a third level for reads, Needs approval: in the assistant panel a card shows the call with Approve
  and Deny, and nothing runs until the person who asked answers (once, from their own session; no answer in two minutes is a
  denial; each outcome is audited). Where nobody is present (agent runs, MCP clients, plain chat requests) such a tool is
  refused and not offered, and the effective-access view says "needs a person". A change tool still cannot be set to
  Allow. The assistant gets `gcp_resources` and `azure_resources` (read-only, from the same cache as the explorers); both
  join the access policy and the MCP read list. API: `POST /v1/assistant/approvals/{id}`; `/policy` accepts `ask`.
- **Access policy for integration tools** (design note 026, slice 1). The popup of AWS, the AWS MCP Server and Kubernetes
  has an Access tab where an administrator sets each tool to Allow (the default) or Blocked. A blocked tool is refused for
  everyone, whatever an agent's own tool list says, and never reaches a command line or proxy; it is hidden from the
  model and from MCP clients; each refusal is audited and shown in the integration's Activity. A tool that asks for a
  change (`propose_aws_change`, `propose_cloud_change`) can never be set to Allow, because a change is always a proposal
  that a different administrator approves; it can be blocked. A table shows what each agent can do: allowed, blocked by the
  connection, or not in its tool list. An unreadable stored policy fails closed. API: `GET` and `PUT
  /v1/integrations/{id}/policy`. Not built: Needs approval for reads (it needs an approval card in the assistant), and
  Google Cloud and Azure tools.
- **Connect wizard and Activity tab in the integrations popup** (design note 025, slice S1). Google Cloud and Azure
  connect inside the popup in three steps: choose what to read (a scope is checked in the browser the way the server
  checks it), connect (the server proves the login can read it first and, if not, says which role or command is
  needed and saves nothing), and test. Administrators also get an **Activity** tab for each integration: the newest
  30 audit entries that belong to it, with refusals marked, from the new `GET /v1/integrations/{id}/activity`. The
  match is exact, so AWS and the AWS MCP Server never show each other's entries, and no address is included.
- **Azure, read-only** (design note 025, slice S6). Infrastructure has an Azure tab. Forgeline uses the server user's
  `az` login and one Azure Resource Graph query (a fixed one: nothing a person types is in it) over the subscriptions the
  login can read, or the ones an administrator ticks, read in pages of 1000 up to 3000 resources. Virtual machines, App
  Service, AKS, storage, SQL, networks, Key Vaults (names only) and **Azure OpenAI, AI services, AI Search and AI Foundry
  workspaces** under the AI group are listed; an unknown type goes under Other resources. Connecting first proves the
  login can query and says what to fix (no login, no permission, the Resource Graph extension missing, unknown
  subscription). It is a card on the Integrations hub with a read-only test. The explorer now says when a read reached
  its limit (for Google Cloud too). Tried only against a stand-in `az` (`scripts/dev/fake-az`). Not built: the map, Guard
  rules, assistant lookups, spend, changes.
- **Google Cloud, read-only** (design note 025, slice S5). Infrastructure has a Google Cloud tab. Forgeline uses the
  server user's `gcloud` login and one Cloud Asset Inventory call to list the resources in a project, folder or
  organization: VMs, Cloud Run, GKE, Cloud Storage, disks, Cloud SQL, BigQuery, networks, firewall rules, Pub/Sub, Secret
  Manager (names only) and **Vertex AI endpoints and models** under a new AI group. A resource type it does not know is
  listed under Other resources. Connecting first proves the login can read the scope and says what to fix if not (no
  login, no permission, API not enabled, unknown scope); it is on the Integrations hub with a read-only test. It cannot
  change anything: the only commands are `asset search-all-resources`, `auth list` and `config get-value`. Tried only
  against a stand-in `gcloud` (`scripts/dev/fake-gcloud`). Not built: the map, Guard rules, assistant lookups, spend,
  changes, and other ways to sign in.
- **A Settings area behind a gear** (design note 025, slice S2). The sidebar keeps the daily work (Overview, Ship,
  Cloud, Agents and Marketplace, Approvals) and a **Settings** gear at the bottom opens one page that groups the
  setup pages: People and roles, Integrations, Secrets, Models, the Audit log and API tokens. Those pages show a
  Settings menu on the left. Planned entries (single sign-on, access policy, budgets, approval rules) are listed
  dimmed as coming soon, with no link. Proposals is now called Approvals in the sidebar and on its page. Every old
  address still works. Not built yet: the ⌘K search and the entries that are only listed.
- **Integrations hub API** (design note 025, slice S1). `GET /v1/integrations` lists every system Forgeline
  connects to with a status and a plain sentence: GitHub, AWS, the AWS MCP Server, the Docker host, Kubernetes and
  Claude; AWS profiles found on the server (names only, never a credential); and the planned systems in build order
  (Google Cloud, Azure, cloud models, a custom model endpoint, Lambda, Nebius, CoreWeave, RunPod, registries, Datadog
  and others), none of which has a Connect button. An AWS sign-in that ends within 30 minutes, or has ended, is
  flagged with what to do. Each system says who enforces read-only. `POST /v1/integrations/{id}/checks` runs a
  read-only test with a result, a detail and a fix per check, and never creates or changes anything. The hub page uses it
  (below).
- **The integrations hub page** (note 025). Integrations is now one page of cards grouped by category, with status
  filters, search and a strip for anything that needs attention. Click a card for its popup: the status and what to
  do, who enforces read-only, and (for administrators) a Test connection button that shows each check with its result,
  fix and time. Each popup has its own address (`#/integrations/aws`). Planned systems are dashed and have no Connect
  button. GitHub and Claude settings moved to `#/integrations/github/settings` and `#/integrations/claude/settings`
  (the GitHub App return address follows). Not built yet: the Settings area, per-tool access policy, a wizard in the
  popup, Google Cloud and Azure.
- **The assistant shows its work as it happens** (design note 024). New `POST /v1/assistant/runs` answers with
  server-sent events (AG-UI vocabulary) from the same lookup loop, limits and audit as `/v1/assistant/chat`. The
  panel shows each lookup as a card while it runs (what it asked, a cut of what came back, opened on demand), then
  the answer as cards, a proposal it filed as a card that follows the proposal's status, and a Stop button that
  cancels the model call. The proposal card has no Approve button: the asker is the proposer and cannot approve,
  so approval stays on the Proposals page. The answer still arrives in one piece, not word by word.
- **Assistant panel you can resize, dock or float, with answers shown as cards.** Answers used to be plain text, so
  tables showed as `|---|` rows and `**bold**` showed its stars. Markdown tables now render as tables, and an answer
  is split by its headings into cards (a summary, then one card per section), with code blocks that keep Copy and
  "Open as a new pipeline". What the assistant looked up is shown as chips above the answer, and a proposal it filed
  is a highlighted card. The panel docks at the right and the page makes room for it instead of being covered: drag
  its left edge or use the arrow keys, or the wide button (up to 70% of the window). Or float it as a window that
  you drag by its header and resize from its corner. The mode and size are remembered per browser. Tool calls
  streaming in as they happen, and the answer streaming word by word, are not built yet.
- **CloudWatch alarms on the AWS map** (design note 023, step 2). New `GET /v1/infra/aws/alarms` (viewer, one
  read-only `describe-alarms` call). An alarm in ALARM marks the resource it names as needing a look, and the
  resource's detail lists its alarms. An alarm belongs to a resource only when its namespace and its own
  dimensions name it (EC2, EBS, RDS, Lambda, DynamoDB, SQS, S3, load balancers, ECS clusters and services). A
  resource with no alarm says so; it is never shown as healthy. If the role may not read alarms, the console says
  which permission is missing (`cloudwatch:DescribeAlarms`) and the map still works. Logs and metrics are not built.
- **AWS map** (design note 023, step 1). The AWS explorer opens on a Map: resources sit inside the VPC their
  own VPC field names, the rest are grouped by category, and a strip lists what needs a look (a failing or
  changing state, a security group open to the internet, an unattached or unencrypted volume, a database that
  is publicly accessible or unencrypted). Selecting a resource shows its detail beside the map. The list view
  is one click away. No lines are drawn between resources, because the inventory does not say what talks to
  what. No new API.

### Changed
- The assistant knows where the console's pages are (Design and blueprints, the Watch map, AI use and budgets, approval
  rules), so it can answer "how do I…" about them instead of saying it does not know them.
- **Selected items unreadable in dark mode, and a narrow page on big screens.** The selected tile on the AWS map and
  other selected cards used a colour shade (`brand-950`) that was never defined, so in dark mode they stayed pale
  under light text and the text could not be read. The shade is defined, and a test now fails when the console uses a
  brand shade the theme lacks. The page was capped at 1280 px; it now grows to 1536 px from 1800 px wide screens and
  1920 px from 2300 px.
- **Console fixes.** The server version no longer shows a doubled `v` (`vv0.1.0-...`) when the build comes from git. The audit log table wraps its columns to fit instead of scrolling sideways at 1440px.
- **Docs site.** The console tour, the AWS guide, the assistant guide and the status page now describe the redesign, with new pictures. The console pictures used by the guides were retaken after the redesign. They say plainly that logs, alarms and the assistant event stream are designed but not built.
- **Console redesign, first steps** (design note 022). The sidebar groups pages into Ship, Cloud, AI and
  Govern, and the brand colour is now ember instead of indigo. Agent instructions and skill steps are
  edited as Markdown, with a toolbar, a live preview and advisory checks (for example a T0 agent told to
  delete things, a missing stop rule, or text that looks like a credential). The detail pages show them
  formatted, with a switch to the exact stored text. The stored text and the API are unchanged.

## [0.1.0] - 2026-11-01

First tagged release. Everything below was built and tested in development; see `docs/PLAN.md` and the design notes under `docs/design/` for what each part does and what is not yet verified against real infrastructure (Kubernetes against a real cluster, new environment templates on real AWS, plugin images from a real registry).

### Fixed
- An MCP server that died at start sometimes reported no reason, because its last words on stderr were
  read after the process had been seen to exit. This made a test in `internal/mcp` fail on a busy machine.
- A cloud change canceled while its preview was still being prepared came back as waiting for approval when the preview finished; the cancellation now stays.

### Added
- **Plugins can reach only some hosts.** A plugin may ask for named hosts instead of the whole network,
  and the administrator ticks the ones to allow. Each call runs on a private Docker network with no
  route out and no DNS; the only address it reaches is a per-call proxy that tunnels to a listed name
  and port, resolves the name itself and refuses private, loopback, link-local and other reserved
  addresses. The proxy answers only to a random per-call credential (so another container that can reach
  its address cannot use it), caps a call at 64 MiB shared across tunnels, closes every tunnel when the
  call ends, and tells upstream failures from refusals. What it allowed and refused is in the audit log.
  A changed host list needs a fresh grant.
  (note 021)
- **Environments from the assistant, and three more templates.** The assistant, review-tier agents
  and workflow steps can request a temporary environment in a person's name (`environment_templates`,
  `request_environment`). The request never starts on its own, even under the self-service policy; a
  different administrator approves it, and the audit log and the change's reason say the assistant asked.
  New templates: Lambda sandbox, event pipeline (SNS to SQS with a dead-letter queue) and workflow sandbox
  (Step Functions), each free while idle.
- **Changes in any AWS account.** An administrator can allow change proposals for an added account
  (off by default). Cloud changes and the assistant's AWS change proposals can then name the account;
  each is bound to the account, its AWS account number, profile and region, runs through that
  account's own signing proxy, and is canceled if the account is removed or changes are switched off.
  (note 021)
- **Forgeline as an MCP server.** Claude Code and other AI clients can connect to `/mcp` (or through
  `forgeline mcp` for stdio) with a personal API token made on the Account page. They get the
  assistant's read-only lookups (runs, why a run failed, pipelines, Guard findings, AWS and Kubernetes
  views, agents, the audit log for administrators) and two propose-only tools whose changes a
  different administrator must approve. A token acts as its person with at most the role you give it
  (a demoted or disabled person loses it at once), expires, is stored as a hash, and works nowhere
  else. Every call is audited, output is cut and credential formats are masked, and calls are
  rate-limited. Raw AWS access, agents and plugin tools are not exposed. (note 020)
- **Signed packages from git, workflow and environment-template packages.** The operator can point the
  marketplace at a git repository and the ed25519 keys they trust (`FORGELINE_MARKET_GIT`,
  `FORGELINE_MARKET_KEYS`). A package from git is listed as installable only if a trusted key signed its
  content hash; unsigned, wrongly signed, rolled-back or re-issued ones show as damaged with the reason.
  Manifests are read from git's object store without a checkout, so nothing from the repository is ever
  executed. An update must come from the same source and signer. `forgeline market keygen` and
  `forgeline market sign` are for publishers. Packages can now hold workflows (never scheduled, a
  viewer at tier T0) and environment templates (always administrator-approved, never self-service,
  no IAM or nested stacks). (note 019)
- **Pipeline guard.** Guard now reads saved pipelines and recent run logs, with no cloud account
  needed: unpinned images, secrets with open network, agent steps holding extra secrets, credentials
  or secret-looking values written into a pipeline file, `curl | sh`, scripts that print secrets, and
  credential formats in run logs. Findings never repeat the value.
- **Why a run failed.** A failed run shows a plain summary: the step that failed first, the likely
  causes, what did not run because of it, and the end of its output.
- **Plugins.** A marketplace package can carry MCP servers shipped as container images. They are off
  until an administrator turns them on (Docker must answer), and install grants nothing by default:
  network and each secret are separate choices. Each call runs in a fresh container that is
  read-only, non-root, has no extra privileges, no network unless granted and limits on memory, time
  and output. Their tools become read-only lookups an agent can be given; arguments are checked
  against a schema first and the output is treated as data. An update that asks for a different
  image, network or secrets needs a new grant. `FORGELINE_MARKET_DIR` adds a local directory of
  package manifests. (note 018)
- **Environments.** Temporary infrastructure and developer boxes from catalog templates (a developer
  box and a QA sandbox): each has an owner, a purpose, a time limit and a cost estimate, shown before
  you ask. Approval is the same four-eyes cloud change as ever, unless an administrator turns on a
  self-service policy for chosen templates within a time and cost limit; then those start on their own
  in the name of the policy and are audited. Forgeline deletes an environment when its time is up,
  cleans up a failed creation, and lets an owner extend (within a lifetime limit) or delete early. A
  person can have only so many at once. (note 017)
- **Kubernetes (read-only).** Register clusters (EKS through a connected AWS account, nothing stored;
  or any cluster with a service-account token kept in the vault), explore nodes, deployments,
  StatefulSets, DaemonSets, pods, services, namespaces and warning events, and let Guard check them:
  privileged containers, host networking, running as root, unpinned images, missing memory limits,
  workloads with no ready replicas, failing pods, nodes that are not ready, services exposed through a
  load balancer. The client can only send GET requests. The assistant and agents read clusters with
  `k8s_clusters` and `k8s_resources`. (note 017)
- **Guard.** Scheduled or on-demand read-only security and hygiene scans of every connected AWS
  account: open security groups, unencrypted volumes and databases, public databases, S3 public-access
  settings, the IAM credential report (root keys and MFA, users without MFA, old access keys),
  deprecated Lambda runtimes, failed stacks, stopped instances, logs that never expire, and more.
  Findings keep a stable id and history, resolve themselves when fixed, and are never resolved by a
  check that could not run. An administrator can accept a finding for up to 90 days with a reason; it
  reopens afterwards. The assistant and agents read them with `guard_findings`. (note 017)
- **Workflows.** Stages of agents run in order; steps in a stage run together; a stage can wait
  for an administrator; a workflow can run on an interval. Each step runs an agent at a version
  pinned when the workflow was saved, with each stage's output handed to the next as data. An
  unattended run is capped at developer (never an administrator) and anything it proposes still needs
  a person. Versioned like skills and agents, with run history, cancel, and recovery after a restart.
  (note 017)
- **Skills and agents with versions.** A skill is a procedure; an agent is instructions, pinned
  skill versions, a tool allowlist and a tier (T0 advises, T1 may file proposals). Every save is a
  new immutable version with a fingerprint, history, diff and rollback. The assistant can hand a
  task to an agent (`ask_agent`); an agent never calls another agent and never gets more than the
  person asking could do. (note 017)
- **Several AWS accounts.** Add read-only accounts by profile and region; switch between them in
  the console; the explorer, spend and the assistant read any of them. Changes stay on the primary
  account. (note 017)
- **AWS resource explorer** and the `aws_resources` lookup: every service in an account with state,
  search, detail, and what Forgeline changed there.
- **AWS MCP Server.** Read lookups, change proposals and four-eyes approval that applies exactly the
  approved script (note 016); the Proposals page.
- **Assistant lookups** (runs, logs, pipelines, AWS, audit) and a read-only `call_aws`
  (note 015).

### Changed
- Builds are stamped with the version from the nearest git tag.

## [0.0.0] - development

Console, pipelines, agent steps, assistant, AWS sign-in and GitHub App: see `docs/PLAN.md`.
