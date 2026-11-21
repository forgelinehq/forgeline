# 052: An assistant that works like a colleague, not a script

Status: **accepted 2026-11-21: both decisions answered yes (end of this note); slice 1 built 2026-11-21** (the budgeted loop, a visible
plan and `ask_user`; see "Built" at the end). Next: the permission check.
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
