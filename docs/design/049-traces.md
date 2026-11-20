# 049: Traces of what agents do, with OpenTelemetry

Status: **slice 1 built 2026-11-20**; slice 2 (OTLP export) next. Phase 1 of the product direction the maintainer accepted on
2026-11-20 (traces, then evals, then guardrails and supply chain, then AI SRE). Evals, security alerts and incident
investigation all read traces, so traces come first.

## Question

When an agent answers, Forgeline keeps the answer, an audit line and a line in the model call log. Nobody can see the
run as a whole: which model calls and tool calls it made, in what order, how long each took, what each cost, and where
it failed. Companies already collect this kind of data for their services with OpenTelemetry and look at it in
Grafana, Datadog, Honeycomb or Jaeger. How does Forgeline show a run as a trace, and hand it to the tools they have?

## What the standard says (November 2026)

The GenAI semantic conventions now live in their own repository
([open-telemetry/semantic-conventions-genai](https://github.com/open-telemetry/semantic-conventions-genai)), and every
part is still **Development**, not stable. Checked 2026-11-20:

| What | Span name | `gen_ai.operation.name` | Kind |
| --- | --- | --- | --- |
| An agent run inside the application | `invoke_agent {gen_ai.agent.name}` | `invoke_agent` | INTERNAL |
| A workflow whose control flow the application defines | `invoke_workflow {name}` | `invoke_workflow` | INTERNAL |
| A model call | `chat {gen_ai.request.model}` | `chat` | CLIENT |
| A tool call | `execute_tool {gen_ai.tool.name}` | `execute_tool` | INTERNAL |

Attributes used: `gen_ai.provider.name`, `gen_ai.request.model`, `gen_ai.response.model`,
`gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.agent.id`, `gen_ai.agent.name`,
`gen_ai.agent.version`, `gen_ai.tool.name`, `gen_ai.tool.type`, `gen_ai.conversation.id`, and `error.type` on failure.
Prompts and answers go in `gen_ai.input.messages`, `gen_ai.output.messages` and `gen_ai.system_instructions`, which the
spec makes opt-in; so does Forgeline.

## Decisions (proposed)

### 1. A small tracer of Forgeline's own, no SDK

`internal/trace` records spans in memory along the context, as the SDK does, and hands each finished trace to two
places: Forgeline's store and, when set, an OTLP exporter. OTLP over HTTP with JSON is a stable, documented protocol,
so the exporter is a few hundred lines of standard library. The official Go SDK would add about fifteen modules for what
Forgeline needs; if a later need (metrics, logs, sampling rules) makes the SDK worth it, the span model maps one to one.

### 2. What is traced

- **Slice 1:** every assistant chat, agent chat and agent run (`invoke_agent`), each model call (`chat`), each tool call
  (`execute_tool`, with whether it was refused), and workflow runs (`invoke_workflow`, with a span per node visit and
  `forgeline.node.*` attributes).
- **Slice 3:** missions and team runs, pipeline runs and their steps, proposals filed and approved (as span events, so
  a trace shows "filed proposal p-12" where it happened).

### 3. What is kept, and what is not

- By default a trace has **no prompt, answer, instructions or tool arguments**: names, models, timings, token counts,
  cost, status and error types only. Tool results and prompts can hold anything the agent read.
- An administrator can turn on **Keep prompts and answers in traces**. They are then passed through the same redaction
  as logs (secrets and tokens masked) and cut to 4 KB each.
- A person's user name is kept in Forgeline's store (as the audit log does) but **never exported**; exported spans carry
  the role (`forgeline.role`).
- Forgeline keeps the newest 1,000 traces, and none older than 14 days.

### 4. Where people see it

- **Agents › Runs and traces** lists traces newest first: what started it (assistant, agent, workflow), who, status,
  time, tokens and cost. A filter for agents, workflows and failures.
- A trace opens as a waterfall: each span a bar on one time scale, nested under its parent, with its attributes,
  tokens and cost beside it. Errors are red, refused tools are amber.
- An agent's page and a workflow run link to their traces. Developers and administrators see traces; viewers do not
  (they cannot read agents either).

### 5. Sending it on (slice 2)

- **Settings › Connections › OpenTelemetry**: an OTLP/HTTP endpoint (`https`, or `http` on this machine), headers whose
  values are kept in the vault (an API key for the backend), a service name, and the content setting above. **Send a
  test trace** checks it.
- Export runs in the background in batches; a slow or broken endpoint never slows an agent. The queue is bounded
  (oldest dropped first, counted on the settings page), and a failed batch is retried three times.
- Checked against a local Jaeger before it is called built.

## Order of work

1. The tracer, the store, spans for chats, agents, model calls, tools and workflow runs, and the Runs and traces page.
   **Built:** `internal/trace`, `console_traces.go`, `ui/pages/traces.js`. A question a workflow asks a person is a span
   from when it was asked to when it was answered, with the choice (never a free answer). The streamed assistant
   chat is traced but does not show its trace link yet.
2. OTLP export and its settings, checked against a real collector.
3. Missions, teams and pipelines; proposals as events; content on request.

## Decisions for the maintainer

None block slice 1. Say if you want a different default for keeping prompts and answers (proposed: off), or the user
name exported (proposed: no).
