# 038: Onboarding by conversation, with a small model that runs on a CPU

Status: **Step 1 built (2026-11-08): the first-run steps, in the console and `forgeline onboard`.** The maintainer chose to build steps 1 and 2; step 2 (the local-model backend) is next.

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

## Step 1: built

| Decision | Choice |
| --- | --- |
| Steps | Create the first administrator; bring in your team (single sign-on, or more than one person); connect GitHub; connect a cloud account (AWS, Google Cloud or Azure); turn on the assistant; choose what the AI may touch (the access policy saved at least once); make a first pipeline or blueprint |
| Done | Read from the server's own state each time (`GET /v1/onboarding`), never ticked by hand, so a step done another way still counts |
| Skip | Administrators skip a step or bring it back (`PUT /v1/onboarding/{step}`); audited. A done step is never shown as skipped |
| Console | Home shows **Set up Forgeline** (progress, the next step's button, every step) until all are done or skipped; **Settings › Setup** any time |
| Terminal | `forgeline onboard` prints the steps and asks what a terminal can answer: the assistant (the claude program on the server, or not now) and AWS (a profile the server's user has, and a region). Everything else is linked to its console page. It never asks for a key or a token, which are entered in the console where they are stored write-only. `--non-interactive` prints the list (exit 0 when finished, 3 otherwise); `--json` prints it as JSON |
| Nothing new can change | Each answer goes through the endpoint the console uses, with the same checks |

Tested: each step turns done when its thing exists; skipping is for administrators, audited, finishes the list and can be
undone; a done step is never skipped. The command against a stand-in server: the token header, a skip, AWS connected with a
profile and region, the claude program chosen, no key or token ever sent, the exit codes. Run against the demo server, and
in a browser: the card on Home, skipping from it, bringing a step back from Settings › Setup.

## Step 2, revised (2026-11-08): very light, not llama.cpp or Ollama

The maintainer does not want llama.cpp, Ollama or a multi-gigabyte model: the helper must stay **within about 500 MB,
ideally far less**, and only do regular things (map an answer to a choice, fill a step's fields, pick the right page),
not be a general assistant. What the research found (vendor docs and blogs; nothing below is benchmarked by us yet):

| Option | Size | What it can do | Runtime |
| --- | --- | --- | --- |
| **Tiny embedding model** (for example all-MiniLM-L6-v2 or bge-small) | about 25 to 35 MB | Match a free answer to the closest known choice or page ("the company SSO one" to the SSO step); rank docs for a question. No text generation | ONNX Runtime Web or Transformers.js, in the browser |
| **FunctionGemma 270M** (Gemma 3 270M tuned for function calling, 32K context) | roughly 150 to 300 MB quantized (to measure) | Turn a sentence into one call with fixed fields, for example `connect_aws(profile, region)`. Built to be fine-tuned: Google reports 58% to 85% on its own task after fine-tuning. An ONNX build exists | Transformers.js or ONNX Runtime Web; LiteRT-LM on devices |
| **Qwen3-0.6B** | about 400 MB at 4-bit | Short answers and tool calls; reported, not verified | GGUF or ONNX |
| BitNet b1.58 2B | about 0.4 GB without embeddings, 1.19 GB file | General text | Needs bitnet.cpp; too large here |

**Proposal for step 2:**

1. **Run it in the browser, not on the server.** The console already runs in the person's browser; ONNX Runtime Web or
   Transformers.js run small models there on the CPU (WebAssembly) or the GPU (WebGPU). The Forgeline server, even on a
   Raspberry Pi, carries nothing new: it only serves the model files, downloaded once on the administrator's request and
   checked against their published checksums, like OpenTofu (note 035). The JavaScript runtime is vendored and pinned in
   the binary, so the CSP stays `'self'`.
2. **Tier 1 first: the 25 MB embedding model** for matching answers and finding pages. It cannot write anything, so it
   cannot write anything wrong.
3. **Tier 2 only if the evaluation earns it: FunctionGemma 270M**, fine-tuned on Forgeline's own onboarding calls, turning
   a sentence into the fields of one step. The person always sees and confirms what it filled in.
4. **The evaluation** (before tier 2 ships): 60 cases (30 onboarding answers, 30 "where do I…" questions) with the
   expected step, page or fields; run on a laptop and on the Pi's browser; published here with sizes and timings.
5. Not a general assistant: questions and agent work stay with Claude.

Sources: [small models under 500 MB](https://www.premai.io/blog/best-lightweight-language-models-worth-running/),
[small LLMs guide](https://codersera.com/blog/best-small-llms-to-run-locally-a-comprehensive-guide/),
[FunctionGemma](https://deepmind.google/models/gemma/functiongemma/), [FunctionGemma fine-tuning](https://ai.google.dev/gemma/docs/mobile-actions),
[FunctionGemma ONNX](https://huggingface.co/onnx-community/functiongemma-270m-it-ONNX), [FunctionGemma at InfoQ](https://infoq.com/news/2026/01/functiongemma-edge-function-call),
[LLMs in the browser with WebGPU](https://pinggy.io/blog/run_llm_in_browser_webgpu/), [WebLLM](https://blog.mlc.ai/2024/06/13/webllm-a-high-performance-in-browser-llm-inference-engine),
[BitNet on CPU](https://wavect.io/blog/bitnet-cpu-llm-ram-requirements/), [llama.cpp alternatives](https://infrabase.ai/alternatives/llama-cpp).
