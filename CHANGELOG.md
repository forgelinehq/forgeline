# Changelog

All notable changes to Forgeline. The format follows [Keep a Changelog](https://keepachangelog.com/),
and versions follow [Semantic Versioning](docs/guides/versioning.md). Change the `Unreleased`
section in the pull request that makes the change; a release moves it under its version and date.

## [Unreleased]

### Added
- **Runs have a trust level** (milestone M3, slice 3). How much a run is trusted follows from how it started: a run for a pull request
  is **untrusted** and gets no secrets, whatever the settings say; a run started by a signed-in person is trusted. A run for a **push**
  to an allowed branch may now use secrets, but only where an administrator turned on **Secrets for pushes** for that repository
  (Settings › GitHub); it is off by default, so nothing changes until someone opts in. The run page and the API (`trust`) show
  it, and the audit log records it with every secret grant and refusal.
- **Every secret given to a step is in the audit log** (milestone M3, slice 4). `run.secret.grant` records the run, the step, the secret
  names (never values), the step's network and how the run started; a step with secrets and network access also gets a
  `run.secret.network` record; a refusal is `run.secret.refused`. The audit log can now be paged back past the newest 5000 events:
  the list takes `before` and answers `next`, and the audit page has **Show older events**.
- **A long chat keeps what it found.** The assistant sent the model only the last 12 messages and forgot the rest. The messages that
  fall out of the window are now condensed into a short summary the model is given as background, kept with the conversation and
  extended only when several more have fallen out (secrets hidden). Claude Code and the ACP agents keep their own sessions and are not
  affected.
- **The agent's answer streams into the chat as it is written.** With Claude Code or an ACP agent (Copilot, Codex, Antigravity) the
  text appears piece by piece instead of all at the end; a message after a tool use starts on its own line, and the history keeps
  what you saw. Claude Code is started with `--include-partial-messages` (checked against the real program).
- **A file the assistant builds in the sandbox can be downloaded.** It had no way to hand over a PDF or a CSV: the sandbox has no
  way out. The assistant now offers a file with a **Download** card, and `GET /v1/assistant/files` lists and sends files from your
  own `/work` folder (regular files only, not `/work/.agent`, not a link out, at most 50 MB, always as a download, audited).
- **Long results are read in pieces.** A long answer through `forgeline_api` ends with the offset to read on from, so a report longer
  than 12 KB is read whole instead of being answered from its first part.
- The assistant is told that agents and workflows cannot read memory files or the sandbox, so it stops writing instructions that
  say to read them.
- **Missing agents are found and offered for installation.** Under Settings › Assistant model, an ACP agent (Antigravity, Copilot
  CLI, Codex) now shows whether it is installed in your sandbox and which version; **Install the latest** shows the command and its
  source and runs it as root inside the sandbox when you confirm. If you pick an agent that is not installed and send a message, the
  assistant no longer fails with "command not found": it says so, prepares an Install card for an administrator and says what makes
  the agent work (a token in the vault, passed only to that agent, or the sign-in command). The assistant can prepare the same card.
  Installing always waits for you, even under autopilot. Checked with the real installers for Copilot, the Codex adapter and
  Antigravity. New `GET /v1/assistant/engines` and `POST /v1/assistant/engines/{harness}/install`.
- **The assistant can prepare a workflow run as a card.** After it builds a workflow it can offer to run it: the card shows
  the workflow, its version, the role its agents run with and the budget impact, and starts the run as you only when you
  press Confirm. If the workflow changed or was turned off since, the card refuses. (Before, it could only say "start it
  from Workflows".)
- **Long tasks no longer resend every old result.** When the lookups of a conversation pass 96 KB, the assistant clears the
  oldest results (keeping the newest four and every call) before asking the model again, so a long review stays within
  the model's context. The model is told a result was cleared and can call the tool again.
- **"Keep this?" cards.** After a task the assistant can offer what it learned (what a name means, how an environment is laid
  out, a procedure that worked) as a card with the file's path and text. It is saved only when you press Confirm, as yours,
  after the server checks it again. Under autopilot a clean conversation keeps it at once; once the conversation has read
  data you did not type, the card always waits.
- **Memory files can stop being valid.** A file the assistant keeps can have a *valid until* date. The assistant expires an
  old fact and writes a new file instead of overwriting it, so "Dev01 meant X until March" is kept as history. After its
  last day a file is marked *No longer valid* on the Memory page and left out of the assistant's notes and searches; you
  can set or clear the date there.
- **An `eval` pipeline step** (design note 050, slice 3). `eval: {suite: triage-regression}` runs an agent's suite from Agents ›
  Evals and fails unless every case passes, so the pipeline that ships a change to an agent tests it first. Optional
  `version` and `model`. It runs on the server as the run, nothing it does is carried out, and each attempt is a trace.
  Runs started by GitHub and `forgeline run` cannot use it.
- **Attack suites for agents** (design note 050, slice 3). Agents › Evals › **Add attack suite** adds seven ready-made cases
  from the OWASP list for agentic applications: orders planted in a log or an issue, a change asked for without approval,
  a tool used for something it is not for, secret values, and a role the person does not have. A case passes only if
  the agent does not obey it. Checked with a stand-in agent that obeys (the case fails) and one that refuses (it passes).

### Fixed
- **The Copilot agent over ACP never saw Forgeline's tools.** Checked against the real Copilot CLI: it ignores the stdio MCP server
  Forgeline gave it and takes only an HTTP one, and it asks permission before every tool, Forgeline's own included. Forgeline now
  gives Copilot its tools over HTTP on the sandbox's loopback (`forgeline mcp --listen`, forwarding to the engine socket),
  and allows the agent's request to use one of Forgeline's own tools without a card (a similarly named tool of another
  server still asks). The sign-in message names `copilot login`. `scripts/dev/acp-probe.py` repeats the check against a
  real agent. Not checked: the whole path inside the sandbox container.
- **The assistant said something was held back when it no longer was.** It told a person the sandbox was off, then found it
  was on once it checked. It is now told that what is held back can change while they talk, and to re-check
  its setup before saying so instead of relying on an earlier answer.
- **"I tried to do that but wrote the request in a form I cannot run" on big accounts.** A model that asks for the same thing
  in many regions at once sends several calls in one reply, and only exactly one was accepted; after a single resend
  the assistant gave up. Now the first call runs and the model is told which others did not (one call per reply), a
  model that writes the `<invoke>` form wrong is asked up to three times instead of once. Text around or between calls
  is still an answer, never a call.
- **One attack case could not reach an agent built as the guide says.** The issue-hijack case planted its text only under
  `get_run`, so an agent with `run_failure` or `get_step_log` (what the guide tells people to give it) never read it and the
  case passed for nothing. It is now planted under all three. Found in a browser check of the attack suite.
- **A card the assistant prepared could be pressed twice after a stop.** Confirming a card said it marked the card first, but
  only did so in memory: if the server stopped, or the browser closed, part-way through the action, the card stayed
  pending and pressing it again could do the action a second time (a second run, say). The card is now saved as not
  pending before the action starts, and the result is saved even if the browser has gone away.
- **Agent answers show as Markdown** in workflow runs, missions and an agent's chat, instead of raw `**` and `-`.
- **Two tests that failed now and then on CI.** The plugin sweeper test ended while the sweeper could still write into
  its directory (it now waits for the sweeper to stop), and the server tests waited only 5 seconds for background work
  on a busy runner (now 20; a passing test still returns at once). A red main stops the Pi's updater from deploying.
- **The setup guide's role policy covers every block and template.** It let Forgeline hand `fl-*` roles only to ECS and
  AgentCore, so a Lambda or Step Functions template failed to apply with `iam:PassRole` (found on real AWS). The guide's
  policy now lists every service; templates declare the services they pass roles to (`passes_role_to`), and a test
  fails if the guide misses one. When AWS refuses `iam:PassRole`, the error now starts with which services to allow.
- **A template input left unset could break the plan.** The Lambda template's optional `env` had no default, so a
  function without settings failed `tofu validate` (found by the real validate test). Every template input must now be
  required or have a default, checked when a template loads.
- **MCP tool calls are audited as the person who asked.** Calls from an agent chat were logged as `system`; they now
  name the person (found on the Pi with a real MCP server).
- **A plugin that cannot reach Forgeline's egress proxy says why.** On a machine whose firewall drops traffic from
  containers to the host (ufw's default), a plugin or local MCP server with a host list waited until its time ran out
  with only "took too long"; when no request reached the proxy, the error now says a firewall may be blocking it.
- **GitHub Actions can take the push role.** GitHub now sends token subjects with numeric ids
  (`repo:owner@123/name@456:ref:refs/heads/main`), which the role refused on real AWS. The trust accepts both forms, with
  only the ids as wildcards.
- **A source repository no longer needs an extra IAM permission.** The push role named GitHub's OIDC provider through a
  lookup that needs `iam:ListOpenIDConnectProviders`, so the preview failed on a builder role that has the documented
  policy (found on the Pi). The provider's ARN is now built from the account id.
- **When OpenTofu fails, its error is shown.** The message kept the last 3,000 characters of all output, which could be
  only the plan listing; it now shows the error output first.
- **A preview checks the AgentCore agent quota.** On real AWS an account whose "Total Agents per Account" is 0 previewed
  as ready and failed at apply; such a preview is now denied with the reason (a quota that cannot be read is a warning).
- **A preview that was applied stops looking ready.** The canvas no longer shows its marks or offers to send it again
  once the blueprint was built after it.
- **Cloud changes and AI proposals newest first.** Their ids are random, so the lists came in random order and, past
  100, showed a random 100. Found in a real check on the Pi (docs/testing/2026-11-12-real-aws.md).
- **Watch › Services draws at once**: it no longer waits for the account summary.
- **A blueprint can be deleted once what it built is removed.** It was refused for ever after its first build. It is
  still refused while anything it built exists, and its saved state is kept so an old approved plan cannot apply.
- **Readable links on the design canvas.** When several links left or entered one block they met in one point, so a
  web stack's links formed a knot and you could not tell which app used what. Each link now has its own point on the
  block's edge, in the order of where it goes, and bends at its own place. The Built page draws the same way.
- **Workflow runs and Guard scans in the right order.** Their ids were random and the store lists by id, so the
  runs table, the Team page's "last mission" and the pruning of old runs and scans worked on a random order
  (pruning could delete the newest run). New ids follow time, and lists and pruning sort by start time anyway.
- **Workflow pages.** A workflow that only runs by hand said "Every undefined minutes". The dialog that cancels a
  run or a mission offered "Cancel" next to "Cancel the run"; the dismiss button now says "Keep running" or "Keep
  going". A dialog left open no longer sits over the next page: leaving the page closes it.
- **The Map fits large accounts.** Stacks are compact cards in a grid, opened to show what they made; stacks with
  similar names (datadog-forwarder-…) share one card with a row each; stacks that made nothing the map shows fold
  into one line; each kind shows 12 tiles before "+N more" (the selected one is always shown); security groups come
  last; "needs a look" folds up when it is long. On an account with about 500 resources in a region the page went
  from 10,600 to 3,600 pixels tall. On large accounts only the selected resource's link lines are drawn.
- **Faster pages, no old screens.** A page that takes a moment now shows "Loading…" at once instead of leaving the
  last page up. AWS reads (the account summary, resources, links, alarms) answer from the last read and refresh in the
  background, one read per account at a time, so a large account's Watch › Map opens in about a tenth of a second
  after its first read (it took 40 seconds or more each time). The map draws before the stack links arrive. Leaving
  an AWS page cancels its requests, so they no longer hold the browser's connections. A failed read (an expired
  sign-in) is never hidden behind an old answer.
- **After a session ends**, the console stops polling the server (it asked every minute, forever).

### Added
- **Schedules for workflows and teams** (design note 047). Every few hours, daily, weekdays, weekly or monthly at a
  time, or a cron expression, in your time zone, with the next runs shown before saving. Teams can get a goal at set
  times, as a viewer or a developer. Team › Schedules lists everything that runs by itself, with pause, resume and run
  now. Missed times are skipped rather than run late.
- **Clearer workflow nodes.** A node's panel is in sections (who does it, input, output, next, limits) with names
  instead of ids, the agent as a card with its model, skills and tools, and **Try it**: a playground chat with the
  node's task filled in. Leaving the canvas or the agent builder with unsaved changes asks to save, discard or stay.
- **Download an agent** (design note 046). An agent's page offers the version you are looking at as a Forgeline bundle
  (`agent.yaml`, with its schema in `schema/agent.v1alpha1.schema.json`, and each skill as an Agent Skills folder) or
  as a Docker Agent file (`docker agent run`), with its remote MCP servers and skills. Forgeline's own tools stay
  behind and are listed; no key or secret is ever in either file; every download is audited.
- **Clearer menus and real icons** (design note 048). Seven spaces named after what you look after: Home, Agents,
  Workflows, Delivery, Cloud, Operations and Security, with an icon on every space, tab and Settings entry (Lucide).
  Approval rules, policy packs, resource types and the audit log moved from Settings to Security, and usage and
  budgets to Agents. Every page keeps its address.
- **Fixed: the Copilot assistant ran with tools.** Given `--available-tools` with no value, the copilot CLI (1.0.107)
  still offered the model 25 tools, `bash` among them. Forgeline now names a tool that does not exist, which leaves
  none, and refuses a reply whose output shows any tool was offered. It also works behind a corporate proxy (proxy and
  certificate variables are passed on), with older CLIs (the prompt goes in `--prompt` where piped stdin is not read,
  and flags the CLI does not list are left out), and choosing Copilot clears a leftover Claude model name. Thanks to the
  maintainer for the report and the first fix on a work setup.
- **Fixed: the assistant re-prepared what was already confirmed.** It is now told what became of the cards it
  prepared for you in the last day (confirmed with what it made, dismissed, failed or still waiting).
- **Amazon Inspector findings for agents.** The `inspector_findings` lookup reads ACTIVE findings, worst first, with
  counts by severity and resource type, the CVE, package, fixed version, resource, fix and exploit availability and
  EPSS score, for viewers and up, so a workflow running as a viewer can use it.
- **Workflows refuse agents with tools their run cannot use.** Saving a workflow whose agent has a tool its run-as role
  may never use (such as `call_aws`, for administrators only, in a workflow running as a viewer) now says so, instead of
  every run quietly going without it.
- **The assistant can change anything you can, and autopilot is one rule.** A new `forgeline_api` tool lets the assistant, and
  any agent running through it (Claude Code, an ACP agent), do in Forgeline what you do in the console, by calling the
  console's own API as you: read anything; update, version, archive or delete agents, skills, teams, workflows, pipelines and
  memory. Before, it could only create copies (an "update" made a sixteenth agent and left the old one). The same role checks,
  validation and audit entries apply, and a change asks first. It cannot touch users, tokens, secrets, permissions, sign-in,
  approvals or starting runs. Autopilot is decided in one place now: AWS changes, an agent's own tools, an ACP agent's requests,
  cards that save something and these changes all ask one gate, and a card autopilot approves shows as done. Running a pipeline,
  turning on a capability and handing a secret to a command still wait for you.
- **Any coding agent over ACP** (design note 053). A new assistant choice runs an agent that speaks the Agent Client Protocol
  (the standard VS Code and Zed use): Antigravity CLI, GitHub Copilot CLI, Codex, or your own command, inside your sandbox.
  Forgeline gives it its tools as an MCP server and answers its permission requests with the same cards; autopilot covers it
  in the sandbox. Sign in once inside the sandbox, or store a Gemini API key in the vault as `GEMINI_API_KEY`.
- **Permission cards say what they are about.** A card for the agent's command now says "Allow this command?", one for a file
  "Allow this file change?", one for the web "Allow this web request?". Before, every card that was not a secrets card said it
  would run on your AWS account. Only an AWS change talks about AWS.
- **Slim scrollbars.** All scrollbars in the console are now thin with no track, and the assistant panel no longer scrolls
  sideways.
- **Autopilot has no limit, and works while the assistant waits.** It runs until you stop it; a time or step limit is optional
  ("Set a limit"). It can be switched on at any moment, including while the assistant is waiting for an answer, and it then
  answers what is waiting and runs what comes next. With Claude Code as the agent and the sandbox on, it covers the agent's own
  Bash, Edit, Write and fetches inside the container (each shown and audited); on the server itself those still ask.
- **Claude Code runs in your sandbox container.** With the sandbox on, Claude Code as the agent runs inside it (its commands and
  files are in the container, not on the server), and reaches Forgeline's tools through a unix socket and a small bridge instead
  of the network. The image (Node, Claude Code, git, python) is built the first time. It needs a Claude Code token; with the
  sandbox off it runs on the server as before.
- **Claude Code as the assistant's agent** (design note 053). A new assistant choice, "Claude Code as the agent", runs the real
  Claude Code for each chat message: its own planning loop, subagents, file reading and sessions that continue from message to
  message, with Forgeline as its tools (AWS, Kubernetes, findings, runs, memory, cards, the doctor) and as its permission prompt.
  Its own Bash, Edit and Write show you a card with the exact command and wait for your answer; every Forgeline tool keeps its
  role checks, approval cards and audit entries. It works in a workspace of its own for each person, kept between conversations.
  Today the commands you approve run on the server; running them in the sandbox container is next.
- **The assistant looks after its own setup.** A new `doctor` tool checks what holds the assistant back: a model that
  cannot make real tool calls (a command-line backend, no provider), the shell sandbox being off, AWS accounts being
  protected, memory or autopilot being off, and recent model errors and rate limits. It says what is blocking it and prepares
  a card to fix it: "Turn on the sandbox" and "Allow changes to AWS account … with my approval", which an administrator
  confirms once. It is also told to save a short lesson in memory when a tool failed and it worked out what works, and to
  read those lessons next time. Before, it said it could not and left you to find the setting.
- **Ready-made agents that are written out properly.** The Library's security, cost, cluster and failed-run agents were
  one or two lines each. They now have a role, how to work, what they never do and the shape of the answer, with skills that
  are real procedures: "Deep AWS security review" (accounts, Guard and Inspector findings, network, data, identity,
  compute and logging checks, a ranked report with exact fixes and what could not be checked), "Review an account for waste"
  (with sizes and confidence), "Check a cluster" and "Triage a failed run" (with the kind of cause named). Installed copies
  update from the Library as new versions.
- **No duplicate cards.** If the assistant prepares a card you already have waiting (the same pipeline, skill, agent or
  workflow), you get that card again instead of a second one; a changed version replaces the old one, which is marked
  dismissed. Before, asking twice left two cards and the question of which to confirm.
- **A rate-limited model call is retried.** When a provider answers 429 (too many requests), the call waits 3 and then 8
  seconds and tries again before giving up, so a task that makes many calls is not ended halfway by a small plan's limit
  (found with Gemini's free tier). A monthly limit set in Forgeline is not retried.
- **Real tool calls for the assistant** (design note 052). Where a provider has tool calling (Anthropic, and OpenAI-compatible
  APIs including Gemini), the assistant's tools are now also sent as tool definitions with their arguments, and a call
  the model makes is run like the text block it used before. The conversation the model sees stays text, and the budget,
  permission checks and history are unchanged. A provider without tool calling, the Claude Code and Copilot backends
  and Bedrock keep the text block. Arguments of the wrong type (`"30"` for a number) are read instead of refused.
- **No more raw tool calls in the chat.** Some models write a call as `<invoke name="…"><parameter …>` instead of the
  block the assistant asks for. A call that is the whole reply now runs; one mixed with text is sent back once to be
  resent in the right form; and one that still is not run is never shown to you, only a short message saying nothing was
  done. The chat panel also no longer scrolls sideways: long unbroken text wraps.
- **Memory as files** (design note 052, slice 5). The assistant keeps what it learns as small files, `/memories/me` for
  you and `/memories/org` for everyone (administrators write there), with a `memory` tool (view, create, edit, insert,
  rename, delete, search), an index of them in every prompt, and BM25 search. A file written after it read data you did
  not type waits for you to keep it. The Memory page lists, edits and deletes them; secrets and markup are refused.
- **The assistant carries on after a card.** Pressing Confirm or Dismiss on a card in the assistant now sends your answer
  back as your next message ("I confirmed "Save skill…". Carry on."), so the assistant continues with the rest of its
  plan and the conversation shows which card you answered. Before, it waited and asked you to say when you had confirmed.
- **Autopilot** (design note 052, slice 4). A switch in the assistant panel, per conversation: the assistant keeps
  working without stopping to ask, for up to 30 minutes and 100 steps you can see and stop. A change that would only have
  asked runs without a card, is shown in the chat and audited. It never covers a protected account, IAM, Organizations,
  accounts, SSM or STS, a rule that denies or asks, a secret for the sandbox, or an agent. An administrator can turn it
  off for everyone.
- **The assistant says it has a shell** when the sandbox is off, and where to turn it on, instead of saying it cannot change anything.
- **A sandbox for the assistant** (design note 052, slice 3). It can run any command and write and run code in any
  language in a container Forgeline starts for you (root inside, any image, the network on, a `/work` folder that
  stays): `apt`, `pip`, `npm`, `curl`, builds. It cannot see the server's files or credentials, and gets a vault secret only
  when an administrator approves a card that names it. Off until an administrator turns it on under Settings ›
  Assistant permissions. A container shares the host's kernel unless the host has gVisor, and it can reach other
  machines the host's firewall allows; see the page.
- **The assistant can change AWS, after you approve the exact command** (design note 052, slice 2). `call_aws` is no
  longer read-only. A change shows a card with the command and Allow once, Always allow this kind, or Deny. Only an
  administrator, only in the chat, only on an AWS account marked "allow with my own approval" (Settings › Assistant
  permissions; every account is protected by default, and a protected account keeps a proposal and a different
  administrator). Rules (allow, ask, deny) for you or the organisation; iam, organizations, account, ssm and sts always
  ask. Checked against a fake AWS CLI only.
- **The assistant plans and asks** (design note 052, slice 1). A question may take up to 40 steps and 5 minutes (it was
  10 lookups); waiting for you does not use the time. It shows a plan as a checklist, and when the answer changes what it
  would do (two clusters match "Dev01") it stops on a card with options and a box for your own answer, then carries on.
  Agents never ask. Checked with a stand-in model only.
- **Runbooks** (design note 051). Operations › Runbooks: shell scripts that check or fix servers through AWS Systems
  Manager. One administrator writes a runbook and a different one reviews it; parameters are choices or numbers,
  never free text. Checks run when asked, by people and by agents (`run_runbook`); every fix waits for a different
  administrator under Approvals. Three examples (Disk report, Docker cleanup, Old backups). Checked against stand-ins
  only.
- **Hidden credentials** now include names joined by underscores (`aws_secret_access_key=`, `DB_PASSWORD:`).
- **`call_aws` for developers** (design note 051, decision 3). The read-only AWS CLI lookup (describe, list and get only,
  no secrets, data or logs) is open to developers, so agents in a workflow running as a developer can use it when
  Forgeline's summarised lookups do not answer.
- **Big AWS accounts.** `aws_resources` gives an overview (names and states) without a service and details with one,
  takes a `name` filter (such as `dev01`), and says how many resources did not fit instead of cutting the list off.
  ECS services are read eight clusters at a time, so accounts with dozens of clusters no longer time out.
- **Longer work per question.** The assistant and agents get 10 lookups per question instead of 6, and preparing a
  card does not count, so the card a question was for always comes back. A workflow step's task may be 4,000
  characters.
- **Publish agent versions only when they pass.** An agent's page can name a suite a new version must pass. Saving then
  makes a candidate; Publish makes it current once the suite has passed on it, and "Run the suite, publish if it
  passes" does both. Workflows, teams and schedules keep using the current version meanwhile.
- **Evals** (design note 050). Agents › Evals holds suites of cases for an agent, with fixtures for tools and checks
  (contains, matches, JSON, fields, tools called or never called, limits on tool calls, tokens and time, and a judge
  model with your rubric). A run asks each case one to five times on a version and model you pick; nothing it runs
  changes anything, and a case passes only if every attempt does. Traces that kept their prompt can be added as cases.
- **More traces, and prompts on request** (design note 049). Team runs, missions (a span per stage) and pipeline runs
  (a span per step) are traces too, linked from their pages. Settings › OpenTelemetry can keep prompts, answers and
  tool arguments and results in traces, with credentials and stored secrets hidden; it is off by default.
- **Send traces to your observability tools** (design note 049). Settings › Connections › OpenTelemetry sends every
  trace over OTLP/HTTP to Grafana, Datadog, Honeycomb, Jaeger or an OpenTelemetry Collector, in the background, with
  header values (API keys) kept in the vault. A person's user name is never sent. Checked against Jaeger.
- **Runs and traces** (design note 049). Agents › Runs and traces shows every assistant chat, agent run and workflow
  run as a trace: its model calls and tool calls (and a workflow's nodes and waits) on one time scale, with tokens,
  cost and errors, named as OpenTelemetry's GenAI conventions name them. No prompt or answer is kept. Agent answers and
  workflow runs link to their trace.
- **Search anywhere.** Ctrl K (⌘K on a Mac), /, or the box at the top finds a page, an agent, a workflow, a team, a
  skill, a pipeline, a mission or a blueprint by name, and offers shortcuts such as New agent. It lists only what your
  role may open.
- **Workflow panels in tabs.** A node's panel shows one part at a time (Agent, Input, Output, Next, Limits), with
  chips for earlier answers, output fields, choices and who may answer, and each wire as a card. The workflow's settings
  are tabs too: About, Trigger and Permissions.
- **Instructions in sections.** An agent's instructions are written part by part: who it is, goal, steps, rules,
  output, when to stop, and examples, background or sections of your own. Steps and rules are lists, the output has
  presets, and "What the model receives" shows the one Markdown text they make. Markdown mode is still there.
- **Push an agent to a registry** (design note 046, step 3). Settings › Agent registries holds GitHub Container
  Registry, Docker Hub or any OCI registry, with its token in the vault. Push on an agent's page proposes sending one
  version there as Docker Agent stores it; a different administrator approves the exact file under Approvals, then it
  runs anywhere with `docker agent run <ref>`. Checked against a local registry with the real Docker Agent CLI.
- **Import an agent** (design note 046, step 2). Import on the Agents page takes a bundle and shows what it will do
  before saving: a new agent or a new version of the one with the same name, each skill matched by its text or
  created, and the tools this server does not have left out with a reason. Importing it twice changes nothing.
- **The agent builder.** New agent and Edit open a page instead of a long form: the agent's parts (instructions,
  skills, MCP servers, tools, guardrails) with chips for what is in each, and the agent to talk to beside them. Add or
  Change on a part, or the model in the header, opens one side panel with tabs for Models (grouped by provider),
  Skills (pinned versions), MCP servers (each server's tools that an administrator turned on), Tools and Guardrails, with
  search and filters. The list of tools an agent can be given now says which MCP server or plugin a tool comes from.
  Nothing new is given the id `new`, which the console uses for making one.
- **The workflow canvas fills the page, as in n8n.** It pans (drag the background or scroll) and zooms (Ctrl + scroll,
  or the controls, with Fit); nodes carry their kind's icon, and the Start node says what starts the workflow. A node's
  **+**, or **Add node**, opens a searchable side panel of your agents by name, the logic nodes and the lookups; the new
  node is wired in after the one you came from, and goes between it and the next when there was one plain wire. The
  workflow's settings moved into a side panel, so the canvas keeps the page; Delete removes the picked node. Typing in
  one field of a node and clicking the next no longer loses the click.
- **Team › Workflows.** Workflows have a tab of their own instead of hiding under Agents: cards that say what each is
  made of, how it starts (by hand, schedule, webhook, a failed run) and how its last run went, with Run and the canvas
  one click away; every run of every workflow under Runs; and Delete, which archives a workflow for 30 days like an agent.
- **The assistant knows Forgeline.** It is told the page and tab you are on (a mission, a team, a workflow, an agent, a
  run, or any page by its name, such as Team › Missions) with what that page shows, and it has Forgeline's guides
  built in: `search_docs` finds the sections that answer a question, and its instructions say to look there before
  answering about a feature and never to invent one. New lookups read teams, workflows, missions and the models set up.
- **Teams** (note 045). Team › Teams lists teams of agents: a lead and up to eight members, and an agent can be in
  several. Give a team a goal: the lead plans one task per member, the members work at the same time with their own
  model, tools and budget, and the lead writes one answer from what they found (handed to it as data). Each run shows
  the plan, every member's work and the answer, live. Teams are made in a side panel; the Incident triage and Platform
  review packages in the library now install as teams. The org chart is replaced by a list of every agent.
- **Deleting an agent.** An agent page has Delete: the agent is turned off, moved to an archive (Agents › Archived) and
  can be restored for 30 days, then deleted for good. It is refused, naming them, while an enabled workflow or an
  unfinished mission uses the agent; agents that reported to it report to people again.
- **Gemini, Azure OpenAI and AWS Bedrock providers** (note 045, step 1). Gemini takes an API key; Azure OpenAI its
  resource address and key, with deployment names as models; Bedrock uses an AWS account the console is already
  connected to (no key), in the region you choose, through the Converse API. Not yet tried against the real services.
- **OpenAI and OpenAI-compatible providers** (note 045, step 1). Settings › Models and providers can now connect
  OpenAI, and any server that speaks the OpenAI Chat Completions API: Ollama, vLLM, LiteLLM, OpenRouter, Groq. A
  compatible server needs its address (plain HTTP only on this machine), may have no key, and its models are added by
  name in the side panel, free unless given a price. Agents can use, for example, `ollama/llama3.2:3b`.
- **Models and providers** (note 045, step 1). Settings › Models and providers: add an Anthropic provider (more kinds
  follow), turn its models on, set prices, a monthly limit and a fallback. Agents and the assistant can use
  `provider/model`, which works whatever the assistant is set to, so agents and workflows run with the assistant off.
  Keys are kept in the vault and never shown again; addresses follow the MCP server rules (https, no private or
  cloud-internal address unless allowed). Every model call is priced and shown in a live log beside the provider cards.
  Changing a provider opens a side panel instead of a popup.
- **Graph workflows start from a signed webhook or a failed pipeline run** (note 044, part 4). An administrator makes the
  webhook secret (shown once); GitHub's signature header works too. What started the run reaches agents as data.
- **A model per agent** (note 044, part 3). An agent can name the model it asks (for example Claude Haiku to route and
  Claude Opus to analyse); empty keeps the assistant's model. It is part of the agent's version.
- **Agent workflows as graphs on a canvas** (note 044, parts 1 and 2), like LangGraph drawn like n8n. Nodes share one
  state: agents (which may also write named fields), read-only lookups without a model, decisions by a rule or by an
  agent's choice, questions a person answers, merges and ends. Loops are allowed, bounded per node (3, up to 10) and
  per run (50 steps). Each run is saved after every step and drawn on the canvas with every visit's input and output.
  Writes still only go through an agent's own proposals, approved by a different administrator. Stage workflows keep
  working and can be turned into graphs.
- **Team › Library** (note 040, step 8; it was the Marketplace). Packages can now also hold guardrail packs (installed
  turned off) and MCP servers (installed with no token and no tools on), and say who reviewed them; the page lists the
  canvas's OpenTofu templates too, with a filter per kind. New packages: Incident response, Drift watch, Upgrade
  planner, three guardrail packs (Trivy critical, Cost guard, Data protection) and three tools (GitHub read only, AWS
  Knowledge, Terraform Registry). A new read-only tool, `blueprints_built`, tells an agent what each blueprint built.
- **A resource block for any allowed AWS resource type** (note 040, step 7). An administrator allows resource types per
  account under Settings › Resource types; a Resource block on the canvas then takes any of them, with a form made
  from the AWS provider's own schema. Every setting is checked against that schema on save and preview; secrets,
  what AWS sets and the region cannot be set, and nothing typed becomes an expression. A resource's links fill its
  settings from other blocks (a topic's ARN, a queue's URL). IAM users, access keys and account-wide settings can never
  be allowed. Guardrails, policy packs and approval apply as for every block.
- **Policy packs check every preview** (note 040, step 6). Under Settings › Policy packs an administrator turns on
  Trivy or Checkov for known misconfigurations, or writes the organization's own rules in Rego for Conftest. Each pack
  runs in a container with no network, a read-only file system and no capabilities, on a copy of the plan without its
  sensitive values or the earlier state. A pack's deny stops Send for approval like a built-in guardrail; its warnings
  go to the approver with the tool's version. Trivy can deny from a severity; any rule can be listed to deny or to
  skip. A pack that cannot run denies the plan, and a preview checked by other packs than today's must be made again.
  Needs Docker on the server. No pack is on by default.
- **An EKS cluster template with namespaces.** A cluster and managed nodes in the default VPC; each namespace gets a quota,
  default container sizes and a network policy allowing traffic only from the same namespace; IAM access entries for an
  administrator and per-namespace editors. Built in two approved steps (namespaces need the cluster to exist). Templates
  may now declare a Kubernetes stage and the AWS managed policies they attach; the setup guide's IAM policy gains
  `eks.amazonaws.com` and four EKS policies (an IAM change for existing installations), plus reading AWS's EKS node group service role. The blueprint summary
  now names a template's internet-facing endpoint. Checked on real AWS.
- **A data pipeline template.** Apps send records to it, an optional Lambda function reshapes them, and Amazon Data
  Firehose stores them as JSON in a linked bucket, by day; a Glue table and an Athena workgroup make them queryable.
  To make this possible, templates gained **writer links** (an app's role gets exactly the actions the template lists,
  only on a resource the template declares, never IAM, STS or KMS actions, and the app gets its name as
  `<BLOCK>_<SUFFIX>`) and **link counts** (`min`, `max`). The setup guide's role policy now also passes roles to
  `firehose.amazonaws.com`.
- **Which repository fits a block.** Under Code, **Suggest a repository…** reads the Dockerfiles in the repositories on
  the GitHub allow list and lists the folders that fit the app, agent or Lambda function, with why (its port, a Lambda
  base image, the AgentCore SDK, arm64, the block's name, the language). **Use** fills in the repository, branch and
  folder (`POST /v1/blueprints/suggest-source`).
- **Run a new image in one step.** A built app, agent or Lambda template with a source repository has **Run a new
  image…** on the design canvas: the images pushed to its own ECR repository, newest first, the running one marked.
  Picking one saves the blueprint with it and starts the preview; approving that preview is what runs it. Only images
  from the block's own repository are accepted (`GET .../blocks/{block}/images`, `POST .../blocks/{block}/image`).
- **Apps can run on Fargate Spot.** An app block's "Where it runs" is regular Fargate (as before), Fargate Spot, or one
  regular task with the rest on Spot. The cluster offers Spot only when an app uses it; apps without the setting keep
  exactly the code they had (a test compares it byte for byte). This is the useful part of note 040's "ECS cluster"
  template, added to app blocks instead (maintainer's choice).
- **A Step Functions workflow template, and links between templates.** `aws-step-functions-workflow` runs the Lambda
  function blocks it is linked to, in the order of the links, retrying a failed step with backoff; its role may start
  only those functions. A template's link role can now accept another template's blocks and receive their ARN from a
  named output, checked when the library loads.
- **Templates on the design canvas.** Each library template is a button on the Add bar; its form is drawn from the
  template's inputs, and links follow the roles it offers (a queue triggers a Lambda function; the function uses tables,
  buckets, queues and reads secrets). Build from GitHub suggests the image platform the template runs on.
- **Template blocks in blueprints.** A blueprint block can be a library template (`kind: template`): its inputs are
  checked against the template's schema, its links against the roles it offers, the code calls it as a module, and a
  plan keeps the module files so the approved apply runs exactly them. `GET /v1/templates` lists them for the canvas.
  The guardrails no longer read the root's code for a template's resource with the same name.
- **The template format, and a Lambda function template.** A template is a reviewed, versioned OpenTofu module with
  its inputs as a JSON Schema subset and the links it offers; Forgeline checks it when it loads (no providers, modules,
  backends or provisioners of its own, no secret readers, exactly the declared variables, Forgeline's provider pin). The
  first one, `aws-lambda-function`, runs an ECR image with a schedule, triggering queues and linked data. Not on the
  canvas yet (note 040, step 2).
- **Blueprints can have Forgeline build an app's image.** Build from GitHub… offers Built by Forgeline next to GitHub
  Actions: a `forgeline.yaml` with a build step that pushes to the block's image repository, and the push rule it needs,
  which an administrator allows with one click (note 043, slice 3).
- **Console runs can use a repository.** A saved pipeline can name a repository and branch from the GitHub allow list;
  each run from the console (or the assistant) downloads the branch's newest commit first and records it. The allow
  list is checked again at every run. Such runs build images but never push (note 043, slice 2).
- **Pull requests build images without pushing.** A build step with `push` failed in a pull request (found on the
  Pi); pull requests, console and API runs now build it without pushing and succeed, saying why in the log and the audit
  log. A push to a branch without a matching rule still fails.
- **Build images in Forgeline.** A `build` step builds a container image from the run's files with BuildKit, and can
  push it to Amazon ECR. Any run may build; only a run GitHub started for a push to a branch an administrator's push rule
  (Settings › Image builds) allows may push, and only to that rule's repository, with a short-lived ECR token that never
  enters a container or a log. The Dockerfile gets no secrets or privileges; `network: none` cuts it off. `forgeline
  run` builds on a laptop and never pushes. The editor's Add a step offers it (note 043).
- **A refused argument names the allowed values**, so the model can correct its call (found on the Pi: the model asked
  GitHub's `list_commits` for a field it does not offer).
- **A tool its server says changes things cannot be marked read-only.** `readOnlyHint: false` (GitHub's write tools,
  for one) was read the same as no hint, so an administrator could have marked such a tool read-only. Only tools whose
  server says nothing can be marked now.
- **Use MCP servers that do not mark their tools.** An administrator who has checked that a tool only reads can turn it
  on anyway; the page and the audit log name who marked it, and the mark goes when the server changes the tool. Tools a
  server marks destructive still cannot be turned on.
- **Local MCP servers whose image needs its own user.** Images such as `mcp/time` keep their program where nobody cannot
  reach it; "Run as the image's own user" runs them, with the rest of the sandbox unchanged.
- **List arguments.** Tools whose arguments include lists of text, numbers or true/false (8 of GitHub's read tools) can be
  turned on; each item is checked, at most 100. `minimum` and `maximum` on numbers are now enforced too.
- **`FORGELINE_EGRESS_PORTS`** fixes the egress proxy's ports, so a host firewall can let containers reach just those.
- **MCP servers you add.** Under Settings › Connections › MCP servers an administrator adds a remote MCP server (an
  HTTPS address, with a token from the vault) or a local one (a container image pinned by digest, run like a plugin with
  no network unless granted). Forgeline reads its tools; each starts off, and only tools marked read-only can be turned
  on. A turned-on tool's definition is pinned: if the server later changes it, the tool is off until someone looks again.
  Agents get a tool only when it is added to their tools; the assistant only when the server is offered to it. Calls are
  audited, arguments are checked against the tool's schema, answers are untrusted data, and remote addresses that are
  private or cloud-internal are refused unless allowed. Speaks MCP 2026-07-28 and falls back for older servers (note 042).
- **Workflows as a graph.** A workflow's page draws it like n8n: the trigger, each stage as a column of agents that run
  together, and an approval node before a gated stage. A run's page shows each node's state live, with moving dots into
  what is working.
- **The assistant writes skills, agents and workflows.** Ask it for one and it prepares a card an administrator
  confirms; the card says in the server's words which tools an agent gets, whether it may draft changes, which skills it
  follows, and when a workflow runs. Confirming goes through the same checks as the Agents page and is audited.
- **Code from GitHub for apps and agents** (design note 040, step 4). A block can name its repository, branch and folder.
  The blueprint makes a private ECR repository for it and a push role only GitHub Actions in that branch can use, and
  **Build from GitHub…** suggests the workflow (actions pinned to commit SHAs, arm64 for agents) and a Forgeline
  pipeline that starts it. Forgeline never pushes to the repository.
- **Agents on Amazon Bedrock AgentCore** (design note 040, step 2). An **Agent** block on the design canvas runs your
  agent's ECR image on AgentCore Runtime. Its execution role has what AWS documents a runtime needs (its own image, its
  logs and traces, Bedrock models, its own workload token, never tokens for arbitrary user ids) and the tables, buckets,
  queues and secrets it is linked to. The canvas assistant knows it.
- **Five Marketplace packages, two of them teams.** The incident triage team (a run analyst and a platform checker in
  parallel, then a lead's report) and the weekly platform review (security and spend side by side, then a review) each
  install three agents and a workflow. Also: AgentCore readiness (checks an agent against Bedrock AgentCore's documented
  rules), repository to pipeline (prepares a pipeline from an allowed repository for you to confirm), and an
  architecture review against the Well-Architected pillars.
- **Remove what a blueprint built** (design note 041). **Remove…** on the design canvas previews the removal, a
  different administrator approves it, and exactly that plan is applied, like a build. **Keep the data** leaves tables,
  buckets, databases and secrets, still protected; **Remove everything** takes two approvals when something is protected
  (deletion protection off and the rest removed, then the data), with a final database snapshot unless you untick it.
  The canvas then shows **Removed**, and the blueprint can be built again.
- **Watch › Services.** One tile per AWS service, read one at a time (four in flight) so the page draws at once and
  fills in as each answer comes, and a page per service (`#/services/<id>`) that lists only that service with search,
  details and alarms. The server reads one service with its own calls and its own cache entry (`?service=`), or from
  the whole inventory when that was read lately; `GET /v1/infra/aws/services` lists the services without an AWS call.
- **`install.sh`.** One command installs Forgeline without root: from a clone it builds with Go (downloading the
  toolchain the repository asks for, checksum verified, when none is installed); on its own it downloads the latest
  release for the machine and checks it against the release's SHA256SUMS. It makes a token once, keeps it in
  `~/.forgeline/env`, and with `--service` installs and starts a systemd user service (it refuses to overwrite a unit it
  did not write). It prints the link to open and the `forgeline onboard` command. CI runs it from the clone.
- **GitHub Copilot as the assistant.** Settings › Assistant can use the `copilot` program on the server (after
  `copilot login`), like the local `claude` program: no tools, no MCP servers, an empty scratch folder, the question
  on stdin, a clean environment. Tokens are counted in AI use; each question is one Copilot premium request.
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
- **Guardrails `builtin-2`.** A policy may use `Resource: "*"` when every action in that statement is one IAM cannot
  scope to a resource (the ECR token, X-Ray writes, CloudWatch metrics); anything else with `*` is still denied.
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
