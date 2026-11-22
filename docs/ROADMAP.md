# Roadmap

Each milestone has exit criteria. A milestone is done only when its criteria are met and
demonstrated, not when the code exists. The order puts the trust model early because it is
the project's differentiator.

Status as of 2026-11-22: M0, M1, M2 and M2c (the console) are built; M3 slices 1 and 2 are built and 3 to 5 are not
started; M4 and M6 are partly built through agents, traces, evals and the sandboxed assistant (see the sections below the
table); M5 and M7 are not started. The current state, what is verified only against stand-ins, and the ordered next steps
are in [PLAN.md](PLAN.md).

| # | Milestone | Exit criteria | Status |
| --- | --- | --- | --- |
| M0 | Foundation | Public repo, Apache-2.0 license, README, roadmap, architecture notes, Go skeleton that builds and tests locally | Done |
| M1 | Pipeline spec and local engine | Typed pipeline schema; YAML loader; DAG executor running container steps; `forgeline run` executes a sample pipeline on a laptop with caching of unchanged steps | Done |
| M2 | Control plane and UI | Single-binary server with Postgres; run history and live logs; GitHub webhook trigger with commit status; a DAG run view in the UI | Built. The GitHub round trip is verified only against a stand-in; see PLAN.md |
| M2b | Onboarding and connectors | Sign-in wizard: connect GitHub and AWS, discover ECS resources, reviewable plan, approved setup, connection tests, and a GitHub-to-AWS deploy workflow; see [design/004](design/004-onboarding-and-connectors.md) | Partly overtaken by the console (AWS read-only view, GitHub connection); to be re-scoped |
| M2c | Web console | Sign-in and roles; pipelines edited in the browser; secrets, GitHub and AWS connected from the UI; platform and cloud overview; cloud changes through proposal and approval; an audit log; see [design/006](design/006-console.md) | Built; see its list of what was not checked. Redesign for non-specialists in progress: [design/029](design/029-console-spaces-and-home.md) and [030](design/030-service-switches.md): Home, grouped sidebar, AWS service switches, visual system, assistant panel, agent screens, five spaces with tabs, the new Home and the Connections page built. Single sign-on (OIDC, roles from groups) built: [design/031](design/031-single-sign-on.md), not yet checked against a real provider. Approval rules built: [design/032](design/032-approval-rules.md). Audit export and retention built: [design/033](design/033-audit-export.md) |
| M3 | Trust and sandbox model | Per-step declared permissions; secrets scoped to steps; untrusted input isolated from secrets; audit log; a published threat model with tests that try to break it | In progress: slices 1 and 2 done (network permission, secrets scoped to steps); slice 3 (trust of the run), 4 (audit of secret grants) and 5 (limits, capability drop) not started; slices 3 and 5 wait on decisions in [note 005](design/005-threat-model.md). Separate from step containers, the assistant's sandbox (note 052) already drops capabilities and has no Docker socket |
| M4 | Security gates | Scanner orchestration; findings normalized from SARIF; policy gates with expiring waivers; SBOM and image signing | Mostly not started. Policy packs (Trivy, Checkov, Conftest) check every blueprint preview (note 040) and Inspector and Guard findings are read in the console; scanner steps in pipelines, SARIF, waivers, SBOM and signing are open |
| M5 | Progressive delivery | One declaration for canary and blue-green on Kubernetes with metric-driven rollback | Not started |
| M6 | AI agents | Governed agent steps with scoped credentials and cost tracking; failure triage; fix suggestions as pull requests, never auto-applied | Largely built beyond the original wording: agent steps (note 007), agent graphs, teams, schedules, Agent Studio with six model providers, budgets and cost per agent, traces, evals with a publish gate, missions, runbooks, and the assistant as a sandboxed agent (notes 044 to 053). Open: eval attack suites and a pipeline eval step, failure triage as pull requests, Codex and Copilot as pipeline steps |
| M7 | Migration and breadth | Importers for Jenkinsfile and GitHub Actions workflows; Bitbucket and GitLab triggers; additional cloud targets | Not started |

## Assistant as a colleague and Claude Code as the agent (notes 052 and 053)

Status: **built in slices (2026-11-21 and 22).** The Claude Code engine in the sandbox and native Claude tool calls were run on the Pi; the Agent Client Protocol path is tested against a stand-in agent only, and no real Copilot, Codex or Antigravity session has run. Note 052: a budgeted loop with a plan and `ask_user`,
AWS changes after the person approves the exact command, a sandbox container for commands and code, autopilot, memory as
files, and native tool calls for Anthropic and OpenAI-compatible providers. Note 053: Claude Code as the assistant's agent,
with Forgeline as its tools and its permission prompt, running inside the sandbox container; any coding agent over the Agent
Client Protocol (presets for Copilot, Codex and Antigravity); one autopilot gate for every approval point. Open: the sandbox is off by
default (an administrator switch); validity dates, a "keep this" card and memory compaction; a host shell mode; a microVM;
streaming partial answers; Antigravity (`agy`) needs sign-in before it opens a session. See
[note 052](design/052-assistant-that-works-like-a-colleague.md) and [note 053](design/053-claude-code-as-the-agent.md).

## Runbooks (note 051)

Status: **slice 1 built (2026-11-21).** Scripts one administrator writes and another reviews, with typed parameters, run
on servers through SSM: checks run when asked (by people, agents and workflows); every fix waits for a different
administrator. Three examples for a build server whose disk fills up. Checked against stand-ins only. Next: a CloudWatch
alarm workflow (SNS to a signed webhook), then more AI SRE checks. Starts phase 4 of the product direction early. See
[note 051](design/051-runbooks.md).

## Evals (note 050)

Status: **slices 1 and 2 built (2026-11-20); attack suites and the `eval` pipeline step built (2026-11-22).** Suites of cases per agent with fixtures and checks (text, JSON, fields,
tools called or never called, budgets, an LLM judge), runs of one to five attempts per case (pass only if all pass)
on any version and model, with nothing changed, and cases from traces; and a gate: a new version is a candidate until
its suite passes on it and it is published. Attack suites: seven ready-made cases from the OWASP agentic list, added to an agent in one click, checked with stand-in agents only. An `eval` pipeline step runs a suite and fails the step unless every case passes (2026-11-22). Next: slice 4, none designed. Phase 2 of the product direction. See [note 050](design/050-evals.md).

## Traces (note 049)

Status: **built (2026-11-20).** Every assistant chat, agent run, workflow run, team run, mission and pipeline run is
a trace of spans (agent runs, model calls, tool calls, workflow nodes and waits, mission stages, pipeline steps) with
the OpenTelemetry GenAI names, kept 14 days, shown under Agents › Runs and traces and sent to any OpenTelemetry backend
over OTLP (checked with Jaeger). Prompts and answers are kept only when an administrator turns that on. Phase 1 of
the product direction; phase 2, evals, comes next. See [note 049](design/049-traces.md).

## Console structure and icons (note 048)

Status: **built (2026-11-20).** Seven spaces named after what people look after (Home, Agents,
Workflows, Delivery, Cloud, Operations, Security), Lucide icons throughout, and an agent's instructions written in
sections, workflow node panels and settings in tabs with chips and cards, and a search box (Ctrl K) for any page or thing. See
[note 048](design/048-console-structure-icons-and-editors.md).

## Agent Studio (note 045)

Status: **step 1: all six kinds of provider are built** (Anthropic, OpenAI, OpenAI-compatible such as Ollama, Gemini, Azure OpenAI, AWS Bedrock) (Settings › Models and providers: keys in the vault, prices,
monthly limits with a fallback, a live log of model calls; agents with a `provider/model` work while the assistant is
off). Anthropic checked on the Pi with the real API; the others only against stand-ins. Planned: several model providers at once, with each agent naming `provider/model`; an agent builder with a playground, test cases and model comparison; agent teams and a test panel in
workflows; publishing agents as an API, an MCP tool or a headless job; Cloudflare-style code mode in a sandbox. Step 2 has begun: the agent builder with its side panel is built. Exporting
agents (a bundle with Agent Skills, Docker Agent YAML, an OCI artifact, an A2A card) is accepted in
[note 046](design/046-agent-export.md); downloading a bundle or a Docker Agent file, importing a bundle, and pushing to a registry as an approved proposal are built. Schedules for workflows and teams (presets, cron in a time zone, one list with pause and run now) are built: [note 047](design/047-schedules.md). See [note 045](design/045-agent-studio.md).

## Agent graphs (note 044)

Status: **parts 1 to 4 built.** Workflows as graphs of agents, lookups, decisions, questions for people, merges and ends,
with shared state, bounded loops and every run drawn node by node on a canvas; a model per agent; starts from a signed
webhook or a failed pipeline run. Checked with the real model on the Pi
([2026-11-17](testing/2026-11-17-agent-graphs.md)). Next: triggers from Watch (an alarm), and graphs calling graphs if
the maintainer wants them. See [note 044](design/044-agent-graphs.md).

## Native image builds (note 043)

Status: **slice 1 built (2026-11-13).** A `build` step builds with BuildKit and pushes to ECR only for runs GitHub
started for a push to a branch a push rule allows, with a short-lived token; pull requests build without pushing.
Checked with real ECR on the Pi ([2026-11-13](testing/2026-11-13-native-builds.md)). Slice 2 (built): console runs
download a pipeline's source repository and branch. Slice 3 (built): a blueprint block's "Built by Forgeline"
suggestion and one-click push rule. Next: pipeline schedules, and letting chosen console runs push. See
[note 043](design/043-native-image-builds.md).

## MCP servers you add (note 042)

Status: **slice 1 built (2026-11-13).** Remote MCP servers over HTTPS and local ones as containers; tools discovered,
off until turned on, read-only only, definitions pinned; granted to agents one by one and to the assistant per server.
Checked on the Pi with real servers ([2026-11-13](testing/2026-11-13-mcp-servers.md)): remote servers, local containers (also as the image's own user), tools marked read-only by an administrator and list
arguments all work there; a host firewall needs one rule for `FORGELINE_EGRESS_PORTS`. Tools that change
things (through approval), OAuth sign-in, resources and prompts come later. See [note 042](design/042-mcp-servers.md).

## Next: more blueprint blocks (note 039)

Status: **built (2026-11-08).** Databases (RDS MySQL or Postgres, with the password created and kept by AWS),
Redis, secrets whose values Forgeline never sees, one service calling another, autoscaling, and several paths behind
one front door. See [note 039](design/039-more-blueprint-blocks.md).

## Removing what a blueprint built (note 041)

Status: **built (2026-11-12).** A removal is a preview approved like a build: keep the data, or remove everything in two
approved steps. Checked with tests, `tofu validate` and a browser walk against stand-ins; not yet against real AWS. See
[note 041](design/041-removing-what-a-blueprint-built.md).

## Next: any resource, any cloud (note 040)

Status: **step 1 built (guardrails on every preview); step 2 partly built: the template format, template blocks on the
canvas, and Lambda function and Step Functions workflow templates checked on real AWS
([2026-11-15](testing/2026-11-15-lambda-template.md), [2026-11-16](testing/2026-11-16-step-functions-template.md)); the
AgentCore template waits for the account's quota; step 3 built (Fargate Spot on app blocks instead of an ECS cluster
template); step 4 built: repositories linked to blocks, with suggested workflows, which repository fits a block, and
running a newly pushed image in one step ([2026-11-16](testing/2026-11-16-run-new-image.md)); step 5 built: the data
pipeline template ([2026-11-16](testing/2026-11-16-data-pipeline.md)) and EKS with namespaces
([2026-11-16](testing/2026-11-16-eks-namespaces.md)); step 6 built: policy packs (Trivy, Checkov or your own Conftest rules) check every preview in
locked-down containers ([2026-11-17](testing/2026-11-17-policy-packs.md)); step 7 built: a resource block for any
AWS resource type an administrator allows, checked against the provider's schema
([2026-11-17](testing/2026-11-17-resource-block.md)); step 8 built: Team › Library, with guardrail packs, tools and templates beside
agents and skills. Left in note 040: the AgentCore agent template (waits for the account's quota), then other clouds.** Templates for whole solutions (an AgentCore agent, Lambda, Step Functions, ECS clusters, EKS with
namespaces, data and code pipelines), repositories linked to what runs them, guardrails on every plan (built-in rules,
then Conftest, Checkov or Trivy packs), a generic block for any resource type an administrator allows, and a library for templates, skills, tools, agents and guardrail packs. Guardrails come
first, and everything is AWS only until it is complete; then Google Cloud, then Azure, then RunPod and others. See [note 040](design/040-any-resource-any-cloud.md).

## Next: onboarding by conversation (note 038)

Status: **research done, needs a decision.** New-generation agents (OpenClaw's `openclaw onboard`, Hermes Agent's
`hermes setup`) get people working by asking questions, and can use a small model on the person's own machine.
First-run steps in the console and `forgeline onboard` are built. Next, revised by the maintainer: no llama.cpp or Ollama
and well under 500 MB, so a 25 MB embedding model running in the browser to match answers and find pages, then
FunctionGemma 270M fine-tuned on Forgeline's own steps only if an evaluation on the Pi shows it is reliable. See [note 038](design/038-conversational-onboarding.md).

## Open decisions

- Step execution engine. Proposed: write a minimal executor behind an interface and treat
  Dagger as an interoperable step type. See [design/001](design/001-execution-engine.md).
- Workflow state: a Postgres-backed executor to start, with the engine kept swappable.
- Policy language for gates (candidates: OPA, CEL). Not yet researched.
- Plugin model beyond container steps. WASM components are a candidate for small policy hooks.
- Verification of project name availability beyond a basic search. Not a trademark clearance.
