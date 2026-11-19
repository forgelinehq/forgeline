# 045: Agent Studio: any model, a playground, agent teams, publishing, code mode

Status: **accepted 2026-11-18; step 1 under way: all six kinds of provider are built.** All five decisions are made (end of
this note). The Anthropic provider was checked on the Pi with the real API on 2026-11-18. Builds on agents and skills
(note 024), MCP servers (042), budgets (036), the library (040, step 8) and agent graphs (044). Screen mock-ups were
drawn for the maintainer's review alongside this note.

## Question

The maintainer asked for:

- several model providers at once, with agents and workflows working whatever the assistant is set to;
- any agent running on any model, and a playground to try them;
- MCP servers, skills and other settings given to agents and to workflows;
- agents, and teams of agents, inside workflows;
- Cloudflare's code mode;
- publishing agents and running them headless;
- a playground for workflows;
- and a better experience overall ("I am still not happy with the UX").

What should these be in Forgeline without loosening the trust model? The trust model: pinned versions; writes only
through proposals that a different administrator approves; never above the starter's role; budgets; everything
audited.

## Where Forgeline is today

- **One model backend for everything.** Settings › Assistant picks one backend: the Anthropic API with a key, the
  `claude` program (with the server's login or a vault token), or the `copilot` program. Agents, workflows and the
  assistant all use it. An agent may name a model (note 044, part 3), but only a model of that one backend.
- **Tool calls already work with any model.** Agents call tools through a text protocol (a `lookup` block, then a
  `lookup_result` block marked as data), not through a provider's native tool calling. A new provider therefore only
  needs "send messages, get text back".
- **Agents** already have pinned skills, tools (built-in lookups, plugin tools, `mcp_<server>_<tool>`), a tier (T0
  reads, T1 may propose), a model and a budget. The agent page is a form; there is no place to try an agent other than
  the assistant panel.
- **Workflows** are graphs with a canvas and a run view, but the only way to try one is a real run.

## Decisions (proposed)

### 1. Providers and models

| Decision | Choice | Why |
| --- | --- | --- |
| Where | A new page, **Connect › Models and providers**. An administrator adds any number of providers; each lists its models, and a model must be turned on before agents can pick it | Agents stop depending on the assistant's single backend |
| Provider kinds | **Anthropic** (API key); **OpenAI** (API key); **Google Gemini** (API key, Vertex later); **AWS Bedrock** (uses an existing AWS connection's role, so no key is stored); **Azure OpenAI** (endpoint and key); **OpenAI-compatible** (a base URL: Ollama, vLLM, LiteLLM, OpenRouter, Groq and others); and the existing **Claude Code login** and **GitHub Copilot** programs | Covers hosted, cloud-native and self-hosted models with six adapters |
| Model names | Everywhere a model is chosen it is written `provider/model`, for example `bedrock/anthropic.claude-sonnet-5-5` or `ollama-pi/qwen3:8b`. Existing agents with a bare model name keep using the assistant's provider | One unambiguous name; old versions keep their fingerprints |
| Tool calling | Keep the text protocol for every model. Later, use a provider's native tool calling where it has one, behind the same checks | Works with any model today; native calls are an optimisation |
| Secrets and addresses | Keys go into the vault and are never shown again. Base URLs get the MCP server rules (note 042): HTTPS only, no redirects, and no private, loopback or metadata addresses unless an administrator ticks "private network" (needed for Ollama on the Pi) | The same SSRF protection as MCP servers |
| Prices and limits | Each model has an input and output price (filled in for known models, editable). Use is counted per provider, per agent and per workflow. A provider can have a monthly limit and a fallback model | Budgets (note 036) work across providers |
| The assistant | The assistant gets its own `provider/model` setting. The existing setting becomes the first provider when upgrading | Nothing breaks |

### 2. The agent builder and playground

- One screen per agent: **setup on the left** and the **playground on the right**.
  - Setup holds the model, the instructions, tools (built-in, MCP and plugin tools in one list), skills, code mode,
    limits and tier.
  - The playground runs the unsaved draft as the person using it, with real read tools. Every tool call is shown as
    a card that opens, with tokens, cost and time for the turn.
- **In the playground, a T1 agent cannot file real proposals.** A proposal it would file is shown as a draft marked
  "from the playground", never sent for approval.
- **Test cases**: save an input with checks. A check is a rule (called a tool, said a word, never said a word) or a yes
  or no from a judge model. Test cases run on demand, before publishing, and in Compare.
- **Compare models**: the same draft and input on two to four models side by side, with time, cost, tool calls and checks
  per model, then every test case on every model in one grid. "Use this model" sets it on the draft.
- MCP servers and skills stay where they are added (Connect › MCP servers, Library). The builder only picks from them,
  and shows "2 of 41 tools" per server.

### 3. Workflows: settings, agent teams and a test panel

- **Workflow settings**: a default model for agent steps that have none; skills given to every agent in it; a budget per
  run; who it runs as. These are part of the version.
- **An "Agent team" step**: several agents inside one step, working in one of three ways:
  - a **lead hands out work**, then sums up;
  - **all at once**, then one agent sums up;
  - **take turns** until they agree or reach a round limit.

  It is built from the existing graph engine as a sub-graph, so the same limits, checkpoints and audit apply. Graphs
  calling other graphs ("Another workflow") come with it.
- **A test panel under the canvas**, as in n8n:
  - an input you can pin (taken from a past run, or typed);
  - "pause after each step";
  - "answer questions myself";
  - re-run from a chosen step, using the saved state before it.

  A test run is marked as one and cannot write: proposals are drafts, and triggers ignore it.
- **The palette** groups steps as Start (by hand, schedule, webhook, run failed, Watch alarm), AI (agent, agent team,
  agent decides), Data (lookup, MCP tool, code) and Flow (rule decides, ask a person, merge, another workflow, end).

### 4. Publishing and running headless

- **Publishing** marks one fixed version as live. Callers always get the live version until it is moved, and moving it
  back is one click. "Try 10% of calls first" sends some calls to a new version before it goes live.
- **Where a published agent or workflow can be called:**

  | Where | How | Status |
  | --- | --- | --- |
  | API | `POST /v1/agents/<name>/runs` with an agent key. It answers at once, streams, or returns a run id to check later | Proposed |
  | MCP | The agent becomes one tool on Forgeline's MCP server (note 020), so Claude Code or an IDE can call it | Proposed |
  | In a workflow | Live, or pinned to a version | Proposed |
  | Chat page | Signed-in people can talk to it | Proposed |
  | Slack or Teams | — | Later |

- **Agent keys** are scoped to one agent (or workflow) and one channel. Each has its own limits: calls a minute, spend a
  month, input size. A key runs at most the developer role and never above its maker's role. Keys are audited like
  every other credential.
- **Headless**: `forgeline agent run <name> --input file.json` and a small runner image use the API with a runner
  token. Model calls and tool calls still go through the server, so keys, checks and the audit stay in one place.
- **Exporting** an agent to run with no Forgeline server at all is **not planned** (decision 3). It would carry its own
  keys and skip the audit.

### 5. Code mode

Cloudflare's code mode gives the model the agent's tools as a typed API and lets it write one short script, instead of
calling tools one at a time. The script runs in an isolated sandbox. Cloudflare's version runs it in a Worker made at
run time by the Worker Loader; the credentials stay with the host and the sandbox has no network by default
([Cloudflare docs](https://developers.cloudflare.com/agents/tools/codemode/how-it-works/)).

For Forgeline:

| Decision | Choice | Why |
| --- | --- | --- |
| What the model sees | A TypeScript declaration of the agent's own tools (`tools.runs.get`, `tools.github.search_issues`…), made from their input schemas | Only granted tools exist |
| How tools are called | Each `tools.x()` in the script is a request back to Forgeline. Forgeline runs the same checks as a direct call: the grant, the tier, argument checks, size and time limits, and the audit. Each call counts toward the agent's tool-call limit | The sandbox never holds a credential and cannot get around a check |
| Local sandbox (first) | A container like the policy packs': JavaScript with every permission denied, `--network none`, a read-only root, all capabilities dropped, memory, CPU and time limits. Tool calls go over standard input and output | Principle 3 (containers, not in-process code) and local first |
| No Cloudflare account | **Decided by the maintainer (2026-11-17): local only.** Code mode is the technique (typed tools, one script, a sandbox), not Cloudflare's service. The runtime in the container is a choice to evaluate: Deno with every permission denied, or `workerd`, Cloudflare's open-source Workers runtime | The maintainer wants the technique, not a paid plan |
| Who may turn it on | An agent setting. Off by default; shown on the agent's card | Visible, reviewable |

### 6. The experience

The mock-ups show six screens: the Agents home, Models and providers, the agent builder with its playground, Compare
models, the workflow studio with its test panel, and Publish. The rules behind them:

- **Agents become a top-level area** in the sidebar: Home, Agents, Workflows, Playground, Published, Runs. Providers,
  MCP servers, skills and clouds move to **Connect**.
- **Every editor has a place to try what it edits**, on the same screen.
- **Every run has a trace**: steps, tool calls, model, tokens, cost and time.
- **Plain words**: "Make v5 live", not "promote"; "Reads only", not "T0". Tier names stay in the details.
- **One look**: neutral background, one blue accent, status shown by both colour and text.
- **Pickers in a side panel** (version 2 of the mock-ups, asked for by the maintainer). The agent screen shows what the
  agent is made of as parts: instructions, skills, MCP servers, tools, guardrails. **Add** on any part, or the model chip
  in the header, opens one side panel with tabs for Models, Skills, MCP servers, Tools and Guardrails. Each tab has search,
  filters, and items grouped by provider or server with their logo, what each one does, and whether it reads or changes
  things. "Connect a new MCP server" opens a popup: pick from the catalog or bring your own, sign in, choose the tools,
  and add them to the agent in one go. The side panel closes back to the playground.
- **Logos** for providers and MCP servers, taken from each company's published brand assets where its guidelines allow,
  and a monogram otherwise. The mock-ups use monograms.
- **Providers as cards** with status, models on, spend against the limit and median speed, and a live **log of model
  calls** (who called, which model, tokens, cost, time, failures).

### 7. Guardrails for agents

A Guardrails part on every agent and workflow, in the side panel:

- **Always on, shown locked:** changes need a different administrator; tool results are data, never instructions.
- **Input and output:** hide secrets and personal data in answers; screen inputs for prompt injection; check the answer
  against declared fields.
- **Limits:** tool calls per run, spend per run and per month, and the web addresses MCP servers and code mode may use.

The library's guardrail packs (note 040, step 8) can add more.

## Order of work

1. **Providers**: the page, the six adapters, `provider/model` names, the assistant's own setting, prices and limits.
2. **The agent builder and playground**: the new screen, test cases, Compare. *Built so far (2026-11-19): the builder
   screen with its parts and the one side panel (Models, Skills, MCP servers, Tools, Guardrails); talking to it uses the
   saved version. Still to do: running an unsaved draft, test cases, Compare.*
3. **The workflow studio**: the test panel, re-running from a step, workflow settings, agent teams, workflows calling
   workflows, and the Watch alarm trigger left over from note 044.
4. **Publishing**: live versions, agent keys, the API, the MCP tool, the CLI and the runner image.
5. **Code mode**: the local container sandbox.
6. **Moving the rest of the console** to the new sidebar and look.

Each step is checked on the Pi with real models before the next one.

## Decisions for the maintainer

1. ~~Which providers come first?~~ **Anthropic first; the others after it** (maintainer, 2026-11-18). Built: the
   providers page (Settings › Models and providers), `provider/model` names for agents and the assistant, keys in the
   vault, the MCP address rules, prices, a monthly limit with a fallback, and a log of recent model calls. Then OpenAI
   and OpenAI-compatible servers (Ollama, vLLM, LiteLLM, OpenRouter, Groq), through the Chat Completions API; a
   compatible server needs its address, may have no key, and its models are added by name. Then Gemini (Google's
   OpenAI-compatible endpoint), Azure OpenAI (the v1 API, deployment names as models) and AWS Bedrock (the Converse
   API through the AWS CLI and a connected account, prompts passed in private files); these three are tested against
   stand-ins only, not yet against the real services. Checked on the Pi with the real Anthropic API (2026-11-18): the providers page test, and an agent on
   `anthropic/claude-haiku-5-5` answering while the assistant itself used a Claude Code token, its call priced in the
   log.
2. ~~The code-mode sandbox~~: **local only** (maintainer, 2026-11-17).
3. ~~Headless~~: **only through the server, as proposed** (maintainer, 2026-11-18). No standalone export.
4. ~~A public route on the Pi~~: **yes** (maintainer, 2026-11-18). The exact address and the firewall or tunnel change
   are proposed separately and made only after the maintainer approves that exact change.
5. ~~Playground spending~~: **playground runs count against the agent's monthly budget, as proposed** (maintainer,
   2026-11-18).
