---
title: Traces and OpenTelemetry
parent: Guides
nav_order: 18
---

# Traces and OpenTelemetry

Every assistant chat, agent run, workflow run, team run, mission, pipeline run and runbook run is recorded as a **trace**: what
started it, who, how long it took, its model calls and tool calls, tokens and cost, and whether it failed. A trace opens
as a waterfall of bars on one time scale. Forgeline can also send every trace to the observability tools you already use
over OTLP/HTTP.

Use traces to see why a run was slow, expensive or failed. Use the OpenTelemetry settings to send the same traces to
Grafana, Datadog, Honeycomb, Jaeger or an OpenTelemetry Collector.

## At a glance

| | |
| --- | --- |
| Where in the console | **Agents → Runs and traces**; **Settings → Connections → OpenTelemetry** |
| Needs | Nothing for traces. An OTLP/HTTP endpoint for sending |
| Design note | [Note 049](../design/049-traces.md) |

## Who can do what

| Action | Minimum role | Extra rule |
| --- | --- | --- |
| List and open traces | Developer | |
| Set where traces are sent, send a test trace | Administrator | Header values go to the vault and are never shown again |
| Turn on **Keep prompts and answers in traces** | Administrator | Off by default |

## How it works

1. When something runs, Forgeline records a span for each step: the run, each model call and each tool call. A workflow
   also shows each node and the time it waited for a person.
2. Spans are named as OpenTelemetry's GenAI conventions name them, for example `invoke_agent`, `chat` and
   `execute_tool`.
3. Forgeline keeps the newest 1,000 traces, and none older than 14 days (a trace is dropped when either limit passes).
4. If sending is turned on, the same traces go to your endpoint in the background, in batches.

| Run | What its trace shows |
| --- | --- |
| Agent run | The run, each model call and each tool call |
| Workflow run | Each node, and the time it waited for a person |
| Team run | The lead's and each member's work under it |
| Mission | A span per stage (discover, propose, and verify when something was applied) with the agent's runs and the proposals it filed |
| Pipeline run | Each step, with its exit code |
| Runbook run | A link from the run page (**Open the trace**) |

## Read a trace

1. Open **Agents → Runs and traces**. Filter by **All**, **Agents**, **Workflows**, **Teams**, **Missions**,
   **Pipelines**, **Assistant** or **Failed**.
2. Open a trace. The top shows time taken, model calls, tool calls, tokens in and out, and cost. The bars show each step
   nested under what started it.
3. Click a bar to see its details on the right.

You can also reach a trace from where the run is shown: a trace link under an agent's answer and a team run, and a
**Trace** button on a workflow run, a mission and (for developers) a pipeline run.

By default a trace keeps no prompt, answer, instructions or tool arguments.

![A trace: an agent run with two model calls and a tool call, and the tool call's details]({{ '/console/traces-trace.png' | relative_url }})

## Send traces to your observability tool

1. Open **Settings → Connections → OpenTelemetry**.
2. Enter the **endpoint**. Forgeline adds `/v1/traces`. Plain `http` is accepted only for `localhost` and `127.0.0.1`;
   anything else is refused with `endpoint: must use https (plain http only for localhost or 127.0.0.1)`.
3. Give a **service name** (default `forgeline`) and any **headers**, such as an API key. Values are kept in the vault and
   never shown again. At most 8 headers, each value on one line and at most 4,096 characters.
4. Turn on **Send traces** and choose **Save**, then **Send a test trace**. The test uses the saved settings.

The page counts what was sent, is waiting and was dropped since the server started, and shows the last error. A slow or
broken endpoint never slows an agent.

What is sent is what Forgeline keeps, minus the person's user name.

![The OpenTelemetry settings with counters and the Keep prompts and answers option]({{ '/console/traces-opentelemetry.png' | relative_url }})

### Keep prompts and answers

**Keep prompts and answers in traces** adds the question, the answer, and each tool's arguments and result, cut to 4 KB.
Anything shaped like a credential and the value of every stored secret is replaced by `[hidden]`. It applies to the traces
kept here and to what is sent. Tool results can hold anything an agent read, so turn it on only where that is acceptable.

A trace that kept its prompt can be added to an eval suite as a case.

## Security model

- **No content by default.** Prompts, answers, instructions and tool arguments are not kept unless an administrator turns
  that on.
- **Redaction.** With content on, credential shapes and stored secret values become `[hidden]`, and each field is cut to
  4 KB.
- **No user names.** A person's user name is never sent.
- **Secrets in headers.** Header values are sealed in the vault and never returned.
- **Bounded.** 1,000 traces, 14 days, 8 headers.
- **Audited.** Changes to the settings are audited as `otel.update` and test traces as `otel.test`.

## Limits and verification

{: .warning }
**Checked against a local Jaeger, not a hosted service.** On 2026-11-20 an agent chat arrived in Jaeger 2.23.0 as one trace
with the right nesting, span kinds, GenAI attributes and tokens, and no user name
(`FORGELINE_TEST_JAEGER=127.0.0.1 go test ./internal/trace -run TestSendReachesJaeger`). Grafana Cloud, Datadog and
Honeycomb were **not tried**. A pipeline run's trace was checked with a real Docker run. OpenTelemetry's GenAI
conventions are still in Development, not stable, so attribute names may change
([note 049](../design/049-traces.md)).

**Opened through the console on 2026-11-27**, on the UAT instance at commit `487f373`, as a developer: the traces of a
mission (5 model calls, 3 tool calls, a span per stage with the agent's runs under it), of a workflow run that waited for
two people (a span per node, `ask` spans for the questions), of a team run (the lead's, then each member's `invoke_agent`
and `chat`) and of pipeline runs; the filters All, Workflows, Teams and Failed; a click on a bar showed its details on the
right; a trace link or **Trace** button was on the mission, the workflow run and the pipeline run. No span carried a prompt,
an answer or a tool argument. A viewer and a developer got 403 on the OpenTelemetry settings or the traces list as the
table says, and **Send a test trace** with nothing saved answered 409 "save an endpoint first". Nothing was sent anywhere:
OTLP was not configured in this test (the settings page was only read, and matched its picture). See the
[test note](../testing/2026-11-27-uat-workflows-teams.md).

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| **Send a test trace** says `save an endpoint first` | The test uses the saved settings; choose **Save** first. |
| **Send a test trace** fails | The endpoint, headers or network are wrong; the page shows what the endpoint answered. |
| The counters show dropped spans | The endpoint was slow or refusing; see the last error on the page. |
| Nothing arrives in your tool | **Send traces** is off, or the endpoint should not include `/v1/traces` (Forgeline adds it). |
| A trace has no prompt or answer | **Keep prompts and answers in traces** is off, which is the default. |
| An older run has no trace | Only the newest 1,000 traces, and none older than 14 days, are kept. |
| The page shows no traces to a viewer | Traces are for developers and administrators. |

## Related

- [Assistant](assistant.md)
- [Audit log](audit-log.md)
- [Secrets and memory](secrets-and-memory.md)
- [Models and providers](models-and-providers.md)
