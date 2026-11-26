---
title: Models and providers
parent: Guides
nav_order: 4
---

# Models and providers

Agents, teams, workflows and the assistant need a model to answer. **Settings → Models and providers** is where an
administrator connects model providers, turns their models on, sets spend limits and fallbacks, and sees every model
call as it happens. A model from a provider works whatever the assistant itself is set to, so agents and workflows keep
running with the assistant off.

Use it to choose which models your agents may use, to cap what a provider can cost per month, or to run agents on a
local model with no key.

## At a glance

| | |
| --- | --- |
| Where in the console | **Settings → AI → Models and providers**; **Settings → AI → Assistant model** for the assistant |
| Needs | An API key for the provider (none for AWS Bedrock or a local server without one) |
| Design | [Note 045](../design/045-agent-studio.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| See providers, their spend and the call log | Developer | The key is never shown |
| Add, change, test or remove a provider | Administrator | Every change is audited. At most 20 providers and 50 models each; a monthly limit up to $1,000,000; prices 0 to 1,000 dollars per million tokens |

## How it works

1. An administrator connects a provider and ticks the models agents may use. The key goes into the vault.
2. An agent, team or workflow names a model as `provider/model`. Each call goes to that provider and is logged with its
   tokens and cost.
3. When the provider's monthly limit is reached, or the provider is unreachable, rate limited or overloaded, the call
   goes to the **fallback** model if one is set. Otherwise it stops. A rate-limited call (HTTP 429) is retried twice, after 3
   and 8 seconds, before it falls back.

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
- **Test** asks the first model turned on to reply with one word and shows the time and the cost; with no model on it says "turn on a model first".

Keys are kept encrypted in the vault, are never shown again, and are deleted with the provider. Every change is in the
audit log.

## Use a model

A model from a provider is written `provider/model`, for example `anthropic/claude-haiku-5-5` or `ollama/llama3.2:3b`.

- **An agent**: pick its model in the agent's form; models are grouped by provider. An agent with no model of its own
  uses the assistant's.
- **The assistant**: under **Settings → AI → Assistant model** choose *A model from Models and providers* and pick one.
- **Teams and workflows** use each agent's own model, so one team can mix a quick model for routing with a stronger one
  for analysis.

## The call log

**Recent model calls**, beside the provider cards, refreshes every 15 seconds. Each row says who called (the agent, the
team, the assistant or a test), the model, the tokens in and out, the cost and the time, or the error. Each provider's
card shows whether it is working, its spend against the monthly limit and its median answer time.

## Run a free local model

An OpenAI-compatible provider pointing at [Ollama](https://ollama.com) on the same machine runs agents with no key and
no cost: install Ollama, pull a small model (`ollama pull llama3.2:3b`), add the provider with the address
`http://localhost:11434/v1`, and add `llama3.2:3b` under Models. Small models follow instructions less reliably; try one
on an agent's page before giving it to a team.

## Security model

- Keys are kept encrypted in the vault, are never shown again, and are deleted with the provider.
- Only `https` addresses are allowed, or plain `http` for this machine. Private and cloud-internal addresses are
  refused unless you tick **It is on a private network**.
- AWS Bedrock calls are signed with a connected account's credentials, so no key is stored.
- A refused key or a denied permission does not fall back to another model: that needs a person.
- Every change is in the [audit log](audit-log.md).

## Limits and verification

{: .warning }
**Only the Anthropic provider was checked against the real API.** That was on a Raspberry Pi on 2026-11-18: the Test
button, and an agent on `anthropic/claude-haiku-5-5` answering while the assistant used a different login. The other
providers are tested against stand-ins only.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| A provider's card shows **no-key** or **failing** | The key is missing or refused, or the provider cannot be reached. Use **Test** to see the error. |
| An agent fails with a clear error | Its provider was removed. Give the agent another model. |
| "provider ... has no API key" | The key is missing; edit the provider and enter it. |
| "api_key: the provider needs an API key" | A provider other than Bedrock or a keyless local server was saved without a key. |
| "its fallback ... failed too" | The fallback model also failed; the message gives both errors. |
| Calls stop at the monthly limit | No fallback is set. Add a fallback or raise the limit. |
| Calls do not fall back after an error | A refused key or a denied permission needs a person; only a limit, an unreachable provider, rate limiting or overload fall back. |
| An address is refused | It is private or cloud-internal. Tick **It is on a private network** if it is yours. |
| Bedrock calls are denied | The account's role lacks `bedrock:InvokeModel`. |
| A small local model ignores instructions | Small models follow instructions less reliably; try one on an agent's page before giving it to a team. |

## Related

- [Connect AWS](connect-aws.md)
- [Versioning](versioning.md)
- [People and roles](people-and-roles.md)
- [API: models and providers](../api/reference.md)
