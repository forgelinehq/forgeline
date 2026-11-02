# Changelog

All notable changes to Forgeline. The format follows [Keep a Changelog](https://keepachangelog.com/),
and versions follow [Semantic Versioning](docs/guides/versioning.md). Change the `Unreleased`
section in the pull request that makes the change; a release moves it under its version and date.

## [Unreleased]

### Added
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
