---
title: Models and providers
parent: Guides
nav_order: 4
---

# Models and providers

Agents, teams, workflows and the assistant need a model to answer. **Settings › Models and providers** is where an
administrator connects model providers, turns their models on, and sees every model call as it happens. A model from a
provider works whatever the assistant itself is set to, so agents and workflows keep running with the assistant off.

## Connect a provider

Press **Add a provider**, or **Connect** on one of the cards under *More providers*. A side panel opens:

| Provider | What it needs | Notes |
| --- | --- | --- |
| **Anthropic** | An API key | Claude Haiku, Sonnet, Opus and Fable, with list prices filled in |
| **OpenAI** | An API key | GPT-6 Luna, GPT-6.1 Terra and GPT-6.1 Sol, with list prices filled in |
| **Google Gemini** | An API key | Gemini Flash-Lite, Flash and Pro, through Google's OpenAI-compatible endpoint |
| **Azure OpenAI** | The resource's address (`https://<resource>.openai.azure.com/openai/v1`) and its key | Models are your **deployment names**; add them by name |
| **AWS Bedrock** | One of the AWS accounts the console is connected to, and a region | No key: calls are signed with that account's credentials. Its role needs `bedrock:InvokeModel`. Add model ids or inference profiles by name, such as `us.anthropic.claude-haiku-5-5-v1:0` |
| **OpenAI-compatible** | The server's address, ending in its version path | Ollama (`http://localhost:11434/v1`), vLLM, LiteLLM, OpenRouter, Groq and anything else that speaks the Chat Completions API. A key only if the server asks for one |

In the panel:

- **Models**: tick the ones agents may use. Each has an input and an output price in dollars per million tokens, used to
  count spend; change them to match your contract. **Add** takes any other model by its exact name.
- **Monthly limit**: when the provider's spend this month reaches it, calls go to the fallback or stop.
- **Fallback**: another model, from this provider or another, used when the limit is reached or the provider is
  unreachable, rate limited or overloaded. A refused key or a denied permission does not fall back: that needs a person.
- **Address**: leave it empty for the provider's own API. Only `https` is allowed, or plain `http` for this machine;
  private and cloud-internal addresses are refused unless you tick **It is on a private network**.
- **Test** asks the first model turned on to reply with one word and shows the time and the cost.

Keys are kept encrypted in the vault, are never shown again, and are deleted with the provider. Every change is in the
audit log.

## Use a model

A model from a provider is written `provider/model`, for example `anthropic/claude-haiku-5-5` or `ollama/llama3.2:3b`.

- **An agent**: pick its model in the agent's form; models are grouped by provider. An agent with no model of its own
  uses the assistant's.
- **The assistant**: under Settings › Assistant model choose *A model from Models and providers* and pick one.
- **Teams and workflows** use each agent's own model, so one team can mix a quick model for routing with a stronger one
  for analysis.

## The call log

**Recent model calls**, beside the provider cards, refreshes every 15 seconds. Each row says who called (the agent, the
team, the assistant or a test), the model, the tokens in and out, the cost and the time, or the error. Each provider's
card shows whether it is working, its spend against the monthly limit and its median answer time.

## A free local model

An OpenAI-compatible provider pointing at [Ollama](https://ollama.com) on the same machine runs agents with no key and
no cost: install Ollama, pull a small model (`ollama pull llama3.2:3b`), add the provider with the address
`http://localhost:11434/v1`, and add `llama3.2:3b` under Models. Small models follow instructions less reliably; try one
on an agent's page before giving it to a team.

## What was checked

The Anthropic provider was checked with the real API on a Raspberry Pi (2026-11-18): the Test button, and an agent on
`anthropic/claude-haiku-5-5` answering while the assistant used a different login. The other providers are tested
against stand-ins only.
