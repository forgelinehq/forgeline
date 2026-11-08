# 038: Onboarding by conversation, with a small model that runs on a CPU

Status: **Research and proposal (2026-11-08). Nothing is built.** Needs a decision (see the end).

## Question

New-generation agents such as OpenClaw and Hermes Agent get a person from "installed" to "working" by asking them
questions, not by handing them a configuration file. Some of them can do it with a model running on the person's own
machine. Could Forgeline do the same: a first-run conversation that sets everything up (the first administrator, GitHub,
the cloud account, the assistant, a first pipeline or blueprint), driven by a small model that runs on a CPU, such as on
a Raspberry Pi, before any cloud model or API key exists?

## What others do (sources at the end; vendor docs and blogs, not benchmarks)

| Product | First run | Model during setup |
| --- | --- | --- |
| **OpenClaw** | `openclaw onboard` is a guided wizard that configures the gateway, channels, skills and workspace defaults in one flow. Re-running never wipes anything unless you choose Reset. A non-interactive form takes flags for scripted installs | Has a Model/Auth step: pick a provider (OpenAI-compatible, Anthropic-compatible or auto-detected), then a default model. Can discover local models (Ollama) and pull the chosen one |
| **Hermes Agent** (Nous Research) | A one-line installer, then `hermes setup` with three modes: Quick (sign in, no API keys), Full (choose every provider and tool), and a model picker you can re-run (`hermes model`) | Any OpenAI-compatible endpoint: Ollama (`ollama launch hermes` installs and configures it), vLLM, Unsloth Studio. Guides suggest large models (one needs 24 GB of GPU memory) for good tool use |
| **Forgeline today** | The first visit asks for the server token and creates an administrator; everything else is found by clicking through Settings › Connections | None until an administrator connects Claude (API key, the claude program, or a token) |

The pattern: **one command or one page, a short series of questions with safe defaults, each answer applied at once
and shown, re-runnable without loss, and a scripted form for automation.** The model is optional for the questions; it
helps with free-form answers ("my AWS is the one my company SSO gives me").

## Small models on a CPU (what the evidence says)

- Candidate models for tool use on a CPU, about 3 to 4 GB of memory at 4-bit: **Gemma 4 E4B** and **Qwen 3.5 4B** scored
  best in one hands-on test of tool calling on stock llama.cpp (94% and 91% of its cases). **Phi-4-mini** made no tool
  calls under llama.cpp's default template in the same test, and needed a custom prompt. These are single-author
  tests; we must run our own.
- Runtimes: **llama.cpp** (MIT; its server speaks the OpenAI chat API, including tools) and **Ollama** (MIT, wraps
  llama.cpp, pulls models). Both run on arm64, so on the Pi. An 8 GB Pi can hold a 4B model at 4-bit; expect a few
  tokens a second, enough for short setup questions, not for agent work.
- Not found: any evaluation of small models on onboarding conversations. That is what our tests must measure.

## Proposal

1. **The questions do not need a model.** Build onboarding as a fixed sequence of steps in the server: who you are
   (first administrator), sign-in (SSO later), GitHub (App), cloud account (AWS SSO, Roles Anywhere or a profile),
   the assistant (Claude by key, by the claude program, or a local model), and a first thing to do (a pipeline from a
   repository, or a blueprint from a starting point). Each step shows what it will do, applies it through the existing
   paths (cloud changes still need approval), and can be skipped or re-run. The same steps power
   `forgeline onboard --non-interactive` for scripted installs.
2. **A local model makes the steps conversational**, optionally. A new assistant backend, **Local model
   (OpenAI-compatible)**, points at llama.cpp's server or Ollama on the same machine. During onboarding it turns a
   free-form answer into the step's fields ("the company SSO one" becomes the SSO start URL and region it lists) and
   explains a step in plain words. It never applies anything itself: the step shows what it understood and the person
   confirms, as the assistant's cards do today (note 028).
3. **After onboarding the local model stays a choice, not the default for agents.** The page says plainly that small
   models are fine for setup questions and short answers, and that missions and agents should use Claude. Budgets
   (note 036) count its use in tokens and time; it has no cost.
4. **Safety.** The local model gets the same rules as any backend: no secrets in its context, lookups only through the
   server, and nothing applied without a person's confirmation. Pinned model files are downloaded only when the
   administrator chooses a model, with their published checksums, like OpenTofu (note 035).

## Build order (each its own pull request)

1. The onboarding steps without a model, in the console (a checklist on Home until done) and `forgeline onboard`.
2. The **Local model** backend (OpenAI-compatible endpoint, tools), with a test against a stand-in server.
3. A small evaluation set: 30 onboarding answers with the fields they should produce, run against Gemma 4 E4B and
   Qwen 3.5 4B on the Pi, and the results published here.
4. Conversational onboarding on top, only if step 3 shows the model gets most answers right; otherwise keep the form.

## Decision needed

Build steps 1 and 2 now, and decide on step 4 after the evaluation? Recommendation: yes. Step 1 helps every new user
with or without a model.

## Sources

- OpenClaw onboarding: [wizard](https://docs.openclaw.ai/start/wizard/index.html), [CLI reference](https://docs.openclaw.ai/start/wizard-cli-reference/index.html), [onboard on Mintlify](https://www.mintlify.com/openclaw/openclaw/cli/onboard)
- Hermes Agent: [quickstart](https://hermes-agent.nousresearch.com/docs/getting-started/quickstart), [local models with Unsloth](https://unsloth.ai/docs/integrations/hermes-agent), [NVIDIA playbook](https://build.nvidia.com/playbooks/hermes-agent/instructions), [with Ollama](https://fast.io/resources/hermes-agent-ollama-local-llm/)
- Small models: [5 small language models for agentic tool calling](https://www.kdnuggets.com/5-small-language-models-for-agentic-tool-calling), [tiny models for Raspberry Pi](https://www.kdnuggets.com/7-tiny-ai-models-for-raspberry-pi), [best CPU-only local LLMs in 2026](https://www.popularai.org/p/best-cpu-only-local-llm-2026), [small LLM tool-calling benchmark](https://ai-radar.it/article/valutazione-di-llm-piccoli-l-importanza-del-parsing-negli-agenti-locali)
