# 052: An assistant that works like a colleague, not a script

Status: **accepted 2026-11-21: both decisions answered yes (end of this note); slices 1, 2 and 3 built 2026-11-21** (the budgeted loop, a visible
plan and `ask_user`; the permission check for AWS; the sandbox; see "Built" at the end). Next: autopilot, then memory as
files. The `az`, `gcloud` and `kubectl` parsers are no longer needed: the sandbox runs any CLI.
Phase 4 of the product direction (AI SRE) needs it: runbooks (note 051) are the safe hands, this is the head.

## What the maintainer asked for

After the runbooks work, the maintainer compared the assistant with Claude Code and Copilot: they can add a skill, a
plugin or a CLI and get the job done; the assistant is dull by comparison. It should think and act like them. When it
needs something it should ask, like Claude does. When something is missing it should build it for itself and learn. It
should remember across sessions. For anything about the infrastructure it should work like a professional, and for
research or security findings, when no tool exists, it should do the job with a coding tool, a CLI or an SDK instead
of depending on a tool, including for Azure or Google Cloud when an MCP server is missing. Keys and secrets go in the
vault. In the follow-up the maintainer was explicit: **do not limit it to reading.** It should ask the person before
a tool, an action, an API call or a CLI call where that is needed, and it should be able to improve itself. Memory
must be strong. Later there will be personal and organisation levels, where people connect their own Jira, email and
other systems through MCP or plugins.

## Why it feels dull (what the code does today)

1. **A fixed menu.** About 40 named lookups. When none fits it says it cannot, or guesses. `call_aws` and `aws_script`
   are the only escape hatches, for AWS only, and limited to reading.
2. **A count, not a budget.** Ten lookups a question, then "answer now". Real work needs more steps.
3. **It cannot ask.** Missing information can only be requested in prose, which ends the turn and loses the plan.
4. **Memory is a sticky note.** 300 characters, 8 notes in a prompt, written only when told "remember". Nothing is
   learned from finished work.
5. **It does not make its own tools.** Everything it can do was written beforehand.

## What the field does (November 2026)

- The agent loop is the product: plan, act, observe, repeat, with a budget of time and money, not a count of calls.
- **Permission prompts, not a fixed menu.** Claude Code and Copilot can run any command or call, and ask the person
  first for anything not already allowed. The person can allow once, allow this kind of call from now on, or refuse.
  Rules (allow, ask, deny by pattern) belong to the person and to the organisation.
- **A shell and a code interpreter are the universal tool.** MCP servers, CLIs and SDKs are conveniences on top.
- **Memory:** a short index that is always in context, with entries fetched on demand (Claude Code's memory files and
  the memory tool work this way), plus search for large stores; embeddings are an optional layer.
- **Safety is enforced outside the model:** a sandbox, credentials the model never sees, an audit trail, and limits an
  administrator sets.

## Decisions (proposed)

### 1. A real loop with a budget, a plan and questions

Steps (default 40), wall time (default 5 minutes) and cost (the person's AI budget, note 036) replace the count of
ten. A visible to-do list updates as it works. Independent lookups run in parallel. A budget stop gives a summary, and
"continue" carries on from the same plan.

`ask_user`: a question with two to four options and free text. The run pauses on a card and continues with the answer.
It is used when the answer changes the work ("Dev01 matches two ECS clusters and no EKS cluster; which one?"), and for
approving an action (below).

### 2. Permissions that ask, instead of a ceiling that refuses

The assistant is not limited to reading. Every tool, CLI call, API call or script runs **as the person**, with their
role, and passes through one permission check with three answers:

- **Allow:** runs, audited and traced. Reads on the person's own scope start here (lookups, `describe`, `list`, `get`).
- **Ask:** the person sees the exact command, code, API call or change, and chooses **Allow once**, **Always allow this
  kind** (a pattern, shown and editable) or **Deny**. Anything that changes something starts here.
- **Deny:** an administrator's rule. It always wins.

The person's rules live under Assistant › Permissions (like Claude Code's settings): patterns such as
`aws s3 ls *`, `kubectl get *`, `az * list`, or "any call to Jira". An administrator sets organisation rules above
them: deny lists, and which accounts and environments the assistant may change at all. The assistant says why it asks.

**Where this meets the trust model (note 006).** Today a cloud change is a proposal that a **different administrator**
approves, and CLAUDE.md says no path may change the cloud directly. A permission prompt for the person's own action is
a weaker check. Proposed: the prompt covers everything except changes to a **cloud account the organisation marked as
protected**; those keep the proposal and a different administrator, as now. An account is protected by default; an
administrator may mark an account (a sandbox, a personal account) "allow with my own approval". A solo maintainer then
works without a second person on their own accounts, and a company's production stays under two people. Decision 1.

### 3. When there is no tool

The assistant works down a ladder and says which rung it is on:

1. A summarised lookup we wrote (cheap, fits one answer).
2. A **CLI** (`aws`, `az`, `gcloud`, `kubectl`, `gh`, `docker`, `terraform`, any installed tool) through the permission
   check, in a container that has only the tools and credentials that were allowed.
3. **Code in a sandbox:** a Python or Go program using an SDK or HTTP, in a container with the allowed network hosts,
   a time and memory limit, and credentials passed as environment variables it cannot print.
4. **A new capability for later:** when something will be needed again it proposes an MCP server (from the marketplace,
   note 042) or writes a **skill** (note 045) with its source and a test. Its own skills and tools are reviewed by a
   person before they are kept, like runbooks. It says what was missing and what it did meanwhile.

### 4. Keys and secrets

A task that needs a key opens a card; the value goes to the vault and the model gets only its name. A sandbox or CLI
call receives it as an environment variable. Output is scanned for the value and for secret shapes before the model
or the person sees it, and the scan applies to what the sandbox writes to files too.

### 5. Memory that is strong

What to use, and why it is not a vector database first:

- **Entries, not notes:** each is a short document (a name, a one-line description, a type, a body of up to a few
  thousand characters), stored in the existing document store, so it works on a laptop and with Postgres.
- **An index in every prompt** (name and description only, a few hundred tokens), and a `recall` tool that fetches the
  entries the model chooses. This is the approach Claude Code uses, and it needs no embedding model.
- **Search for large stores:** full-text search in Postgres, and an in-process BM25 ranking without it. Embeddings are an
  optional layer later (pgvector or a local model) for people with thousands of entries; they add a dependency and a
  model to keep running, so they wait until the simple layers are not enough.
- **Learning after work:** when a task ends the assistant proposes entries, with where each came from ("Dev01 means the
  ECS clusters Cluster-bpdev01 and Snap-App-dev01"), as cards to keep, edit or drop. Procedures it worked out become
  skills. Notes about the system wait for an administrator, as now. A note is data, never an instruction, and cannot
  change a role, a policy or a permission.
- **Scopes:** personal (only the person), organisation (shared, administrator-curated), and later per project and per
  environment. A page, Assistant › What it knows, lists every entry with its source; any can be edited or removed.

### 6. Connectors, later: personal and organisation

People will connect their own Jira, email, calendar and chat through MCP servers or plugins. The permission check is
the same for them: a connector's tools are named in rules, personal credentials stay in the person's vault space, and
organisation connectors are set up by an administrator with the permissions they allow. Nothing in this note blocks
that; the permission model above is built so it can be added without changing it.

### 7. A model that can do it

The loop and the checks are ours; the quality of the plan is the model's. It works best with a strong tool-using
model (the Claude models, or Copilot CLI as a backend with its own tools turned off, note 047). A weaker model gets a
shorter ladder (no sandbox) and says so.

## Order of work

1. The budgeted loop, the visible plan and `ask_user` (decision 1). The smallest change with the largest effect.
2. The permission check and the person's rules, with "allow once / always allow this kind" cards (decision 2), and CLI
   calls for the clouds through it (rung 2), replacing the read-only parsers as the only way in.
3. Memory entries, the index, `recall`, learning cards and the page (decision 5).
4. The sandbox and the secret-request card (decisions 3 rung 3, and 4).
5. Skills and MCP servers it proposes for itself (rung 4); then connectors and organisation scope (decision 6).

## Decisions for the maintainer

Both answered **yes** on 2026-11-21.

1. **Protected accounts.** Is the proposed split right: a person's own approval is enough everywhere except accounts the
   organisation marks protected, which keep a different administrator? This changes a rule in CLAUDE.md and note 006.
2. **Memory.** Start with the index plus `recall` plus full-text search, and add embeddings only if that proves
   too weak? Proposed: yes.

## Models and local CLIs (later deep dive)

The maintainer added a Gemini key and asked whether the assistant can use other models and providers, and whether that
works alongside local Claude Code, Codex or Gemini CLI. Today: any provider under Models and providers (Anthropic,
OpenAI, Gemini, Azure OpenAI, Bedrock, OpenAI-compatible servers) can answer for the assistant; Claude Code (login or
token) and Copilot run as CLI backends with their own tools turned off. Codex and Gemini CLI are not backends yet.
The new loop must work across all of them: native tool calls where a provider supports them, the existing fenced
lookup blocks otherwise, and the same permission check either way. Planned for the deep dive: Codex and Gemini CLI as
backends, choosing a model per task (a cheap one to route and summarise, a strong one to plan), and a CLI backend that
may use its own tools only when its calls go through our permission check.

## Built (slice 1, 2026-11-21)

- Budget: 40 steps a question (it was 10) and 5 minutes of wall time; time spent waiting for the person does not count.
  A question to the person, a plan update and a prepared card are free, with a hard stop at twice the budget. At the
  budget the model is told to say what is done, what is left and what it would do next, so the person can say "continue".
- `update_plan`: the model keeps a checklist of up to 12 items (todo, doing, done) that the panel shows.
- `ask_user`: a question with up to four options; the run stops on a card and waits up to ten minutes; the answer comes
  back to the model as data, and the card keeps "question → answer" in the run. Answered only through
  `POST /v1/assistant/questions/{id}`, by the person asked; an approval id cannot be answered as a question or the other
  way round. Offered only where a person is watching (the chat stream); agents never get it. With nobody present the
  model is told to assume and carry on.
- The system prompt now says how to work: find out before asking, try another way before saying it cannot, plan, ask
  when the answer changes the work, never ask permission to read.
- Checked in a browser against a stand-in model that plans, asks which cluster, looks something up and finishes. Not yet
  checked with a real model: how well Claude, Gemini or the others choose to plan and ask is the next thing to look at.

## Built (slice 2, 2026-11-21): the permission check, for AWS

- `call_aws` is no longer read-only. A describe, list or get still runs at once. Anything else is a **change**, and goes
  through one check (`permitChange`): an agent never (it has nobody to ask); a person who is not an administrator never
  (propose it instead); an account that is not marked "allow with my own approval" never (it stays protected, and the
  answer names `propose_aws_change`, note 006); then the rules; then, unless a rule allows it, a card in the chat showing
  the exact command with **Allow once**, **Always allow `aws <service> <operation> *`** and **Deny**. No answer in five
  minutes is a no. The AWS service switches (off, look only) still apply to a change, and an eval records a change
  without running it. Every decision is audited (`assistant.cli.change`).
- Still refused outright, not asked about: the shape rules (no pipes, substitution, redirecting options, local files or
  addresses), the services that stay out of reach, operations that return a secret to the model (`create-access-key`,
  `create-login-profile`, and the existing list), and, for an allow rule, anything under `iam`, `organizations`,
  `account`, `ssm` and `sts`, which always ask, so no rule or "always" can skip the card. (`ssm send-command` runs
  commands on machines; a reviewed runbook, note 051, is the way to do that repeatedly.)
- Rules: allow, ask or deny, for one person or for the organisation. A deny from anyone wins; an organisation ask cannot
  be skipped by a person's allow; an allow must name its service and operation. Settings › Assistant permissions lists
  them, removes them, and lets an administrator mark accounts. "Always allow" in the chat makes the person's own rule.
- Checked in a browser against `scripts/dev/fake-aws`: refused while protected; after marking the account, the card;
  "Always allow" ran the change, made the rule, and the audit shows each step. Not checked against a real account.
- Not in this slice: `az`, `gcloud` and `kubectl` (they need their own credential handling and parsers), a
  general shell or code sandbox (slice 4: a shell on the server would see every credential on the machine), and editing a
  pattern before saying "always".

## Revision 2026-11-21 (evening): no limits by cloud, a real sandbox, autopilot, native memory

The maintainer's answer to slice 2: the assistant must not be limited to a list of clouds or CLIs. It should download and
install what it needs, write code in any language or SDK and run it, use Docker or micro-VMs, have shell access, have an
**autopilot** mode, and have memory that is native to the AI rather than something we invented. This section replaces the
narrower plan for slices 3 to 5 above; the permission check of slice 2 stays as the way anything that changes something
is approved.

### What the research says (November 2026)

- **Sandboxing.** Containers share the host's kernel, so for hostile code the field uses gVisor (a user-space kernel; fits
  container tooling) or microVMs (Firecracker, Kata, Docker Sandboxes: each session has its own kernel), with default-deny
  network egress and runtime monitoring. Neither isolates the network or enforces permissions by itself.
  Sources: [Northflank](https://northflank.com/blog/how-to-sandbox-ai-agents), [Vercel on gVisor vs microVMs](https://vercel.com/i/gvisor-vs-microvms),
  [DEV: Firecracker and gVisor runtimes](https://dev.to/aiagentengineering/how-to-sandbox-ai-agents-in-2026-firecracker-gvisor-runtimes-isolation-strategies-14pk).
- **Memory.** Anthropic's memory tool is *client-side*: the model asks to view, create, replace, insert, delete or rename
  files in a `/memories` directory and the application performs them against storage it controls; every path must be
  validated. Context editing and compaction clear old tool results and prompt the model to save what matters to memory
  first. Source: [Claude Platform docs: memory tool](https://platform.claude.com/docs/en/agents-and-tools/tool-use/memory-tool).
  Framework approaches differ: Mem0 extracts user facts (add, search, update, delete by user, agent and session); Zep and
  Graphiti keep a temporal graph so "what was true then" can be asked; LangMem adds hot-path tools and background
  consolidation inside LangGraph; Letta lets the agent manage its own memory blocks. Benchmarks conflict and are mostly
  from vendors. Sources: [Atlan](https://atlan.com/know/best-ai-agent-memory-frameworks-2026/),
  [Vectorize](https://vectorize.io/articles/best-ai-agent-memory-systems), [Graphlit](https://www.graphlit.com/blog/survey-of-ai-agent-memory-frameworks).

### Decisions (proposed, building from slice 3)

**A. A sandbox where it can do anything.** A tool `run_shell` runs commands, and any language or SDK, in a container that
Forgeline starts for the conversation: a persistent workspace, the network on (so it can install with apt, pip, npm or
curl, and call any API), CPU, memory, process and time limits, no host mounts, no Docker socket, dropped capabilities.
Egress is closed to the host, the local network and the cloud metadata address, so a prompt-injected command cannot
reach this machine's credentials or an instance role. Credentials are never in it unless the person names a vault secret
for the task; the model gets the name, the container gets an environment variable, and output is scanned for it.
The runtime is chosen from what the host has: **gVisor** (`runsc`) when installed, **a microVM** (Kata or Firecracker,
a later slice) where available, and a hardened plain container otherwise, with the card saying which. The assistant can
choose an image (Python, Node, Go, Ubuntu, or its own after a build), and can use Docker inside the sandbox where the
runtime allows it. This is not a limit on what it can do; it is where it does it.

**B. Asking, as before.** Running in the sandbox without credentials or secrets is allowed by default (nothing outside
it can change). Anything that gives the sandbox a credential, a secret or a connection to an account is a **change** and
passes the slice 2 check: administrator, chat, account marked for it, rules, a card. A command in the sandbox that then
calls a cloud with that credential is covered by that approval, so the card says which credential and which account.

**C. Autopilot.** A switch in the chat, on for one conversation with an envelope: a time (default 30 minutes), a number of
steps, and the accounts it covers. While it is on, a change that would have shown a card runs without one if a rule
or the envelope covers it; every such change is audited as autopilot and shown in the run. Autopilot **never** covers
what the slice 2 check refuses (protected accounts, non-administrators, agents), a deny rule, changes under `iam`,
`organizations`, `account`, `ssm` and `sts` (those always ask), or a host shell. Stop ends it at once, it ends itself
when the envelope runs out, and it never carries over to another conversation. Proposed default; an administrator can
turn autopilot off for everyone.

**D. A shell on the server itself** is a separate, off-by-default mode an administrator may enable. It always asks,
never runs in autopilot, and shows what it can reach. It exists for the cases that need the host (the Docker host, files
on the server); for everything else the sandbox is the answer.

**E. Memory, natively.** Memory becomes a small **file system per scope** (`/memories/me`, `/memories/org`, later per
project), stored in the document store:
- Claude models get Anthropic's **memory tool** as is (the model views, creates, edits and deletes files itself); every
  other provider gets the same operations through a tool of ours with the same shape, so memory is the same everywhere.
- An index file (`/memories/README.md`) is always in the prompt; the model opens files when it needs them.
- Long runs use context clearing and compaction, and the model is told to save what matters first.
- Safeguards: every path is validated (no `..`, one root, limits on files, size and depth); secrets are scanned out of
  anything written; what a lookup returned, not what the person said, is saved only as a suggestion the person confirms;
  memory is data in the prompt, never an instruction, and cannot change a permission or a rule; org memory is edited
  by administrators or needs one to confirm; everything is listed under Assistant › What it knows, editable and
  removable, with where it came from.
- Search over memory: full-text (BM25) first. Facts that change over time get a validity date (the idea from Zep),
  so "Dev01 meant X until March" is kept, not overwritten. Embeddings and a graph stay optional, for stores too big for
  the first two.
- Learning: after work, the assistant proposes what to save; routine facts about the person's own preferences are saved
  at once with an undo, facts about the system wait for confirmation.

### Order of work (replaces the earlier list)

3. The sandbox: `run_shell`, the workspace, the network rules, the secret-by-name card, the runtime choice. Audited, traced.
4. Autopilot (envelope, switch, stop, audit), over the slice 2 check and the sandbox.
5. Memory as files: the store, the memory tool for Claude and ours for the rest, the index, search, learning, the page.
6. The host shell mode (decision D), microVM runtimes, other CLIs' own login handling, then skills and MCP servers it
   proposes for itself.

### Decisions for the maintainer

1. **Sandbox first, the server's own shell later and always asking** (decision D)? Proposed: yes. It keeps your work
   credentials off any command the model writes, while the sandbox can install and run anything.
2. **Autopilot never covers** protected accounts, deny rules, `iam`, `organizations`, `account`, `ssm`, `sts` or the host
   shell (decision C)? Proposed: yes.

## Built (slice 3, 2026-11-21): the sandbox

- `run_shell` runs a command, with a timeout up to 15 minutes, in a container Forgeline starts for the person (one per
  image, a persistent `/work` volume, root inside, any image, the network on). The command is one argument to
  `bash -c` under `timeout` inside the container; no host shell sees it. Output keeps its start and end (12 KB), with
  credentials and any secret value hidden. The container is removed after 30 minutes unused; the workspace stays.
- What makes it a sandbox, checked by reading the exact `docker run` arguments: one volume (its own workspace), no host
  mounts, no Docker socket, not privileged, `--cap-drop ALL` plus six capabilities (`apt` and file work need them),
  `no-new-privileges`, 2 GB of memory, 2 CPUs, 512 processes. gVisor (`--runtime runsc`) is used when Docker has it;
  this machine has only runc, so the page says "hardened container (shares the host kernel)".
- Off until an administrator turns it on (Settings › Assistant permissions). Not offered to agents.
- **A secret reaches a command only on a card.** The model names vault secrets; an administrator, in the chat, sees the
  command and the names and answers once (never "always"). The value goes through the environment of the docker
  process, never its arguments, is hidden in the output, and is not in the audit log. Internal secrets (names with a
  dot) cannot be named.
- **What I could not do, and the design said I would.** The network is not closed to other machines. A container on
  Docker's bridge reaches whatever the host's firewall lets it. Measured here: a service that listens only on the loopback
  address (Forgeline on 8080) is not reachable from the container, and the metadata address did not answer; a service
  listening on all interfaces or on the LAN would be reachable. Closing that needs firewall rules on the host, which an
  unprivileged server cannot add. Instead: `FORGELINE_SANDBOX_NETWORK` names a Docker network an operator has fenced,
  the setting `none` turns the network off, and the page says so.
- Checked for real, with Docker 29.3 on this machine: `apt` installed Python 3.12 and git under the dropped
  capabilities, the network worked, `/work` persisted, the capability mask had the six, and there was no socket and no host
  folder. In the browser a stand-in model installed Python, wrote a script and ran it, and a reset removed the container
  and the volume. Not checked: gVisor or any microVM runtime, a second person's sandbox in parallel under load, and
  the secret card in a browser (covered by tests).
- Not in this slice: autopilot, memory, a general host shell (decision D), installing a runtime for the person, and
  running Docker inside the sandbox.

## Built (slice 4, 2026-11-21): autopilot

- A switch in the chat panel, **per conversation**. On, each message carries `autopilot` and the server holds an
  envelope in memory: 30 minutes and 100 steps by default (up to 240 and 300), shown as "Autopilot on, 12 of 100 steps,
  28 min left". It is not stored, so a restart ends it; a new or another conversation starts with it off; a message sent
  with the switch off ends it for good. **Stop** ends it at once (a run in flight stops at its next step).
- Inside the envelope the loop's budget is the person's, not 40 steps and 5 minutes, and the model is told to carry on
  without asking unless it is blocked on something only the person knows, to say what it chose, and to install or write
  a tool in the sandbox when none exists.
- It skips a card only where nothing but the default said "ask": the command is not matched by any rule (a person's or the
  organisation's "ask", or a deny, still hold), the service is not one that always asks (`iam`, `organizations`,
  `account`, `ssm`, `sts`, `sso-admin`, `identitystore`), the account is marked "allow with my own approval", the person
  is an administrator, and nobody is an agent. Every such change is shown in the chat ("Autopilot ran: ...") and audited
  as `autopilot, no card`, with `assistant.autopilot.start`, `.stop` and `.end` around it.
- A secret given to the sandbox still needs its card. Preparing a pipeline, an agent or a workflow still waits for Confirm.
- An administrator can turn autopilot off for everyone (Settings › Assistant permissions).
- Tested by breaking it: dropping the "no rule asked" check, the always-ask check, the "still running" check, the
  administrator switch, the end-on-switch-off and the step count each fails a test. Browser-checked against the fake
  AWS CLI and a scripted model only.

## Built (slice 5, 2026-11-21): memory as files

- One tool, `memory`, with the operations of Anthropic's memory tool (`view`, `create`, `str_replace`, `insert`,
  `delete`, `rename`) and a `search`, over a small file system: `/memories/me/...` for one person and `/memories/org/...`
  for everyone. Files live in the document store (so Postgres works), never on the server's disk. A path is checked
  before anything happens: it must start with `/memories/me/` or `/memories/org/`, names are letters, digits, space, dot,
  dash and underscore, at most four folders deep, and a file ends in `.md` or `.txt`. A file is at most 8000 characters;
  100 per person and 200 for the organisation. Content with markup or something shaped like a secret is refused.
- **Same for every model.** The assistant reaches its tools through a text block, not a provider's native tool call, so
  Claude does not get the native `memory_20250818` tool yet; ours has the same commands and arguments, so moving to the
  native one later is a change of transport, not of behaviour.
- **An index in every prompt:** each file's path and first line (2 KB), newest first, so the model knows what it has and
  opens what it needs. `search` is BM25 over the paths and contents the person may read.
- **Who writes where.** A person's files: the assistant, on their behalf. The organisation's: an administrator only, and
  not after the conversation has read data the person did not type. A file the assistant writes after such a lookup is
  **pending**: it stays out of the index until the person keeps it on the Memory page (the rule the notes already had).
- **Seen and editable:** the Memory page lists every file with its text, who wrote it and when; a person can edit,
  keep or delete their own, and "Forget everything about me" removes their files too. The chat shows "Saved to memory: path".
  Every write is audited (path, action, size, never the content). Agents and workflows do not get the tool.
- The notes of slice 1 (`remember`, 300 characters) still work beside it.
- **Not built yet:** validity dates for facts that change (the idea from Zep), a suggestion card after a task ("keep
  this?"), context clearing and compaction, moving a personal file to the organisation, embeddings. Verified with a
  scripted model only.

## Built (native tool calls, 2026-11-21)

- The tools were described in the prompt and the model asked for one by writing a block of text. Models get that wrong
  in ways that depend on the model (the Claude command line wrote XML calls; others put text around the block).
- Now, for Anthropic and OpenAI-compatible providers (Gemini, OpenAI, Azure OpenAI, local servers), each tool is also sent
  as a tool definition: its name, its description and a schema read from the example arguments in its help line (every
  argument optional; the tool says what it needs). When the reply is a tool call, the adapter writes it back as the block
  the loop reads, alone, whatever else the model said. One call per step, so the loop, its budget, the permission checks,
  autopilot and the history are untouched, and the model is still shown the conversation as text.
- Not native: Bedrock, and the Claude Code and Copilot command-line backends (their own tool formats). They keep the text
  block. A tool whose name providers refuse (the dots in some MCP tool names) is left to the text block as well.
- Arguments are now read loosely: `"30"` where a number is wanted and `12` where text is are accepted.
- Next, if the text history proves to confuse a model: send tool results back as native results too.
